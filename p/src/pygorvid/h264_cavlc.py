"""H.264 CAVLC residual syntax from clauses 7.3.5.3.2 and 9.2."""

from copy import copy
from dataclasses import dataclass
from functools import lru_cache

from .bitreader import BitReader, BitstreamError
from .h264_cavlc_tables import COEFF_TOKEN, TOTAL_ZEROS, CHROMA_ZEROS_4, CHROMA_ZEROS_8, RUN_BEFORE


class CAVLCError(BitstreamError):
    pass


@dataclass(frozen=True)
class CAVLCResidualBlock:
    coefficients: tuple[int, ...]
    total_coeff: int


class _VLC:
    def __init__(self):
        self.nodes = [[None, None, None]]

    def insert(self, code, value):
        index = 0
        for bit in code:
            branch = int(bit)
            next_index = self.nodes[index][branch]
            if next_index is None:
                next_index = len(self.nodes)
                self.nodes.append([None, None, None])
                self.nodes[index][branch] = next_index
            index = next_index
        if self.nodes[index][2] is not None:
            raise AssertionError("duplicate CAVLC codeword")
        self.nodes[index][2] = value

    def read(self, reader):
        index = 0
        while self.nodes[index][2] is None:
            index = self.nodes[index][reader.read_bits(1)]
            if index is None:
                raise CAVLCError("invalid CAVLC codeword")
        return self.nodes[index][2]


@lru_cache(maxsize=1)
def _tables():
    tokens = [_VLC() for _ in range(5)]
    for row in COEFF_TOKEN.splitlines():
        total, trailing, *codes = row.split()
        for table, code in zip(tokens, codes):
            if code != "-":
                table.insert(code, int(total) * 4 + int(trailing))

    def make_rows(rows):
        result = []
        for row in rows:
            table = _VLC()
            for value, code in enumerate(row.split()):
                table.insert(code, value)
            result.append(table)
        return result

    return tokens, make_rows(TOTAL_ZEROS), make_rows(CHROMA_ZEROS_4), make_rows(CHROMA_ZEROS_8), make_rows(RUN_BEFORE)


def decode_cavlc_residual_block(reader: BitReader, nc: int, max_coeff: int) -> CAVLCResidualBlock:
    valid = ((nc, max_coeff) in ((-1, 4), (-2, 8))
             or 0 <= nc <= 16 and max_coeff in (15, 16))
    if not valid:
        raise CAVLCError("invalid CAVLC block context or coefficient count")
    trial = copy(reader)
    try:
        block = _decode_block(trial, nc, max_coeff)
    except BitstreamError as error:
        raise CAVLCError(str(error)) from error
    reader._bit_offset = trial.bit_offset
    return block


def _decode_block(reader, nc, max_coeff):
    tokens, zeros, chroma4, chroma8, runs = _tables()
    if nc >= 8:
        bits = reader.read_bits(6)
        token = 0 if bits == 3 else (bits // 4 + 1) * 4 + bits % 4
    else:
        column = 4 if nc == -2 else 3 if nc == -1 else 0 if nc < 2 else 1 if nc < 4 else 2
        token = tokens[column].read(reader)
    total, trailing = divmod(token, 4)
    if total > max_coeff or trailing > total:
        raise CAVLCError("CAVLC coeff_token exceeds block capacity")
    coefficients = [0] * max_coeff
    if total == 0:
        return CAVLCResidualBlock(tuple(coefficients), 0)
    levels = [(-1 if reader.read_bit() else 1) for _ in range(trailing)]
    suffix_length = int(total > 10 and trailing < 3)
    for index in range(trailing, total):
        prefix = 0
        while not reader.read_bit():
            prefix += 1
            if prefix > 31:
                raise CAVLCError("CAVLC level prefix exceeds supported range")
        suffix_size = 4 if prefix == 14 and suffix_length == 0 else prefix - 3 if prefix >= 15 else suffix_length
        code = (min(prefix, 15) << suffix_length) + reader.read_bits(suffix_size)
        if prefix >= 15 and suffix_length == 0:
            code += 15
        if prefix >= 16:
            code += (1 << (prefix - 3)) - 4096
        if index == trailing and trailing < 3:
            code += 2
        level = (code + 2) // 2 if code % 2 == 0 else -(code + 1) // 2
        if not -(1 << 31) <= level < 1 << 31:
            raise CAVLCError("CAVLC coefficient exceeds int32")
        levels.append(level)
        suffix_length = max(suffix_length, 1)
        if suffix_length < 6 and abs(level) > 3 << (suffix_length - 1):
            suffix_length += 1
    zeros_left = 0
    if total < max_coeff:
        zero_tables = chroma4 if max_coeff == 4 else chroma8 if max_coeff == 8 else zeros
        zeros_left = zero_tables[total - 1].read(reader)
    if zeros_left > max_coeff - total:
        raise CAVLCError("CAVLC total_zeros exceeds block capacity")
    run_values = [0] * total
    for index in range(total - 1):
        if zeros_left:
            run_values[index] = runs[min(zeros_left, 7) - 1].read(reader)
            if run_values[index] > zeros_left:
                raise CAVLCError("CAVLC run_before exceeds zeros left")
            zeros_left -= run_values[index]
    run_values[-1] = zeros_left
    position = 0
    for index in reversed(range(total)):
        position += run_values[index]
        coefficients[position] = levels[index]
        position += 1
    return CAVLCResidualBlock(tuple(coefficients), total)