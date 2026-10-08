"""Reader for the mp4 box structure. Each box type is a named class.

To read more of the format, add a Box subclass and register it in _REGISTRY.
Unregistered box types are skipped without being parsed.
"""

import struct
from dataclasses import dataclass
from typing import BinaryIO, Dict, List, Optional, Type


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
    "hdlr": HdlrBox,
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
