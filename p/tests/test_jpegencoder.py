import io
import math

import pytest

pytest.importorskip("PIL")
from PIL import Image

from pygorvid import (
    DEFAULT_JPEG_QUALITY,
    JPEGEncoder,
    JPEGEncoderInputError,
    JPEGEncoderInvalidMarkerError,
    JPEGEncoderQualityError,
    JPEGEncoderSegmentTooLargeError,
    JPEGEncoderWriterError,
    PixelBuffer,
    PixelColorRange,
)
from pygorvid.jpegbitwriter import JPEGBitWriter, JPEGEncoderBitCountError, JPEGEncoderBitWriterFinishedError
from pygorvid.jpegencoder import (
    _append_jpeg_frame_headers,
    _jpeg_dht_payload,
    _jpeg_dqt_payload,
    _jpeg_jfif_app0_payload,
    _jpeg_sof0_payload,
    _jpeg_sos_payload,
)
from pygorvid.jpegencoder import _append_jpeg_marker, convert_rgb_to_ycbcr444, jpeg_rgb_to_ycbcr
from pygorvid.jpegtransform import (
    JPEGEncoderCoefficientOutOfRangeError,
    JPEGEncoderInvalidQuantizationTableError,
    forward_dct_8x8,
    quantization_tables_for_quality,
    quantize_block,
    zigzag,
)
from pygorvid.jpegentropy import (
    JPEGEncoderInvalidHuffmanTableError,
    JPEGEncoderMissingHuffmanSymbolError,
    JPEGCodeword,
    default_huffman_specs,
    encode_ac_block,
    encode_dc_coefficient,
    new_huffman_table,
)


def test_jpeg_encoder_default_and_explicit_quality_contract():
    assert JPEGEncoder().quality == DEFAULT_JPEG_QUALITY == 75
    assert JPEGEncoder(quality=90).quality == 90
    assert JPEGEncoder(quality=1).quality == 1
    assert JPEGEncoder(quality=100).quality == 100
    for quality in (0, 101, True):
        with pytest.raises(JPEGEncoderQualityError):
            JPEGEncoder(quality=quality)


def test_jpeg_encoder_emits_baseline_frame_and_validates_rgb_buffer():
    encoder = JPEGEncoder()
    buffer = PixelBuffer(2, 1, 8, 3, PixelColorRange.FULL, b"abcdef\x99\x99")

    encoded = encoder.encode(buffer)
    assert encoded.startswith(bytes((0xFF, 0xD8)))
    assert encoded.endswith(bytes((0xFF, 0xD9)))
    assert bytes((0xFF, 0xC0, 0, 17, 8, 0, 1, 0, 2, 3)) in encoded
    assert encoder.encode_buffer(buffer) == encoded

    with pytest.raises(JPEGEncoderInputError):
        encoder.encode(PixelBuffer(2, 1, 6, 1, PixelColorRange.FULL, b"abcdef"))
    with pytest.raises(JPEGEncoderInputError):
        encoder.encode(b"rgb")


def test_jpeg_encoder_write_rejects_missing_writer():
    buffer = PixelBuffer(1, 1, 3, 3, PixelColorRange.FULL, b"rgb")
    with pytest.raises(JPEGEncoderWriterError):
        JPEGEncoder().write(None, buffer)


def test_jpeg_marker_writer_writes_standalone_and_length_bearing_markers():
    encoded = _append_jpeg_marker(b"", 0xD8, b"")
    encoded = _append_jpeg_marker(encoded, 0xE0, b"JFIF\x00\x01\x02")
    encoded = _append_jpeg_marker(encoded, 0xD9, b"")

    assert encoded == bytes(
        (0xFF, 0xD8, 0xFF, 0xE0, 0, 9)
    ) + b"JFIF\x00\x01\x02" + bytes((0xFF, 0xD9))


def test_jpeg_marker_writer_rejects_invalid_marker_payload_and_length():
    with pytest.raises(JPEGEncoderInvalidMarkerError):
        _append_jpeg_marker(b"", 0xD8, b"unexpected")
    with pytest.raises(JPEGEncoderInvalidMarkerError):
        _append_jpeg_marker(b"", 0, b"")
    with pytest.raises(JPEGEncoderSegmentTooLargeError):
        _append_jpeg_marker(b"", 0xE0, bytes(65534))


def test_jpeg_marker_writer_accepts_maximum_segment_payload():
    payload = bytes(65533)
    encoded = _append_jpeg_marker(b"", 0xE1, payload)

    assert len(encoded) == 65537
    assert encoded[:4] == bytes((0xFF, 0xE1, 0xFF, 0xFF))
    assert encoded[4:] == payload


@pytest.mark.parametrize(
    "rgb, expected",
    [
        ((0, 0, 0), (0, 128, 128)),
        ((255, 255, 255), (255, 128, 128)),
        ((255, 0, 0), (76, 85, 255)),
        ((0, 255, 0), (150, 44, 21)),
        ((0, 0, 255), (29, 255, 107)),
    ],
)
def test_jpeg_rgb_to_ycbcr_matches_go_primary_vectors(rgb, expected):
    assert jpeg_rgb_to_ycbcr(*rgb) == expected


def test_jpeg_rgb_to_ycbcr444_honors_stride_and_owns_planar_output():
    buffer = PixelBuffer(
        3,
        2,
        11,
        3,
        PixelColorRange.FULL,
        bytes((
            255, 0, 0, 0, 255, 0, 0, 0, 255, 99, 99,
            0, 0, 0, 255, 255, 255, 128, 128, 128, 88, 88,
        )),
    )

    converted = convert_rgb_to_ycbcr444(buffer)

    assert (converted.width, converted.height) == (3, 2)
    assert converted.y == bytes((76, 150, 29, 0, 255, 128))
    assert converted.cb == bytes((85, 44, 255, 128, 128, 128))
    assert converted.cr == bytes((255, 21, 107, 128, 128, 128))


def test_jpeg_forward_dct_matches_constant_and_impulse_vectors():
    assert forward_dct_8x8(bytes((128,)) * 64) == (0.0,) * 64

    constant = forward_dct_8x8(bytes((129,)) * 64)
    assert abs(constant[0] - 8.0) <= 1e-12
    assert all(abs(value) <= 1e-12 for value in constant[1:])

    impulse_samples = bytearray((128,)) * 64
    impulse_samples[0] = 129
    impulse = forward_dct_8x8(impulse_samples)
    expected = (0.125, 0.1733799806652684, 0.1733799806652684, 0.24048494156391084)
    for index, value in zip((0, 1, 8, 9), expected):
        assert abs(impulse[index] - value) <= 1e-12


def test_jpeg_quantize_block_rounds_signed_values_away_from_zero():
    coefficients = [0.0] * 64
    coefficients[0] = 7.0
    coefficients[1] = -7.0
    coefficients[2] = 8.49
    quantization = [2] * 64

    result = quantize_block(coefficients, quantization)

    assert result[:3] == (4, -4, 4)
    quantization[3] = 0
    with pytest.raises(JPEGEncoderInvalidQuantizationTableError):
        quantize_block(coefficients, quantization)
    quantization[3] = 2
    coefficients[4] = math.inf
    with pytest.raises(JPEGEncoderCoefficientOutOfRangeError):
        quantize_block(coefficients, quantization)


def test_jpeg_zigzag_orders_raster_coefficients():
    raster = tuple(range(64))
    expected_indices = (
        0, 1, 8, 16, 9, 2, 3, 10,
        17, 24, 32, 25, 18, 11, 4, 5,
        12, 19, 26, 33, 40, 48, 41, 34,
        27, 20, 13, 6, 7, 14, 21, 28,
        35, 42, 49, 56, 57, 50, 43, 36,
        29, 22, 15, 23, 30, 37, 44, 51,
        58, 59, 52, 45, 38, 31, 39, 46,
        53, 60, 61, 54, 47, 55, 62, 63,
    )

    assert zigzag(raster) == tuple(raster[index] for index in expected_indices)


def test_jpeg_quality_scaled_quantization_tables():
    luma_50, chroma_50 = quantization_tables_for_quality(50)
    luma_75, chroma_75 = quantization_tables_for_quality(75)
    luma_100, chroma_100 = quantization_tables_for_quality(100)

    assert luma_50[:4] == (16, 11, 10, 16)
    assert chroma_50[:4] == (17, 18, 24, 47)
    assert luma_75[:4] == (8, 6, 5, 8)
    assert chroma_75[:4] == (9, 9, 12, 24)
    assert set(luma_100) == {1}
    assert set(chroma_100) == {1}


def test_jpeg_huffman_table_builds_canonical_codes_from_local_dht_vector():
    counts = [0] * 16
    for length, count in ((1, 1), (2, 3), (3, 3), (4, 2), (5, 7), (6, 1)):
        counts[length] = count
    symbols = (4, 3, 20, 1, 19, 5, 36, 2, 6, 52, 51, 0, 21, 134, 7, 180, 35)

    table = new_huffman_table(counts, symbols)

    assert table.code_for(4) == JPEGCodeword(0, 2)
    assert table.code_for(3) == JPEGCodeword(2, 3)


@pytest.mark.parametrize(
    "counts, symbols",
    [
        ((3,) + (0,) * 15, (0, 1, 2)),
        ((2,) + (0,) * 15, (0, 1)),
        ((1,) + (0,) * 15, ()),
    ],
)
def test_jpeg_huffman_table_rejects_invalid_trees(counts, symbols):
    with pytest.raises(JPEGEncoderInvalidHuffmanTableError):
        new_huffman_table(counts, symbols)


def test_jpeg_encode_dc_difference_and_signed_amplitude_vectors():
    counts = [0] * 16
    counts[2] = 4
    table = new_huffman_table(counts, (0, 1, 2, 3))

    assert encode_dc_coefficient(-3, 0, table) == (
        JPEGCodeword(2, 3),
        JPEGCodeword(0, 2),
    )
    assert encode_dc_coefficient(7, 7, table) == (JPEGCodeword(0, 3),)


def test_jpeg_encode_ac_zero_run_zrl_amplitude_and_eob_vectors():
    counts = [0] * 16
    counts[2] = 4
    table = new_huffman_table(counts, (0x03, 0xF0, 0x02, 0x00))
    coefficients = [0] * 64
    coefficients[1] = 5
    coefficients[18] = -2

    assert encode_ac_block(coefficients, table) == (
        JPEGCodeword(0, 3), JPEGCodeword(5, 3),
        JPEGCodeword(1, 3),
        JPEGCodeword(2, 3), JPEGCodeword(1, 2),
        JPEGCodeword(3, 3),
    )


def test_jpeg_default_huffman_specs_cover_baseline_symbols():
    specs = default_huffman_specs()

    assert len(specs) == 4
    assert [(spec.table_class, spec.table_id) for spec in specs] == [
        (0, 0), (1, 0), (0, 1), (1, 1)
    ]
    assert len(specs[0].symbols) == 12
    assert len(specs[1].symbols) == 162


def test_jpeg_bit_writer_is_msb_first_pads_ones_and_stuffs_ff():
    writer = JPEGBitWriter()
    writer.write_bits(5, 3)
    writer.write_bits(255, 8)

    assert writer.finish() == bytes((0xBF, 0xFF, 0x00))
    with pytest.raises(JPEGEncoderBitWriterFinishedError):
        writer.write_bits(0, 1)

    partial = JPEGBitWriter()
    partial.write_bits(5, 3)
    assert partial.finish() == bytes((0xBF,))


def test_jpeg_bit_writer_rejects_invalid_codeword_width_or_value():
    writer = JPEGBitWriter()
    with pytest.raises(JPEGEncoderBitCountError):
        writer.write_bits(0, 0)
    with pytest.raises(JPEGEncoderBitCountError):
        writer.write_bits(8, 3)


def test_jpeg_marker_payloads_match_jfif_baseline_444_layout():
    assert _jpeg_jfif_app0_payload() == b"JFIF\x00\x01\x01\x00\x00\x01\x00\x01\x00\x00"
    assert _jpeg_sof0_payload(0x1234, 0x5678) == bytes(
        (8, 0x56, 0x78, 0x12, 0x34, 3, 1, 0x11, 0, 2, 0x11, 1, 3, 0x11, 1)
    )
    assert _jpeg_sos_payload() == bytes((3, 1, 0, 2, 0x11, 3, 0x11, 0, 63, 0))

    luma, chroma = quantization_tables_for_quality(75)
    dqt = _jpeg_dqt_payload(luma, chroma)
    assert len(dqt) == 130
    assert dqt[0] == 0 and dqt[65] == 1
    dht = _jpeg_dht_payload(default_huffman_specs())
    assert len(dht) == 4 * 17 + 2 * 12 + 2 * 162


def _validate_jpeg_baseline_structure(encoded: bytes, width: int, height: int) -> None:
    assert encoded[:2] == b"\xff\xd8"
    assert encoded[-2:] == b"\xff\xd9"

    offset = 2
    expected_markers = (0xE0, 0xDB, 0xC0, 0xC4, 0xDA)
    saw = {marker: False for marker in expected_markers}
    for marker in expected_markers:
        assert offset + 4 <= len(encoded)
        assert encoded[offset] == 0xFF and encoded[offset + 1] == marker
        length = int.from_bytes(encoded[offset + 2 : offset + 4], "big")
        assert length >= 2
        segment_end = offset + 2 + length
        assert segment_end <= len(encoded)
        payload = encoded[offset + 4 : segment_end]

        if marker == 0xE0:
            saw[marker] = len(payload) == 14 and payload[:5] == b"JFIF\x00"
        elif marker == 0xDB:
            saw[marker] = len(payload) == 130 and payload[0] == 0 and payload[65] == 1
            for index, value in enumerate(payload):
                if index % 65 != 0 and value == 0:
                    raise AssertionError(f"DQT contains zero quantizer at payload index {index}")
        elif marker == 0xC0:
            saw[marker] = (
                len(payload) == 15
                and payload[0] == 8
                and int.from_bytes(payload[1:3], "big") == height
                and int.from_bytes(payload[3:5], "big") == width
                and payload[5] == 3
                and payload[7] == 0x11
                and payload[10] == 0x11
                and payload[13] == 0x11
            )
        elif marker == 0xC4:
            offset_in_payload = 0
            while offset_in_payload < len(payload):
                assert offset_in_payload + 17 <= len(payload)
                selector = payload[offset_in_payload]
                assert selector >> 4 <= 1 and (selector & 0x0F) <= 3
                count = sum(payload[offset_in_payload + 1 : offset_in_payload + 17])
                offset_in_payload += 17
                assert offset_in_payload + count <= len(payload)
                offset_in_payload += count
            saw[marker] = len(payload) > 0
        elif marker == 0xDA:
            saw[marker] = len(payload) == 10 and payload[0] == 3 and payload[7] == 0 and payload[8] == 63 and payload[9] == 0
        offset = segment_end

    assert all(saw.values())
    while offset < len(encoded) - 2:
        if encoded[offset] == 0xFF:
            assert offset + 1 < len(encoded) - 2 and encoded[offset + 1] == 0
            offset += 2
            continue
        offset += 1
    assert offset == len(encoded) - 2


def test_jpeg_frame_header_sequence_and_marker_lengths():
    luma, chroma = quantization_tables_for_quality(75)
    headers = _append_jpeg_frame_headers(2, 1, luma, chroma, default_huffman_specs())

    assert headers.startswith(bytes((0xFF, 0xD8)))
    markers = []
    offset = 2
    while offset < len(headers):
        assert headers[offset] == 0xFF
        marker = headers[offset + 1]
        length = int.from_bytes(headers[offset + 2 : offset + 4], "big")
        assert length >= 2
        segment_end = offset + 2 + length
        assert segment_end <= len(headers)
        markers.append((marker, length))
        offset = segment_end
        if marker == 0xDA:
            break
    assert [marker for marker, _ in markers] == [0xE0, 0xDB, 0xC0, 0xC4, 0xDA]
    assert [length for _, length in markers] == [16, 132, 17, 418, 12]


def test_jpeg_encoder_baseline_structure_and_error_bounds():
    cases = [
        ("single-pixel", 1, 1, False, 24, 8),
        ("tiny-gradient", 2, 2, False, 24, 8),
        ("odd-gradient", 3, 5, False, 24, 8),
        ("block-gradient", 8, 8, False, 24, 8),
        ("high-frequency", 8, 8, True, 64, 20),
    ]

    for name, width, height, high_frequency, max_error, mean_error in cases:
        stride = width * 3 + 2
        pixels = bytearray(stride * height)
        for y in range(height):
            for x in range(width):
                offset = y * stride + x * 3
                if high_frequency:
                    pixels[offset] = (x * 71 + y * 31) & 0xFF
                    pixels[offset + 1] = (x * 13 + y * 83) & 0xFF
                    pixels[offset + 2] = (x * 149 + y * 7) & 0xFF
                else:
                    pixels[offset] = (16 + x * 20 + y * 5) & 0xFF
                    pixels[offset + 1] = (32 + x * 8 + y * 15) & 0xFF
                    pixels[offset + 2] = (64 + x * 10 + y * 9) & 0xFF

        buffer = PixelBuffer(width, height, stride, 3, PixelColorRange.FULL, bytes(pixels))
        encoded = JPEGEncoder().encode(buffer)
        _validate_jpeg_baseline_structure(encoded, width, height)

        decoded = Image.open(io.BytesIO(encoded))
        assert decoded.size == (width, height)

        maximum_error = 0
        total_error = 0
        for y in range(height):
            for x in range(width):
                rgb = decoded.getpixel((x, y))
                for channel, value in enumerate(rgb):
                    source = pixels[y * stride + x * 3 + channel]
                    difference = abs(int(value) - int(source))
                    maximum_error = max(maximum_error, difference)
                    total_error += difference
        mean_error_value = total_error / float(width * height * 3)
        assert maximum_error <= max_error, f"{name}: max error {maximum_error} > {max_error}"
        assert mean_error_value <= mean_error, f"{name}: mean error {mean_error_value} > {mean_error}"
