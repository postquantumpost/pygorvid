import subprocess

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


def test_h264_decoder_tracks_p_frame_reference_cache_validation():
    reader = VideoSampleReader(FIXTURE)
    try:
        decoder = H264Decoder(reader)
        frame = Yuv420Frame(2, 2, 3, 2, 2, bytes([1, 2, 99, 3, 4, 99]), bytes([5, 99]), bytes([6, 99]))
        packed = Yuv420Frame(2, 2, 2, 1, 1, bytes([1, 2, 3, 4]), bytes([5]), bytes([6]))
        reference = ReferencePicture(3, frame_num=3, picture_order_cnt=3)
        decoder.store_reference_picture(reference, frame)

        cached = decoder.decode_frame(3)
        assert cached == packed
        assert len(decoder.reference_picture_buffer.references()) == 1
        assert decoder.decode_frame(3) == packed
        assert decoder.reference_picture_buffer.get(3).frame == frame
    finally:
        reader.close()


def test_h264_decoder_tracks_b_frame_cache_and_presentation_order():
    reader = VideoSampleReader(FIXTURE)
    try:
        decoder = H264Decoder(reader)
        frame = Yuv420Frame(2, 2, 2, 1, 1, bytes([1, 2, 3, 4]), bytes([5]), bytes([6]))
        decoder.store_reference_picture(ReferencePicture(1, frame_num=1, picture_order_cnt=1), frame)
        decoder.store_reference_picture(ReferencePicture(2, frame_num=2, picture_order_cnt=2), frame)
        assert decoder.decode_frame(1) == frame
        assert decoder.decode_frame(2) == frame

        decoder.presentation_order_buffer = PresentationOrderBuffer(2)
        coded_pictures = [
            PresentationPicture(poc, Yuv420Frame(2, 2, 2, 1, 1, bytes([poc] * 4), bytes([5]), bytes([6])))
            for poc in (0, 6, 2, 4)
        ]
        ready = [decoder.queue_presentation(picture) for picture in coded_pictures]
        assert [picture.picture_order_cnt if picture is not None else None for picture in ready] == [None, None, 0, 2]
        drained = decoder.presentation_order_buffer.drain()
        assert [picture.picture_order_cnt for picture in drained] == [4, 6]
        assert [picture.frame.y[0] for picture in drained] == [4, 6]
        assert decoder.decode_frame(1) == frame
        assert decoder.decode_frame(2) == frame
        assert len(decoder.reference_picture_buffer.references()) == 2
    finally:
        reader.close()


def test_h264_decoder_tracks_first_sync_index_and_reference_validation():
    reader = VideoSampleReader(FIXTURE)
    try:
        decoder = H264Decoder(reader)
        assert decoder.first_sync_sample_index() == 0
        frame = Yuv420Frame(2, 2, 2, 1, 1, bytes([1, 2, 3, 4]), bytes([5]), bytes([6]))
        decoder.store_reference_picture(ReferencePicture(0), frame)
        decoder.validate_reference_frame(0, frame.y, frame.u, frame.v)
        with pytest.raises(DecoderError, match="reference"):
            decoder.validate_reference_frame(0, bytes([9, 2, 3, 4]), frame.u, frame.v)
    finally:
        reader.close()


@pytest.mark.parametrize("frame_index", [0, 1, 3])
def test_h264_decoder_does_not_invoke_runtime_subprocesses(monkeypatch, frame_index):
    def reject_subprocess(*args, **kwargs):
        raise AssertionError("decoder invoked a runtime subprocess")

    monkeypatch.setattr(subprocess, "check_output", reject_subprocess)
    reader = VideoSampleReader(FIXTURE)
    try:
        decoder = H264Decoder(reader)
        with pytest.raises(DecoderError, match="native decode does not invoke"):
            decoder.decode_frame(frame_index)
        assert decoder.reference_picture_buffer.references() == ()
    finally:
        reader.close()
