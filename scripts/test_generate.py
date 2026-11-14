"""Check generated media and CLI behavior for one or more built commands."""

import argparse
import ctypes
import ctypes.util
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


def read_compatibility_metadata(payload: bytes):
    movie = dict(boxes(payload))[b"moov"]
    user_data = dict(boxes(movie))[b"udta"]
    meta = dict(boxes(user_data))[b"meta"]
    assert meta[:4] == bytes(4)
    children = dict(boxes(meta[4:]))
    assert children[b"hdlr"][8:16] == b"mdirappl"
    assert b"keys" not in children
    text = {}
    numbers = {}
    free_form = {}
    for tag, item in boxes(children[b"ilst"]):
        contents = dict(boxes(item))
        data = contents[b"data"]
        if tag in (b"trkn", b"disk"):
            assert data[:8] == bytes(8)
            assert data[8:10] == bytes(2) and data[12:14] == bytes(2)
            assert len(data) == (16 if tag == b"trkn" else 14)
            numbers[tag] = int.from_bytes(data[10:12], "big")
        else:
            assert data[:8] == b"\0\0\0\1\0\0\0\0"
            if tag == b"----":
                assert contents[b"mean"] == bytes(4) + b"com.apple.iTunes"
                assert contents[b"name"][:4] == bytes(4)
                key = contents[b"name"][4:].decode("utf-8")
                assert key not in free_form
                free_form[key] = data[8:].decode("utf-8")
            else:
                assert tag not in text
                text[tag] = data[8:].decode("utf-8")
    return text, numbers, free_form


def read_windows_metadata(payload: bytes) -> dict[str, str]:
    movie = dict(boxes(payload))[b"moov"]
    user_data = dict(boxes(movie))[b"udta"]
    records = dict(boxes(user_data))[b"Xtra"]
    values = {}
    offset = 0
    while offset < len(records):
        assert len(records) - offset >= 20
        size = int.from_bytes(records[offset:offset + 4], "big")
        name_size = int.from_bytes(records[offset + 4:offset + 8], "big")
        assert 20 + name_size <= size <= len(records) - offset
        name_end = offset + 8 + name_size
        name = records[offset + 8:name_end].decode("ascii")
        assert name and "\0" not in name and name not in values
        assert records[name_end:name_end + 4] == b"\0\0\0\1"
        value_size = int.from_bytes(records[name_end + 4:name_end + 8], "big")
        assert size == 12 + name_size + value_size
        assert records[name_end + 8:name_end + 10] == b"\0\10"  # Xtra Unicode type 8.
        data = records[name_end + 10:offset + size]
        assert len(data) >= 2 and len(data) % 2 == 0 and data[-2:] == b"\0\0"
        values[name] = data[:-2].decode("utf-16le")
        offset += size
    assert offset == len(records)
    return values


def without_mdta(payload: bytes) -> bytes:
    def pack_box(tag: bytes, data: bytes) -> bytes:
        return (len(data) + 8).to_bytes(4, "big") + tag + data

    output = bytearray()
    for tag, data in boxes(payload):
        if tag == b"moov":
            data = b"".join(pack_box(child, contents) for child, contents in boxes(data)
                            if child != b"meta")
        output.extend(pack_box(tag, data))
    return bytes(output)


def check_vlc(output: Path, compatibility_only: bool = False) -> None:
    library = ctypes.util.find_library("vlc")
    if not library:
        raise RuntimeError("--check-vlc requires an installed libvlc")
    vlc = ctypes.CDLL(library)
    vlc.libvlc_new.argtypes = [ctypes.c_int, ctypes.POINTER(ctypes.c_char_p)]
    vlc.libvlc_new.restype = ctypes.c_void_p
    vlc.libvlc_media_new_path.argtypes = [ctypes.c_void_p, ctypes.c_char_p]
    vlc.libvlc_media_new_path.restype = ctypes.c_void_p
    vlc.libvlc_media_parse.argtypes = [ctypes.c_void_p]
    vlc.libvlc_media_get_meta.argtypes = [ctypes.c_void_p, ctypes.c_int]
    vlc.libvlc_media_get_meta.restype = ctypes.c_void_p
    vlc.libvlc_free.argtypes = [ctypes.c_void_p]
    vlc.libvlc_media_release.argtypes = [ctypes.c_void_p]
    vlc.libvlc_release.argtypes = [ctypes.c_void_p]
    arguments = (ctypes.c_char_p * 3)(b"--ignore-config", b"--no-video", b"--no-audio")
    instance = vlc.libvlc_new(3, arguments)
    if not instance:
        raise RuntimeError("cannot create VLC instance")
    try:
        media = vlc.libvlc_media_new_path(instance, os.fsencode(output))
        if not media:
            raise RuntimeError("cannot create VLC media")
        try:
            vlc.libvlc_media_parse(media)
            # Public libvlc_meta_t enumeration; parse via VLC's native MP4 demuxer.
            expected = {0: "Title", 1: "Artist", 2: "Genre", 3: "Copyright",
                        5: "1", 8: "2016-12-31 23:59:60 UTC", 6: "Description",
                        14: "pygorvid", 23: "Album Artist", 24: "1"}
            if compatibility_only:
                expected[8] = "2016"  # VLC's legacy tag reader exposes the year.
                expected[6] = "Comments"  # Its description slot maps to the comment.
            for field, value in expected.items():
                pointer = vlc.libvlc_media_get_meta(media, field)
                try:
                    actual = ctypes.string_at(pointer).decode("utf-8") if pointer else None
                    assert actual == value, (output, "VLC", field, actual, value)
                finally:
                    if pointer:
                        vlc.libvlc_free(pointer)
        finally:
            vlc.libvlc_media_release(media)
    finally:
        vlc.libvlc_release(instance)


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
    parser.add_argument("--check-vlc", action="store_true", help="also check tags using installed libvlc")
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
            assert read_windows_metadata(payload) == {
                "Title": "Title", "Author": "Artist", "WM/AlbumArtist": "Album Artist",
                "WM/Genre": "Genre", "WM/TrackNumber": "1", "WM/PartOfSet": "1",
                "WM/OriginalReleaseTime": "2016-12-31 23:59:60 UTC", "WM/Composer": "Composer",
                "Description": "Comments", "WM/SubTitleDescription": "Description",
                "Copyright": "Copyright", "CameraManufacturer": "Camera make",
                "CameraModel": "Camera model", "WM/EncodedBy": "pygorvid", "WM/Year": "2016",
                **{"pygorvid_" + key: value for key, value in EXPECTED_METADATA.items()},
            }
            text, numbers, free_form = read_compatibility_metadata(payload)
            assert free_form == EXPECTED_METADATA
            assert numbers == {b"trkn": 1, b"disk": 1}
            assert text == {
                b"\xa9nam": "Title", b"\xa9ART": "Artist", b"aART": "Album Artist",
                b"\xa9gen": "Genre", b"\xa9day": "2016-12-31 23:59:60 UTC",
                b"\xa9wrt": "Composer", b"\xa9com": "Composer", b"\xa9cmt": "Comments",
                b"desc": "Description", b"cprt": "Copyright", b"\xa9cpy": "Copyright",
                b"\xa9too": "pygorvid",
            }
            if options.check_vlc:
                check_vlc(output)
            compatibility_only = directory / f"compatibility-only-{index}.mp4"
            compatibility_only.write_bytes(without_mdta(payload))
            compatibility_tags = json.loads(run([
                ffprobe, "-v", "error", "-show_entries", "format_tags", "-of", "json",
                str(compatibility_only),
            ]))["format"]["tags"]
            for key, value in EXPECTED_METADATA.items():
                assert compatibility_tags.get(key) == value, (executable, key, compatibility_tags)
            if options.check_vlc:
                check_vlc(compatibility_only, compatibility_only=True)
            compatibility_only.unlink()
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
            suffix = ", VLC metadata" if options.check_vlc else ""
            print(f"{executable}: UTF-8, VLC and Windows Xtra metadata, media, silence, CLI errors and output parity{suffix} passed")
        assert len(list(directory.iterdir())) == 2 * len(options.commands), "temporary files leaked"


if __name__ == "__main__":
    main()
