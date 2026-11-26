#include "h264_utils.hpp"

#include <algorithm>
#include <array>
#include <stdexcept>

namespace pygorvid {
namespace h264_utils {
namespace {

constexpr std::array<std::size_t, 16> zigzag4x4{{
    0, 1, 4, 8, 5, 2, 3, 6, 9, 12, 13, 10, 7, 11, 14, 15,
}};

constexpr std::array<std::array<std::int64_t, 3>, 6> scale4x4{{
    {{10, 13, 16}}, {{11, 14, 18}}, {{13, 16, 20}},
    {{14, 18, 23}}, {{16, 20, 25}}, {{18, 23, 29}},
}};

constexpr std::array<std::array<std::int64_t, 6>, 6> scale8x8{{
    {{20, 18, 32, 19, 25, 24}},
    {{22, 19, 35, 21, 28, 26}},
    {{26, 23, 42, 24, 33, 31}},
    {{28, 25, 45, 26, 35, 33}},
    {{32, 28, 51, 30, 40, 38}},
    {{36, 32, 58, 34, 46, 43}},
}};

constexpr std::array<std::size_t, 64> scale8x8_classes{{
    0, 3, 4, 3, 0, 3, 4, 3, 3, 1, 5, 1, 3, 1, 5, 1,
    4, 5, 2, 5, 4, 5, 2, 5, 3, 1, 5, 1, 3, 1, 5, 1,
    0, 3, 4, 3, 0, 3, 4, 3, 3, 1, 5, 1, 3, 1, 5, 1,
    4, 5, 2, 5, 4, 5, 2, 5, 3, 1, 5, 1, 3, 1, 5, 1,
}};

std::array<std::int64_t, 4> hadamard4(
    const std::array<std::int64_t, 4>& values) {
    return {{
        values[0] + values[1] + values[2] + values[3],
        values[0] + values[1] - values[2] - values[3],
        values[0] - values[1] - values[2] + values[3],
        values[0] - values[1] + values[2] - values[3],
    }};
}

Residual4x4 inverse_scale_intra4x4(const CavlcResidualBlock& block,
                                   std::int32_t qp, bool ac_only,
                                   std::int64_t dc_coefficient) {
    const auto expected_coefficients = ac_only ? 15U : 16U;
    if (block.coefficients.size() != expected_coefficients ||
        qp < 0 || qp > 51) {
        throw std::runtime_error(
            ac_only ? "invalid I16x16 AC reconstruction input"
                    : "invalid I4x4 residual reconstruction input");
    }

    Residual4x4 coefficients{};
    std::size_t scan_index = 0;
    if (ac_only) {
        coefficients[0] = dc_coefficient;
        scan_index = 1;
    }
    for (; scan_index < zigzag4x4.size(); ++scan_index) {
        const auto natural_index = zigzag4x4[scan_index];
        const auto row = natural_index / 4;
        const auto column = natural_index % 4;
        const auto factor_class = row % 2 + column % 2;
        const auto coefficient_index = scan_index - (ac_only ? 1U : 0U);
        const auto value =
            static_cast<std::int64_t>(block.coefficients[coefficient_index]) *
            scale4x4[static_cast<std::size_t>(qp % 6)][factor_class] * 16;
        coefficients[natural_index] =
            qp >= 24
                ? value * (std::int64_t{1} << (qp / 6 - 4))
                : floor_shift(value + (std::int64_t{1} << (3 - qp / 6)),
                              static_cast<unsigned>(4 - qp / 6));
    }
    return coefficients;
}

Residual4x4 inverse_transform_intra4x4(const Residual4x4& coefficients) {
    const auto transform_line =
        [](const std::array<std::int64_t, 4>& values) {
            const auto s0 = values[0] + values[2];
            const auto s1 = values[0] - values[2];
            const auto s2 = floor_shift(values[1], 1) - values[3];
            const auto s3 = values[1] + floor_shift(values[3], 1);
            return std::array<std::int64_t, 4>{{
                s0 + s3, s1 + s2, s1 - s2, s0 - s3,
            }};
        };

    Residual4x4 horizontal{};
    for (std::size_t row = 0; row < 4; ++row) {
        const auto transformed = transform_line({{
            coefficients[row * 4], coefficients[row * 4 + 1],
            coefficients[row * 4 + 2], coefficients[row * 4 + 3],
        }});
        for (std::size_t column = 0; column < 4; ++column) {
            horizontal[row * 4 + column] = transformed[column];
        }
    }

    Residual4x4 residual{};
    for (std::size_t column = 0; column < 4; ++column) {
        const auto transformed = transform_line({{
            horizontal[column], horizontal[4 + column],
            horizontal[8 + column], horizontal[12 + column],
        }});
        for (std::size_t row = 0; row < 4; ++row) {
            residual[row * 4 + column] =
                floor_shift(transformed[row] + 32, 6);
        }
    }
    return residual;
}

std::array<std::int64_t, 8> inverse_transform_luma8x8_line(
    const std::array<std::int64_t, 8>& coefficients) {
    const auto a0 = coefficients[0] + coefficients[4];
    const auto a2 = coefficients[0] - coefficients[4];
    const auto a4 = (coefficients[2] >> 1) - coefficients[6];
    const auto a6 = coefficients[2] + (coefficients[6] >> 1);
    const auto b0 = a0 + a6;
    const auto b2 = a2 + a4;
    const auto b4 = a2 - a4;
    const auto b6 = a0 - a6;
    const auto a1 = -coefficients[3] + coefficients[5] - coefficients[7] -
                    (coefficients[7] >> 1);
    const auto a3 = coefficients[1] + coefficients[7] - coefficients[3] -
                    (coefficients[3] >> 1);
    const auto a5 = -coefficients[1] + coefficients[7] + coefficients[5] +
                    (coefficients[5] >> 1);
    const auto a7 = coefficients[3] + coefficients[5] + coefficients[1] +
                    (coefficients[1] >> 1);
    const auto b1 = a1 + (a7 >> 2);
    const auto b3 = a3 + (a5 >> 2);
    const auto b5 = (a3 >> 2) - a5;
    const auto b7 = a7 - (a1 >> 2);
    return {{b0 + b7, b2 + b5, b4 + b3, b6 + b1,
             b6 - b1, b4 - b3, b2 - b5, b0 - b7}};
}

} // namespace

std::int64_t floor_shift(std::int64_t value, unsigned shift) {
    const auto divisor = std::int64_t{1} << shift;
    auto quotient = value / divisor;
    if (value < 0 && value % divisor != 0) {
        --quotient;
    }
    return quotient;
}

BlockPosition block_position_2x2(std::uint32_t block_index) {
    return {block_index % 2, block_index / 2};
}

void add_residual_block(std::vector<std::uint8_t>& samples,
                        std::uint32_t stride, std::uint32_t origin_x,
                        std::uint32_t origin_y,
                        const std::vector<std::int64_t>& residual,
                        std::uint32_t block_size) {
    if (residual.size() !=
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
                                 std::int32_t qp) {
    return inverse_transform_intra4x4(
        inverse_scale_intra4x4(block, qp, false, 0));
}

Residual4x4 reconstruct_intra4x4_ac(const CavlcResidualBlock& block,
                                     std::int32_t qp,
                                     std::int64_t dc_coefficient) {
    return inverse_transform_intra4x4(
        inverse_scale_intra4x4(block, qp, true, dc_coefficient));
}

Residual4x4 hadamard4x4(const Residual4x4& levels) {
    Residual4x4 horizontal{};
    for (std::size_t row = 0; row < 4; ++row) {
        const auto transformed = hadamard4({{
            levels[row * 4], levels[row * 4 + 1],
            levels[row * 4 + 2], levels[row * 4 + 3],
        }});
        std::copy(transformed.begin(), transformed.end(),
                  horizontal.begin() + static_cast<std::ptrdiff_t>(row * 4));
    }
    Residual4x4 transformed{};
    for (std::size_t column = 0; column < 4; ++column) {
        const auto line = hadamard4({{
            horizontal[column], horizontal[4 + column],
            horizontal[8 + column], horizontal[12 + column],
        }});
        for (std::size_t row = 0; row < 4; ++row) {
            transformed[row * 4 + column] = line[row];
        }
    }
    return transformed;
}

Residual8x8 inverse_scale_luma8x8(
    const std::array<std::int32_t, 64>& levels, std::int32_t qp) {
    if (qp < 0 || qp > 51) {
        throw std::runtime_error("8x8 inverse scaling QPY is outside [0,51]");
    }
    Residual8x8 scaled{};
    for (std::size_t index = 0; index < scaled.size(); ++index) {
        const auto value =
            static_cast<std::int64_t>(levels[index]) *
            scale8x8[static_cast<std::size_t>(qp % 6)]
                    [scale8x8_classes[index]] *
            16;
        scaled[index] =
            qp >= 36
                ? value * (std::int64_t{1} << (qp / 6 - 6))
                : floor_shift(value + (std::int64_t{1} << (5 - qp / 6)),
                              static_cast<unsigned>(6 - qp / 6));
    }
    return scaled;
}

Residual8x8 inverse_transform_luma8x8(const Residual8x8& coefficients) {
    Residual8x8 horizontal{};
    for (std::size_t row = 0; row < 8; ++row) {
        std::array<std::int64_t, 8> line{};
        std::copy_n(coefficients.begin() +
                        static_cast<std::ptrdiff_t>(row * 8),
                    8, line.begin());
        const auto transformed = inverse_transform_luma8x8_line(line);
        std::copy(transformed.begin(), transformed.end(),
                  horizontal.begin() + static_cast<std::ptrdiff_t>(row * 8));
    }
    Residual8x8 residual{};
    for (std::size_t column = 0; column < 8; ++column) {
        std::array<std::int64_t, 8> line{};
        for (std::size_t row = 0; row < 8; ++row) {
            line[row] = horizontal[row * 8 + column];
        }
        const auto transformed = inverse_transform_luma8x8_line(line);
        for (std::size_t row = 0; row < 8; ++row) {
            residual[row * 8 + column] =
                floor_shift(transformed[row] + 32, 6);
        }
    }
    return residual;
}

Residual8x8 reconstruct_luma8x8(
    const std::array<std::int32_t, 64>& levels, std::int32_t qp) {
    return inverse_transform_luma8x8(inverse_scale_luma8x8(levels, qp));
}

Residual8x8 reconstruct_chroma_component(
    const std::array<CavlcResidualBlock, 4>& ac_blocks,
    const std::array<std::int64_t, 4>& dc_samples, std::int32_t qp) {
    Residual8x8 residual{};
    for (std::size_t block_index = 0; block_index < ac_blocks.size();
         ++block_index) {
        const auto block =
            reconstruct_intra4x4_ac(ac_blocks[block_index], qp,
                                    dc_samples[block_index]);
        const auto position = block_position_2x2(
            static_cast<std::uint32_t>(block_index));
        for (std::uint32_t row = 0; row < 4; ++row) {
            for (std::uint32_t column = 0; column < 4; ++column) {
                residual[(position.y * 4 + row) * 8 + position.x * 4 + column] =
                    block[row * 4 + column];
            }
        }
    }
    return residual;
}

void validate_luma_scaling(bool has_coefficients, bool transform_8x8,
                           bool seq_4x4_matrix_present,
                           bool seq_8x8_matrix_present,
                           bool pic_matrix_present,
                           const char* message) {
    const auto seq_matrix_present =
        transform_8x8 ? seq_8x8_matrix_present : seq_4x4_matrix_present;
    if (has_coefficients &&
        (seq_matrix_present || pic_matrix_present)) {
        throw std::runtime_error(message);
    }
}

void validate_chroma_scaling(bool has_coefficients, bool seq_matrix_present,
                             bool pic_matrix_present, const char* message) {
    if (has_coefficients && (seq_matrix_present || pic_matrix_present)) {
        throw std::runtime_error(message);
    }
}

} // namespace h264_utils
} // namespace pygorvid
