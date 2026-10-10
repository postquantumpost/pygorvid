"""Native JPEG encoder API and marker-writing primitives."""

import struct
import sys
from dataclasses import dataclass
from typing import BinaryIO, Sequence

from .jpegbitwriter import JPEGBitWriter
from .jpegentropy import (
    JPEGEncoderInvalidHuffmanTableError,
    JPEGHuffmanSpec,
    encode_ac_block,
    encode_dc_coefficient,
    new_huffman_table,
    default_huffman_specs,
)
from .pixelbuffer import PixelBuffer, PixelColorRange
from .jpegtransform import forward_dct_8x8, quantization_tables_for_quality, quantize_block, zigzag


DEFAULT_JPEG_QUALITY = 75


class JPEGEncoderInputError(ValueError):
    """Raised when JPEG input is not a valid RGB pixel buffer."""


class JPEGEncoderDimensionsError(ValueError):
    """Raised when JPEG dimensions are outside the baseline format limits."""


class JPEGEncoderImageTooLargeError(ValueError):
    """Raised when planar JPEG image storage exceeds the addressable size."""


class JPEGEncoderQualityError(ValueError):
    """Raised when JPEG quality is outside the supported range 1..100."""


class JPEGEncoderInvalidMarkerError(ValueError):
    """Raised when a marker code or standalone-marker payload is invalid."""


class JPEGEncoderSegmentTooLargeError(ValueError):
    """Raised when a JPEG marker segment exceeds its 16-bit length limit."""


class JPEGEncoderNotImplementedError(NotImplementedError):
    """Raised until JPEG color conversion and entropy encoding are implemented."""


class JPEGEncoderWriterError(OSError):
    """Raised when the JPEG output writer is missing or fails."""


class JPEGEncoder:
    def __init__(self, quality: int = DEFAULT_JPEG_QUALITY) -> None:
        if not isinstance(quality, int) or isinstance(quality, bool) or not 1 <= quality <= 100:
            raise JPEGEncoderQualityError("JPEG quality must be between 1 and 100")
        self.quality = quality

    def encode(self, buffer: PixelBuffer) -> bytes:
        if not isinstance(buffer, PixelBuffer):
            raise JPEGEncoderInputError("JPEG encoder requires a PixelBuffer")
        _validate_jpeg_pixel_buffer(buffer)
        luma_quantization, chroma_quantization = quantization_tables_for_quality(self.quality)
        huffman_specs = default_huffman_specs()
        image = convert_rgb_to_ycbcr444(buffer)
        output = _append_jpeg_frame_headers(
            image.width,
            image.height,
            luma_quantization,
            chroma_quantization,
            huffman_specs,
        )
        output += _jpeg_encode_scan(image, luma_quantization, chroma_quantization, huffman_specs)
        return _append_jpeg_marker(output, 0xD9, b"")

    def encode_buffer(self, buffer: PixelBuffer) -> bytes:
        return self.encode(buffer)

    def write(self, writer: BinaryIO, buffer: PixelBuffer) -> None:
        if writer is None or not callable(getattr(writer, "write", None)):
            raise JPEGEncoderWriterError("JPEG encoder requires a valid writer")
        payload = self.encode(buffer)
        try:
            written = writer.write(payload)
        except OSError as error:
            raise JPEGEncoderWriterError(f"JPEG encoder write failed: {error}") from error
        if written is not None and written != len(payload):
            raise JPEGEncoderWriterError(
                f"JPEG encoder wrote {written} of {len(payload)} output bytes"
            )


def _append_jpeg_marker(output: bytes, marker: int, payload: bytes) -> bytes:
    if not isinstance(marker, int) or isinstance(marker, bool) or not 0 <= marker <= 0xFF:
        raise JPEGEncoderInvalidMarkerError("JPEG marker code must be one byte")
    if marker in (0, 0xFF):
        raise JPEGEncoderInvalidMarkerError("JPEG marker code is invalid")
    if not isinstance(payload, bytes):
        raise TypeError("JPEG marker payload must be bytes")

    if _is_jpeg_standalone_marker(marker):
        if payload:
            raise JPEGEncoderInvalidMarkerError("standalone JPEG markers cannot have payloads")
        return output + bytes((0xFF, marker))

    if len(payload) > 0xFFFF - 2:
        raise JPEGEncoderSegmentTooLargeError("JPEG marker segment exceeds the format limit")
    segment_length = len(payload) + 2
    return output + bytes((0xFF, marker, segment_length >> 8, segment_length & 0xFF)) + payload


def _is_jpeg_standalone_marker(marker: int) -> bool:
    return marker in (0x01, 0xD8, 0xD9) or 0xD0 <= marker <= 0xD7


def _jpeg_jfif_app0_payload() -> bytes:
    return b"JFIF\x00\x01\x01\x00\x00\x01\x00\x01\x00\x00"


def _jpeg_sof0_payload(width: int, height: int) -> bytes:
    if (
        not isinstance(width, int)
        or isinstance(width, bool)
        or not isinstance(height, int)
        or isinstance(height, bool)
        or not 1 <= width <= 0xFFFF
        or not 1 <= height <= 0xFFFF
    ):
        raise JPEGEncoderDimensionsError("JPEG dimensions must be between 1 and 65535")
    return struct.pack(">BHHB", 8, height, width, 3) + bytes((
        1, 0x11, 0,
        2, 0x11, 1,
        3, 0x11, 1,
    ))


def _jpeg_sos_payload() -> bytes:
    return bytes((3, 1, 0, 2, 0x11, 3, 0x11, 0, 63, 0))


def _jpeg_dqt_payload(luma: tuple[int, ...], chroma: tuple[int, ...]) -> bytes:
    if len(luma) != 64 or len(chroma) != 64:
        raise JPEGEncoderInputError("JPEG quantization tables must contain 64 entries")
    payload = bytearray()
    for table_id, table in enumerate((luma, chroma)):
        if any(not isinstance(value, int) or isinstance(value, bool) or not 1 <= value <= 255 for value in table):
            raise JPEGEncoderInputError("JPEG quantization entries must be in the range 1..255")
        payload.append(table_id)
        payload.extend(zigzag(table))
    return bytes(payload)


def _jpeg_dht_payload(specs) -> bytes:
    payload = bytearray()
    for spec in specs:
        if spec.table_class not in (0, 1) or not 0 <= spec.table_id <= 3:
            raise JPEGEncoderInvalidHuffmanTableError("JPEG Huffman table selector is invalid")
        new_huffman_table(spec.counts, spec.symbols)
        if len(spec.symbols) > 255 or len(payload) + 17 + len(spec.symbols) > 0xFFFF - 2:
            raise JPEGEncoderSegmentTooLargeError("JPEG DHT segment exceeds the format limit")
        payload.append((spec.table_class << 4) | spec.table_id)
        payload.extend(spec.counts)
        payload.extend(spec.symbols)
    return bytes(payload)


def _append_jpeg_frame_headers(
    width: int,
    height: int,
    luma_quantization: tuple[int, ...],
    chroma_quantization: tuple[int, ...],
    huffman_specs,
) -> bytes:
    output = _append_jpeg_marker(b"", 0xD8, b"")
    output = _append_jpeg_marker(output, 0xE0, _jpeg_jfif_app0_payload())
    output = _append_jpeg_marker(
        output, 0xDB, _jpeg_dqt_payload(luma_quantization, chroma_quantization)
    )
    output = _append_jpeg_marker(output, 0xC0, _jpeg_sof0_payload(width, height))
    output = _append_jpeg_marker(output, 0xC4, _jpeg_dht_payload(huffman_specs))
    return _append_jpeg_marker(output, 0xDA, _jpeg_sos_payload())


@dataclass(frozen=True)
class JPEGYCbCr444:
    width: int
    height: int
    y: bytes
    cb: bytes
    cr: bytes


def convert_rgb_to_ycbcr444(buffer: PixelBuffer) -> JPEGYCbCr444:
    _validate_jpeg_pixel_buffer(buffer)
    pixel_count = buffer.width * buffer.height
    y_plane = bytearray(pixel_count)
    cb_plane = bytearray(pixel_count)
    cr_plane = bytearray(pixel_count)
    for row in range(buffer.height):
        source_row = row * buffer.stride
        destination_row = row * buffer.width
        for column in range(buffer.width):
            source = source_row + column * 3
            destination = destination_row + column
            y_value, cb_value, cr_value = jpeg_rgb_to_ycbcr(
                buffer.data[source], buffer.data[source + 1], buffer.data[source + 2]
            )
            y_plane[destination] = y_value
            cb_plane[destination] = cb_value
            cr_plane[destination] = cr_value
    return JPEGYCbCr444(
        buffer.width,
        buffer.height,
        bytes(y_plane),
        bytes(cb_plane),
        bytes(cr_plane),
    )


def jpeg_rgb_to_ycbcr(red: int, green: int, blue: int) -> tuple[int, int, int]:
    channels = (red, green, blue)
    if any(not isinstance(value, int) or isinstance(value, bool) or not 0 <= value <= 255 for value in channels):
        raise JPEGEncoderInputError("RGB samples must be 8-bit integers")
    y_value = (19595 * red + 38470 * green + 7471 * blue + 32768) >> 16
    cb_value = (-11059 * red - 21709 * green + 32768 * blue + (128 << 16) + 32768) >> 16
    cr_value = (32768 * red - 27439 * green - 5329 * blue + (128 << 16) + 32768) >> 16
    return _clamp_jpeg_byte(y_value), _clamp_jpeg_byte(cb_value), _clamp_jpeg_byte(cr_value)


def _validate_jpeg_pixel_buffer(buffer: PixelBuffer) -> None:
    if buffer.width <= 0 or buffer.height <= 0 or buffer.channels != 3:
        raise JPEGEncoderInputError("JPEG encoder requires a valid RGB pixel buffer")
    if buffer.width > 0xFFFF or buffer.height > 0xFFFF:
        raise JPEGEncoderDimensionsError("JPEG dimensions must be between 1 and 65535")
    if buffer.color_range not in (PixelColorRange.LIMITED, PixelColorRange.FULL):
        raise JPEGEncoderInputError("JPEG pixel-buffer color range is invalid")
    if buffer.stride < buffer.width * 3 or len(buffer.data) < buffer.stride * buffer.height:
        raise JPEGEncoderInputError("JPEG pixel-buffer layout is invalid")
    if buffer.width > sys.maxsize // buffer.height:
        raise JPEGEncoderImageTooLargeError("JPEG image exceeds the addressable size limit")


def _clamp_jpeg_byte(value: int) -> int:
    return min(255, max(0, value))


def _jpeg_encode_scan(
    image: JPEGYCbCr444,
    luma_quantization: Sequence[int],
    chroma_quantization: Sequence[int],
    specs: Sequence[JPEGHuffmanSpec],
) -> bytes:
    dc_tables = {}
    ac_tables = {}
    for spec in specs:
        if spec.table_class not in (0, 1) or not 0 <= spec.table_id <= 3:
            raise JPEGEncoderInvalidHuffmanTableError("JPEG Huffman table selector is invalid")
        table = new_huffman_table(spec.counts, spec.symbols)
        target = dc_tables if spec.table_class == 0 else ac_tables
        target[spec.table_id] = table

    planes = (image.y, image.cb, image.cr)
    quantization = (luma_quantization, chroma_quantization, chroma_quantization)
    previous_dc = [0, 0, 0]
    writer = JPEGBitWriter()
    block_columns = (image.width + 7) // 8
    block_rows = (image.height + 7) // 8
    for block_y in range(block_rows):
        for block_x in range(block_columns):
            for component, plane in enumerate(planes):
                samples = []
                for row in range(8):
                    y = min(block_y * 8 + row, image.height - 1)
                    for column in range(8):
                        x = min(block_x * 8 + column, image.width - 1)
                        samples.append(plane[y * image.width + x])

                coefficients = forward_dct_8x8(samples)
                quantized = quantize_block(coefficients, quantization[component])
                ordered = zigzag(quantized)
                table_id = 0 if component == 0 else 1
                dc_codewords = encode_dc_coefficient(
                    ordered[0], previous_dc[component], dc_tables[table_id]
                )
                ac_codewords = encode_ac_block(ordered, ac_tables[table_id])
                writer.write_codewords(dc_codewords)
                writer.write_codewords(ac_codewords)
                previous_dc[component] = ordered[0]
    return writer.finish()