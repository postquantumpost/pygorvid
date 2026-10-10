import pytest

from pygorvid import PixelColorRange, PixelConversionError, PixelConverter
from pygorvid.colorconv import _upsample_chroma420


def test_limited_range_black_and_white_match_reference_vectors():
    converter = PixelConverter(PixelColorRange.LIMITED)

    output = converter.convert_yuv420_to_rgb(
        2,
        1,
        bytes((16, 235)),
        bytes((128, 128)),
        bytes((128, 128)),
        2,
        2,
        2,
    )

    assert output.data == bytes((0, 0, 0, 255, 255, 255))
    assert output.color_range is PixelColorRange.FULL
    assert output.stride == 6


def test_full_range_gray_and_primaries_match_reference_vectors():
    converter = PixelConverter(PixelColorRange.FULL)

    gray = converter.convert_yuv420_to_rgb(
        1, 1, bytes((128,)), bytes((128,)), bytes((128,)), 1, 1, 1
    )
    assert gray.data == bytes((128, 128, 128))

    vectors = (
        (76, 85, 255, (254, 0, 0)),
        (150, 44, 21, (0, 255, 1)),
        (29, 255, 107, (0, 0, 254)),
    )
    for y_value, u_value, v_value, expected in vectors:
        output = converter.convert_yuv420_to_rgb(
            1, 1, bytes((y_value,)), bytes((u_value,)), bytes((v_value,)), 1, 1, 1
        )
        assert output.data == bytes(expected)


def test_odd_dimensions_and_plane_stride_use_2x2_chroma_upsampling():
    converter = PixelConverter(PixelColorRange.FULL)
    width, height = 3, 3
    y_plane = bytes((128, 128, 128, 90, 90, 128, 128, 128, 91, 91, 128, 128, 128, 92, 92))
    u_plane = bytes((128, 128, 30, 64, 192, 31))
    v_plane = bytes((128, 128, 40, 80, 160, 41))

    output = converter.convert_yuv420_to_rgb(
        width, height, y_plane, u_plane, v_plane, 5, 3, 3
    )

    assert (output.width, output.height, output.stride) == (3, 3, 9)
    assert output.data[0:3] == bytes((128, 128, 128))
    assert output.data[3:6] == bytes((128, 128, 128))
    assert output.data[9:12] == bytes((128, 128, 128))
    assert output.data[12:15] == bytes((128, 128, 128))


def test_chroma_upsampling_duplicates_samples_on_odd_dimensions():
    assert _upsample_chroma420(bytes((10, 20, 30, 40)), 3, 3, 2) == bytes(
        (10, 10, 20, 10, 10, 20, 30, 30, 40)
    )


@pytest.mark.parametrize(
    "arguments",
    [
        (0, 1, b"x", b"x", b"x", 1, 1, 1),
        (1, 0, b"x", b"x", b"x", 1, 1, 1),
        (2, 1, b"x", b"x", b"x", 1, 1, 1),
        (1, 1, b"x", b"x", b"x", 0, 1, 1),
        (1, 1, b"x", b"x", b"x", 1, 0, 1),
        (1, 1, b"x", b"x", b"x", 1, 1, 0),
        (1, 1, b"", b"x", b"x", 1, 1, 1),
        (1, 1, b"x", b"", b"x", 1, 1, 1),
        (1, 1, b"x", b"x", b"", 1, 1, 1),
    ],
)
def test_converter_rejects_invalid_dimensions_strides_and_plane_lengths(arguments):
    with pytest.raises(PixelConversionError):
        PixelConverter(PixelColorRange.FULL).convert_yuv420_to_rgb(*arguments)


@pytest.mark.parametrize("color_range", [9, True, "limited"])
def test_converter_rejects_invalid_range(color_range):
    with pytest.raises(PixelConversionError):
        PixelConverter(color_range)