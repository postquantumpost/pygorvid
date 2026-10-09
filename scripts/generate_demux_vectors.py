#!/usr/bin/env python3
"""Generate packet-level demux expectations from the active MP4 corpus."""

import hashlib
import json
import subprocess
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
HASH_MANIFEST = ROOT / "silkroad1.sha256"
STREAM_INVENTORY = ROOT / "silkroad1-streams.json"
OUTPUT = ROOT / "silkroad1-demux.tsv"
MAX_SAMPLE_BYTES = 15_000_000


def read_hash_manifest() -> dict[str, str]:
    entries = {}
    for line in HASH_MANIFEST.read_text(encoding="ascii").splitlines():
        digest, relative_path = line.split("  ", 1)
        entries[relative_path] = digest
    return entries


def parameter_sets_hex(configuration: bytes) -> str:
    if len(configuration) < 7 or configuration[0] != 1:
        raise ValueError("invalid AVC configuration record")
    offset = 6
    parameter_sets = []
    for _ in range(configuration[5] & 0x1F):
        parameter_set, offset = read_avcc_nal(configuration, offset)
        parameter_sets.append(parameter_set)
    if offset >= len(configuration):
        raise ValueError("AVC configuration has no PPS count")
    picture_count = configuration[offset]
    offset += 1
    for _ in range(picture_count):
        parameter_set, offset = read_avcc_nal(configuration, offset)
        parameter_sets.append(parameter_set)
    encoded = bytearray()
    for parameter_set in parameter_sets:
        encoded.extend(len(parameter_set).to_bytes(4, "big"))
        encoded.extend(parameter_set)
    return encoded.hex()


def read_avcc_nal(data: bytes, offset: int) -> tuple[bytes, int]:
    if offset + 2 > len(data):
        raise ValueError("truncated AVC configuration NAL length")
    size = int.from_bytes(data[offset : offset + 2], "big")
    offset += 2
    end = offset + size
    if size == 0 or end > len(data):
        raise ValueError("invalid AVC configuration NAL size")
    return data[offset:end], end


def probe_packets(path: Path) -> list[dict]:
    result = subprocess.run(
        [
            "ffprobe",
            "-v",
            "error",
            "-ignore_editlist",
            "1",
            "-select_streams",
            "v:0",
            "-show_packets",
            "-show_entries",
            "packet=pts,dts,duration,pos,size,flags",
            "-of",
            "json",
            str(path),
        ],
        check=True,
        capture_output=True,
        text=True,
    )
    packets = json.loads(result.stdout).get("packets", [])
    if not packets:
        raise ValueError(f"no video packets found in {path}")
    for index, packet in enumerate(packets):
        required = ("pts", "dts", "duration", "pos", "size", "flags")
        if any(packet.get(field) is None for field in required):
            raise ValueError(f"packet {index} in {path} is missing demux metadata")
    return packets


def main() -> None:
    expected_hashes = read_hash_manifest()
    inventory = json.loads(STREAM_INVENTORY.read_text(encoding="utf-8"))
    records = inventory["samples"]
    inventory_paths = {record["file"] for record in records}
    if inventory_paths != set(expected_hashes):
        raise SystemExit("stream inventory and active hash manifest differ")

    lines = [
        "# Silkroad demux vectors v1; ffprobe -ignore_editlist 1; DTS rebased to first packet"
    ]
    for record in records:
        relative_path = record["file"]
        path = ROOT / relative_path
        if path.stat().st_size > MAX_SAMPLE_BYTES:
            raise SystemExit(f"{relative_path}: exceeds active sample size limit")
        digest = hashlib.sha256(path.read_bytes()).hexdigest()
        if digest != expected_hashes[relative_path]:
            raise SystemExit(f"{relative_path}: hash differs from {HASH_MANIFEST.name}")
        video_streams = [
            stream for stream in record["streams"] if stream["codec_type"] == "video"
        ]
        if not video_streams or video_streams[0]["codec_name"] != "h264":
            raise SystemExit(f"{relative_path}: first video stream is not H.264")
        configuration_hex = video_streams[0]["codec_configuration"]["hex"]
        parameter_hex = parameter_sets_hex(bytes.fromhex(configuration_hex))
        packets = probe_packets(path)
        dts_origin = int(packets[0]["dts"])
        lines.append(f"S\t{relative_path}\t{len(packets)}\t{parameter_hex}")
        for index, packet in enumerate(packets):
            position = int(packet["pos"])
            size = int(packet["size"])
            if position < 0 or size <= 0 or position + size > path.stat().st_size:
                raise ValueError(f"packet {index} in {relative_path} has an invalid byte range")
            fields = (
                "P",
                relative_path,
                str(index),
                str(position),
                str(size),
                str(int(packet["dts"]) - dts_origin),
                str(packet["pts"]),
                str(packet["duration"]),
                str(int("K" in packet["flags"])),
            )
            lines.append("\t".join(fields))

    OUTPUT.write_text("\n".join(lines) + "\n", encoding="ascii")
    print(f"Wrote packet vectors for {len(records)} active samples to {OUTPUT.name}")


if __name__ == "__main__":
    main()