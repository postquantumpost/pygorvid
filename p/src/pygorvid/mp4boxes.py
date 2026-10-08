"""Reader for the mp4 box structure. Each box type is a named class.

To read more of the format, add a Box subclass and register it in _REGISTRY.
Unregistered box types are skipped without being parsed.
"""

import struct
from dataclasses import dataclass
from typing import BinaryIO, Dict, List, Optional, Tuple, Type


class Mp4FormatError(Exception):
    pass


@dataclass(frozen=True)
class BoxHeader:
    type: str
    start: int
    header_size: int
    end: int

    @property
    def payload_start(self) -> int:
        return self.start + self.header_size


def read_header(f: BinaryIO, limit: int) -> Optional[BoxHeader]:
    """Read a box header at the current position; None if no room is left before limit."""
    start = f.tell()
    if start + 8 > limit:
        return None
    b = f.read(8)
    if len(b) < 8:
        raise Mp4FormatError("truncated box header")
    size, kind = struct.unpack(">I4s", b)
    header_size = 8
    if size == 1:
        b = f.read(8)
        if len(b) < 8:
            raise Mp4FormatError("truncated box header")
        (size,) = struct.unpack(">Q", b)
        header_size = 16
    elif size == 0:
        size = limit - start
    if size < header_size or start + size > limit:
        raise Mp4FormatError(f"invalid box size {size} at offset {start}")
    return BoxHeader(kind.decode("latin-1"), start, header_size, start + size)


class Box:
    def __init__(self, header: BoxHeader) -> None:
        self.header = header

    def read(self, f: BinaryIO) -> None:
        """Parse the payload. Called with no position guarantee; seek as needed."""


class UnknownBox(Box):
    pass


class ContainerBox(Box):
    def __init__(self, header: BoxHeader) -> None:
        super().__init__(header)
        self.children: List[Box] = []

    def read(self, f: BinaryIO) -> None:
        self.children = read_boxes(f, self.header.payload_start, self.header.end)


class MoovBox(ContainerBox):
    pass


class TrakBox(ContainerBox):
    pass


class MdiaBox(ContainerBox):
    pass


class MinfBox(ContainerBox):
    pass


class StblBox(ContainerBox):
    pass


class TkhdBox(Box):
    def __init__(self, header: BoxHeader) -> None:
        super().__init__(header)
        self.width = 0
        self.height = 0

    def read(self, f: BinaryIO) -> None:
        payload_size = self.header.end - self.header.payload_start
        if payload_size < 84:
            raise Mp4FormatError("tkhd box too short")
        f.seek(self.header.payload_start)
        version = f.read(1)[0]
        width_offset = 88 if version == 1 else 76
        if payload_size < width_offset + 8:
            raise Mp4FormatError("tkhd box too short")
        f.seek(self.header.payload_start + width_offset)
        width, height = struct.unpack(">II", f.read(8))
        self.width = width >> 16
        self.height = height >> 16


class MdhdBox(Box):
    def __init__(self, header: BoxHeader) -> None:
        super().__init__(header)
        self.timescale = 0

    def read(self, f: BinaryIO) -> None:
        payload_size = self.header.end - self.header.payload_start
        if payload_size < 20:
            raise Mp4FormatError("mdhd box too short")
        f.seek(self.header.payload_start)
        version = f.read(1)[0]
        timescale_offset = 20 if version == 1 else 12
        if payload_size < timescale_offset + 4:
            raise Mp4FormatError("mdhd box too short")
        f.seek(self.header.payload_start + timescale_offset)
        self.timescale = struct.unpack(">I", f.read(4))[0]


class SttsBox(Box):
    def __init__(self, header: BoxHeader) -> None:
        super().__init__(header)
        self.entries: List[Tuple[int, int]] = []

    def read(self, f: BinaryIO) -> None:
        payload_size = self.header.end - self.header.payload_start
        if payload_size < 8:
            raise Mp4FormatError("stts box too short")
        f.seek(self.header.payload_start + 4)
        count = struct.unpack(">I", f.read(4))[0]
        if count * 8 > payload_size - 8:
            raise Mp4FormatError("stts entries exceed box size")
        self.entries = [struct.unpack(">II", f.read(8)) for _ in range(count)]


class StsdBox(Box):
    def __init__(self, header: BoxHeader) -> None:
        super().__init__(header)
        self.sample_rate = 0
        self.channels = 0

    def read(self, f: BinaryIO) -> None:
        payload_size = self.header.end - self.header.payload_start
        if payload_size < 8:
            raise Mp4FormatError("stsd box too short")
        f.seek(self.header.payload_start + 4)
        count = struct.unpack(">I", f.read(4))[0]
        if count == 0:
            return
        entry = read_header(f, self.header.end)
        if entry is None:
            raise Mp4FormatError("audio sample entry too short")
        if entry.end - entry.payload_start < 28:
            return
        f.seek(entry.payload_start + 16)
        self.channels = struct.unpack(">H", f.read(2))[0]
        f.seek(entry.payload_start + 24)
        self.sample_rate = struct.unpack(">I", f.read(4))[0] >> 16


class HdlrBox(Box):
    def __init__(self, header: BoxHeader) -> None:
        super().__init__(header)
        self.handler_type = ""

    def read(self, f: BinaryIO) -> None:
        # payload: version/flags (4), pre_defined (4), handler_type (4), ...
        if self.header.end - self.header.payload_start < 12:
            raise Mp4FormatError("hdlr box too short")
        f.seek(self.header.payload_start + 8)
        self.handler_type = f.read(4).decode("latin-1")


_REGISTRY: Dict[str, Type[Box]] = {
    "moov": MoovBox,
    "trak": TrakBox,
    "mdia": MdiaBox,
    "minf": MinfBox,
    "stbl": StblBox,
    "hdlr": HdlrBox,
    "tkhd": TkhdBox,
    "mdhd": MdhdBox,
    "stts": SttsBox,
    "stsd": StsdBox,
}


def read_boxes(f: BinaryIO, start: int, end: int) -> List[Box]:
    """Read the sibling boxes occupying [start, end)."""
    f.seek(start)
    boxes: List[Box] = []
    while True:
        header = read_header(f, end)
        if header is None:
            return boxes
        box = _REGISTRY.get(header.type, UnknownBox)(header)
        box.read(f)
        f.seek(header.end)
        boxes.append(box)


def find(boxes: List[Box], *path: str) -> List[Box]:
    """Boxes reached by matching path[0] among boxes, then path[1] among their children, ..."""
    level = boxes
    for i, kind in enumerate(path):
        matched = [b for b in level if b.header.type == kind]
        if i == len(path) - 1:
            return matched
        level = [c for b in matched if isinstance(b, ContainerBox) for c in b.children]
    return []
