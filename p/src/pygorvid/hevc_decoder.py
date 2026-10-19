"""HEVC decoder availability and operator-supplied licensing assertion."""

import os

from .codec import CodecError, CodecErrorCode
from .codec_frame import Yuv420Frame


class HEVCDecoder:
    def __init__(self, sample_reader=None, *, threads=None, debug_progress=False):
        self.sample_reader = sample_reader
        self.licensed = os.environ.get("HEVC_IS_LICENSED") == "true"

    def check_available(self) -> None:
        raise self._availability_error()

    def _availability_error(self) -> CodecError:
        if self.licensed:
            return CodecError(CodecErrorCode.NOT_IMPLEMENTED, "HEVC decoding is not implemented")
        return CodecError(
            CodecErrorCode.NOT_IMPLEMENTED_AND_NOT_LICENSED,
            "HEVC decoding is not implemented and not licensed",
        )

    def decode_frame(self, index: int) -> Yuv420Frame:
        raise self._availability_error()