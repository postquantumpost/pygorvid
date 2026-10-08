from pygorvid import Mp4File, openfile

MP4 = b"\0\0\0\x18ftypisom" + b"\0" * 16


def test_mp4file(tmp_path):
    p = tmp_path / "a.mp4"
    p.write_bytes(MP4)
    m = Mp4File()
    assert not m.isopen() and m.errorinfo() == ("",)
    assert m.open(str(p)) and m.isopen()
    m.close()
    assert not m.isopen()


def test_mp4file_rejects_other(tmp_path):
    p = tmp_path / "a.mkv"
    p.write_bytes(b"\x1a\x45\xdf\xa3rest")
    m = Mp4File()
    assert not m.open(str(p))
    assert m.errorinfo()[0] != ""


def test_vidfile_uses_mp4file(tmp_path):
    p = tmp_path / "a.mp4"
    p.write_bytes(MP4)
    v = openfile(str(p))
    assert v.isopen() and v.format == "mp4"
    assert isinstance(v._mp4, Mp4File) and v._mp4.isopen()
    v.close()
    assert not v.isopen()
