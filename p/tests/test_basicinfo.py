import struct

from pygorvid import BasicInfo, Mp4File, openfile


def box(kind: bytes, payload: bytes = b"") -> bytes:
    return struct.pack(">I4s", 8 + len(payload), kind) + payload


def track(handler: bytes) -> bytes:
    hdlr = box(b"hdlr", b"\0" * 4 + b"\0" * 4 + handler + b"\0" * 13)
    return box(b"trak", box(b"tkhd", b"\0" * 8) + box(b"mdia", box(b"mdhd", b"\0" * 4) + hdlr))


FTYP = box(b"ftyp", b"isom\0\0\0\0isom")


def write(tmp_path, *tracks: bytes) -> str:
    p = tmp_path / "a.mp4"
    p.write_bytes(FTYP + box(b"free", b"xx") + box(b"moov", b"".join(tracks)))
    return str(p)


def test_video_only(tmp_path):
    m = Mp4File()
    assert m.open(write(tmp_path, track(b"vide")))
    assert m.getbasicinfo() == BasicInfo(hasvideo=True, hasaudio=False)
    assert m.errorinfo() == ("",)


def test_video_and_audio(tmp_path):
    m = Mp4File()
    assert m.open(write(tmp_path, track(b"vide"), track(b"soun"), track(b"soun")))
    assert m.getbasicinfo() == BasicInfo(hasvideo=True, hasaudio=True)


def test_no_tracks(tmp_path):
    m = Mp4File()
    assert m.open(write(tmp_path))
    assert m.getbasicinfo() == BasicInfo()
    assert m.errorinfo() == ("",)


def test_through_vidfile(tmp_path):
    v = openfile(write(tmp_path, track(b"soun")))
    assert v.getbasicinfo() == BasicInfo(hasvideo=False, hasaudio=True)


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
