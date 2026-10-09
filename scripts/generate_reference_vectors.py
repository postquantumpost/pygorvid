#!/usr/bin/env python3
"""Generate decoded-frame hashes and independently checked PNG/JPEG vectors."""

import hashlib
import json
import os
import struct
import subprocess
import tempfile
import zlib
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
FIXTURE_DIR = ROOT / "testdata" / "h264"
FIXTURE_MANIFEST = FIXTURE_DIR / "fixtures.json"
IMAGE_DIR = ROOT / "testdata" / "image-vectors"
OUTPUT = ROOT / "silkroad1-reference-vectors.json"


def sha256(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def run(command: list[str], *, input_data: bytes | None = None) -> bytes:
    result = subprocess.run(
        command,
        input=input_data,
        check=True,
        capture_output=True,
    )
    return result.stdout


def first_frames_by_type(path: Path) -> dict[str, dict]:
    output = run(
        [
            "ffprobe",
            "-v",
            "error",
            "-select_streams",
            "v:0",
            "-show_frames",
            "-show_entries",
            "frame=pict_type,key_frame,best_effort_timestamp_time",
            "-of",
            "json",
            str(path),
        ]
    )
    found = {}
    for index, frame in enumerate(json.loads(output)["frames"]):
        picture_type = frame["pict_type"]
        if picture_type in {"I", "P", "B"} and picture_type not in found:
            found[picture_type] = {
                "index": index,
                "key_frame": bool(int(frame["key_frame"])),
                "pts_seconds": frame.get("best_effort_timestamp_time"),
            }
    missing = {"I", "P", "B"} - set(found)
    if missing:
        raise ValueError(f"{path}: missing picture types {sorted(missing)}")
    return found


def read_exact(stream, size: int) -> bytes:
    chunks = bytearray()
    while len(chunks) < size:
        block = stream.read(size - len(chunks))
        if not block:
            raise ValueError(f"truncated decoded frame ({len(chunks)} of {size} bytes)")
        chunks.extend(block)
    return bytes(chunks)


def hash_reference_planes(path: Path, width: int, height: int, selections: dict) -> dict:
    if width % 2 or height % 2:
        raise ValueError("reference hashing currently requires even yuv420p dimensions")
    frame_size = width * height * 3 // 2
    maximum_index = max(frame["index"] for frame in selections.values())
    filter_expression = f"select=lte(n\\,{maximum_index})"
    process = subprocess.Popen(
        [
            "ffmpeg",
            "-nostdin",
            "-v",
            "error",
            "-i",
            str(path),
            "-map",
            "0:v:0",
            "-vf",
            filter_expression,
            "-fps_mode",
            "passthrough",
            "-frames:v",
            str(maximum_index + 1),
            "-pix_fmt",
            "yuv420p",
            "-f",
            "rawvideo",
            "pipe:1",
        ],
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
    )
    if process.stdout is None:
        raise RuntimeError("failed to capture decoded reference frames")
    by_index = {frame["index"]: (picture_type, frame) for picture_type, frame in selections.items()}
    references = {}
    try:
        for index in range(maximum_index + 1):
            frame_data = read_exact(process.stdout, frame_size)
            if index not in by_index:
                continue
            picture_type, metadata = by_index[index]
            y_size = width * height
            chroma_size = y_size // 4
            planes = {
                "y": frame_data[:y_size],
                "u": frame_data[y_size : y_size + chroma_size],
                "v": frame_data[y_size + chroma_size :],
            }
            references[picture_type] = {
                **metadata,
                "planes": {
                    name: {"bytes": len(plane), "sha256": sha256(plane)}
                    for name, plane in planes.items()
                },
            }
        extra_stdout, stderr = process.communicate()
    except Exception:
        process.kill()
        process.wait()
        raise
    if extra_stdout:
        raise ValueError(f"{path}: decoder emitted unexpected extra frame data")
    if process.returncode:
        raise RuntimeError(f"ffmpeg reference decode failed for {path}: {stderr.decode(errors='replace')}")
    if len(references) != 3:
        raise ValueError(f"{path}: did not capture all I/P/B reference planes")
    return references


def paeth(left: int, above: int, upper_left: int) -> int:
    prediction = left + above - upper_left
    distances = (
        abs(prediction - left),
        abs(prediction - above),
        abs(prediction - upper_left),
    )
    return (left, above, upper_left)[distances.index(min(distances))]


def validate_png(data: bytes, expected_rgb: bytes, width: int, height: int) -> dict:
    if data[:8] != b"\x89PNG\r\n\x1a\n":
        raise ValueError("invalid PNG signature")
    offset = 8
    compressed = bytearray()
    seen_ihdr = False
    seen_iend = False
    while offset < len(data):
        if offset + 12 > len(data):
            raise ValueError("truncated PNG chunk")
        length = struct.unpack(">I", data[offset : offset + 4])[0]
        chunk_type = data[offset + 4 : offset + 8]
        end = offset + 12 + length
        if end > len(data):
            raise ValueError("PNG chunk exceeds file size")
        payload = data[offset + 8 : offset + 8 + length]
        expected_crc = struct.unpack(">I", data[offset + 8 + length : end])[0]
        if zlib.crc32(chunk_type + payload) & 0xFFFFFFFF != expected_crc:
            raise ValueError(f"invalid CRC for PNG {chunk_type!r} chunk")
        if chunk_type == b"IHDR":
            if seen_ihdr or length != 13:
                raise ValueError("invalid PNG IHDR")
            image_width, image_height, depth, color_type, compression, filtering, interlace = struct.unpack(
                ">IIBBBBB", payload
            )
            if (image_width, image_height, depth, color_type, compression, filtering, interlace) != (
                width,
                height,
                8,
                2,
                0,
                0,
                0,
            ):
                raise ValueError("unexpected PNG IHDR values")
            seen_ihdr = True
        elif chunk_type == b"IDAT":
            compressed.extend(payload)
        elif chunk_type == b"IEND":
            if length != 0 or end != len(data):
                raise ValueError("invalid PNG IEND or trailing data")
            seen_iend = True
            break
        offset = end
    if not seen_ihdr or not seen_iend or not compressed:
        raise ValueError("PNG is missing IHDR, IDAT, or IEND")

    decoded = zlib.decompress(compressed)
    row_bytes = width * 3
    if len(decoded) != height * (row_bytes + 1):
        raise ValueError("unexpected decompressed PNG data size")
    previous = bytearray(row_bytes)
    pixels = bytearray()
    cursor = 0
    for _ in range(height):
        filter_type = decoded[cursor]
        cursor += 1
        encoded = decoded[cursor : cursor + row_bytes]
        cursor += row_bytes
        row = bytearray(row_bytes)
        for index, value in enumerate(encoded):
            left = row[index - 3] if index >= 3 else 0
            above = previous[index]
            upper_left = previous[index - 3] if index >= 3 else 0
            if filter_type == 0:
                predictor = 0
            elif filter_type == 1:
                predictor = left
            elif filter_type == 2:
                predictor = above
            elif filter_type == 3:
                predictor = (left + above) // 2
            elif filter_type == 4:
                predictor = paeth(left, above, upper_left)
            else:
                raise ValueError(f"unsupported PNG filter {filter_type}")
            row[index] = (value + predictor) & 0xFF
        pixels.extend(row)
        previous = row
    if bytes(pixels) != expected_rgb:
        raise ValueError("PNG decoded pixels differ from the RGB test vector")
    return {"width": width, "height": height, "decoded_rgb_sha256": sha256(pixels)}


def validate_jpeg(data: bytes, width: int, height: int) -> dict:
    if data[:2] != b"\xff\xd8" or data[-2:] != b"\xff\xd9":
        raise ValueError("JPEG is missing SOI or EOI")
    offset = 2
    saw_dqt = False
    saw_dht = False
    saw_sof = False
    saw_sos = False
    scan_started = False
    while offset < len(data) - 2:
        if scan_started:
            while offset < len(data) - 1:
                if data[offset] != 0xFF:
                    offset += 1
                    continue
                marker_start = offset
                while offset < len(data) and data[offset] == 0xFF:
                    offset += 1
                if offset >= len(data):
                    raise ValueError("truncated JPEG marker in scan data")
                marker = data[offset]
                offset += 1
                if marker == 0x00 or 0xD0 <= marker <= 0xD7:
                    continue
                if marker == 0xD9:
                    offset = marker_start
                    scan_started = False
                    break
                offset = marker_start
                scan_started = False
                break
            if data[offset : offset + 2] == b"\xff\xd9":
                break
        if data[offset] != 0xFF:
            raise ValueError("invalid JPEG marker alignment")
        while offset < len(data) and data[offset] == 0xFF:
            offset += 1
        if offset >= len(data):
            raise ValueError("truncated JPEG marker")
        marker = data[offset]
        offset += 1
        if marker in {0xD8, 0xD9} or 0xD0 <= marker <= 0xD7:
            if marker == 0xD9:
                break
            continue
        if offset + 2 > len(data):
            raise ValueError("truncated JPEG segment length")
        segment_length = struct.unpack(">H", data[offset : offset + 2])[0]
        if segment_length < 2 or offset + segment_length > len(data):
            raise ValueError("invalid JPEG segment length")
        payload = data[offset + 2 : offset + segment_length]
        if marker == 0xDB:
            saw_dqt = True
        elif marker == 0xC4:
            saw_dht = True
        elif marker == 0xC0:
            if len(payload) < 6:
                raise ValueError("truncated JPEG SOF0")
            precision = payload[0]
            actual_height, actual_width = struct.unpack(">HH", payload[1:5])
            components = payload[5]
            if (precision, actual_width, actual_height, components) != (8, width, height, 3):
                raise ValueError("unexpected JPEG baseline frame header")
            saw_sof = True
        elif marker == 0xDA:
            saw_sos = True
            scan_started = True
        offset += segment_length
    if not (saw_dqt and saw_dht and saw_sof and saw_sos):
        raise ValueError("JPEG is missing required baseline tables or scan markers")
    if offset != len(data) - 2:
        raise ValueError("unexpected bytes after JPEG end marker")
    return {"width": width, "height": height, "baseline": True}


def rgb_vectors() -> list[dict]:
    colors = [
        (0, 0, 0),
        (255, 255, 255),
        (255, 0, 0),
        (0, 255, 0),
        (0, 0, 255),
        (255, 255, 0),
        (0, 255, 255),
        (255, 0, 255),
    ]
    checker = bytes(channel for color in (colors[0], colors[2], colors[3], colors[4]) for channel in color)
    bars = bytes(channel for y in range(8) for x in range(8) for channel in colors[(x + y) % len(colors)])
    return [
        {"name": "primary-checker-2x2", "width": 2, "height": 2, "rgb": checker},
        {"name": "color-bars-8x8", "width": 8, "height": 8, "rgb": bars},
    ]


def generate_image_vector(vector: dict, temp_dir: Path, output_dir: Path) -> dict:
    width = vector["width"]
    height = vector["height"]
    pixels = vector["rgb"]
    if len(pixels) != width * height * 3:
        raise ValueError(f"{vector['name']}: invalid RGB vector size")
    record = {
        "name": vector["name"],
        "width": width,
        "height": height,
        "rgb_hex": pixels.hex(),
        "rgb_sha256": sha256(pixels),
        "encodings": {},
    }
    for extension in ("png", "jpg"):
        temporary = temp_dir / f"{vector['name']}.{extension}"
        command = [
            "ffmpeg",
            "-nostdin",
            "-v",
            "error",
            "-f",
            "rawvideo",
            "-pixel_format",
            "rgb24",
            "-video_size",
            f"{width}x{height}",
            "-framerate",
            "1",
            "-i",
            "pipe:0",
            "-frames:v",
            "1",
        ]
        if extension == "jpg":
            command.extend(["-q:v", "2"])
        command.extend(["-y", str(temporary)])
        subprocess.run(command, input=pixels, check=True, capture_output=True)
        encoded = temporary.read_bytes()
        if extension == "png":
            validation = validate_png(encoded, pixels, width, height)
        else:
            validation = validate_jpeg(encoded, width, height)
        target = output_dir / temporary.name
        os.replace(temporary, target)
        record["encodings"][extension] = {
            "file": target.relative_to(ROOT).as_posix(),
            "bytes": len(encoded),
            "sha256": sha256(encoded),
            "structural_validation": validation,
        }
    return record


def main() -> None:
    fixture_manifest = json.loads((FIXTURE_DIR / "fixtures.json").read_text(encoding="utf-8"))
    image_dir = ROOT / "testdata" / "image-vectors"
    image_dir.mkdir(parents=True, exist_ok=True)
    temp_root = ROOT / "tmp"
    temp_root.mkdir(exist_ok=True)

    document = {
        "format_version": 1,
        "generation_tools": {
            "ffprobe": subprocess.run(
                ["ffprobe", "-version"], check=True, capture_output=True, text=True
            ).stdout.splitlines()[0],
            "ffmpeg": subprocess.run(
                ["ffmpeg", "-version"], check=True, capture_output=True, text=True
            ).stdout.splitlines()[0],
        },
        "fixture_source": "testdata/h264/fixtures.json",
        "reference_frames": [],
        "image_vectors": [],
    }

    with tempfile.TemporaryDirectory(prefix="silkroad-references-", dir=temp_root) as temp_name:
        temp_dir = Path(temp_name)
        for fixture in fixture_manifest["fixtures"]:
            path = FIXTURE_DIR / fixture["file"]
            frames = first_frames_by_type(path)
            references = hash_reference_planes(path, fixture["width"], fixture["height"], frames)
            document["reference_frames"].append(
                {
                    "fixture": fixture["file"],
                    "codec_configuration_sha256": fixture["codec_configuration_sha256"],
                    "width": fixture["width"],
                    "height": fixture["height"],
                    "pix_fmt": fixture["pix_fmt"],
                    "frame_references": references,
                }
            )

        document["image_vectors"] = [
            generate_image_vector(vector, temp_dir, image_dir) for vector in rgb_vectors()
        ]
        manifest_temp = temp_dir / OUTPUT.name
        manifest_temp.write_text(json.dumps(document, indent=2, sort_keys=True) + "\n", encoding="utf-8")
        os.replace(manifest_temp, OUTPUT)

    print(f"Wrote frame and PNG/JPEG references to {OUTPUT.relative_to(ROOT)}")


if __name__ == "__main__":
    main()