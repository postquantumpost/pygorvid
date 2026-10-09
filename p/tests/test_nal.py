import pytest

from pygorvid import NALUnitError, ebsp_to_rbsp, parse_nal_header


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