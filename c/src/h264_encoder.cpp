#include "pygorvid/h264_encoder.hpp"

#include <algorithm>
#include <array>
#include <cstddef>
#include <stdexcept>

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

    void se(std::int32_t value) {
        ue(value > 0 ? static_cast<std::uint32_t>(2 * value - 1)
                     : static_cast<std::uint32_t>(-2 * value));
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
    return encode_pcm_frame(idr_pic_id, false);
}

std::vector<std::uint8_t> H264Encoder::encode_generated_frame(std::uint16_t frame_index) const {
    if (frame_index == 3) {
        BitWriter writer;
        for (const auto value : {0u, 2u, 0u}) {
            writer.ue(value);
        }
        writer.bits(2, 4);
        writer.bits(0, 1);
        writer.ue(0);
        writer.ue(1);
        for (int macroblock = 0; macroblock < 16; ++macroblock) {
            writer.ue(3);
            writer.ue(0);
            writer.ue(0);
            writer.bits(1, 1);
        }
        const auto nal = writer.nal(0x21);
        const auto size = static_cast<std::uint32_t>(nal.size());
        std::vector<std::uint8_t> sample;
        for (int shift = 24; shift >= 0; shift -= 8) {
            sample.push_back(static_cast<std::uint8_t>(size >> shift));
        }
        sample.insert(sample.end(), nal.begin(), nal.end());
        return sample;
    }
    if (frame_index == 4) {
        BitWriter writer;
        for (const auto value : {0u, 2u, 0u}) {
            writer.ue(value);
        }
        writer.bits(3, 4);
        writer.bits(0, 1);
        writer.ue(0);
        writer.ue(1);
        const std::array<std::uint32_t, 16> luma_modes{
            2, 2, 2, 2, 0, 3, 2, 1, 0, 0, 1, 2, 0, 3, 2, 1};
        const std::array<std::uint32_t, 16> chroma_modes{
            0, 1, 1, 1, 2, 3, 0, 1, 2, 2, 3, 0, 2, 3, 2, 1};
        for (std::size_t index = 0; index < luma_modes.size(); ++index) {
            writer.ue(1 + luma_modes[index] + (index == 0 ? 8 : 0));
            writer.ue(chroma_modes[index]);
            writer.ue(0);
            if (index == 0) {
                writer.bits(0b01000011, 8);
                writer.bits(0b10000, 5);
                writer.bits(0b10000, 5);
                writer.bits(0xff, 8);
            } else {
                writer.bits(1, 1);
            }
        }
        const auto nal = writer.nal(0x21);
        const auto size = static_cast<std::uint32_t>(nal.size());
        std::vector<std::uint8_t> sample;
        for (int shift = 24; shift >= 0; shift -= 8) {
            sample.push_back(static_cast<std::uint8_t>(size >> shift));
        }
        sample.insert(sample.end(), nal.begin(), nal.end());
        return sample;
    }
    if (frame_index == 5) {
        BitWriter writer;
        for (const auto value : {0u, 2u, 0u}) {
            writer.ue(value);
        }
        writer.bits(4, 4);
        writer.bits(0, 1);
        writer.ue(0);
        writer.ue(1);
        for (int macroblock = 0; macroblock < 16; ++macroblock) {
            writer.ue(0);
            for (int block = 0; block < 16; ++block) {
                writer.bits(1, 1);
            }
            writer.ue(0);
            writer.ue(3);
        }
        const auto nal = writer.nal(0x21);
        const auto size = static_cast<std::uint32_t>(nal.size());
        std::vector<std::uint8_t> sample;
        for (int shift = 24; shift >= 0; shift -= 8) {
            sample.push_back(static_cast<std::uint8_t>(size >> shift));
        }
        sample.insert(sample.end(), nal.begin(), nal.end());
        return sample;
    }
    if (frame_index == 6 || frame_index == 7) {
        BitWriter writer;
        for (const auto value : {0u, 2u, 0u}) {
            writer.ue(value);
        }
        writer.bits(frame_index == 6 ? 5 : 6, 4);
        writer.bits(0, 1);
        writer.ue(0);
        writer.ue(1);

        writer.ue(25);
        writer.align();
        for (int row = 0; row < 16; ++row) {
            for (int column = 0; column < 16; ++column) {
                writer.data.push_back(
                    static_cast<std::uint8_t>(32 + 3 * column + 5 * row));
            }
        }
        for (const int base : {80, 176}) {
            for (int row = 0; row < 8; ++row) {
                for (int column = 0; column < 8; ++column) {
                    writer.data.push_back(
                        static_cast<std::uint8_t>(base + column + 2 * row));
                }
            }
        }
        writer.bit_count += 384 * 8;

        const std::array<int, 16> modes{
            2, 2, 2, 4, 8, 1, 5, 6, 7, 0, 3, 4, 5, 7, 6, 8};
        const std::array<int, 16> scan{
            0, 1, 4, 5, 2, 3, 6, 7, 8, 9, 12, 13, 10, 11, 14, 15};
        const auto scan_index = [&scan](int raster) {
            const auto found = std::find(scan.begin(), scan.end(), raster);
            if (found == scan.end()) {
                throw std::logic_error("invalid Intra_4x4 raster index");
            }
            return static_cast<std::size_t>(found - scan.begin());
        };
        std::array<std::array<int, 16>, 16> macroblock_modes{};
        macroblock_modes[0].fill(2);
        for (int macroblock = 1; macroblock < 16; ++macroblock) {
            writer.ue(0);
            const int mb_x = macroblock % 4;
            const int mb_y = macroblock / 4;
            for (std::size_t block = 0; block < modes.size(); ++block) {
                const int raster = scan[block];
                const int column = raster % 4;
                const int row = raster / 4;
                int left_mode = -1;
                int top_mode = -1;
                if (column) {
                    left_mode = modes[scan_index(raster - 1)];
                } else if (mb_x) {
                    left_mode = macroblock_modes[macroblock - 1][scan_index(row * 4 + 3)];
                }
                if (row) {
                    top_mode = modes[scan_index(raster - 4)];
                } else if (mb_y) {
                    top_mode = macroblock_modes[macroblock - 4][scan_index(12 + column)];
                }
                const int predicted = left_mode >= 0 && top_mode >= 0
                    ? std::min(left_mode, top_mode) : 2;
                const int desired = modes[block];
                if (desired == predicted) {
                    writer.bits(1, 1);
                } else {
                    writer.bits(0, 1);
                    writer.bits(desired - (desired >= predicted ? 1 : 0), 3);
                }
            }
            writer.ue(0);
            if (frame_index == 7 && macroblock == 15) {
                writer.ue(29);
                writer.se(1);
                // Positive trailing-one token, zero runs, then three empty blocks.
                writer.bits(0b01, 2);
                writer.bits(0, 1);
                writer.bits(1, 1);
                writer.bits(1, 1);
                writer.bits(1, 1);
                writer.bits(1, 1);
            } else {
                writer.ue(3);
            }
            macroblock_modes[macroblock] = modes;
        }
        const auto nal = writer.nal(0x21);
        const auto size = static_cast<std::uint32_t>(nal.size());
        std::vector<std::uint8_t> sample;
        for (int shift = 24; shift >= 0; shift -= 8) {
            sample.push_back(static_cast<std::uint8_t>(size >> shift));
        }
        sample.insert(sample.end(), nal.begin(), nal.end());
        return sample;
    }
    return encode_pcm_frame(frame_index, frame_index == 1);
}

std::vector<std::uint8_t> H264Encoder::encode_pcm_frame(
    std::uint16_t idr_pic_id, bool emulation_prevention_test) const {
    BitWriter writer;
    for (const auto value : {0u, 2u, 0u}) {
        writer.ue(value);
    }
    const bool non_idr_reference = idr_pic_id == 2;
    if (non_idr_reference) {
        writer.bits(1, 4);
        writer.bits(0, 1);
    } else {
        writer.bits(0, 4);
        writer.ue(idr_pic_id);
        writer.bits(0, 2);
    }
    writer.ue(0);
    writer.ue(1);
    for (int macroblock = 0; macroblock < 16; ++macroblock) {
        writer.ue(25);
        writer.align();
        if (emulation_prevention_test && macroblock == 0) {
            writer.data.insert(writer.data.end(), {0, 0, 1});
            writer.data.insert(writer.data.end(), 253, 128);
        } else {
            writer.data.insert(writer.data.end(), 256, 128);
        }
        writer.data.insert(writer.data.end(), 128, 128);
        writer.bit_count += 384 * 8;
    }
    const auto nal = writer.nal(non_idr_reference ? 0x21 : 0x65);
    const auto size = static_cast<std::uint32_t>(nal.size());
    std::vector<std::uint8_t> sample;
    for (int shift = 24; shift >= 0; shift -= 8) {
        sample.push_back(static_cast<std::uint8_t>(size >> shift));
    }
    sample.insert(sample.end(), nal.begin(), nal.end());
    return sample;
}

} // namespace pygorvid
