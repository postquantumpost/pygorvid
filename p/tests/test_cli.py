import struct
import subprocess
from pathlib import Path

import pytest

from pygorvid.cli import main


FIXTURE = Path(__file__).resolve().parents[2] / "samples" / "decoder-smoke-16x16.mp4"


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