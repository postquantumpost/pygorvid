"""Owned packed-pixel storage shared by native image encoders."""

from dataclasses import dataclass
from enum import IntEnum
from typing import Union


class PixelBufferLayoutError(ValueError):
    """Raised when pixel-buffer dimensions, stride, or storage are invalid."""


class PixelBufferRangeError(ValueError):
    """Raised when a pixel buffer uses an unsupported color range."""


class PixelColorRange(IntEnum):
    LIMITED = 0
    FULL = 1


PixelData = Union[bytes, bytearray, memoryview]


@dataclass(frozen=True)
class PixelBuffer:
    width: int
    height: int
    stride: int
    channels: int
    color_range: PixelColorRange
    data: PixelData

    def __post_init__(self) -> None:
        dimensions = (self.width, self.height, self.stride, self.channels)
        if any(not isinstance(value, int) or isinstance(value, bool) or value <= 0 for value in dimensions):
            raise PixelBufferLayoutError("dimensions, stride, and channels must be positive integers")
        if self.stride < self.width * self.channels:
            raise PixelBufferLayoutError("stride must cover the row width")
        if isinstance(self.color_range, bool):
            raise PixelBufferRangeError("color range is invalid")
        try:
            color_range = PixelColorRange(self.color_range)
        except (TypeError, ValueError) as error:
            raise PixelBufferRangeError("color range is invalid") from error
        if not isinstance(self.data, (bytes, bytearray, memoryview)):
            raise PixelBufferLayoutError("pixel data must be bytes-like")

        try:
            source = memoryview(self.data).tobytes()
        except (TypeError, ValueError) as error:
            raise PixelBufferLayoutError("pixel data must be readable bytes-like storage") from error
        required_size = self.stride * self.height
        if len(source) < required_size:
            raise PixelBufferLayoutError(
                f"data must hold at least {required_size} bytes for "
                f"{self.width}x{self.height} stride={self.stride}"
            )

        object.__setattr__(self, "color_range", color_range)
        object.__setattr__(self, "data", source[:required_size])

    def clone(self) -> "PixelBuffer":
        return PixelBuffer(
            self.width,
            self.height,
            self.stride,
            self.channels,
            self.color_range,
            self.data,
        )

    def row(self, y: int) -> bytes:
        if not isinstance(y, int) or isinstance(y, bool) or not 0 <= y < self.height:
            raise PixelBufferLayoutError(f"row index {y} is outside [0, {self.height})")
        start = y * self.stride
        return self.data[start : start + self.stride]

    def pixel_offset(self, x: int, y: int) -> int:
        if (
            not isinstance(x, int)
            or isinstance(x, bool)
            or not isinstance(y, int)
            or isinstance(y, bool)
            or not 0 <= x < self.width
            or not 0 <= y < self.height
        ):
            raise PixelBufferLayoutError(
                f"pixel ({x},{y}) is outside {self.width}x{self.height} buffer"
            )
        return y * self.stride + x * self.channels