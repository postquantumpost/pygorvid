#include "pygorvid/h264_annexb.hpp"

#include <array>
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

} // namespace pygorvid
