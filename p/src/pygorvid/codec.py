"""Codec-neutral decoder selection, error codes, and execution defaults."""

from enum import Enum
import os


class VideoCodec(str, Enum):
    H264 = "h264"
    HEVC = "hevc"


class CodecErrorCode(str, Enum):
    NOT_IMPLEMENTED = "NOT_IMPLEMENTED"
    NOT_IMPLEMENTED_AND_NOT_LICENSED = "NOT_IMPLEMENTED_AND_NOT_LICENSED"


class DecoderError(NotImplementedError):
    """Raised when decoding fails or the requested decoder is unavailable."""


class CodecError(DecoderError):
    def __init__(self, code: CodecErrorCode, message: str):
        super().__init__(message)
        self.code = code


def default_decoder_threads() -> int:
    return max(1, (os.cpu_count() or 1) // 2)


def read_codec_configuration(entry, timescale):
    if entry.header.type == "avc1":
        from .h264_mp4 import read_sample_configuration
    elif entry.header.type in {"hvc1", "hev1"}:
        from .hevc_mp4 import read_sample_configuration
    else:
        from .mp4boxes import Mp4FormatError
        raise Mp4FormatError("unsupported video sample entry")
    return read_sample_configuration(entry, timescale)


def create_decoder(reader, **options):
    if reader.codec == VideoCodec.HEVC:
        from .hevc_decoder import HEVCDecoder
        decoder = HEVCDecoder(reader, **options)
        decoder.check_available()
        return decoder
    from .h264_decoder import H264Decoder
    return H264Decoder(reader, **options)