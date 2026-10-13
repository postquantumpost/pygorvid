#!/usr/bin/env python3
"""Generate small CABAC I/P/P smoke fixtures from samples/boss0.mp4."""

import hashlib
import json
import os
import subprocess
import tempfile
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
SAMPLES = ROOT / "samples"
SOURCE = SAMPLES / "boss0.mp4"
SOURCE_HASHES = SAMPLES / "SHA256SUMS"
SIZES = (16, 64)


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as media:
        for block in iter(lambda: media.read(1024 * 1024), b""):
            digest.update(block)
    return digest.hexdigest()


def probe_video(path: Path) -> dict:
    result = subprocess.run(
        [
            "ffprobe",
            "-v",
            "error",
            "-select_streams",
            "v:0",
            "-show_entries",
            "stream=codec_name,profile,width,height,pix_fmt:frame=pict_type",
            "-show_frames",
            "-of",
            "json",
            str(path),
        ],
        check=True,
        capture_output=True,
        text=True,
    )
    return json.loads(result.stdout)


def source_manifest_hash() -> str:
    for line in SOURCE_HASHES.read_text(encoding="ascii").splitlines():
        digest, name = line.split(maxsplit=1)
        if name.strip().lstrip("*") == SOURCE.name:
            return digest
    raise ValueError(f"{SOURCE.name} is not listed in {SOURCE_HASHES.name}")


def main() -> None:
    if not SOURCE.is_file() or sha256_file(SOURCE) != source_manifest_hash():
        raise ValueError(f"{SOURCE}: missing or does not match {SOURCE_HASHES.name}")

    temp_root = ROOT / "tmp"
    temp_root.mkdir(exist_ok=True)
    fixture_specs = [(size, SAMPLES / f"decoder-smoke-{size}x{size}.mp4") for size in SIZES]
    with tempfile.TemporaryDirectory(prefix="decoder-smoke-", dir=temp_root) as temp_name:
        temp_dir = Path(temp_name)
        generated = []
        for size, destination in fixture_specs:
            temp_fixture = temp_dir / destination.name
            video_filter = (
                "select='eq(n,0)+eq(n,30)+eq(n,60)',setpts=N/(1*TB),"
                f"scale={size}:{size}:flags=area"
            )
            subprocess.run(
                [
                    "ffmpeg",
                    "-nostdin",
                    "-v",
                    "error",
                    "-i",
                    str(SOURCE),
                    "-map",
                    "0:v:0",
                    "-an",
                    "-sn",
                    "-dn",
                    "-vf",
                    video_filter,
                    "-fps_mode",
                    "passthrough",
                    "-frames:v",
                    "3",
                    "-c:v",
                    "libx264",
                    "-preset",
                    "ultrafast",
                    "-profile:v",
                    "high",
                    "-level:v",
                    "1.0",
                    "-pix_fmt",
                    "yuv420p",
                    "-x264-params",
                    "cabac=1:bframes=0:keyint=120:min-keyint=120:scenecut=0:ref=1:weightp=0:8x8dct=1:deblock=0",
                    "-movflags",
                    "+faststart",
                    "-y",
                    str(temp_fixture),
                ],
                check=True,
                capture_output=True,
            )

            probe = probe_video(temp_fixture)
            streams = probe.get("streams", [])
            frames = probe.get("frames", [])
            if len(streams) != 1:
                raise ValueError(f"{destination.name}: expected exactly one video stream")
            stream = streams[0]
            if (
                stream.get("codec_name") != "h264"
                or stream.get("profile") != "High"
                or stream.get("width") != size
                or stream.get("height") != size
                or stream.get("pix_fmt") != "yuv420p"
            ):
                raise ValueError(f"{destination.name}: unexpected H.264 stream properties {stream}")
            picture_types = [frame.get("pict_type") for frame in frames]
            if picture_types != ["I", "P", "P"]:
                raise ValueError(f"{destination.name}: expected I/P/P frames, found {picture_types}")
            generated.append((temp_fixture, destination))

        for source, destination in generated:
            os.replace(source, destination)
            print(f"{destination.relative_to(ROOT)} sha256={sha256_file(destination)}")


if __name__ == "__main__":
    main()