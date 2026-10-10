"""JPEG entropy bit packing and byte stuffing."""

from typing import Iterable

from .jpegentropy import JPEGCodeword


class JPEGEncoderBitCountError(ValueError):
    """Raised when a JPEG codeword value does not fit its bit width."""


class JPEGEncoderBitWriterFinishedError(ValueError):
    """Raised when writing is attempted after the entropy stream is finalized."""


class JPEGBitWriter:
    def __init__(self) -> None:
        self._data = bytearray()
        self._current = 0
        self._bit_count = 0
        self._finished = False

    def write_bits(self, value: int, bit_count: int) -> None:
        if self._finished:
            raise JPEGEncoderBitWriterFinishedError("JPEG bit writer is already finished")
        if (
            not isinstance(value, int)
            or isinstance(value, bool)
            or not isinstance(bit_count, int)
            or isinstance(bit_count, bool)
            or bit_count < 1
            or bit_count > 16
            or value < 0
            or value >> bit_count
        ):
            raise JPEGEncoderBitCountError("JPEG codeword bit count must be between 1 and 16")

        for bit in range(bit_count - 1, -1, -1):
            self._current = (self._current << 1) | ((value >> bit) & 1)
            self._bit_count += 1
            if self._bit_count == 8:
                self._flush_byte()

    def write_codewords(self, codewords: Iterable[JPEGCodeword]) -> None:
        for codeword in codewords:
            self.write_bits(codeword.value, codeword.bit_count)

    def finish(self) -> bytes:
        if not self._finished:
            if self._bit_count:
                while self._bit_count < 8:
                    self._current = (self._current << 1) | 1
                    self._bit_count += 1
                self._flush_byte()
            self._finished = True
        return bytes(self._data)

    def _flush_byte(self) -> None:
        self._data.append(self._current)
        if self._current == 0xFF:
            self._data.append(0)
        self._current = 0
        self._bit_count = 0