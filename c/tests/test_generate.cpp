#include "pygorvid/h264.hpp"
#include "pygorvid/h264_encoder.hpp"
#include "pygorvid/mp4_writer.hpp"

#include <cassert>
#include <algorithm>
#include <cstdint>
#include <vector>
#include <stdexcept>

int main() {
    const pygorvid::H264Encoder encoder;
    const auto sps = pygorvid::parse_sps(encoder.sequence_parameter_set());
    const auto pps = pygorvid::parse_pps(encoder.picture_parameter_set());
    for (const std::uint16_t index : {0, 24, 65535}) {
        const auto sample = encoder.encode_grey_frame(index);
        const auto frame = pygorvid::decode_idr_i_pcm_sample(sample, 4, sps, pps);
        assert(frame.width == 64 && frame.height == 64);
        assert(frame.luma == std::vector<std::uint8_t>(4096, 128));
        assert(frame.chroma_u == std::vector<std::uint8_t>(1024, 128));
        assert(frame.chroma_v == std::vector<std::uint8_t>(1024, 128));
    }
    const pygorvid::Mp4Writer writer;
    const auto payload = writer.encode(encoder);
    assert(payload.size() < 400000);
    assert(payload == writer.encode(encoder));
    const pygorvid::Mp4Metadata empty;
    assert(payload == writer.encode(encoder, &empty));
    pygorvid::Mp4Metadata metadata;
    metadata.title = u8"Caf\u00e9 \u65e5\u672c \U0001f4f7";
    metadata.title += std::string("\0tail", 5);
    const auto tagged = writer.encode(encoder, &metadata);
    assert(std::search(tagged.begin(), tagged.end(), metadata.title.begin(), metadata.title.end(),
        [](std::uint8_t byte, char ch) { return byte == static_cast<std::uint8_t>(ch); }) != tagged.end());
    assert(tagged.size() > payload.size() + 69 + 32 + 5 + metadata.title.size());
    const std::string handler = "mdirappl";
    assert(std::search(tagged.begin(), tagged.end(), handler.begin(), handler.end()) != tagged.end());
    pygorvid::Mp4Metadata windows;
    windows.title = u8"A\u00e9\u65e5\U0001f4f7";
    windows.title += std::string("\0tail", 5);
    const std::vector<std::uint8_t> utf16{
        'A', 0, 0xe9, 0, 0xe5, 0x65, 0x3d, 0xd8, 0xf7, 0xdc, 0, 0,
        't', 0, 'a', 0, 'i', 0, 'l', 0, 0, 0,
    };
    std::vector<std::uint8_t> xtra{0, 0, 0, 107, 'X', 't', 'r', 'a'};
    for (const auto& name : {std::string("Title"), std::string("pygorvid_title")}) {
        xtra.insert(xtra.end(), {0, 0, 0, static_cast<std::uint8_t>(18 + name.size() + utf16.size()),
                                0, 0, 0, static_cast<std::uint8_t>(name.size())});
        xtra.insert(xtra.end(), name.begin(), name.end());
        xtra.insert(xtra.end(), {0, 0, 0, 1, 0, 0, 0, 28, 0, 8});
        xtra.insert(xtra.end(), utf16.begin(), utf16.end());
    }
    const auto windows_payload = writer.encode(encoder, &windows);
    assert(std::search(windows_payload.begin(), windows_payload.end(), xtra.begin(), xtra.end()) != windows_payload.end());
    const std::string xtra_tag = "Xtra";
    assert(std::search(payload.begin(), payload.end(), xtra_tag.begin(), xtra_tag.end()) == payload.end());
    for (const auto& date : {"2016", "2016-12-31 23:59:60 UTC", "2016junk", "-2016", u8"\u0662\u0660\u0661\u0666", ""}) {
        windows.title.clear();
        windows.date = date;
        const auto date_payload = writer.encode(encoder, &windows);
        const std::string year_tag = "WM/Year";
        const bool year = windows.date == "2016" || windows.date == "2016-12-31 23:59:60 UTC";
        assert((std::search(date_payload.begin(), date_payload.end(), year_tag.begin(), year_tag.end()) != date_payload.end()) == year);
    }
    for (const auto& number : {"0", "1", "001", "65535", "65536", "-1", "1/2", u8"\u0661"}) {
        pygorvid::Mp4Metadata numeric;
        numeric.track = number;
        numeric.disc = number;
        const auto encoded = writer.encode(encoder, &numeric);
        const std::string track_tag = "trkn";
        const std::string disc_tag = "disk";
        const bool binary = numeric.track == "0" || numeric.track == "1" ||
                            numeric.track == "001" || numeric.track == "65535";
        assert((std::search(encoded.begin(), encoded.end(), track_tag.begin(), track_tag.end()) != encoded.end()) == binary);
        assert((std::search(encoded.begin(), encoded.end(), disc_tag.begin(), disc_tag.end()) != encoded.end()) == binary);
    }
    for (const auto& invalid : {std::string("\xff"), std::string("\xc0\x80"),
            std::string("\xed\xa0\x80"), std::string("\xf4\x90\x80\x80"), std::string("\xe2\x82")}) {
        metadata.title = invalid;
        bool rejected = false;
        try {
            (void)writer.encode(encoder, &metadata);
        } catch (const std::invalid_argument& error) {
            rejected = std::string(error.what()).find("title is not valid UTF-8") != std::string::npos;
        }
        assert(rejected);
    }
    assert((std::vector<std::uint8_t>(payload.begin() + 4, payload.begin() + 8) ==
            std::vector<std::uint8_t>{'f', 't', 'y', 'p'}));
}
