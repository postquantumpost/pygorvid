"""Check generated media and CLI behavior for one or more built commands."""

import argparse
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile


EXPECTED_METADATA = {
    "title": "Title", "artist": "Artist", "album_artist": "Album Artist", "genre": "Genre",
    "track": "1", "disc": "1", "date": "2016-12-31 23:59:60 UTC", "composer": "Composer",
    "comment": "Comments", "description": "Description", "copyright": "Copyright",
    "make": "Camera make", "model": "Camera model", "firmware": "firmware version",
    "location": "11.3733\u00b0 N 142.5917\u00b0 E", "altitude": "-10925.0 meters",
    "heading": "180.0", "encoder": "pygorvid",
}


def boxes(data: bytes):
    offset = 0
    while offset < len(data):
        assert len(data) - offset >= 8
        size = int.from_bytes(data[offset:offset + 4], "big")
        assert 8 <= size <= len(data) - offset
        yield data[offset + 4:offset + 8], data[offset + 8:offset + size]
        offset += size


def read_metadata(payload: bytes) -> dict[str, str]:
    movie = dict(boxes(payload))[b"moov"]
    meta = dict(boxes(movie))[b"meta"]
    assert meta[:4] == bytes(4)  # FullBox version and flags.
    children = dict(boxes(meta[4:]))
    handler = children[b"hdlr"]
    assert handler[8:12] == b"mdta"
    keys_box = children[b"keys"]
    assert keys_box[:4] == bytes(4)
    count = int.from_bytes(keys_box[4:8], "big")
    keys = []
    for namespace, key in boxes(keys_box[8:]):
        assert namespace == b"mdta"
        keys.append(key.decode("utf-8"))
    assert count == len(keys)
    values = {}
    for index_bytes, item in boxes(children[b"ilst"]):
        index = int.from_bytes(index_bytes, "big")
        assert 1 <= index <= count
        contents = list(boxes(item))
        assert len(contents) == 1 and contents[0][0] == b"data"
        data = contents[0][1]
        assert data[:8] == b"\0\0\0\1\0\0\0\0"  # UTF-8 type 1, locale 0.
        key = keys[index - 1]
        assert key not in values
        values[key] = data[8:].decode("utf-8")
    assert len(values) == count
    return values


def run(command: list[str], expected_status: int = 0) -> bytes:
    result = subprocess.run(command, capture_output=True, timeout=10)
    assert result.returncode == expected_status, (
        command, result.returncode, result.stdout.decode(), result.stderr.decode()
    )
    if expected_status:
        assert result.stderr, f"missing error message: {command}"
    return result.stdout


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("commands", nargs="+", help="paths to built vidprobe commands or p/pygorvid")
    options = parser.parse_args()
    ffprobe = shutil.which("ffprobe")
    ffmpeg = shutil.which("ffmpeg")
    if not ffprobe or not ffmpeg:
        parser.error("media validation requires ffprobe and ffmpeg (not generation dependencies)")
    os.environ["PATH"] = str(Path(sys.executable).parent) + os.pathsep + os.environ.get("PATH", "")
    root = Path(__file__).resolve().parents[1]
    (root / "tmp").mkdir(exist_ok=True)
    with tempfile.TemporaryDirectory(prefix="generate-test-", dir=root / "tmp") as temporary:
        directory = Path(temporary)
        reference = None
        for index, executable in enumerate(options.commands):
            executable = str(Path(executable).resolve())
            output = directory / f"grey video {index}.MP4"
            run([executable, "--generate", str(output)])
            payload = output.read_bytes()
            assert read_metadata(payload) == EXPECTED_METADATA
            if reference is None:
                reference = payload
            assert payload == reference, f"output differs across languages: {executable}"
            run([executable, f"--generate={output}"])
            assert output.read_bytes() == payload
            for prefix in ("native_", ""):
                requested = directory / f"prefixed video {index}.MP4"
                written = requested.with_name(prefix + requested.name)
                message = run([executable, f"--generate={requested}", "--prefix", prefix])
                assert str(written).encode() in message
                assert written.read_bytes() == payload
                assert not prefix or not requested.exists()
                written.unlink()
                written.mkdir()
                run([executable, "--generate", str(requested), f"--prefix={prefix}"], 1)
                assert written.is_dir()
                written.rmdir()
            metadata = json.loads(run([
                ffprobe, "-v", "error", "-show_streams", "-show_format", "-of", "json", str(output),
            ]))
            tags = metadata["format"]["tags"]
            for key, value in EXPECTED_METADATA.items():
                assert tags.get(key) == value, (executable, key, tags.get(key), value)
            assert len(metadata["streams"]) == 2
            video, audio = metadata["streams"]
            assert video["codec_type"] == "video" and video["codec_name"] == "h264"
            assert video["profile"] == "Constrained Baseline" and video["level"] == 20
            assert (video["width"], video["height"]) == (64, 64)
            assert video["r_frame_rate"] == "25/1" and video["nb_frames"] == "25"
            assert video["pix_fmt"] == "yuv420p"
            assert audio["codec_type"] == "audio" and audio["codec_name"] == "pcm_s16le"
            assert audio["channels"] == 2 and audio["sample_rate"] == "48000"
            assert audio["nb_frames"] == "48000"
            for stream in (video, audio):
                assert float(stream["start_time"]) == 0
                assert float(stream["duration"]) == 1
            assert float(metadata["format"]["duration"]) == 1
            decoded = run([
                ffmpeg, "-v", "error", "-xerror", "-i", str(output), "-map", "0:v:0",
                "-pix_fmt", "yuv420p", "-f", "rawvideo", "-",
            ])
            assert decoded == bytes([128]) * (25 * 64 * 64 * 3 // 2)
            silence = run([
                ffmpeg, "-v", "error", "-xerror", "-i", str(output), "-map", "0:a:0",
                "-acodec", "pcm_s16le", "-f", "s16le", "-",
            ])
            assert silence == bytes(48000 * 2 * 2)
            run([executable, str(output)])  # Native metadata reader accepts both tracks.
            for arguments in (
                ["--generate"], ["--generate="], ["--generate", "bad.avi"],
                ["--generate", str(output), "input.mp4"],
                ["--generate", str(output), "--input", "input.mp4"],
                ["--generate", str(output), "--extractframe", "0"],
                ["--generate", str(output), "--output", ""],
                ["--generate", str(output), "--readall"],
            ):
                run([executable, *arguments], 2)
                assert output.read_bytes() == payload
            blocked = directory / f"directory-{index}.mp4"
            blocked.mkdir()
            run([executable, "--generate", str(blocked)], 1)
            assert blocked.is_dir()
            run([executable, "--generate", str(directory / "missing" / "video.mp4")], 1)
            assert output.read_bytes() == payload
            print(f"{executable}: UTF-8 metadata, media, silence, CLI errors and output parity passed")
        assert len(list(directory.iterdir())) == 2 * len(options.commands), "temporary files leaked"


if __name__ == "__main__":
    main()
