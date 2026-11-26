#ifndef PYGORVID_H264_UTILS_HPP
#define PYGORVID_H264_UTILS_HPP

#include "pygorvid/cavlc.hpp"

#include <algorithm>
#include <array>
#include <cstdint>
#include <stdexcept>
#include <vector>

namespace pygorvid {
namespace h264_utils {

using Residual4x4 = std::array<std::int64_t, 16>;
using Residual8x8 = std::array<std::int64_t, 64>;

struct BlockPosition {
    std::uint32_t x;
    std::uint32_t y;
};

std::int64_t floor_shift(std::int64_t value, unsigned shift);

BlockPosition block_position_2x2(std::uint32_t block_index);

void add_residual_block(std::vector<std::uint8_t>& samples,
                        std::uint32_t stride, std::uint32_t origin_x,
                        std::uint32_t origin_y,
                        const std::vector<std::int64_t>& residual,
                        std::uint32_t block_size);

template <std::size_t SampleCount>
void add_residual_block(std::vector<std::uint8_t>& samples,
                        std::uint32_t stride, std::uint32_t origin_x,
                        std::uint32_t origin_y,
                        const std::array<std::int64_t, SampleCount>& residual,
                        std::uint32_t block_size) {
    if (SampleCount !=
        static_cast<std::size_t>(block_size) * block_size) {
        throw std::invalid_argument("residual block dimensions are invalid");
    }
    for (std::uint32_t row = 0; row < block_size; ++row) {
        for (std::uint32_t column = 0; column < block_size; ++column) {
            auto& sample =
                samples[static_cast<std::size_t>(origin_y + row) * stride +
                        origin_x + column];
            sample = static_cast<std::uint8_t>(std::clamp<std::int64_t>(
                static_cast<std::int64_t>(sample) +
                    residual[static_cast<std::size_t>(row) * block_size +
                             column],
                0, 255));
        }
    }
}

Residual4x4 reconstruct_intra4x4(const CavlcResidualBlock& block,
                                std::int32_t qp);

Residual4x4 reconstruct_intra4x4_ac(const CavlcResidualBlock& block,
                                    std::int32_t qp,
                                    std::int64_t dc_coefficient);

Residual4x4 hadamard4x4(const Residual4x4& levels);

Residual8x8 inverse_scale_luma8x8(
    const std::array<std::int32_t, 64>& levels, std::int32_t qp);

Residual8x8 inverse_transform_luma8x8(const Residual8x8& coefficients);

Residual8x8 reconstruct_luma8x8(
    const std::array<std::int32_t, 64>& levels, std::int32_t qp);

Residual8x8 reconstruct_chroma_component(
    const std::array<CavlcResidualBlock, 4>& ac_blocks,
    const std::array<std::int64_t, 4>& dc_samples, std::int32_t qp);

void validate_luma_scaling(bool has_coefficients, bool transform_8x8,
                           bool seq_4x4_matrix_present,
                           bool seq_8x8_matrix_present,
                           bool pic_matrix_present,
                           const char* message);

void validate_chroma_scaling(bool has_coefficients, bool seq_matrix_present,
                             bool pic_matrix_present, const char* message);

} // namespace h264_utils
} // namespace pygorvid

#endif
