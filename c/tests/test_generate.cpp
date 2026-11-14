#include "pygorvid/h264.hpp"
#include "pygorvid/h264_encoder.hpp"
#include "pygorvid/mp4_writer.hpp"

#include <cassert>
#include <cstdint>
#include <vector>

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
    assert((std::vector<std::uint8_t>(payload.begin() + 4, payload.begin() + 8) ==
            std::vector<std::uint8_t>{'f', 't', 'y', 'p'}));
}
