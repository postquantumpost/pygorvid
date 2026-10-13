import hashlib
import os
import subprocess
from types import SimpleNamespace

import pytest

from pathlib import Path

from pygorvid import (
    DecoderError,
    DecoderTimeLimitExceeded,
    H264Decoder,
    PresentationOrderBuffer,
    PresentationPicture,
    ReferencePicture,
    VideoSampleReader,
    Yuv420Frame,
)
from pygorvid.reconstruction import Yuv420FrameBuilder

ROOT = Path(__file__).resolve().parents[2]
FIXTURE = ROOT / "testdata" / "h264" / "high42-1080p.mp4"


def _test_time_limit_seconds():
    value = os.environ.get("PYGORVID_TEST_TIME_LIMIT")
    return None if not value else float(value)


def test_a_decoder_smoke_16x16():
    _assert_decoder_smoke_fixture(
        "decoder-smoke-16x16.mp4", 16, (0xCABF755E2A6B7D56, 0x29631735FF370504)
    )


def test_b_decoder_smoke_64x64():
    _assert_decoder_smoke_fixture(
        "decoder-smoke-64x64.mp4", 64, (0xDD69FBA2006D59E0, 0x93ECF9BED15545A5)
    )


def _assert_decoder_smoke_fixture(name, dimension, expected_hashes):
    reader = VideoSampleReader(ROOT / "samples" / name)
    try:
        decoder = H264Decoder(reader)
        for frame_index, expected_hash in enumerate(expected_hashes):
            frame = decoder.decode_frame(frame_index)
            assert (frame.width, frame.height) == (dimension, dimension)
            hash_value = 0xCBF29CE484222325
            for plane in (frame.y, frame.u, frame.v):
                for sample in plane:
                    hash_value = ((hash_value ^ sample) * 0x100000001B3) & 0xFFFFFFFFFFFFFFFF
            assert hash_value == expected_hash, f"{name} frame {frame_index} YUV hash differs"
    finally:
        reader.close()


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


def test_h264_decoder_checkpoint_save_and_load(tmp_path):
    fixture_path = ROOT / "samples" / "decoder-smoke-16x16.mp4"
    reader1 = VideoSampleReader(fixture_path)
    ckpt_file = tmp_path / "smoke16.ckpt"
    try:
        decoder1 = H264Decoder(reader1)
        f0 = decoder1.decode_frame(0)
        assert decoder1._last_decoded_sample_index == 0
        decoder1.save_checkpoint(ckpt_file)
        assert ckpt_file.is_file()
    finally:
        reader1.close()

    reader2 = VideoSampleReader(fixture_path)
    try:
        decoder2 = H264Decoder(reader2)
        decoder2.load_checkpoint(ckpt_file)
        assert decoder2._gop_sync_sample_index == 0
        assert decoder2._last_decoded_sample_index == 0
        assert 0 in decoder2._decoded_frames
        assert decoder2._decoded_frames[0] == f0
        assert decoder2.reference_picture_buffer.get(0) is not None

        f1 = decoder2.decode_frame(1)
        assert (f1.width, f1.height) == (16, 16)
        hash_value = 0xCBF29CE484222325
        for plane in (f1.y, f1.u, f1.v):
            for sample in plane:
                hash_value = ((hash_value ^ sample) * 0x100000001B3) & 0xFFFFFFFFFFFFFFFF
        assert hash_value == 0x29631735FF370504
        assert decoder2._last_decoded_sample_index == 1
    finally:
        reader2.close()


def test_h264_decoder_checkpoint_auto_load_and_save(tmp_path):
    fixture_path = ROOT / "samples" / "decoder-smoke-16x16.mp4"
    ckpt_file = tmp_path / "auto.ckpt"

    reader1 = VideoSampleReader(fixture_path)
    try:
        decoder1 = H264Decoder(reader1, checkpoint_path=ckpt_file)
        decoder1.decode_frame(0)
        assert ckpt_file.is_file()
    finally:
        reader1.close()

    reader2 = VideoSampleReader(fixture_path)
    try:
        decoder2 = H264Decoder(reader2, checkpoint_path=ckpt_file)
        assert decoder2._last_decoded_sample_index == 0
        assert 0 in decoder2._decoded_frames
        decoder2.decode_frame(1)
        assert decoder2._last_decoded_sample_index == 1
    finally:
        reader2.close()


def test_h264_decoder_checkpoint_errors(tmp_path):
    import pickle

    fixture_path = ROOT / "samples" / "decoder-smoke-16x16.mp4"
    reader = VideoSampleReader(fixture_path)
    try:
        decoder = H264Decoder(reader)
        with pytest.raises(FileNotFoundError):
            decoder.load_checkpoint(tmp_path / "nonexistent.ckpt")

        bad_file = tmp_path / "bad.ckpt"
        bad_file.write_bytes(b"not a valid pickle")
        with pytest.raises(DecoderError, match="corrupt"):
            decoder.load_checkpoint(bad_file)

        wrong_version = tmp_path / "wrong_version.ckpt"
        wrong_version.write_bytes(pickle.dumps({"version": 999}))
        with pytest.raises(DecoderError, match="unsupported"):
            decoder.load_checkpoint(wrong_version)

        mismatched_sps = tmp_path / "mismatch.ckpt"
        mismatched_sps.write_bytes(pickle.dumps({
            "version": 1,
            "sequence_parameter_sets": (b"\x67\x00\x00\x00",),
            "picture_parameter_sets": (),
        }))
        with pytest.raises(DecoderError, match="parameter sets do not match"):
            decoder.load_checkpoint(mismatched_sps)
    finally:
        reader.close()


def test_h264_decoder_requires_sample_reader():
    decoder = H264Decoder(None)
    with pytest.raises(DecoderError, match="sample reader"):
        decoder.decode_frame(0)


def test_h264_decoder_rejects_out_of_range_frame_requests():
    decoder = H264Decoder(type("Reader", (), {"sample_count": 0})())
    with pytest.raises(DecoderError, match="out of range"):
        decoder.decode_frame(0)


def test_h264_decoder_requires_avc_configuration_for_uncached_decode():
    reader = type(
        "Reader",
        (),
        {"sample_count": 1, "presentation_sample_index": lambda self, index: 0},
    )()
    decoder = H264Decoder(reader)
    with pytest.raises(DecoderError, match="AVC configuration"):
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


def test_h264_decoder_reconstructs_filtered_p_picture_planes():
    reader = VideoSampleReader(FIXTURE)
    try:
        decoder = H264Decoder(
            reader,
            debug_progress=os.environ.get("PYGORVID_DEBUG_PROGRESS") == "1",
            time_limit_seconds=_test_time_limit_seconds(),
            checkpoint_dir=ROOT / "tmp" / "checkpoints",
        )
        try:
            frame = decoder.decode_frame(3)
        except DecoderTimeLimitExceeded as error:
            pytest.skip(str(error))
        reference = decoder.reference_picture_buffer.get(0)
        assert reference is not None
        assert reference.frame.height == 1088
        assert hashlib.sha256(frame.y).hexdigest() == "ccec3593f0d7593ef7837dc2b180ac57801226a636480e1afb7275a4b8898ae4"
        assert hashlib.sha256(frame.u).hexdigest() == "f617912e620ab4aa0cb2d68f632155b65f688a705253e896372003ff7c77d673"
        assert hashlib.sha256(frame.v).hexdigest() == "8adb12498b11dc9eaf69a11078a59734429f2ed65056bbf7370e4abd6d9104d6"
        assert {reference.identifier for reference in decoder.reference_picture_buffer.references()} == {0, 3}
    finally:
        reader.close()


def test_h264_decoder_reconstructs_filtered_b_picture_planes():
    reader = VideoSampleReader(FIXTURE)
    try:
        decoder = H264Decoder(
            reader,
            debug_progress=os.environ.get("PYGORVID_DEBUG_PROGRESS") == "1",
            time_limit_seconds=_test_time_limit_seconds(),
            checkpoint_dir=ROOT / "tmp" / "checkpoints",
        )
        try:
            frame = decoder.decode_frame(1)
        except DecoderTimeLimitExceeded as error:
            pytest.skip(str(error))
        assert (frame.width, frame.height) == (1920, 1080)
        assert hashlib.sha256(frame.y).hexdigest() == "0aa418bbf2ad460d6246a6bbf09e1f85ef79c33a4543482d9fadf2757913db6d"
        assert hashlib.sha256(frame.u).hexdigest() == "4387bede65226f45383960ace74ac53c365bc3ec4b6b586d079ec38c301acaa7"
        assert hashlib.sha256(frame.v).hexdigest() == "94910e086498b34d635417f29773dbc811afed0e2e9b530abc5cd4fd22d31969"
        assert {reference.identifier for reference in decoder.reference_picture_buffer.references()} == {0, 3}
        assert decoder.decode_frame(1) == frame
    finally:
        reader.close()


def test_picture_deblocking_combines_intra_luma_ac_flags_per_block():
    from pygorvid.cabac import CABACIIntraMacroblockResult
    from pygorvid.decoder import _picture_deblocking_macroblock

    ac_flags = [False] * 16
    ac_flags[4] = True
    decoded = CABACIIntraMacroblockResult(
        macroblock_type=0,
        coded_block_pattern_luma=0,
        coded_block_pattern_chroma=0,
        transform_size_8x8=False,
        intra16x16_luma_mode=0,
        chroma_prediction_mode=0,
        luma_4x4_modes=(),
        luma_8x8_modes=(),
        qp_delta=0,
        qpy=26,
        luma=(0,) * 256,
        cb=(0,) * 64,
        cr=(0,) * 64,
        luma_4x4_coded_block_flags=(False,) * 16,
        chroma_dc_coded=(False, False),
        chroma_ac_coded_flags=((False,) * 4, (False,) * 4),
        luma_ac_coded_block_flags=tuple(ac_flags),
    )
    dependency_slice = SimpleNamespace(
        header=SimpleNamespace(
            disable_deblocking_filter_idc=0,
            slice_alpha_c0_offset_div2=0,
            slice_beta_offset_div2=0,
        )
    )
    picture_set = SimpleNamespace(chroma_qp_index_offset=0, second_chroma_qp_index_offset=0)

    metadata = _picture_deblocking_macroblock(
        SimpleNamespace(macroblock=decoded), dependency_slice, ((), ()), picture_set, 0
    )

    assert metadata.nonzero[2]


def test_h264_decoder_stops_at_time_limit_between_macroblocks():
    reader = VideoSampleReader(FIXTURE)
    try:
        decoder = H264Decoder(reader, time_limit_seconds=1e-12)
        with pytest.raises(DecoderTimeLimitExceeded, match="time limit"):
            decoder.decode_frame(0)
    finally:
        reader.close()


def test_adaptive_reference_marking_applies_all_frame_operations_in_order():
    from pygorvid.decoder import _apply_adaptive_reference_marking
    from pygorvid.reconstruction import ReferencePictureBuffer
    from pygorvid.slice import MemoryManagementOperation

    buffer = ReferencePictureBuffer()
    frame = Yuv420Frame(1, 1, 1, 1, 1, bytes([0]), bytes([0]), bytes([0]))
    for identifier, frame_num, long_term_frame_idx in (
        (14, 14, None),
        (15, 15, None),
        (50, 2, 1),
        (51, 3, 3),
        (52, 4, 2),
    ):
        buffer.store(
            ReferencePicture(identifier, frame_num, frame_num, long_term_frame_idx), frame
        )
    operations = (
        MemoryManagementOperation(1, (2,)),
        MemoryManagementOperation(4, (3,)),
        MemoryManagementOperation(3, (1, 1)),
        MemoryManagementOperation(2, (1,)),
        MemoryManagementOperation(6, (2,)),
    )

    current_long_term_idx, reset_frame_num, maximum_long_term_idx = (
        _apply_adaptive_reference_marking(buffer, 1, 16, operations, None)
    )

    assert (current_long_term_idx, reset_frame_num, maximum_long_term_idx) == (2, False, 2)
    assert buffer.references() == ()


def test_adaptive_reference_marking_mmco5_clears_and_resets():
    from pygorvid.decoder import _apply_adaptive_reference_marking
    from pygorvid.reconstruction import ReferencePictureBuffer
    from pygorvid.slice import MemoryManagementOperation

    buffer = ReferencePictureBuffer()
    frame = Yuv420Frame(1, 1, 1, 1, 1, bytes([0]), bytes([0]), bytes([0]))
    buffer.store(ReferencePicture(1, 1, 2), frame)

    result = _apply_adaptive_reference_marking(
        buffer, 2, 16, (MemoryManagementOperation(5, ()),), 3
    )

    assert result == (None, True, None)
    assert buffer.references() == ()


def test_decoder_parses_dependency_slice_headers_without_advancing_reader():
    reader = VideoSampleReader(FIXTURE)
    try:
        decoder = H264Decoder(reader)
        pictures = decoder.dependency_slice_pictures(0)
        assert pictures
        assert pictures[0]
        assert pictures[0][0].sample_index == 0
        assert pictures[0][0].header.idr
        assert pictures[0][0].header.first_macroblock_in_slice == 0
        assert reader._next_index == 0
    finally:
        reader.close()


def test_decoder_builds_next_real_idr_macroblock_input_from_history(monkeypatch):
    from dataclasses import replace
    from pygorvid import CABACIIntraMacroblockInput

    class StopAfterProviderBuild(Exception):
        pass

    reader = VideoSampleReader(FIXTURE)
    try:
        decoder = H264Decoder(reader)
        dependency_slice = decoder.dependency_slice_pictures(0)[0][0]
        configuration = reader.configuration
        from pygorvid import parse_sps

        sps = parse_sps(configuration.sequence_parameter_sets[0])
        builder = Yuv420FrameBuilder(
            sps.coded_width // 16,
            sps.coded_height // 16,
            sps.frame_crop_left,
            sps.frame_crop_top,
            sps.frame_crop_right,
            sps.frame_crop_bottom,
        )
        import pygorvid.decoder as decoder_module

        parser_calls = {"pps": 0, "sps": 0}
        original_parse_pps = decoder_module.parse_pps
        original_parse_sps = decoder_module.parse_sps

        def count_parse_pps(nal):
            parser_calls["pps"] += 1
            return original_parse_pps(nal)

        def count_parse_sps(nal):
            parser_calls["sps"] += 1
            return original_parse_sps(nal)

        monkeypatch.setattr(decoder_module, "parse_pps", count_parse_pps)
        monkeypatch.setattr(decoder_module, "parse_sps", count_parse_sps)
        addresses = []

        def input_provider(address, history):
            addresses.append((address, len(history)))
            macroblock_input = decoder.macroblock_input_from_history(
                dependency_slice, address, history
            )
            assert isinstance(macroblock_input, CABACIIntraMacroblockInput)
            assert macroblock_input.top_luma_cbp == 15
            if address == 0:
                assert macroblock_input.left_luma_cbp == 15
            if address == 1:
                assert history[0].macroblock.luma[0] == 29
                assert macroblock_input.left_available
                assert not macroblock_input.top_available
                assert macroblock_input.left_luma_cbp == history[0].macroblock.coded_block_pattern_luma
                assert macroblock_input.chroma_references[0].left_available
                assert not macroblock_input.chroma_references[0].top_available
                assert macroblock_input.luma_8x8_blocks[0].references_filtered is False
                pcm_history = (
                    replace(history[0], macroblock=replace(history[0].macroblock, macroblock_type=25)),
                )
                pcm_input = decoder.macroblock_input_from_history(
                    dependency_slice, address, pcm_history
                )
                assert pcm_input.left_luma_cbp == 15
                assert pcm_input.left_chroma_cbp == 2
                assert pcm_input.top_chroma_cbp == 0
            if address == 2:
                assert history[1].macroblock.luma[0] == 29
                assert history[1].macroblock.chroma_prediction_mode == 0
                raise StopAfterProviderBuild
            return macroblock_input

        with pytest.raises(StopAfterProviderBuild):
            decoder.decode_dependency_slice_data(dependency_slice, builder, input_provider)
        assert addresses == [(0, 0), (1, 1), (2, 2)]
        assert parser_calls == {"pps": 0, "sps": 0}
        assert builder._written[0]
        assert builder._written[1]
    finally:
        reader.close()


def test_decoder_reconstructs_complete_idr_row_against_unfiltered_reference():
    from pygorvid import parse_sps

    class RowComplete(Exception):
        pass

    reader = VideoSampleReader(FIXTURE)
    try:
        decoder = H264Decoder(reader)
        dependency_slice = decoder.dependency_slice_pictures(0)[0][0]
        sps = parse_sps(reader.configuration.sequence_parameter_sets[0])
        width_in_mbs = sps.coded_width // 16
        builder = Yuv420FrameBuilder(width_in_mbs, 1)

        def provider(address, history):
            if address == width_in_mbs:
                assert len(history) == width_in_mbs
                raise RowComplete
            return decoder.macroblock_input_from_history(dependency_slice, address, history)

        with pytest.raises(RowComplete):
            decoder.decode_dependency_slice_data(dependency_slice, builder, provider)
        frame = builder.finish()
        assert (frame.width, frame.height) == (1920, 16)
        expected = (
            "6f2f099fd78cb515e8b82e994953dbec014464e8d299b1ccf2f63d8e622fa718",
            "4f425df0e64a24976cde5df7c46a7616d620347a1113e2c207cf2c3d8095e65c",
            "91bb129b4f34b17ddbcf034994f5ccd66f97925754c2fa5ca5e20915236ca8d4",
        )
        assert tuple(hashlib.sha256(plane).hexdigest() for plane in (frame.y, frame.u, frame.v)) == expected
        assert reader._next_index == 0
    finally:
        reader.close()


@pytest.mark.parametrize(
    ("fixture", "width", "height", "expected"),
    (
        (
            "high42-1080p.mp4", 1920, 1080,
            (
                "c0c5e21d3b83efdd7dddabbf2d512f25db7c7d21153cf4d6260ba43853a40643",
                "e7c2b2fca087820ecf5798e9744b61284c395acb164f3bb03af9c8d05ab445c0",
                "9a7422ce1ec42db3856d7e08f7d86c4271975790a18efaccaca89876e5c39f9d",
            ),
        ),
        (
            "high52-2160p.mp4", 3840, 2160,
            (
                "d5c11a8e30d271c1e61e625ca5896fe000a6274f80c8a13929763f7556233cbf",
                "888fafb21f5b4ec781cccf0b17692f35502ec3f62f4fcd37451861259f5e3ad9",
                "888fafb21f5b4ec781cccf0b17692f35502ec3f62f4fcd37451861259f5e3ad9",
            ),
        ),
    ),
)
def test_decoder_assembles_complete_native_idr_against_unfiltered_reference(
    fixture, width, height, expected, monkeypatch
):
    def reject_subprocess(*args, **kwargs):
        raise AssertionError("native IDR decoder invoked a subprocess")

    monkeypatch.setattr(subprocess, "Popen", reject_subprocess)
    reader = VideoSampleReader(ROOT / "testdata" / "h264" / fixture)
    try:
        decoder = H264Decoder(reader)
        from pygorvid import parse_sps

        sps = parse_sps(reader.configuration.sequence_parameter_sets[0])
        builder = Yuv420FrameBuilder(
            sps.coded_width // 16, sps.coded_height // 16,
            sps.frame_crop_left, sps.frame_crop_top, sps.frame_crop_right, sps.frame_crop_bottom,
        )
        for dependency_slice in decoder.dependency_slice_pictures(0)[0]:
            decoder.decode_dependency_slice_data(
                dependency_slice, builder,
                lambda address, history: decoder.macroblock_input_from_history(dependency_slice, address, history),
            )
        frame = builder.finish()
        assert (frame.width, frame.height) == (width, height)
        assert (len(frame.y), len(frame.u), len(frame.v)) == (
            width * height, width * height // 4, width * height // 4,
        )
        assert tuple(hashlib.sha256(plane).hexdigest() for plane in (frame.y, frame.u, frame.v)) == expected
        assert reader._next_index == 0
    finally:
        reader.close()


@pytest.mark.parametrize("fixture", ("high42-1080p.mp4", "high52-2160p.mp4"))
def test_decoder_filtered_idr_matches_reference_planes(fixture, monkeypatch):
    import json

    def reject_subprocess(*args, **kwargs):
        raise AssertionError("native IDR decoder invoked a subprocess")

    monkeypatch.setattr(subprocess, "Popen", reject_subprocess)
    reference = next(
        record for record in json.loads((ROOT / "silkroad1-reference-vectors.json").read_text())["reference_frames"]
        if record["fixture"] == fixture
    )["frame_references"]["I"]["planes"]
    reader = VideoSampleReader(ROOT / "testdata" / "h264" / fixture)
    try:
        decoder = H264Decoder(reader)
        frame = decoder.decode_frame(0)
        for name, plane in (("y", frame.y), ("u", frame.u), ("v", frame.v)):
            assert hashlib.sha256(plane).hexdigest() == reference[name]["sha256"], name
        assert decoder.decode_frame(0) == frame
        assert reader._next_index == 0
    finally:
        reader.close()


def test_decoder_builds_p_and_spatial_b_macroblock_inputs_from_reference_state():
    from pygorvid.cabac import (
        CABACInterMacroblockPartition,
        CABACInterMacroblockResult,
        CABACPBMacroblockLayerResult,
        CABACSliceMacroblockResult,
    )
    from pygorvid import CABACPBMacroblockLayerInput
    from pygorvid import parse_sps

    reader = VideoSampleReader(FIXTURE)
    try:
        decoder = H264Decoder(reader)
        pictures = decoder.dependency_slice_pictures(1)
        p_slice = pictures[1][0]
        b_slice = pictures[2][0]
        sps = parse_sps(reader.configuration.sequence_parameter_sets[0])
        frame = Yuv420Frame(
            sps.width,
            sps.height,
            sps.width,
            (sps.width + 1) // 2,
            (sps.width + 1) // 2,
            bytes(sps.width * sps.height),
            bytes(((sps.width + 1) // 2) * ((sps.height + 1) // 2)),
            bytes(((sps.width + 1) // 2) * ((sps.height + 1) // 2)),
        )
        decoder.store_reference_picture(ReferencePicture(10, 0, 0), frame)

        p_input = decoder.macroblock_input_from_history(p_slice, 0, ())
        assert isinstance(p_input, CABACPBMacroblockLayerInput)
        assert p_input.residual_input.left_luma_cbp == 15
        assert p_input.residual_input.top_luma_cbp == 15
        assert p_input.residual_input.left_chroma_cbp == 0
        assert p_input.residual_input.top_chroma_cbp == 0
        assert len(p_input.inter_input.reference_lists[0]) == 1
        assert p_input.inter_input.reference_lists[0][0].frame == frame
        assert len(p_input.inter_input.partition_neighbors) == 16
        assert p_input.b_skip_input is None

        colocated_macroblock = CABACInterMacroblockResult(
            macroblock_type=2,
            sub_macroblock_types=(),
            partitions=(
                CABACInterMacroblockPartition(
                    0,
                    0,
                    8,
                    16,
                    (True, False),
                    (0, -1),
                    ((0, 1), (0, 0)),
                ),
                CABACInterMacroblockPartition(
                    8,
                    0,
                    8,
                    16,
                    (True, False),
                    (0, -1),
                    ((4, 0), (0, 0)),
                ),
            ),
            luma=(0,) * 256,
            cb=(0,) * 64,
            cr=(0,) * 64,
        )
        colocated_history = (
            CABACSliceMacroblockResult(
                0, CABACPBMacroblockLayerResult(False, colocated_macroblock)
            ),
        )
        next_p_input = decoder.macroblock_input_from_history(p_slice, 1, colocated_history)
        assert next_p_input.residual_input.left_luma_cbp == 0
        assert next_p_input.residual_input.top_luma_cbp == 15
        decoder.store_reference_picture(
            ReferencePicture(11, 1, 6), frame, colocated_history
        )
        b_input = decoder.macroblock_input_from_history(b_slice, 0, ())
        assert isinstance(b_input, CABACPBMacroblockLayerInput)
        assert b_input.residual_input.left_luma_cbp == 15
        assert b_input.residual_input.top_luma_cbp == 15
        assert len(b_input.inter_input.reference_lists[0]) == 1
        assert len(b_input.inter_input.reference_lists[1]) == 1
        assert b_input.inter_input.reference_lists[0][0].picture_order_count == 0
        assert b_input.inter_input.reference_lists[1][0].picture_order_count == 6
        assert b_input.b_skip_input is not None
        assert b_input.b_skip_input.direct_spatial
        assert b_input.b_skip_input.colocated_reference_index == 0
        assert b_input.b_skip_input.colocated_motion_vector == (0, 1)
        assert b_input.b_skip_input.colocated_motion_field[0].vector.y == 1
        assert b_input.b_skip_input.colocated_motion_field[3].vector.x == 4
    finally:
        reader.close()


@pytest.mark.parametrize("frame_index", [0, 1, 3])
def test_h264_decoder_does_not_invoke_runtime_subprocesses(monkeypatch, frame_index):
    def reject_subprocess(*args, **kwargs):
        raise AssertionError("decoder invoked a runtime subprocess")

    monkeypatch.setattr(subprocess, "check_output", reject_subprocess)
    monkeypatch.setattr(subprocess, "run", reject_subprocess)
    reader = VideoSampleReader(FIXTURE)
    try:
        decoder = H264Decoder(reader)
        try:
            frame = decoder.decode_frame(frame_index)
        except DecoderError as error:
            assert "ffmpeg" not in str(error).lower()
        else:
            assert frame.width > 0 and frame.height > 0
    finally:
        reader.close()
