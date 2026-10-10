"""Decoder contract for the native H.264 frame pipeline."""

from typing import Optional

from .reconstruction import (
    PresentationOrderBuffer,
    PresentationPicture,
    ReferencePicture,
    ReferencePictureBuffer,
    Yuv420Frame,
)
from .sps import SPSParseError, parse_sps
from .videosamplereader import VideoSampleReader


class DecoderError(NotImplementedError):
    """Raised while the native decoder pipeline is still under construction."""


class DecoderReferenceMismatch(DecoderError):
    """Raised when cached frame data does not match the expected reference bytes."""


class H264Decoder:
    """Own H.264 sample and picture state for the native decode pipeline."""

    def __init__(self, sample_reader: Optional[VideoSampleReader] = None):
        self.sample_reader = sample_reader
        self.sample_count = 0
        self.sequence_parameter_sets = ()
        self.picture_parameter_sets = ()
        self.reference_picture_buffer = ReferencePictureBuffer()
        self.presentation_order_buffer = PresentationOrderBuffer(0)
        self.unsupported_feature = False

        if sample_reader is not None:
            sample_count = getattr(sample_reader, "sample_count", None)
            if sample_count is not None:
                self.sample_count = int(sample_count)
            configuration = getattr(sample_reader, "configuration", None)
            if configuration is not None:
                self.sequence_parameter_sets = tuple(
                    getattr(configuration, "sequence_parameter_sets", ())
                )
                self.picture_parameter_sets = tuple(
                    getattr(configuration, "picture_parameter_sets", ())
                )
                for nal in self.sequence_parameter_sets:
                    try:
                        parse_sps(nal)
                    except (SPSParseError, ValueError):
                        self.unsupported_feature = True
                        break

    def store_reference_picture(self, reference: ReferencePicture, frame: Yuv420Frame) -> None:
        self.reference_picture_buffer.store(reference, frame)

    def queue_presentation(self, picture: PresentationPicture) -> PresentationPicture | None:
        return self.presentation_order_buffer.push(picture)

    def first_sync_sample_index(self) -> int:
        if self.sample_reader is None:
            raise DecoderError("H.264 decoder requires a sample reader")
        sync_index = getattr(self.sample_reader, "first_sync_sample_index", None)
        if callable(sync_index):
            value = sync_index()
            if value is None:
                raise DecoderError("sample reader does not expose a first sync sample")
            return int(value)
        sample_count = getattr(self.sample_reader, "sample_count", None)
        if sample_count is None:
            raise DecoderError("H.264 decoder requires a sample reader")
        for index in range(sample_count):
            sample = getattr(self.sample_reader, "_samples", [])[index]
            if getattr(sample, "is_sync", True):
                return index
        raise DecoderError("sample reader does not expose a first sync sample")

    def decode_first_sync_frame(self) -> Yuv420Frame:
        return self.decode_frame(self.first_sync_sample_index())

    def validate_first_sync_frame_reference(self, y_plane: bytes, u_plane: bytes, v_plane: bytes) -> None:
        index = self.first_sync_sample_index()
        self.validate_reference_frame(index, y_plane, u_plane, v_plane)

    def decode_frame(self, index: int) -> Yuv420Frame:
        if self.sample_reader is None:
            raise DecoderError("H.264 decoder requires a sample reader")
        if self.unsupported_feature:
            raise DecoderError("unsupported H.264 profile, chroma format, bit depth, or interlace mode")

        sample_count = getattr(self.sample_reader, "sample_count", None)
        if sample_count is None:
            raise DecoderError("H.264 decoder requires a sample reader")
        if index < 0 or index >= sample_count:
            raise DecoderError(f"frame index {index} is out of range for {sample_count} sample(s)")

        cached = self.reference_picture_buffer.get(index)
        if cached is not None:
            frame = cached.frame
            try:
                y_plane = frame.luma_plane_bytes()
                u_plane = frame.u_plane_bytes()
                v_plane = frame.v_plane_bytes()
            except ValueError as error:
                raise DecoderError("cached frame has an invalid YUV plane layout") from error
            chroma_width = frame.width // 2 + frame.width % 2
            return Yuv420Frame(
                frame.width,
                frame.height,
                frame.width,
                chroma_width,
                chroma_width,
                y_plane,
                u_plane,
                v_plane,
            )

        raise DecoderError(
            "H.264 decoder is not implemented yet; native decode does not invoke ffprobe or ffmpeg"
        )

    def validate_reference_frame(
        self, index: int, y_plane: bytes, u_plane: bytes, v_plane: bytes
    ) -> None:
        frame = self.decode_frame(index)
        if not frame.luma_plane_matches(y_plane):
            raise DecoderReferenceMismatch("decoded frame does not match the reference frame data")
        if frame.u_plane_bytes() != bytes(u_plane):
            raise DecoderReferenceMismatch("decoded frame does not match the reference frame data")
        if frame.v_plane_bytes() != bytes(v_plane):
            raise DecoderReferenceMismatch("decoded frame does not match the reference frame data")


__all__ = ["DecoderError", "DecoderReferenceMismatch", "H264Decoder"]
