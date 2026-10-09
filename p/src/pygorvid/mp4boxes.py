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


class CttsBox(Box):
    def __init__(self, header: BoxHeader) -> None:
        super().__init__(header)
        self.version = 0
        self.entries: List[Tuple[int, int]] = []

    def read(self, f: BinaryIO) -> None:
        payload_size = self.header.end - self.header.payload_start
        if payload_size < 8:
            raise Mp4FormatError("ctts box too short")
        f.seek(self.header.payload_start)
        version_flags = f.read(4)
        self.version = version_flags[0]
        if self.version not in (0, 1):
            raise Mp4FormatError(f"unsupported ctts version {self.version}")
        count = struct.unpack(">I", f.read(4))[0]
        if count * 8 > payload_size - 8:
            raise Mp4FormatError("ctts entries exceed box size")
        self.entries = []
        for _ in range(count):
            sample_count = struct.unpack(">I", f.read(4))[0]
            offset_bytes = f.read(4)
            sample_offset = struct.unpack(">i" if self.version == 1 else ">I", offset_bytes)[0]
            self.entries.append((sample_count, sample_offset))


class StssBox(Box):
    def __init__(self, header: BoxHeader) -> None:
        super().__init__(header)
        self.sample_numbers: List[int] = []

    def read(self, f: BinaryIO) -> None:
        payload_size = self.header.end - self.header.payload_start
        if payload_size < 8:
            raise Mp4FormatError("stss box too short")
        f.seek(self.header.payload_start + 4)
        count = struct.unpack(">I", f.read(4))[0]
        if count * 4 > payload_size - 8:
            raise Mp4FormatError("stss entries exceed box size")
        self.sample_numbers = [struct.unpack(">I", f.read(4))[0] for _ in range(count)]
        if any(number == 0 for number in self.sample_numbers) or any(
            current <= previous
            for previous, current in zip(self.sample_numbers, self.sample_numbers[1:])
        ):
            raise Mp4FormatError("stss sample numbers must be positive and increasing")


class StszBox(Box):
    def __init__(self, header: BoxHeader) -> None:
        super().__init__(header)
        self.constant_size = 0
        self.sample_count = 0
        self.sizes: List[int] = []

    def sample_size(self, index: int) -> int:
        if not 0 <= index < self.sample_count:
            raise IndexError(index)
        return self.constant_size or self.sizes[index]

    def read(self, f: BinaryIO) -> None:
        payload_size = self.header.end - self.header.payload_start
        if payload_size < 12:
            raise Mp4FormatError("stsz box too short")
        f.seek(self.header.payload_start + 4)
        self.constant_size, self.sample_count = struct.unpack(">II", f.read(8))
        if self.constant_size:
            self.sizes = []
            return
        if self.sample_count * 4 > payload_size - 12:
            raise Mp4FormatError("stsz entries exceed box size")
        self.sizes = [struct.unpack(">I", f.read(4))[0] for _ in range(self.sample_count)]


class StscBox(Box):
    def __init__(self, header: BoxHeader) -> None:
        super().__init__(header)
        self.entries: List[Tuple[int, int, int]] = []

    def read(self, f: BinaryIO) -> None:
        payload_size = self.header.end - self.header.payload_start
        if payload_size < 8:
            raise Mp4FormatError("stsc box too short")
        f.seek(self.header.payload_start + 4)
        count = struct.unpack(">I", f.read(4))[0]
        if count * 12 > payload_size - 8:
            raise Mp4FormatError("stsc entries exceed box size")
        self.entries = [struct.unpack(">III", f.read(12)) for _ in range(count)]
        previous = 0
        for first_chunk, samples_per_chunk, description_index in self.entries:
            if not first_chunk or not samples_per_chunk or not description_index:
                raise Mp4FormatError("invalid stsc entry values")
            if first_chunk <= previous or (previous == 0 and first_chunk != 1):
                raise Mp4FormatError("stsc first_chunk values must start at 1 and increase")
            previous = first_chunk


class ChunkOffsetBox(Box):
    entry_format = ">I"

    def __init__(self, header: BoxHeader) -> None:
        super().__init__(header)
        self.offsets: List[int] = []

    def read(self, f: BinaryIO) -> None:
        payload_size = self.header.end - self.header.payload_start
        entry_size = struct.calcsize(self.entry_format)
        if payload_size < 8:
            raise Mp4FormatError("chunk offset box too short")
        f.seek(self.header.payload_start + 4)
        count = struct.unpack(">I", f.read(4))[0]
        if count * entry_size > payload_size - 8:
            raise Mp4FormatError("chunk offsets exceed box size")
        self.offsets = [struct.unpack(self.entry_format, f.read(entry_size))[0] for _ in range(count)]


class Co64Box(ChunkOffsetBox):
    entry_format = ">Q"


class MdatBox(Box):
    def __init__(self, header: BoxHeader) -> None:
        super().__init__(header)
        self.data_start = header.payload_start
        self.data_end = header.end


class AvcCBox(Box):
    def __init__(self, header: BoxHeader) -> None:
        super().__init__(header)
        self.profile = 0
        self.profile_compat = 0
        self.level = 0
        self.nal_length_size = 0
        self.sequence_sets: List[bytes] = []
        self.picture_sets: List[bytes] = []

    def read(self, f: BinaryIO) -> None:
        payload_size = self.header.end - self.header.payload_start
        if payload_size < 7:
            raise Mp4FormatError("avcC box too short")
        f.seek(self.header.payload_start)
        data = f.read(payload_size)
        if len(data) != payload_size or data[0] != 1:
            raise Mp4FormatError("invalid AVCDecoderConfigurationRecord")
        if data[4] & 0xFC != 0xFC or data[5] & 0xE0 != 0xE0:
            raise Mp4FormatError("avcC reserved bits are invalid")
        if data[4] & 0x03 != 0x03:
            raise Mp4FormatError("only four-byte AVC NAL length prefixes are supported")
        self.profile, self.profile_compat, self.level = data[1:4]
        self.nal_length_size = 4
        sequence_count = data[5] & 0x1F
        if sequence_count == 0:
            raise Mp4FormatError("avcC contains no sequence parameter sets")
        self.sequence_sets, offset = _read_avcc_nals(data, 6, sequence_count, 7)
        if offset >= len(data):
            raise Mp4FormatError("avcC picture-set count missing")
        picture_count = data[offset]
        if picture_count == 0:
            raise Mp4FormatError("avcC contains no picture parameter sets")
        self.picture_sets, offset = _read_avcc_nals(data, offset + 1, picture_count, 8)
        if offset < len(data):
            if self.profile not in {44, 83, 86, 100, 110, 118, 122, 128, 134, 135, 138, 139, 144, 244} or len(data) - offset < 4:
                raise Mp4FormatError("avcC has invalid trailing data")
            if data[offset] & 0xFC != 0xFC or data[offset + 1] & 0xF8 != 0xF8 or data[offset + 2] & 0xF8 != 0xF8:
                raise Mp4FormatError("avcC extension reserved bits are invalid")
            extension_count = data[offset + 3]
            _, offset = _read_avcc_nals(data, offset + 4, extension_count, 13)
        if offset != len(data):
            raise Mp4FormatError("avcC has invalid trailing data")


def _read_avcc_nals(data: bytes, offset: int, count: int, nal_type: int) -> Tuple[List[bytes], int]:
    nals = []
    for _ in range(count):
        if offset + 2 > len(data):
            raise Mp4FormatError("avcC NAL length truncated")
        size = struct.unpack(">H", data[offset : offset + 2])[0]
        offset += 2
        end = offset + size
        if size == 0 or end > len(data):
            raise Mp4FormatError("avcC NAL exceeds box size")
        if data[offset] & 0x80 or data[offset] & 0x1F != nal_type:
            raise Mp4FormatError(f"avcC parameter-set NAL has unexpected type; want {nal_type}")
        nals.append(data[offset:end])
        offset = end
    return nals, offset


class Avc1Box(ContainerBox):
    def __init__(self, header: BoxHeader) -> None:
        super().__init__(header)
        self.width = 0
        self.height = 0

    def read(self, f: BinaryIO) -> None:
        if self.header.end - self.header.payload_start < 78:
            raise Mp4FormatError("avc1 sample entry too short")
        f.seek(self.header.payload_start + 24)
        self.width, self.height = struct.unpack(">HH", f.read(4))
        self.children = read_boxes(f, self.header.payload_start + 78, self.header.end)


class StsdBox(ContainerBox):
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
        self.children = []
        for _ in range(count):
            entry = read_header(f, self.header.end)
            if entry is None:
                raise Mp4FormatError("stsd entry missing")
            box = _new_box(entry)
            if entry.type == "mp4a" and entry.end - entry.payload_start >= 28:
                f.seek(entry.payload_start + 16)
                self.channels = struct.unpack(">H", f.read(2))[0]
                f.seek(entry.payload_start + 24)
                self.sample_rate = struct.unpack(">I", f.read(4))[0] >> 16
            box.read(f)
            self.children.append(box)
            f.seek(entry.end)


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
    "ctts": CttsBox,
    "stss": StssBox,
    "stsz": StszBox,
    "stsc": StscBox,
    "stco": ChunkOffsetBox,
    "co64": Co64Box,
    "mdat": MdatBox,
    "stsd": StsdBox,
    "avc1": Avc1Box,
    "avcC": AvcCBox,
}


def _new_box(header: BoxHeader) -> Box:
    return _REGISTRY.get(header.type, UnknownBox)(header)


def read_boxes(f: BinaryIO, start: int, end: int) -> List[Box]:
    """Read the sibling boxes occupying [start, end)."""
    f.seek(start)
    boxes: List[Box] = []
    while True:
        header = read_header(f, end)
        if header is None:
            return boxes
        box = _new_box(header)
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
