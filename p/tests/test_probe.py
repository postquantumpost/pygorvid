from pygorvid import detect_format


def test_mp4():
    assert detect_format(b"\0\0\0\x18ftypisom") == "mp4"


def test_mov():
    assert detect_format(b"\0\0\0\x14ftypqt  ") == "mov"


def test_matroska():
    assert detect_format(b"\x1a\x45\xdf\xa3rest") == "matroska"


def test_avi():
    assert detect_format(b"RIFF\0\0\0\0AVI LIST") == "avi"


def test_unknown():
    assert detect_format(b"") == "unknown"
