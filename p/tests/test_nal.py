from pathlib import Path

import pytest

from pygorvid import (
    NALUnitError, ebsp_to_rbsp, parse_nal_header, parse_pps, parse_slice_header, parse_sps,
    split_h264_sample_nals, split_length_prefixed_nals,
)


@pytest.mark.parametrize(
    ("value", "expected"),
    [(0x65, (3, 5)), (0x41, (2, 1)), (0x06, (0, 6)), (0x1F, (0, 31))],
)
def test_parse_nal_header_fields(value, expected):
    header = parse_nal_header(bytes((value,)))
    assert (header.reference_idc, header.unit_type) == expected


def test_parse_nal_header_rejects_empty_and_forbidden_bit():
    with pytest.raises(NALUnitError, match="empty"):
        parse_nal_header(b"")
    with pytest.raises(NALUnitError, match="forbidden_zero_bit"):
        parse_nal_header(b"\xe5")


@pytest.mark.parametrize("length_size", (1, 2, 4))
def test_split_length_prefixed_nals_uses_configured_prefix_width(length_size):
    nals = (b"\x65\xaa", b"\x41\xbb\xcc")
    sample = b"".join(len(nal).to_bytes(length_size, "big") + nal for nal in nals)
    assert split_length_prefixed_nals(sample, length_size) == nals


@pytest.mark.parametrize(
    ("sample", "length_size", "message"),
    (
        (b"\x00", 2, "truncated NAL length prefix"),
        (b"\x00\x00", 2, "zero length"),
        (b"\x00\x02\x65", 2, "NAL unit is truncated"),
        (b"", 3, "length size"),
    ),
)
def test_split_length_prefixed_nals_rejects_invalid_lengths(sample, length_size, message):
    with pytest.raises(NALUnitError, match=message):
        split_length_prefixed_nals(sample, length_size)


@pytest.mark.parametrize("value", range(4))
def test_ebsp_to_rbsp_removes_valid_escape(value):
    assert ebsp_to_rbsp(bytes((0, 0, 3, value))) == bytes((0, 0, value))


def test_ebsp_to_rbsp_handles_repeated_escapes_and_unescaped_bytes():
    encoded = bytes((0x67, 0, 0, 3, 0, 0, 3, 0, 1, 0x80))
    assert ebsp_to_rbsp(encoded) == bytes((0x67, 0, 0, 0, 0, 0, 1, 0x80))
    assert ebsp_to_rbsp(bytes((0x67, 0x12, 0x34))) == bytes((0x67, 0x12, 0x34))


@pytest.mark.parametrize(
    "encoded",
    [bytes((0, 0, value)) for value in range(3)] + [bytes((0, 0, 3)), bytes((0, 0, 3, 4))],
)
def test_ebsp_to_rbsp_rejects_malformed_sequences(encoded):
    with pytest.raises(NALUnitError, match="emulation-prevention"):
        ebsp_to_rbsp(encoded)


def _sample_bytes(nals, width):
    return b"".join(len(nal).to_bytes(width, "big") + nal for nal in nals)


@pytest.mark.parametrize("width", (1, 2, 4))
@pytest.mark.parametrize("header", (0x41, 0x65))
@pytest.mark.parametrize("marker", (0x0A, 0x0B))
@pytest.mark.parametrize("prefix", (b"\x00\x00\x01", b"\x00\x00\x00\x01"))
def test_split_h264_sample_nals_recovers_end_markers(width, header, marker, prefix):
    slice_nal = bytes((header, 0x80))
    end_nal = bytes((marker,))
    nal = slice_nal + prefix + end_nal
    sample = _sample_bytes((nal,), width)
    with pytest.raises(NALUnitError, match="emulation-prevention"):
        ebsp_to_rbsp(nal[1:])
    assert split_length_prefixed_nals(sample, width) == (nal,)
    assert split_h264_sample_nals(sample, width) == (slice_nal, end_nal)
    assert ebsp_to_rbsp(split_h264_sample_nals(sample, width)[0][1:]) == b"\x80"


@pytest.mark.parametrize("nal", (
    b"\x41\x80",
    b"\x06\x80\x00\x00\x00\x01\x0a",
    b"\x41\x80\x00\x00\x03\x01\x0a",
    b"\x41\x80\x00\x00\x00\x01\x0c",
    b"\x41\x80\x00\x00\x00\x01\x0a\x80",
    b"\x41\x00\x00\x00\x01\x0a",
))
def test_split_h264_sample_nals_preserves_other_payloads(nal):
    assert split_h264_sample_nals(_sample_bytes((nal,), 4), 4) == (nal,)


def test_split_h264_sample_nals_keeps_rejecting_other_malformed_payloads():
    bad = b"\x41\x80\x00\x00\x00\x01\x0a"
    nals = (bad, b"\x41\x80")
    assert split_h264_sample_nals(_sample_bytes(nals, 4), 4) == nals
    with pytest.raises(NALUnitError, match="emulation-prevention"):
        ebsp_to_rbsp(nals[0][1:])
    bad = b"\x41\x00\x00\x02\x80\x00\x00\x00\x01\x0a"
    recovered = split_h264_sample_nals(_sample_bytes((bad,), 4), 4)
    with pytest.raises(NALUnitError, match="emulation-prevention"):
        ebsp_to_rbsp(recovered[0][1:])
    for data, width in ((b"\x00", 2), (b"\x00", 1), (b"\x02\x41", 1), (b"", 3)):
        with pytest.raises(NALUnitError):
            split_h264_sample_nals(data, width)


def test_rannirune_final_slice_framing_recovery():
    path = Path(__file__).resolve().parents[2] / "testdata/h264/rannirune-eos.hex"
    sps_nal, pps_nal, nal = (bytes.fromhex(line) for line in path.read_text().splitlines())
    assert len(nal) == 1092
    sps, pps = parse_sps(sps_nal), parse_pps(pps_nal)
    with pytest.raises(ValueError, match="emulation-prevention"):
        parse_slice_header(nal, sps, pps)
    recovered = split_h264_sample_nals(_sample_bytes((nal,), 4), 4)
    assert recovered == (nal[:1087], b"\x0a")
    assert len(ebsp_to_rbsp(recovered[0][1:])) == 1086
    assert parse_slice_header(recovered[0], sps, pps) == parse_slice_header(nal[:1087], sps, pps)