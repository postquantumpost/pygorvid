import pytest

from pathlib import Path

from pygorvid import (
    DecoderError,
    H264Decoder,
    PresentationOrderBuffer,
    PresentationPicture,
    ReferencePicture,
    VideoSampleReader,
    Yuv420Frame,
)

ROOT = Path(__file__).resolve().parents[2]
FIXTURE = ROOT / "testdata" / "h264" / "high42-1080p.mp4"


def test_h264_decoder_tracks_reader_state():
    reader = type("Reader", (), {"sample_count": 2})()
    decoder = H264Decoder(reader)
    assert decoder.sample_reader is reader
    assert decoder.sample_count == 2
    assert decoder.reference_picture_buffer.references() == ()

    frame = Yuv420Frame(2, 2, 2, 1, 1, bytes([1, 2, 3, 4]), bytes([5]), bytes([6]))
    reference = ReferencePicture(7, frame_num=3, picture_order_cnt=8)
    decoder.store_reference_picture(reference, frame)
    stored = decoder.reference_picture_buffer.get(reference.identifier)
    assert stored is not None and stored.reference.identifier == reference.identifier and stored.frame.y[0] == 1

    decoder.presentation_order_buffer = PresentationOrderBuffer(1)
    queued = decoder.queue_presentation(PresentationPicture(8, frame))
    assert queued is None


def test_h264_decoder_requires_sample_reader():
    decoder = H264Decoder(None)
    with pytest.raises(DecoderError, match="sample reader"):
        decoder.decode_frame(0)


def test_h264_decoder_rejects_out_of_range_frame_requests():
    decoder = H264Decoder(type("Reader", (), {"sample_count": 0})())
    with pytest.raises(DecoderError, match="out of range"):
        decoder.decode_frame(0)


def test_h264_decoder_contract_marks_frame_decode_as_unimplemented():
    decoder = H264Decoder(type("Reader", (), {"sample_count": 1})())
    with pytest.raises(DecoderError, match="not implemented"):
        decoder.decode_frame(0)


def test_h264_decoder_uses_real_sample_reader_fixture():
    reader = VideoSampleReader(FIXTURE)
    decoder = H264Decoder(reader)
    assert decoder.sample_count == reader.sample_count
    frame = decoder.decode_frame(0)
    assert frame.width == 1920 and frame.height == 1080
    assert frame.y[0] == 29 and frame.u[0] == 129 and frame.v[0] == 127


def test_h264_decoder_uses_real_sample_reader_fixture_for_p_frame():
    reader = VideoSampleReader(FIXTURE)
    decoder = H264Decoder(reader)
    frame = decoder.decode_frame(3)
    assert frame.width == 1920 and frame.height == 1080
    assert frame.y[100000] == 41 and frame.u[100] == 129 and frame.v[100] == 127


def test_h264_decoder_uses_real_sample_reader_fixture_for_b_frame():
    reader = VideoSampleReader(FIXTURE)
    decoder = H264Decoder(reader)
    frame = decoder.decode_frame(1)
    assert frame.width == 1920 and frame.height == 1080
    assert frame.y[0] == 29 and frame.u[0] == 129 and frame.v[0] == 127
    assert len(decoder.reference_picture_buffer.references()) == 1

    cached = decoder.decode_frame(1)
    assert len(decoder.reference_picture_buffer.references()) == 1
    assert cached.y == frame.y and cached.u == frame.u and cached.v == frame.v
