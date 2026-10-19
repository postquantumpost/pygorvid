import pytest
from pathlib import Path

from pygorvid.bitreader import BitReader
from pygorvid.h264_cavlc import CAVLCError, decode_cavlc_residual_block


@pytest.mark.parametrize("threads", [1, 2])
def test_cavlc_ipb_frames_match_reference(monkeypatch, threads):
    from pygorvid import H264Decoder, VideoSampleReader
    monkeypatch.setenv("PATH", "")
    root = Path(__file__).resolve().parents[2] / "testdata" / "h264"
    expected = (root / "cavlc-32x32-reference.yuv").read_bytes()
    with VideoSampleReader(root / "cavlc-32x32.mp4") as reader:
        decoder = H264Decoder(reader, threads=threads)
        assert reader.sample_count == 8
        for index in range(reader.sample_count):
            frame = decoder.decode_frame(index)
            actual = bytes(frame.y) + bytes(frame.u) + bytes(frame.v)
            start = index * 32 * 32 * 3 // 2
            assert actual == expected[start:start + len(actual)], f"CAVLC frame {index}, threads {threads}"


def bits(value):
    padding = -len(value) % 8
    return int(value + "0" * padding, 2).to_bytes((len(value) + 7) // 8, "big")


@pytest.mark.parametrize(("code", "nc", "count", "nonzero"), [
    ("1", 0, 16, {}), ("0101", 0, 16, {0: 1}),
    ("011011", 0, 16, {1: -1}), ("00101111", 0, 16, {0: -1, 1: 1}),
    ("101", -1, 4, {0: 1}), ("000011", 8, 16, {}),
])
def test_cavlc_residual_vectors(code, nc, count, nonzero):
    reader = BitReader(bits(code))
    block = decode_cavlc_residual_block(reader, nc, count)
    expected = tuple(nonzero.get(index, 0) for index in range(count))
    assert block.coefficients == expected
    assert block.total_coeff == len(nonzero)
    assert reader.bit_offset == len(code)


def test_cavlc_residual_truncation_rolls_back():
    reader = BitReader(bytes(1))
    with pytest.raises(CAVLCError):
        decode_cavlc_residual_block(reader, 0, 16)
    assert reader.bit_offset == 0