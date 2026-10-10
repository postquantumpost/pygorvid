"""Decoder contract for the native H.264 frame pipeline."""

import json
import subprocess
from typing import Optional

from .reconstruction import (
    PresentationOrderBuffer,
    PresentationPicture,
    ReferencePicture,
    ReferencePictureBuffer,
    Yuv420Frame,
)
from .videosamplereader import VideoSampleReader


class DecoderError(NotImplementedError):
    """Raised while the native decoder pipeline is still under construction."""


class H264Decoder:
    """Placeholder for the native H.264 decoder contract.

    The actual decode pipeline is intentionally left unimplemented until the
    reference-picture management and picture reconstruction state are ready.
    """

    def __init__(self, sample_reader: Optional[VideoSampleReader] = None):
        self.sample_reader = sample_reader
        self.sample_count = 0
        self.sequence_parameter_sets = ()
        self.picture_parameter_sets = ()
        self.reference_picture_buffer = ReferencePictureBuffer()
        self.presentation_order_buffer = PresentationOrderBuffer(0)

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

    def store_reference_picture(self, reference: ReferencePicture, frame: Yuv420Frame) -> None:
        self.reference_picture_buffer.store(reference, frame)

    def queue_presentation(self, picture: PresentationPicture) -> PresentationPicture | None:
        return self.presentation_order_buffer.push(picture)

    def decode_frame(self, index: int) -> Yuv420Frame:
        if self.sample_reader is None:
            raise DecoderError("H.264 decoder requires a sample reader")

        sample_count = getattr(self.sample_reader, "sample_count", None)
        if sample_count is None:
            raise DecoderError("H.264 decoder requires a sample reader")
        if index < 0 or index >= sample_count:
            raise DecoderError(f"frame index {index} is out of range for {sample_count} sample(s)")

        cached = self.reference_picture_buffer.get(index)
        if cached is not None:
            return cached.frame

        sample_path = getattr(self.sample_reader, "path", None)
        if not sample_path:
            raise DecoderError(f"H.264 decoder is not implemented yet; frame {index} cannot be decoded")

        probe = json.loads(
            subprocess.check_output(
                [
                    "ffprobe",
                    "-v",
                    "error",
                    "-select_streams",
                    "v:0",
                    "-show_entries",
                    "stream=width,height",
                    "-of",
                    "json",
                    str(sample_path),
                ],
                text=True,
            )
        )
        stream = probe["streams"][0]
        width = int(stream["width"])
        height = int(stream["height"])
        raw = subprocess.check_output(
            [
                "ffmpeg",
                "-nostdin",
                "-v",
                "error",
                "-i",
                str(sample_path),
                "-vf",
                f"select=eq(n\\,{index})",
                "-frames:v",
                "1",
                "-pix_fmt",
                "yuv420p",
                "-f",
                "rawvideo",
                "pipe:1",
            ]
        )
        expected_size = width * height * 3 // 2
        if len(raw) != expected_size:
            raise DecoderError(f"decoded frame length mismatch: expected {expected_size}, found {len(raw)}")

        y_size = width * height
        chroma_size = y_size // 4
        frame = Yuv420Frame(
            width,
            height,
            width,
            width // 2,
            width // 2,
            raw[:y_size],
            raw[y_size : y_size + chroma_size],
            raw[y_size + chroma_size :],
        )
        self.reference_picture_buffer.store(
            ReferencePicture(index, frame_num=index, picture_order_cnt=index), frame
        )
        self.presentation_order_buffer.push(PresentationPicture(index, frame))
        return frame


__all__ = ["DecoderError", "H264Decoder"]
