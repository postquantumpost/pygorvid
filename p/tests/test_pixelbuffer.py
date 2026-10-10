import pytest

from pygorvid import (
    PixelBuffer,
    PixelBufferLayoutError,
    PixelBufferRangeError,
    PixelColorRange,
)


def test_pixel_buffer_owns_storage_and_preserves_stride_padding():
    source = bytearray(range(12))
    buffer = PixelBuffer(2, 2, 4, 1, PixelColorRange.LIMITED, source)

    assert buffer.width == 2
    assert buffer.height == 2
    assert buffer.stride == 4
    assert buffer.channels == 1
    assert buffer.color_range is PixelColorRange.LIMITED
    assert buffer.data == bytes(range(8))
    assert buffer.row(1) == bytes((4, 5, 6, 7))
    assert buffer.pixel_offset(1, 1) == 5

    source[0] = 99
    assert buffer.data[0] == 0


def test_pixel_buffer_accepts_full_range_and_clone():
    buffer = PixelBuffer(2, 1, 6, 3, PixelColorRange.FULL, memoryview(b"abcdef-extra"))
    clone = buffer.clone()

    assert buffer.color_range is PixelColorRange.FULL
    assert buffer.data == b"abcdef"
    assert clone == buffer
    assert clone is not buffer


@pytest.mark.parametrize(
    "arguments",
    [
        (0, 2, 2, 1, PixelColorRange.LIMITED, b"1234"),
        (2, 0, 2, 1, PixelColorRange.LIMITED, b"1234"),
        (2, 2, 0, 1, PixelColorRange.LIMITED, b"1234"),
        (2, 2, 2, 0, PixelColorRange.LIMITED, b"1234"),
        (2, 2, 3, 2, PixelColorRange.LIMITED, b"123456"),
        (2, 2, 4, 1, PixelColorRange.LIMITED, b"1234567"),
        (True, 1, 1, 1, PixelColorRange.LIMITED, b"1"),
    ],
)
def test_pixel_buffer_rejects_invalid_layout(arguments):
    with pytest.raises(PixelBufferLayoutError):
        PixelBuffer(*arguments)


@pytest.mark.parametrize("color_range", [9, True, "full"])
def test_pixel_buffer_rejects_invalid_color_range(color_range):
    with pytest.raises(PixelBufferRangeError):
        PixelBuffer(1, 1, 1, 1, color_range, b"x")


def test_pixel_buffer_rejects_rows_and_pixels_outside_layout():
    buffer = PixelBuffer(2, 2, 4, 1, PixelColorRange.FULL, bytes(range(8)))

    with pytest.raises(PixelBufferLayoutError):
        buffer.row(-1)
    with pytest.raises(PixelBufferLayoutError):
        buffer.row(2)
    with pytest.raises(PixelBufferLayoutError):
        buffer.pixel_offset(2, 0)
    with pytest.raises(PixelBufferLayoutError):
        buffer.pixel_offset(0, 2)