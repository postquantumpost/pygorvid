"""Baseline JPEG 8x8 transform, quantization, and scan-order primitives."""

import math
from collections.abc import Sequence


class JPEGEncoderInvalidQuantizationTableError(ValueError):
    """Raised when a JPEG quantization entry is not in the range 1..255."""


class JPEGEncoderCoefficientOutOfRangeError(ValueError):
    """Raised when a quantized coefficient cannot fit in a signed 16-bit value."""


_ZIGZAG_INDICES = (
    0, 1, 8, 16, 9, 2, 3, 10,
    17, 24, 32, 25, 18, 11, 4, 5,
    12, 19, 26, 33, 40, 48, 41, 34,
    27, 20, 13, 6, 7, 14, 21, 28,
    35, 42, 49, 56, 57, 50, 43, 36,
    29, 22, 15, 23, 30, 37, 44, 51,
    58, 59, 52, 45, 38, 31, 39, 46,
    53, 60, 61, 54, 47, 55, 62, 63,
)

_BASE_LUMA_QUANTIZATION = (
    16, 11, 10, 16, 24, 40, 51, 61,
    12, 12, 14, 19, 26, 58, 60, 55,
    14, 13, 16, 24, 40, 57, 69, 56,
    14, 17, 22, 29, 51, 87, 80, 62,
    18, 22, 37, 56, 68, 109, 103, 77,
    24, 35, 55, 64, 81, 104, 113, 92,
    49, 64, 78, 87, 103, 121, 120, 101,
    72, 92, 95, 98, 112, 100, 103, 99,
)

_BASE_CHROMA_QUANTIZATION = (
    17, 18, 24, 47, 99, 99, 99, 99,
    18, 21, 26, 66, 99, 99, 99, 99,
    24, 26, 56, 99, 99, 99, 99, 99,
    47, 66, 99, 99, 99, 99, 99, 99,
    99, 99, 99, 99, 99, 99, 99, 99,
    99, 99, 99, 99, 99, 99, 99, 99,
    99, 99, 99, 99, 99, 99, 99, 99,
    99, 99, 99, 99, 99, 99, 99, 99,
)


def _build_dct_basis() -> tuple[tuple[float, ...], ...]:
    basis = []
    for frequency in range(8):
        normalization = 0.5 / math.sqrt(2) if frequency == 0 else 0.5
        basis.append(
            tuple(
                normalization * math.cos((2 * position + 1) * frequency * math.pi / 16)
                for position in range(8)
            )
        )
    return tuple(basis)


_DCT_BASIS = _build_dct_basis()


def forward_dct_8x8(samples: Sequence[int]) -> tuple[float, ...]:
    """Apply a level-shifted orthonormal 8x8 forward DCT in row-major order."""
    if len(samples) != 64 or any(
        not isinstance(sample, int) or isinstance(sample, bool) or not 0 <= sample <= 255
        for sample in samples
    ):
        raise ValueError("JPEG FDCT requires exactly 64 8-bit samples")

    horizontal = [0.0] * 64
    for row in range(8):
        for horizontal_frequency in range(8):
            total = 0.0
            for column in range(8):
                sample = samples[row * 8 + column] - 128
                total += sample * _DCT_BASIS[horizontal_frequency][column]
            horizontal[row * 8 + horizontal_frequency] = total

    coefficients = [0.0] * 64
    for vertical_frequency in range(8):
        for horizontal_frequency in range(8):
            total = 0.0
            for row in range(8):
                total += horizontal[row * 8 + horizontal_frequency] * _DCT_BASIS[vertical_frequency][row]
            coefficients[vertical_frequency * 8 + horizontal_frequency] = total
    return tuple(coefficients)


def quantize_block(
    coefficients: Sequence[float], quantization: Sequence[int]
) -> tuple[int, ...]:
    if len(coefficients) != 64 or len(quantization) != 64:
        raise JPEGEncoderInvalidQuantizationTableError("JPEG quantization requires 64 entries")

    quantized = []
    for coefficient, quantizer in zip(coefficients, quantization):
        if (
            not isinstance(quantizer, int)
            or isinstance(quantizer, bool)
            or not 1 <= quantizer <= 255
        ):
            raise JPEGEncoderInvalidQuantizationTableError(
                "JPEG quantization entries must be between 1 and 255"
            )
        if not isinstance(coefficient, (int, float)) or isinstance(coefficient, bool):
            raise JPEGEncoderCoefficientOutOfRangeError("JPEG coefficient is not numeric")
        value = float(coefficient) / quantizer
        if not math.isfinite(value):
            raise JPEGEncoderCoefficientOutOfRangeError("JPEG coefficient is not finite")
        rounded = math.floor(value + 0.5) if value >= 0 else math.ceil(value - 0.5)
        if not -32768 <= rounded <= 32767:
            raise JPEGEncoderCoefficientOutOfRangeError("JPEG quantized coefficient is out of range")
        quantized.append(rounded)
    return tuple(quantized)


def zigzag(raster: Sequence[int]) -> tuple[int, ...]:
    if len(raster) != 64:
        raise ValueError("JPEG zigzag ordering requires exactly 64 coefficients")
    return tuple(raster[index] for index in _ZIGZAG_INDICES)


def quantization_tables_for_quality(quality: int) -> tuple[tuple[int, ...], tuple[int, ...]]:
    if not isinstance(quality, int) or isinstance(quality, bool) or not 1 <= quality <= 100:
        raise ValueError("JPEG quality must be between 1 and 100")
    scale = 200 - 2 * quality if quality >= 50 else 5000 // quality
    return (
        _scale_quantization_table(_BASE_LUMA_QUANTIZATION, scale),
        _scale_quantization_table(_BASE_CHROMA_QUANTIZATION, scale),
    )


def _scale_quantization_table(base: Sequence[int], scale: int) -> tuple[int, ...]:
    return tuple(min(255, max(1, (value * scale + 50) // 100)) for value in base)