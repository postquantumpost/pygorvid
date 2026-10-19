import struct
import subprocess
from pathlib import Path

import pytest

from pygorvid.cli import main


FIXTURE = Path(__file__).resolve().parents[2] / "samples" / "decoder-smoke-16x16.mp4"


def test_read_all_frames_and_multiple_inputs(tmp_path, monkeypatch, capsys):
    monkeypatch.chdir(tmp_path)
    assert main([
        str(FIXTURE), "--readall", "--input", str(FIXTURE), "--threads", "2",
    ]) == 0
    captured = capsys.readouterr()
    assert captured.out.count(f"{FIXTURE}: read 3 frames") == 2
    assert not list(tmp_path.iterdir())
    assert main(["--readall", "missing.mp4", str(FIXTURE), "--threads", "1"]) == 1
    captured = capsys.readouterr()
    assert "missing.mp4:" in captured.err
    assert f"{FIXTURE}: read 3 frames" in captured.out


@pytest.mark.parametrize("options", (
    ("--extractframe", "0"),
    ("--output", "out.png"),
    ("--extractframe", "0", "--output", "out.png"),
))
def test_read_all_rejects_extraction_options(options):
    with pytest.raises(SystemExit) as error:
        main(["--readall", str(FIXTURE), *options])
    assert error.value.code == 2


def test_read_all_reports_failing_frame(monkeypatch, capsys):
    from pygorvid import cli
    from pygorvid.decoder import DecoderError

    class FailingDecoder:
        def __init__(self, reader, *, threads, debug_progress=False):
            pass

        def decode_frame(self, index):
            if index == 1:
                raise DecoderError("invalid frame data")

    monkeypatch.setattr(cli, "create_decoder", FailingDecoder)
    assert main(["--readall", str(FIXTURE), "--threads", "1"]) == 1
    assert "decode frame 1: invalid frame data" in capsys.readouterr().err


def test_verbose_flag_enables_decoder_progress(monkeypatch, capsys):
    from pygorvid import cli

    class FakeReader:
        sample_count = 2

        def __init__(self, path):
            pass

        def sample_at(self, index):
            return type("Sample", (), {"is_sync": index == 0})()

        def close(self):
            pass

    class FakeDecoder:
        def __init__(self, reader, *, threads, debug_progress):
            assert debug_progress

        def decode_frame(self, index):
            print(f"Frame {index}: 0%", file=__import__("sys").stderr)

    monkeypatch.setattr(cli, "VideoSampleReader", FakeReader)
    monkeypatch.setattr(cli, "create_decoder", FakeDecoder)
    assert main(["--readall", "fake.mp4", "--verbose"]) == 0
    captured = capsys.readouterr()
    assert "Frame 0: 0%" in captured.err and "Frame 1: 0%" in captured.err


@pytest.mark.parametrize("extension", (".png", ".jpg", ".PNG", ".JPG"))
@pytest.mark.parametrize("frame_index", (0, 1))
def test_native_extraction(tmp_path, monkeypatch, extension, frame_index):
    def reject_subprocess(*args, **kwargs):
        raise AssertionError("native extraction invoked a subprocess")

    monkeypatch.setattr(subprocess, "Popen", reject_subprocess)
    monkeypatch.setenv("PATH", "")
    output = tmp_path / ("image with spaces" + extension)
    assert main([
        "--input", str(FIXTURE), "--extractframe", str(frame_index),
        "--output", str(output), "--prefix", "native_",
    ]) == 0
    written = output.with_name("native_" + output.name)
    payload = written.read_bytes()
    if extension.lower() == ".png":
        assert payload[:8] == b"\x89PNG\r\n\x1a\n"
        assert struct.unpack(">II", payload[16:24]) == (16, 16)
    else:
        assert payload[:2] == b"\xff\xd8"
        assert payload[-2:] == b"\xff\xd9"
        assert b"JFIF\0" in payload
    assert not output.exists()
    assert sorted(path.name for path in tmp_path.iterdir()) == [written.name]


@pytest.mark.parametrize("threads", ("1", "3"))
def test_threads_option_extracts_identical_frames(tmp_path, threads):
    output = tmp_path / f"threads{threads}.png"
    assert main([str(FIXTURE), "--threads", threads, "--extractframe", "1", "--output", str(output)]) == 0
    reference = tmp_path / "reference.png"
    assert main([str(FIXTURE), "--threads", "1", "--extractframe", "1", "--output", str(reference)]) == 0
    assert output.read_bytes() == reference.read_bytes()


@pytest.mark.parametrize("threads", ("0", "-1", "x"))
def test_threads_option_rejects_non_positive_counts(threads):
    with pytest.raises(SystemExit) as error:
        main([str(FIXTURE), "--threads", threads])
    assert error.value.code == 2


@pytest.mark.parametrize("frame_index,extension", ((999, ".png"), (0, ".gif")))
def test_failed_extraction_preserves_output(tmp_path, frame_index, extension):
    output = tmp_path / ("existing" + extension)
    output.write_bytes(b"original")
    assert main([
        str(FIXTURE), "--extractframe", str(frame_index), "--output", str(output),
    ]) == 1
    assert output.read_bytes() == b"original"
    assert sorted(path.name for path in tmp_path.iterdir()) == [output.name]


def test_malformed_input_and_write_failure_leave_no_partial_output(tmp_path):
    invalid = tmp_path / "invalid.mp4"
    invalid.write_bytes(b"not an MP4")
    output = tmp_path / "image.png"
    assert main([str(invalid), "--extractframe", "0", "--output", str(output)]) == 1
    assert not output.exists()
    output.mkdir()
    assert main([str(FIXTURE), "--extractframe", "0", "--output", str(output)]) == 1
    assert output.is_dir()
    assert sorted(path.name for path in tmp_path.iterdir()) == sorted([invalid.name, output.name])