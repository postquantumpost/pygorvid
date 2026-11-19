#include "pygorvid/h264_annexb.hpp"

#include <array>
#include <initializer_list>
#include <limits>
#include <stdexcept>
#include <string>
#include <utility>

namespace pygorvid {
namespace {

using Bytes = std::vector<std::uint8_t>;

class BitWriter {
public:
    void bits(std::uint32_t value, int count) {
        for (int shift = count - 1; shift >= 0; --shift) {
            if (bit_count % 8 == 0) data.push_back(0);
            data.back() |= static_cast<std::uint8_t>(
                ((value >> shift) & 1) << (7 - bit_count % 8));
            ++bit_count;
        }
    }

    void ue(std::uint32_t value) {
        int count = 0;
        for (auto remaining = value + 1; remaining; remaining >>= 1) ++count;
        bits(0, count - 1);
        bits(value + 1, count);
    }

    void se(std::int32_t value) {
        ue(value > 0 ? static_cast<std::uint32_t>(2 * value - 1)
                     : static_cast<std::uint32_t>(-2 * value));
    }

    void align() {
        while (bit_count % 8) bits(0, 1);
    }

    Bytes nal(std::uint8_t header) {
        bits(1, 1);
        align();
        Bytes result{header};
        int zeros = 0;
        for (const auto value : data) {
            if (zeros == 2 && value <= 3) {
                result.push_back(3);
                zeros = 0;
            }
            result.push_back(value);
            zeros = value == 0 ? zeros + 1 : 0;
        }
        return result;
    }

    Bytes data;
    std::size_t bit_count = 0;
};

struct Sequence {
    std::uint8_t profile_idc;
    std::uint8_t chroma_format_idc = 1;
    bool separate_colour_plane = false;
    std::uint8_t bit_depth_luma = 8;
    std::uint8_t bit_depth_chroma = 8;
    bool frame_mbs_only = true;
    bool mb_adaptive_frame_field = false;
    bool gaps_allowed = false;
    std::uint8_t poc_type = 2;
    int width_mbs = 4;
    int height_mbs = 4;
    std::uint32_t sps_id = 0;
};

Bytes sps(const Sequence& sequence) {
    BitWriter writer;
    writer.bits(sequence.profile_idc, 8);
    writer.bits(0, 8);
    writer.bits(20, 8);
    writer.ue(sequence.sps_id);
    switch (sequence.profile_idc) {
    case 44: case 83: case 86: case 100: case 110: case 118: case 122:
    case 128: case 134: case 135: case 138: case 139: case 144: case 244:
        writer.ue(sequence.chroma_format_idc);
        if (sequence.chroma_format_idc == 3)
            writer.bits(sequence.separate_colour_plane, 1);
        writer.ue(sequence.bit_depth_luma - 8);
        writer.ue(sequence.bit_depth_chroma - 8);
        writer.bits(0, 2);
        break;
    default:
        break;
    }
    writer.ue(0);
    writer.ue(sequence.poc_type);
    if (sequence.poc_type == 0) writer.ue(2);
    writer.ue(2);
    writer.bits(sequence.gaps_allowed, 1);
    writer.ue(sequence.width_mbs - 1);
    const int height_units = sequence.frame_mbs_only
        ? sequence.height_mbs : sequence.height_mbs / 2;
    writer.ue(height_units - 1);
    writer.bits(sequence.frame_mbs_only, 1);
    if (!sequence.frame_mbs_only) writer.bits(sequence.mb_adaptive_frame_field, 1);
    writer.bits(1, 1);
    writer.bits(0, 2);
    return writer.nal(0x67);
}

Bytes pps(bool redundant = false, bool transform_8x8 = false,
         int slice_groups = 0, int map_type = 0) {
    BitWriter writer;
    writer.ue(0);
    writer.ue(0);
    writer.bits(0, 1);
    writer.bits(0, 1);
    writer.ue(slice_groups);
    if (slice_groups) {
        writer.ue(map_type);
        if (map_type == 0) {
            writer.ue(7);
            writer.ue(7);
        }
    }
    writer.ue(0);
    writer.ue(0);
    writer.bits(0, 1);
    writer.bits(0, 2);
    writer.se(0);
    writer.se(0);
    writer.se(0);
    writer.bits(1, 1);
    writer.bits(0, 1);
    writer.bits(redundant, 1);
    if (transform_8x8) {
        writer.bits(1, 1);
        writer.bits(0, 1);
        writer.se(0);
    }
    return writer.nal(0x68);
}

void slice_header(BitWriter& writer, int first_mb, const Sequence& sequence,
                  bool idr = true, int field = -1, int colour_plane = -1,
                  int frame_num = 0, int redundant_pic_cnt = -1) {
    writer.ue(first_mb);
    writer.ue(2);
    writer.ue(0);
    if (sequence.separate_colour_plane) {
        writer.bits(colour_plane < 0 ? 0 : colour_plane, 2);
    }
    writer.bits(frame_num, 4);
    if (!sequence.frame_mbs_only) {
        if (field < 0) writer.bits(0, 1);
        else {
            writer.bits(1, 1);
            writer.bits(field, 1);
        }
    }
    if (idr) writer.ue(0);
    if (sequence.poc_type == 0) writer.bits(0, 6);
    if (redundant_pic_cnt >= 0) writer.ue(redundant_pic_cnt);
    writer.bits(0, idr ? 2 : 1);
    writer.se(0);
    writer.ue(1);
}

void pcm_macroblocks(BitWriter& writer, const Sequence& sequence, int count,
                     bool field_mbaff = false) {
    for (int macroblock = 0; macroblock < count; ++macroblock) {
        if (field_mbaff && macroblock % 2 == 0)
            writer.bits((macroblock / 2) % 2, 1);
        writer.ue(25);
        writer.align();
        const int planes = sequence.separate_colour_plane ? 1 : 3;
        for (int plane = 0; plane < planes; ++plane) {
            int sample_count = 256;
            std::uint8_t depth = sequence.bit_depth_luma;
            if (!sequence.separate_colour_plane && plane) {
                sample_count = sequence.chroma_format_idc == 1 ? 64
                    : sequence.chroma_format_idc == 2 ? 128 : 256;
                depth = sequence.bit_depth_chroma;
            }
            const std::uint32_t maximum = (1U << depth) - 1;
            const std::uint32_t value =
                (macroblock * 11 + 32 + plane * 47) & maximum;
            if (depth == 8) {
                writer.data.insert(writer.data.end(), sample_count,
                                   static_cast<std::uint8_t>(value));
                writer.bit_count += static_cast<std::size_t>(sample_count) * 8;
            } else {
                for (int index = 0; index < sample_count; ++index)
                    writer.bits(value, depth);
            }
        }
    }
}

Bytes idr_picture(const Sequence& sequence, bool idr = true, int field = -1,
                  int colour_plane = -1, int first_mb = 0,
                  int macroblock_count = -1, int frame_num = 0,
                  int redundant_pic_cnt = -1) {
    BitWriter writer;
    slice_header(writer, first_mb, sequence, idr, field, colour_plane,
                 frame_num, redundant_pic_cnt);
    int count = macroblock_count;
    if (count < 0) {
        count = sequence.width_mbs * sequence.height_mbs;
        if (field >= 0) count /= 2;
    }
    pcm_macroblocks(writer, sequence, count,
                    !sequence.frame_mbs_only &&
                    sequence.mb_adaptive_frame_field);
    return writer.nal(idr ? 0x65 : 0x21);
}

Bytes annex_b(const std::vector<Bytes>& nals) {
    Bytes result;
    for (const auto& nal : nals) {
        result.insert(result.end(), {0, 0, 0, 1});
        result.insert(result.end(), nal.begin(), nal.end());
    }
    return result;
}

Bytes u32s(std::initializer_list<std::uint32_t> values) {
    Bytes result;
    for (const auto value : values) {
        for (int shift = 24; shift >= 0; shift -= 8) {
            result.push_back(static_cast<std::uint8_t>(value >> shift));
        }
    }
    return result;
}

Bytes u16s(std::initializer_list<std::uint16_t> values) {
    Bytes result;
    for (const auto value : values) {
        result.push_back(static_cast<std::uint8_t>(value >> 8));
        result.push_back(static_cast<std::uint8_t>(value));
    }
    return result;
}

Bytes text(const std::string& value) {
    return Bytes(value.begin(), value.end());
}

Bytes box(const std::string& kind, std::initializer_list<Bytes> parts) {
    std::size_t length = 8;
    for (const auto& part : parts) {
        if (part.size() > std::numeric_limits<std::uint32_t>::max() - length) {
            throw std::length_error("MP4 box exceeds the 32-bit size limit");
        }
        length += part.size();
    }
    Bytes result = u32s({static_cast<std::uint32_t>(length)});
    result.reserve(length);
    result.insert(result.end(), kind.begin(), kind.end());
    for (const auto& part : parts) {
        result.insert(result.end(), part.begin(), part.end());
    }
    return result;
}

Bytes full_box(const std::string& kind, std::uint32_t flags,
               std::initializer_list<Bytes> parts) {
    Bytes result = u32s({flags});
    for (const auto& part : parts) {
        result.insert(result.end(), part.begin(), part.end());
    }
    return box(kind, {std::move(result)});
}

Bytes matrix() {
    return u32s({0x10000, 0, 0, 0, 0x10000, 0, 0, 0, 0x40000000});
}

Bytes mp4_track(const Bytes& entry, const Bytes& sizes,
                std::uint32_t sample_count, std::uint32_t offset) {
    const auto tkhd = full_box("tkhd", 7, {
        u32s({0, 0, 1, 0, sample_count}), Bytes(8), u16s({0, 0, 0, 0}),
        matrix(), u32s({64u << 16, 64u << 16}),
    });
    const auto mdhd = full_box("mdhd", 0, {
        u32s({0, 0, 60, sample_count}), u16s({0x55c4, 0}),
    });
    const auto hdlr = full_box("hdlr", 0, {
        u32s({0}), text("vide"), Bytes(12), text("Video"), Bytes(1),
    });
    const auto media_header = full_box("vmhd", 1, {Bytes(8)});
    const auto dinf = box("dinf", {
        full_box("dref", 0, {u32s({1}), full_box("url ", 1, {})}),
    });
    auto sync = u32s({sample_count});
    for (std::uint32_t sample = 1; sample <= sample_count; ++sample) {
        const auto value = u32s({sample});
        sync.insert(sync.end(), value.begin(), value.end());
    }
    const auto stbl = box("stbl", {
        full_box("stsd", 0, {u32s({1}), entry}),
        full_box("stts", 0, {u32s({1, sample_count, 1})}),
        full_box("stsc", 0, {u32s({1, 1, sample_count, 1})}),
        sizes,
        full_box("stss", 0, {std::move(sync)}),
        full_box("stco", 0, {u32s({1, offset})}),
    });
    return box("trak", {
        std::move(tkhd),
        box("mdia", {
            std::move(mdhd), std::move(hdlr),
            box("minf", {std::move(media_header), std::move(dinf),
                         std::move(stbl)}),
        }),
    });
}

std::vector<Bytes> annex_b_nals(const Bytes& data) {
    std::vector<std::size_t> starts;
    for (std::size_t index = 0; index + 4 <= data.size(); ++index) {
        if (data[index] == 0 && data[index + 1] == 0 &&
            data[index + 2] == 0 && data[index + 3] == 1) {
            starts.push_back(index);
        }
    }
    if (starts.empty()) {
        throw std::invalid_argument("companion stream contains no Annex-B NAL units");
    }
    std::vector<Bytes> result;
    for (std::size_t index = 0; index < starts.size(); ++index) {
        const auto first = starts[index] + 4;
        const auto last = index + 1 < starts.size() ? starts[index + 1]
                                                   : data.size();
        if (first == last) {
            throw std::invalid_argument("companion stream contains an empty NAL unit");
        }
        result.emplace_back(data.begin() + static_cast<std::ptrdiff_t>(first),
                            data.begin() + static_cast<std::ptrdiff_t>(last));
    }
    return result;
}

Bytes mux_avc_mp4(const Bytes& annex, std::uint32_t sample_count) {
    if (!sample_count) {
        throw std::invalid_argument("companion sample count must be positive");
    }
    const auto nals = annex_b_nals(annex);
    std::vector<Bytes> sequence_sets, picture_sets, picture_nals;
    for (const auto& nal : nals) {
        const auto type = nal.front() & 31;
        if (type == 7) sequence_sets.push_back(nal);
        else if (type == 8) picture_sets.push_back(nal);
        else picture_nals.push_back(nal);
    }
    if (sequence_sets.size() != 1 || picture_sets.size() != 1) {
        throw std::invalid_argument(
            "companion MP4 requires exactly one SPS and one PPS");
    }

    std::vector<std::vector<Bytes>> grouped(sample_count);
    if (sample_count == 1) {
        grouped.front() = picture_nals;
    } else {
        std::vector<std::vector<Bytes>> aud_groups;
        std::vector<Bytes> current;
        for (const auto& nal : picture_nals) {
            if ((nal.front() & 31) == 9 && !current.empty()) {
                aud_groups.push_back(std::move(current));
                current.clear();
            }
            current.push_back(nal);
        }
        if (!current.empty()) aud_groups.push_back(std::move(current));
        if (aud_groups.size() == sample_count) {
            grouped = std::move(aud_groups);
        } else {
            grouped.clear();
            for (const auto& nal : picture_nals) {
                if ((nal.front() & 31) == 1 || (nal.front() & 31) == 5) {
                    grouped.push_back({nal});
                }
            }
            if (grouped.size() != sample_count) {
                throw std::invalid_argument("companion sample grouping is ambiguous");
            }
        }
    }

    Bytes data;
    Bytes sample_sizes = u32s({0, sample_count});
    for (const auto& group : grouped) {
        Bytes sample;
        for (const auto& nal : group) {
            const auto length = u32s({static_cast<std::uint32_t>(nal.size())});
            sample.insert(sample.end(), length.begin(), length.end());
            sample.insert(sample.end(), nal.begin(), nal.end());
        }
        const auto size = u32s({static_cast<std::uint32_t>(sample.size())});
        sample_sizes.insert(sample_sizes.end(), size.begin(), size.end());
        data.insert(data.end(), sample.begin(), sample.end());
    }
    const auto sizes = full_box("stsz", 0, {std::move(sample_sizes)});
    const auto& sps = sequence_sets.front();
    const auto& pps = picture_sets.front();
    const auto avcc = box("avcC", {
        Bytes{1, sps[1], sps[2], sps[3], 0xff, 0xe1},
        u16s({static_cast<std::uint16_t>(sps.size())}), sps, Bytes{1},
        u16s({static_cast<std::uint16_t>(pps.size())}), pps,
    });
    const auto visual = box("avc1", {
        Bytes(6), u16s({1}), Bytes(16), u16s({64, 64}),
        u32s({0x480000, 0x480000, 0}), u16s({1}), Bytes(32),
        u16s({24, 0xffff}), avcc,
    });
    const auto ftyp = box("ftyp", {
        text("isom"), u32s({0x200}), text("isomiso2avc1mp41"),
    });
    const auto initial_track = mp4_track(visual, sizes, sample_count, 0);
    const auto movie_header = full_box("mvhd", 0, {
        u32s({0, 0, 600, sample_count * 10}), u16s({1, 0}), Bytes(8),
        u32s({0x00010000, 0x01000000, 0}), Bytes(36),
        u32s({0, 0, 0, 0, 0, 0}), u32s({2}),
    });
    const auto movie = box("moov", {movie_header, initial_track});
    const auto video_offset =
        static_cast<std::uint32_t>(ftyp.size() + movie.size() + 8);
    const auto final_movie = box("moov", {
        movie_header, mp4_track(visual, sizes, sample_count, video_offset),
    });
    Bytes result;
    const auto media = box("mdat", {std::move(data)});
    for (const auto* part : {&ftyp, &final_movie, &media}) {
        result.insert(result.end(), part->begin(), part->end());
    }
    return result;
}

Bytes aud() {
    BitWriter writer;
    writer.bits(0, 8);
    return writer.nal(0x09);
}

Bytes partition_a_nal(int macroblock) {
    BitWriter writer;
    writer.ue(macroblock); writer.ue(2); writer.ue(0);
    writer.bits(1, 4); writer.bits(0, 1); writer.se(0);
    writer.ue(1); writer.ue(macroblock); writer.ue(25);
    return writer.nal(0x42);
}

Bytes partition_b_nal(int macroblock) {
    BitWriter writer;
    writer.ue(macroblock);
    writer.align();
    for (const auto& [value, count] :
         {std::pair<int, int>{112 + macroblock, 256},
          std::pair<int, int>{144 + macroblock, 64},
          std::pair<int, int>{160 + macroblock, 64}}) {
        writer.data.insert(writer.data.end(), count,
                           static_cast<std::uint8_t>(value));
        writer.bit_count += static_cast<std::size_t>(count) * 8;
    }
    return writer.nal(0x43);
}

Bytes partition_c_nal(int macroblock) {
    BitWriter writer;
    writer.ue(macroblock);
    return writer.nal(0x44);
}

Bytes switching_slice(bool sp) {
    BitWriter writer;
    writer.ue(0); writer.ue(sp ? 3 : 4); writer.ue(0);
    writer.bits(sp ? 1 : 2, 4);
    if (sp) { writer.bits(0, 1); writer.bits(0, 1); }
    writer.bits(0, 1);
    writer.se(0);
    if (sp) writer.bits(0, 1);
    writer.se(0);
    writer.ue(1);
    if (sp) writer.ue(16);
    else {
        for (int mb = 0; mb < 16; ++mb) {
            writer.ue(26);
            writer.align();
            writer.data.insert(writer.data.end(), 384, 128);
            writer.bit_count += 384 * 8;
        }
    }
    return writer.nal(0x21);
}

Bytes extension_nal(const std::string& family) {
    Bytes result;
    if (family == "svc") result.insert(result.end(), {0,0,0,1,0x6e,0x80});
    result.insert(result.end(), {0,0,0,1});
    if (family == "svc")
        result.insert(result.end(), {0x74,0x80,0x00,0x00,0x80});
    else if (family == "mvc")
        result.insert(result.end(), {0x74,0x00,0x00,0x00,0x80});
    else if (family == "mvcd")
        result.insert(result.end(), {0x75,0x80,0x00,0x00,0x80});
    else
        result.insert(result.end(), {0x75,0xc0,0x00,0x00,0x80});
    return result;
}

H264AnnexBStream stream(const std::string& suffix, Bytes data) {
    return {suffix, std::move(data)};
}

} // namespace

std::vector<H264AnnexBStream> generate_h264_annex_b_streams() {
    const Sequence chroma422{122, 2};
    const Sequence chroma444{244, 3};
    const Sequence separate444{244, 3, true};
    const Sequence bit10{110, 1, false, 10, 10};
    const Sequence bit12{244, 3, false, 12, 12};
    const Sequence fields{100, 1, false, 8, 8, false, false};
    const Sequence mbaff{100, 1, false, 8, 8, false, true};
    const Sequence base{100};
    const Sequence extended{88};
    const auto extension_base =
        annex_b({sps(base), pps(), idr_picture(base)});
    auto appended = [&extension_base](const std::string& family) {
        auto data = extension_base;
        const auto extension = extension_nal(family);
        data.insert(data.end(), extension.begin(), extension.end());
        return data;
    };
    return {
        stream("frame-40-422.264", annex_b({sps(chroma422), pps(),
            idr_picture(chroma422)})),
        stream("frame-41-444.264", annex_b({sps(chroma444),
            pps(false, true), idr_picture(chroma444)})),
        stream("frame-41-separate.264", annex_b({sps(separate444),
            pps(false, true), idr_picture(separate444, true, -1, 0),
            idr_picture(separate444, true, -1, 1),
            idr_picture(separate444, true, -1, 2)})),
        stream("frame-42-10bit.264", annex_b({sps(bit10), pps(),
            idr_picture(bit10)})),
        stream("frame-42-12bit.264", annex_b({sps(bit12), pps(),
            idr_picture(bit12)})),
        stream("frame-43-fields.264", annex_b({sps(fields), pps(),
            idr_picture(fields, true, 0),
            idr_picture(fields, false, 1, -1, 0, -1, 1)})),
        stream("frame-44-mbaff.264", annex_b({sps(mbaff), pps(),
            idr_picture(mbaff)})),
        stream("frame-45-fmo.264", annex_b({sps(base),
            pps(false, false, 1, 0), idr_picture(base, true, -1, -1, 0, 8),
            idr_picture(base, true, -1, -1, 1, 8)})),
        stream("frame-46-redundant.264", annex_b({sps(base),
            pps(true), aud(), idr_picture(base, true, -1, -1, 0, -1, 0, 0),
            aud(), idr_picture(base, false, -1, -1, 0, -1, 1, 0),
            aud(), idr_picture(base, false, -1, -1, 0, -1, 1, 1)})),
        stream("frame-47-partitions.264", [&] {
            std::vector<Bytes> nals{sps(base), pps(), aud(),
                                    idr_picture(base), aud()};
            for (int macroblock = 0; macroblock < 16; ++macroblock) {
                nals.push_back(partition_a_nal(macroblock));
                nals.push_back(partition_b_nal(macroblock));
                nals.push_back(partition_c_nal(macroblock));
            }
            return annex_b(nals);
        }()),
        stream("frame-48-sp-si.264", annex_b({sps(extended), pps(), aud(),
            idr_picture(extended), aud(), switching_slice(true),
            aud(), switching_slice(false)})),
        stream("frame-49-svc.264", appended("svc")),
        stream("frame-49-mvc.264", appended("mvc")),
        stream("frame-49-mvcd.264", appended("mvcd")),
        stream("frame-49-3davc.264", appended("3davc")),
    };
}

std::vector<H264AnnexBStream> generate_h264_companion_files() {
    static const std::array<std::uint32_t, 11> sample_counts{{
        1, 1, 1, 1, 1, 2, 1, 1, 3, 2, 3,
    }};
    auto result = generate_h264_annex_b_streams();
    for (std::size_t index = 0; index < sample_counts.size(); ++index) {
        auto suffix = result[index].suffix;
        suffix.replace(suffix.size() - 4, 4, ".mp4");
        result[index] = stream(std::move(suffix),
            mux_avc_mp4(result[index].data, sample_counts[index]));
    }
    return result;
}

} // namespace pygorvid
