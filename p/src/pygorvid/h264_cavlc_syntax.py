"""CAVLC macroblock syntax and per-slice nonzero-coefficient contexts."""

from copy import copy

from .bitreader import BitReader
from .h264_cavlc import CAVLCError, decode_cavlc_residual_block

INTRA_CBP = (47,31,15,0,23,27,29,30,7,11,13,14,39,43,45,46,16,3,5,10,12,19,21,26,28,35,37,42,44,1,2,4,8,17,18,20,24,6,9,22,25,32,33,34,36,40,38,41)
INTER_CBP = (0,16,1,2,4,8,32,3,5,10,12,15,47,7,11,13,14,6,9,31,35,37,42,44,33,34,36,40,39,43,45,46,17,18,20,24,19,21,26,28,23,27,29,30,22,25,38,41)
BLOCK_SCAN = (0,1,4,5,2,3,6,7,8,9,12,13,10,11,14,15)


class CAVLCSyntax:
    def __init__(self):
        self.skip_remaining = 0
        self.coded_after_skip = False
        self.begin(None, None)

    def clone(self):
        result = copy(self)
        result.counts = self.counts.copy()
        return result

    def begin(self, left, top):
        self.counts = [0] * 24
        self.left, self.top = left, top
        self.luma_index = self.chroma_index = self.chroma_dc_index = 0
        self.luma_cbp = self.chroma_cbp = 0
        self.intra = False

    def skipped(self, reader):
        if self.skip_remaining:
            self.skip_remaining -= 1
            return True
        if self.coded_after_skip:
            self.coded_after_skip = False
            return False
        run = reader.read_ue()
        if not run:
            return False
        self.skip_remaining, self.coded_after_skip = run - 1, True
        return True

    def finished(self, reader):
        return not self.skip_remaining and not reader.more_rbsp_data()

    def mb_type(self, reader, slice_type):
        base = {2: 0, 0: 5, 1: 23}[slice_type % 5]
        value = reader.read_ue()
        if value > base + 25:
            raise CAVLCError("CAVLC mb_type exceeds slice range")
        self.intra = value >= base
        if self.intra:
            kind = value - base
            if kind == 25:
                self.counts = [16] * 24
            elif kind:
                self.luma_cbp = 15 if kind >= 13 else 0
                self.chroma_cbp = (kind - 1) // 4 % 3
        return value

    def luma_pattern(self, reader):
        code = reader.read_ue()
        if code >= 48:
            raise CAVLCError("CAVLC coded_block_pattern exceeds [0,47]")
        pattern = (INTRA_CBP if self.intra else INTER_CBP)[code]
        self.luma_cbp, self.chroma_cbp = pattern & 15, pattern >> 4
        return self.luma_cbp

    @staticmethod
    def unsigned(reader, maximum):
        value = reader.read_ue()
        if value > maximum:
            raise CAVLCError("CAVLC syntax value exceeds supported range")
        return value

    @staticmethod
    def signed(reader):
        value = reader.read_se()
        if not -(1 << 31) <= value < 1 << 31:
            raise CAVLCError("CAVLC signed value exceeds int32")
        return value

    @staticmethod
    def reference(reader, maximum):
        if maximum == 0:
            return 0
        return int(not reader.read_bit()) if maximum == 1 else CAVLCSyntax.unsigned(reader, maximum)

    @staticmethod
    def prediction_mode(reader, predicted):
        if reader.read_bit():
            return predicted
        remaining = reader.read_bits(3)
        return remaining + int(remaining >= predicted)

    def nc(self, plane, block):
        offset, width = (0, 4) if plane == 0 else (16 + (plane - 1) * 4, 2)
        x, y = block % width, block // width
        left = self.counts[offset + block - 1] if x else None if self.left is None else self.left[offset + y * width + width - 1]
        top = self.counts[offset + block - width] if y else None if self.top is None else self.top[offset + (width - 1) * width + x]
        if left is not None and top is not None:
            return (left + top + 1) // 2
        return left if left is not None else top if top is not None else 0

    def residual(self, reader, category):
        plane, block, capacity, nc = 0, 0, 16, self.nc(0, 0)
        if category in (1, 2):
            while self.luma_index < 16 and not self.luma_cbp & (1 << (self.luma_index // 4)):
                self.luma_index += 1
            if self.luma_index >= 16:
                raise CAVLCError("CAVLC luma residual exceeds coded block pattern")
            block = BLOCK_SCAN[self.luma_index]
            self.luma_index += 1
            capacity, nc = 15 if category == 1 else 16, self.nc(0, block)
        elif category == 3:
            plane, capacity, nc = self.chroma_dc_index + 1, 4, -1
            self.chroma_dc_index += 1
        elif category == 4:
            plane, block, capacity = self.chroma_index // 4 + 1, self.chroma_index % 4, 15
            self.chroma_index += 1
            if plane > 2:
                raise CAVLCError("CAVLC chroma AC plane exceeds 4:2:0")
            nc = self.nc(plane, block)
        elif category != 0:
            raise CAVLCError("unsupported CAVLC residual category")
        if plane > 2:
            raise CAVLCError("CAVLC chroma DC plane exceeds 4:2:0")
        decoded = decode_cavlc_residual_block(reader, nc, capacity)
        if category not in (0, 3):
            offset = 0 if plane == 0 else 16 + (plane - 1) * 4
            self.counts[offset + block] = decoded.total_coeff
        return list(decoded.coefficients) + [0] * (16 - capacity), bool(decoded.total_coeff)

    def residual8x8(self, reader):
        levels = [0] * 64
        for group in range(4):
            block, _ = self.residual(reader, 2)
            levels[group::4] = block
        return levels


def new_cavlc_syntax_decoder(data, bit_offset, timeout_check=None):
    from .h264_cabac import CABACArithmeticDecoder
    decoder = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
    decoder._bits = BitReader(data)
    decoder._bits.read_bits(0)
    for _ in range(bit_offset):
        decoder._bits.read_bit()
    decoder._timeout_check = timeout_check
    decoder._code_range, decoder._code_offset, decoder._terminated = 510, 0, False
    decoder._cavlc = CAVLCSyntax()
    return decoder