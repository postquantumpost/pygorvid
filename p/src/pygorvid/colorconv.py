"""YUV 4:2:0 to packed RGB conversion primitives."""

import math
from typing import Union

from .pixelbuffer import PixelBuffer, PixelColorRange


class PixelConversionError(ValueError):
    """Raised when color-conversion input planes or strides are invalid."""


BytePlane = Union[bytes, bytearray, memoryview]


class PixelConverter:
    def __init__(self, color_range: PixelColorRange) -> None:
        if isinstance(color_range, bool):
            raise PixelConversionError("color range is invalid")
        try:
            self.color_range = PixelColorRange(color_range)
        except (TypeError, ValueError) as error:
            raise PixelConversionError("color range is invalid") from error

    def convert_yuv420_to_rgb(
        self,
        width: int,
        height: int,
        y_plane: BytePlane,
        u_plane: BytePlane,
        v_plane: BytePlane,
        y_stride: int,
        u_stride: int,
        v_stride: int,
    ) -> PixelBuffer:
        dimensions = (width, height, y_stride, u_stride, v_stride)
        if any(not isinstance(value, int) or isinstance(value, bool) or value <= 0 for value in dimensions):
            raise PixelConversionError("dimensions and strides must be positive integers")

        chroma_width = (width + 1) // 2
        chroma_height = (height + 1) // 2
        if y_stride < width or u_stride < chroma_width or v_stride < chroma_width:
            raise PixelConversionError("chroma and luma strides are too small")

        y_data = _plane_bytes(y_plane)
        u_data = _plane_bytes(u_plane)
        v_data = _plane_bytes(v_plane)
        if (
            len(y_data) < y_stride * height
            or len(u_data) < u_stride * chroma_height
            or len(v_data) < v_stride * chroma_height
        ):
            raise PixelConversionError("input planes are too short for the requested dimensions")

        output_stride = width * 3
        output = bytearray(output_stride * height)
        u_upsampled = _upsample_chroma420(u_data, width, height, u_stride)
        v_upsampled = _upsample_chroma420(v_data, width, height, v_stride)
        for row in range(height):
            for column in range(width):
                output_offset = row * output_stride + column * 3
                y_value = y_data[row * y_stride + column]
                u_value = u_upsampled[row * width + column]
                v_value = v_upsampled[row * width + column]
                red, green, blue = _convert_yuv_sample(
                    y_value, u_value, v_value, self.color_range
                )
                output[output_offset : output_offset + 3] = bytes((red, green, blue))

        return PixelBuffer(
            width,
            height,
            output_stride,
            3,
            PixelColorRange.FULL,
            output,
        )


def _plane_bytes(plane: BytePlane) -> bytes:
    if not isinstance(plane, (bytes, bytearray, memoryview)):
        raise PixelConversionError("input planes must be bytes-like")
    try:
        return memoryview(plane).tobytes()
    except (TypeError, ValueError) as error:
        raise PixelConversionError("input planes must be readable bytes-like data") from error


def _upsample_chroma420(chroma: bytes, width: int, height: int, stride: int) -> bytes:
    upsampled = bytearray(width * height)
    for row in range(height):
        source_row = (row // 2) * stride
        destination_row = row * width
        for column in range(width):
            upsampled[destination_row + column] = chroma[source_row + column // 2]
    return bytes(upsampled)


def _convert_yuv_sample(
    y_value: int, u_value: int, v_value: int, color_range: PixelColorRange
) -> tuple[int, int, int]:
    luma = y_value
    blue_difference = u_value - 128
    red_difference = v_value - 128
    if color_range is PixelColorRange.LIMITED:
        luma -= 16
        red = _round_half_away_from_zero(
            1.164 * luma + 1.596 * red_difference
        )
        green = _round_half_away_from_zero(
            1.164 * luma - 0.392 * blue_difference - 0.813 * red_difference
        )
        blue = _round_half_away_from_zero(
            1.164 * luma + 2.017 * blue_difference
        )
    else:
        red = _round_half_away_from_zero(luma + 1.402 * red_difference)
        green = _round_half_away_from_zero(
            luma - 0.344 * blue_difference - 0.714 * red_difference
        )
        blue = _round_half_away_from_zero(luma + 1.772 * blue_difference)
    return _clamp_byte(red), _clamp_byte(green), _clamp_byte(blue)


def _round_half_away_from_zero(value: float) -> int:
    if value >= 0:
        return math.floor(value + 0.5)
    return math.ceil(value - 0.5)


def _clamp_byte(value: int) -> int:
    return min(255, max(0, value))