import struct
from dataclasses import dataclass
from typing import ClassVar
from typing import BinaryIO, List, Tuple

from .mp4boxes import Box, BoxHeader, Mp4FormatError, VisualSampleEntryBox
from .codec import VideoCodec


@dataclass(frozen=True)
class HEVCConfiguration:
    profile: int
    level: int
    bit_depth_luma: int
    bit_depth_chroma: int
    nal_length_size: int
    arrays: Tuple[Tuple[bool, int, Tuple[bytes, ...]], ...]
    timescale: int
    codec: ClassVar[VideoCodec] = VideoCodec.HEVC


def read_sample_configuration(entry, timescale: int) -> HEVCConfiguration:
    configurations = [child for child in entry.children if isinstance(child, HvcCBox)]
    if len(configurations) != 1:
        raise Mp4FormatError("HEVC sample entry must contain one hvcC box")
    config = configurations[0]
    return HEVCConfiguration(
        config.profile, config.level, config.bit_depth_luma, config.bit_depth_chroma,
        config.nal_length_size,
        tuple((complete, nal_type, tuple(units)) for complete, nal_type, units in config.arrays),
        timescale,
    )


class HvcCBox(Box):
    def __init__(self, header: BoxHeader) -> None:
        super().__init__(header)
        self.profile = 0
        self.level = 0
        self.bit_depth_luma = 0
        self.bit_depth_chroma = 0
        self.nal_length_size = 0
        self.arrays: List[Tuple[bool, int, List[bytes]]] = []

    def read(self, f: BinaryIO) -> None:
        payload_size = self.header.end - self.header.payload_start
        if payload_size < 23:
            raise Mp4FormatError("hvcC box too short")
        f.seek(self.header.payload_start)
        data = f.read(payload_size)
        if len(data) != payload_size or data[0] != 1:
            raise Mp4FormatError("invalid HEVCDecoderConfigurationRecord")
        if (data[13] & 0xF0 != 0xF0 or data[15] & 0xFC != 0xFC
                or data[16] & 0xFC != 0xFC or data[17] & 0xF8 != 0xF8
                or data[18] & 0xF8 != 0xF8):
            raise Mp4FormatError("hvcC reserved bits are invalid")
        self.nal_length_size = (data[21] & 3) + 1
        if self.nal_length_size == 3:
            raise Mp4FormatError("hvcC uses a reserved NAL length size")
        self.profile = data[1] & 0x1F
        self.level = data[12]
        self.bit_depth_luma = (data[17] & 7) + 8
        self.bit_depth_chroma = (data[18] & 7) + 8
        self.arrays = []
        offset = 23
        for _ in range(data[22]):
            if len(data) - offset < 3:
                raise Mp4FormatError("hvcC array header truncated")
            flags, count = struct.unpack_from(">BH", data, offset)
            if flags & 0x40:
                raise Mp4FormatError("hvcC array reserved bit is invalid")
            offset += 3
            units = []
            for _ in range(count):
                if len(data) - offset < 2:
                    raise Mp4FormatError("hvcC NAL length truncated")
                length = struct.unpack_from(">H", data, offset)[0]
                offset += 2
                if length < 2 or length > len(data) - offset:
                    raise Mp4FormatError("hvcC NAL exceeds box size or is too short")
                if (data[offset] & 0x80 or data[offset + 1] & 7 == 0
                        or (data[offset] >> 1) & 0x3F != flags & 0x3F):
                    raise Mp4FormatError("hvcC NAL header does not match array type")
                units.append(data[offset:offset + length])
                offset += length
            self.arrays.append((bool(flags & 0x80), flags & 0x3F, units))
        if offset != len(data):
            raise Mp4FormatError("hvcC has invalid trailing data")


class HevcSampleEntryBox(VisualSampleEntryBox):
    pass


