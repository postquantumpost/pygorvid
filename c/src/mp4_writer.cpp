#include "pygorvid/mp4_writer.hpp"

#include <initializer_list>
#include <array>
#include <limits>
#include <stdexcept>
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

bool valid_utf8(const std::string& value) {
    std::size_t index = 0;
    while (index < value.size()) {
        const auto first = static_cast<unsigned char>(value[index++]);
        if (first < 0x80) {
            continue;
        }
        int remaining;
        unsigned char lower = 0x80, upper = 0xbf;
        if (first >= 0xc2 && first <= 0xdf) {
            remaining = 1;
        } else if (first >= 0xe0 && first <= 0xef) {
            remaining = 2;
            if (first == 0xe0) lower = 0xa0;
            if (first == 0xed) upper = 0x9f;
        } else if (first >= 0xf0 && first <= 0xf4) {
            remaining = 3;
            if (first == 0xf0) lower = 0x90;
            if (first == 0xf4) upper = 0x8f;
        } else {
            return false;
        }
        if (value.size() - index < static_cast<std::size_t>(remaining)) {
            return false;
        }
        for (int continuation = 0; continuation < remaining; ++continuation) {
            const auto byte = static_cast<unsigned char>(value[index++]);
            if (byte < lower || byte > upper) return false;
            lower = 0x80;
            upper = 0xbf;
        }
    }
    return true;
}

std::array<std::pair<const char*, const std::string*>, 18>
metadata_entries(const Mp4Metadata* metadata) {
    return {{
        {"title", &metadata->title}, {"artist", &metadata->artist},
        {"album_artist", &metadata->album_artist}, {"genre", &metadata->genre},
        {"track", &metadata->track}, {"disc", &metadata->disc}, {"date", &metadata->date},
        {"composer", &metadata->composer}, {"comment", &metadata->comment},
        {"description", &metadata->description}, {"copyright", &metadata->copyright},
        {"make", &metadata->make}, {"model", &metadata->model}, {"firmware", &metadata->firmware},
        {"location", &metadata->location}, {"altitude", &metadata->altitude},
        {"heading", &metadata->heading}, {"encoder", &metadata->encoder},
    }};
}

Bytes metadata_box(const Mp4Metadata* metadata) {
    if (!metadata) return {};
    const auto entries = metadata_entries(metadata);
    std::uint64_t length = 69;
    std::uint32_t count = 0;
    for (const auto& entry : entries) {
        if (!valid_utf8(*entry.second)) {
            throw std::invalid_argument(std::string("MP4 metadata ") + entry.first + " is not valid UTF-8");
        }
        if (!entry.second->empty()) {
            length += 32 + std::string(entry.first).size() + entry.second->size();
            ++count;
        }
    }
    if (!count) return {};
    if (length > std::numeric_limits<std::uint32_t>::max()) {
        throw std::length_error("MP4 metadata exceeds the 32-bit box size limit");
    }
    Bytes keys, items;
    std::uint32_t index = 0;
    for (const auto& entry : entries) {
        if (entry.second->empty()) continue;
        ++index;
        const auto key = box("mdta", {text(entry.first)});
        keys.insert(keys.end(), key.begin(), key.end());
        const auto index_bytes = u32s({index});
        const auto item = box(std::string(index_bytes.begin(), index_bytes.end()), {
            full_box("data", 1, join({u32s({0}), text(*entry.second)})),
        });
        items.insert(items.end(), item.begin(), item.end());
    }
    const auto handler = full_box("hdlr", 0, join({u32s({0}), text("mdta"), Bytes(12), Bytes(1)}));
    return full_box("meta", 0, join({handler, full_box("keys", 0, join({u32s({count}), keys})),
                                   box("ilst", {items})}));
}

Bytes compatibility_metadata_box(const Mp4Metadata* metadata) {
    if (!metadata) return {};
    const auto entries = metadata_entries(metadata);
    const std::array<std::pair<const char*, const std::string*>, 12> text_tags{{
        {"\xa9nam", &metadata->title}, {"\xa9" "ART", &metadata->artist},
        {"aART", &metadata->album_artist}, {"\xa9gen", &metadata->genre},
        {"\xa9" "day", &metadata->date}, {"\xa9wrt", &metadata->composer},
        {"\xa9" "com", &metadata->composer}, {"\xa9" "cmt", &metadata->comment},
        {"desc", &metadata->description}, {"cprt", &metadata->copyright},
        {"\xa9" "cpy", &metadata->copyright}, {"\xa9too", &metadata->encoder},
    }};
    std::uint64_t length = 61 + 32 + 30;
    std::size_t count = 0;
    for (const auto& entry : entries) {
        if (!entry.second->empty()) {
            length += 64 + std::string(entry.first).size() + entry.second->size();
            ++count;
        }
    }
    if (!count) return {};
    for (const auto& entry : text_tags) {
        if (!entry.second->empty()) length += 24 + entry.second->size();
    }
    if (length > std::numeric_limits<std::uint32_t>::max()) {
        throw std::length_error("MP4 compatibility metadata exceeds the 32-bit box size limit");
    }
    Bytes items;
    const auto append_item = [&items](const Bytes& item) {
        items.insert(items.end(), item.begin(), item.end());
    };
    for (const auto& entry : text_tags) {
        if (!entry.second->empty()) {
            append_item(box(entry.first, {full_box("data", 1, join({u32s({0}), text(*entry.second)}))}));
        }
    }
    for (const auto& entry : {std::make_pair("trkn", &metadata->track),
                              std::make_pair("disk", &metadata->disc)}) {
        const auto& value = *entry.second;
        if (value.empty() || value.size() > 5) continue;
        std::uint32_t number = 0;
        bool digits = true;
        for (const auto ch : value) {
            if (ch < '0' || ch > '9') { digits = false; break; }
            number = number * 10 + static_cast<unsigned>(ch - '0');
        }
        // Arbitrary text remains in mdta and free-form tags.
        if (!digits || number > 65535) continue;
        auto data = u16s({0, static_cast<std::uint16_t>(number), 0});
        if (entry.first == std::string("trkn")) data.insert(data.end(), 2, 0);
        append_item(box(entry.first, {full_box("data", 0, join({u32s({0}), data}))}));
    }
    for (const auto& entry : entries) {
        if (!entry.second->empty()) {
            append_item(box("----", {
                full_box("mean", 0, text("com.apple.iTunes")),
                full_box("name", 0, text(entry.first)),
                full_box("data", 1, join({u32s({0}), text(*entry.second)})),
            }));
        }
    }
    const auto handler = full_box("hdlr", 0, join({u32s({0}), text("mdirappl"), Bytes(8), Bytes(1)}));
    return box("udta", {full_box("meta", 0, join({handler, box("ilst", {items})}))});
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

Bytes Mp4Writer::encode(const H264Encoder& encoder, const Mp4Metadata* metadata) const {
    const auto metadata_bytes = metadata_box(metadata);
    const auto compatibility_bytes = compatibility_metadata_box(metadata);
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
    return join({ftyp, box("mdat", {video, audio}),
                 box("moov", {mvhd, video_track, audio_track, metadata_bytes, compatibility_bytes})});
}

} // namespace pygorvid
