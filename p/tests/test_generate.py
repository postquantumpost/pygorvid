import subprocess

import pytest

from pygorvid import (
    H264Decoder,
    H264Encoder,
    Mp4Metadata,
    Mp4Writer,
    VideoSampleReader,
    parse_pps,
    parse_slice_header,
    parse_sps,
)
from pygorvid.cli import _generation_metadata, main
from pygorvid.mp4writer import _metadata_box


def test_generate_native_video_with_gray_padding(tmp_path, monkeypatch):
    def reject_subprocess(*args, **kwargs):
        raise AssertionError("generation invoked a subprocess")

    monkeypatch.setattr(subprocess, "Popen", reject_subprocess)
    monkeypatch.setenv("PATH", "")
    output = tmp_path / "grey video.MP4"
    assert main(["--generate", str(output)]) == 0
    assert output.read_bytes() == Mp4Writer().encode(H264Encoder(), _generation_metadata())
    reader = VideoSampleReader(output)
    try:
        assert reader.sample_count == 60
        assert [(reader.sample_at(index).dts_ticks, reader.sample_at(index).pts_ticks)
                for index in (18, 19, 20, 21)] == [(18, 18), (19, 20), (20, 19), (21, 21)]
        decoder = H264Decoder(reader, threads=1)
        for index in (0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 59):
            sample = reader.sample_at(index)
            assert (sample.dts_ticks, sample.pts_ticks, sample.duration_ticks) == (index, index, 1)
            assert sample.is_sync == (index not in (2, 3, 4, 5, 6, 7, 8, 9))
            if index == 1:
                assert b"\x00\x00\x03\x01" in sample.data
            if index == 2:
                assert sample.data[4] == 0x21
            if index == 3:
                assert sample.data[4] == 0x21
                header = parse_slice_header(
                    sample.data[4:],
                    parse_sps(H264Encoder().sequence_parameter_set()),
                    parse_pps(H264Encoder().picture_parameter_set()),
                )
                assert (header.frame_num, header.slice_type) == (2, 2)
                assert not header.idr and header.nal_ref_idc == 1
            if index == 4:
                header = parse_slice_header(
                    sample.data[4:],
                    parse_sps(H264Encoder().sequence_parameter_set()),
                    parse_pps(H264Encoder().picture_parameter_set()),
                )
                assert (header.frame_num, header.slice_type) == (3, 2)
                assert not header.idr and header.nal_ref_idc == 1
            if index == 5:
                header = parse_slice_header(
                    sample.data[4:],
                    parse_sps(H264Encoder().sequence_parameter_set()),
                    parse_pps(H264Encoder().picture_parameter_set()),
                )
                assert (header.frame_num, header.slice_type) == (4, 2)
                assert not header.idr and header.nal_ref_idc == 1
            if index == 6:
                header = parse_slice_header(
                    sample.data[4:],
                    parse_sps(H264Encoder().sequence_parameter_set()),
                    parse_pps(H264Encoder().picture_parameter_set()),
                )
                assert (header.frame_num, header.slice_type) == (5, 2)
                assert not header.idr and header.nal_ref_idc == 1
            if index == 7:
                header = parse_slice_header(
                    sample.data[4:],
                    parse_sps(H264Encoder().sequence_parameter_set()),
                    parse_pps(H264Encoder().picture_parameter_set()),
                )
                assert (header.frame_num, header.slice_type) == (6, 2)
                assert not header.idr and header.nal_ref_idc == 1
            if index == 8:
                header = parse_slice_header(
                    sample.data[4:],
                    parse_sps(H264Encoder().sequence_parameter_set()),
                    parse_pps(H264Encoder().picture_parameter_set()),
                )
                assert (header.frame_num, header.slice_type) == (7, 2)
                assert not header.idr and header.nal_ref_idc == 1
            if index == 9:
                header = parse_slice_header(
                    sample.data[4:],
                    parse_sps(H264Encoder().sequence_parameter_set()),
                    parse_pps(H264Encoder().picture_parameter_set()),
                )
                assert (header.frame_num, header.slice_type) == (8, 2)
                assert not header.idr and header.nal_ref_idc == 1
            frame = decoder.decode_frame(index)
            assert (frame.width, frame.height) == (64, 64)
            expected_luma = bytearray([128] * 4096)
            if index == 1:
                expected_luma[:3] = bytes((0, 0, 1))
            if index == 4:
                assert set(frame.y) == {127, 129}
                assert set(frame.u) == set(frame.v) == {126, 130}
            elif index == 6:
                assert bytes(frame.y[:16]) == bytes(
                    32 + 3 * column for column in range(16)
                )
                assert len(set(frame.y)) > 100
                assert set(frame.u) == set(range(80, 102))
                assert set(frame.v) == set(range(176, 198))
            elif index == 7:
                directional = decoder.decode_frame(6)
                residual_origin = 48 * 64 + 48
                assert frame.y[residual_origin] == directional.y[residual_origin] + 4
                for row in range(64):
                    for column in range(64):
                        if row < 48 or column < 48:
                            assert frame.y[row * 64 + column] == directional.y[row * 64 + column]
                assert any(
                    frame.y[row * 64 + column] != directional.y[row * 64 + column]
                    for row in range(48, 64)
                    for column in range(48, 64)
                )
                assert frame.u == directional.u and frame.v == directional.v
            elif index == 8:
                luma_residual = decoder.decode_frame(7)
                assert frame.y == luma_residual.y
                for row in range(32):
                    for column in range(32):
                        if row < 24 or column < 24:
                            offset = row * 32 + column
                            assert frame.u[offset] == luma_residual.u[offset]
                            assert frame.v[offset] == luma_residual.v[offset]
                assert any(
                    frame.u[row * 32 + column] != luma_residual.u[row * 32 + column]
                    for row in range(24, 32) for column in range(24, 32)
                )
                assert any(
                    frame.v[row * 32 + column] != luma_residual.v[row * 32 + column]
                    for row in range(24, 32) for column in range(24, 32)
                )
            elif index == 9:
                chroma_residual = decoder.decode_frame(8)
                assert frame.u == chroma_residual.u and frame.v == chroma_residual.v
                for row in range(64):
                    for column in range(64):
                        if row < 48 or column < 48:
                            offset = row * 64 + column
                            assert frame.y[offset] == chroma_residual.y[offset]
                assert any(
                    frame.y[row * 64 + column] != chroma_residual.y[row * 64 + column]
                    for row in range(48, 64) for column in range(48, 64)
                )
            else:
                assert bytes(frame.y) == expected_luma
                assert bytes(frame.u) == bytes(frame.v) == bytes([128]) * 1024
        sps = parse_sps(H264Encoder().sequence_parameter_set())
        pps = parse_pps(H264Encoder().picture_parameter_set())
        for sample_index in range(10, 19):
            header = parse_slice_header(reader.sample_at(sample_index).data[4:], sps, pps)
            assert header.slice_type == 0
            assert header.frame_num == (sample_index - 1) & 15
        future = parse_slice_header(reader.sample_at(19).data[4:], sps, pps)
        b_picture = parse_slice_header(reader.sample_at(20).data[4:], sps, pps)
        multi_reference = parse_slice_header(reader.sample_at(16).data[4:], sps, pps)
        reordered = parse_slice_header(reader.sample_at(17).data[4:], sps, pps)
        weighted = parse_slice_header(reader.sample_at(18).data[4:], sps, pps)
        assert multi_reference.num_ref_idx_active_override
        assert multi_reference.num_ref_idx_l0_active_minus1 == 1
        assert reordered.ref_pic_list_reordering_l0
        assert reordered.adaptive_ref_pic_marking
        assert reordered.memory_management_operations
        assert weighted.prediction_weight_table is not None
        weight = weighted.prediction_weight_table.lists[0][0]
        assert weight.weights == (2, 2, 2)
        assert weight.offsets == (-16, 8, -8)
        assert (future.frame_num, future.slice_type) == (2, 0)
        assert (b_picture.frame_num, b_picture.slice_type) == (2, 1)
        assert reader.sample_at(20).data[4] == 0x01
        assert decoder.decode_frame(19).y != decoder.decode_frame(20).y
    finally:
        reader.close()
    assert list(tmp_path.iterdir()) == [output]


@pytest.mark.parametrize("arguments", (
    [], ["input.mp4"], ["--input", "input.mp4"], ["--readall"],
    ["--extractframe", "0"], ["--output", ""],
))
def test_generate_rejects_invalid_arguments(tmp_path, arguments):
    output = tmp_path / ("bad.avi" if not arguments else "grey.mp4")
    output.write_bytes(b"keep")
    with pytest.raises(SystemExit) as error:
        main(["--generate", str(output), *arguments])
    assert error.value.code == 2
    assert output.read_bytes() == b"keep"


def test_generate_equals_and_io_failure(tmp_path):
    output = tmp_path / "grey.mp4"
    assert main([f"--generate={output}"]) == 0
    directory = tmp_path / "directory.mp4"
    directory.mkdir()
    assert main(["--generate", str(directory)]) == 1
    assert directory.is_dir()
    assert sorted(path.name for path in tmp_path.iterdir()) == ["directory.mp4", "grey.mp4"]


@pytest.mark.parametrize("prefix", ("native_", ""))
def test_generate_prefix(tmp_path, capsys, prefix):
    output = tmp_path / "grey video.MP4"
    written = output.with_name(prefix + output.name)
    assert main([f"--generate={output}", "--prefix", prefix]) == 0
    assert written.read_bytes() == Mp4Writer().encode(H264Encoder(), _generation_metadata())
    assert str(written) in capsys.readouterr().out
    assert list(tmp_path.iterdir()) == [written]
    written.unlink()
    written.mkdir()
    assert main(["--generate", str(output), f"--prefix={prefix}"]) == 1
    assert str(written) in capsys.readouterr().err
    assert list(tmp_path.iterdir()) == [written]


@pytest.mark.parametrize("picture_id", (-1, 65536))
def test_encoder_rejects_out_of_range_picture_id(picture_id):
    with pytest.raises(ValueError, match="IDR picture ID"):
        H264Encoder().encode_grey_frame(picture_id)

@pytest.mark.parametrize("frame_index", (-1, 65536))
def test_generated_frame_rejects_out_of_range_picture_id(frame_index):
    with pytest.raises(ValueError, match="IDR picture ID"):
        H264Encoder().encode_generated_frame(frame_index)


def test_optional_metadata_and_utf8():
    writer, encoder = Mp4Writer(), H264Encoder()
    plain = writer.encode(encoder)
    assert writer.encode(encoder, None) == plain
    assert writer.encode(encoder, Mp4Metadata()) == plain
    value = "Caf\u00e9 \u65e5\u672c \U0001f4f7\0tail"
    tagged = writer.encode(encoder, Mp4Metadata(title=value))
    assert value.encode("utf-8") in tagged
    legacy_mdta = _metadata_box(Mp4Metadata(title=value))
    assert len(legacy_mdta) == 69 + 32 + len("title") + len(value.encode("utf-8"))
    assert legacy_mdta in tagged
    assert b"mdirappl" in tagged
    assert b"mdta" not in plain
    assert b"ilst" in tagged
    assert b"Xtra" not in plain
    assert value.encode("utf-16le") + bytes(2) in tagged
    with pytest.raises(ValueError, match="must be a string"):
        writer.encode(encoder, Mp4Metadata(track=1))
    with pytest.raises(UnicodeEncodeError):
        writer.encode(encoder, Mp4Metadata(title="\ud800"))


def test_windows_xtra_utf16_layout():
    from struct import pack
    from pygorvid.mp4writer import _windows_metadata_box

    value = "A\u00e9\u65e5\U0001f4f7\0tail"
    data = b"A\0\xe9\0\xe5\x65\x3d\xd8\xf7\xdc\0\0t\0a\0i\0l\0\0\0"
    records = b""
    for name in (b"Title", b"pygorvid_title"):
        records += pack(">II", 12 + len(name) + 6 + len(data), len(name)) + name
        records += pack(">IIH", 1, 6 + len(data), 8) + data
    expected = pack(">I", len(records) + 8) + b"Xtra" + records
    metadata = Mp4Metadata(title=value)
    assert _windows_metadata_box(metadata) == expected
    assert expected in Mp4Writer().encode(H264Encoder(), metadata)
    assert _windows_metadata_box(None) == _windows_metadata_box(Mp4Metadata()) == b""


@pytest.mark.parametrize("date,year", (
    ("2016", "2016"), ("2016-12-31 23:59:60 UTC", "2016"),
    ("2016junk", None), ("-2016", None), ("\u0662\u0660\u0661\u0666", None), ("", None),
))
def test_windows_xtra_year_and_exact_date(date, year):
    from pygorvid.mp4writer import _windows_metadata_box

    payload = _windows_metadata_box(Mp4Metadata(date=date))
    assert (b"WM/Year" in payload) == (year is not None)
    if date:
        assert date.encode("utf-16le") + bytes(2) in payload


@pytest.mark.parametrize("number", ("0", "1", "001", "65535", "65536", "-1", "1/2", "\u0661"))
def test_compatibility_metadata_number_and_text_formats(number):
    from pygorvid.mp4writer import _compatibility_metadata_box
    from struct import unpack

    metadata = Mp4Metadata(track=number, disc=number)
    compatibility = _compatibility_metadata_box(metadata)
    assert number.encode("utf-8") in compatibility
    binary_number = number.isascii() and number.isdecimal() and int(number) <= 65535
    assert (b"trkn" in compatibility) == binary_number
    assert (b"disk" in compatibility) == binary_number
    if binary_number:
        track = compatibility.index(b"trkn") + 4
        assert compatibility[track:track + 16] == b"\0\0\0\x18data" + bytes(8)
        assert unpack(">4H", compatibility[track + 16:track + 24]) == (0, int(number), 0, 0)
