"""Container format detection from file header bytes."""

HEADER_SIZE = 380


def detect_format(header: bytes) -> str:
    """Return a container name, or "unknown"."""
    if header[4:8] == b"ftyp":
        return "mov" if header[8:12] == b"qt  " else "mp4"
    if header[:4] == b"\x1a\x45\xdf\xa3":
        return "matroska"
    if header[:4] == b"RIFF" and header[8:12] == b"AVI ":
        return "avi"
    if header[:4] == b"OggS":
        return "ogg"
    if header[:3] == b"FLV":
        return "flv"
    if len(header) > 188 and header[0] == 0x47 and header[188] == 0x47:
        return "mpegts"
    return "unknown"


def probe_file(path: str) -> str:
    with open(path, "rb") as f:
        return detect_format(f.read(HEADER_SIZE))
