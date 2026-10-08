"""Handle object for a video file."""

from typing import BinaryIO, Optional, Tuple

from .basicinfo import BasicInfo
from .mp4file import Mp4File
from .probe import HEADER_SIZE, detect_format


class VidFile:
    """Starts idle (not open); open() moves it to the open state on success.

    An mp4 file is handled by a held Mp4File.
    """

    def __init__(self) -> None:
        self._f: Optional[BinaryIO] = None
        self._mp4: Optional[Mp4File] = None
        self._error = ""
        self.format = ""

    def open(self, name: str) -> bool:
        self.close()
        self._error = ""
        try:
            f = open(name, "rb")
        except OSError as e:
            self._error = str(e)
            return False
        fmt = detect_format(f.read(HEADER_SIZE))
        if fmt == "unknown":
            f.close()
            self._error = f"{name}: unrecognized video format"
            return False
        if fmt == "mp4":
            f.close()
            m = Mp4File()
            if not m.open(name):
                self._error = m.errorinfo()[0]
                return False
            self._mp4 = m
            self.format = fmt
            return True
        self._f = f
        self.format = fmt
        return True

    def close(self) -> None:
        if self._f is not None:
            self._f.close()
        if self._mp4 is not None:
            self._mp4.close()
        self._f = None
        self._mp4 = None
        self.format = ""

    def isopen(self) -> bool:
        return self._f is not None or (self._mp4 is not None and self._mp4.isopen())

    def errorinfo(self) -> Tuple[str]:
        return (self._error,)

    def getbasicinfo(self) -> BasicInfo:
        """Return basic file info; on failure return an all-false BasicInfo and set errorinfo."""
        if self._mp4 is not None:
            info = self._mp4.getbasicinfo()
            self._error = self._mp4.errorinfo()[0]
            return info
        if self._f is None:
            self._error = "file not open"
        else:
            self._error = f"basic info not supported for {self.format}"
        return BasicInfo()

    def __enter__(self) -> "VidFile":
        return self

    def __exit__(self, *exc) -> None:
        self.close()


def construct() -> VidFile:
    """Return an idle VidFile, ready for its open() method."""
    return VidFile()


def openfile(name: str) -> VidFile:
    """Return a VidFile; it is open on success, otherwise idle with errorinfo set."""
    v = construct()
    v.open(name)
    return v
