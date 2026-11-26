"""Shared H.264 4x4 residual reconstruction utilities."""

from collections.abc import Sequence

from .h264_cabac import place_chroma4x4_scan_levels


class InverseScaleError(ValueError):
    """Raised when inverse-scaling inputs are invalid."""


class InverseTransformError(ValueError):
    """Raised when inverse-transform inputs are invalid."""


INVERSE_SCALE_4X4_FACTORS = (
    (10, 13, 16),
    (11, 14, 18),
    (13, 16, 20),
    (14, 18, 23),
    (16, 20, 25),
    (18, 23, 29),
)

INVERSE_SCALE_8X8_FACTORS = (
    (20, 18, 32, 19, 25, 24),
    (22, 19, 35, 21, 28, 26),
    (26, 23, 42, 24, 33, 31),
    (28, 25, 45, 26, 35, 33),
    (32, 28, 51, 30, 40, 38),
    (36, 32, 58, 34, 46, 43),
)

INVERSE_SCALE_8X8_CLASSES = (
    0, 3, 4, 3, 0, 3, 4, 3, 3, 1, 5, 1, 3, 1, 5, 1,
    4, 5, 2, 5, 4, 5, 2, 5, 3, 1, 5, 1, 3, 1, 5, 1,
    0, 3, 4, 3, 0, 3, 4, 3, 3, 1, 5, 1, 3, 1, 5, 1,
    4, 5, 2, 5, 4, 5, 2, 5, 3, 1, 5, 1, 3, 1, 5, 1,
)


def inverse_scale_luma4x4(
    levels: Sequence[int], scaling_list: Sequence[int], qpy: int
) -> list[int]:
    """Apply H.264 4x4 luma inverse scaling to raster-order coefficient levels."""
    if not isinstance(qpy, int) or isinstance(qpy, bool) or not 0 <= qpy <= 51:
        raise InverseScaleError("inverse scaling QPY is outside [0,51]")
    if len(levels) != 16 or any(
        not isinstance(level, int)
        or isinstance(level, bool)
        or not -(1 << 31) <= level < (1 << 31)
        for level in levels
    ):
        raise InverseScaleError(
            "inverse scaling levels must contain 16 signed 32-bit integers"
        )
    if len(scaling_list) != 16 or any(
        not isinstance(weight, int)
        or isinstance(weight, bool)
        or not 1 <= weight <= 255
        for weight in scaling_list
    ):
        raise InverseScaleError(
            "inverse scaling list must contain 16 values in [1,255]"
        )

    scaled = []
    for index, (level, weight) in enumerate(zip(levels, scaling_list)):
        row, column = divmod(index, 4)
        scaled.append(
            inverse_scale_4x4_coefficient(
                level, weight, qpy, row % 2 + column % 2
            )
        )
    return scaled


def inverse_scale_chroma4x4(
    levels: Sequence[int], scaling_list: Sequence[int], qpc: int
) -> list[int]:
    """Preserve scaled chroma DC and inverse-scale 4x4 chroma AC levels."""
    if not isinstance(qpc, int) or isinstance(qpc, bool) or not 0 <= qpc <= 39:
        raise InverseScaleError("inverse scaling QPC is outside [0,39]")
    if len(levels) != 16 or any(
        not isinstance(level, int)
        or isinstance(level, bool)
        or not -(1 << 15) <= level < (1 << 15)
        for level in levels
    ):
        raise InverseScaleError(
            "chroma block levels must contain 16 signed 16-bit integers"
        )
    if len(scaling_list) != 16 or any(
        not isinstance(weight, int)
        or isinstance(weight, bool)
        or not 1 <= weight <= 255
        for weight in scaling_list
    ):
        raise InverseScaleError(
            "inverse scaling list must contain 16 values in [1,255]"
        )

    scaled = [levels[0]] + [0] * 15
    for index in range(1, 16):
        row, column = divmod(index, 4)
        scaled[index] = inverse_scale_4x4_coefficient(
            levels[index], scaling_list[index], qpc, row % 2 + column % 2
        )
        if not -(1 << 15) <= scaled[index] < (1 << 15):
            raise InverseScaleError(
                "scaled chroma block value is outside the signed 16-bit range"
            )
    return scaled


def reconstruct_chroma4x4_residual(
    dc_c: int,
    ac_scan_levels: Sequence[int],
    scaling_list: Sequence[int],
    qpc: int,
) -> list[int]:
    """Assemble, scale, and inverse-transform one 4x4 chroma residual block."""
    if not isinstance(qpc, int) or isinstance(qpc, bool) or not 0 <= qpc <= 39:
        raise InverseScaleError("inverse scaling QPC is outside [0,39]")
    if (
        not isinstance(dc_c, int)
        or isinstance(dc_c, bool)
        or not -(1 << 15) <= dc_c < (1 << 15)
    ):
        raise InverseScaleError("chroma block DC must be a signed 16-bit integer")

    levels = place_chroma4x4_scan_levels(dc_c, list(ac_scan_levels))
    return inverse_transform_luma4x4(
        inverse_scale_chroma4x4(levels, scaling_list, qpc)
    )


def inverse_transform_luma4x4(coefficients: Sequence[int]) -> list[int]:
    """Transform dequantized raster-order coefficients into residual samples."""
    if len(coefficients) != 16 or any(
        not isinstance(coefficient, int) or isinstance(coefficient, bool)
        for coefficient in coefficients
    ):
        raise InverseTransformError(
            "inverse transform requires 16 integer coefficients"
        )

    horizontal = [0] * 16
    for row in range(4):
        offset = row * 4
        horizontal[offset : offset + 4] = inverse_transform_4x4_line(
            coefficients[offset : offset + 4]
        )
    residual = [0] * 16
    for column in range(4):
        transformed = inverse_transform_4x4_line(
            [horizontal[row * 4 + column] for row in range(4)]
        )
        for row, value in enumerate(transformed):
            residual[row * 4 + column] = (value + 32) >> 6
    return residual


def reconstruct_luma4x4_residual(
    levels: Sequence[int], scaling_list: Sequence[int], qpy: int
) -> list[int]:
    """Apply luma 4x4 inverse scaling and inverse transform to raster-order levels."""
    return inverse_transform_luma4x4(
        inverse_scale_luma4x4(levels, scaling_list, qpy)
    )


def reconstruct_luma4x4_ac_residual(
    ac_raster_levels: Sequence[int],
    dc_coefficient: int,
    scaling_list: Sequence[int],
    qpy: int,
) -> list[int]:
    """Scale AC levels, insert pre-scaled DC, and inverse-transform one 4x4 block."""
    if len(ac_raster_levels) != 16:
        raise InverseScaleError(
            "luma 4x4 AC levels must contain 16 raster coefficients"
        )
    coefficients = inverse_scale_luma4x4(ac_raster_levels, scaling_list, qpy)
    coefficients[0] = dc_coefficient
    return inverse_transform_luma4x4(coefficients)


def hadamard4x4(values: Sequence[int]) -> list[int]:
    """Apply the separable 4x4 Hadamard transform."""
    horizontal = [0] * 16
    for row in range(4):
        horizontal[row * 4 : row * 4 + 4] = hadamard4(
            values[row * 4 : row * 4 + 4]
        )
    transformed = [0] * 16
    for column in range(4):
        column_values = hadamard4(
            [horizontal[row * 4 + column] for row in range(4)]
        )
        for row, value in enumerate(column_values):
            transformed[row * 4 + column] = value
    return transformed


def inverse_scale_luma8x8(
    levels: Sequence[int], scaling_list: Sequence[int], qpy: int
) -> list[int]:
    """Apply H.264 8x8 luma inverse scaling to raster-order coefficient levels."""
    if not isinstance(qpy, int) or isinstance(qpy, bool) or not 0 <= qpy <= 51:
        raise InverseScaleError("inverse scaling QPY is outside [0,51]")
    if len(levels) != 64 or any(
        not isinstance(level, int)
        or isinstance(level, bool)
        or not -(1 << 31) <= level < (1 << 31)
        for level in levels
    ):
        raise InverseScaleError(
            "inverse scaling levels must contain 64 signed 32-bit integers"
        )
    if len(scaling_list) != 64 or any(
        not isinstance(weight, int)
        or isinstance(weight, bool)
        or not 1 <= weight <= 255
        for weight in scaling_list
    ):
        raise InverseScaleError(
            "inverse scaling list must contain 64 values in [1,255]"
        )

    scaled = []
    for index, (level, weight) in enumerate(zip(levels, scaling_list)):
        value = (
            level
            * INVERSE_SCALE_8X8_FACTORS[qpy % 6][
                INVERSE_SCALE_8X8_CLASSES[index]
            ]
            * weight
        )
        if qpy >= 36:
            scaled.append(value << (qpy // 6 - 6))
        else:
            shift = 6 - qpy // 6
            scaled.append((value + (1 << (shift - 1))) >> shift)
    return scaled


def inverse_transform_luma8x8(coefficients: Sequence[int]) -> list[int]:
    """Transform 8x8 dequantized raster-order coefficients into residual samples."""
    if len(coefficients) != 64 or any(
        not isinstance(coefficient, int) or isinstance(coefficient, bool)
        for coefficient in coefficients
    ):
        raise InverseTransformError(
            "inverse transform requires 64 integer coefficients"
        )
    horizontal = [0] * 64
    for row in range(8):
        offset = row * 8
        horizontal[offset : offset + 8] = inverse_transform_8x8_line(
            coefficients[offset : offset + 8]
        )
    residual = [0] * 64
    for column in range(8):
        transformed = inverse_transform_8x8_line(
            [horizontal[row * 8 + column] for row in range(8)]
        )
        for row, value in enumerate(transformed):
            residual[row * 8 + column] = (value + 32) >> 6
    return residual


def reconstruct_luma8x8_residual(
    levels: Sequence[int], scaling_list: Sequence[int], qpy: int
) -> list[int]:
    """Apply 8x8 inverse scaling and inverse transform."""
    return inverse_transform_luma8x8(
        inverse_scale_luma8x8(levels, scaling_list, qpy)
    )


def block_position_2x2(block_index: int) -> tuple[int, int]:
    """Return the x/y position of one raster block in a 2x2 grid."""
    return block_index % 2, block_index // 2


def place_residual_block(
    destination: list[int],
    stride: int,
    origin_x: int,
    origin_y: int,
    residual: Sequence[int],
    block_size: int,
) -> None:
    """Place one residual block into a residual plane without clipping."""
    if len(residual) != block_size * block_size:
        raise InverseTransformError("residual block dimensions are invalid")
    for row in range(block_size):
        start = (origin_y + row) * stride + origin_x
        destination[start : start + block_size] = residual[
            row * block_size : (row + 1) * block_size
        ]


def reconstruct_chroma_component_residual(
    dc_samples: Sequence[int],
    ac_scan_blocks: Sequence[Sequence[int]],
    scaling_list: Sequence[int],
    qpc: int,
) -> list[int]:
    """Reconstruct and assemble all four 4x4 blocks of one chroma component."""
    if len(dc_samples) != 4 or len(ac_scan_blocks) != 4:
        raise InverseTransformError(
            "chroma component requires four DC values and four AC blocks"
        )
    blocks = [
        reconstruct_chroma4x4_residual(
            dc_samples[index],
            ac_scan_blocks[index],
            scaling_list,
            qpc,
        )
        for index in range(4)
    ]
    macroblock = [0] * 64
    for block_index, block in enumerate(blocks):
        block_x, block_y = block_position_2x2(block_index)
        place_residual_block(
            macroblock,
            8,
            block_x * 4,
            block_y * 4,
            block,
            4,
        )
    return macroblock


def inverse_scale_4x4_coefficient(
    level: int, weight: int, qp: int, factor_class: int
) -> int:
    value = level * INVERSE_SCALE_4X4_FACTORS[qp % 6][factor_class] * weight
    if qp >= 24:
        return value << (qp // 6 - 4)
    shift = 4 - qp // 6
    return (value + (1 << (shift - 1))) >> shift


def inverse_transform_4x4_line(coefficients: Sequence[int]) -> list[int]:
    even_sum = coefficients[0] + coefficients[2]
    even_difference = coefficients[0] - coefficients[2]
    odd_difference = (coefficients[1] >> 1) - coefficients[3]
    odd_sum = coefficients[1] + (coefficients[3] >> 1)
    return [
        even_sum + odd_sum,
        even_difference + odd_difference,
        even_difference - odd_difference,
        even_sum - odd_sum,
    ]


def hadamard4(values: Sequence[int]) -> list[int]:
    a, b, c, d = values
    return [
        a + b + c + d,
        a + b - c - d,
        a - b - c + d,
        a - b + c - d,
    ]


def inverse_transform_8x8_line(coefficients: Sequence[int]) -> list[int]:
    a0 = coefficients[0] + coefficients[4]
    a2 = coefficients[0] - coefficients[4]
    a4 = (coefficients[2] >> 1) - coefficients[6]
    a6 = coefficients[2] + (coefficients[6] >> 1)
    b0 = a0 + a6
    b2 = a2 + a4
    b4 = a2 - a4
    b6 = a0 - a6
    a1 = -coefficients[3] + coefficients[5] - coefficients[7] - (
        coefficients[7] >> 1
    )
    a3 = coefficients[1] + coefficients[7] - coefficients[3] - (
        coefficients[3] >> 1
    )
    a5 = -coefficients[1] + coefficients[7] + coefficients[5] + (
        coefficients[5] >> 1
    )
    a7 = coefficients[3] + coefficients[5] + coefficients[1] + (
        coefficients[1] >> 1
    )
    b1 = a1 + (a7 >> 2)
    b3 = a3 + (a5 >> 2)
    b5 = (a3 >> 2) - a5
    b7 = a7 - (a1 >> 2)
    return [
        b0 + b7,
        b2 + b5,
        b4 + b3,
        b6 + b1,
        b6 - b1,
        b4 - b3,
        b2 - b5,
        b0 - b7,
    ]
