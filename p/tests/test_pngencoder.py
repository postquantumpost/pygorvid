import io
import binascii
import struct
import zlib

import pytest

from pygorvid import (
    PNGEncoder,
    PNGEncoderChunkTooLargeError,
    PNGEncoderDimensionsError,
    PNGEncoderImageTooLargeError,
    PixelBuffer,
    PixelColorRange,
    PNGEncoderInputError,
    PNGEncoderNotImplementedError,
    PNGEncoderStoredBlockTooLargeError,
    PNGEncoderWriterError,
)
from pygorvid.pngencoder import (
    _adler32,
    _append_png_chunk,
    _frame_png_chunks,
    _png_crc32,
    _zlib_stored_block,
    _zlib_stored_blocks,
)


def test_png_encoder_writes_filter_zero_scanlines_and_ignores_padding():
    encoder = PNGEncoder()
    buffer = PixelBuffer(2, 2, 8, 3, PixelColorRange.FULL, bytes((
        10, 20, 30, 40, 50, 60, 99, 99,
        70, 80, 90, 100, 110, 120, 88, 88,
    )))

    encoded = encoder.encode(buffer)
    idat = _png_idat(encoded)

    assert zlib.decompress(idat) == bytes((
        0, 10, 20, 30, 40, 50, 60,
        0, 70, 80, 90, 100, 110, 120,
    ))
    assert encoder.encode_buffer(buffer) == encoded


def test_png_encoder_round_trips_odd_dimensions_and_stride():
    width, height, stride = 3, 3, 12
    pixels = bytes((
        1, 2, 3, 4, 5, 6, 7, 8, 9, 201, 202, 203,
        10, 11, 12, 13, 14, 15, 16, 17, 18, 204, 205, 206,
        19, 20, 21, 22, 23, 24, 25, 26, 27, 207, 208, 209,
    ))
    buffer = PixelBuffer(width, height, stride, 3, PixelColorRange.FULL, pixels)
    chunks = _read_png_chunks(PNGEncoder().encode(buffer))
    ihdr = chunks[0][1]

    assert struct.unpack_from(">II", ihdr) == (width, height)
    assert ihdr[8:] == bytes((8, 2, 0, 0, 0))
    scanlines = zlib.decompress(b"".join(data for kind, data in chunks if kind == b"IDAT"))
    expected = b"".join(
        b"\x00" + pixels[row * stride : row * stride + width * 3]
        for row in range(height)
    )
    assert scanlines == expected


def test_png_encoder_rejects_non_rgb_input():
    buffer = PixelBuffer(2, 1, 6, 1, PixelColorRange.FULL, b"abcdef")
    with pytest.raises(PNGEncoderInputError):
        PNGEncoder().encode(buffer)


def test_png_encoder_rejects_non_pixel_buffer_input():
    with pytest.raises(PNGEncoderInputError):
        PNGEncoder().encode(b"rgb")


def test_png_encoder_write_rejects_missing_writer_before_encoding():
    buffer = PixelBuffer(1, 1, 3, 3, PixelColorRange.FULL, b"rgb")
    with pytest.raises(PNGEncoderWriterError):
        PNGEncoder().write(None, buffer)


def test_png_encoder_write_writes_encoded_payload():
    buffer = PixelBuffer(1, 1, 3, 3, PixelColorRange.FULL, b"rgb")
    writer = io.BytesIO()

    PNGEncoder().write(writer, buffer)

    assert writer.getvalue() == PNGEncoder().encode(buffer)


def test_png_chunk_framing_signature_ihdr_idat_iend_and_checksums():
    idat = bytes((0x78, 0x01, 0x02, 0x03))
    encoded = _frame_png_chunks(2, 3, idat)

    assert encoded[:8] == bytes((137, 80, 78, 71, 13, 10, 26, 10))
    offset = 8
    chunks = []
    while offset < len(encoded):
        length = struct.unpack_from(">I", encoded, offset)[0]
        chunk_type = encoded[offset + 4 : offset + 8]
        payload_start = offset + 8
        payload_end = payload_start + length
        payload = encoded[payload_start:payload_end]
        checksum = struct.unpack_from(">I", encoded, payload_end)[0]
        assert checksum == binascii.crc32(chunk_type + payload)
        chunks.append((chunk_type, payload))
        offset = payload_end + 4

    assert [chunk_type for chunk_type, _ in chunks] == [b"IHDR", b"IDAT", b"IEND"]
    assert chunks[0][1] == struct.pack(">IIBBBBB", 2, 3, 8, 2, 0, 0, 0)
    assert chunks[1][1] == idat
    assert chunks[2][1] == b""
    assert offset == len(encoded)


def test_png_crc32_matches_standard_check_value():
    assert _png_crc32(b"123456789") == 0xCBF43926


def test_png_chunk_framing_rejects_zero_and_oversized_dimensions():
    with pytest.raises(PNGEncoderDimensionsError):
        _frame_png_chunks(0, 1, b"")
    with pytest.raises(PNGEncoderDimensionsError):
        _frame_png_chunks(0x100000000, 1, b"")


def test_png_chunk_writer_rejects_invalid_chunk_type():
    with pytest.raises(PNGEncoderChunkTooLargeError):
        _append_png_chunk(b"", b"ID", b"")


@pytest.mark.parametrize(
    "payload",
    [b"", bytes((0, 1, 127, 255)), bytes((0xA5,)) * 65535],
)
def test_png_zlib_stored_block_round_trips_and_has_valid_lengths(payload):
    encoded = _zlib_stored_block(payload)

    assert encoded[:3] == bytes((0x78, 0x01, 0x01))
    length, complement = struct.unpack_from("<HH", encoded, 3)
    assert length == len(payload)
    assert complement == (length ^ 0xFFFF)
    assert encoded[7:-4] == payload
    assert struct.unpack_from(">I", encoded, len(encoded) - 4)[0] == _adler32(payload)
    assert zlib.decompress(encoded) == payload


def test_png_zlib_stored_block_rejects_payload_over_65535_bytes():
    with pytest.raises(PNGEncoderStoredBlockTooLargeError):
        _zlib_stored_block(bytes(65536))


def test_png_zlib_stored_blocks_round_trip_across_block_boundaries():
    payload = bytes((index * 31) & 0xFF for index in range(2 * 65535 + 17))
    encoded = _zlib_stored_blocks(payload)

    assert zlib.decompress(encoded) == payload
    offset = 2
    for expected_length in (65535, 65535, 17):
        header = encoded[offset]
        assert header & 0x06 == 0
        assert header & 0x01 == (1 if expected_length == 17 else 0)
        length, complement = struct.unpack_from("<HH", encoded, offset + 1)
        assert length == expected_length
        assert complement == (length ^ 0xFFFF)
        offset += 5 + length
    assert offset + 4 == len(encoded)


def test_png_adler32_matches_standard_vectors():
    assert _adler32(b"") == 0x00000001
    assert _adler32(b"Wikipedia") == 0x11E60398


def test_png_encoder_handles_image_across_stored_block_boundary():
    width, height = 256, 86
    stride = width * 3
    pixels = bytes((index * 13) & 0xFF for index in range(stride * height))
    buffer = PixelBuffer(width, height, stride, 3, PixelColorRange.FULL, pixels)
    encoded = PNGEncoder().encode(buffer)
    scanlines = zlib.decompress(_png_idat(encoded))

    assert len(scanlines) == height * (stride + 1)
    for row in range(height):
        start = row * (stride + 1)
        assert scanlines[start] == 0
        assert scanlines[start + 1 : start + stride + 1] == pixels[row * stride : (row + 1) * stride]


def test_png_encoder_handles_multi_megabyte_output():
    width, height = 1024, 1024
    stride = width * 3
    pixels = bytearray(stride * height)
    for y in range(height):
        for x in range(width):
            offset = y * stride + x * 3
            pixels[offset] = x & 0xFF
            pixels[offset + 1] = y & 0xFF
            pixels[offset + 2] = (x ^ y) & 0xFF
    buffer = PixelBuffer(width, height, stride, 3, PixelColorRange.FULL, pixels)
    encoded = PNGEncoder().encode(buffer)
    chunks = _read_png_chunks(encoded)

    assert len(encoded) > 3 * 1024 * 1024
    ihdr = chunks[0][1]
    assert struct.unpack_from(">II", ihdr) == (width, height)
    scanlines = zlib.decompress(b"".join(data for kind, data in chunks if kind == b"IDAT"))
    assert len(scanlines) == height * (stride + 1)
    for x, y in ((0, 0), (width // 2, height // 2), (width - 1, height - 1)):
        row_offset = y * (stride + 1)
        offset = row_offset + 1 + x * 3
        assert scanlines[row_offset] == 0
        assert scanlines[offset : offset + 3] == bytes((x & 0xFF, y & 0xFF, (x ^ y) & 0xFF))


def _png_idat(encoded):
    return b"".join(payload for chunk_type, payload in _read_png_chunks(encoded) if chunk_type == b"IDAT")


def _read_png_chunks(encoded):
    assert encoded[:8] == bytes((137, 80, 78, 71, 13, 10, 26, 10))
    offset = 8
    chunks = []
    while offset < len(encoded):
        assert offset + 12 <= len(encoded)
        length = struct.unpack_from(">I", encoded, offset)[0]
        chunk_type = encoded[offset + 4 : offset + 8]
        payload_start = offset + 8
        payload_end = payload_start + length
        assert payload_end + 4 <= len(encoded)
        payload = encoded[payload_start:payload_end]
        checksum = struct.unpack_from(">I", encoded, payload_end)[0]
        assert checksum == binascii.crc32(chunk_type + payload)
        chunks.append((chunk_type, payload))
        offset = payload_end + 4
        if chunk_type == b"IEND":
            break
    assert offset == len(encoded)
    assert chunks[0][0] == b"IHDR"
    assert chunks[-1] == (b"IEND", b"")
    assert all(chunk_type in (b"IHDR", b"IDAT", b"IEND") for chunk_type, _ in chunks)
    return chunks