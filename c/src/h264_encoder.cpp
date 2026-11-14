#include "pygorvid/h264_encoder.hpp"

#include <cstddef>

namespace pygorvid {
namespace {

class BitWriter {
public:
    std::vector<std::uint8_t> data;
    std::size_t bit_count = 0;

    void bits(std::uint32_t value, int count) {
        for (int shift = count - 1; shift >= 0; --shift) {
            if (bit_count % 8 == 0) {
                data.push_back(0);
            }
            data.back() |= static_cast<std::uint8_t>(
                ((value >> shift) & 1) << (7 - bit_count % 8));
            ++bit_count;
        }
    }

    void ue(std::uint32_t value) {
        int count = 0;
        for (auto remaining = value + 1; remaining; remaining >>= 1) {
            ++count;
        }
        bits(0, count - 1);
        bits(value + 1, count);
    }

    void align() {
        while (bit_count % 8) {
            bits(0, 1);
        }
    }

    std::vector<std::uint8_t> nal(std::uint8_t header) {
        bits(1, 1);
        align();
        std::vector<std::uint8_t> result{header};
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
};

} // namespace

std::vector<std::uint8_t> H264Encoder::sequence_parameter_set() const {
    BitWriter writer;
    writer.bits(66, 8);
    writer.bits(0xc0, 8);
    writer.bits(20, 8);
    for (const auto value : {0u, 0u, 2u, 1u}) {
        writer.ue(value);
    }
    writer.bits(0, 1);
    writer.ue(3);
    writer.ue(3);
    writer.bits(0b1100, 4);
    return writer.nal(0x67);
}

std::vector<std::uint8_t> H264Encoder::picture_parameter_set() const {
    BitWriter writer;
    writer.ue(0);
    writer.ue(0);
    writer.bits(0, 2);
    for (int index = 0; index < 3; ++index) {
        writer.ue(0);
    }
    writer.bits(0, 3);
    for (int index = 0; index < 3; ++index) {
        writer.ue(0);
    }
    writer.bits(0b100, 3);
    return writer.nal(0x68);
}

std::vector<std::uint8_t> H264Encoder::encode_grey_frame(std::uint16_t idr_pic_id) const {
    BitWriter writer;
    for (const auto value : {0u, 2u, 0u}) {
        writer.ue(value);
    }
    writer.bits(0, 4);
    writer.ue(idr_pic_id);
    writer.bits(0, 2);
    writer.ue(0);
    writer.ue(1);
    for (int macroblock = 0; macroblock < 16; ++macroblock) {
        writer.ue(25);
        writer.align();
        writer.data.insert(writer.data.end(), 384, 128);
        writer.bit_count += 384 * 8;
    }
    const auto nal = writer.nal(0x65);
    const auto size = static_cast<std::uint32_t>(nal.size());
    std::vector<std::uint8_t> sample;
    for (int shift = 24; shift >= 0; shift -= 8) {
        sample.push_back(static_cast<std::uint8_t>(size >> shift));
    }
    sample.insert(sample.end(), nal.begin(), nal.end());
    return sample;
}

} // namespace pygorvid
