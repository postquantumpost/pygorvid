import struct

from pygorvid import (
    BasicAudioStreamInfo,
    BasicInfo,
    BasicVideoStreamInfo,
    Mp4File,
    openfile,
)


def box(kind: bytes, payload: bytes = b"") -> bytes:
    return struct.pack(">I4s", 8 + len(payload), kind) + payload


def track(
    handler: bytes,
    width: int = 0,
    height: int = 0,
    sample_count: int = 0,
    sample_delta: int = 0,
    timescale: int = 1000,
    sample_rate: int = 0,
    channels: int = 0,
) -> bytes:
    hdlr = box(b"hdlr", b"\0" * 4 + b"\0" * 4 + handler + b"\0" * 13)
    tkhd = bytearray(84)
    struct.pack_into(">II", tkhd, 76, width << 16, height << 16)
    mdhd = bytearray(20)
    struct.pack_into(">I", mdhd, 12, timescale)
    stts = struct.pack(">IIII", 0, 1, sample_count, sample_delta)
    sample_table = b""
    if handler == b"soun":
        sample_entry = bytearray(28)
        struct.pack_into(">H", sample_entry, 16, channels)
        struct.pack_into(">I", sample_entry, 24, sample_rate << 16)
        stsd = struct.pack(">II", 0, 1) + box(b"mp4a", sample_entry)
        sample_table += box(b"stsd", stsd)
    sample_table += box(b"stts", stts)
    return box(
        b"trak",
        box(b"tkhd", tkhd)
        + box(
            b"mdia",
            box(b"mdhd", mdhd)
            + hdlr
            + box(b"minf", box(b"stbl", sample_table)),
        ),
    )


FTYP = box(b"ftyp", b"isom\0\0\0\0isom")


def write(tmp_path, *tracks: bytes) -> str:
    p = tmp_path / "a.mp4"
    p.write_bytes(FTYP + box(b"free", b"xx") + box(b"moov", b"".join(tracks)))
    return str(p)


def test_video_only(tmp_path):
    m = Mp4File()
    assert m.open(write(tmp_path, track(b"vide")))
    assert m.getbasicinfo() == BasicInfo(
        hasvideo=True, videostreams=[BasicVideoStreamInfo()]
    )
    assert m.errorinfo() == ("",)


def test_video_and_audio(tmp_path):
    m = Mp4File()
    assert m.open(write(tmp_path, track(b"vide"), track(b"soun"), track(b"soun")))
    assert m.getbasicinfo() == BasicInfo(
        hasvideo=True,
        hasaudio=True,
        videostreams=[BasicVideoStreamInfo()],
        audiostreams=[BasicAudioStreamInfo(), BasicAudioStreamInfo()],
    )


def test_multiple_audio_streams(tmp_path):
    m = Mp4File()
    assert m.open(
        write(
            tmp_path,
            track(b"soun", sample_rate=44100, channels=2),
            track(b"soun", sample_rate=48000, channels=6),
        )
    )
    assert m.getbasicinfo().audiostreams == [
        BasicAudioStreamInfo(sample_rate=44100, channels=2),
        BasicAudioStreamInfo(sample_rate=48000, channels=6),
    ]


def test_multiple_video_streams(tmp_path):
    m = Mp4File()
    assert m.open(
        write(
            tmp_path,
            track(b"vide", 1920, 1080, 300, 1000, 30000),
            track(b"vide", 640, 480, 3, 15000, 30000),
        )
    )
    assert m.getbasicinfo().videostreams == [
        BasicVideoStreamInfo(
            width=1920,
            height=1080,
            framerate=30.0,
            frame_count=300,
            duration_seconds=10.0,
        ),
        BasicVideoStreamInfo(
            width=640,
            height=480,
            framerate=2.0,
            frame_count=3,
            duration_seconds=1.5,
        ),
    ]


def test_no_tracks(tmp_path):
    m = Mp4File()
    assert m.open(write(tmp_path))
    assert m.getbasicinfo() == BasicInfo()
    assert m.errorinfo() == ("",)


def test_through_vidfile(tmp_path):
    v = openfile(write(tmp_path, track(b"soun")))
    assert v.getbasicinfo() == BasicInfo(
        hasvideo=False,
        hasaudio=True,
        audiostreams=[BasicAudioStreamInfo()],
    )


def test_truncated_box(tmp_path):
    p = tmp_path / "a.mp4"
    p.write_bytes(FTYP + struct.pack(">I4s", 1000, b"moov") + b"\0" * 8)
    m = Mp4File()
    assert m.open(str(p))
    assert m.getbasicinfo() == BasicInfo()
    assert m.errorinfo()[0] != ""


def test_not_open():
    m = Mp4File()
    assert m.getbasicinfo() == BasicInfo()
    assert m.errorinfo()[0] != ""


def test_vidfile_other_format_unsupported(tmp_path):
    p = tmp_path / "a.mkv"
    p.write_bytes(b"\x1a\x45\xdf\xa3rest")
    v = openfile(str(p))
    assert v.isopen()
    assert v.getbasicinfo() == BasicInfo()
    assert v.errorinfo()[0] != ""
