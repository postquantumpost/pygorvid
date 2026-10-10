"""Native PNG encoder API contract."""

import struct
import sys
from typing import BinaryIO

from .pixelbuffer import PixelBuffer, PixelColorRange


class PNGEncoderInputError(ValueError):
    """Raised when PNG input is not a valid RGB pixel buffer."""


class PNGEncoderDimensionsError(ValueError):
    """Raised when image dimensions exceed the PNG format limit."""


class PNGEncoderChunkTooLargeError(ValueError):
    """Raised when a PNG chunk cannot be represented by its 32-bit length."""


class PNGEncoderStoredBlockTooLargeError(ValueError):
    """Raised when zlib input exceeds one uncompressed DEFLATE block."""


class PNGEncoderImageTooLargeError(ValueError):
    """Raised when PNG scanline storage exceeds the addressable size limit."""


class PNGEncoderNotImplementedError(NotImplementedError):
    """Raised until the PNG serialization stages are implemented."""


class PNGEncoderWriterError(OSError):
    """Raised when the PNG output writer is missing or fails."""


PNG_SIGNATURE = bytes((137, 80, 78, 71, 13, 10, 26, 10))


class PNGEncoder:
    def encode(self, buffer: PixelBuffer) -> bytes:
        if not isinstance(buffer, PixelBuffer):
            raise PNGEncoderInputError("PNG encoder requires a PixelBuffer")
        if buffer.width <= 0 or buffer.height <= 0 or buffer.channels != 3:
            raise PNGEncoderInputError("PNG encoder requires a valid RGB pixel buffer")
        if buffer.width > 0xFFFFFFFF or buffer.height > 0xFFFFFFFF:
            raise PNGEncoderDimensionsError("PNG dimensions exceed the format limit")
        if buffer.stride < buffer.width * 3:
            raise PNGEncoderInputError("PNG input stride is too small for RGB24")
        if buffer.color_range not in (PixelColorRange.LIMITED, PixelColorRange.FULL):
            raise PNGEncoderInputError("PNG pixel-buffer color range is invalid")
        if len(buffer.data) < buffer.stride * buffer.height:
            raise PNGEncoderInputError("PNG pixel-buffer data is too short")

        row_bytes = buffer.width * 3
        scanline_bytes = row_bytes + 1
        if scanline_bytes > sys.maxsize or buffer.height > sys.maxsize // scanline_bytes:
            raise PNGEncoderImageTooLargeError("PNG image data exceeds the addressable size limit")

        scanlines = bytearray(buffer.height * scanline_bytes)
        for row in range(buffer.height):
            destination = row * scanline_bytes
            source = row * buffer.stride
            scanlines[destination] = 0
            scanlines[destination + 1 : destination + scanline_bytes] = buffer.data[
                source : source + row_bytes
            ]

        idat = _zlib_stored_blocks(scanlines)
        return _frame_png_chunks(buffer.width, buffer.height, idat)

    def encode_buffer(self, buffer: PixelBuffer) -> bytes:
        return self.encode(buffer)

    def write(self, writer: BinaryIO, buffer: PixelBuffer) -> None:
        if writer is None or not callable(getattr(writer, "write", None)):
            raise PNGEncoderWriterError("PNG encoder requires a valid writer")
        payload = self.encode(buffer)
        try:
            written = writer.write(payload)
        except OSError as error:
            raise PNGEncoderWriterError(f"PNG encoder write failed: {error}") from error
        if written is not None and written != len(payload):
            raise PNGEncoderWriterError(
                f"PNG encoder wrote {written} of {len(payload)} output bytes"
            )


def _frame_png_chunks(width: int, height: int, idat: bytes) -> bytes:
    if (
        not isinstance(width, int)
        or isinstance(width, bool)
        or not isinstance(height, int)
        or isinstance(height, bool)
        or width <= 0
        or height <= 0
        or width > 0xFFFFFFFF
        or height > 0xFFFFFFFF
    ):
        raise PNGEncoderDimensionsError("PNG dimensions exceed the format limit")
    if not isinstance(idat, bytes):
        raise TypeError("PNG IDAT payload must be bytes")

    ihdr = struct.pack(">IIBBBBB", width, height, 8, 2, 0, 0, 0)
    output = PNG_SIGNATURE
    output = _append_png_chunk(output, b"IHDR", ihdr)
    output = _append_png_chunk(output, b"IDAT", idat)
    return _append_png_chunk(output, b"IEND", b"")


def _append_png_chunk(output: bytes, chunk_type: bytes, payload: bytes) -> bytes:
    if not isinstance(chunk_type, bytes) or len(chunk_type) != 4:
        raise PNGEncoderChunkTooLargeError("PNG chunk type must be exactly four bytes")
    if not isinstance(payload, bytes):
        raise TypeError("PNG chunk payload must be bytes")
    if len(payload) > 0xFFFFFFFF:
        raise PNGEncoderChunkTooLargeError("PNG chunk exceeds the format limit")

    checksum = _png_crc32(chunk_type + payload)
    return output + struct.pack(">I", len(payload)) + chunk_type + payload + struct.pack(">I", checksum)


def _png_crc32(data: bytes) -> int:
    checksum = 0xFFFFFFFF
    for value in data:
        checksum ^= value
        for _ in range(8):
            if checksum & 1:
                checksum = (checksum >> 1) ^ 0xEDB88320
            else:
                checksum >>= 1
    return checksum ^ 0xFFFFFFFF


def _adler32(data: bytes) -> int:
    modulus = 65521
    max_block_size = 5552
    a = 1
    b = 0
    offset = 0
    while offset < len(data):
        end = min(offset + max_block_size, len(data))
        for value in data[offset:end]:
            a += value
            b += a
        a %= modulus
        b %= modulus
        offset = end
    return (b << 16) | a


def _zlib_stored_block(data: bytes) -> bytes:
    if not isinstance(data, bytes):
        raise TypeError("zlib input must be bytes")
    if len(data) > 0xFFFF:
        raise PNGEncoderStoredBlockTooLargeError(
            "PNG zlib stored block exceeds 65535 bytes"
        )
    return _zlib_stored_blocks(data)


def _zlib_stored_blocks(data: bytes) -> bytes:
    max_block_size = 0xFFFF
    block_count = max(1, (len(data) + max_block_size - 1) // max_block_size)
    output = bytearray((0x78, 0x01))
    offset = 0
    for block_index in range(block_count):
        block = data[offset : offset + max_block_size]
        output.append(1 if block_index == block_count - 1 else 0)
        length = len(block)
        output.extend(struct.pack("<HH", length, length ^ 0xFFFF))
        output.extend(block)
        offset += length
    output.extend(struct.pack(">I", _adler32(data)))
    return bytes(output)