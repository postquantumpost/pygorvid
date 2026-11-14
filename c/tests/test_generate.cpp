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
    assert(tagged.size() - payload.size() == 69 + 32 + 5 + metadata.title.size());
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
