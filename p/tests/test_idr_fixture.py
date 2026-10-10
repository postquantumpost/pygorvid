import hashlib
import json
import shutil
import subprocess
from pathlib import Path

import pytest

ROOT = Path(__file__).resolve().parents[2]
FIXTURE = ROOT / "testdata" / "h264" / "high42-1080p.mp4"
MANIFEST = ROOT / "silkroad1-reference-vectors.json"


def _check_tool(name: str) -> None:
    if shutil.which(name) is None:
        pytest.skip(f"{name} is not installed; fixture validation requires it")


def _read_json(command: list[str]) -> dict:
    return json.loads(subprocess.check_output(command, text=True))


def _first_i_frame_index(video_path: Path) -> int:
    frames = _read_json(
        [
            "ffprobe",
            "-v",
            "error",
            "-select_streams",
            "v:0",
            "-show_frames",
            "-show_entries",
            "frame=pict_type,key_frame",
            "-of",
            "json",
            str(video_path),
        ]
    )["frames"]
    for index, frame in enumerate(frames):
        if frame.get("pict_type") == "I" and int(frame.get("key_frame", 0)):
            return index
    raise AssertionError(f"{video_path} does not contain a key-frame I picture")


def _extract_frame(video_path: Path, frame_index: int, width: int, height: int) -> bytes:
    raw = subprocess.check_output(
        [
            "ffmpeg",
            "-nostdin",
            "-v",
            "error",
            "-i",
            str(video_path),
            "-vf",
            f"select=eq(n\\,{frame_index})",
            "-frames:v",
            "1",
            "-pix_fmt",
            "yuv420p",
            "-f",
            "rawvideo",
            "pipe:1",
        ]
    )
    expected_size = width * height * 3 // 2
    assert len(raw) == expected_size, f"expected {expected_size} bytes, got {len(raw)}"
    return raw


def _plane_hash(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


@pytest.mark.skipif(not FIXTURE.exists(), reason="fixture is missing")
def test_high42_first_idr_fixture_matches_reference_vectors():
    _check_tool("ffmpeg")
    _check_tool("ffprobe")

    with MANIFEST.open("r", encoding="utf-8") as fh:
        manifest = json.load(fh)

    fixture_manifest = next(
        record for record in manifest["reference_frames"] if record["fixture"] == "high42-1080p.mp4"
    )
    i_ref = fixture_manifest["frame_references"]["I"]
    fps = next(
        record for record in json.loads((ROOT / "testdata" / "h264" / "fixtures.json").read_text())
        ["fixtures"]
        if record["file"] == "high42-1080p.mp4"
    )
    width = fps["width"]
    height = fps["height"]

    frame_index = _first_i_frame_index(FIXTURE)
    frame = _extract_frame(FIXTURE, frame_index, width, height)

    y_size = width * height
    chroma_size = y_size // 4
    y_plane = frame[:y_size]
    u_plane = frame[y_size : y_size + chroma_size]
    v_plane = frame[y_size + chroma_size :]

    assert _plane_hash(y_plane) == i_ref["planes"]["y"]["sha256"]
    assert _plane_hash(u_plane) == i_ref["planes"]["u"]["sha256"]
    assert _plane_hash(v_plane) == i_ref["planes"]["v"]["sha256"]
