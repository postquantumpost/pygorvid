from pygorvid import VidFile, construct, openfile


def test_open_ok(tmp_path):
    p = tmp_path / "a.mkv"
    p.write_bytes(b"\x1a\x45\xdf\xa3rest")
    v = openfile(str(p))
    assert v.isopen() and v.errorinfo() == ("",) and v.format == "matroska"
    v.close()
    assert not v.isopen()


def test_open_missing():
    v = openfile("/nonexistent/x.mp4")
    assert not v.isopen()
    assert v.errorinfo()[0] != ""


def test_open_unrecognized(tmp_path):
    p = tmp_path / "a.bin"
    p.write_bytes(b"hello")
    v = VidFile()
    assert not v.open(str(p))
    assert not v.isopen() and v.errorinfo()[0] != ""


def test_construct(tmp_path):
    v = construct()
    assert not v.isopen() and v.errorinfo() == ("",)
    p = tmp_path / "a.mkv"
    p.write_bytes(b"\x1a\x45\xdf\xa3rest")
    assert v.open(str(p)) and v.isopen()
