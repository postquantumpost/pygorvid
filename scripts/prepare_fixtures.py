#!/usr/bin/env python3
"""Create compact stream-copy MP4 fixtures for active H.264 configurations."""

import json
import os
import subprocess
import tempfile
from pathlib import Path

from inventory_streams import (
    HASH_MANIFEST,
    ROOT,
    probe_sample,
    read_manifest,
    sha256_file,
)


OUTPUT_DIR = ROOT / "testdata" / "h264"
MANIFEST = OUTPUT_DIR / "fixtures.json"
MAX_SAMPLE_BYTES = 15_000_000
FRAME_PACKET_COUNT = 48
FIXTURE_SOURCES = (
    {
        "name": "high42-1080p.mp4",
        "source": "samples/boss0.mp4",
        "width": 1920,
        "height": 1080,
        "level": 42,
    },
    {
        "name": "high52-2160p.mp4",
        "source": "samples/samp3.mp4",
        "width": 3840,
        "height": 2160,
        "level": 52,
    },
)


def video_stream(path: Path) -> dict:
    streams = probe_sample(path)["streams"]
    video = [stream for stream in streams if stream.get("codec_type") == "video"]
    if len(video) != 1:
        raise ValueError(f"{path}: expected exactly one video stream")
    return video[0]


def packet_count(path: Path) -> int:
    result = subprocess.run(
        [
            "ffprobe",
            "-v",
            "error",
            "-count_packets",
            "-select_streams",
            "v:0",
            "-show_entries",
            "stream=nb_read_packets",
            "-of",
            "default=noprint_wrappers=1:nokey=1",
            str(path),
        ],
        check=True,
        capture_output=True,
        text=True,
    )
    return int(result.stdout.strip())


def main() -> None:
    source_hashes = read_manifest()
    temp_root = ROOT / "tmp"
    temp_root.mkdir(exist_ok=True)
    OUTPUT_DIR.mkdir(parents=True, exist_ok=True)
    fixture_records = []

    with tempfile.TemporaryDirectory(prefix="silkroad-fixtures-", dir=temp_root) as temp_name:
        temp_dir = Path(temp_name)
        for spec in FIXTURE_SOURCES:
            source = ROOT / spec["source"]
            expected_hash = source_hashes.get(spec["source"])
            if expected_hash is None or sha256_file(source) != expected_hash:
                raise ValueError(f"{spec['source']}: not present or hash differs from {HASH_MANIFEST.name}")
            if source.stat().st_size > MAX_SAMPLE_BYTES:
                raise ValueError(f"{spec['source']}: source exceeds active sample size limit")

            source_stream = video_stream(source)
            temp_fixture = temp_dir / spec["name"]
            subprocess.run(
                [
                    "ffmpeg",
                    "-nostdin",
                    "-v",
                    "error",
                    "-i",
                    str(source),
                    "-map",
                    "0:v:0",
                    "-an",
                    "-sn",
                    "-dn",
                    "-c:v",
                    "copy",
                    "-frames:v",
                    str(FRAME_PACKET_COUNT),
                    "-movflags",
                    "+faststart",
                    "-y",
                    str(temp_fixture),
                ],
                check=True,
                capture_output=True,
            )

            fixture_stream = video_stream(temp_fixture)
            if packet_count(temp_fixture) != FRAME_PACKET_COUNT:
                raise ValueError(f"{spec['name']}: expected {FRAME_PACKET_COUNT} video packets")
            for field in ("codec_name", "profile", "level", "width", "height", "pix_fmt"):
                if fixture_stream.get(field) != source_stream.get(field):
                    raise ValueError(f"{spec['name']}: stream property {field} changed")
            if fixture_stream["width"] != spec["width"] or fixture_stream["height"] != spec["height"]:
                raise ValueError(f"{spec['name']}: unexpected dimensions")
            if fixture_stream["level"] != spec["level"]:
                raise ValueError(f"{spec['name']}: unexpected H.264 level")
            if fixture_stream["codec_configuration"]["sha256"] != source_stream["codec_configuration"]["sha256"]:
                raise ValueError(f"{spec['name']}: AVC configuration changed during stream copy")
            slice_types = fixture_stream["slice_summary"]["slice_type_counts"]
            if not {"I", "P", "B"}.issubset(slice_types):
                raise ValueError(f"{spec['name']}: fixture does not include I, P, and B slices")

            fixture_records.append(
                {
                    "file": spec["name"],
                    "source": spec["source"],
                    "source_sha256": expected_hash,
                    "sha256": sha256_file(temp_fixture),
                    "size_bytes": temp_fixture.stat().st_size,
                    "video_packets": FRAME_PACKET_COUNT,
                    "codec_configuration_sha256": fixture_stream["codec_configuration"]["sha256"],
                    "width": fixture_stream["width"],
                    "height": fixture_stream["height"],
                    "profile": fixture_stream["profile"],
                    "level": fixture_stream["level"],
                    "pix_fmt": fixture_stream["pix_fmt"],
                    "slice_type_counts": slice_types,
                }
            )

        manifest = {
            "format_version": 1,
            "generation_method": "48 video packets copied without re-encoding",
            "fixtures": fixture_records,
        }
        temp_manifest = temp_dir / MANIFEST.name
        temp_manifest.write_text(json.dumps(manifest, indent=2, sort_keys=True) + "\n", encoding="utf-8")

        for record in fixture_records:
            os.replace(temp_dir / record["file"], OUTPUT_DIR / record["file"])
        os.replace(temp_manifest, MANIFEST)

    print(f"Wrote {len(fixture_records)} compact fixtures to {OUTPUT_DIR.relative_to(ROOT)}")


if __name__ == "__main__":
    main()