"""Bounded bit-level reads for H.264 syntax parsing."""


class BitstreamError(ValueError):
    """Raised when a bitstream read is invalid or incomplete."""


class BitReader:
    """Read network-order bits from a byte buffer without copying it."""

    def __init__(self, data: bytes):
        self._data = memoryview(data)
        self._bit_offset = 0

    def read_bits(self, count: int) -> int:
        if count < 0 or count > 32:
            raise BitstreamError("bit read width must be between 0 and 32")
        if count > len(self._data) * 8 - self._bit_offset:
            raise BitstreamError("truncated bitstream")
        value = 0
        for _ in range(count):
            byte_index = self._bit_offset // 8
            shift = 7 - self._bit_offset % 8
            value = (value << 1) | ((self._data[byte_index] >> shift) & 1)
            self._bit_offset += 1
        return value

    def read_bit(self) -> bool:
        return bool(self.read_bits(1))

    def align_to_byte(self) -> None:
        remainder = self._bit_offset % 8
        if remainder:
            self._bit_offset += 8 - remainder

    def more_rbsp_data(self) -> bool:
        bit_count = len(self._data) * 8
        if self._bit_offset >= bit_count:
            return False

        def read_at(offset: int) -> bool:
            return bool((self._data[offset // 8] >> (7 - offset % 8)) & 1)

        if not read_at(self._bit_offset):
            return True
        return any(read_at(offset) for offset in range(self._bit_offset + 1, bit_count))

    def read_ue(self) -> int:
        start = self._bit_offset
        leading_zeros = 0
        try:
            while not self.read_bit():
                leading_zeros += 1
                if leading_zeros > 32:
                    raise BitstreamError("Exp-Golomb value exceeds uint32")
            suffix = self.read_bits(leading_zeros)
        except BitstreamError:
            self._bit_offset = start
            raise
        value = (1 << leading_zeros) - 1 + suffix
        if value > 0xFFFFFFFF:
            self._bit_offset = start
            raise BitstreamError("Exp-Golomb value exceeds uint32")
        return value

    def read_se(self) -> int:
        code_number = self.read_ue()
        magnitude = (code_number + 1) // 2
        return magnitude if code_number & 1 else -magnitude