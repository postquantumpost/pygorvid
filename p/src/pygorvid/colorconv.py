"""YUV 4:2:0 to packed RGB conversion primitives."""

from typing import Union

import numpy as np

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

        y_values = np.frombuffer(y_data[: y_stride * height], dtype=np.uint8).reshape(
            height, y_stride
        )
        y_values = y_values[:, :width].astype(np.float64)
        u_values = np.frombuffer(
            _upsample_chroma420(u_data, width, height, u_stride), dtype=np.uint8
        ).reshape(height, width).astype(np.float64) - 128
        v_values = np.frombuffer(
            _upsample_chroma420(v_data, width, height, v_stride), dtype=np.uint8
        ).reshape(height, width).astype(np.float64) - 128
        blue_difference = u_values
        red_difference = v_values

        luma = y_values
        if self.color_range is PixelColorRange.LIMITED:
            luma = luma - 16
            red_values = 1.164 * luma + 1.596 * red_difference
            green_values = 1.164 * luma - 0.392 * blue_difference - 0.813 * red_difference
            blue_values = 1.164 * luma + 2.017 * blue_difference
        else:
            red_values = luma + 1.402 * red_difference
            green_values = luma - 0.344 * blue_difference - 0.714 * red_difference
            blue_values = luma + 1.772 * blue_difference

        rgb = np.stack(
            (
                _clamp_bytes(_round_half_away_from_zero(red_values)),
                _clamp_bytes(_round_half_away_from_zero(green_values)),
                _clamp_bytes(_round_half_away_from_zero(blue_values)),
            ),
            axis=-1,
        )
        output_stride = width * 3

        return PixelBuffer(
            width,
            height,
            output_stride,
            3,
            PixelColorRange.FULL,
            rgb.tobytes(),
        )


def _plane_bytes(plane: BytePlane) -> bytes:
    if not isinstance(plane, (bytes, bytearray, memoryview)):
        raise PixelConversionError("input planes must be bytes-like")
    try:
        return memoryview(plane).tobytes()
    except (TypeError, ValueError) as error:
        raise PixelConversionError("input planes must be readable bytes-like data") from error


def _upsample_chroma420(chroma: bytes, width: int, height: int, stride: int) -> bytes:
    chroma_height = (height + 1) // 2
    chroma_width = (width + 1) // 2
    plane = np.frombuffer(chroma[: chroma_height * stride], dtype=np.uint8).reshape(
        chroma_height, stride
    )
    upsampled = np.repeat(np.repeat(plane[:, :chroma_width], 2, axis=0), 2, axis=1)
    return upsampled[:height, :width].tobytes()


def _round_half_away_from_zero(values: np.ndarray) -> np.ndarray:
    return np.where(values >= 0, np.floor(values + 0.5), np.ceil(values - 0.5))


def _clamp_bytes(values: np.ndarray) -> np.ndarray:
    return np.clip(values, 0, 255).astype(np.uint8)