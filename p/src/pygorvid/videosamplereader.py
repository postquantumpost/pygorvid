"""Read length-prefixed AVC samples from the first video track in an MP4."""

from dataclasses import dataclass
from pathlib import Path
from typing import BinaryIO, Iterator, List, Optional, Sequence, Tuple, Union

from .mp4boxes import (
    Avc1Box,
    AvcCBox,
    HdlrBox,
    MdhdBox,
    Mp4FormatError,
    StscBox,
    StsdBox,
    StssBox,
    StszBox,
    SttsBox,
    TrakBox,
    find,
    read_boxes,
)


@dataclass(frozen=True)
class AVCConfiguration:
    profile: int
    profile_compat: int
    level: int
    nal_length_size: int
    sequence_parameter_sets: Tuple[bytes, ...]
    picture_parameter_sets: Tuple[bytes, ...]
    timescale: int


@dataclass(frozen=True)
class CompressedSample:
    index: int
    data: bytes
    dts_ticks: int
    pts_ticks: int
    duration_ticks: int
    is_sync: bool


@dataclass(frozen=True)
class _SampleLocation:
    offset: int
    size: int
    dts_ticks: int
    pts_ticks: int
    duration_ticks: int
    is_sync: bool


class VideoSampleReader:
    """Own an MP4 input and read samples from its first AVC video track."""

    def __init__(self, path: Union[str, Path]):
        self.path = Path(path)
        self._file: Optional[BinaryIO] = None
        self._closed = True
        self._samples: List[_SampleLocation] = []
        self._next_index = 0
        self.configuration: Optional[AVCConfiguration] = None
        try:
            self._file = self.path.open("rb")
            self._closed = False
            self._read_file()
        except Exception:
            self.close()
            raise

    def _read_file(self) -> None:
        assert self._file is not None
        self._file.seek(0, 2)
        file_size = self._file.tell()
        boxes = read_boxes(self._file, 0, file_size)
        track = self._first_video_track(boxes)
        mdhd_boxes = find([track], "trak", "mdia", "mdhd")
        if len(mdhd_boxes) != 1 or not isinstance(mdhd_boxes[0], MdhdBox):
            raise Mp4FormatError("video track must contain one mdhd box")
        timescale = mdhd_boxes[0].timescale
        if timescale == 0:
            raise Mp4FormatError("video track has a zero media timescale")

        stsd_boxes = find([track], "trak", "mdia", "minf", "stbl", "stsd")
        if len(stsd_boxes) != 1 or not isinstance(stsd_boxes[0], StsdBox):
            raise Mp4FormatError("video track must contain one stsd box")
        entries = stsd_boxes[0].children
        if len(entries) != 1 or not isinstance(entries[0], Avc1Box):
            raise Mp4FormatError("only one avc1 sample description is supported")
        avcc_boxes = [child for child in entries[0].children if isinstance(child, AvcCBox)]
        if len(avcc_boxes) != 1:
            raise Mp4FormatError("avc1 sample entry must contain one avcC box")
        avcc = avcc_boxes[0]
        self.configuration = AVCConfiguration(
            profile=avcc.profile,
            profile_compat=avcc.profile_compat,
            level=avcc.level,
            nal_length_size=avcc.nal_length_size,
            sequence_parameter_sets=tuple(avcc.sequence_sets),
            picture_parameter_sets=tuple(avcc.picture_sets),
            timescale=timescale,
        )

        stsz = self._one_box(track, "stsz", StszBox)
        stsc = self._one_box(track, "stsc", StscBox)
        stts = self._one_box(track, "stts", SttsBox)
        offset_tables = find([track], "trak", "mdia", "minf", "stbl", "stco")
        offset_tables += find([track], "trak", "mdia", "minf", "stbl", "co64")
        if len(offset_tables) != 1:
            raise Mp4FormatError("video track must contain one stco or co64 box")
        offsets = getattr(offset_tables[0], "offsets", None)
        if offsets is None:
            raise Mp4FormatError("invalid chunk offset box")

        durations = self._expand_timing(stts.entries, stsz.sample_count, "stts")
        ctts_boxes = find([track], "trak", "mdia", "minf", "stbl", "ctts")
        if len(ctts_boxes) > 1:
            raise Mp4FormatError("multiple ctts boxes are unsupported")
        composition_offsets = (
            self._expand_timing(ctts_boxes[0].entries, stsz.sample_count, "ctts")
            if ctts_boxes
            else [0] * stsz.sample_count
        )
        stss_boxes = find([track], "trak", "mdia", "minf", "stbl", "stss")
        if len(stss_boxes) > 1 or (stss_boxes and not isinstance(stss_boxes[0], StssBox)):
            raise Mp4FormatError("invalid stss table")
        sync_numbers = None
        if stss_boxes:
            sync_numbers = set(stss_boxes[0].sample_numbers)
            if any(number > stsz.sample_count for number in sync_numbers):
                raise Mp4FormatError("stss sample number exceeds sample count")

        media_ranges = [
            (box.data_start, box.data_end)
            for box in find(boxes, "mdat")
            if hasattr(box, "data_start") and hasattr(box, "data_end")
        ]
        if stsz.sample_count and not media_ranges:
            raise Mp4FormatError("video samples exist but no mdat box was found")
        self._samples = self._map_samples(
            stsz,
            stsc,
            offsets,
            durations,
            composition_offsets,
            sync_numbers,
            media_ranges,
        )

    @staticmethod
    def _first_video_track(boxes) -> TrakBox:
        for track in find(boxes, "moov", "trak"):
            if not isinstance(track, TrakBox):
                continue
            handlers = [
                box.handler_type
                for box in find([track], "trak", "mdia", "hdlr")
                if isinstance(box, HdlrBox)
            ]
            if "vide" in handlers:
                return track
        raise Mp4FormatError("MP4 contains no video track")

    @staticmethod
    def _one_box(track: TrakBox, kind: str, box_type):
        boxes = find([track], "trak", "mdia", "minf", "stbl", kind)
        if len(boxes) != 1 or not isinstance(boxes[0], box_type):
            raise Mp4FormatError(f"video track must contain one {kind} box")
        return boxes[0]

    @staticmethod
    def _expand_timing(entries: Sequence[Tuple[int, int]], sample_count: int, name: str) -> List[int]:
        values = []
        for count, value in entries:
            if count > sample_count - len(values):
                raise Mp4FormatError(f"{name} timing entries exceed sample count")
            values.extend([value] * count)
        if len(values) != sample_count:
            raise Mp4FormatError(f"{name} timing entries do not cover all samples")
        return values

    def _map_samples(
        self,
        stsz: StszBox,
        stsc: StscBox,
        chunk_offsets: Sequence[int],
        durations: Sequence[int],
        composition_offsets: Sequence[int],
        sync_numbers: Optional[set],
        media_ranges: Sequence[Tuple[int, int]],
    ) -> List[_SampleLocation]:
        if stsz.sample_count == 0:
            return []
        if not stsc.entries or not chunk_offsets:
            raise Mp4FormatError("sample table lacks chunk mapping entries")
        locations = []
        sample_index = 0
        run_index = 0
        dts = 0
        for chunk_number, chunk_offset in enumerate(chunk_offsets, 1):
            while (
                run_index + 1 < len(stsc.entries)
                and stsc.entries[run_index + 1][0] <= chunk_number
            ):
                run_index += 1
            first_chunk, samples_per_chunk, description_index = stsc.entries[run_index]
            if first_chunk > chunk_number or description_index != 1:
                raise Mp4FormatError("chunk uses an unsupported sample description")
            sample_offset = chunk_offset
            for _ in range(samples_per_chunk):
                if sample_index >= stsz.sample_count:
                    raise Mp4FormatError("chunk map contains more samples than stsz")
                size = stsz.sample_size(sample_index)
                sample_end = sample_offset + size
                if size == 0 or not any(
                    start <= sample_offset and sample_end <= end
                    for start, end in media_ranges
                ):
                    raise Mp4FormatError("sample byte range is outside every mdat payload")
                locations.append(
                    _SampleLocation(
                        offset=sample_offset,
                        size=size,
                        dts_ticks=dts,
                        pts_ticks=dts + composition_offsets[sample_index],
                        duration_ticks=durations[sample_index],
                        is_sync=(sample_index + 1 in sync_numbers) if sync_numbers is not None else True,
                    )
                )
                sample_offset = sample_end
                dts += durations[sample_index]
                sample_index += 1
        if sample_index != stsz.sample_count:
            raise Mp4FormatError("chunk map does not cover every stsz sample")
        return locations

    @property
    def sample_count(self) -> int:
        return len(self._samples)

    def first_sync_sample_index(self) -> Optional[int]:
        for index, location in enumerate(self._samples):
            if location.is_sync:
                return index
        return None

    def sample_at(self, index: int) -> CompressedSample:
        if (
            not isinstance(index, int)
            or isinstance(index, bool)
            or not 0 <= index < len(self._samples)
        ):
            raise IndexError("sample index is out of range")
        if self._closed or self._file is None:
            raise ValueError("sample reader is closed")
        location = self._samples[index]
        self._file.seek(location.offset)
        data = self._file.read(location.size)
        if len(data) != location.size:
            raise Mp4FormatError("truncated MP4 sample data")
        try:
            _validate_length_prefixed_nals(data)
        except Mp4FormatError as error:
            raise Mp4FormatError(f"sample {index}: {error}") from error
        return CompressedSample(
            index=index,
            data=data,
            dts_ticks=location.dts_ticks,
            pts_ticks=location.pts_ticks,
            duration_ticks=location.duration_ticks,
            is_sync=location.is_sync,
        )

    def decode_order_dependency_samples(
        self, presentation_index: int
    ) -> tuple[CompressedSample, ...]:
        if (
            not isinstance(presentation_index, int)
            or isinstance(presentation_index, bool)
            or not 0 <= presentation_index < len(self._samples)
        ):
            raise IndexError("presentation frame index is out of range")
        display_order = sorted(
            range(len(self._samples)),
            key=lambda index: (self._samples[index].pts_ticks, index),
        )
        target_index = display_order[presentation_index]
        sync_index = next(
            (index for index in range(target_index, -1, -1) if self._samples[index].is_sync),
            None,
        )
        if sync_index is None:
            raise Mp4FormatError("presentation frame has no preceding sync sample")
        gop_end = next(
            (
                index
                for index in range(sync_index + 1, len(self._samples))
                if self._samples[index].is_sync
            ),
            len(self._samples),
        )
        target_pts = self._samples[target_index].pts_ticks
        dependency_end = max(
            index
            for index in range(sync_index, gop_end)
            if self._samples[index].pts_ticks <= target_pts
        )
        return tuple(self.sample_at(index) for index in range(sync_index, dependency_end + 1))

    def next_sample(self) -> Optional[CompressedSample]:
        if self._closed or self._file is None:
            raise ValueError("sample reader is closed")
        if self._next_index == len(self._samples):
            return None
        sample = self.sample_at(self._next_index)
        self._next_index += 1
        return sample

    def __iter__(self) -> Iterator[CompressedSample]:
        while True:
            sample = self.next_sample()
            if sample is None:
                return
            yield sample

    def close(self) -> None:
        if self._file is not None:
            self._file.close()
        self._file = None
        self._closed = True

    def __enter__(self) -> "VideoSampleReader":
        if self._closed:
            raise ValueError("sample reader is closed")
        return self

    def __exit__(self, exc_type, exc, traceback) -> None:
        self.close()


def _validate_length_prefixed_nals(data: bytes) -> None:
    offset = 0
    while offset < len(data):
        if len(data) - offset < 4:
            raise Mp4FormatError("truncated NAL length prefix")
        nal_size = int.from_bytes(data[offset : offset + 4], "big")
        offset += 4
        if nal_size == 0:
            raise Mp4FormatError("NAL unit has zero length")
        if nal_size > len(data) - offset:
            raise Mp4FormatError("NAL unit is truncated")
        offset += nal_size