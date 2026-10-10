"""Baseline JPEG Huffman and coefficient-symbol coding primitives."""

from dataclasses import dataclass
from typing import Sequence

from .jpegtransform import JPEGEncoderCoefficientOutOfRangeError


class JPEGEncoderInvalidHuffmanTableError(ValueError):
    """Raised when JPEG canonical Huffman table data is invalid."""


class JPEGEncoderMissingHuffmanSymbolError(ValueError):
    """Raised when a Huffman table lacks a required symbol."""


@dataclass(frozen=True)
class JPEGCodeword:
    value: int
    bit_count: int


@dataclass(frozen=True)
class JPEGHuffmanTable:
    codes: dict[int, JPEGCodeword]

    def code_for(self, symbol: int) -> JPEGCodeword:
        try:
            return self.codes[symbol]
        except KeyError as error:
            raise JPEGEncoderMissingHuffmanSymbolError(
                f"JPEG Huffman table does not contain symbol {symbol:#04x}"
            ) from error


@dataclass(frozen=True)
class JPEGHuffmanSpec:
    table_class: int
    table_id: int
    counts: tuple[int, ...]
    symbols: tuple[int, ...]


def new_huffman_table(counts: Sequence[int], symbols: Sequence[int]) -> JPEGHuffmanTable:
    if len(counts) != 16 or any(
        not isinstance(count, int) or isinstance(count, bool) or not 0 <= count <= 255
        for count in counts
    ):
        raise JPEGEncoderInvalidHuffmanTableError("JPEG Huffman tables require 16 byte counts")
    if any(
        not isinstance(symbol, int) or isinstance(symbol, bool) or not 0 <= symbol <= 255
        for symbol in symbols
    ):
        raise JPEGEncoderInvalidHuffmanTableError("JPEG Huffman symbols must be bytes")

    total = sum(counts)
    if total == 0 or total != len(symbols) or total > 256:
        raise JPEGEncoderInvalidHuffmanTableError("JPEG Huffman symbol count does not match its lengths")

    codes: dict[int, JPEGCodeword] = {}
    code = 0
    symbol_index = 0
    for length, count in enumerate(counts, start=1):
        limit = 1 << length
        if code + count > limit:
            raise JPEGEncoderInvalidHuffmanTableError("JPEG Huffman tree is oversubscribed")
        for _ in range(count):
            if code == limit - 1:
                raise JPEGEncoderInvalidHuffmanTableError("JPEG Huffman tree assigns an all-ones code")
            symbol = symbols[symbol_index]
            if symbol in codes:
                raise JPEGEncoderInvalidHuffmanTableError("JPEG Huffman table has duplicate symbols")
            codes[symbol] = JPEGCodeword(code, length)
            code += 1
            symbol_index += 1
        code <<= 1
    return JPEGHuffmanTable(codes)


def encode_dc_coefficient(current: int, previous: int, table: JPEGHuffmanTable) -> tuple[JPEGCodeword, ...]:
    if not _is_signed16(current) or not _is_signed16(previous):
        raise JPEGEncoderCoefficientOutOfRangeError("JPEG DC coefficient is out of range")
    difference = current - previous
    category = magnitude_category(difference)
    if category > 11:
        raise JPEGEncoderCoefficientOutOfRangeError("JPEG DC difference exceeds baseline category 11")
    codewords = [table.code_for(category)]
    if category:
        codewords.append(amplitude_codeword(difference, category))
    return tuple(codewords)


def encode_ac_block(zigzagged: Sequence[int], table: JPEGHuffmanTable) -> tuple[JPEGCodeword, ...]:
    if len(zigzagged) != 64 or any(not _is_signed16(value) for value in zigzagged):
        raise JPEGEncoderCoefficientOutOfRangeError("JPEG AC block requires 64 signed 16-bit coefficients")

    codewords: list[JPEGCodeword] = []
    zero_run = 0
    for coefficient in zigzagged[1:]:
        if coefficient == 0:
            zero_run += 1
            continue

        category = magnitude_category(coefficient)
        if category > 10:
            raise JPEGEncoderCoefficientOutOfRangeError("JPEG AC coefficient exceeds baseline category 10")
        while zero_run >= 16:
            codewords.append(table.code_for(0xF0))
            zero_run -= 16
        symbol = (zero_run << 4) | category
        codewords.append(table.code_for(symbol))
        codewords.append(amplitude_codeword(coefficient, category))
        zero_run = 0

    if zero_run:
        codewords.append(table.code_for(0x00))
    return tuple(codewords)


def magnitude_category(value: int) -> int:
    return abs(value).bit_length()


def amplitude_codeword(value: int, category: int) -> JPEGCodeword:
    if not 0 <= category <= 16 or magnitude_category(value) != category:
        raise JPEGEncoderCoefficientOutOfRangeError("JPEG amplitude category does not match value")
    if category == 0:
        return JPEGCodeword(0, 0)
    encoded = value if value >= 0 else value + (1 << category) - 1
    return JPEGCodeword(encoded, category)


def default_huffman_specs() -> tuple[JPEGHuffmanSpec, ...]:
    dc_counts = (0, 0, 0, 12) + (0,) * 12
    ac_counts = (0,) * 7 + (162,) + (0,) * 8
    dc_symbols = tuple(range(12))
    ac_symbols = (0x00, 0xF0) + tuple(
        (run << 4) | size for run in range(16) for size in range(1, 11)
    )
    specs = []
    for table_id in range(2):
        specs.extend((
            JPEGHuffmanSpec(0, table_id, dc_counts, dc_symbols),
            JPEGHuffmanSpec(1, table_id, ac_counts, ac_symbols),
        ))
    return tuple(specs)


def _is_signed16(value: int) -> bool:
    return isinstance(value, int) and not isinstance(value, bool) and -32768 <= value <= 32767