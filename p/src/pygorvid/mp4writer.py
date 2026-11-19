import struct
from dataclasses import dataclass, fields
from typing import Optional, Sequence

from .h264encoder import H264Encoder

_VIDEO_FRAME_RATE = 60
_MIN_VIDEO_FRAME_COUNT = _VIDEO_FRAME_RATE
_GENERATED_FEATURE_FRAME_COUNT = 31
_AUDIO_SAMPLE_RATE = 48000


@dataclass
class Mp4Metadata:
    title: str = ""
    artist: str = ""
    album: str = ""
    publisher: str = ""
    language: str = ""
    now_playing: str = ""
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


def _compatibility_metadata_box(metadata: Optional[Mp4Metadata]) -> bytes:
    if metadata is None:
        return b""
    entries = []
    for field in fields(metadata):
        value = getattr(metadata, field.name)
        if value:
            entries.append((field.name, value.encode("utf-8")))
    if not entries:
        return b""
    text_tags = (
        (b"\xa9nam", metadata.title), (b"\xa9ART", metadata.artist),
        (b"aART", metadata.album_artist), (b"\xa9gen", metadata.genre),
        (b"\xa9day", metadata.date), (b"\xa9wrt", metadata.composer),
        (b"\xa9com", metadata.composer), (b"\xa9cmt", metadata.comment),
        (b"desc", metadata.description), (b"cprt", metadata.copyright),
        (b"\xa9cpy", metadata.copyright), (b"\xa9too", metadata.encoder),
    )
    items = [
        _box(tag, _full_box(b"data", _u32(0), value.encode("utf-8"), flags=1))
        for tag, value in text_tags if value
    ]
    for tag, value, trailing in ((b"trkn", metadata.track, True), (b"disk", metadata.disc, False)):
        # Arbitrary string values still survive in mdta and free-form tags.
        if value and len(value) <= 5 and all("0" <= char <= "9" for char in value):
            number = int(value)
            if number <= 65535:
                data = _u16(0, number, 0) + (bytes(2) if trailing else b"")
                items.append(_box(tag, _full_box(b"data", _u32(0), data)))
    items.extend(
        _box(
            b"----", _full_box(b"mean", b"com.apple.iTunes"),
            _full_box(b"name", key.encode("ascii")), _full_box(b"data", _u32(0), value, flags=1),
        )
        for key, value in entries
    )
    handler = _full_box(b"hdlr", _u32(0), b"mdirappl", bytes(8), b"\0")
    return _box(b"udta", _full_box(b"meta", handler, _box(b"ilst", *items)),
                _windows_metadata_box(metadata))


def _windows_metadata_box(metadata: Optional[Mp4Metadata]) -> bytes:
    if metadata is None:
        return b""
    attributes = [
        ("Title", metadata.title), ("Author", metadata.artist),
        ("WM/AlbumArtist", metadata.album_artist), ("WM/Genre", metadata.genre),
        ("WM/TrackNumber", metadata.track), ("WM/PartOfSet", metadata.disc),
        ("WM/OriginalReleaseTime", metadata.date), ("WM/Composer", metadata.composer),
        ("Description", metadata.comment), ("WM/SubTitleDescription", metadata.description),
        ("Copyright", metadata.copyright), ("CameraManufacturer", metadata.make),
        ("CameraModel", metadata.model), ("WM/EncodedBy", metadata.encoder),
    ]
    year = metadata.date[:4]
    if len(year) == 4 and all("0" <= char <= "9" for char in year) and (
        len(metadata.date) == 4 or metadata.date[4] == "-"
    ):
        attributes.append(("WM/Year", year))
    attributes.extend(("pygorvid_" + field.name, getattr(metadata, field.name))
                      for field in fields(metadata))
    records = []
    for name, value in attributes:
        if not value:
            continue
        name_bytes = name.encode("ascii")
        data = value.encode("utf-16le") + bytes(2)
        value_size = len(data) + 6
        record_size = 12 + len(name_bytes) + value_size
        if record_size > 0xFFFFFFFF:
            raise ValueError("MP4 Xtra record exceeds the 32-bit size limit")
        records.append(_u32(record_size, len(name_bytes)) + name_bytes +
                       _u32(1, value_size) + _u16(8) + data)
    return _box(b"Xtra", *records) if records else b""


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
           sample_count: int, timescale: int, duration_ms: int,
           chunk_offset: int, video: bool, sync_samples: Sequence[int] = (),
           composition_offsets: Sequence[tuple[int, int]] = ()) -> bytes:
    tkhd = _full_box(
        b"tkhd", _u32(0, 0, track_id, 0, duration_ms), bytes(8),
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
        (_box(
             b"ctts", bytes([1, 0, 0, 0]), _u32(len(composition_offsets)),
             *(_u32(count, offset & 0xFFFFFFFF) for count, offset in composition_offsets),
         ) if composition_offsets else b""),
        _full_box(b"stsc", _u32(1, 1, sample_count, 1)),
        sample_sizes,
        (_full_box(b"stss", _u32(len(sync_samples)), _u32(*sync_samples))
         if sync_samples else b""),
        _full_box(b"stco", _u32(1, chunk_offset)),
    )
    return _box(b"trak", tkhd, _box(b"mdia", mdhd, hdlr, _box(b"minf", media_header, dinf, stbl)))


class Mp4Writer:
    """Write generated H.264 frames and synchronized stereo PCM silence."""

    def encode(self, encoder: H264Encoder, metadata: Optional[Mp4Metadata] = None) -> bytes:
        metadata_box = _metadata_box(metadata)
        compatibility_box = _compatibility_metadata_box(metadata)
        frame_count = max(_MIN_VIDEO_FRAME_COUNT, _GENERATED_FEATURE_FRAME_COUNT)
        samples = []
        for index in range(frame_count):
            try:
                samples.append(encoder.encode_generated_frame(index))
            except (ValueError, OverflowError, RuntimeError) as error:
                raise type(error)(
                    f"presentation frame {index}: {error}"
                ) from error
        # IDR IDs can change the sample size, so retain individual sizes.
        video = b"".join(samples)
        audio_sample_count = max(_AUDIO_SAMPLE_RATE, frame_count * _AUDIO_SAMPLE_RATE // _VIDEO_FRAME_RATE)
        audio = bytes(audio_sample_count * 2 * 2)
        video_duration_ms = (frame_count * 1000 + _VIDEO_FRAME_RATE - 1) // _VIDEO_FRAME_RATE
        audio_duration_ms = (audio_sample_count * 1000 + _AUDIO_SAMPLE_RATE - 1) // _AUDIO_SAMPLE_RATE
        movie_duration_ms = max(video_duration_ms, audio_duration_ms)
        sps = encoder.sequence_parameter_set()
        pps_sets = (
            encoder.picture_parameter_set(),
            encoder._picture_parameter_set(1, False, 1),
            encoder._picture_parameter_set(2, False, 2),
            encoder._picture_parameter_set(3, True, 0),
        )
        avcc = _box(
            b"avcC", bytes([1, sps[1], sps[2], sps[3], 0xFF, 0xE1]),
            _u16(len(sps)), sps, bytes([len(pps_sets)]),
            *(_u16(len(pps)) + pps for pps in pps_sets),
        )
        visual = _box(
            b"avc1", bytes(6), _u16(1), bytes(16), _u16(64, 64),
            _u32(0x480000, 0x480000, 0), _u16(1), bytes(32), _u16(24, 0xFFFF), avcc,
        )
        sound = _box(
            b"sowt", bytes(6), _u16(1), bytes(8), _u16(2, 16, 0, 0),
            _u32(_AUDIO_SAMPLE_RATE << 16),
        )
        ftyp = _box(b"ftyp", b"isom", _u32(0x200), b"isomiso2avc1qt  ")
        video_offset = len(ftyp) + 8
        sizes = _full_box(b"stsz", _u32(0, frame_count), _u32(*(len(sample) for sample in samples)))
        video_track = _track(
            1, visual, sizes, frame_count, _VIDEO_FRAME_RATE, video_duration_ms,
            video_offset, True,
            tuple(index + 1 for index in range(frame_count)
                  if index not in range(2, 31)),
            ((19, 0), (1, 1), (1, -1), (frame_count - 21, 0)),
        )
        audio_sizes = _full_box(b"stsz", _u32(4, audio_sample_count))
        audio_track = _track(
            2, sound, audio_sizes, audio_sample_count, _AUDIO_SAMPLE_RATE,
            audio_duration_ms, video_offset + len(video), False,
        )
        mvhd = _full_box(
            b"mvhd", _u32(0, 0, 1000, movie_duration_ms, 0x10000), _u16(0x100, 0),
            bytes(8), _matrix(), bytes(24), _u32(3),
        )
        return ftyp + _box(b"mdat", video, audio) + _box(
            b"moov", mvhd, video_track, audio_track, metadata_box, compatibility_box,
        )
