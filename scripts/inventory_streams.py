#!/usr/bin/env python3
"""Build a stream and codec-configuration inventory for the bundled MP4 corpus."""

import hashlib
import json
import re
import subprocess
from collections import Counter, deque
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
SAMPLE_DIR = ROOT / "samples"
HASH_MANIFEST = ROOT / "silkroad1.sha256"
OUTPUT = ROOT / "silkroad1-streams.json"
MAX_SAMPLE_BYTES = 15_000_000
STREAM_FIELDS = (
    "index",
    "codec_type",
    "codec_name",
    "codec_tag_string",
    "profile",
    "level",
    "has_b_frames",
    "refs",
    "width",
    "height",
    "pix_fmt",
    "bits_per_raw_sample",
    "sample_aspect_ratio",
    "field_order",
    "color_range",
    "color_space",
    "color_transfer",
    "color_primaries",
    "chroma_location",
    "sample_rate",
    "channels",
    "channel_layout",
    "avg_frame_rate",
    "r_frame_rate",
    "time_base",
    "extradata_size",
    "extradata",
)
CHROMA_FORMATS = {
    "410": "4:1:0",
    "411": "4:1:1",
    "420": "4:2:0",
    "422": "4:2:2",
    "440": "4:4:0",
    "444": "4:4:4",
}


class BitReader:
    def __init__(self, data: bytes) -> None:
        self.data = data
        self.bit_offset = 0

    def read_bit(self) -> int:
        if self.bit_offset >= len(self.data) * 8:
            raise ValueError("truncated PPS bitstream")
        byte = self.data[self.bit_offset // 8]
        bit = (byte >> (7 - self.bit_offset % 8)) & 1
        self.bit_offset += 1
        return bit

    def read_ue(self) -> int:
        leading_zeros = 0
        while self.read_bit() == 0:
            leading_zeros += 1
            if leading_zeros > 31:
                raise ValueError("oversized Exp-Golomb value in PPS")
        suffix = 0
        for _ in range(leading_zeros):
            suffix = (suffix << 1) | self.read_bit()
        return (1 << leading_zeros) - 1 + suffix


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as sample:
        for block in iter(lambda: sample.read(1024 * 1024), b""):
            digest.update(block)
    return digest.hexdigest()


def read_manifest() -> dict[str, str]:
    manifest = {}
    for line in HASH_MANIFEST.read_text(encoding="ascii").splitlines():
        digest, relative_path = line.split("  ", 1)
        manifest[relative_path] = digest
    return manifest


def parse_extradata(dump: str) -> bytes:
    words = []
    for line in dump.splitlines():
        if ":" not in line:
            continue
        hex_part = line.split(":", 1)[1].split("  ", 1)[0]
        line_words = hex_part.split()
        if all(re.fullmatch(r"[0-9a-fA-F]+", word) and len(word) % 2 == 0 for word in line_words):
            words.extend(line_words)
    return bytes.fromhex("".join(words))


def remove_emulation_prevention(data: bytes) -> bytes:
    output = bytearray()
    zero_count = 0
    for byte in data:
        if zero_count >= 2 and byte == 3:
            zero_count = 0
            continue
        output.append(byte)
        zero_count = zero_count + 1 if byte == 0 else 0
    return bytes(output)


def read_avcc_nal(data: bytes, offset: int) -> tuple[bytes, int]:
    if offset + 2 > len(data):
        raise ValueError("truncated AVC configuration NAL length")
    size = int.from_bytes(data[offset : offset + 2], "big")
    offset += 2
    end = offset + size
    if size == 0 or end > len(data):
        raise ValueError("invalid AVC configuration NAL size")
    return data[offset:end], end


def parse_pps_entropy_mode(nal: bytes) -> dict:
    if not nal or nal[0] & 0x1F != 8:
        raise ValueError("AVC configuration contains a non-PPS in its PPS list")
    rbsp = remove_emulation_prevention(nal[1:])
    bits = BitReader(rbsp)
    pps_id = bits.read_ue()
    sps_id = bits.read_ue()
    entropy_mode = "cabac" if bits.read_bit() else "cavlc"
    return {
        "pic_parameter_set_id": pps_id,
        "seq_parameter_set_id": sps_id,
        "entropy_coding_mode": entropy_mode,
    }


def parse_avcc(data: bytes) -> dict:
    if len(data) < 7 or data[0] != 1:
        raise ValueError("invalid AVCDecoderConfigurationRecord")
    nal_length_size = (data[4] & 0x03) + 1
    sps_count = data[5] & 0x1F
    offset = 6
    for _ in range(sps_count):
        _, offset = read_avcc_nal(data, offset)
    if offset >= len(data):
        raise ValueError("AVC configuration has no PPS count")
    pps_count = data[offset]
    offset += 1
    picture_parameter_sets = []
    for _ in range(pps_count):
        nal, offset = read_avcc_nal(data, offset)
        picture_parameter_sets.append(parse_pps_entropy_mode(nal))
    return {
        "nal_length_size": nal_length_size,
        "sps_count": sps_count,
        "pps_count": pps_count,
        "picture_parameter_sets": picture_parameter_sets,
        "entropy_coding_modes": sorted(
            {pps["entropy_coding_mode"] for pps in picture_parameter_sets}
        ),
    }


def pixel_properties(stream: dict) -> None:
    pixel_format = stream.get("pix_fmt", "")
    match = re.search(r"(?:yuvj?|yuva)(420|422|444|440|411|410)p", pixel_format)
    if match:
        stream["chroma_format"] = CHROMA_FORMATS[match.group(1)]

    raw_depth = stream.get("bits_per_raw_sample")
    if raw_depth and raw_depth.isdigit():
        stream["bit_depth"] = int(raw_depth)
    else:
        depth_match = re.search(r"p(\d+)(?:le|be)?$", pixel_format)
        if depth_match:
            stream["bit_depth"] = int(depth_match.group(1))
        elif pixel_format.endswith("p"):
            stream["bit_depth"] = 8


def slice_summary(path: Path, video_ordinal: int) -> dict:
    process = subprocess.Popen(
        [
            "ffmpeg",
            "-nostdin",
            "-hide_banner",
            "-loglevel",
            "trace",
            "-i",
            str(path),
            "-map",
            f"0:v:{video_ordinal}",
            "-an",
            "-sn",
            "-dn",
            "-c:v",
            "copy",
            "-bsf:v",
            "trace_headers",
            "-f",
            "null",
            "-",
        ],
        stdout=subprocess.DEVNULL,
        stderr=subprocess.PIPE,
        text=True,
        bufsize=1,
    )
    if process.stderr is None:
        raise RuntimeError("failed to capture FFmpeg trace output")

    slice_type_names = {0: "P", 1: "B", 2: "I", 3: "SP", 4: "SI"}
    slice_counts = Counter()
    nal_type_counts = Counter()
    recent_lines = deque(maxlen=20)
    waiting_for_slice_value = False
    try:
        for line in process.stderr:
            recent_lines.append(line.rstrip())
            if waiting_for_slice_value:
                match = re.search(r"=\s*(\d+)\s*$", line)
                if match is None:
                    raise ValueError(f"could not parse slice_type value: {line.rstrip()}")
                value = int(match.group(1))
                slice_counts[slice_type_names[value % 5]] += 1
                waiting_for_slice_value = False
            if "[trace_headers @ " in line:
                nal = re.search(r"nal_unit_type:\s*(\d+)", line)
                if nal is not None:
                    nal_type_counts[int(nal.group(1))] += 1
                if re.search(r"\bslice_type\b", line):
                    match = re.search(r"\bslice_type\b.*?=\s*(\d+)\s*$", line)
                    if match is None:
                        waiting_for_slice_value = True
                    else:
                        value = int(match.group(1))
                        slice_counts[slice_type_names[value % 5]] += 1
        return_code = process.wait()
    except Exception:
        process.kill()
        process.wait()
        raise

    if return_code != 0:
        details = "\n".join(recent_lines)
        raise RuntimeError(f"ffmpeg trace_headers failed for {path}:\n{details}")
    if waiting_for_slice_value:
        raise ValueError(f"truncated slice_type value in trace for {path}")
    if not slice_counts:
        raise ValueError(f"no H.264 slice headers found in {path}")
    return {
        "slice_type_counts": dict(sorted(slice_counts.items())),
        "nal_unit_type_counts": dict(sorted(nal_type_counts.items())),
    }


def probe_sample(path: Path) -> dict:
    relative_path = path.relative_to(ROOT).as_posix()
    result = subprocess.run(
        [
            "ffprobe",
            "-v",
            "error",
            "-show_data",
            "-show_entries",
            "stream=" + ",".join(STREAM_FIELDS),
            "-of",
            "json",
            str(path),
        ],
        check=True,
        capture_output=True,
        text=True,
    )
    streams = json.loads(result.stdout).get("streams", [])
    video_ordinal = 0
    for stream in streams:
        dump = stream.pop("extradata", None)
        if dump is not None:
            data = parse_extradata(dump)
            reported_size = stream.get("extradata_size")
            if reported_size is not None and len(data) != reported_size:
                raise ValueError(
                    f"{relative_path} stream {stream.get('index')}: "
                    f"extradata size mismatch ({len(data)} != {reported_size})"
                )
            configuration = {
                "bytes": len(data),
                "sha256": hashlib.sha256(data).hexdigest(),
                "hex": data.hex(),
            }
            if stream.get("codec_name") == "h264":
                configuration.update(parse_avcc(data))
            stream["codec_configuration"] = configuration
        if stream.get("codec_type") == "video":
            pixel_properties(stream)
            stream["slice_summary"] = slice_summary(path, video_ordinal)
            video_ordinal += 1
    return {"file": relative_path, "streams": streams}


def main() -> None:
    expected = read_manifest()
    source_samples = sorted(SAMPLE_DIR.glob("*.mp4"))
    samples = []
    excluded_samples = []
    for path in source_samples:
        size = path.stat().st_size
        if size > MAX_SAMPLE_BYTES:
            excluded_samples.append(
                {
                    "file": path.relative_to(ROOT).as_posix(),
                    "size_bytes": size,
                    "reason": f"exceeds {MAX_SAMPLE_BYTES}-byte working limit",
                }
            )
        else:
            samples.append(path)
    actual_paths = {path.relative_to(ROOT).as_posix() for path in samples}
    if actual_paths != set(expected):
        missing = sorted(set(expected) - actual_paths)
        unlisted = sorted(actual_paths - set(expected))
        raise SystemExit(f"sample manifest mismatch; missing={missing}, unlisted={unlisted}")

    inventory = []
    for path in samples:
        relative_path = path.relative_to(ROOT).as_posix()
        actual_hash = sha256_file(path)
        if actual_hash != expected[relative_path]:
            raise SystemExit(f"{relative_path}: hash differs from {HASH_MANIFEST.name}")
        inventory.append(probe_sample(path))

    ffprobe_version = subprocess.run(
        ["ffprobe", "-version"], check=True, capture_output=True, text=True
    ).stdout.splitlines()[0]
    ffmpeg_version = subprocess.run(
        ["ffmpeg", "-version"], check=True, capture_output=True, text=True
    ).stdout.splitlines()[0]
    document = {
        "format_version": 1,
        "generation_tools": {
            "ffprobe": ffprobe_version,
            "ffmpeg": ffmpeg_version,
        },
        "slice_summary_method": "FFmpeg trace_headers bitstream filter with video stream copy; no frame decoding",
        "hash_manifest": HASH_MANIFEST.name,
        "sample_size_limit_bytes": MAX_SAMPLE_BYTES,
        "source_sample_count": len(source_samples),
        "sample_count": len(inventory),
        "excluded_sample_count": len(excluded_samples),
        "excluded_samples": excluded_samples,
        "samples": inventory,
    }
    OUTPUT.write_text(json.dumps(document, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    print(f"Wrote {len(inventory)} sample records to {OUTPUT.relative_to(ROOT)}")


if __name__ == "__main__":
    main()