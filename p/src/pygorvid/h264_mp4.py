import struct
from dataclasses import dataclass
from typing import ClassVar
from typing import BinaryIO, List, Tuple

from .mp4boxes import Box, BoxHeader, Mp4FormatError, VisualSampleEntryBox
from .codec import VideoCodec


@dataclass(frozen=True)
class AVCConfiguration:
    profile: int
    profile_compat: int
    level: int
    nal_length_size: int
    sequence_parameter_sets: Tuple[bytes, ...]
    picture_parameter_sets: Tuple[bytes, ...]
    timescale: int
    codec: ClassVar[VideoCodec] = VideoCodec.H264


def read_sample_configuration(entry, timescale: int) -> AVCConfiguration:
    configurations = [child for child in entry.children if isinstance(child, AvcCBox)]
    if len(configurations) != 1:
        raise Mp4FormatError("avc1 sample entry must contain one avcC box")
    config = configurations[0]
    return AVCConfiguration(
        config.profile, config.profile_compat, config.level, config.nal_length_size,
        tuple(config.sequence_sets), tuple(config.picture_sets), timescale,
    )


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


class Avc1Box(VisualSampleEntryBox):
    pass
