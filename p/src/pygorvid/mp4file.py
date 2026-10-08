"""Handle object for an mp4 file."""

from typing import BinaryIO, Optional, Tuple

from .basicinfo import BasicAudioStreamInfo, BasicInfo, BasicVideoStreamInfo
from .mp4boxes import (
    HdlrBox,
    MdhdBox,
    Mp4FormatError,
    SttsBox,
    StsdBox,
    TkhdBox,
    TrakBox,
    find,
    read_boxes,
)
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
        info = BasicInfo()
        for track in find(boxes, "moov", "trak"):
            if not isinstance(track, TrakBox):
                continue
            handlers = [
                b.handler_type
                for b in find([track], "trak", "mdia", "hdlr")
                if isinstance(b, HdlrBox)
            ]
            if "vide" in handlers:
                info.hasvideo = True
                info.videostreams.append(self._get_basic_video_stream_info(track))
            if "soun" in handlers:
                info.hasaudio = True
                info.audiostreams.append(self._get_basic_audio_stream_info(track))
        return info

    @staticmethod
    def _get_basic_video_stream_info(track: TrakBox) -> BasicVideoStreamInfo:
        stream = BasicVideoStreamInfo()
        for box in find([track], "trak", "tkhd"):
            if isinstance(box, TkhdBox):
                stream.width = box.width
                stream.height = box.height
        timescale = 0
        for box in find([track], "trak", "mdia", "mdhd"):
            if isinstance(box, MdhdBox):
                timescale = box.timescale
        samples = 0
        duration = 0
        for box in find([track], "trak", "mdia", "minf", "stbl", "stts"):
            if isinstance(box, SttsBox):
                for sample_count, sample_delta in box.entries:
                    samples += sample_count
                    duration += sample_count * sample_delta
        stream.frame_count = samples
        if timescale > 0 and duration > 0:
            stream.duration_seconds = duration / timescale
            stream.framerate = samples / stream.duration_seconds
        return stream

    @staticmethod
    def _get_basic_audio_stream_info(track: TrakBox) -> BasicAudioStreamInfo:
        stream = BasicAudioStreamInfo()
        for box in find([track], "trak", "mdia", "minf", "stbl", "stsd"):
            if isinstance(box, StsdBox):
                stream.sample_rate = box.sample_rate
                stream.channels = box.channels
        return stream
