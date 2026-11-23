"""AV1 decoder availability."""

from .codec import CodecError, CodecErrorCode
from .codec_frame import Yuv420Frame


class AV1Decoder:
    def __init__(
        self, sample_reader=None, *, threads=None, debug_progress=False,
        readahead=True,
    ):
        self.sample_reader = sample_reader

    def check_available(self) -> None:
        raise CodecError(CodecErrorCode.NOT_IMPLEMENTED, "AV1 decoding is not implemented")

    def decode_frame(self, index: int) -> Yuv420Frame:
        self.check_available()

    def decode_sequential_frames(self, frame_indices):
        for index in frame_indices:
            yield index, self.decode_frame(index)
