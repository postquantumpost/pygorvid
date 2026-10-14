"""Decoder contract for the native H.264 frame pipeline."""

from collections.abc import Callable
from dataclasses import dataclass
import math
import os
from pathlib import Path
import pickle
import sys
import time
from typing import Optional, Sequence

from .cabac import (
    CABACArithmeticDecoder,
    CABACError,
    CABACIIntraMacroblockInput,
    CABACPBMacroblockLayerInput,
    CABACSliceMacroblockResult,
    initialize_slice_contexts,
)

from .reconstruction import (
    PresentationOrderBuffer,
    PresentationPicture,
    ReferencePicture,
    ReferencePictureBuffer,
    Yuv420Frame,
    Yuv420FrameBuilder,
)
from .nal import NALUnitError, ebsp_to_rbsp, parse_nal_header, split_length_prefixed_nals
from .pps import PPSParseError, parse_pps
from .slice import (
    MemoryManagementOperation,
    SliceHeader,
    SliceHeaderError,
    parse_slice_header,
    same_primary_picture,
)
from .sps import SPSParseError, parse_sps
from .videosamplereader import VideoSampleReader


class DecoderError(NotImplementedError):
    """Raised while the native decoder pipeline is still under construction."""


class DecoderReferenceMismatch(DecoderError):
    """Raised when cached frame data does not match the expected reference bytes."""


class DecoderTimeLimitExceeded(DecoderError):
    """Raised when a configured frame-decode time limit expires."""


def default_decoder_threads() -> int:
    """Return half the available processors, at least one."""
    return max(1, (os.cpu_count() or 1) // 2)


@dataclass(frozen=True)
class DependencySlice:
    sample_index: int
    dts_ticks: int
    pts_ticks: int
    is_sync: bool
    nal_unit: bytes
    header: SliceHeader


def _apply_adaptive_reference_marking(
    reference_buffer: ReferencePictureBuffer,
    current_frame_num: int,
    maximum_frame_num: int,
    operations: Sequence[MemoryManagementOperation],
    maximum_long_term_frame_idx: Optional[int],
) -> tuple[Optional[int], bool, Optional[int]]:
    if maximum_frame_num <= 0 or not 0 <= current_frame_num < maximum_frame_num:
        raise ValueError("current frame number is outside MaxFrameNum")
    current_long_term_frame_idx = None
    reset_frame_num = False
    for command in operations:
        operands = command.operands

        def operand(index: int) -> int:
            if index >= len(operands):
                raise ValueError("decoded-reference marking operand is missing")
            return operands[index]

        if command.operation in (1, 3):
            picture_number = current_frame_num - operand(0) - 1
            target = next((
                reference for reference in reference_buffer.references()
                if reference.long_term_frame_idx is None
                and reference.frame_num < maximum_frame_num
                and (reference.frame_num - maximum_frame_num
                     if reference.frame_num > current_frame_num else reference.frame_num)
                == picture_number
            ), None)
            if target is None:
                raise ValueError("decoded-reference marking short-term target is unavailable")
            if command.operation == 1:
                reference_buffer.remove(target.identifier)
            else:
                long_term_frame_idx = operand(1)
                if maximum_long_term_frame_idx is None or long_term_frame_idx > maximum_long_term_frame_idx:
                    raise ValueError("long-term frame index exceeds MaxLongTermFrameIdx")
                for reference in reference_buffer.references():
                    if reference.long_term_frame_idx == long_term_frame_idx:
                        reference_buffer.remove(reference.identifier)
                if not reference_buffer.set_long_term_frame_idx(target.identifier, long_term_frame_idx):
                    raise ValueError("decoded-reference marking target disappeared")
        elif command.operation == 2:
            long_term_pic_num = operand(0)
            target = next((
                reference for reference in reference_buffer.references()
                if reference.long_term_frame_idx == long_term_pic_num
            ), None)
            if target is None:
                raise ValueError("decoded-reference marking long-term target is unavailable")
            reference_buffer.remove(target.identifier)
        elif command.operation == 4:
            maximum_plus_one = operand(0)
            maximum_long_term_frame_idx = maximum_plus_one - 1 if maximum_plus_one else None
            for reference in reference_buffer.references():
                if reference.long_term_frame_idx is not None and (
                    maximum_long_term_frame_idx is None
                    or reference.long_term_frame_idx > maximum_long_term_frame_idx
                ):
                    reference_buffer.remove(reference.identifier)
        elif command.operation == 5:
            reference_buffer.clear()
            maximum_long_term_frame_idx = None
            current_long_term_frame_idx = None
            reset_frame_num = True
        elif command.operation == 6:
            long_term_frame_idx = operand(0)
            if maximum_long_term_frame_idx is None or long_term_frame_idx > maximum_long_term_frame_idx:
                raise ValueError("long-term frame index exceeds MaxLongTermFrameIdx")
            for reference in reference_buffer.references():
                if reference.long_term_frame_idx == long_term_frame_idx:
                    reference_buffer.remove(reference.identifier)
            current_long_term_frame_idx = long_term_frame_idx
        else:
            raise ValueError("decoded-reference marking operation is invalid")
    return current_long_term_frame_idx, reset_frame_num, maximum_long_term_frame_idx


def _colocated_motion_facts(macroblock_result):
    from .cabac import (
        CABACBDirectMacroblockResult,
        CABACIIntraMacroblockResult,
        CABACInterMacroblockResult,
        CABACPBMacroblockLayerResult,
        CABACPSkipMacroblockResult,
    )
    from .reconstruction import ColocatedMotionFact, MotionVector

    decoded = macroblock_result
    if isinstance(decoded, CABACPBMacroblockLayerResult):
        decoded = decoded.macroblock
    facts = []
    for cell in range(16):
        x, y = (cell % 4) * 4, (cell // 4) * 4
        if isinstance(decoded, CABACIIntraMacroblockResult):
            facts.append(ColocatedMotionFact(-1, MotionVector(0, 0)))
        elif isinstance(decoded, CABACPSkipMacroblockResult):
            facts.append(
                ColocatedMotionFact(decoded.reference_index, MotionVector(*decoded.motion_vector))
            )
        elif isinstance(decoded, CABACBDirectMacroblockResult):
            list_index = 0 if decoded.prediction_flags[0] else 1
            reference_index = decoded.reference_indices[list_index]
            vector = decoded.motion_vectors[list_index]
            facts.append(ColocatedMotionFact(reference_index, MotionVector(*vector)))
        elif isinstance(decoded, CABACInterMacroblockResult):
            partition = next(
                (
                    item
                    for item in decoded.partitions
                    if item.x <= x < item.x + item.width
                    and item.y <= y < item.y + item.height
                ),
                None,
            )
            if partition is None:
                raise DecoderError("decoded reference macroblock has incomplete motion partitions")
            list_index = 0 if partition.prediction_flags[0] else 1
            if not partition.prediction_flags[list_index]:
                facts.append(ColocatedMotionFact(-1, MotionVector(0, 0)))
            else:
                facts.append(
                    ColocatedMotionFact(
                        partition.reference_indices[list_index],
                        MotionVector(*partition.motion_vectors[list_index]),
                    )
                )
        else:
            raise DecoderError("decoded reference macroblock has invalid motion metadata")
    return tuple(facts)


def _picture_deblocking_macroblock(
    entry,
    dependency_slice: DependencySlice,
    reference_lists: tuple[Sequence[ReferencePicture], Sequence[ReferencePicture]],
    picture_set,
    slice_id: int,
):
    from .cabac import (
        CABACBDirectMacroblockResult,
        CABACIIntraMacroblockResult,
        CABACInterMacroblockResult,
        CABACPBMacroblockLayerResult,
        CABACPSkipMacroblockResult,
        _LUMA4X4_BLOCK_SCAN_TO_RASTER,
    )
    from .reconstruction import LumaPredictionVector, MotionVector, PictureDeblockingMacroblock

    decoded = entry.macroblock
    qpy = None
    if isinstance(decoded, CABACPBMacroblockLayerResult):
        qpy = decoded.qpy
        decoded = decoded.macroblock

    nonzero = [False] * 16
    if isinstance(decoded, CABACIIntraMacroblockResult):
        coded_flags = decoded.luma_4x4_coded_block_flags
        ac_flags = decoded.luma_ac_coded_block_flags
        flags = (
            tuple(coded or ac for coded, ac in zip(coded_flags, ac_flags))
            if coded_flags and ac_flags
            else coded_flags or ac_flags
        )
        qpy = 0 if decoded.macroblock_type == 25 else decoded.qpy
        transform_size_8x8 = decoded.transform_size_8x8
        intra = True
    elif isinstance(decoded, CABACPSkipMacroblockResult):
        flags = ()
        transform_size_8x8 = False
        intra = False
    elif isinstance(decoded, CABACBDirectMacroblockResult):
        flags = ()
        transform_size_8x8 = False
        intra = False
    elif isinstance(decoded, CABACInterMacroblockResult):
        flags = decoded.luma_4x4_coded_block_flags
        qpy = decoded.qpy
        transform_size_8x8 = decoded.transform_size_8x8
        intra = False
    else:
        raise DecoderError("decoded macroblock has invalid deblocking metadata")
    for scan_index, flag in enumerate(flags):
        nonzero[_LUMA4X4_BLOCK_SCAN_TO_RASTER[scan_index]] = flag

    predictions = []
    for cell in range(16):
        x, y = (cell % 4) * 4, (cell // 4) * 4
        if isinstance(decoded, CABACPSkipMacroblockResult):
            sources = ((0, decoded.reference_index, decoded.motion_vector),)
        elif isinstance(decoded, CABACBDirectMacroblockResult):
            sources = tuple(
                (list_index, decoded.reference_indices[list_index], decoded.motion_vectors[list_index])
                for list_index in range(2)
                if decoded.prediction_flags[list_index]
            )
        elif isinstance(decoded, CABACInterMacroblockResult):
            partition = next(
                (
                    item for item in decoded.partitions
                    if item.x <= x < item.x + item.width and item.y <= y < item.y + item.height
                ),
                None,
            )
            if partition is None:
                raise DecoderError("decoded inter macroblock has incomplete partitions")
            sources = tuple(
                (list_index, partition.reference_indices[list_index], partition.motion_vectors[list_index])
                for list_index in range(2)
                if partition.prediction_flags[list_index]
            )
        else:
            sources = ()
        cell_predictions = []
        for list_index, reference_index, vector in sources:
            if not 0 <= reference_index < len(reference_lists[list_index]):
                raise DecoderError("decoded inter macroblock references an unavailable picture")
            cell_predictions.append(
                LumaPredictionVector(
                    reference_lists[list_index][reference_index].identifier,
                    MotionVector(*vector),
                )
            )
        predictions.append(tuple(cell_predictions))

    if qpy is None:
        raise DecoderError("decoded macroblock has no QPY for deblocking")
    return PictureDeblockingMacroblock(
        qpy=qpy,
        intra=intra,
        transform_size_8x8=transform_size_8x8,
        nonzero=tuple(nonzero),
        predictions=tuple(predictions),
        slice_id=slice_id,
        disable_idc=dependency_slice.header.disable_deblocking_filter_idc or 0,
        alpha_offset_div2=dependency_slice.header.slice_alpha_c0_offset_div2,
        beta_offset_div2=dependency_slice.header.slice_beta_offset_div2,
        chroma_qp_offsets=(
            picture_set.chroma_qp_index_offset,
            picture_set.second_chroma_qp_index_offset,
        ),
    )


class H264Decoder:
    """Own H.264 sample and picture state for the native decode pipeline."""

    def __init__(
        self,
        sample_reader: Optional[VideoSampleReader] = None,
        *,
        debug_progress: bool = False,
        time_limit_seconds: Optional[float] = None,
        checkpoint_path: Optional[str | Path] = None,
        checkpoint_dir: Optional[str | Path] = None,
        threads: Optional[int] = None,
    ):
        if threads is not None and (
            isinstance(threads, bool) or not isinstance(threads, int) or threads < 1
        ):
            raise ValueError("decoder thread count must be a positive integer")
        self.threads = default_decoder_threads() if threads is None else threads
        if time_limit_seconds is not None and (
            isinstance(time_limit_seconds, bool)
            or not isinstance(time_limit_seconds, (int, float))
            or not math.isfinite(time_limit_seconds)
            or time_limit_seconds <= 0
        ):
            raise ValueError("decoder time limit must be a positive finite number of seconds")
        self.sample_reader = sample_reader
        self.debug_progress = debug_progress
        self.time_limit_seconds = (
            None if time_limit_seconds is None else float(time_limit_seconds)
        )
        self.sample_count = 0
        self.sequence_parameter_sets = ()
        self.picture_parameter_sets = ()
        self.reference_picture_buffer = ReferencePictureBuffer()
        self.presentation_order_buffer = PresentationOrderBuffer(0)
        self._decoded_frames: dict[int, Yuv420Frame] = {}
        self.unsupported_feature = False
        self._poc_type0_state = None
        self._poc_type12_state = None
        self._poc_cache = None
        self._parsed_parameter_sets = None
        self._gop_sync_sample_index: Optional[int] = None
        self._last_decoded_sample_index: Optional[int] = None
        self._maximum_long_term_frame_idx: Optional[int] = None

        env_checkpoint = os.environ.get("PYGORVID_CHECKPOINT_PATH")
        env_checkpoint_dir = os.environ.get("PYGORVID_CHECKPOINT_DIR")
        if checkpoint_path is not None:
            self.checkpoint_path = Path(checkpoint_path)
        elif env_checkpoint:
            self.checkpoint_path = Path(env_checkpoint)
        elif (checkpoint_dir is not None or env_checkpoint_dir) and sample_reader is not None:
            base_dir = Path(checkpoint_dir if checkpoint_dir is not None else env_checkpoint_dir)
            reader_path = getattr(sample_reader, "path", None) or getattr(sample_reader, "_path", None)
            stem = Path(reader_path).stem if reader_path else "decoder"
            self.checkpoint_path = base_dir / f"{stem}.ckpt"
        else:
            self.checkpoint_path = None

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

        if self.checkpoint_path is not None and self.checkpoint_path.is_file():
            self.load_checkpoint(self.checkpoint_path)

    def _parsed_parameter_sets_by_id(self):
        if self._parsed_parameter_sets is None:
            sequence_sets_by_id = {}
            picture_sets_by_id = {}
            for nal in self.sequence_parameter_sets:
                sequence_set = parse_sps(nal)
                sequence_sets_by_id.setdefault(sequence_set.sps_id, sequence_set)
            for nal in self.picture_parameter_sets:
                picture_set = parse_pps(nal)
                picture_sets_by_id.setdefault(picture_set.picture_parameter_set_id, picture_set)
            self._parsed_parameter_sets = sequence_sets_by_id, picture_sets_by_id
        return self._parsed_parameter_sets

    def store_reference_picture(
        self,
        reference: ReferencePicture,
        frame: Yuv420Frame,
        macroblocks: Sequence[CABACSliceMacroblockResult] = (),
    ) -> None:
        motion_field = {}
        for entry in macroblocks:
            if entry.address in motion_field:
                raise DecoderError("decoded reference picture repeats a macroblock address")
            motion_field[entry.address] = _colocated_motion_facts(entry.macroblock)
        self.reference_picture_buffer.store(reference, frame, motion_field)

    def queue_presentation(self, picture: PresentationPicture) -> PresentationPicture | None:
        return self.presentation_order_buffer.push(picture)

    def _report_frame_progress(self, frame_index: int, sample_index: int, percent: int) -> None:
        if self.debug_progress:
            print(
                f"Frame {frame_index} (sample {sample_index}): {percent}%",
                file=sys.stderr,
                flush=True,
            )

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

    def save_checkpoint(self, path: str | Path) -> None:
        """Save decoder GOP state, reference pictures, and decoded frames to a checkpoint file."""
        target_path = Path(path)
        target_dir = target_path.parent
        target_dir.mkdir(parents=True, exist_ok=True)
        temp_path = target_dir / f".tmp_{target_path.name}_{os.getpid()}_{time.monotonic_ns()}"
        state = {
            "version": 1,
            "gop_sync_sample_index": self._gop_sync_sample_index,
            "last_decoded_sample_index": self._last_decoded_sample_index,
            "reference_pictures": dict(self.reference_picture_buffer._pictures),
            "reference_motion_fields": dict(self.reference_picture_buffer._motion_fields),
            "decoded_frames": dict(self._decoded_frames),
            "poc_type0_state": self._poc_type0_state,
            "poc_type12_state": self._poc_type12_state,
            "poc_cache": self._poc_cache,
            "maximum_long_term_frame_idx": self._maximum_long_term_frame_idx,
            "sequence_parameter_sets": self.sequence_parameter_sets,
            "picture_parameter_sets": self.picture_parameter_sets,
            "sample_count": self.sample_count,
        }
        try:
            with open(temp_path, "wb") as file:
                pickle.dump(state, file, protocol=pickle.HIGHEST_PROTOCOL)
                file.flush()
                os.fsync(file.fileno())
            os.replace(temp_path, target_path)
        except Exception:
            if temp_path.exists():
                try:
                    temp_path.unlink()
                except OSError:
                    pass
            raise

    def load_checkpoint(self, path: str | Path) -> None:
        """Restore decoder GOP state, reference pictures, and decoded frames from a checkpoint file."""
        target_path = Path(path)
        if not target_path.is_file():
            raise FileNotFoundError(f"checkpoint file not found: {target_path}")
        try:
            with open(target_path, "rb") as file:
                state = pickle.load(file)
        except Exception as error:
            raise DecoderError(f"corrupt checkpoint file {target_path}: {error}") from error
        if not isinstance(state, dict) or state.get("version") != 1:
            raise DecoderError("invalid or unsupported checkpoint file format")
        if self.sample_reader is not None:
            ckpt_sps = state.get("sequence_parameter_sets")
            ckpt_pps = state.get("picture_parameter_sets")
            if (
                self.sequence_parameter_sets
                and ckpt_sps is not None
                and self.sequence_parameter_sets != ckpt_sps
            ) or (
                self.picture_parameter_sets
                and ckpt_pps is not None
                and self.picture_parameter_sets != ckpt_pps
            ):
                raise DecoderError("checkpoint parameter sets do not match the loaded video stream")

        self._gop_sync_sample_index = state.get("gop_sync_sample_index")
        self._last_decoded_sample_index = state.get("last_decoded_sample_index")
        self.reference_picture_buffer.clear()
        self.reference_picture_buffer._pictures.update(state.get("reference_pictures", {}))
        self.reference_picture_buffer._motion_fields.update(state.get("reference_motion_fields", {}))
        self._decoded_frames.clear()
        self._decoded_frames.update(state.get("decoded_frames", {}))
        self._poc_type0_state = state.get("poc_type0_state")
        self._poc_type12_state = state.get("poc_type12_state")
        self._poc_cache = state.get("poc_cache")
        self._maximum_long_term_frame_idx = state.get("maximum_long_term_frame_idx")

    def dependency_slice_pictures(
        self, presentation_index: int
    ) -> tuple[tuple[DependencySlice, ...], ...]:
        """Parse and group coded slices from the dependency window without moving the reader cursor."""
        if self.sample_reader is None:
            raise DecoderError("H.264 decoder requires a sample reader")
        configuration = getattr(self.sample_reader, "configuration", None)
        if configuration is None:
            raise DecoderError("sample reader does not expose AVC configuration")
        try:
            samples = self.sample_reader.decode_order_dependency_samples(presentation_index)
        except (IndexError, ValueError) as error:
            raise DecoderError(f"cannot read frame dependency window: {error}") from error

        try:
            sequence_by_id, picture_sets_by_id = self._parsed_parameter_sets_by_id()
        except (SPSParseError, PPSParseError, ValueError) as error:
            raise DecoderError(f"invalid AVC parameter set: {error}") from error
        if not picture_sets_by_id:
            raise DecoderError("AVC configuration contains no picture parameter set")

        groups: list[list[DependencySlice]] = []
        for sample in samples:
            try:
                nal_units = split_length_prefixed_nals(sample.data, configuration.nal_length_size)
            except NALUnitError as error:
                raise DecoderError(f"sample {sample.index}: {error}") from error
            for nal_unit in nal_units:
                try:
                    nal_header = parse_nal_header(nal_unit)
                except NALUnitError as error:
                    raise DecoderError(f"sample {sample.index}: {error}") from error
                if nal_header.unit_type not in (1, 5):
                    if 1 <= nal_header.unit_type <= 5:
                        raise DecoderError(
                            f"unsupported VCL NAL unit type {nal_header.unit_type}"
                        )
                    continue

                parsed_header = None
                parse_error = None
                for picture_set in picture_sets_by_id.values():
                    sequence_set = sequence_by_id.get(picture_set.sequence_parameter_set_id)
                    if sequence_set is None:
                        continue
                    try:
                        parsed_header = parse_slice_header(nal_unit, sequence_set, picture_set)
                        break
                    except SliceHeaderError as error:
                        parse_error = error
                if parsed_header is None:
                    message = str(parse_error) if parse_error is not None else "slice references an unavailable SPS"
                    raise DecoderError(f"sample {sample.index}: {message}")

                dependency_slice = DependencySlice(
                    sample_index=sample.index,
                    dts_ticks=sample.dts_ticks,
                    pts_ticks=sample.pts_ticks,
                    is_sync=sample.is_sync,
                    nal_unit=nal_unit,
                    header=parsed_header,
                )
                if not groups or not same_primary_picture(groups[-1][0].header, parsed_header):
                    groups.append([dependency_slice])
                else:
                    groups[-1].append(dependency_slice)
        return tuple(tuple(group) for group in groups)

    def decode_dependency_slice_data(
        self,
        dependency_slice: DependencySlice,
        builder: Yuv420FrameBuilder,
        input_provider: Callable[
            [int, Sequence[CABACSliceMacroblockResult]],
            CABACIIntraMacroblockInput | CABACPBMacroblockLayerInput,
        ],
        progress_callback: Callable[[int], None] | None = None,
        timeout_check: Callable[[], None] | None = None,
    ) -> tuple[CABACSliceMacroblockResult, ...]:
        """Initialize and decode one parsed dependency slice using gathered macroblock inputs."""
        if self.sample_reader is None:
            raise DecoderError("H.264 decoder requires a sample reader")
        if (
            not isinstance(dependency_slice, DependencySlice)
            or builder is None
            or not callable(input_provider)
            or progress_callback is not None and not callable(progress_callback)
            or timeout_check is not None and not callable(timeout_check)
        ):
            raise DecoderError("dependency slice decode inputs are invalid")
        configuration = getattr(self.sample_reader, "configuration", None)
        if configuration is None:
            raise DecoderError("sample reader does not expose AVC configuration")

        try:
            sequence_sets_by_id, picture_sets_by_id = self._parsed_parameter_sets_by_id()
            picture_set = picture_sets_by_id.get(
                dependency_slice.header.picture_parameter_set_id
            )
            if picture_set is None:
                raise DecoderError("dependency slice references an unavailable PPS")
            sequence_set = sequence_sets_by_id.get(picture_set.sequence_parameter_set_id)
            if sequence_set is None:
                raise DecoderError("dependency slice references an unavailable SPS")
            if not picture_set.entropy_coding_mode:
                raise DecoderError("dependency slice does not use CABAC")
            bit_offset = dependency_slice.header.slice_data_bit_offset
            if bit_offset % 8:
                raise DecoderError("CABAC slice-data offset is not byte-aligned")
            rbsp = ebsp_to_rbsp(dependency_slice.nal_unit[1:])
            cabac = CABACArithmeticDecoder(
                rbsp[bit_offset // 8 :], timeout_check=timeout_check
            )
            slice_qpy = 26 + picture_set.pic_init_qp_minus26 + dependency_slice.header.slice_qp_delta
            contexts = initialize_slice_contexts(
                dependency_slice.header.slice_type,
                dependency_slice.header.cabac_init_idc or 0,
                slice_qpy,
            )
            macroblock_count = (sequence_set.coded_width // 16) * (sequence_set.coded_height // 16)
            reference_frames = tuple(
                self.reference_picture_buffer.get(reference.identifier).frame
                for reference in self.reference_picture_buffer.references()
            )
            return cabac.decode_slice_data(
                dependency_slice.header.slice_type,
                dependency_slice.header.first_macroblock_in_slice,
                macroblock_count,
                contexts,
                builder,
                input_provider,
                progress_callback,
                threads=getattr(self, "threads", 1),
                reference_frames=reference_frames,
            )
        except (CABACError, NALUnitError, PPSParseError, SPSParseError, ValueError) as error:
            raise DecoderError(f"sample {dependency_slice.sample_index}: {error}") from error

    def _picture_order_count(self, header: SliceHeader, sequence_set) -> int:
        from .reconstruction import POCType0State, POCType12State

        identity = header.picture_identity()
        if self._poc_cache is not None and self._poc_cache[0] == identity:
            return self._poc_cache[1]
        max_frame_num = 1 << (sequence_set.log2_max_frame_num_minus4 + 4)
        if sequence_set.pic_order_cnt_type == 0:
            max_poc_lsb = 1 << ((sequence_set.log2_max_pic_order_cnt_lsb_minus4 or 0) + 4)
            if self._poc_type0_state is None:
                self._poc_type0_state = POCType0State()
            current_poc = self._poc_type0_state.calculate(
                max_poc_lsb,
                header.pic_order_cnt_lsb,
                header.delta_pic_order_bottom,
                header.nal_ref_idc,
                header.idr,
            )
        else:
            if self._poc_type12_state is None:
                self._poc_type12_state = POCType12State()
            if sequence_set.pic_order_cnt_type == 1:
                current_poc = self._poc_type12_state.calculate_type1(
                    max_frame_num,
                    header.frame_num,
                    header.delta_pic_order_cnt0,
                    header.delta_pic_order_cnt1,
                    sequence_set.offset_for_non_ref_pic or 0,
                    sequence_set.offset_for_top_to_bottom_field or 0,
                    sequence_set.offset_for_ref_frame,
                    header.nal_ref_idc,
                    header.idr,
                )
            else:
                current_poc = self._poc_type12_state.calculate_type2(
                    max_frame_num,
                    header.frame_num,
                    header.nal_ref_idc,
                    header.idr,
                )
        self._poc_cache = identity, current_poc
        return current_poc

    def intra_macroblock_input_from_history(
        self,
        dependency_slice: DependencySlice,
        macroblock_address: int,
        history: Sequence[CABACSliceMacroblockResult],
    ) -> CABACIIntraMacroblockInput:
        """Gather I-slice neighbors; Intra_8x8 edges stay raw for reconstruction-time filtering."""
        from .cabac import (
            CABACChroma420EdgeState,
            CABACChroma420References,
            CABACIIntraMacroblockResult,
            CABACInterMacroblockResult,
            CABACIntra16x16EdgeState,
            CABACIntra4x4EdgeState,
            CABACPBMacroblockLayerResult,
            derive_cabac_macroblock_edge_samples,
            derive_cabac_macroblock_neighbor_coded_state,
            derive_cabac_slice_neighbor_indices,
            _LUMA4X4_BLOCK_SCAN_TO_RASTER,
        )
        from .reconstruction import LumaIntra4x4Block, LumaIntra8x8Block

        if self.sample_reader is None:
            raise DecoderError("H.264 decoder requires a sample reader")
        if not isinstance(dependency_slice, DependencySlice):
            raise DecoderError("dependency slice is invalid")
        if dependency_slice.header.slice_type % 5 not in (0, 1, 2):
            raise DecoderError("history input builder requires an I-, P-, or B-slice")
        configuration = getattr(self.sample_reader, "configuration", None)
        if configuration is None:
            raise DecoderError("sample reader does not expose AVC configuration")

        try:
            sequence_sets_by_id, picture_sets_by_id = self._parsed_parameter_sets_by_id()
            picture_set = picture_sets_by_id.get(
                dependency_slice.header.picture_parameter_set_id
            )
            if picture_set is None:
                raise DecoderError("dependency slice references an unavailable PPS")
            sequence_set = sequence_sets_by_id.get(picture_set.sequence_parameter_set_id)
            if sequence_set is None:
                raise DecoderError("dependency slice references an unavailable SPS")
            width_in_mbs = sequence_set.coded_width // 16
            picture_macroblock_count = width_in_mbs * (sequence_set.coded_height // 16)
            if not 0 <= macroblock_address < picture_macroblock_count:
                raise DecoderError("macroblock address is outside the picture")
            neighbor_indices = derive_cabac_slice_neighbor_indices(
                macroblock_address,
                width_in_mbs,
                dependency_slice.header.first_macroblock_in_slice,
                len(history),
            )

            def entry_at(index: int | None, expected_address: int | None):
                if index is None or expected_address is None:
                    return None
                if index >= len(history) or history[index].address != expected_address:
                    raise DecoderError("decoded slice history is not contiguous")
                return history[index]

            left_entry = entry_at(
                neighbor_indices.left,
                macroblock_address - 1 if macroblock_address % width_in_mbs else None,
            )
            top_entry = entry_at(
                neighbor_indices.top,
                macroblock_address - width_in_mbs
                if macroblock_address >= width_in_mbs
                else None,
            )
            top_right_entry = entry_at(
                neighbor_indices.top_right,
                macroblock_address - width_in_mbs + 1
                if macroblock_address >= width_in_mbs
                and macroblock_address % width_in_mbs + 1 < width_in_mbs
                else None,
            )
            top_left_entry = entry_at(
                neighbor_indices.top_left,
                macroblock_address - width_in_mbs - 1
                if macroblock_address >= width_in_mbs
                and macroblock_address % width_in_mbs
                else None,
            )

            def neighbor_state(entry, position: str):
                return (
                    derive_cabac_macroblock_neighbor_coded_state(entry, position)
                    if entry is not None
                    else None
                )

            left_state = neighbor_state(left_entry, "left")
            top_state = neighbor_state(top_entry, "top")
            top_right_state = neighbor_state(top_right_entry, "top")
            top_left_state = neighbor_state(top_left_entry, "left")

            def prediction_available(entry, state) -> bool:
                if entry is None or state is None:
                    return False
                if not picture_set.constrained_intra_pred:
                    return True
                return state.is_intra_nxn or state.is_intra16x16 or state.is_ipcm

            left_prediction_available = prediction_available(left_entry, left_state)
            top_prediction_available = prediction_available(top_entry, top_state)
            top_right_prediction_available = prediction_available(top_right_entry, top_right_state)
            top_left_prediction_available = prediction_available(top_left_entry, top_left_state)
            left_samples = (
                derive_cabac_macroblock_edge_samples(left_entry, "left")
                if left_prediction_available
                else None
            )
            top_samples = (
                derive_cabac_macroblock_edge_samples(top_entry, "top")
                if top_prediction_available
                else None
            )
            top_right_samples = (
                derive_cabac_macroblock_edge_samples(top_right_entry, "top")
                if top_right_prediction_available
                else None
            )
            top_left_samples = (
                derive_cabac_macroblock_edge_samples(top_left_entry, "left")
                if top_left_prediction_available
                else None
            )

            def top_references(edge_start: int, count: int) -> tuple[int, ...] | None:
                if top_samples is None:
                    return None
                result = []
                for column in range(edge_start, edge_start + count):
                    if column < 16:
                        result.append(top_samples.luma[column])
                    elif top_right_samples is not None:
                        result.append(top_right_samples.luma[column - 16])
                    else:
                        result.append(result[-1] if result else top_samples.luma[-1])
                return tuple(result)

            def left_references(edge_start: int, count: int) -> tuple[int, ...] | None:
                if left_samples is None:
                    return None
                result = []
                for row in range(edge_start, edge_start + count):
                    result.append(left_samples.luma[row] if row < 16 else left_samples.luma[-1])
                return tuple(result)

            def boundary_top_left(block_x: int, block_y: int) -> int | None:
                if block_y == 0:
                    if block_x == 0:
                        return None if top_left_samples is None else top_left_samples.luma[-1]
                    return None if top_samples is None else top_samples.luma[block_x - 1]
                if block_x == 0:
                    return None if left_samples is None else left_samples.luma[block_y - 1]
                return None

            luma4_blocks = []
            for raster_index in _LUMA4X4_BLOCK_SCAN_TO_RASTER:
                block_x = (raster_index % 4) * 4
                block_y = (raster_index // 4) * 4
                luma4_blocks.append(
                    LumaIntra4x4Block(
                        2,
                        top=top_references(block_x, 8) if block_y == 0 else None,
                        left=left_references(block_y, 8) if block_x == 0 else None,
                        top_left=boundary_top_left(block_x, block_y),
                    )
                )

            luma8_blocks = []
            for block_y in (0, 8):
                for block_x in (0, 8):
                    top = top_references(block_x, 16) if block_y == 0 else None
                    left8 = left_references(block_y, 8) if block_x == 0 else None
                    left = None if left8 is None else left8 + (left8[-1],) * 8
                    luma8_blocks.append(
                        LumaIntra8x8Block(
                            2,
                            top=top,
                            left=left,
                            top_left=boundary_top_left(block_x, block_y),
                            references_filtered=False,
                        )
                    )

            corner_cb = 0 if top_left_samples is None else top_left_samples.cb[-1]
            corner_cr = 0 if top_left_samples is None else top_left_samples.cr[-1]
            chroma_references = tuple(
                CABACChroma420References(
                    top_available=top_prediction_available,
                    left_available=left_prediction_available,
                    top_left_available=top_left_prediction_available,
                    top=(top_samples.cb if component == 0 else top_samples.cr)
                    if top_samples is not None
                    else (0,) * 8,
                    left=(left_samples.cb if component == 0 else left_samples.cr)
                    if left_samples is not None
                    else (0,) * 8,
                    top_left=corner_cb if component == 0 else corner_cr,
                )
                for component in range(2)
            )
            slice_qpy = (
                26 + picture_set.pic_init_qp_minus26 + dependency_slice.header.slice_qp_delta
            )
            previous_qpy, previous_qp_delta = slice_qpy, 0
            for previous in reversed(history):
                previous_macroblock = previous.macroblock
                if isinstance(previous_macroblock, CABACPBMacroblockLayerResult):
                    previous_qpy = previous_macroblock.qpy
                    if previous_macroblock.skipped:
                        previous_qp_delta = 0
                        break
                    previous_macroblock = previous_macroblock.macroblock
                if isinstance(previous_macroblock, CABACIIntraMacroblockResult):
                    previous_qpy = previous_macroblock.qpy
                    previous_qp_delta = previous_macroblock.qp_delta
                    break
                if isinstance(previous_macroblock, CABACInterMacroblockResult):
                    previous_qpy = previous_macroblock.qpy
                    previous_qp_delta = previous_macroblock.qp_delta
                    break

            default_luma4_edge = CABACIntra4x4EdgeState()
            default_luma16_edge = CABACIntra16x16EdgeState()
            default_chroma_edge = CABACChroma420EdgeState()
            return CABACIIntraMacroblockInput(
                slice_type=dependency_slice.header.slice_type,
                left_available=left_entry is not None,
                left_intra16_or_pcm=bool(
                    left_state and (left_state.is_intra16x16 or left_state.is_ipcm)
                ),
                top_available=top_entry is not None,
                top_intra16_or_pcm=bool(
                    top_state and (top_state.is_intra16x16 or top_state.is_ipcm)
                ),
                previous_qpy=previous_qpy,
                previous_qp_delta=previous_qp_delta,
                left_luma_cbp=(
                    left_state.coded_block_pattern_luma
                    if left_state and not left_state.is_ipcm else 15
                ),
                top_luma_cbp=(
                    top_state.coded_block_pattern_luma
                    if top_state and not top_state.is_ipcm else 15
                ),
                left_chroma_cbp=(
                    2 if left_state and left_state.is_ipcm
                    else left_state.coded_block_pattern_chroma if left_state else 0
                ),
                top_chroma_cbp=(
                    2 if top_state and top_state.is_ipcm
                    else top_state.coded_block_pattern_chroma if top_state else 0
                ),
                transform_8x8_mode_enabled=picture_set.transform_8x8_mode,
                left_has_8x8_transform=bool(left_state and left_state.transform_size_8x8),
                top_has_8x8_transform=bool(top_state and top_state.transform_size_8x8),
                top_modes_4x4=top_state.luma_4x4_modes if top_prediction_available else (2,) * 4,
                left_modes_4x4=left_state.luma_4x4_modes if left_prediction_available else (2,) * 4,
                top_modes_8x8=top_state.luma_8x8_modes if top_prediction_available else (2,) * 2,
                left_modes_8x8=left_state.luma_8x8_modes if left_prediction_available else (2,) * 2,
                top_mode_available=top_prediction_available,
                left_mode_available=left_prediction_available,
                luma_4x4_top_edge=top_state.luma_4x4_edge if top_state else default_luma4_edge,
                luma_4x4_left_edge=left_state.luma_4x4_edge if left_state else default_luma4_edge,
                luma_16x16_top_edge=top_state.luma_16x16_edge if top_state else default_luma16_edge,
                luma_16x16_left_edge=left_state.luma_16x16_edge if left_state else default_luma16_edge,
                luma_4x4_blocks=tuple(luma4_blocks),
                luma_8x8_blocks=tuple(luma8_blocks),
                intra16x16_top=top_samples.luma if top_samples else (0,) * 16,
                intra16x16_left=left_samples.luma if left_samples else (0,) * 16,
                intra16x16_top_available=top_prediction_available,
                intra16x16_left_available=left_prediction_available,
                intra16x16_top_left=0 if top_left_samples is None else top_left_samples.luma[-1],
                intra16x16_top_left_available=top_left_prediction_available,
                chroma_left_mode_nonzero=bool(
                    left_state and left_prediction_available and left_state.chroma_mode_nonzero
                ),
                chroma_top_mode_nonzero=bool(
                    top_state and top_prediction_available and top_state.chroma_mode_nonzero
                ),
                chroma_references=chroma_references,
                chroma_left_edges=left_state.chroma_edges if left_state else (default_chroma_edge,) * 2,
                chroma_top_edges=top_state.chroma_edges if top_state else (default_chroma_edge,) * 2,
                chroma_qp_index_offsets=(
                    picture_set.chroma_qp_index_offset,
                    picture_set.second_chroma_qp_index_offset,
                ),
            )
        except (CABACError, PPSParseError, SPSParseError, ValueError) as error:
            raise DecoderError(f"sample {dependency_slice.sample_index}: {error}") from error

    def macroblock_input_from_history(
        self,
        dependency_slice: DependencySlice,
        macroblock_address: int,
        history: Sequence[CABACSliceMacroblockResult],
    ) -> CABACIIntraMacroblockInput | CABACPBMacroblockLayerInput:
        """Build the I or P/B CABAC input selected by this dependency slice."""
        if not isinstance(dependency_slice, DependencySlice):
            raise DecoderError("dependency slice is invalid")
        slice_type = dependency_slice.header.slice_type % 5
        if slice_type == 2:
            return self.intra_macroblock_input_from_history(
                dependency_slice, macroblock_address, history
            )
        if slice_type in (0, 1):
            return self.inter_macroblock_input_from_history(
                dependency_slice, macroblock_address, history
            )
        raise DecoderError("history input builder supports only I, P, and B slices")

    def inter_macroblock_input_from_history(
        self,
        dependency_slice: DependencySlice,
        macroblock_address: int,
        history: Sequence[CABACSliceMacroblockResult],
    ) -> CABACPBMacroblockLayerInput:
        """Build P/B slice inputs from the decoded DPB and earlier slice macroblocks."""
        from .cabac import (
            CABACChroma420EdgeState,
            CABACBDirectMacroblockInput,
            CABACBReferencePicture,
            CABACIntra4x4EdgeState,
            CABACInterMacroblockInput,
            CABACInterMacroblockResult,
            CABACInterMacroblockResidualInput,
            CABACIIntraMacroblockResult,
            CABACPBMacroblockLayerResult,
            derive_cabac_inter_macroblock_neighbor_state_from_history,
            derive_cabac_macroblock_neighbor_coded_state,
            derive_cabac_slice_neighbor_indices,
        )
        from .reconstruction import (
            apply_reference_list_modifications,
            build_b_reference_lists,
            build_p_reference_list,
        )

        header = dependency_slice.header
        if header.slice_type % 5 not in (0, 1):
            raise DecoderError("inter history input builder requires a P- or B-slice")
        configuration = getattr(self.sample_reader, "configuration", None)
        if configuration is None:
            raise DecoderError("sample reader does not expose AVC configuration")
        try:
            sequence_sets_by_id, picture_sets_by_id = self._parsed_parameter_sets_by_id()
            picture_set = picture_sets_by_id.get(header.picture_parameter_set_id)
            if picture_set is None:
                raise DecoderError("dependency slice references an unavailable PPS")
            sequence_set = sequence_sets_by_id.get(picture_set.sequence_parameter_set_id)
            if sequence_set is None:
                raise DecoderError("dependency slice references an unavailable SPS")
            if not picture_set.entropy_coding_mode:
                raise DecoderError("dependency slice does not use CABAC")
            if picture_set.weighted_pred or picture_set.weighted_bipred_idc:
                raise DecoderError("weighted inter prediction is unsupported")

            max_frame_num = 1 << (sequence_set.log2_max_frame_num_minus4 + 4)
            current_poc = self._picture_order_count(header, sequence_set)

            list0, list1 = self._reference_picture_lists(
                dependency_slice, picture_set, sequence_set, current_poc
            )
            references = self.reference_picture_buffer.references()
            active_l0 = len(list0) - 1
            active_l1 = max(0, len(list1) - 1)

            def decoded_references(pictures):
                result = []
                for reference in pictures:
                    decoded = self.reference_picture_buffer.get(reference.identifier)
                    if decoded is None:
                        raise DecoderError(
                            f"reference picture {reference.identifier} is not decoded"
                        )
                    result.append(
                        CABACBReferencePicture(
                            decoded.frame,
                            reference.picture_order_cnt,
                            reference.long_term_frame_idx is not None,
                        )
                    )
                return tuple(result)

            references0 = decoded_references(list0)
            references1 = decoded_references(list1)
            if not references0 or header.slice_type % 5 == 1 and not references1:
                raise DecoderError("P/B slice reference list is empty")

            width_in_mbs = sequence_set.coded_width // 16
            picture_macroblock_count = width_in_mbs * (sequence_set.coded_height // 16)
            if not 0 <= macroblock_address < picture_macroblock_count:
                raise DecoderError("macroblock address is outside the picture")

            # CABAC learns the partition geometry only after decoding mb_type. Provide
            # external facts for every 4x4 cell; current-MB facts are resolved by CABAC.
            neighbor_states = tuple(
                derive_cabac_inter_macroblock_neighbor_state_from_history(
                    macroblock_address,
                    width_in_mbs,
                    header.first_macroblock_in_slice,
                    history,
                    header.slice_type,
                    (cell % 4) * 4,
                    (cell // 4) * 4,
                    4,
                    4,
                )
                for cell in range(16)
            )
            neighbor_indices = derive_cabac_slice_neighbor_indices(
                macroblock_address,
                width_in_mbs,
                header.first_macroblock_in_slice,
                len(history),
            )

            def coded_neighbor_state(history_index, expected_address, direction):
                if history_index is None:
                    return None
                entry = history[history_index]
                if entry.address != expected_address:
                    raise DecoderError("decoded slice history is not contiguous")
                return derive_cabac_macroblock_neighbor_coded_state(entry, direction)

            left_state = coded_neighbor_state(
                neighbor_indices.left,
                macroblock_address - 1,
                "left",
            )
            top_state = coded_neighbor_state(
                neighbor_indices.top,
                macroblock_address - width_in_mbs,
                "top",
            )
            slice_qpy = (
                26 + picture_set.pic_init_qp_minus26 + header.slice_qp_delta
            )
            previous_qpy, previous_qp_delta = slice_qpy, 0
            if history:
                previous_macroblock = history[-1].macroblock
                if isinstance(previous_macroblock, CABACPBMacroblockLayerResult):
                    previous_qpy = previous_macroblock.qpy
                    if not previous_macroblock.skipped:
                        previous_macroblock = previous_macroblock.macroblock
                        if isinstance(
                            previous_macroblock,
                            (CABACIIntraMacroblockResult, CABACInterMacroblockResult),
                        ):
                            previous_qp_delta = previous_macroblock.qp_delta
                elif isinstance(
                    previous_macroblock,
                    (CABACIIntraMacroblockResult, CABACInterMacroblockResult),
                ):
                    previous_qpy = previous_macroblock.qpy
                    previous_qp_delta = previous_macroblock.qp_delta

            inter_input = CABACInterMacroblockInput(
                slice_type=header.slice_type,
                macroblock_x=macroblock_address % width_in_mbs,
                macroblock_y=macroblock_address // width_in_mbs,
                left_available=neighbor_states[0].left_available,
                top_available=neighbor_states[0].top_available,
                left_b_skip_or_direct=neighbor_states[0].left_b_skip_or_direct,
                top_b_skip_or_direct=neighbor_states[0].top_b_skip_or_direct,
                max_reference_index_minus1=(active_l0, active_l1),
                reference_lists=(references0, references1),
                partition_neighbors=tuple(state.partition_neighbors for state in neighbor_states),
            )
            residual_input = CABACInterMacroblockResidualInput(
                previous_qpy=previous_qpy,
                previous_qp_delta=previous_qp_delta,
                left_luma_cbp=(
                    left_state.coded_block_pattern_luma
                    if left_state and not left_state.is_ipcm else 15
                ),
                top_luma_cbp=(
                    top_state.coded_block_pattern_luma
                    if top_state and not top_state.is_ipcm else 15
                ),
                left_chroma_cbp=(
                    2 if left_state and left_state.is_ipcm
                    else left_state.coded_block_pattern_chroma if left_state else 0
                ),
                top_chroma_cbp=(
                    2 if top_state and top_state.is_ipcm
                    else top_state.coded_block_pattern_chroma if top_state else 0
                ),
                transform_8x8_mode_enabled=picture_set.transform_8x8_mode,
                direct_8x8_inference_flag=sequence_set.direct_8x8_inference,
                left_has_8x8_transform=bool(left_state and left_state.transform_size_8x8),
                top_has_8x8_transform=bool(top_state and top_state.transform_size_8x8),
                luma_4x4_left_edge=(
                    left_state.luma_4x4_edge
                    if left_state is not None
                    else CABACIntra4x4EdgeState()
                ),
                luma_4x4_top_edge=(
                    top_state.luma_4x4_edge
                    if top_state is not None
                    else CABACIntra4x4EdgeState()
                ),
                chroma_left_edges=(
                    left_state.chroma_edges
                    if left_state is not None
                    else (CABACChroma420EdgeState(),) * 2
                ),
                chroma_top_edges=(
                    top_state.chroma_edges
                    if top_state is not None
                    else (CABACChroma420EdgeState(),) * 2
                ),
                chroma_qp_index_offsets=(
                    picture_set.chroma_qp_index_offset,
                    picture_set.second_chroma_qp_index_offset,
                ),
            )
            b_skip_input = None
            if header.slice_type % 5 == 1:
                if not header.direct_spatial_mv_pred:
                    raise DecoderError(
                        "temporal B direct requires co-located motion metadata in the reference buffer"
                    )
                direct_neighbors = derive_cabac_inter_macroblock_neighbor_state_from_history(
                    macroblock_address,
                    width_in_mbs,
                    header.first_macroblock_in_slice,
                    history,
                    header.slice_type,
                    0,
                    0,
                    16,
                    16,
                )
                colocated_picture = list1[0]
                colocated_facts = self.reference_picture_buffer.colocated_motion_facts(
                    colocated_picture.identifier, macroblock_address
                )
                if colocated_facts is None:
                    raise DecoderError(
                        f"co-located motion facts are unavailable for reference picture "
                        f"{colocated_picture.identifier} macroblock {macroblock_address}"
                    )
                colocated_top_left = colocated_facts[0]
                list0_neighbors = direct_neighbors.partition_neighbors.list0
                list1_neighbors = direct_neighbors.partition_neighbors.list1
                b_skip_input = CABACBDirectMacroblockInput(
                    slice_type=header.slice_type,
                    macroblock_x=inter_input.macroblock_x,
                    macroblock_y=inter_input.macroblock_y,
                    direct_spatial=True,
                    left_available=direct_neighbors.left_available,
                    left_skipped=direct_neighbors.left_skipped,
                    top_available=direct_neighbors.top_available,
                    top_skipped=direct_neighbors.top_skipped,
                    list0_references=references0,
                    list1_references=references1,
                    left_l0_candidate=list0_neighbors.left_candidate,
                    top_l0_candidate=list0_neighbors.top_candidate,
                    top_right_l0_candidate=list0_neighbors.top_right_candidate,
                    top_left_l0_candidate=list0_neighbors.top_left_candidate,
                    left_l1_candidate=list1_neighbors.left_candidate,
                    top_l1_candidate=list1_neighbors.top_candidate,
                    top_right_l1_candidate=list1_neighbors.top_right_candidate,
                    top_left_l1_candidate=list1_neighbors.top_left_candidate,
                    colocated_reference_index=colocated_top_left.reference_index,
                    colocated_motion_vector=(
                        colocated_top_left.vector.x,
                        colocated_top_left.vector.y,
                    ),
                    colocated_motion_field=colocated_facts,
                    current_picture_order_count=current_poc,
                )
            return CABACPBMacroblockLayerInput(
                inter_input=inter_input,
                residual_input=residual_input,
                left_skipped=neighbor_states[0].left_skipped,
                top_skipped=neighbor_states[0].top_skipped,
                b_skip_input=b_skip_input,
                intra_input=self.intra_macroblock_input_from_history(
                    dependency_slice,
                    macroblock_address,
                    history,
                ),
            )
        except (CABACError, PPSParseError, SPSParseError, ValueError) as error:
            raise DecoderError(f"sample {dependency_slice.sample_index}: {error}") from error

    def _reference_picture_lists(self, dependency_slice, picture_set, sequence_set, current_poc):
        from .reconstruction import (
            apply_reference_list_modifications,
            build_b_reference_lists,
            build_p_reference_list,
        )

        header = dependency_slice.header
        references = self.reference_picture_buffer.references()
        max_frame_num = 1 << (sequence_set.log2_max_frame_num_minus4 + 4)
        if header.slice_type % 5 == 0:
            list0 = build_p_reference_list(references, header.frame_num, max_frame_num)
            list1 = []
        else:
            list0, list1 = build_b_reference_lists(references, current_poc)
        if header.ref_pic_list_reordering_l0:
            list0 = apply_reference_list_modifications(
                list0, references, header.frame_num, max_frame_num,
                header.ref_pic_list_reordering_l0,
            )
        if header.slice_type % 5 == 1 and header.ref_pic_list_reordering_l1:
            list1 = apply_reference_list_modifications(
                list1, references, header.frame_num, max_frame_num,
                header.ref_pic_list_reordering_l1,
            )
        active_l0 = (
            header.num_ref_idx_l0_active_minus1
            if header.num_ref_idx_active_override
            else picture_set.num_ref_idx_l0_default_active_minus1
        )
        active_l1 = (
            header.num_ref_idx_l1_active_minus1
            if header.num_ref_idx_active_override
            else picture_set.num_ref_idx_l1_default_active_minus1
        )
        return list0[: active_l0 + 1], list1[: active_l1 + 1] if header.slice_type % 5 == 1 else []

    def decode_frame(self, index: int) -> Yuv420Frame:
        if self.sample_reader is None:
            raise DecoderError("H.264 decoder requires a sample reader")
        if self.unsupported_feature:
            raise DecoderError("unsupported H.264 profile, chroma format, bit depth, or interlace mode")

        sample_count = getattr(self.sample_reader, "sample_count", None)
        if sample_count is None:
            raise DecoderError("H.264 decoder requires a sample reader")
        if not isinstance(index, int) or isinstance(index, bool) or index < 0 or index >= sample_count:
            raise DecoderError(f"frame index {index} is out of range for {sample_count} sample(s)")

        cached_frame = self._decoded_frames.get(index)
        if cached_frame is not None:
            return cached_frame

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

        deadline = (
            None
            if self.time_limit_seconds is None
            else time.monotonic() + self.time_limit_seconds
        )
        try:
            target_sample_index = self.sample_reader.presentation_sample_index(index)
            picture_groups = self.dependency_slice_pictures(index)
        except (IndexError, ValueError) as error:
            raise DecoderError(f"cannot resolve requested frame {index}: {error}") from error
        if not picture_groups:
            raise DecoderError("frame dependency window contains no coded pictures")

        gop_sync_sample_index = picture_groups[0][0].sample_index
        if (
            self._gop_sync_sample_index != gop_sync_sample_index
            or self._last_decoded_sample_index is None
        ):
            self.reference_picture_buffer.clear()
            self._poc_type0_state = None
            self._poc_type12_state = None
            self._poc_cache = None
            self._maximum_long_term_frame_idx = None
            self._gop_sync_sample_index = gop_sync_sample_index
            self._last_decoded_sample_index = None

        target_frame = None
        for picture in picture_groups:
            first_slice = picture[0]
            header = first_slice.header
            configuration = self.sample_reader.configuration
            sequence_sets_by_id, picture_sets_by_id = self._parsed_parameter_sets_by_id()
            picture_set = picture_sets_by_id.get(header.picture_parameter_set_id)
            if picture_set is None:
                raise DecoderError("dependency picture references an unavailable PPS")
            sequence_set = sequence_sets_by_id.get(picture_set.sequence_parameter_set_id)
            if sequence_set is None:
                raise DecoderError("dependency picture references an unavailable SPS")
            sample_index = first_slice.sample_index
            display_index = self.sample_reader.presentation_index_for_sample_index(sample_index)

            if (
                self._last_decoded_sample_index is not None
                and sample_index <= self._last_decoded_sample_index
            ):
                if sample_index == target_sample_index and display_index == index:
                    target_frame = self._decoded_frames.get(display_index)
                    if target_frame is None:
                        cached = self.reference_picture_buffer.get(display_index)
                        if cached is not None:
                            target_frame = cached.frame
                    if target_frame is not None:
                        break
                continue

            self._report_frame_progress(display_index, sample_index, 0)
            if header.idr:
                self.reference_picture_buffer.clear()
                self._maximum_long_term_frame_idx = 0 if header.long_term_reference else None
            try:
                picture_order_count = self._picture_order_count(header, sequence_set)
                builder = Yuv420FrameBuilder(
                    sequence_set.coded_width // 16,
                    sequence_set.coded_height // 16,
                )
                macroblock_count = (
                    sequence_set.coded_width // 16
                ) * (sequence_set.coded_height // 16)
                decoded_macroblocks = 0
                next_progress_percent = 10

                def check_frame_deadline() -> None:
                    if deadline is not None and time.monotonic() >= deadline:
                        raise DecoderTimeLimitExceeded(
                            f"frame {index} exceeded its {self.time_limit_seconds:g}s decode time limit"
                        )

                def report_macroblock_progress(_address: int) -> None:
                    nonlocal decoded_macroblocks, next_progress_percent
                    check_frame_deadline()
                    decoded_macroblocks += 1
                    while (
                        next_progress_percent < 100
                        and decoded_macroblocks * 100 >= macroblock_count * next_progress_percent
                    ):
                        self._report_frame_progress(
                            display_index, sample_index, next_progress_percent
                        )
                        next_progress_percent += 10

                picture_history = []
                deblocking = [None] * (
                    (sequence_set.coded_width // 16) * (sequence_set.coded_height // 16)
                )
                from .reconstruction import PictureDeblockingMacroblock, deblock_yuv420_picture
                from .cabac import _LUMA4X4_BLOCK_SCAN_TO_RASTER

                for slice_id, dependency_slice in enumerate(picture):
                    slice_references = self._reference_picture_lists(
                        dependency_slice,
                        picture_set,
                        sequence_set,
                        picture_order_count,
                    ) if header.slice_type % 5 in (0, 1) else ([], [])
                    slice_results = self.decode_dependency_slice_data(
                        dependency_slice,
                        builder,
                        lambda address, history, current_slice=dependency_slice: self.macroblock_input_from_history(
                            current_slice, address, history
                        ),
                        report_macroblock_progress
                        if self.debug_progress or deadline is not None
                        else None,
                        check_frame_deadline if deadline is not None else None,
                    )
                    picture_history.extend(slice_results)
                    for entry in slice_results:
                        deblocking[entry.address] = _picture_deblocking_macroblock(
                            entry,
                            dependency_slice,
                            slice_references,
                            picture_set,
                            slice_id,
                        )
                if any(metadata is None for metadata in deblocking):
                    raise DecoderError("picture deblocking metadata is incomplete")
                coded_frame = deblock_yuv420_picture(
                    builder.finish(),
                    deblocking,
                    timeout_check=check_frame_deadline if deadline is not None else None,
                    threads=self.threads,
                )
                planes = []
                for plane, divisor in ((coded_frame.y, 1), (coded_frame.u, 2), (coded_frame.v, 2)):
                    stride = coded_frame.width // divisor
                    left = sequence_set.frame_crop_left // divisor
                    top = sequence_set.frame_crop_top // divisor
                    width = sequence_set.width // divisor
                    height = sequence_set.height // divisor
                    planes.append(b"".join(plane[(top + row) * stride + left:(top + row) * stride + left + width] for row in range(height)))
                frame = Yuv420Frame(
                    sequence_set.width, sequence_set.height, sequence_set.width,
                    sequence_set.width // 2, sequence_set.width // 2, *planes,
                )
                self._report_frame_progress(display_index, sample_index, 100)
            except (ValueError, IndexError) as error:
                raise DecoderError(
                    f"sample {first_slice.sample_index}: picture reconstruction failed: {error}"
                ) from error

            self._decoded_frames[display_index] = frame
            if header.nal_ref_idc:
                if header.idr:
                    stored_frame_num = header.frame_num
                    long_term_frame_idx = 0 if header.long_term_reference else None
                    reset_poc_state = False
                elif header.adaptive_ref_pic_marking:
                    maximum_frame_num = 1 << (sequence_set.log2_max_frame_num_minus4 + 4)
                    try:
                        (
                            long_term_frame_idx,
                            reset_frame_num,
                            self._maximum_long_term_frame_idx,
                        ) = _apply_adaptive_reference_marking(
                            self.reference_picture_buffer,
                            header.frame_num,
                            maximum_frame_num,
                            header.memory_management_operations,
                            self._maximum_long_term_frame_idx,
                        )
                    except ValueError as error:
                        raise DecoderError(str(error)) from error
                    stored_frame_num = 0 if reset_frame_num else header.frame_num
                    reset_poc_state = reset_frame_num
                else:
                    stored_frame_num = header.frame_num
                    long_term_frame_idx = None
                    reset_poc_state = False
                stored_picture_order_count = 0 if reset_poc_state else picture_order_count
                references = self.reference_picture_buffer.references()
                maximum_references = max(sequence_set.max_num_ref_frames, 1)
                if not header.idr and not header.adaptive_ref_pic_marking and len(references) >= maximum_references:
                    max_frame_num = 1 << (sequence_set.log2_max_frame_num_minus4 + 4)
                    short_term = [
                        reference for reference in references
                        if reference.long_term_frame_idx is None
                    ]
                    if not short_term:
                        raise DecoderError("sliding-window marking found no short-term reference to evict")
                    oldest = min(
                        short_term,
                        key=lambda reference: (
                            reference.frame_num - max_frame_num
                            if reference.frame_num > header.frame_num
                            else reference.frame_num
                        ),
                    )
                    self.reference_picture_buffer.remove(oldest.identifier)
                self.store_reference_picture(
                    ReferencePicture(
                        display_index,
                        frame_num=stored_frame_num,
                        picture_order_cnt=stored_picture_order_count,
                        long_term_frame_idx=long_term_frame_idx,
                    ),
                    coded_frame,
                    tuple(picture_history),
                )
                if reset_poc_state:
                    if sequence_set.pic_order_cnt_type == 0:
                        from .reconstruction import POCType0State

                        state = self._poc_type0_state
                        if state is None:
                            raise DecoderError("MMCO 5 picture order count state is unavailable")
                        top_order_count = (
                            state.previous_pic_order_cnt_msb + header.pic_order_cnt_lsb
                        )
                        bottom_order_count = top_order_count + header.delta_pic_order_bottom
                        self._poc_type0_state = POCType0State(
                            0, top_order_count - min(top_order_count, bottom_order_count)
                        )
                    else:
                        self._poc_type0_state = None
                    self._poc_type12_state = None
                    self._poc_cache = None

            self._last_decoded_sample_index = sample_index
            if self.checkpoint_path is not None:
                try:
                    self.save_checkpoint(self.checkpoint_path)
                except Exception:
                    pass

            if sample_index == target_sample_index and display_index == index:
                target_frame = frame
                break

        if target_frame is None:
            raise DecoderError(
                f"requested presentation frame {index} is absent from its coded dependency window"
            )
        return target_frame

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


__all__ = ["DecoderError", "DecoderReferenceMismatch", "DependencySlice", "H264Decoder"]
