import pytest

from pygorvid import BitReader, BitstreamError


def pack_bits(bits: str) -> bytes:
    data = bytearray((len(bits) + 7) // 8)
    for index, bit in enumerate(bits):
        if bit == "1":
            data[index // 8] |= 1 << (7 - index % 8)
    return bytes(data)


def test_bounded_reads_and_byte_alignment():
    reader = BitReader(bytes((0b10110011, 0b01101010)))
    assert reader.read_bits(3) == 5
    reader.align_to_byte()
    assert reader.read_bits(8) == 0x6A
    assert reader.read_bits(0) == 0
    with pytest.raises(BitstreamError, match="truncated bitstream"):
        reader.read_bits(1)


def test_invalid_and_truncated_reads_do_not_consume_bits():
    reader = BitReader(b"\xa5")
    with pytest.raises(BitstreamError, match="width"):
        reader.read_bits(33)
    with pytest.raises(BitstreamError, match="truncated bitstream"):
        reader.read_bits(9)
    assert reader.read_bits(8) == 0xA5


def test_more_rbsp_data_distinguishes_syntax_from_trailing_bits():
    reader = BitReader(b"\xb0")
    assert reader.more_rbsp_data()
    assert reader.read_bits(3) == 5
    assert not reader.more_rbsp_data()
    assert not BitReader(b"").more_rbsp_data()


def test_unsigned_exp_golomb_vectors_and_uint32_boundary():
    reader = BitReader(pack_bits("1010011001000010100110"))
    assert [reader.read_ue() for _ in range(6)] == [0, 1, 2, 3, 4, 5]
    maximum = BitReader(pack_bits("0" * 32 + "1" + "0" * 32))
    assert maximum.read_ue() == 0xFFFFFFFF


def test_signed_exp_golomb_vectors():
    reader = BitReader(pack_bits("1010011001000010100110"))
    assert [reader.read_se() for _ in range(6)] == [0, 1, -1, 2, -2, 3]


@pytest.mark.parametrize("bits", ["0", "0" * 33, "0" * 32 + "1" + "0" * 31 + "1"])
def test_exp_golomb_errors_do_not_consume_bits(bits):
    reader = BitReader(pack_bits(bits))
    with pytest.raises(BitstreamError):
        reader.read_ue()
    assert reader.read_bits(1) == 0