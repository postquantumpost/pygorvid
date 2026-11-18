from dataclasses import dataclass
from typing import BinaryIO, ClassVar, List, Tuple

from .codec import VideoCodec
from .mp4boxes import Box, BoxHeader, Mp4FormatError, VisualSampleEntryBox


@dataclass(frozen=True)
class AV1Configuration:
    profile: int
    level: int
    tier: int
    bit_depth: int
    monochrome: bool
    chroma_subsampling_x: bool
    chroma_subsampling_y: bool
    chroma_sample_position: int
    config_obus: Tuple[bytes, ...]
    timescale: int
    codec: ClassVar[VideoCodec] = VideoCodec.AV1


def read_sample_configuration(entry, timescale: int) -> AV1Configuration:
    configurations = [child for child in entry.children if isinstance(child, Av1CBox)]
    if len(configurations) != 1:
        raise Mp4FormatError("av01 sample entry must contain one av1C box")
    config = configurations[0]
    return AV1Configuration(
        config.profile,
        config.level,
        config.tier,
        config.bit_depth,
        config.monochrome,
        config.chroma_subsampling_x,
        config.chroma_subsampling_y,
        config.chroma_sample_position,
        tuple(config.config_obus),
        timescale,
    )


class Av1CBox(Box):
    def __init__(self, header: BoxHeader) -> None:
        super().__init__(header)
        self.profile = 0
        self.level = 0
        self.tier = 0
        self.bit_depth = 0
        self.monochrome = False
        self.chroma_subsampling_x = False
        self.chroma_subsampling_y = False
        self.chroma_sample_position = 0
        self.config_obus: List[bytes] = []

    def read(self, f: BinaryIO) -> None:
        size = self.header.end - self.header.payload_start
        if size < 4:
            raise Mp4FormatError("av1C box too short")
        f.seek(self.header.payload_start)
        data = f.read(size)
        if len(data) != size:
            raise Mp4FormatError("cannot read av1C box")
        if data[0] != 0x81:
            raise Mp4FormatError("invalid AV1DecoderConfigurationRecord header")
        self.profile = data[1] >> 5
        self.level = data[1] & 0x1F
        self.tier = (data[2] >> 7) & 1
        high_bitdepth = bool(data[2] & 0x40)
        twelve_bit = bool(data[2] & 0x20)
        self.bit_depth = 12 if twelve_bit else 10 if high_bitdepth else 8
        self.monochrome = bool(data[2] & 0x10)
        self.chroma_subsampling_x = bool(data[2] & 0x08)
        self.chroma_subsampling_y = bool(data[2] & 0x04)
        self.chroma_sample_position = data[2] & 0x03
        if data[3] & 0xE0:
            raise Mp4FormatError("av1C reserved bits are invalid")
        self.config_obus = [data[4:]] if len(data) > 4 else []


class Av1SampleEntryBox(VisualSampleEntryBox):
    pass
