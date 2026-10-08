"""Handle object for an mp4 file."""

from typing import BinaryIO, Optional, Tuple

from .basicinfo import BasicInfo
from .mp4boxes import HdlrBox, Mp4FormatError, find, read_boxes
from .probe import HEADER_SIZE, detect_format


class Mp4File:
    """Starts idle (not open); open() moves it to the open state on success."""

    def __init__(self) -> None:
        self._f: Optional[BinaryIO] = None
        self._error = ""

    def open(self, name: str) -> bool:
        self.close()
        self._error = ""
        try:
            f = open(name, "rb")
        except OSError as e:
            self._error = str(e)
            return False
        if detect_format(f.read(HEADER_SIZE)) != "mp4":
            f.close()
            self._error = f"{name}: not an mp4 file"
            return False
        self._f = f
        return True

    def close(self) -> None:
        if self._f is not None:
            self._f.close()
        self._f = None

    def isopen(self) -> bool:
        return self._f is not None

    def errorinfo(self) -> Tuple[str]:
        return (self._error,)

    def getbasicinfo(self) -> BasicInfo:
        """Return basic file info; on failure return an all-false BasicInfo and set errorinfo."""
        self._error = ""
        if self._f is None:
            self._error = "file not open"
            return BasicInfo()
        try:
            self._f.seek(0, 2)
            size = self._f.tell()
            boxes = read_boxes(self._f, 0, size)
        except (Mp4FormatError, OSError) as e:
            self._error = str(e)
            return BasicInfo()
        handlers = {
            b.handler_type
            for b in find(boxes, "moov", "trak", "mdia", "hdlr")
            if isinstance(b, HdlrBox)
        }
        return BasicInfo(hasvideo="vide" in handlers, hasaudio="soun" in handlers)
