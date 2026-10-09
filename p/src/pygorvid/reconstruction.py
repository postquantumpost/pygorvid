"""H.264 coefficient reconstruction primitives."""

from collections.abc import Sequence
from typing import Optional


class InverseScaleError(ValueError):
    """Raised when inverse-scaling inputs are invalid."""


class InverseTransformError(ValueError):
    """Raised when inverse-transform inputs are invalid."""


class IntraPredictionError(ValueError):
    """Raised when intra-prediction reference samples are invalid."""


_INVERSE_SCALE_4X4_FACTORS = (
    (10, 13, 16),
    (11, 14, 18),
    (13, 16, 20),
    (14, 18, 23),
    (16, 20, 25),
    (18, 23, 29),
)
_INVERSE_SCALE_8X8_FACTORS = (
    (20, 18, 32, 19, 25, 24),
    (22, 19, 35, 21, 28, 26),
    (26, 23, 42, 24, 33, 31),
    (28, 25, 45, 26, 35, 33),
    (32, 28, 51, 30, 40, 38),
    (36, 32, 58, 34, 46, 43),
)
_INVERSE_SCALE_8X8_CLASSES = (
    0, 3, 4, 3, 0, 3, 4, 3,
    3, 1, 5, 1, 3, 1, 5, 1,
    4, 5, 2, 5, 4, 5, 2, 5,
    3, 1, 5, 1, 3, 1, 5, 1,
    0, 3, 4, 3, 0, 3, 4, 3,
    3, 1, 5, 1, 3, 1, 5, 1,
    4, 5, 2, 5, 4, 5, 2, 5,
    3, 1, 5, 1, 3, 1, 5, 1,
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
        raise InverseScaleError("inverse scaling levels must contain 16 signed 32-bit integers")
    if len(scaling_list) != 16 or any(
        not isinstance(weight, int)
        or isinstance(weight, bool)
        or not 1 <= weight <= 255
        for weight in scaling_list
    ):
        raise InverseScaleError("inverse scaling list must contain 16 values in [1,255]")

    scaled = []
    for index, (level, weight) in enumerate(zip(levels, scaling_list)):
        row, column = divmod(index, 4)
        factor_class = row % 2 + column % 2
        value = level * _INVERSE_SCALE_4X4_FACTORS[qpy % 6][factor_class] * weight
        if qpy >= 24:
            scaled.append(value << (qpy // 6 - 4))
        else:
            shift = 4 - qpy // 6
            rounding = 1 << (shift - 1)
            scaled.append((value + rounding) >> shift)
    return scaled


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
        raise InverseScaleError("inverse scaling levels must contain 64 signed 32-bit integers")
    if len(scaling_list) != 64 or any(
        not isinstance(weight, int)
        or isinstance(weight, bool)
        or not 1 <= weight <= 255
        for weight in scaling_list
    ):
        raise InverseScaleError("inverse scaling list must contain 64 values in [1,255]")

    scaled = []
    for index, (level, weight) in enumerate(zip(levels, scaling_list)):
        factor_class = _INVERSE_SCALE_8X8_CLASSES[index]
        value = level * _INVERSE_SCALE_8X8_FACTORS[qpy % 6][factor_class] * weight
        if qpy >= 24:
            scaled.append(value << (qpy // 6 - 4))
        else:
            shift = 4 - qpy // 6
            rounding = 1 << (shift - 1)
            scaled.append((value + rounding) >> shift)
    return scaled


def predict_luma_intra8x8_vertical(top: Sequence[int]) -> list[int]:
    """Repeat eight filtered top samples across an 8x8 luma block."""
    if len(top) != 8 or any(
        not isinstance(sample, int)
        or isinstance(sample, bool)
        or not 0 <= sample <= 255
        for sample in top
    ):
        raise IntraPredictionError(
            "vertical 8x8 prediction requires eight 8-bit filtered top samples"
        )
    return list(top) * 8


def predict_luma_intra8x8_horizontal(left: Sequence[int]) -> list[int]:
    """Repeat each filtered left sample across one row of an 8x8 luma block."""
    if len(left) != 8 or any(
        not isinstance(sample, int)
        or isinstance(sample, bool)
        or not 0 <= sample <= 255
        for sample in left
    ):
        raise IntraPredictionError(
            "horizontal 8x8 prediction requires eight 8-bit filtered left samples"
        )
    return [sample for sample in left for _ in range(8)]


def predict_luma_intra8x8_dc(
    top: Optional[Sequence[int]] = None, left: Optional[Sequence[int]] = None
) -> list[int]:
    """Predict an 8x8 luma block from whichever filtered edges are available."""
    for name, references in (("top", top), ("left", left)):
        if references is not None and (
            len(references) != 8
            or any(
                not isinstance(sample, int)
                or isinstance(sample, bool)
                or not 0 <= sample <= 255
                for sample in references
            )
        ):
            raise IntraPredictionError(
                f"DC 8x8 prediction requires eight 8-bit filtered {name} samples"
            )

    if top is not None and left is not None:
        dc_value = (sum(top) + sum(left) + 8) >> 4
    elif top is not None:
        dc_value = (sum(top) + 4) >> 3
    elif left is not None:
        dc_value = (sum(left) + 4) >> 3
    else:
        dc_value = 128
    return [dc_value] * 64


def predict_luma_intra8x8_diagonal_down_left(top: Sequence[int]) -> list[int]:
    """Interpolate an 8x8 luma block from 16 filtered top references."""
    if len(top) != 16 or any(
        not isinstance(sample, int)
        or isinstance(sample, bool)
        or not 0 <= sample <= 255
        for sample in top
    ):
        raise IntraPredictionError(
            "diagonal-down-left 8x8 prediction requires sixteen 8-bit filtered top samples"
        )

    prediction = [0] * 64
    for row in range(8):
        for column in range(8):
            index = row + column
            last_index = min(index + 2, 15)
            prediction[row * 8 + column] = (
                top[index] + 2 * top[index + 1] + top[last_index] + 2
            ) >> 2
    return prediction


def predict_luma_intra8x8_diagonal_down_right(
    top: Sequence[int], left: Sequence[int], top_left: int
) -> list[int]:
    """Interpolate an 8x8 luma block from filtered top, left, and top-left references."""
    for name, references, expected_length in (("top", top, 16), ("left", left, 8)):
        if len(references) != expected_length or any(
            not isinstance(sample, int)
            or isinstance(sample, bool)
            or not 0 <= sample <= 255
            for sample in references
        ):
            sample_count = "sixteen" if expected_length == 16 else "eight"
            raise IntraPredictionError(
                f"diagonal-down-right 8x8 prediction requires {sample_count} 8-bit filtered {name} samples"
            )
    if (
        not isinstance(top_left, int)
        or isinstance(top_left, bool)
        or not 0 <= top_left <= 255
    ):
        raise IntraPredictionError(
            "diagonal-down-right 8x8 prediction requires an 8-bit filtered top-left sample"
        )

    def reference_at(position: int) -> int:
        if position == -1:
            return top_left
        if position < -1:
            return left[-position - 2]
        return top[position]

    prediction = [0] * 64
    for row in range(8):
        for column in range(8):
            position = column - row
            prediction[row * 8 + column] = (
                reference_at(position - 1)
                + 2 * reference_at(position)
                + reference_at(position + 1)
                + 2
            ) >> 2
    return prediction


def predict_luma_intra8x8_vertical_right(
    top: Sequence[int], left: Sequence[int], top_left: int
) -> list[int]:
    """Interpolate an 8x8 luma block across vertical-right reference phases."""
    for name, references, expected_length in (("top", top, 16), ("left", left, 8)):
        if len(references) != expected_length or any(
            not isinstance(sample, int)
            or isinstance(sample, bool)
            or not 0 <= sample <= 255
            for sample in references
        ):
            sample_count = "sixteen" if expected_length == 16 else "eight"
            raise IntraPredictionError(
                f"vertical-right 8x8 prediction requires {sample_count} 8-bit filtered {name} samples"
            )
    if (
        not isinstance(top_left, int)
        or isinstance(top_left, bool)
        or not 0 <= top_left <= 255
    ):
        raise IntraPredictionError(
            "vertical-right 8x8 prediction requires an 8-bit filtered top-left sample"
        )

    def top_reference_at(position: int) -> int:
        return top_left if position == -1 else top[position]

    prediction = [0] * 64
    for row in range(8):
        for column in range(8):
            phase = 2 * column - row
            if phase >= 0 and phase % 2 == 0:
                center = phase // 2
                value = (
                    top_reference_at(center - 1)
                    + 2 * top_reference_at(center)
                    + top_reference_at(center + 1)
                    + 2
                ) >> 2
            elif phase >= 0:
                center = (phase - 1) // 2
                value = (
                    top_reference_at(center - 1) + top_reference_at(center) + 1
                ) >> 1
            elif phase == -1:
                value = (left[0] + top_left + 1) >> 1
            elif phase == -2:
                value = (left[0] + 2 * top_left + top[0] + 2) >> 2
            elif -phase % 2 == 1:
                center = (-phase - 3) // 2
                value = (left[center] + left[center + 1] + 1) >> 1
            else:
                center = (-phase - 4) // 2
                value = (
                    left[center] + 2 * left[center + 1] + left[center + 2] + 2
                ) >> 2
            prediction[row * 8 + column] = value
    return prediction


def predict_luma_intra8x8_horizontal_down(
    top: Sequence[int], left: Sequence[int], top_left: int
) -> list[int]:
    """Predict an 8x8 luma block by transposing vertical-right prediction."""
    for name, references, expected_length in (("top", top, 8), ("left", left, 16)):
        if len(references) != expected_length or any(
            not isinstance(sample, int)
            or isinstance(sample, bool)
            or not 0 <= sample <= 255
            for sample in references
        ):
            sample_count = "eight" if expected_length == 8 else "sixteen"
            raise IntraPredictionError(
                f"horizontal-down 8x8 prediction requires {sample_count} 8-bit filtered {name} samples"
            )
    if (
        not isinstance(top_left, int)
        or isinstance(top_left, bool)
        or not 0 <= top_left <= 255
    ):
        raise IntraPredictionError(
            "horizontal-down 8x8 prediction requires an 8-bit filtered top-left sample"
        )

    vertical_prediction = predict_luma_intra8x8_vertical_right(left, top, top_left)
    return [
        vertical_prediction[column * 8 + row]
        for row in range(8)
        for column in range(8)
    ]


def predict_luma_intra8x8_vertical_left(top: Sequence[int]) -> list[int]:
    """Interpolate an 8x8 luma block across vertical-left top-reference phases."""
    if len(top) != 16 or any(
        not isinstance(sample, int)
        or isinstance(sample, bool)
        or not 0 <= sample <= 255
        for sample in top
    ):
        raise IntraPredictionError(
            "vertical-left 8x8 prediction requires sixteen 8-bit filtered top samples"
        )

    prediction = [0] * 64
    for row in range(8):
        for column in range(8):
            phase = 2 * column + row
            if phase % 2 == 0:
                center = phase // 2
                value = (top[center] + top[center + 1] + 1) >> 1
            else:
                center = (phase - 1) // 2
                value = (
                    top[center] + 2 * top[center + 1] + top[center + 2] + 2
                ) >> 2
            prediction[row * 8 + column] = value
    return prediction


def predict_luma_intra8x8_horizontal_up(left: Sequence[int]) -> list[int]:
    """Predict an 8x8 luma block by transposing vertical-left prediction."""
    if len(left) != 16 or any(
        not isinstance(sample, int)
        or isinstance(sample, bool)
        or not 0 <= sample <= 255
        for sample in left
    ):
        raise IntraPredictionError(
            "horizontal-up 8x8 prediction requires sixteen 8-bit filtered left samples"
        )

    vertical_prediction = predict_luma_intra8x8_vertical_left(left)
    return [
        vertical_prediction[column * 8 + row]
        for row in range(8)
        for column in range(8)
    ]


def predict_luma_intra4x4_vertical(top: Sequence[int]) -> list[int]:
    """Repeat four available top reference samples across a 4x4 luma block."""
    if len(top) != 4 or any(
        not isinstance(sample, int)
        or isinstance(sample, bool)
        or not 0 <= sample <= 255
        for sample in top
    ):
        raise IntraPredictionError("vertical 4x4 prediction requires four 8-bit top samples")
    return list(top) * 4


def predict_luma_intra4x4_horizontal(left: Sequence[int]) -> list[int]:
    """Repeat each of four available left reference samples across one block row."""
    if len(left) != 4 or any(
        not isinstance(sample, int)
        or isinstance(sample, bool)
        or not 0 <= sample <= 255
        for sample in left
    ):
        raise IntraPredictionError("horizontal 4x4 prediction requires four 8-bit left samples")
    return [sample for sample in left for _ in range(4)]


def predict_luma_intra4x4_dc(
    top: Optional[Sequence[int]] = None, left: Optional[Sequence[int]] = None
) -> list[int]:
    """Predict a 4x4 luma block from the available top and left references."""
    for name, references in (("top", top), ("left", left)):
        if references is not None and (
            len(references) != 4
            or any(
                not isinstance(sample, int)
                or isinstance(sample, bool)
                or not 0 <= sample <= 255
                for sample in references
            )
        ):
            raise IntraPredictionError(f"DC 4x4 prediction requires four 8-bit {name} samples")

    if top is not None and left is not None:
        dc_value = (sum(top) + sum(left) + 4) >> 3
    elif top is not None:
        dc_value = (sum(top) + 2) >> 2
    elif left is not None:
        dc_value = (sum(left) + 2) >> 2
    else:
        dc_value = 128
    return [dc_value] * 16


def predict_luma_intra4x4_diagonal_down_left(top: Sequence[int]) -> list[int]:
    """Interpolate a 4x4 luma block from four top and four top-right samples."""
    if len(top) != 8 or any(
        not isinstance(sample, int)
        or isinstance(sample, bool)
        or not 0 <= sample <= 255
        for sample in top
    ):
        raise IntraPredictionError(
            "diagonal-down-left 4x4 prediction requires eight 8-bit top samples"
        )

    prediction = [0] * 16
    for row in range(4):
        for column in range(4):
            index = row + column
            last_index = min(index + 2, 7)
            prediction[row * 4 + column] = (
                top[index] + 2 * top[index + 1] + top[last_index] + 2
            ) >> 2
    return prediction


def predict_luma_intra4x4_diagonal_down_right(
    top: Sequence[int], left: Sequence[int], top_left: int
) -> list[int]:
    """Interpolate a 4x4 luma block from top, left, and top-left references."""
    for name, references, expected_length in (
        ("top", top, 8),
        ("left", left, 4),
    ):
        if len(references) != expected_length or any(
            not isinstance(sample, int)
            or isinstance(sample, bool)
            or not 0 <= sample <= 255
            for sample in references
        ):
            sample_count = "eight" if expected_length == 8 else "four"
            raise IntraPredictionError(
                f"diagonal-down-right 4x4 prediction requires {sample_count} 8-bit {name} samples"
            )
    if (
        not isinstance(top_left, int)
        or isinstance(top_left, bool)
        or not 0 <= top_left <= 255
    ):
        raise IntraPredictionError("diagonal-down-right 4x4 prediction requires an 8-bit top-left sample")

    def reference_at(position: int) -> int:
        if position == -1:
            return top_left
        if position < -1:
            return left[-position - 2]
        return top[position]

    prediction = [0] * 16
    for row in range(4):
        for column in range(4):
            position = column - row
            prediction[row * 4 + column] = (
                reference_at(position - 1)
                + 2 * reference_at(position)
                + reference_at(position + 1)
                + 2
            ) >> 2
    return prediction


def predict_luma_intra4x4_vertical_right(
    top: Sequence[int], left: Sequence[int], top_left: int
) -> list[int]:
    """Interpolate a 4x4 luma block from vertical-right reference phases."""
    for name, references, expected_length in (("top", top, 8), ("left", left, 4)):
        if len(references) != expected_length or any(
            not isinstance(sample, int)
            or isinstance(sample, bool)
            or not 0 <= sample <= 255
            for sample in references
        ):
            sample_count = "eight" if expected_length == 8 else "four"
            raise IntraPredictionError(
                f"vertical-right 4x4 prediction requires {sample_count} 8-bit {name} samples"
            )
    if (
        not isinstance(top_left, int)
        or isinstance(top_left, bool)
        or not 0 <= top_left <= 255
    ):
        raise IntraPredictionError("vertical-right 4x4 prediction requires an 8-bit top-left sample")

    def top_reference_at(position: int) -> int:
        return top_left if position == -1 else top[position]

    prediction = [0] * 16
    for row in range(4):
        for column in range(4):
            phase = 2 * column - row
            if phase >= 0 and phase % 2 == 0:
                center = phase // 2
                value = (
                    top_reference_at(center - 1)
                    + 2 * top_reference_at(center)
                    + top_reference_at(center + 1)
                    + 2
                ) >> 2
            elif phase >= 0:
                center = (phase - 1) // 2
                value = (top_reference_at(center - 1) + top_reference_at(center) + 1) >> 1
            elif phase == -1:
                value = (left[0] + top_left + 1) >> 1
            elif phase == -2:
                value = (left[0] + 2 * top_left + top[0] + 2) >> 2
            else:
                value = (left[0] + left[1] + 1) >> 1
            prediction[row * 4 + column] = value
    return prediction


def predict_luma_intra4x4_horizontal_down(
    top: Sequence[int], left: Sequence[int], top_left: int
) -> list[int]:
    """Transpose vertical-right prediction, extending the final left sample."""
    for name, references in (("top", top), ("left", left)):
        if len(references) != 4 or any(
            not isinstance(sample, int)
            or isinstance(sample, bool)
            or not 0 <= sample <= 255
            for sample in references
        ):
            raise IntraPredictionError(f"horizontal-down 4x4 prediction requires four 8-bit {name} samples")
    if (
        not isinstance(top_left, int)
        or isinstance(top_left, bool)
        or not 0 <= top_left <= 255
    ):
        raise IntraPredictionError("horizontal-down 4x4 prediction requires an 8-bit top-left sample")

    vertical_top = list(left) + [left[-1]] * 4
    vertical_prediction = predict_luma_intra4x4_vertical_right(
        vertical_top, top, top_left
    )
    return [
        vertical_prediction[column * 4 + row]
        for row in range(4)
        for column in range(4)
    ]


def predict_luma_intra4x4_vertical_left(top: Sequence[int]) -> list[int]:
    """Interpolate a 4x4 luma block using alternating top-reference phases."""
    if len(top) != 8 or any(
        not isinstance(sample, int)
        or isinstance(sample, bool)
        or not 0 <= sample <= 255
        for sample in top
    ):
        raise IntraPredictionError("vertical-left 4x4 prediction requires eight 8-bit top samples")

    prediction = [0] * 16
    for row in range(4):
        for column in range(4):
            phase = 2 * column + row
            if phase % 2 == 0:
                center = phase // 2
                value = (top[center] + top[center + 1] + 1) >> 1
            else:
                center = (phase - 1) // 2
                value = (top[center] + 2 * top[center + 1] + top[center + 2] + 2) >> 2
            prediction[row * 4 + column] = value
    return prediction


def predict_luma_intra4x4_horizontal_up(left: Sequence[int]) -> list[int]:
    """Interpolate a 4x4 luma block using alternating left-reference phases."""
    if len(left) != 8 or any(
        not isinstance(sample, int)
        or isinstance(sample, bool)
        or not 0 <= sample <= 255
        for sample in left
    ):
        raise IntraPredictionError("horizontal-up 4x4 prediction requires eight 8-bit left samples")

    prediction = [0] * 16
    for row in range(4):
        for column in range(4):
            phase = 2 * row + column
            if phase % 2 == 0:
                center = phase // 2
                value = (left[center] + left[center + 1] + 1) >> 1
            else:
                center = (phase - 1) // 2
                value = (left[center] + 2 * left[center + 1] + left[center + 2] + 2) >> 2
            prediction[row * 4 + column] = value
    return prediction


def inverse_transform_luma4x4(coefficients: Sequence[int]) -> list[int]:
    """Transform dequantized raster-order coefficients into residual samples."""
    if len(coefficients) != 16 or any(
        not isinstance(coefficient, int) or isinstance(coefficient, bool)
        for coefficient in coefficients
    ):
        raise InverseTransformError("inverse transform requires 16 integer coefficients")

    horizontal = [0] * 16
    for row in range(4):
        row_offset = row * 4
        transformed = _inverse_transform_4x4_line(coefficients[row_offset : row_offset + 4])
        horizontal[row_offset : row_offset + 4] = transformed

    residual = [0] * 16
    for column in range(4):
        transformed = _inverse_transform_4x4_line(
            [horizontal[row * 4 + column] for row in range(4)]
        )
        for row, value in enumerate(transformed):
            residual[row * 4 + column] = (value + 32) >> 6
    return residual


def inverse_transform_luma8x8(coefficients: Sequence[int]) -> list[int]:
    """Transform 8x8 dequantized raster-order coefficients into residual samples."""
    if len(coefficients) != 64 or any(
        not isinstance(coefficient, int) or isinstance(coefficient, bool)
        for coefficient in coefficients
    ):
        raise InverseTransformError("inverse transform requires 64 integer coefficients")

    horizontal = [0] * 64
    for row in range(8):
        row_offset = row * 8
        horizontal[row_offset : row_offset + 8] = _inverse_transform_8x8_line(
            coefficients[row_offset : row_offset + 8]
        )

    residual = [0] * 64
    for column in range(8):
        transformed = _inverse_transform_8x8_line(
            [horizontal[row * 8 + column] for row in range(8)]
        )
        for row, value in enumerate(transformed):
            residual[row * 8 + column] = (value + 32) >> 6
    return residual


def _inverse_transform_4x4_line(coefficients: Sequence[int]) -> list[int]:
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


def _inverse_transform_8x8_line(coefficients: Sequence[int]) -> list[int]:
    a0 = coefficients[0] + coefficients[4]
    a2 = coefficients[0] - coefficients[4]
    a4 = (coefficients[2] >> 1) - coefficients[6]
    a6 = coefficients[2] + (coefficients[6] >> 1)
    b0 = a0 + a6
    b2 = a2 + a4
    b4 = a2 - a4
    b6 = a0 - a6

    a1 = -coefficients[3] + coefficients[5] - coefficients[7] - (coefficients[7] >> 1)
    a3 = coefficients[1] + coefficients[7] - coefficients[3] - (coefficients[3] >> 1)
    a5 = -coefficients[1] + coefficients[7] + coefficients[5] + (coefficients[5] >> 1)
    a7 = coefficients[3] + coefficients[5] + coefficients[1] + (coefficients[1] >> 1)
    b1 = a1 + (a7 >> 2)
    b3 = a3 + (a5 >> 2)
    b5 = a5 - (a3 >> 2)
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