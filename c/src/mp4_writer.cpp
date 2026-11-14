#include "pygorvid/mp4_writer.hpp"

#include <initializer_list>
#include <string>

namespace pygorvid {
namespace {

using Bytes = std::vector<std::uint8_t>;

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

Bytes full_box(const std::string& kind, std::uint32_t flags, Bytes payload = {}) {
    return box(kind, {u32s({flags}), payload});
}

Bytes matrix() {
    return u32s({0x10000, 0, 0, 0, 0x10000, 0, 0, 0, 0x40000000});
}

Bytes join(std::initializer_list<Bytes> parts) {
    Bytes result;
    for (const auto& part : parts) {
        result.insert(result.end(), part.begin(), part.end());
    }
    return result;
}

Bytes track(std::uint32_t id, const Bytes& entry, const Bytes& sizes,
            std::uint32_t count, std::uint32_t timescale,
            std::uint32_t offset, bool video) {
    const std::uint32_t dimension = video ? 64u << 16 : 0;
    const std::uint16_t volume = video ? 0 : 0x100;
    const auto tkhd = full_box("tkhd", 7, join({
        u32s({0, 0, id, 0, 1000}), Bytes(8), u16s({0, 0, volume, 0}),
        matrix(), u32s({dimension, dimension}),
    }));
    const auto mdhd = full_box("mdhd", 0, join({
        u32s({0, 0, timescale, count}), u16s({0x55c4, 0}),
    }));
    const auto hdlr = full_box("hdlr", 0, join({
        u32s({0}), text(video ? "vide" : "soun"), Bytes(12),
        text(video ? "Video" : "Audio"), Bytes(1),
    }));
    const auto header = video ? full_box("vmhd", 1, Bytes(8))
                              : full_box("smhd", 0, Bytes(4));
    const auto dinf = box("dinf", {
        full_box("dref", 0, join({u32s({1}), full_box("url ", 1)})),
    });
    const auto stbl = box("stbl", {
        full_box("stsd", 0, join({u32s({1}), entry})),
        full_box("stts", 0, u32s({1, count, 1})),
        full_box("stsc", 0, u32s({1, 1, count, 1})), sizes,
        full_box("stco", 0, u32s({1, offset})),
    });
    return box("trak", {tkhd, box("mdia", {mdhd, hdlr, box("minf", {header, dinf, stbl})})});
}

} // namespace

Bytes Mp4Writer::encode(const H264Encoder& encoder) const {
    Bytes video;
    auto sizes = u32s({0, 25});
    for (std::uint16_t index = 0; index < 25; ++index) {
        const auto sample = encoder.encode_grey_frame(index);
        const auto size = u32s({static_cast<std::uint32_t>(sample.size())});
        sizes.insert(sizes.end(), size.begin(), size.end());
        video.insert(video.end(), sample.begin(), sample.end());
    }
    const Bytes audio(48000 * 2 * 2);
    const auto sps = encoder.sequence_parameter_set();
    const auto pps = encoder.picture_parameter_set();
    const auto avcc = box("avcC", {
        Bytes{1, sps[1], sps[2], sps[3], 0xff, 0xe1},
        u16s({static_cast<std::uint16_t>(sps.size())}), sps, Bytes{1},
        u16s({static_cast<std::uint16_t>(pps.size())}), pps,
    });
    const auto visual = box("avc1", {
        Bytes(6), u16s({1}), Bytes(16), u16s({64, 64}),
        u32s({0x480000, 0x480000, 0}), u16s({1}), Bytes(32), u16s({24, 0xffff}), avcc,
    });
    const auto sound = box("sowt", {
        Bytes(6), u16s({1}), Bytes(8), u16s({2, 16, 0, 0}), u32s({48000u << 16}),
    });
    const auto ftyp = box("ftyp", {text("isom"), u32s({0x200}), text("isomiso2avc1qt  ")});
    const auto offset = static_cast<std::uint32_t>(ftyp.size()) + 8;
    const auto video_track = track(1, visual, full_box("stsz", 0, sizes), 25, 25, offset, true);
    const auto audio_track = track(2, sound, full_box("stsz", 0, u32s({4, 48000})),
        48000, 48000, offset + static_cast<std::uint32_t>(video.size()), false);
    const auto mvhd = full_box("mvhd", 0, join({
        u32s({0, 0, 1000, 1000, 0x10000}), u16s({0x100, 0}), Bytes(8), matrix(),
        Bytes(24), u32s({3}),
    }));
    return join({ftyp, box("mdat", {video, audio}), box("moov", {mvhd, video_track, audio_track})});
}

} // namespace pygorvid
