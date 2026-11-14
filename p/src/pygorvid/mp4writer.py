import struct
from dataclasses import dataclass, fields
from typing import Optional

from .h264encoder import H264Encoder


@dataclass
class Mp4Metadata:
    title: str = ""
    artist: str = ""
    album_artist: str = ""
    genre: str = ""
    track: str = ""
    disc: str = ""
    date: str = ""
    composer: str = ""
    comment: str = ""
    description: str = ""
    copyright: str = ""
    make: str = ""
    model: str = ""
    firmware: str = ""
    location: str = ""
    altitude: str = ""
    heading: str = ""
    encoder: str = ""


def _metadata_box(metadata: Optional[Mp4Metadata]) -> bytes:
    if metadata is None:
        return b""
    entries = []
    for field in fields(metadata):
        value = getattr(metadata, field.name)
        if not isinstance(value, str):
            raise ValueError(f"MP4 metadata {field.name} must be a string")
        if value:
            entries.append((field.name.encode("ascii"), value.encode("utf-8")))
    if not entries:
        return b""
    keys = _full_box(b"keys", _u32(len(entries)), *(
        _box(b"mdta", key) for key, _ in entries
    ))
    items = _box(b"ilst", *(
        _box(_u32(index), _full_box(b"data", _u32(0), value, flags=1))
        for index, (_, value) in enumerate(entries, 1)
    ))
    handler = _full_box(b"hdlr", _u32(0), b"mdta", bytes(12), b"\0")
    return _full_box(b"meta", handler, keys, items)


def _u32(*values: int) -> bytes:
    return struct.pack(">" + "I" * len(values), *values)


def _u16(*values: int) -> bytes:
    return struct.pack(">" + "H" * len(values), *values)


def _box(kind: bytes, *parts: bytes) -> bytes:
    if sum(len(part) for part in parts) > 0xFFFFFFFF - 8:
        raise ValueError("MP4 box exceeds the 32-bit size limit")
    payload = b"".join(parts)
    return _u32(len(payload) + 8) + kind + payload


def _full_box(kind: bytes, *parts: bytes, flags: int = 0) -> bytes:
    return _box(kind, _u32(flags), *parts)


def _matrix() -> bytes:
    return _u32(0x10000, 0, 0, 0, 0x10000, 0, 0, 0, 0x40000000)


def _track(track_id: int, sample_entry: bytes, sample_sizes: bytes,
           sample_count: int, timescale: int, chunk_offset: int, video: bool) -> bytes:
    tkhd = _full_box(
        b"tkhd", _u32(0, 0, track_id, 0, 1000), bytes(8),
        _u16(0, 0, 0 if video else 0x100, 0), _matrix(),
        _u32(64 << 16 if video else 0, 64 << 16 if video else 0), flags=7,
    )
    mdhd = _full_box(b"mdhd", _u32(0, 0, timescale, sample_count), _u16(0x55C4, 0))
    hdlr = _full_box(
        b"hdlr", _u32(0), b"vide" if video else b"soun", bytes(12),
        b"Video\0" if video else b"Audio\0",
    )
    media_header = (_full_box(b"vmhd", bytes(8), flags=1) if video
                    else _full_box(b"smhd", bytes(4)))
    dinf = _box(b"dinf", _full_box(b"dref", _u32(1), _full_box(b"url ", flags=1)))
    stbl = _box(
        b"stbl",
        _full_box(b"stsd", _u32(1), sample_entry),
        _full_box(b"stts", _u32(1, sample_count, 1)),
        _full_box(b"stsc", _u32(1, 1, sample_count, 1)),
        sample_sizes,
        _full_box(b"stco", _u32(1, chunk_offset)),
    )
    return _box(b"trak", tkhd, _box(b"mdia", mdhd, hdlr, _box(b"minf", media_header, dinf, stbl)))


class Mp4Writer:
    """Initial subset: one second of 64x64 H.264 and stereo PCM silence."""

    def encode(self, encoder: H264Encoder, metadata: Optional[Mp4Metadata] = None) -> bytes:
        metadata_box = _metadata_box(metadata)
        samples = [encoder.encode_grey_frame(index) for index in range(25)]
        # IDR IDs can change the sample size, so retain individual sizes.
        video = b"".join(samples)
        audio = bytes(48000 * 2 * 2)
        sps = encoder.sequence_parameter_set()
        pps = encoder.picture_parameter_set()
        avcc = _box(
            b"avcC", bytes([1, sps[1], sps[2], sps[3], 0xFF, 0xE1]),
            _u16(len(sps)), sps, bytes([1]), _u16(len(pps)), pps,
        )
        visual = _box(
            b"avc1", bytes(6), _u16(1), bytes(16), _u16(64, 64),
            _u32(0x480000, 0x480000, 0), _u16(1), bytes(32), _u16(24, 0xFFFF), avcc,
        )
        sound = _box(
            b"sowt", bytes(6), _u16(1), bytes(8), _u16(2, 16, 0, 0), _u32(48000 << 16),
        )
        ftyp = _box(b"ftyp", b"isom", _u32(0x200), b"isomiso2avc1qt  ")
        video_offset = len(ftyp) + 8
        sizes = _full_box(b"stsz", _u32(0, 25), _u32(*(len(sample) for sample in samples)))
        video_track = _track(1, visual, sizes, 25, 25, video_offset, True)
        audio_sizes = _full_box(b"stsz", _u32(4, 48000))
        audio_track = _track(2, sound, audio_sizes, 48000, 48000, video_offset + len(video), False)
        mvhd = _full_box(
            b"mvhd", _u32(0, 0, 1000, 1000, 0x10000), _u16(0x100, 0),
            bytes(8), _matrix(), bytes(24), _u32(3),
        )
        return ftyp + _box(b"mdat", video, audio) + _box(b"moov", mvhd, video_track, audio_track, metadata_box)
