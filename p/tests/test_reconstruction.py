import pytest

from pygorvid import (
    InverseScaleError,
    InverseTransformError,
    IntraPredictionError,
    inverse_scale_luma4x4,
    inverse_scale_luma8x8,
    inverse_transform_luma4x4,
    inverse_transform_luma8x8,
    predict_luma_intra4x4_horizontal,
    predict_luma_intra8x8_horizontal,
    predict_luma_intra8x8_dc,
    predict_luma_intra8x8_diagonal_down_left,
    predict_luma_intra8x8_diagonal_down_right,
    predict_luma_intra8x8_vertical_right,
    predict_luma_intra8x8_horizontal_down,
    predict_luma_intra8x8_vertical_left,
    predict_luma_intra8x8_horizontal_up,
    predict_luma_intra4x4_vertical,
    predict_luma_intra8x8_vertical,
    predict_luma_intra4x4_dc,
    predict_luma_intra4x4_diagonal_down_left,
    predict_luma_intra4x4_diagonal_down_right,
    predict_luma_intra4x4_vertical_right,
    predict_luma_intra4x4_horizontal_down,
    predict_luma_intra4x4_vertical_left,
    predict_luma_intra4x4_horizontal_up,
)


@pytest.mark.parametrize(
    ("qpy", "expected"),
    (
        (0, [10, 13, 10, 13, 13, 16, 13, 16, 10, 13, 10, 13, 13, 16, 13, 16]),
        (24, [160, 208, 160, 208, 208, 256, 208, 256, 160, 208, 160, 208, 208, 256, 208, 256]),
        (51, [3584, 4608, 3584, 4608, 4608, 5888, 4608, 5888, 3584, 4608, 3584, 4608, 4608, 5888, 4608, 5888]),
    ),
)
def test_inverse_scale_luma4x4_qp_vectors(qpy, expected):
    assert inverse_scale_luma4x4([1] * 16, [16] * 16, qpy) == expected


def test_inverse_scale_luma4x4_negative_level_and_custom_weight():
    levels = [0] * 16
    levels[0:2] = [-1, 1]
    scaling_list = [16] * 16
    scaling_list[1] = 8

    scaled = inverse_scale_luma4x4(levels, scaling_list, 0)

    assert (scaled[0], scaled[1]) == (-10, 7)


@pytest.mark.parametrize("qpy", (-1, 52))
def test_inverse_scale_luma4x4_rejects_qpy_outside_range(qpy):
    with pytest.raises(InverseScaleError, match="QPY is outside"):
        inverse_scale_luma4x4([0] * 16, [16] * 16, qpy)


@pytest.mark.parametrize(
    ("levels", "scaling_list", "message"),
    (
        ([0] * 15, [16] * 16, "16 signed 32-bit integers"),
        ([1 << 31] + [0] * 15, [16] * 16, "16 signed 32-bit integers"),
        ([0] * 16, [16] * 15, "16 values in \\[1,255\\]"),
        ([0] * 16, [16] * 7 + [0] + [16] * 8, "16 values in \\[1,255\\]"),
    ),
)
def test_inverse_scale_luma4x4_rejects_invalid_vectors(levels, scaling_list, message):
    with pytest.raises(InverseScaleError, match=message):
        inverse_scale_luma4x4(levels, scaling_list, 0)


@pytest.mark.parametrize(
    ("qpy", "expected"),
    (
        (
            0,
            [
                20, 19, 25, 19, 20, 19, 25, 19,
                19, 18, 24, 18, 19, 18, 24, 18,
                25, 24, 32, 24, 25, 24, 32, 24,
                19, 18, 24, 18, 19, 18, 24, 18,
                20, 19, 25, 19, 20, 19, 25, 19,
                19, 18, 24, 18, 19, 18, 24, 18,
                25, 24, 32, 24, 25, 24, 32, 24,
                19, 18, 24, 18, 19, 18, 24, 18,
            ],
        ),
        (
            24,
            [
                320, 304, 400, 304, 320, 304, 400, 304,
                304, 288, 384, 288, 304, 288, 384, 288,
                400, 384, 512, 384, 400, 384, 512, 384,
                304, 288, 384, 288, 304, 288, 384, 288,
                320, 304, 400, 304, 320, 304, 400, 304,
                304, 288, 384, 288, 304, 288, 384, 288,
                400, 384, 512, 384, 400, 384, 512, 384,
                304, 288, 384, 288, 304, 288, 384, 288,
            ],
        ),
        (
            51,
            [
                7168, 6656, 8960, 6656, 7168, 6656, 8960, 6656,
                6656, 6400, 8448, 6400, 6656, 6400, 8448, 6400,
                8960, 8448, 11520, 8448, 8960, 8448, 11520, 8448,
                6656, 6400, 8448, 6400, 6656, 6400, 8448, 6400,
                7168, 6656, 8960, 6656, 7168, 6656, 8960, 6656,
                6656, 6400, 8448, 6400, 6656, 6400, 8448, 6400,
                8960, 8448, 11520, 8448, 8960, 8448, 11520, 8448,
                6656, 6400, 8448, 6400, 6656, 6400, 8448, 6400,
            ],
        ),
    ),
)
def test_inverse_scale_luma8x8_scaling_classes_and_qp_vectors(qpy, expected):
    assert inverse_scale_luma8x8([1] * 64, [16] * 64, qpy) == expected


def test_inverse_scale_luma8x8_negative_level_and_custom_weight():
    levels = [0] * 64
    levels[0:2] = [-1, 1]
    scaling_list = [16] * 64
    scaling_list[1] = 8

    scaled = inverse_scale_luma8x8(levels, scaling_list, 0)

    assert (scaled[0], scaled[1]) == (-20, 10)


@pytest.mark.parametrize("qpy", (-1, 52))
def test_inverse_scale_luma8x8_rejects_qpy_outside_range(qpy):
    with pytest.raises(InverseScaleError, match="QPY is outside"):
        inverse_scale_luma8x8([0] * 64, [16] * 64, qpy)


@pytest.mark.parametrize(
    ("levels", "scaling_list", "message"),
    (
        ([0] * 63, [16] * 64, "64 signed 32-bit integers"),
        ([1 << 31] + [0] * 63, [16] * 64, "64 signed 32-bit integers"),
        ([0] * 64, [16] * 63, "64 values in \\[1,255\\]"),
        ([0] * 64, [16] * 42 + [0] + [16] * 21, "64 values in \\[1,255\\]"),
    ),
)
def test_inverse_scale_luma8x8_rejects_invalid_vectors(levels, scaling_list, message):
    with pytest.raises(InverseScaleError, match=message):
        inverse_scale_luma8x8(levels, scaling_list, 0)


def test_inverse_transform_luma4x4_dc_and_frequency_impulse_vectors():
    dc_coefficients = [64] + [0] * 15
    assert inverse_transform_luma4x4(dc_coefficients) == [1] * 16

    horizontal_frequency = [0] * 16
    horizontal_frequency[1] = 64
    expected = [1, 1, 0, -1] * 4
    assert inverse_transform_luma4x4(horizontal_frequency) == expected


@pytest.mark.parametrize(("dc_level", "expected"), ((31, 0), (32, 1), (-33, -1)))
def test_inverse_transform_luma4x4_rounding_vectors(dc_level, expected):
    assert inverse_transform_luma4x4([dc_level] + [0] * 15) == [expected] * 16


@pytest.mark.parametrize(
    "coefficients",
    ([0] * 15, [0] * 15 + [True]),
)
def test_inverse_transform_luma4x4_rejects_invalid_inputs(coefficients):
    with pytest.raises(InverseTransformError, match="16 integer coefficients"):
        inverse_transform_luma4x4(coefficients)


def test_inverse_transform_luma8x8_dc_and_frequency_impulse_vectors():
    dc_coefficients = [64] + [0] * 63
    assert inverse_transform_luma8x8(dc_coefficients) == [1] * 64

    frequency = [2, -1, 1, 0, 0, -1, 1, -1]
    horizontal_frequency = [0] * 64
    horizontal_frequency[1] = 64
    assert inverse_transform_luma8x8(horizontal_frequency) == frequency * 8

    vertical_frequency = [0] * 64
    vertical_frequency[8] = 64
    assert inverse_transform_luma8x8(vertical_frequency) == [
        value for value in frequency for _ in range(8)
    ]


@pytest.mark.parametrize(("dc_level", "expected"), ((31, 0), (32, 1), (-33, -1)))
def test_inverse_transform_luma8x8_rounding_vectors(dc_level, expected):
    assert inverse_transform_luma8x8([dc_level] + [0] * 63) == [expected] * 64


@pytest.mark.parametrize(
    "coefficients",
    ([0] * 63, [0] * 63 + [True]),
)
def test_inverse_transform_luma8x8_rejects_invalid_inputs(coefficients):
    with pytest.raises(InverseTransformError, match="64 integer coefficients"):
        inverse_transform_luma8x8(coefficients)


def test_predict_luma_intra8x8_vertical_repeats_filtered_top_samples():
    top = [0, 17, 63, 129, 190, 220, 254, 255]
    assert predict_luma_intra8x8_vertical(top) == top * 8


def test_predict_luma_intra8x8_horizontal_repeats_filtered_left_samples():
    left = [0, 17, 63, 129, 190, 220, 254, 255]
    assert predict_luma_intra8x8_horizontal(left) == [sample for sample in left for _ in range(8)]


@pytest.mark.parametrize(
    "left", ([1, 2, 3], [0, 1, 2, 3, 4, 5, 6, 256], [0, 1, 2, 3, 4, 5, 6, True])
)
def test_predict_luma_intra8x8_horizontal_rejects_invalid_left_samples(left):
    with pytest.raises(IntraPredictionError, match="eight 8-bit filtered left samples"):
        predict_luma_intra8x8_horizontal(left)


@pytest.mark.parametrize("top", ([1, 2, 3], [0, 1, 2, 3, 4, 5, 6, 256], [0, 1, 2, 3, 4, 5, 6, True]))
def test_predict_luma_intra8x8_vertical_rejects_invalid_top_samples(top):
    with pytest.raises(IntraPredictionError, match="eight 8-bit filtered top samples"):
        predict_luma_intra8x8_vertical(top)


@pytest.mark.parametrize(
    ("top", "expected"),
    (
        ([10, 40, 90, 160], [10, 40, 90, 160] * 4),
        ([0, 255, 0, 255], [0, 255, 0, 255] * 4),
    ),
)
def test_predict_luma_intra4x4_vertical_repeats_top_samples(top, expected):
    assert predict_luma_intra4x4_vertical(top) == expected


@pytest.mark.parametrize("top", ([1, 2, 3], [0, 1, 2, 256], [0, 1, 2, True]))
def test_predict_luma_intra4x4_vertical_rejects_invalid_top_samples(top):
    with pytest.raises(IntraPredictionError, match="four 8-bit top samples"):
        predict_luma_intra4x4_vertical(top)


@pytest.mark.parametrize(
    ("left", "expected"),
    (
        ([10, 40, 90, 160], [10] * 4 + [40] * 4 + [90] * 4 + [160] * 4),
        ([0, 255, 0, 255], [0] * 4 + [255] * 4 + [0] * 4 + [255] * 4),
    ),
)
def test_predict_luma_intra4x4_horizontal_repeats_left_samples(left, expected):
    assert predict_luma_intra4x4_horizontal(left) == expected


@pytest.mark.parametrize("left", ([1, 2, 3], [0, 1, 2, 256], [0, 1, 2, True]))
def test_predict_luma_intra4x4_horizontal_rejects_invalid_left_samples(left):
    with pytest.raises(IntraPredictionError, match="four 8-bit left samples"):
        predict_luma_intra4x4_horizontal(left)


@pytest.mark.parametrize(
    ("top", "left", "expected"),
    (
        ([10, 40, 90, 160, 20, 50, 80, 110], [20, 60, 100, 140, 30, 70, 110, 150], 78),
        ([10, 40, 90, 160, 20, 50, 80, 110], None, 70),
        (None, [20, 60, 100, 140, 30, 70, 110, 150], 85),
        (None, None, 128),
    ),
)
def test_predict_luma_intra8x8_dc_reference_availability(top, left, expected):
    assert predict_luma_intra8x8_dc(top, left) == [expected] * 64


@pytest.mark.parametrize(
    ("top", "left", "message"),
    (
        ([1, 2, 3], None, "eight 8-bit filtered top samples"),
        ([0, 1, 2, 3, 4, 5, 6, 256], None, "eight 8-bit filtered top samples"),
        (None, [1, 2, 3], "eight 8-bit filtered left samples"),
        (None, [0, 1, 2, 3, 4, 5, 6, True], "eight 8-bit filtered left samples"),
    ),
)
def test_predict_luma_intra8x8_dc_rejects_invalid_references(top, left, message):
    with pytest.raises(IntraPredictionError, match=message):
        predict_luma_intra8x8_dc(top, left)


def test_predict_luma_intra8x8_diagonal_down_left_interpolates_filtered_top_references():
    top = list(range(10, 161, 10))
    assert predict_luma_intra8x8_diagonal_down_left(top) == [
        20, 30, 40, 50, 60, 70, 80, 90,
        30, 40, 50, 60, 70, 80, 90, 100,
        40, 50, 60, 70, 80, 90, 100, 110,
        50, 60, 70, 80, 90, 100, 110, 120,
        60, 70, 80, 90, 100, 110, 120, 130,
        70, 80, 90, 100, 110, 120, 130, 140,
        80, 90, 100, 110, 120, 130, 140, 150,
        90, 100, 110, 120, 130, 140, 150, 158,
    ]


@pytest.mark.parametrize(
    "top", ([1, 2, 3], [*range(15), 256], [*range(15), True])
)
def test_predict_luma_intra8x8_diagonal_down_left_rejects_invalid_top_samples(top):
    with pytest.raises(
        IntraPredictionError,
        match="sixteen 8-bit filtered top samples",
    ):
        predict_luma_intra8x8_diagonal_down_left(top)


def test_predict_luma_intra8x8_diagonal_down_right_uses_all_reference_regions():
    assert predict_luma_intra8x8_diagonal_down_right(
        [20, 40, 60, 80, 100, 120, 140, 160, 180, 200, 220, 240, 250, 230, 210, 190],
        [60, 100, 140, 180, 220, 240, 200, 160],
        30,
    ) == [
        28, 40, 60, 80, 100, 120, 140, 160,
        35, 28, 40, 60, 80, 100, 120, 140,
        63, 35, 28, 40, 60, 80, 100, 120,
        100, 63, 35, 28, 40, 60, 80, 100,
        140, 100, 63, 35, 28, 40, 60, 80,
        180, 140, 100, 63, 35, 28, 40, 60,
        215, 180, 140, 100, 63, 35, 28, 40,
        225, 215, 180, 140, 100, 63, 35, 28,
    ]


@pytest.mark.parametrize(
    ("top", "left", "top_left", "message"),
    (
        ([1] * 15, [2] * 8, 3, "sixteen 8-bit filtered top samples"),
        ([1] * 16, [2] * 7, 3, "eight 8-bit filtered left samples"),
        ([1] * 15 + [256], [2] * 8, 3, "sixteen 8-bit filtered top samples"),
        ([1] * 16, [2] * 7 + [True], 3, "eight 8-bit filtered left samples"),
        ([1] * 16, [2] * 8, 256, "8-bit filtered top-left sample"),
        ([1] * 16, [2] * 8, True, "8-bit filtered top-left sample"),
    ),
)
def test_predict_luma_intra8x8_diagonal_down_right_rejects_invalid_references(
    top, left, top_left, message
):
    with pytest.raises(IntraPredictionError, match=message):
        predict_luma_intra8x8_diagonal_down_right(top, left, top_left)


def test_predict_luma_intra8x8_vertical_right_uses_all_reference_regions():
    assert predict_luma_intra8x8_vertical_right(
        [20, 40, 60, 80, 100, 120, 140, 160, 180, 200, 220, 240, 250, 230, 210, 190],
        [60, 100, 140, 180, 220, 240, 200, 160],
        30,
    ) == [
        28, 40, 60, 80, 100, 120, 140, 160,
        45, 25, 30, 50, 70, 90, 110, 130,
        35, 28, 40, 60, 80, 100, 120, 140,
        80, 45, 25, 30, 50, 70, 90, 110,
        100, 35, 28, 40, 60, 80, 100, 120,
        120, 80, 45, 25, 30, 50, 70, 90,
        140, 100, 35, 28, 40, 60, 80, 100,
        160, 120, 80, 45, 25, 30, 50, 70,
    ]


@pytest.mark.parametrize(
    ("top", "left", "top_left", "message"),
    (
        ([1] * 15, [2] * 8, 3, "sixteen 8-bit filtered top samples"),
        ([1] * 16, [2] * 7, 3, "eight 8-bit filtered left samples"),
        ([1] * 15 + [256], [2] * 8, 3, "sixteen 8-bit filtered top samples"),
        ([1] * 16, [2] * 7 + [True], 3, "eight 8-bit filtered left samples"),
        ([1] * 16, [2] * 8, 256, "8-bit filtered top-left sample"),
        ([1] * 16, [2] * 8, True, "8-bit filtered top-left sample"),
    ),
)
def test_predict_luma_intra8x8_vertical_right_rejects_invalid_references(
    top, left, top_left, message
):
    with pytest.raises(IntraPredictionError, match=message):
        predict_luma_intra8x8_vertical_right(top, left, top_left)


def test_predict_luma_intra8x8_horizontal_down_transposes_vertical_right():
    assert predict_luma_intra8x8_horizontal_down(
        [60, 100, 140, 180, 220, 240, 200, 160],
        [20, 40, 60, 80, 100, 120, 140, 160, 180, 200, 220, 240, 250, 230, 210, 190],
        30,
    ) == [
        28, 45, 35, 80, 100, 120, 140, 160,
        40, 25, 28, 45, 35, 80, 100, 120,
        60, 30, 40, 25, 28, 45, 35, 80,
        80, 50, 60, 30, 40, 25, 28, 45,
        100, 70, 80, 50, 60, 30, 40, 25,
        120, 90, 100, 70, 80, 50, 60, 30,
        140, 110, 120, 90, 100, 70, 80, 50,
        160, 130, 140, 110, 120, 90, 100, 70,
    ]


@pytest.mark.parametrize(
    ("top", "left", "top_left", "message"),
    (
        ([1] * 7, [2] * 16, 3, "eight 8-bit filtered top samples"),
        ([1] * 8, [2] * 15, 3, "sixteen 8-bit filtered left samples"),
        ([1] * 7 + [256], [2] * 16, 3, "eight 8-bit filtered top samples"),
        ([1] * 8, [2] * 15 + [True], 3, "sixteen 8-bit filtered left samples"),
        ([1] * 8, [2] * 16, 256, "8-bit filtered top-left sample"),
        ([1] * 8, [2] * 16, True, "8-bit filtered top-left sample"),
    ),
)
def test_predict_luma_intra8x8_horizontal_down_rejects_invalid_references(
    top, left, top_left, message
):
    with pytest.raises(IntraPredictionError, match=message):
        predict_luma_intra8x8_horizontal_down(top, left, top_left)


def test_predict_luma_intra8x8_vertical_left_alternates_top_reference_phases():
    top = list(range(10, 161, 10))
    assert predict_luma_intra8x8_vertical_left(top) == [
        15, 25, 35, 45, 55, 65, 75, 85,
        20, 30, 40, 50, 60, 70, 80, 90,
        25, 35, 45, 55, 65, 75, 85, 95,
        30, 40, 50, 60, 70, 80, 90, 100,
        35, 45, 55, 65, 75, 85, 95, 105,
        40, 50, 60, 70, 80, 90, 100, 110,
        45, 55, 65, 75, 85, 95, 105, 115,
        50, 60, 70, 80, 90, 100, 110, 120,
    ]


@pytest.mark.parametrize("top", ([1] * 15, [1] * 15 + [256], [1] * 15 + [True]))
def test_predict_luma_intra8x8_vertical_left_rejects_invalid_top_samples(top):
    with pytest.raises(IntraPredictionError, match="sixteen 8-bit filtered top samples"):
        predict_luma_intra8x8_vertical_left(top)


def test_predict_luma_intra8x8_horizontal_up_transposes_vertical_left():
    left = list(range(10, 161, 10))
    assert predict_luma_intra8x8_horizontal_up(left) == [
        15, 20, 25, 30, 35, 40, 45, 50,
        25, 30, 35, 40, 45, 50, 55, 60,
        35, 40, 45, 50, 55, 60, 65, 70,
        45, 50, 55, 60, 65, 70, 75, 80,
        55, 60, 65, 70, 75, 80, 85, 90,
        65, 70, 75, 80, 85, 90, 95, 100,
        75, 80, 85, 90, 95, 100, 105, 110,
        85, 90, 95, 100, 105, 110, 115, 120,
    ]


@pytest.mark.parametrize("left", ([1] * 15, [1] * 15 + [256], [1] * 15 + [True]))
def test_predict_luma_intra8x8_horizontal_up_rejects_invalid_left_samples(left):
    with pytest.raises(IntraPredictionError, match="sixteen 8-bit filtered left samples"):
        predict_luma_intra8x8_horizontal_up(left)


@pytest.mark.parametrize(
    ("top", "left", "expected"),
    (
        ([10, 40, 90, 161], [20, 60, 100, 141], 78),
        ([10, 40, 90, 161], None, 75),
        (None, [20, 60, 100, 141], 80),
        (None, None, 128),
    ),
)
def test_predict_luma_intra4x4_dc_reference_availability(top, left, expected):
    assert predict_luma_intra4x4_dc(top, left) == [expected] * 16


@pytest.mark.parametrize(
    ("top", "left", "message"),
    (
        ([1, 2, 3], None, "four 8-bit top samples"),
        ([0, 1, 2, 256], None, "four 8-bit top samples"),
        (None, [1, 2, 3], "four 8-bit left samples"),
        (None, [0, 1, 2, True], "four 8-bit left samples"),
    ),
)
def test_predict_luma_intra4x4_dc_rejects_invalid_references(top, left, message):
    with pytest.raises(IntraPredictionError, match=message):
        predict_luma_intra4x4_dc(top, left)


def test_predict_luma_intra4x4_diagonal_down_left_interpolates_top_references():
    assert predict_luma_intra4x4_diagonal_down_left(
        [10, 20, 30, 40, 50, 60, 70, 80]
    ) == [
        20, 30, 40, 50,
        30, 40, 50, 60,
        40, 50, 60, 70,
        50, 60, 70, 78,
    ]


@pytest.mark.parametrize(
    "top",
    ([1, 2, 3, 4, 5, 6, 7], [0, 1, 2, 3, 4, 5, 6, 256], [0, 1, 2, 3, 4, 5, 6, True]),
)
def test_predict_luma_intra4x4_diagonal_down_left_rejects_invalid_top(top):
    with pytest.raises(IntraPredictionError, match="eight 8-bit top samples"):
        predict_luma_intra4x4_diagonal_down_left(top)


def test_predict_luma_intra4x4_diagonal_down_right_uses_all_reference_regions():
    assert predict_luma_intra4x4_diagonal_down_right(
        [20, 40, 80, 120, 160, 200, 220, 240],
        [60, 100, 140, 180],
        30,
    ) == [
        28, 45, 80, 120,
        35, 28, 45, 80,
        63, 35, 28, 45,
        100, 63, 35, 28,
    ]


@pytest.mark.parametrize(
    ("top", "left", "top_left", "message"),
    (
        ([1] * 7, [2] * 4, 3, "eight 8-bit top samples"),
        ([1] * 8, [2] * 3, 3, "four 8-bit left samples"),
        ([1] * 8, [2] * 4, 256, "8-bit top-left sample"),
        ([1] * 8, [2] * 4, True, "8-bit top-left sample"),
    ),
)
def test_predict_luma_intra4x4_diagonal_down_right_rejects_invalid_references(
    top, left, top_left, message
):
    with pytest.raises(IntraPredictionError, match=message):
        predict_luma_intra4x4_diagonal_down_right(top, left, top_left)


def test_predict_luma_intra4x4_vertical_right_uses_all_reference_regions():
    assert predict_luma_intra4x4_vertical_right(
        [20, 40, 80, 120, 160, 200, 220, 240],
        [60, 100, 140, 180],
        30,
    ) == [
        28, 45, 80, 120,
        45, 25, 30, 60,
        35, 28, 45, 80,
        80, 45, 25, 30,
    ]


@pytest.mark.parametrize(
    ("top", "left", "top_left", "message"),
    (
        ([1] * 7, [2] * 4, 3, "eight 8-bit top samples"),
        ([1] * 8, [2] * 3, 3, "four 8-bit left samples"),
        ([1] * 8, [2] * 4, 256, "8-bit top-left sample"),
        ([1] * 8, [2] * 4, True, "8-bit top-left sample"),
    ),
)
def test_predict_luma_intra4x4_vertical_right_rejects_invalid_references(
    top, left, top_left, message
):
    with pytest.raises(IntraPredictionError, match=message):
        predict_luma_intra4x4_vertical_right(top, left, top_left)


def test_predict_luma_intra4x4_horizontal_down_transposes_vertical_right():
    assert predict_luma_intra4x4_horizontal_down(
        [20, 40, 80, 120],
        [60, 100, 140, 180],
        30,
    ) == [
        63, 25, 35, 30,
        100, 45, 63, 25,
        140, 80, 100, 45,
        170, 120, 140, 80,
    ]


@pytest.mark.parametrize(
    ("top", "left", "top_left", "message"),
    (
        ([1, 2, 3], [4, 5, 6, 7], 8, "four 8-bit top samples"),
        ([1, 2, 3, 4], [5, 6, 7], 8, "four 8-bit left samples"),
        ([1, 2, 3, 4], [5, 6, 7, 8], 256, "8-bit top-left sample"),
        ([1, 2, 3, 4], [5, 6, 7, 8], True, "8-bit top-left sample"),
    ),
)
def test_predict_luma_intra4x4_horizontal_down_rejects_invalid_references(
    top, left, top_left, message
):
    with pytest.raises(IntraPredictionError, match=message):
        predict_luma_intra4x4_horizontal_down(top, left, top_left)


def test_predict_luma_intra4x4_vertical_left_alternates_top_reference_phases():
    assert predict_luma_intra4x4_vertical_left(
        [10, 20, 30, 40, 50, 60, 70, 80]
    ) == [
        15, 25, 35, 45,
        20, 30, 40, 50,
        25, 35, 45, 55,
        30, 40, 50, 60,
    ]


@pytest.mark.parametrize(
    "top",
    ([1, 2, 3, 4, 5, 6, 7], [0, 1, 2, 3, 4, 5, 6, 256], [0, 1, 2, 3, 4, 5, 6, True]),
)
def test_predict_luma_intra4x4_vertical_left_rejects_invalid_top_samples(top):
    with pytest.raises(IntraPredictionError, match="eight 8-bit top samples"):
        predict_luma_intra4x4_vertical_left(top)


def test_predict_luma_intra4x4_horizontal_up_transposes_vertical_left():
    assert predict_luma_intra4x4_horizontal_up(
        [10, 20, 30, 40, 50, 60, 70, 80]
    ) == [
        15, 20, 25, 30,
        25, 30, 35, 40,
        35, 40, 45, 50,
        45, 50, 55, 60,
    ]


@pytest.mark.parametrize(
    "left",
    ([1, 2, 3, 4, 5, 6, 7], [0, 1, 2, 3, 4, 5, 6, 256], [0, 1, 2, 3, 4, 5, 6, True]),
)
def test_predict_luma_intra4x4_horizontal_up_rejects_invalid_left_samples(left):
    with pytest.raises(IntraPredictionError, match="eight 8-bit left samples"):
        predict_luma_intra4x4_horizontal_up(left)