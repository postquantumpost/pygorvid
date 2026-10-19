"""CABAC arithmetic-state initialization and renormalization."""

from collections.abc import Callable, Sequence
from dataclasses import dataclass
from typing import Literal

from .bitreader import BitReader, BitstreamError
from .h264_cabac_init import CONTEXT_INIT_TABLE
from .h264_cavlc_syntax import CAVLCSyntax
from .h264_deferred import (
    PLACEHOLDER_SAMPLES as _PLACEHOLDER_SAMPLES,
    InterResidual as _InterResidual,
    IntraNeighborsPending as _IntraNeighborsPending,
    SliceReconstructionDriver as _SliceReconstructionDriver,
    parallel_reconstruction_available as _parallel_reconstruction_available,
    reconstruct_or_defer as _reconstruct_or_defer,
    require_intra_neighbors as _require_intra_neighbors,
)


class CABACError(ValueError):
    """Raised when CABAC arithmetic state or initialization bits are invalid."""


class CABACTerminatedError(CABACError):
    """Raised when decoding is attempted after a terminate bin was true."""


@dataclass(frozen=True)
class CABACInterNeighbor:
    available: bool = False
    skip: bool = False
    intra: bool = False
    prediction_mode_matches: bool = False
    reference_index: int = 0
    motion_vector_difference: tuple[int, int] = (0, 0)
    is_field: bool = False


@dataclass(frozen=True)
class CABACPSkipMacroblockInput:
    slice_type: int = 0
    macroblock_x: int = 0
    macroblock_y: int = 0
    left_available: bool = False
    left_skipped: bool = False
    top_available: bool = False
    top_skipped: bool = False
    left_candidate: object | None = None
    top_candidate: object | None = None
    top_right_candidate: object | None = None
    top_left_candidate: object | None = None
    reference_frame: object | None = None


@dataclass(frozen=True)
class CABACPSkipMacroblockResult:
    skipped: bool
    reference_index: int
    motion_vector: tuple[int, int]
    luma: tuple[int, ...]
    cb: tuple[int, ...]
    cr: tuple[int, ...]


@dataclass(frozen=True)
class CABACBReferencePicture:
    frame: object
    picture_order_count: int
    long_term: bool = False


@dataclass(frozen=True)
class CABACBDirectMacroblockInput:
    slice_type: int = 1
    macroblock_x: int = 0
    macroblock_y: int = 0
    direct_spatial: bool = True
    left_available: bool = False
    left_skipped: bool = False
    top_available: bool = False
    top_skipped: bool = False
    list0_references: tuple[CABACBReferencePicture, ...] = ()
    list1_references: tuple[CABACBReferencePicture, ...] = ()
    left_l0_candidate: object | None = None
    top_l0_candidate: object | None = None
    top_right_l0_candidate: object | None = None
    top_left_l0_candidate: object | None = None
    left_l1_candidate: object | None = None
    top_l1_candidate: object | None = None
    top_right_l1_candidate: object | None = None
    top_left_l1_candidate: object | None = None
    colocated_reference_index: int = -1
    colocated_reference_picture_order_count: int | None = None
    colocated_motion_vector: tuple[int, int] = (0, 0)
    colocated_motion_field: tuple[object, ...] = ()
    current_picture_order_count: int = 0


@dataclass(frozen=True)
class CABACBDirectMacroblockResult:
    skipped: bool
    reference_indices: tuple[int, int]
    motion_vectors: tuple[tuple[int, int], tuple[int, int]]
    prediction_flags: tuple[bool, bool]
    luma: tuple[int, ...]
    cb: tuple[int, ...]
    cr: tuple[int, ...]


@dataclass(frozen=True)
class CABACInterListNeighbors:
    left: CABACInterNeighbor = CABACInterNeighbor()
    top: CABACInterNeighbor = CABACInterNeighbor()
    left_candidate: object | None = None
    top_candidate: object | None = None
    top_right_candidate: object | None = None
    top_left_candidate: object | None = None


@dataclass(frozen=True)
class CABACInterPartitionNeighbors:
    list0: CABACInterListNeighbors = CABACInterListNeighbors()
    list1: CABACInterListNeighbors = CABACInterListNeighbors()
    direct_prediction: CABACBDirectMacroblockResult | None = None


def _aligned_partition_top_candidate(
    partition_neighbors: Sequence[CABACInterPartitionNeighbors],
    list_index: int,
    sample_x: int,
    sample_y: int,
) -> tuple[bool, object | None]:
    if len(partition_neighbors) != 16 or sample_y >= 0 or not 0 <= sample_x < 16:
        return False, None
    cell = partition_neighbors[sample_x // 4]
    list_neighbors = cell.list0 if list_index == 0 else cell.list1
    return True, list_neighbors.top_candidate


@dataclass(frozen=True)
class CABACInterMacroblockInput:
    slice_type: int
    macroblock_x: int
    macroblock_y: int
    left_available: bool
    top_available: bool
    left_b_skip_or_direct: bool = False
    top_b_skip_or_direct: bool = False
    max_reference_index_minus1: tuple[int, int] = (0, 0)
    reference_lists: tuple[
        tuple[CABACBReferencePicture, ...], tuple[CABACBReferencePicture, ...]
    ] = ((), ())
    partition_neighbors: tuple[CABACInterPartitionNeighbors, ...] = ()


@dataclass(frozen=True)
class CABACInterMacroblockPartition:
    x: int
    y: int
    width: int
    height: int
    prediction_flags: tuple[bool, bool]
    reference_indices: tuple[int, int]
    motion_vectors: tuple[tuple[int, int], tuple[int, int]]
    motion_vector_differences: tuple[tuple[int, int], tuple[int, int]] = ((0, 0), (0, 0))


@dataclass(frozen=True)
class CABACInterMacroblockResult:
    macroblock_type: int
    sub_macroblock_types: tuple[int, ...]
    partitions: tuple[CABACInterMacroblockPartition, ...]
    luma: tuple[int, ...]
    cb: tuple[int, ...]
    cr: tuple[int, ...]
    coded_block_pattern_luma: int = 0
    coded_block_pattern_chroma: int = 0
    transform_size_8x8: bool = False
    qp_delta: int = 0
    qpy: int = 26
    luma_4x4_coded_block_flags: tuple[bool, ...] = ()
    chroma_dc_coded: tuple[bool, bool] = (False, False)
    chroma_ac_coded_flags: tuple[tuple[bool, ...], tuple[bool, ...]] = ((False,) * 4, (False,) * 4)
    luma_8x8_nonzero: tuple[bool, ...] = (False,) * 4


@dataclass(frozen=True)
class CABACIntra4x4EdgeState:
    available: bool = False
    is_ipcm: bool = False
    transform_block_available: tuple[bool, bool, bool, bool] = (False,) * 4
    transform_block_coded: tuple[bool, bool, bool, bool] = (False,) * 4


@dataclass(frozen=True)
class Intra4x4LumaMacroblockResult:
    modes: tuple[int, ...]
    coded_block_flags: tuple[bool, ...]
    residuals: tuple[tuple[int, ...], ...]
    samples: tuple[int, ...]


@dataclass(frozen=True)
class Intra8x8LumaMacroblockResult:
    transform_size_8x8: bool
    modes: tuple[int, ...]
    residuals: tuple[tuple[int, ...], ...]
    samples: tuple[int, ...]
    nonzero: tuple[bool, ...] = (False,) * 4


@dataclass(frozen=True)
class CABACIntra16x16EdgeState:
    available: bool = False
    is_ipcm: bool = False
    dc_transform_block_available: bool = False
    dc_transform_block_coded: bool = False
    ac_transform_block_available: tuple[bool, bool, bool, bool] = (False,) * 4
    ac_transform_block_coded: tuple[bool, bool, bool, bool] = (False,) * 4


@dataclass(frozen=True)
class Intra16x16LumaMacroblockResult:
    prediction_mode: int
    coded_block_pattern_luma: int
    dc_coded: bool
    dc_levels: tuple[int, ...]
    ac_coded_block_flags: tuple[bool, ...]
    residual: tuple[int, ...]
    samples: tuple[int, ...]


@dataclass(frozen=True)
class CABACChroma420References:
    top_available: bool = False
    left_available: bool = False
    top_left_available: bool = False
    top: tuple[int, ...] = (0,) * 8
    left: tuple[int, ...] = (0,) * 8
    top_left: int = 0


@dataclass(frozen=True)
class CABACChroma420EdgeState:
    available: bool = False
    is_ipcm: bool = False
    dc_block_available: bool = False
    dc_block_coded: bool = False
    ac_block_available: tuple[bool, bool] = (False, False)
    ac_block_coded: tuple[bool, bool] = (False, False)


@dataclass(frozen=True)
class CABACInterMacroblockResidualInput:
    previous_qpy: int = 26
    previous_qp_delta: int = 0
    left_luma_cbp: int = 0
    top_luma_cbp: int = 0
    left_chroma_cbp: int = 0
    top_chroma_cbp: int = 0
    transform_8x8_mode_enabled: bool = False
    direct_8x8_inference_flag: bool = True
    left_has_8x8_transform: bool = False
    top_has_8x8_transform: bool = False
    luma_4x4_top_edge: CABACIntra4x4EdgeState = CABACIntra4x4EdgeState()
    luma_4x4_left_edge: CABACIntra4x4EdgeState = CABACIntra4x4EdgeState()
    chroma_left_edges: tuple[CABACChroma420EdgeState, CABACChroma420EdgeState] = (
        CABACChroma420EdgeState(), CABACChroma420EdgeState()
    )
    chroma_top_edges: tuple[CABACChroma420EdgeState, CABACChroma420EdgeState] = (
        CABACChroma420EdgeState(), CABACChroma420EdgeState()
    )
    luma_4x4_scaling_list: tuple[int, ...] = (16,) * 16
    luma_8x8_scaling_list: tuple[int, ...] = (16,) * 64
    chroma_scaling_lists: tuple[Sequence[int], Sequence[int]] = ((16,) * 16, (16,) * 16)
    chroma_qp_index_offsets: tuple[int, int] = (0, 0)


@dataclass(frozen=True)
class CABACPBMacroblockLayerInput:
    inter_input: CABACInterMacroblockInput
    residual_input: CABACInterMacroblockResidualInput
    left_skipped: bool = False
    top_skipped: bool = False
    b_skip_input: CABACBDirectMacroblockInput | None = None
    intra_input: "CABACIIntraMacroblockInput | None" = None


@dataclass(frozen=True)
class CABACPBMacroblockLayerResult:
    skipped: bool
    macroblock: "CABACPSkipMacroblockResult | CABACBDirectMacroblockResult | CABACInterMacroblockResult | CABACIIntraMacroblockResult"
    qpy: int = 26


@dataclass(frozen=True)
class IntraChroma420MacroblockResult:
    prediction_mode: int
    qpc: tuple[int, int]
    dc_coded: tuple[bool, bool]
    ac_coded_block_flags: tuple[tuple[bool, ...], tuple[bool, ...]]
    cb: tuple[int, ...]
    cr: tuple[int, ...]
    cb_residual: tuple[int, ...]
    cr_residual: tuple[int, ...]


@dataclass(frozen=True)
class CABACIIntraMacroblockInput:
    slice_type: int = 2
    left_available: bool = False
    left_intra16_or_pcm: bool = False
    top_available: bool = False
    top_intra16_or_pcm: bool = False
    previous_qpy: int = 26
    previous_qp_delta: int = 0
    left_luma_cbp: int = 0
    top_luma_cbp: int = 0
    left_chroma_cbp: int = 0
    top_chroma_cbp: int = 0
    transform_8x8_mode_enabled: bool = False
    left_has_8x8_transform: bool = False
    top_has_8x8_transform: bool = False
    top_modes_4x4: tuple[int, ...] = (0,) * 4
    left_modes_4x4: tuple[int, ...] = (0,) * 4
    top_modes_8x8: tuple[int, ...] = (0,) * 2
    left_modes_8x8: tuple[int, ...] = (0,) * 2
    top_mode_available: bool = False
    left_mode_available: bool = False
    luma_4x4_top_edge: CABACIntra4x4EdgeState = CABACIntra4x4EdgeState()
    luma_4x4_left_edge: CABACIntra4x4EdgeState = CABACIntra4x4EdgeState()
    luma_16x16_top_edge: CABACIntra16x16EdgeState = CABACIntra16x16EdgeState()
    luma_16x16_left_edge: CABACIntra16x16EdgeState = CABACIntra16x16EdgeState()
    luma_4x4_blocks: tuple[object, ...] = ()
    luma_8x8_blocks: tuple[object, ...] = ()
    intra16x16_top: tuple[int, ...] = (0,) * 16
    intra16x16_left: tuple[int, ...] = (0,) * 16
    intra16x16_top_available: bool = False
    intra16x16_left_available: bool = False
    intra16x16_top_left: int = 0
    intra16x16_top_left_available: bool = False
    chroma_left_mode_nonzero: bool = False
    chroma_top_mode_nonzero: bool = False
    chroma_references: tuple[CABACChroma420References, CABACChroma420References] = (
        CABACChroma420References(), CABACChroma420References()
    )
    chroma_left_edges: tuple[CABACChroma420EdgeState, CABACChroma420EdgeState] = (
        CABACChroma420EdgeState(), CABACChroma420EdgeState()
    )
    chroma_top_edges: tuple[CABACChroma420EdgeState, CABACChroma420EdgeState] = (
        CABACChroma420EdgeState(), CABACChroma420EdgeState()
    )
    luma_4x4_scaling_list: tuple[int, ...] = (16,) * 16
    luma_8x8_scaling_list: tuple[int, ...] = (16,) * 64
    chroma_scaling_lists: tuple[Sequence[int], Sequence[int]] = ((16,) * 16, (16,) * 16)
    chroma_qp_index_offsets: tuple[int, int] = (0, 0)


@dataclass(frozen=True)
class CABACIIntraMacroblockResult:
    macroblock_type: int
    coded_block_pattern_luma: int
    coded_block_pattern_chroma: int
    transform_size_8x8: bool
    intra16x16_luma_mode: int
    chroma_prediction_mode: int
    luma_4x4_modes: tuple[int, ...]
    luma_8x8_modes: tuple[int, ...]
    qp_delta: int
    qpy: int
    luma: tuple[int, ...]
    cb: tuple[int, ...]
    cr: tuple[int, ...]
    luma_4x4_coded_block_flags: tuple[bool, ...]
    chroma_dc_coded: tuple[bool, bool]
    chroma_ac_coded_flags: tuple[tuple[bool, ...], tuple[bool, ...]]
    luma_dc_coded: bool = False
    luma_ac_coded_block_flags: tuple[bool, ...] = ()
    luma_8x8_coded_block_flags: tuple[bool, ...] = (False,) * 4
    luma_8x8_nonzero: tuple[bool, ...] = (False,) * 4


@dataclass(frozen=True)
class CABACSliceMacroblockResult:
    address: int
    macroblock: CABACIIntraMacroblockResult | CABACPBMacroblockLayerResult


@dataclass(frozen=True)
class CABACSliceNeighborIndices:
    left: int | None
    top: int | None
    top_right: int | None
    top_left: int | None


def derive_cabac_slice_neighbor_indices(
    macroblock_address: int,
    picture_width_in_mbs: int,
    first_history_address: int,
    history_count: int,
) -> CABACSliceNeighborIndices:
    """Map raster neighbors to indices in a contiguous, already-decoded slice history."""
    values = (macroblock_address, picture_width_in_mbs, first_history_address, history_count)
    if any(not isinstance(value, int) or isinstance(value, bool) for value in values):
        raise CABACError("CABAC slice neighbor coordinates must be integers")
    if (
        macroblock_address < 0
        or picture_width_in_mbs <= 0
        or first_history_address < 0
        or history_count < 0
        or first_history_address + history_count > macroblock_address
    ):
        raise CABACError("CABAC slice neighbor history range is invalid")

    column = macroblock_address % picture_width_in_mbs
    history_end = first_history_address + history_count

    def history_index(neighbor_address: int | None) -> int | None:
        if neighbor_address is None or not first_history_address <= neighbor_address < history_end:
            return None
        return neighbor_address - first_history_address

    return CABACSliceNeighborIndices(
        left=history_index(macroblock_address - 1 if column else None),
        top=history_index(
            macroblock_address - picture_width_in_mbs
            if macroblock_address >= picture_width_in_mbs
            else None
        ),
        top_right=history_index(
            macroblock_address - picture_width_in_mbs + 1
            if macroblock_address >= picture_width_in_mbs and column + 1 < picture_width_in_mbs
            else None
        ),
        top_left=history_index(
            macroblock_address - picture_width_in_mbs - 1
            if macroblock_address >= picture_width_in_mbs and column
            else None
        ),
    )


@dataclass(frozen=True)
class CABACMacroblockEdgeSamples:
    luma: tuple[int, ...]
    cb: tuple[int, ...]
    cr: tuple[int, ...]


def derive_cabac_macroblock_edge_samples(
    neighbor: CABACSliceMacroblockResult,
    neighbor_position: Literal["left", "top"],
) -> CABACMacroblockEdgeSamples:
    """Gather the reconstructed edge facing the current macroblock from slice history."""
    if not isinstance(neighbor, CABACSliceMacroblockResult):
        raise CABACError("macroblock neighbor result is invalid")
    if neighbor_position not in ("left", "top"):
        raise CABACError("macroblock neighbor position must be left or top")

    decoded = neighbor.macroblock
    if isinstance(decoded, CABACPBMacroblockLayerResult):
        decoded = decoded.macroblock
    if not isinstance(
        decoded,
        (
            CABACIIntraMacroblockResult,
            CABACPSkipMacroblockResult,
            CABACBDirectMacroblockResult,
            CABACInterMacroblockResult,
        ),
    ):
        raise CABACError("macroblock neighbor result does not contain reconstructed planes")
    if len(decoded.luma) != 256 or len(decoded.cb) != 64 or len(decoded.cr) != 64:
        raise CABACError("macroblock neighbor planes have invalid dimensions")

    if neighbor_position == "left":
        luma = tuple(decoded.luma[row * 16 + 15] for row in range(16))
        cb = tuple(decoded.cb[row * 8 + 7] for row in range(8))
        cr = tuple(decoded.cr[row * 8 + 7] for row in range(8))
    else:
        luma = tuple(decoded.luma[15 * 16 + column] for column in range(16))
        cb = tuple(decoded.cb[7 * 8 + column] for column in range(8))
        cr = tuple(decoded.cr[7 * 8 + column] for column in range(8))
    return CABACMacroblockEdgeSamples(luma, cb, cr)


@dataclass(frozen=True)
class CABACMacroblockNeighborCodedState:
    coded_block_pattern_luma: int
    coded_block_pattern_chroma: int
    transform_size_8x8: bool
    is_intra16x16: bool
    is_ipcm: bool
    is_skipped: bool
    chroma_mode_nonzero: bool
    luma_4x4_edge: CABACIntra4x4EdgeState
    luma_16x16_edge: CABACIntra16x16EdgeState
    chroma_edges: tuple[CABACChroma420EdgeState, CABACChroma420EdgeState]
    is_intra_nxn: bool
    luma_4x4_modes: tuple[int, int, int, int]
    luma_8x8_modes: tuple[int, int]


def derive_cabac_macroblock_neighbor_coded_state(
    neighbor: CABACSliceMacroblockResult,
    neighbor_position: Literal["left", "top"],
) -> CABACMacroblockNeighborCodedState:
    """Derive residual-context facts at a left or top macroblock boundary."""
    if not isinstance(neighbor, CABACSliceMacroblockResult):
        raise CABACError("macroblock neighbor result is invalid")
    if neighbor_position not in ("left", "top"):
        raise CABACError("macroblock neighbor position must be left or top")

    decoded = neighbor.macroblock
    is_skipped = False
    if isinstance(decoded, CABACPBMacroblockLayerResult):
        is_skipped = decoded.skipped
        decoded = decoded.macroblock

    is_intra = isinstance(decoded, CABACIIntraMacroblockResult)
    is_ipcm = is_intra and decoded.macroblock_type == 25
    is_intra16x16 = is_intra and decoded.macroblock_type not in (0, 25)
    is_intra_nxn = is_intra and decoded.macroblock_type == 0
    chroma_mode_nonzero = is_intra and decoded.chroma_prediction_mode != 0
    luma_4x4_modes = [2] * 4
    luma_8x8_modes = [2] * 2
    if isinstance(decoded, CABACIIntraMacroblockResult) and not is_skipped:
        cbp_luma = decoded.coded_block_pattern_luma
        cbp_chroma = decoded.coded_block_pattern_chroma
        transform_8x8 = decoded.transform_size_8x8
        luma_4x4_flags = decoded.luma_4x4_coded_block_flags
        luma_8x8_flags = decoded.luma_8x8_coded_block_flags
        luma_ac_flags = decoded.luma_ac_coded_block_flags
        luma_dc_coded = decoded.luma_dc_coded
        chroma_dc_flags = decoded.chroma_dc_coded
        chroma_ac_flags = decoded.chroma_ac_coded_flags
        if is_intra_nxn:
            if transform_8x8:
                for edge_index, group in enumerate((1, 3) if neighbor_position == "left" else (2, 3)):
                    if group < len(decoded.luma_8x8_modes):
                        luma_8x8_modes[edge_index] = decoded.luma_8x8_modes[group]
                for edge_coordinate in range(4):
                    raster_index = (
                        edge_coordinate * 4 + 3
                        if neighbor_position == "left"
                        else 12 + edge_coordinate
                    )
                    group = (raster_index // 8) * 2 + (raster_index % 4) // 2
                    if group < len(decoded.luma_8x8_modes):
                        luma_4x4_modes[edge_coordinate] = decoded.luma_8x8_modes[group]
            else:
                for edge_coordinate in range(4):
                    raster_index = (
                        edge_coordinate * 4 + 3
                        if neighbor_position == "left"
                        else 12 + edge_coordinate
                    )
                    scan_index = _LUMA4X4_BLOCK_SCAN_TO_RASTER.index(raster_index)
                    if scan_index < len(decoded.luma_4x4_modes):
                        luma_4x4_modes[edge_coordinate] = decoded.luma_4x4_modes[scan_index]
                edge_8x8_indices = (1, 3) if neighbor_position == "left" else (2, 3)
                mode_offset = 1 if neighbor_position == "left" else 2
                for edge_index, group in enumerate(edge_8x8_indices):
                    mode_index = group * 4 + mode_offset
                    if mode_index < len(decoded.luma_4x4_modes):
                        luma_8x8_modes[edge_index] = decoded.luma_4x4_modes[mode_index]
    elif isinstance(decoded, CABACInterMacroblockResult) and not is_skipped:
        cbp_luma = decoded.coded_block_pattern_luma
        cbp_chroma = decoded.coded_block_pattern_chroma
        transform_8x8 = decoded.transform_size_8x8
        luma_4x4_flags = decoded.luma_4x4_coded_block_flags
        luma_8x8_flags = (False,) * 4
        luma_ac_flags = (False,) * 16
        luma_dc_coded = False
        chroma_dc_flags = decoded.chroma_dc_coded
        chroma_ac_flags = decoded.chroma_ac_coded_flags
    else:
        cbp_luma = 0
        cbp_chroma = 0
        transform_8x8 = False
        luma_4x4_flags = (False,) * 16
        luma_8x8_flags = (False,) * 4
        luma_ac_flags = (False,) * 16
        luma_dc_coded = False
        chroma_dc_flags = (False, False)
        chroma_ac_flags = ((False,) * 4, (False,) * 4)

    luma_available = []
    luma_coded = []
    luma16_ac_available = []
    luma16_ac_coded = []
    for edge_coordinate in range(4):
        raster_index = (
            edge_coordinate * 4 + 3
            if neighbor_position == "left"
            else 12 + edge_coordinate
        )
        scan_index = _LUMA4X4_BLOCK_SCAN_TO_RASTER.index(raster_index)
        group = (raster_index // 8) * 2 + (raster_index % 4) // 2
        block_available = bool(cbp_luma & (1 << group)) and not is_skipped and not is_ipcm
        if is_intra16x16:
            block_coded = luma_ac_flags[scan_index] if scan_index < len(luma_ac_flags) else False
        elif transform_8x8:
            block_coded = (
                luma_8x8_flags[group] or block_available
                if group < len(luma_8x8_flags)
                else block_available
            )
        else:
            block_coded = luma_4x4_flags[scan_index] if scan_index < len(luma_4x4_flags) else False
        luma_available.append(block_available)
        luma_coded.append(block_coded)
        luma16_ac_available.append(block_available)
        luma16_ac_coded.append(block_coded)

    luma4x4_edge = CABACIntra4x4EdgeState(
        available=True,
        is_ipcm=is_ipcm,
        transform_block_available=tuple(luma_available),
        transform_block_coded=tuple(luma_coded),
    )
    luma16x16_edge = CABACIntra16x16EdgeState(
        available=True,
        is_ipcm=is_ipcm,
        dc_transform_block_available=is_intra16x16,
        dc_transform_block_coded=luma_dc_coded,
        ac_transform_block_available=tuple(luma16_ac_available),
        ac_transform_block_coded=tuple(luma16_ac_coded),
    )
    edge_chroma_blocks = (1, 3) if neighbor_position == "left" else (2, 3)
    chroma_edges = tuple(
        CABACChroma420EdgeState(
            available=True,
            is_ipcm=is_ipcm,
            dc_block_available=cbp_chroma > 0 and not is_skipped and not is_ipcm,
            dc_block_coded=chroma_dc_flags[component],
            ac_block_available=(
                cbp_chroma == 2 and not is_skipped and not is_ipcm,
                cbp_chroma == 2 and not is_skipped and not is_ipcm,
            ),
            ac_block_coded=tuple(
                chroma_ac_flags[component][block] for block in edge_chroma_blocks
            ),
        )
        for component in range(2)
    )
    return CABACMacroblockNeighborCodedState(
        coded_block_pattern_luma=cbp_luma,
        coded_block_pattern_chroma=cbp_chroma,
        transform_size_8x8=transform_8x8,
        is_intra16x16=is_intra16x16,
        is_ipcm=is_ipcm,
        is_skipped=is_skipped,
        chroma_mode_nonzero=chroma_mode_nonzero,
        luma_4x4_edge=luma4x4_edge,
        luma_16x16_edge=luma16x16_edge,
        chroma_edges=chroma_edges,
        is_intra_nxn=is_intra_nxn,
        luma_4x4_modes=tuple(luma_4x4_modes),
        luma_8x8_modes=tuple(luma_8x8_modes),
    )


@dataclass(frozen=True)
class CABACInterMacroblockNeighborState:
    left_available: bool
    top_available: bool
    left_skipped: bool
    top_skipped: bool
    left_b_skip_or_direct: bool
    top_b_skip_or_direct: bool
    partition_neighbors: CABACInterPartitionNeighbors


def derive_cabac_inter_macroblock_neighbor_state_from_history(
    macroblock_address: int,
    picture_width_in_mbs: int,
    first_history_address: int,
    history: Sequence[CABACSliceMacroblockResult],
    slice_type: int,
    partition_x: int,
    partition_y: int,
    partition_width: int,
    partition_height: int,
) -> CABACInterMacroblockNeighborState:
    """Build external P/B partition neighbor facts from already-decoded slice history."""
    from .h264_reconstruction import MotionVector, MotionVectorCandidate

    values = (
        macroblock_address,
        picture_width_in_mbs,
        first_history_address,
        slice_type,
        partition_x,
        partition_y,
        partition_width,
        partition_height,
    )
    if any(not isinstance(value, int) or isinstance(value, bool) for value in values):
        raise CABACError("inter macroblock neighbor coordinates must be integers")
    if (
        macroblock_address < 0
        or picture_width_in_mbs <= 0
        or not 0 <= slice_type <= 9
        or slice_type % 5 not in (0, 1)
        or min(partition_x, partition_y) < 0
        or min(partition_width, partition_height) <= 0
        or partition_x + partition_width > 16
        or partition_y + partition_height > 16
        or not isinstance(history, Sequence)
    ):
        raise CABACError("inter macroblock neighbor inputs are invalid")
    neighbor_indices = derive_cabac_slice_neighbor_indices(
        macroblock_address, picture_width_in_mbs, first_history_address, len(history)
    )
    b_slice = slice_type % 5 == 1
    history_indices = {
        (-1, 0): neighbor_indices.left,
        (0, -1): neighbor_indices.top,
        (1, -1): neighbor_indices.top_right,
        (-1, -1): neighbor_indices.top_left,
    }

    def history_neighbor(sample_x: int, sample_y: int):
        macroblock_dx = -1 if sample_x < 0 else 1 if sample_x >= 16 else 0
        macroblock_dy = -1 if sample_y < 0 else 1 if sample_y >= 16 else 0
        history_index = history_indices.get((macroblock_dx, macroblock_dy))
        if history_index is None:
            return None
        expected_address = macroblock_address + macroblock_dy * picture_width_in_mbs + macroblock_dx
        entry = history[history_index]
        if not isinstance(entry, CABACSliceMacroblockResult) or entry.address != expected_address:
            raise CABACError("inter macroblock slice history is not contiguous")
        return entry, sample_x % 16, sample_y % 16

    def macroblock_skip_direct(entry: CABACSliceMacroblockResult) -> tuple[bool, bool]:
        decoded = entry.macroblock
        skipped = False
        if isinstance(decoded, CABACPBMacroblockLayerResult):
            skipped = decoded.skipped
            decoded = decoded.macroblock
        direct = isinstance(decoded, CABACBDirectMacroblockResult)
        if isinstance(decoded, CABACInterMacroblockResult):
            direct = b_slice and decoded.macroblock_type == 0
        return skipped, direct

    def partition_fact(sample_x: int, sample_y: int, list_index: int):
        location = history_neighbor(sample_x, sample_y)
        if location is None:
            return CABACInterNeighbor(), None
        entry, local_x, local_y = location
        decoded = entry.macroblock
        skipped = False
        if isinstance(decoded, CABACPBMacroblockLayerResult):
            skipped = decoded.skipped
            decoded = decoded.macroblock
        if isinstance(decoded, CABACIIntraMacroblockResult):
            return (
                CABACInterNeighbor(available=True, intra=True),
                MotionVectorCandidate(None, MotionVector(0, 0)),
            )

        if isinstance(decoded, CABACPSkipMacroblockResult):
            prediction_flags = (True, False)
            reference_indices = (decoded.reference_index, -1)
            motion_vectors = (decoded.motion_vector, (0, 0))
            motion_differences = ((0, 0), (0, 0))
            skipped = True
        elif isinstance(decoded, CABACBDirectMacroblockResult):
            prediction_flags = decoded.prediction_flags
            reference_indices = decoded.reference_indices
            motion_vectors = decoded.motion_vectors
            motion_differences = ((0, 0), (0, 0))
            skipped = skipped or decoded.skipped
        elif isinstance(decoded, CABACInterMacroblockResult):
            partition = next(
                (
                    item
                    for item in decoded.partitions
                    if item.x <= local_x < item.x + item.width
                    and item.y <= local_y < item.y + item.height
                ),
                None,
            )
            if partition is None:
                return CABACInterNeighbor(available=True), None
            prediction_flags = partition.prediction_flags
            reference_indices = partition.reference_indices
            motion_vectors = partition.motion_vectors
            motion_differences = partition.motion_vector_differences
        else:
            return CABACInterNeighbor(available=True), None

        active = prediction_flags[list_index]
        reference_index = reference_indices[list_index] if active else -1
        difference = motion_differences[list_index] if active else (0, 0)
        candidate = (
            MotionVectorCandidate(
                reference_index,
                MotionVector(*motion_vectors[list_index]),
            )
            if active and reference_index >= 0
            else MotionVectorCandidate(None, MotionVector(0, 0))
        )
        return (
            CABACInterNeighbor(
                available=True,
                skip=skipped,
                prediction_mode_matches=active,
                reference_index=max(0, reference_index),
                motion_vector_difference=tuple(difference),
            ),
            candidate,
        )

    def macroblock_flags(index: int | None) -> tuple[bool, bool, bool]:
        if index is None:
            return False, False, False
        entry = history[index]
        if not isinstance(entry, CABACSliceMacroblockResult):
            raise CABACError("inter macroblock slice history entry is invalid")
        skipped, direct = macroblock_skip_direct(entry)
        return True, skipped, b_slice and (skipped or direct)

    left_address = macroblock_address - 1 if macroblock_address % picture_width_in_mbs else None
    top_address = (
        macroblock_address - picture_width_in_mbs
        if macroblock_address >= picture_width_in_mbs
        else None
    )
    left_index = neighbor_indices.left if left_address is not None else None
    top_index = neighbor_indices.top if top_address is not None else None
    left_available, left_skipped, left_b_skip_or_direct = macroblock_flags(left_index)
    top_available, top_skipped, top_b_skip_or_direct = macroblock_flags(top_index)

    list_neighbors = []
    for list_index in range(2):
        left_neighbor, left_candidate = partition_fact(partition_x - 1, partition_y, list_index)
        top_neighbor, top_candidate = partition_fact(partition_x, partition_y - 1, list_index)
        top_right_neighbor, top_right_candidate = partition_fact(
            partition_x + partition_width, partition_y - 1, list_index
        )
        top_left_neighbor, top_left_candidate = partition_fact(
            partition_x - 1, partition_y - 1, list_index
        )
        list_neighbors.append(
            CABACInterListNeighbors(
                left=left_neighbor,
                top=top_neighbor,
                left_candidate=left_candidate,
                top_candidate=top_candidate,
                top_right_candidate=top_right_candidate,
                top_left_candidate=top_left_candidate,
            )
        )
    return CABACInterMacroblockNeighborState(
        left_available,
        top_available,
        left_skipped,
        top_skipped,
        left_b_skip_or_direct,
        top_b_skip_or_direct,
        CABACInterPartitionNeighbors(list0=list_neighbors[0], list1=list_neighbors[1]),
    )


_LUMA8X8_SCAN_TO_RASTER = (
    0, 1, 8, 16, 9, 2, 3, 10, 17, 24, 32, 25, 18, 11, 4, 5,
    12, 19, 26, 33, 40, 48, 41, 34, 27, 20, 13, 6, 7, 14, 21, 28,
    35, 42, 49, 56, 57, 50, 43, 36, 29, 22, 15, 23, 30, 37, 44, 51,
    58, 59, 52, 45, 38, 31, 39, 46, 53, 60, 61, 54, 47, 55, 62, 63,
)


def derive_cabac_reference_index_context_increment(
    left: CABACInterNeighbor,
    top: CABACInterNeighbor,
    mbaff_frame: bool = False,
    current_is_field: bool = False,
) -> int:
    """Derive ref_idx_lX ctxIdxInc from clause 9.3.3.1.1.6 neighbour facts."""
    _validate_inter_neighbors(left, top)
    increment = 0
    for neighbor, bit in ((left, 1), (top, 2)):
        if (
            not neighbor.available
            or neighbor.skip
            or neighbor.intra
            or not neighbor.prediction_mode_matches
        ):
            continue
        zero_threshold = int(mbaff_frame and not current_is_field and neighbor.is_field)
        if neighbor.reference_index > zero_threshold:
            increment |= bit
    return increment


def derive_cabac_mvd_context_increment(
    left: CABACInterNeighbor,
    top: CABACInterNeighbor,
    component: int,
    mbaff_frame: bool = False,
    current_is_field: bool = False,
) -> int:
    """Derive mvd_lX ctxIdxInc from clause 9.3.3.1.1.7 neighbour facts."""
    _validate_inter_neighbors(left, top)
    if not isinstance(component, int) or isinstance(component, bool) or component not in (0, 1):
        raise CABACError("MVD component index must be 0 or 1")
    left_abs = _cabac_mvd_neighbor_magnitude(left, component, mbaff_frame, current_is_field)
    top_abs = _cabac_mvd_neighbor_magnitude(top, component, mbaff_frame, current_is_field)
    if left_abs > 32 or top_abs > 32 or left_abs + top_abs > 32:
        return 2
    return int(left_abs + top_abs > 2)


def _validate_inter_neighbors(left: CABACInterNeighbor, top: CABACInterNeighbor) -> None:
    for neighbor in (left, top):
        if not isinstance(neighbor, CABACInterNeighbor):
            raise CABACError("inter prediction neighbors must be CABACInterNeighbor values")
        if (
            not isinstance(neighbor.reference_index, int)
            or isinstance(neighbor.reference_index, bool)
            or not 0 <= neighbor.reference_index <= 31
        ):
            raise CABACError("neighbor reference index is outside [0,31]")
        if (
            not isinstance(neighbor.motion_vector_difference, tuple)
            or len(neighbor.motion_vector_difference) != 2
            or any(
                not isinstance(value, int)
                or isinstance(value, bool)
                or not -(1 << 31) <= value < (1 << 31)
                for value in neighbor.motion_vector_difference
            )
        ):
            raise CABACError("neighbor MVD must contain two signed 32-bit components")


def _cabac_mvd_neighbor_magnitude(
    neighbor: CABACInterNeighbor,
    component: int,
    mbaff_frame: bool,
    current_is_field: bool,
) -> int:
    if (
        not neighbor.available
        or neighbor.skip
        or neighbor.intra
        or not neighbor.prediction_mode_matches
    ):
        return 0
    magnitude = abs(neighbor.motion_vector_difference[component])
    if component == 1 and mbaff_frame:
        if not current_is_field and neighbor.is_field:
            magnitude *= 2
        elif current_is_field and not neighbor.is_field:
            magnitude //= 2
    return magnitude


def _intra4x4_luma_cond_term(
    block_index: int,
    block_x: int,
    block_y: int,
    is_left: bool,
    coded_flags: Sequence[bool],
    edge: CABACIntra4x4EdgeState,
) -> bool:
    neighbor_coordinate = block_x if is_left else block_y
    edge_coordinate = block_y if is_left else block_x
    if neighbor_coordinate > 0:
        raster_index = _LUMA4X4_BLOCK_SCAN_TO_RASTER[block_index]
        neighbor_raster = raster_index - (1 if is_left else 4)
        neighbor_index = _LUMA4X4_BLOCK_SCAN_TO_RASTER.index(neighbor_raster)
        return derive_coded_block_flag_cond_term(
            True, True, False, True, coded_flags[neighbor_index]
        )
    if not edge.available:
        return True
    if edge.is_ipcm:
        return True
    return derive_coded_block_flag_cond_term(
        True,
        True,
        False,
        edge.transform_block_available[edge_coordinate],
        edge.transform_block_coded[edge_coordinate],
    )


def _inter4x4_luma_cond_term(
    block_index: int,
    block_x: int,
    block_y: int,
    is_left: bool,
    coded_flags: Sequence[bool],
    edge: CABACIntra4x4EdgeState,
) -> bool:
    neighbor_coordinate = block_x if is_left else block_y
    edge_coordinate = block_y if is_left else block_x
    if neighbor_coordinate > 0:
        raster_index = _LUMA4X4_BLOCK_SCAN_TO_RASTER[block_index]
        neighbor_raster = raster_index - (1 if is_left else 4)
        neighbor_index = _LUMA4X4_BLOCK_SCAN_TO_RASTER.index(neighbor_raster)
        return derive_coded_block_flag_cond_term(
            True, False, False, True, coded_flags[neighbor_index]
        )
    if not edge.available:
        return derive_coded_block_flag_cond_term(False, False, False, False, False)
    if edge.is_ipcm:
        return True
    return derive_coded_block_flag_cond_term(
        True,
        False,
        False,
        edge.transform_block_available[edge_coordinate],
        edge.transform_block_coded[edge_coordinate],
    )


def _intra16x16_ac_cond_term(
    block_index: int,
    block_x: int,
    block_y: int,
    is_left: bool,
    coded_flags: Sequence[bool],
    edge: CABACIntra16x16EdgeState,
) -> bool:
    neighbor_coordinate = block_x if is_left else block_y
    edge_coordinate = block_y if is_left else block_x
    if neighbor_coordinate > 0:
        raster_index = _LUMA4X4_BLOCK_SCAN_TO_RASTER[block_index]
        neighbor_raster = raster_index - (1 if is_left else 4)
        neighbor_index = _LUMA4X4_BLOCK_SCAN_TO_RASTER.index(neighbor_raster)
        return derive_coded_block_flag_cond_term(
            True, True, False, True, coded_flags[neighbor_index]
        )
    if not edge.available or edge.is_ipcm:
        return True
    return derive_coded_block_flag_cond_term(
        True,
        True,
        False,
        edge.ac_transform_block_available[edge_coordinate],
        edge.ac_transform_block_coded[edge_coordinate],
    )


_TRANSITION_LPS = (
    0, 0, 1, 2, 2, 4, 4, 5, 6, 7, 8, 9, 9, 11, 11, 12,
    13, 13, 15, 15, 16, 16, 18, 18, 19, 19, 21, 21, 22, 22, 23, 24,
    24, 25, 26, 26, 27, 27, 28, 29, 29, 30, 30, 30, 31, 32, 32, 33,
    33, 33, 34, 34, 35, 35, 35, 36, 36, 36, 37, 37, 37, 38, 38, 63,
)
_MAX_QPY = 51
_MAX_MOTION_VECTOR_DIFFERENCE = 0x7FFFFFFF
_COEFF_ABS_LEVEL1_CONTEXT = (1, 2, 3, 4, 0, 0, 0, 0)
_COEFF_ABS_LEVEL_GREATER1_CONTEXT = (5, 5, 5, 5, 6, 7, 8, 9)
_COEFF_LEVEL1_TRANSITION = (1, 2, 3, 3, 4, 5, 6, 7)
_COEFF_LEVEL_GREATER1_TRANSITION = (4, 4, 4, 4, 5, 6, 7, 7)
_I_INTRA_MB_TYPE_INIT = (
    (20, -15), (2, 54), (3, 74), (-28, 127),
    (-23, 104), (-6, 53), (-1, 54), (7, 51),
)
_P_INTER_MB_TYPE_INIT = (
    ((1, 9), (0, 49), (-37, 118), (5, 57)),
    ((-2, 9), (4, 41), (-29, 118), (2, 65)),
    ((-10, 51), (-3, 62), (-27, 99), (26, 16)),
)
_INTER_MVD_INIT = (
    ((-3, 69), (-6, 81), (-11, 96), (6, 55), (7, 67), (-5, 86), (2, 88), (0, 58), (-3, 76), (-10, 94), (5, 54), (4, 69), (-3, 81), (0, 88)),
    ((-2, 69), (-5, 82), (-10, 96), (2, 59), (2, 75), (-3, 87), (-3, 100), (1, 56), (-3, 74), (-6, 85), (0, 59), (-3, 81), (-7, 86), (-5, 95)),
    ((-11, 89), (-15, 103), (-21, 116), (19, 57), (20, 58), (4, 84), (6, 96), (1, 63), (-5, 85), (-13, 106), (5, 63), (6, 75), (-3, 90), (-1, 101)),
)
_INTER_REF_IDX_INIT = (
    ((-7, 67), (-5, 74), (-4, 74), (-5, 80), (-7, 72), (1, 58)),
    ((-1, 66), (-1, 77), (1, 70), (-2, 86), (-5, 72), (0, 61)),
    ((3, 55), (-4, 79), (-2, 75), (-12, 97), (-7, 50), (1, 60)),
)
_I_MB_QP_DELTA_INIT = ((0, 41), (0, 63), (0, 63), (0, 63))
_I_INTRA_CHROMA_PRED_MODE_INIT = ((-9, 83), (4, 86), (0, 97), (-7, 72))
_I_INTRA4X4_PRED_MODE_INIT = ((13, 41), (3, 62))
_I_TRANSFORM_SIZE_8X8_INIT = ((31, 21), (31, 31), (25, 50))
_I_LUMA_CODED_BLOCK_PATTERN_INIT = ((-17, 127), (-13, 102), (0, 82), (-7, 74))
_I_CHROMA_CODED_BLOCK_PATTERN_INIT = (
    (-21, 107), (-27, 127), (-31, 127), (-24, 127),
    (-18, 95), (-27, 127), (-21, 114), (-30, 127),
)
_I_LUMA4X4_CODED_BLOCK_FLAG_INIT = ((-3, 70), (-8, 93), (-10, 90), (-30, 127))
_RANGE_LPS = (
    (128, 128, 128, 123, 116, 111, 105, 100, 95, 90, 85, 81, 77, 73, 69, 66, 62, 59, 56, 53, 51, 48, 46, 43, 41, 39, 37, 35, 33, 32, 30, 29, 27, 26, 24, 23, 22, 21, 20, 19, 18, 17, 16, 15, 14, 14, 13, 12, 12, 11, 11, 10, 10, 9, 9, 8, 8, 7, 7, 7, 6, 6, 6, 2),
    (176, 167, 158, 150, 142, 135, 128, 122, 116, 110, 104, 99, 94, 89, 85, 80, 76, 72, 69, 65, 62, 59, 56, 53, 50, 48, 45, 43, 41, 39, 37, 35, 33, 31, 30, 28, 27, 26, 24, 23, 22, 21, 20, 19, 18, 17, 16, 15, 14, 14, 13, 12, 12, 11, 11, 10, 9, 9, 9, 8, 8, 7, 7, 2),
    (208, 197, 187, 178, 169, 160, 152, 144, 137, 130, 123, 117, 111, 105, 100, 95, 90, 86, 81, 77, 73, 69, 66, 63, 59, 56, 54, 51, 48, 46, 43, 41, 39, 37, 35, 33, 32, 30, 29, 27, 26, 25, 23, 22, 21, 20, 19, 18, 17, 16, 15, 15, 14, 13, 12, 12, 11, 11, 10, 10, 9, 9, 8, 2),
    (240, 227, 216, 205, 195, 185, 175, 166, 158, 150, 142, 135, 128, 122, 116, 110, 104, 99, 94, 89, 85, 80, 76, 72, 69, 65, 62, 59, 56, 53, 50, 48, 45, 43, 41, 39, 37, 35, 33, 31, 30, 28, 27, 25, 24, 23, 22, 21, 20, 19, 18, 17, 16, 15, 14, 14, 13, 12, 12, 11, 11, 10, 9, 2),
)
_LUMA4X4_SCAN_TO_RASTER = (0, 1, 4, 8, 5, 2, 3, 6, 9, 12, 13, 10, 7, 11, 14, 15)
_LUMA4X4_BLOCK_SCAN_TO_RASTER = (0, 1, 4, 5, 2, 3, 6, 7, 8, 9, 12, 13, 10, 11, 14, 15)
_LUMA4X4_BLOCK_RASTER_TO_SCAN = tuple(
    _LUMA4X4_BLOCK_SCAN_TO_RASTER.index(raster_index) for raster_index in range(16)
)


def derive_intra4x4_predicted_mode(
    decoded_modes: Sequence[int],
    block_index: int,
    top_modes: Sequence[int],
    left_modes: Sequence[int],
    top_available: bool,
    left_available: bool,
) -> int:
    """Derive predIntra4x4PredMode from A/B neighbors in syntax scan order."""
    if (
        len(decoded_modes) != 16
        or len(top_modes) != 4
        or len(left_modes) != 4
        or not isinstance(block_index, int)
        or isinstance(block_index, bool)
        or not 0 <= block_index < 16
    ):
        raise CABACError("Intra_4x4 prediction neighbor state is invalid")

    raster_index = _LUMA4X4_BLOCK_SCAN_TO_RASTER[block_index]
    column, row = raster_index % 4, raster_index // 4
    neighbors = []
    for is_left, coordinate, available, external_modes in (
        (True, column, left_available, left_modes),
        (False, row, top_available, top_modes),
    ):
        if coordinate > 0:
            neighbor_raster = raster_index - (1 if is_left else 4)
            neighbor_index = _LUMA4X4_BLOCK_SCAN_TO_RASTER.index(neighbor_raster)
            if neighbor_index >= block_index:
                raise CABACError("Intra_4x4 prediction neighbor is not decoded yet")
            mode = decoded_modes[neighbor_index]
            available = True
        elif available:
            mode = external_modes[row if is_left else column]
        else:
            mode = 2
        if available and (
            not isinstance(mode, int)
            or isinstance(mode, bool)
            or not 0 <= mode <= 8
        ):
            raise CABACError("Intra_4x4 prediction neighbor mode is outside [0,8]")
        neighbors.append(mode if available else None)

    left_mode, top_mode = neighbors
    if left_mode is None or top_mode is None:
        return 2
    return min(left_mode, top_mode)


class CABACContextModel:
    """One H.264 CABAC context's probability state and most-probable bin."""

    def __init__(self, m: int, n: int, slice_qpy: int):
        if not -128 <= m <= 127 or not -128 <= n <= 127 or not 0 <= slice_qpy <= 51:
            raise CABACError("CABAC context initialization value is outside its valid range")
        pre_context_state = ((m * slice_qpy) >> 4) + n
        pre_context_state = min(126, max(1, pre_context_state))
        if pre_context_state <= 63:
            self._state_index = 63 - pre_context_state
            self._value_mps = False
        else:
            self._state_index = pre_context_state - 64
            self._value_mps = True

    @property
    def state_index(self) -> int:
        return self._state_index

    @property
    def mps(self) -> bool:
        return self._value_mps

    def update(self, bin_value: bool) -> None:
        if bin_value == self._value_mps:
            if self._state_index < 62:
                self._state_index += 1
            return
        if self._state_index == 0:
            self._value_mps = not self._value_mps
        self._state_index = _TRANSITION_LPS[self._state_index]


def initialize_i_intra_mb_type_contexts(slice_qpy: int) -> list[CABACContextModel]:
    """Initialize CABAC context indices 3-10 from H.264 Table 9-12."""
    return [CABACContextModel(m, n, slice_qpy) for m, n in _I_INTRA_MB_TYPE_INIT]


def initialize_p_inter_mb_type_contexts(
    cabac_init_idc: int, slice_qpy: int
) -> list[CABACContextModel]:
    """Initialize P-slice mb_type contexts 14-17 from H.264 Table 9-13."""
    if not isinstance(cabac_init_idc, int) or isinstance(cabac_init_idc, bool) or not 0 <= cabac_init_idc <= 2:
        raise CABACError("CABAC init idc is outside [0,2]")
    return [
        CABACContextModel(m, n, slice_qpy)
        for m, n in _P_INTER_MB_TYPE_INIT[cabac_init_idc]
    ]


def initialize_inter_prediction_contexts(
    cabac_init_idc: int, slice_qpy: int
) -> tuple[list[CABACContextModel], list[CABACContextModel], list[CABACContextModel]]:
    """Initialize MVD contexts 40-53 and the ref_idx contexts 54-59 shared by both lists."""
    if not isinstance(cabac_init_idc, int) or isinstance(cabac_init_idc, bool) or not 0 <= cabac_init_idc <= 2:
        raise CABACError("CABAC init idc is outside [0,2]")
    parameters = _INTER_MVD_INIT[cabac_init_idc]
    mvd_x = [CABACContextModel(m, n, slice_qpy) for m, n in parameters[:7]]
    mvd_y = [CABACContextModel(m, n, slice_qpy) for m, n in parameters[7:]]
    ref_idx = [CABACContextModel(m, n, slice_qpy) for m, n in _INTER_REF_IDX_INIT[cabac_init_idc]]
    return mvd_x, mvd_y, ref_idx


def initialize_i_mb_qp_delta_contexts(slice_qpy: int) -> list[CABACContextModel]:
    """Initialize I-slice mb_qp_delta contexts 60-63 from Table 9-17."""
    return [CABACContextModel(m, n, slice_qpy) for m, n in _I_MB_QP_DELTA_INIT]


def initialize_i_intra_chroma_pred_mode_contexts(
    slice_qpy: int,
) -> list[CABACContextModel]:
    """Initialize I-slice chroma prediction contexts 64-67 from Table 9-17."""
    return [CABACContextModel(m, n, slice_qpy) for m, n in _I_INTRA_CHROMA_PRED_MODE_INIT]


def initialize_i_intra4x4_pred_mode_contexts(slice_qpy: int) -> list[CABACContextModel]:
    """Initialize I-slice Intra_NxN mode contexts 68-69 from Table 9-17."""
    return [CABACContextModel(m, n, slice_qpy) for m, n in _I_INTRA4X4_PRED_MODE_INIT]


def initialize_i_transform_size_8x8_contexts(slice_qpy: int) -> list[CABACContextModel]:
    """Initialize I-slice transform-size contexts 399-401 from Table 9-16."""
    return [CABACContextModel(m, n, slice_qpy) for m, n in _I_TRANSFORM_SIZE_8X8_INIT]


def initialize_i_luma_coded_block_pattern_contexts(
    slice_qpy: int,
) -> list[CABACContextModel]:
    """Initialize I-slice luma CBP contexts 73-76 from Table 9-18."""
    return [CABACContextModel(m, n, slice_qpy) for m, n in _I_LUMA_CODED_BLOCK_PATTERN_INIT]


def initialize_i_chroma_coded_block_pattern_contexts(
    slice_qpy: int,
) -> list[CABACContextModel]:
    """Initialize I-slice chroma CBP contexts 77-84 from Table 9-18."""
    return [CABACContextModel(m, n, slice_qpy) for m, n in _I_CHROMA_CODED_BLOCK_PATTERN_INIT]


def initialize_i_luma4x4_coded_block_flag_contexts(
    slice_qpy: int,
) -> list[CABACContextModel]:
    """Initialize I-slice luma 4x4 coded-block-flag contexts 93-96 from Table 9-18."""
    return [CABACContextModel(m, n, slice_qpy) for m, n in _I_LUMA4X4_CODED_BLOCK_FLAG_INIT]


CONTEXT_COUNT = 460
# maxNumCoeff for ctxBlockCat 0-4 with 4:2:0 chroma (4 * NumC8x8 = 4 chroma DC).
_RESIDUAL_MAX_NUM_COEFF = (16, 15, 16, 4, 15)
# Table 9-43 ctxIdxInc by levelListIdx for frame-coded significant/last flags.
_LUMA8X8_FRAME_SIGNIFICANT_INC = (
    0, 1, 2, 3, 4, 5, 5, 4, 4, 3, 3, 4, 4, 4, 5, 5, 4, 4, 4, 4, 3, 3, 6, 7, 7, 7, 8, 9, 10, 9, 8, 7,
    7, 6, 11, 12, 13, 11, 6, 7, 8, 9, 14, 10, 9, 8, 6, 11, 12, 13, 11, 6, 9, 14, 10, 9, 11, 12, 13, 11, 14, 10, 12,
)
_LUMA8X8_LAST_INC = (
    0, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2,
    3, 3, 3, 3, 3, 3, 3, 3, 4, 4, 4, 4, 4, 4, 4, 4, 5, 5, 5, 5, 6, 6, 6, 6, 7, 7, 7, 7, 8, 8, 8,
)


def _validate_slice_contexts(
    contexts: list[CABACContextModel],
    context_indices: Sequence[int] | None = None,
) -> None:
    if (
        not isinstance(contexts, list)
        or len(contexts) != CONTEXT_COUNT
    ):
        raise CABACError(f"residual block requires {CONTEXT_COUNT} slice contexts")
    models = (
        contexts
        if context_indices is None
        else (contexts[index] for index in context_indices)
    )
    if any(not isinstance(model, CABACContextModel) for model in models):
        raise CABACError(f"residual block requires {CONTEXT_COUNT} slice contexts")


def derive_coded_block_flag_cond_term(
    neighbor_available: bool,
    current_intra: bool,
    neighbor_ipcm: bool,
    trans_block_available: bool,
    trans_block_coded: bool,
) -> bool:
    """Return condTermFlagN for coded_block_flag (clause 9.3.3.1.1.9).

    The slice-data-partitioning rule is omitted because partitioning is unsupported.
    """
    if not neighbor_available:
        return bool(current_intra)
    if neighbor_ipcm:
        return True
    if not trans_block_available:
        return False
    return bool(trans_block_coded)


def initialize_slice_contexts(
    slice_type: int, cabac_init_idc: int, slice_qpy: int
) -> list[CABACContextModel]:
    """Initialize ctxIdx 0-459 for one slice from H.264 Tables 9-12 to 9-24.

    I/SI slices ignore cabac_init_idc; entries unused by the slice type
    initialize from (0, 0).
    """
    if not isinstance(slice_type, int) or isinstance(slice_type, bool) or not 0 <= slice_type <= 9:
        raise CABACError("CABAC syntax is unsupported for this slice type")
    column = 0
    if slice_type % 5 not in (2, 4):
        if not isinstance(cabac_init_idc, int) or isinstance(cabac_init_idc, bool) or not 0 <= cabac_init_idc <= 2:
            raise CABACError("CABAC init idc is outside [0,2]")
        column = cabac_init_idc + 1
    return [CABACContextModel(m, n, slice_qpy) for m, n in CONTEXT_INIT_TABLE[column]]


def residual_context_bases(ctx_block_cat: int) -> tuple[int | None, int, int, int]:
    """Return frame-coded first ctxIdx for coded_block_flag, significant, last, and abs level.

    coded_block_flag is None for ctxBlockCat 5, whose flag is inferred when
    ChromaArrayType != 3.
    """
    if not isinstance(ctx_block_cat, int) or isinstance(ctx_block_cat, bool) or not 0 <= ctx_block_cat <= 5:
        raise CABACError("CABAC syntax is unsupported for this ctxBlockCat")
    if ctx_block_cat == 5:
        return None, 402, 417, 426
    significance_offset = (0, 15, 29, 44, 47)[ctx_block_cat]
    return (
        85 + 4 * ctx_block_cat,
        105 + significance_offset,
        166 + significance_offset,
        227 + (0, 10, 20, 30, 39)[ctx_block_cat],
    )


def _derive_spatial_b_direct_partition(
    input: CABACBDirectMacroblockInput,
    neighbors: CABACInterPartitionNeighbors,
    partition_x: int = 0,
    partition_y: int = 0,
    direct_8x8_inference: bool = False,
) -> CABACBDirectMacroblockResult:
    if not input.direct_spatial:
        raise CABACError("temporal B direct requires co-located partition metadata")
    from .h264_reconstruction import MotionVector, MotionVectorCandidate, predict_motion_vector

    def minimum_positive(left: int, right: int) -> int:
        return min(left, right) if left >= 0 and right >= 0 else max(left, right)

    list_neighbors = (neighbors.list0, neighbors.list1)
    references = (input.list0_references, input.list1_references)
    reference_indices = []
    for list_index, state in enumerate(list_neighbors):
        top_right = (
            state.top_right_candidate
            if state.top_right_candidate is not None
            else state.top_left_candidate
        )
        candidates = (state.left_candidate, state.top_candidate, top_right)
        if any(
            candidate is not None and not isinstance(candidate, MotionVectorCandidate)
            for candidate in candidates
        ):
            raise CABACError("B_Direct neighbor motion metadata is invalid")
        indices = tuple(
            -1 if candidate is None or candidate.reference_index is None
            else candidate.reference_index
            for candidate in candidates
        )
        a, b, c = indices
        reference_indices.append(minimum_positive(a, minimum_positive(b, c)))

    direct_zero = reference_indices == [-1, -1]
    if direct_zero:
        reference_indices = [0, 0]

    motion_vectors = []
    for list_index, state in enumerate(list_neighbors):
        reference_index = reference_indices[list_index]
        if reference_index < 0:
            motion_vectors.append((0, 0))
            continue
        if reference_index >= len(references[list_index]):
            raise CABACError("B_Direct spatial reference index is unavailable")
        colocated_index = (partition_y // 4) * 4 + partition_x // 4
        if direct_8x8_inference:
            macroblock_partition_index = (partition_y // 8) * 2 + partition_x // 8
            colocated_index = _LUMA4X4_BLOCK_SCAN_TO_RASTER[
                5 * macroblock_partition_index
            ]
        colocated_reference_index = input.colocated_reference_index
        colocated_motion_vector = input.colocated_motion_vector
        if input.colocated_motion_field:
            if len(input.colocated_motion_field) != 16:
                raise CABACError("B_Direct co-located motion field is invalid")
            colocated_fact = input.colocated_motion_field[colocated_index]
            from .h264_reconstruction import ColocatedMotionFact

            if not isinstance(colocated_fact, ColocatedMotionFact):
                raise CABACError("B_Direct co-located motion fact is invalid")
            colocated_reference_index = colocated_fact.reference_index
            colocated_motion_vector = (
                colocated_fact.vector.x,
                colocated_fact.vector.y,
            )
        candidate_zero = (
            not input.list1_references[0].long_term
            and colocated_reference_index == 0
            and all(-1 <= value <= 1 for value in colocated_motion_vector)
        )
        if direct_zero or (reference_index == 0 and candidate_zero):
            motion_vectors.append((0, 0))
            continue
        top_left = state.top_left_candidate
        predicted = predict_motion_vector(
            reference_index,
            state.left_candidate,
            state.top_candidate,
            state.top_right_candidate,
            top_left,
        )
        motion_vectors.append((predicted.x, predicted.y))

    prediction_flags = tuple(index >= 0 for index in reference_indices)
    if not any(prediction_flags):
        raise CABACError("B_Direct has no active prediction list")
    return CABACBDirectMacroblockResult(
        False,
        tuple(reference_indices),
        tuple(motion_vectors),
        prediction_flags,
        (),
        (),
        (),
    )


class CABACArithmeticDecoder:
    """Own one CABAC substream's arithmetic range and code offset.

    Input must start at the byte-aligned CABAC RBSP data, after removing NAL
    emulation-prevention bytes and consuming slice-header alignment bits.
    """

    INITIAL_RANGE = 510

    def __init__(
        self,
        data: bytes,
        *,
        timeout_check: Callable[[], None] | None = None,
    ):
        if timeout_check is not None and not callable(timeout_check):
            raise CABACError("CABAC timeout check must be callable")
        self._bits = BitReader(data)
        self._timeout_check = timeout_check
        try:
            self._code_offset = self._bits.read_bits(9)
        except BitstreamError as error:
            raise CABACError(f"CABAC initial offset: {error}") from error
        if self._code_offset >= self.INITIAL_RANGE:
            raise CABACError("CABAC initial offset is outside the arithmetic range")
        self._code_range = self.INITIAL_RANGE
        self._terminated = False
        self._cavlc = None

    @property
    def code_range(self) -> int:
        return self._code_range

    @property
    def code_offset(self) -> int:
        return self._code_offset

    def decode_bin(self, model: CABACContextModel) -> bool:
        if getattr(self, "_cavlc", None) is not None:
            raise CABACError("unhandled CAVLC syntax reached arithmetic decoder")
        if not isinstance(model, CABACContextModel) or not 0 <= model._state_index < 64:
            raise CABACError("CABAC context state index is outside [0,63]")
        self._validate_bin_state()
        original_range, original_offset = self._code_range, self._code_offset
        start_offset = self._bits._bit_offset
        range_lps = _RANGE_LPS[(self._code_range >> 6) & 3][model._state_index]
        range_mps = self._code_range - range_lps
        decoded = model._value_mps
        if self._code_offset >= range_mps:
            decoded = not decoded
            self._code_offset -= range_mps
            self._code_range = range_lps
        else:
            self._code_range = range_mps
        try:
            self.renormalize()
        except CABACError:
            self._code_range, self._code_offset = original_range, original_offset
            self._bits._bit_offset = start_offset
            raise
        model.update(decoded)
        return decoded

    def decode_mb_qp_delta(self, contexts: list[CABACContextModel], previous_delta: int) -> int:
        if getattr(self, "_cavlc", None) is not None:
            return CAVLCSyntax.signed(self._bits)
        if not isinstance(contexts, list) or len(contexts) != 4 or any(
            not isinstance(model, CABACContextModel) or not 0 <= model._state_index < 64
            for model in contexts
        ):
            raise CABACError("mb_qp_delta requires context models 60 through 63")
        if len({id(model) for model in contexts}) != 4:
            raise CABACError("mb_qp_delta context models must be distinct")

        trial_bits = BitReader(self._bits._data)
        trial_bits._bit_offset = self._bits._bit_offset
        trial = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
        trial._bits = trial_bits
        trial._code_range = self._code_range
        trial._code_offset = self._code_offset
        trial._terminated = self._terminated
        trial._cavlc = self._cavlc.clone() if getattr(self, "_cavlc", None) is not None else None
        trial_contexts = [CABACContextModel.__new__(CABACContextModel) for _ in range(4)]
        for target, source in zip(trial_contexts, contexts):
            target._state_index = source._state_index
            target._value_mps = source._value_mps

        context_index = int(previous_delta != 0)
        first = trial.decode_bin(trial_contexts[context_index])
        if first:
            value = 1
            context_index = 2
            while trial.decode_bin(trial_contexts[context_index]):
                value += 1
                if value > 2 * _MAX_QPY:
                    raise CABACError("CABAC mb_qp_delta exceeds the 8-bit QP range")
                context_index = 3
            delta = (value + 1) >> 1
            if value % 2 == 0:
                delta = -delta
        else:
            delta = 0

        self._bits._bit_offset = trial._bits._bit_offset
        self._code_range = trial._code_range
        self._code_offset = trial._code_offset
        self._terminated = trial._terminated
        self._cavlc = getattr(trial, "_cavlc", None)
        for target, source in zip(contexts, trial_contexts):
            target._state_index = source._state_index
            target._value_mps = source._value_mps
        return delta

    def decode_mb_skip_flag(
        self,
        slice_type: int,
        contexts: list[CABACContextModel],
        left_available: bool,
        left_skipped: bool,
        top_available: bool,
        top_skipped: bool,
    ) -> bool:
        if getattr(self, "_cavlc", None) is not None:
            return self._cavlc.skipped(self._bits)
        if slice_type > 9 or slice_type % 5 not in (0, 1):
            raise CABACError("CABAC mb_skip_flag is unsupported for this slice type")
        if not isinstance(contexts, list) or len(contexts) != 3:
            raise CABACError("mb_skip_flag requires three P- or B-slice context models")
        context_index = int(left_available and not left_skipped) + int(top_available and not top_skipped)
        return self.decode_bin(contexts[context_index])

    def decode_p_skip_macroblock(
        self,
        input: CABACPSkipMacroblockInput,
        contexts: list[CABACContextModel],
        builder,
        address: int,
    ) -> CABACPSkipMacroblockResult:
        """Decode one P_Skip flag and, when true, motion-compensate RefPicList0[0]."""
        _validate_slice_contexts(contexts)
        if not isinstance(input, CABACPSkipMacroblockInput) or input.slice_type % 5 != 0:
            raise CABACError("P_Skip requires a P-slice macroblock input")
        if builder is None:
            raise CABACError("P_Skip frame builder is required")
        if any(
            not isinstance(coordinate, int)
            or isinstance(coordinate, bool)
            or coordinate < 0
            for coordinate in (input.macroblock_x, input.macroblock_y)
        ):
            raise CABACError("P_Skip macroblock coordinates are invalid")
        trial, trial_contexts = self._slice_trial(contexts)
        skipped = trial.decode_mb_skip_flag(
            input.slice_type,
            trial_contexts[11:14],
            input.left_available,
            input.left_skipped,
            input.top_available,
            input.top_skipped,
        )
        if not skipped:
            self._commit_slice_trial(trial, contexts, trial_contexts)
            return CABACPSkipMacroblockResult(False, -1, (0, 0), (), (), ())

        from .h264_reconstruction import predict_motion_vector

        left, top = input.left_candidate, input.top_candidate
        motion = (0, 0)
        if input.left_available and input.top_available and not (
            left is not None
            and left.reference_index == 0
            and left.vector.x == 0
            and left.vector.y == 0
        ) and not (
            top is not None
            and top.reference_index == 0
            and top.vector.x == 0
            and top.vector.y == 0
        ):
            predicted = predict_motion_vector(
                0,
                left,
                top,
                input.top_right_candidate,
                input.top_left_candidate,
            )
            motion = (predicted.x, predicted.y)
        frame = input.reference_frame
        if frame is None:
            raise CABACError("P_Skip reference picture is required")
        samples = _reconstruct_or_defer(
            "pskip",
            (frame,),
            (input.macroblock_x, input.macroblock_y, motion),
            getattr(trial, "_timeout_check", None),
        )
        if samples is not None:
            builder.place_macroblock(address, *samples)
        luma, cb, cr = samples or _PLACEHOLDER_SAMPLES
        self._commit_slice_trial(trial, contexts, trial_contexts)
        return CABACPSkipMacroblockResult(True, 0, motion, tuple(luma), tuple(cb), tuple(cr))

    def decode_b_direct_macroblock(
        self,
        input: CABACBDirectMacroblockInput,
        builder,
        address: int,
    ) -> CABACBDirectMacroblockResult:
        """Derive and reconstruct one B_Direct macroblock from supplied reference metadata."""
        return self._reconstruct_b_direct_macroblock(input, builder, address, False)

    def decode_b_skip_macroblock(
        self,
        input: CABACBDirectMacroblockInput,
        contexts: list[CABACContextModel],
        builder,
        address: int,
    ) -> CABACBDirectMacroblockResult:
        """Decode one B_Skip flag and reconstruct its direct prediction when skipped."""
        _validate_slice_contexts(contexts)
        if not isinstance(input, CABACBDirectMacroblockInput) or input.slice_type % 5 != 1:
            raise CABACError("B_Skip requires a B-slice macroblock input")
        trial, trial_contexts = self._slice_trial(contexts)
        skipped = trial.decode_mb_skip_flag(
            input.slice_type,
            trial_contexts[24:27],
            input.left_available,
            input.left_skipped,
            input.top_available,
            input.top_skipped,
        )
        if not skipped:
            self._commit_slice_trial(trial, contexts, trial_contexts)
            return CABACBDirectMacroblockResult(
                False, (-1, -1), ((0, 0), (0, 0)), (False, False), (), (), ()
            )
        result = self._reconstruct_b_direct_macroblock(input, builder, address, True)
        self._commit_slice_trial(trial, contexts, trial_contexts)
        return result

    def _reconstruct_b_direct_macroblock(
        self,
        input: CABACBDirectMacroblockInput,
        builder,
        address: int,
        skipped: bool,
    ) -> CABACBDirectMacroblockResult:
        if not isinstance(input, CABACBDirectMacroblockInput) or input.slice_type % 5 != 1:
            raise CABACError("B_Direct requires a B-slice macroblock input")
        if builder is None:
            raise CABACError("B_Direct frame builder is required")
        if (
            any(
                not isinstance(value, int)
                or isinstance(value, bool)
                or value < 0
                for value in (input.macroblock_x, input.macroblock_y)
            )
            or not isinstance(input.direct_spatial, bool)
            or not isinstance(input.current_picture_order_count, int)
            or isinstance(input.current_picture_order_count, bool)
            or not isinstance(input.colocated_reference_index, int)
            or isinstance(input.colocated_reference_index, bool)
            or (
                input.colocated_reference_picture_order_count is not None
                and (
                    not isinstance(input.colocated_reference_picture_order_count, int)
                    or isinstance(input.colocated_reference_picture_order_count, bool)
                )
            )
            or not isinstance(input.colocated_motion_vector, tuple)
            or len(input.colocated_motion_vector) != 2
            or any(
                not isinstance(value, int) or isinstance(value, bool)
                for value in input.colocated_motion_vector
            )
            or not isinstance(input.colocated_motion_field, tuple)
            or len(input.colocated_motion_field) not in (0, 16)
        ):
            raise CABACError("B_Direct macroblock metadata is invalid")

        from .h264_reconstruction import (
            MotionVectorCandidate,
            Yuv420Frame,
            predict_motion_vector,
        )

        list0 = input.list0_references
        list1 = input.list1_references
        if (
            not isinstance(list0, tuple)
            or not isinstance(list1, tuple)
            or not list0
            or not list1
            or any(
                not isinstance(reference, CABACBReferencePicture)
                or not isinstance(reference.frame, Yuv420Frame)
                or not isinstance(reference.picture_order_count, int)
                or isinstance(reference.picture_order_count, bool)
                or not isinstance(reference.long_term, bool)
                for reference in (*list0, *list1)
            )
        ):
            raise CABACError("B_Direct reference lists are invalid or empty")
        reference_frames = [reference.frame for reference in (*list0, *list1)]
        dimensions = {(frame.width, frame.height) for frame in reference_frames}
        if len(dimensions) != 1 or any(frame.width <= 0 or frame.height <= 0 for frame in reference_frames):
            raise CABACError("B_Direct reference picture dimensions do not match")

        def candidate_index(candidate) -> int:
            if candidate is None:
                return -1
            if not isinstance(candidate, MotionVectorCandidate):
                raise CABACError("B_Direct neighbor motion metadata is invalid")
            return candidate.reference_index if candidate.reference_index is not None else -1

        if input.direct_spatial:
            def minimum_positive(left: int, right: int) -> int:
                return min(left, right) if left >= 0 and right >= 0 else max(left, right)

            def minimum_nonnegative(values: tuple[int, int, int]) -> int:
                return minimum_positive(values[0], minimum_positive(values[1], values[2]))

            neighbor_sets = (
                (
                    input.left_l0_candidate,
                    input.top_l0_candidate,
                    input.top_right_l0_candidate
                    if input.top_right_l0_candidate is not None
                    else input.top_left_l0_candidate,
                ),
                (
                    input.left_l1_candidate,
                    input.top_l1_candidate,
                    input.top_right_l1_candidate
                    if input.top_right_l1_candidate is not None
                    else input.top_left_l1_candidate,
                ),
            )
            reference_indices = tuple(
                minimum_nonnegative(tuple(candidate_index(c) for c in candidates))
                for candidates in neighbor_sets
            )
            direct_zero = reference_indices == (-1, -1)
            if direct_zero:
                reference_indices = (0, 0)
            colocated_zero = (
                not list1[0].long_term
                and input.colocated_reference_index == 0
                and all(-1 <= value <= 1 for value in input.colocated_motion_vector)
            )
            vectors = []
            for ref_index, candidates in zip(reference_indices, neighbor_sets):
                if direct_zero or ref_index < 0 or (ref_index == 0 and colocated_zero):
                    vectors.append((0, 0))
                    continue
                if ref_index >= len(list0 if len(vectors) == 0 else list1):
                    raise CABACError("B_Direct spatial reference index is unavailable")
                left, top, top_right = candidates
                predicted = predict_motion_vector(
                    ref_index, left, top, top_right, input.top_left_l0_candidate
                    if len(vectors) == 0
                    else input.top_left_l1_candidate,
                )
                vectors.append((predicted.x, predicted.y))
            motion_vectors = (vectors[0], vectors[1])
        else:
            col_index = input.colocated_reference_index
            if col_index < 0:
                ref_index_l0 = 0
                colocated_motion = (0, 0)
            else:
                col_poc = input.colocated_reference_picture_order_count
                matching_indices = [
                    index
                    for index, reference in enumerate(list0)
                    if reference.picture_order_count == col_poc
                ]
                if not matching_indices:
                    raise CABACError("B_Direct co-located reference picture is unavailable in list0")
                ref_index_l0 = matching_indices[0]
                colocated_motion = input.colocated_motion_vector
            ref0, ref1 = list0[ref_index_l0], list1[0]
            td = max(-128, min(127, ref1.picture_order_count - ref0.picture_order_count))
            if ref0.long_term or td == 0:
                motion_l0 = colocated_motion
                motion_l1 = (0, 0)
            else:
                tb = max(
                    -128,
                    min(127, input.current_picture_order_count - ref0.picture_order_count),
                )
                tx = int((16384 + abs(int(td / 2))) / td)
                scale = max(-1024, min(1023, (tb * tx + 32) >> 6))
                motion_l0 = tuple((scale * value + 128) >> 8 for value in colocated_motion)
                motion_l1 = tuple(
                    value - col_value for value, col_value in zip(motion_l0, colocated_motion)
                )
            reference_indices = (ref_index_l0, 0)
            motion_vectors = (motion_l0, motion_l1)

        prediction_flags = tuple(index >= 0 for index in reference_indices)
        selected = []
        for enabled, ref_index, references in zip(
            prediction_flags, reference_indices, (list0, list1)
        ):
            if enabled:
                if ref_index >= len(references):
                    raise CABACError("B_Direct selected reference index is unavailable")
                selected.append(references[ref_index].frame)
            else:
                selected.append(None)
        if not any(prediction_flags):
            raise CABACError("B_Direct has no active prediction list")

        samples = _reconstruct_or_defer(
            "bdirect",
            tuple(selected),
            (motion_vectors, input.macroblock_x, input.macroblock_y),
            getattr(self, "_timeout_check", None),
        )
        if samples is not None:
            builder.place_macroblock(address, *samples)
        luma, cb, cr = samples or _PLACEHOLDER_SAMPLES
        return CABACBDirectMacroblockResult(
            skipped,
            reference_indices,
            motion_vectors,
            prediction_flags,
            tuple(luma),
            tuple(cb),
            tuple(cr),
        )

    def decode_reference_index(
        self,
        max_ref_idx_minus1: int,
        neighbor_context_increment: int,
        contexts: list[CABACContextModel],
    ) -> int:
        if getattr(self, "_cavlc", None) is not None:
            return CAVLCSyntax.reference(self._bits, max_ref_idx_minus1)
        """Decode unary ref_idx_l0/l1 using a six-context offset bank."""
        if (
            not isinstance(max_ref_idx_minus1, int)
            or isinstance(max_ref_idx_minus1, bool)
            or not 0 <= max_ref_idx_minus1 <= 31
        ):
            raise CABACError("ref_idx maximum is outside [0,31]")
        if (
            not isinstance(neighbor_context_increment, int)
            or isinstance(neighbor_context_increment, bool)
            or not 0 <= neighbor_context_increment <= 3
        ):
            raise CABACError("ref_idx neighbor context increment is outside [0,3]")
        if not isinstance(contexts, list) or len(contexts) != 6 or any(
            not isinstance(model, CABACContextModel) or not 0 <= model._state_index < 64
            for model in contexts
        ):
            raise CABACError("ref_idx requires six valid consecutive context models")
        if len({id(model) for model in contexts}) != 6:
            raise CABACError("ref_idx contexts must be distinct")
        if max_ref_idx_minus1 == 0:
            return 0

        trial_bits = BitReader(self._bits._data)
        trial_bits._bit_offset = self._bits._bit_offset
        trial = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
        trial._bits = trial_bits
        trial._code_range = self._code_range
        trial._code_offset = self._code_offset
        trial._terminated = self._terminated
        trial._cavlc = self._cavlc.clone() if getattr(self, "_cavlc", None) is not None else None
        trial_contexts = [CABACContextModel.__new__(CABACContextModel) for _ in range(6)]
        for target, source in zip(trial_contexts, contexts):
            target._state_index = source._state_index
            target._value_mps = source._value_mps

        ref_idx = 0
        if trial.decode_bin(trial_contexts[neighbor_context_increment]):
            ref_idx = 1
            while True:
                context_index = 4 if ref_idx == 1 else 5
                if not trial.decode_bin(trial_contexts[context_index]):
                    break
                if ref_idx >= max_ref_idx_minus1:
                    raise CABACError("ref_idx exceeds the active reference maximum")
                ref_idx += 1

        self._bits._bit_offset = trial._bits._bit_offset
        self._code_range = trial._code_range
        self._code_offset = trial._code_offset
        self._terminated = trial._terminated
        self._cavlc = getattr(trial, "_cavlc", None)
        for target, source in zip(contexts, trial_contexts):
            target._state_index = source._state_index
            target._value_mps = source._value_mps
        return ref_idx

    def decode_reference_index_for_partition(
        self,
        max_ref_idx_minus1: int,
        left: CABACInterNeighbor,
        top: CABACInterNeighbor,
        contexts: list[CABACContextModel],
        mbaff_frame: bool = False,
        current_is_field: bool = False,
    ) -> int:
        increment = derive_cabac_reference_index_context_increment(
            left, top, mbaff_frame, current_is_field
        )
        return self.decode_reference_index(max_ref_idx_minus1, increment, contexts)

    def decode_i_intra_mb_type(
        self,
        slice_type: int,
        contexts: list[CABACContextModel],
        left_available: bool,
        left_intra16_or_pcm: bool,
        top_available: bool,
        top_intra16_or_pcm: bool,
    ) -> int:
        if getattr(self, "_cavlc", None) is not None:
            return self._cavlc.mb_type(self._bits, slice_type)
        if slice_type > 9 or slice_type % 5 != 2:
            raise CABACError("I-slice mb_type is unsupported for this slice type")
        if not isinstance(contexts, list) or len(contexts) != 8 or any(
            not isinstance(model, CABACContextModel) or not 0 <= model._state_index < 64
            for model in contexts
        ):
            raise CABACError("I-slice mb_type requires context models 3 through 10")
        if len({id(model) for model in contexts}) != 8:
            raise CABACError("I-slice mb_type context models must be distinct")

        trial_bits = BitReader(self._bits._data)
        trial_bits._bit_offset = self._bits._bit_offset
        trial = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
        trial._bits = trial_bits
        trial._code_range = self._code_range
        trial._code_offset = self._code_offset
        trial._terminated = self._terminated
        trial._cavlc = self._cavlc.clone() if getattr(self, "_cavlc", None) is not None else None
        trial_contexts = [CABACContextModel.__new__(CABACContextModel) for _ in range(8)]
        for target, source in zip(trial_contexts, contexts):
            target._state_index = source._state_index
            target._value_mps = source._value_mps

        first_context = int(left_available and left_intra16_or_pcm)
        first_context += int(top_available and top_intra16_or_pcm)
        if not trial.decode_bin(trial_contexts[first_context]):
            mb_type = 0
        elif trial.decode_terminate_bin():
            mb_type = 25
        else:
            mb_type = 1 + 12 * int(trial.decode_bin(trial_contexts[3]))
            if trial.decode_bin(trial_contexts[4]):
                mb_type += 4 + 4 * int(trial.decode_bin(trial_contexts[5]))
            mb_type += 2 * int(trial.decode_bin(trial_contexts[6]))
            mb_type += int(trial.decode_bin(trial_contexts[7]))

        self._bits._bit_offset = trial._bits._bit_offset
        self._code_range = trial._code_range
        self._code_offset = trial._code_offset
        self._terminated = trial._terminated
        self._cavlc = getattr(trial, "_cavlc", None)
        for target, source in zip(contexts, trial_contexts):
            target._state_index = source._state_index
            target._value_mps = source._value_mps
        return mb_type

    def decode_intra_nxn_4x4_luma_macroblock(
        self,
        contexts: list[CABACContextModel],
        coded_block_pattern_luma: int,
        qpy: int,
        scaling_list: Sequence[int],
        top_modes: Sequence[int],
        left_modes: Sequence[int],
        top_mode_available: bool,
        left_mode_available: bool,
        top_edge: CABACIntra4x4EdgeState,
        left_edge: CABACIntra4x4EdgeState,
        blocks: Sequence[object],
    ) -> Intra4x4LumaMacroblockResult:
        """Decode Intra_NxN 4x4 modes/CBFs/luma residuals and reconstruct its luma plane.

        Sample references for each block are caller-gathered; external modes and CBF
        facts describe the top and left macroblocks.
        """
        _validate_slice_contexts(contexts)
        if (
            not isinstance(coded_block_pattern_luma, int)
            or isinstance(coded_block_pattern_luma, bool)
            or not 0 <= coded_block_pattern_luma <= 15
            or len(blocks) != 16
            or len(top_modes) != 4
            or len(left_modes) != 4
            or not isinstance(top_edge, CABACIntra4x4EdgeState)
            or not isinstance(left_edge, CABACIntra4x4EdgeState)
            or len(top_edge.transform_block_available) != 4
            or len(top_edge.transform_block_coded) != 4
            or len(left_edge.transform_block_available) != 4
            or len(left_edge.transform_block_coded) != 4
        ):
            raise CABACError("Intra_NxN 4x4 luma macroblock inputs are invalid")
        from .h264_reconstruction import (
            LumaIntra4x4Block,
            reconstruct_luma4x4_residual,
            reconstruct_luma_intra4x4_macroblock,
        )

        if any(not isinstance(block, LumaIntra4x4Block) for block in blocks):
            raise CABACError("Intra_NxN 4x4 luma macroblock blocks are invalid")
        trial, trial_contexts = self._slice_trial(contexts)
        modes = trial.decode_intra4x4_pred_modes(
            trial_contexts[68:70],
            top_modes,
            left_modes,
            top_mode_available,
            left_mode_available,
        )
        coded_flags = [False] * 16
        residuals = [[0] * 16 for _ in range(16)]
        reconstructed_blocks = []
        for block_index, block in enumerate(blocks):
            raster_index = _LUMA4X4_BLOCK_SCAN_TO_RASTER[block_index]
            block_x, block_y = raster_index % 4, raster_index // 4
            cbp_index = (block_y // 2) * 2 + block_x // 2
            if coded_block_pattern_luma & (1 << cbp_index):
                left_cond = _intra4x4_luma_cond_term(
                    block_index, block_x, block_y, True, coded_flags, left_edge
                )
                top_cond = _intra4x4_luma_cond_term(
                    block_index, block_x, block_y, False, coded_flags, top_edge
                )
                scan_levels, coded = trial.decode_residual_block(
                    0, left_cond, top_cond, trial_contexts
                )
                coded_flags[block_index] = coded
                if coded:
                    residuals[block_index] = reconstruct_luma4x4_residual(
                        place_luma4x4_scan_levels(scan_levels), scaling_list, qpy
                    )
            reconstructed_blocks.append(
                LumaIntra4x4Block(
                    mode=modes[block_index],
                    residual=residuals[block_index],
                    top=block.top,
                    left=block.left,
                    top_left=block.top_left,
                )
            )
        samples = reconstruct_luma_intra4x4_macroblock(reconstructed_blocks)
        self._commit_slice_trial(trial, contexts, trial_contexts)
        return Intra4x4LumaMacroblockResult(
            tuple(modes),
            tuple(coded_flags),
            tuple(tuple(residual) for residual in residuals),
            tuple(samples),
        )

    def decode_intra_nxn_8x8_luma_macroblock(
        self,
        contexts: list[CABACContextModel],
        transform_8x8_mode_enabled: bool,
        left_has_8x8_transform: bool,
        top_has_8x8_transform: bool,
        coded_block_pattern_luma: int,
        qpy: int,
        scaling_list: Sequence[int],
        top_modes: Sequence[int],
        left_modes: Sequence[int],
        top_mode_available: bool,
        left_mode_available: bool,
        blocks: Sequence[object],
    ) -> Intra8x8LumaMacroblockResult:
        """Decode an 8x8-transform Intra_NxN macroblock and reconstruct luma.

        Block sample references are gathered and filtered by the caller per
        clause 8.3.2.2.1. This entry point handles the CABAC flag/modes and residuals.
        """
        _validate_slice_contexts(contexts)
        if (
            not transform_8x8_mode_enabled
            or not isinstance(coded_block_pattern_luma, int)
            or isinstance(coded_block_pattern_luma, bool)
            or not 0 <= coded_block_pattern_luma <= 15
            or len(top_modes) != 2
            or len(left_modes) != 2
            or len(blocks) != 4
        ):
            raise CABACError("Intra_NxN 8x8 macroblock inputs are unsupported or invalid")
        from .h264_reconstruction import (
            LumaIntra8x8Block,
            inverse_scale_luma8x8,
            inverse_transform_luma8x8,
            reconstruct_luma_intra8x8_macroblock,
        )

        if any(not isinstance(block, LumaIntra8x8Block) for block in blocks):
            raise CABACError("Intra_NxN 8x8 macroblock blocks are invalid")
        inverse_scale_luma8x8([0] * 64, scaling_list, qpy)
        trial, trial_contexts = self._slice_trial(contexts)
        transform_contexts = trial_contexts[399:402]
        transform_size_8x8 = trial.decode_transform_size_8x8_flag(
            transform_contexts, left_has_8x8_transform, top_has_8x8_transform
        )
        if not transform_size_8x8:
            raise CABACError("Intra_NxN 8x8 residual path requires transform_size_8x8_flag")

        mode_contexts = trial_contexts[68:70]
        modes = []
        for block_index in range(4):
            column, row = block_index % 2, block_index // 2
            if column > 0:
                left_mode = modes[block_index - 1]
                left_available = True
            else:
                left_mode = left_modes[row]
                left_available = left_mode_available
            if row > 0:
                top_mode = modes[block_index - 2]
                top_available = True
            else:
                top_mode = top_modes[column]
                top_available = top_mode_available
            predicted_mode = min(left_mode, top_mode) if left_available and top_available else 2
            modes.append(trial.decode_intra4x4_pred_mode(predicted_mode, mode_contexts))

        residuals = [[0] * 64 for _ in range(4)]
        nonzero_8x8 = [False] * 4
        reconstructed_blocks = []
        for block_index, block in enumerate(blocks):
            if coded_block_pattern_luma & (1 << block_index):
                scan_levels = trial.decode_luma8x8_residual_block(trial_contexts)
                nonzero_8x8[block_index] = any(scan_levels)
                raster_levels = [0] * 64
                for scan_index, raster_index in enumerate(_LUMA8X8_SCAN_TO_RASTER):
                    raster_levels[raster_index] = scan_levels[scan_index]
                scaled = inverse_scale_luma8x8(raster_levels, scaling_list, qpy)
                residuals[block_index] = inverse_transform_luma8x8(scaled)
            reconstructed_blocks.append(
                LumaIntra8x8Block(
                    mode=modes[block_index],
                    residual=residuals[block_index],
                    top=block.top,
                    left=block.left,
                    top_left=block.top_left,
                    references_filtered=block.references_filtered,
                )
            )
        samples = reconstruct_luma_intra8x8_macroblock(reconstructed_blocks)
        self._commit_slice_trial(trial, contexts, trial_contexts)
        return Intra8x8LumaMacroblockResult(
            True,
            tuple(modes),
            tuple(tuple(residual) for residual in residuals),
            tuple(samples),
            tuple(nonzero_8x8),
        )

    def decode_intra16x16_luma_macroblock(
        self,
        mb_type: int,
        contexts: list[CABACContextModel],
        qpy: int,
        scaling_list: Sequence[int],
        top: Sequence[int] | None,
        left: Sequence[int] | None,
        top_left: int,
        top_left_available: bool,
        top_edge: CABACIntra16x16EdgeState,
        left_edge: CABACIntra16x16EdgeState,
    ) -> Intra16x16LumaMacroblockResult:
        """Decode I16x16 luma DC/AC residual syntax and reconstruct one luma macroblock.

        I16x16 mb_type/QPY and prediction references are provided by the enclosing parser.
        """
        _validate_slice_contexts(contexts)
        if not isinstance(mb_type, int) or isinstance(mb_type, bool) or not 1 <= mb_type <= 24:
            raise CABACError("I16x16 mb_type is outside [1,24]")
        if not isinstance(top_edge, CABACIntra16x16EdgeState) or not isinstance(left_edge, CABACIntra16x16EdgeState):
            raise CABACError("I16x16 edge state is invalid")
        from .h264_reconstruction import (
            inverse_scale_luma4x4,
            inverse_transform_luma4x4,
            reconstruct_intra16x16_luma_dc,
            reconstruct_luma_intra16x16_macroblock,
        )

        reconstruct_intra16x16_luma_dc([0] * 16, scaling_list, qpy)
        trial, trial_contexts = self._slice_trial(contexts)
        prediction_mode = (mb_type - 1) % 4
        cbp_luma = 15 if mb_type >= 13 else 0
        dc_cond_left = derive_coded_block_flag_cond_term(
            left_edge.available,
            True,
            left_edge.is_ipcm,
            left_edge.dc_transform_block_available,
            left_edge.dc_transform_block_coded,
        )
        dc_cond_top = derive_coded_block_flag_cond_term(
            top_edge.available,
            True,
            top_edge.is_ipcm,
            top_edge.dc_transform_block_available,
            top_edge.dc_transform_block_coded,
        )
        dc_scan, dc_coded = trial.decode_residual_block(
            0, dc_cond_left, dc_cond_top, trial_contexts
        )
        dc_levels = (
            reconstruct_intra16x16_luma_dc(
                place_luma4x4_scan_levels(dc_scan), scaling_list, qpy
            )
            if dc_coded
            else [0] * 16
        )

        coded_flags = [False] * 16
        residual = [0] * 256
        for block_index in range(16):
            raster_index = _LUMA4X4_BLOCK_SCAN_TO_RASTER[block_index]
            block_x, block_y = raster_index % 4, raster_index // 4
            cbp_index = (block_y // 2) * 2 + block_x // 2
            ac_levels = [0] * 15
            if cbp_luma & (1 << cbp_index):
                cond_left = _intra16x16_ac_cond_term(
                    block_index, block_x, block_y, True, coded_flags, left_edge
                )
                cond_top = _intra16x16_ac_cond_term(
                    block_index, block_x, block_y, False, coded_flags, top_edge
                )
                decoded_levels, coded = trial.decode_residual_block(
                    1, cond_left, cond_top, trial_contexts
                )
                coded_flags[block_index] = coded
                if coded:
                    ac_levels = decoded_levels[:15]

            if cbp_luma & (1 << cbp_index) and coded_flags[block_index]:
                raster_levels = place_luma4x4_scan_levels([0, *ac_levels])
                raster_levels[0] = 0
                coefficients = inverse_scale_luma4x4(raster_levels, scaling_list, qpy)
            else:
                coefficients = inverse_scale_luma4x4([0] * 16, scaling_list, qpy)
            coefficients[0] = dc_levels[raster_index]
            block_residual = inverse_transform_luma4x4(coefficients)
            for row in range(4):
                destination = (block_y * 4 + row) * 16 + block_x * 4
                residual[destination : destination + 4] = block_residual[row * 4 : row * 4 + 4]

        samples = reconstruct_luma_intra16x16_macroblock(
            prediction_mode, top, left, top_left, top_left_available, residual
        )
        self._commit_slice_trial(trial, contexts, trial_contexts)
        return Intra16x16LumaMacroblockResult(
            prediction_mode,
            cbp_luma,
            dc_coded,
            tuple(dc_levels),
            tuple(coded_flags),
            tuple(residual),
            tuple(samples),
        )

    def decode_intra_chroma420_macroblock(
        self,
        contexts: list[CABACContextModel],
        mode: int,
        mode_already_decoded: bool,
        mb_type: int,
        intra16x16: bool,
        left_mode_nonzero: bool,
        top_mode_nonzero: bool,
        coded_block_pattern_chroma: int,
        qpy: int,
        qp_offsets: tuple[int, int],
        scaling_lists: tuple[Sequence[int], Sequence[int]],
        references: tuple[CABACChroma420References, CABACChroma420References],
        left_edges: tuple[CABACChroma420EdgeState, CABACChroma420EdgeState],
        top_edges: tuple[CABACChroma420EdgeState, CABACChroma420EdgeState],
    ) -> IntraChroma420MacroblockResult:
        """Decode/reconstruct 8-bit 4:2:0 intra chroma; neighbor/reference facts are supplied."""
        _validate_slice_contexts(contexts)
        if intra16x16:
            if not isinstance(mb_type, int) or isinstance(mb_type, bool) or not 1 <= mb_type <= 24:
                raise CABACError("I16x16 mb_type is outside [1,24]")
            coded_block_pattern_chroma = ((mb_type - 1) // 4) % 3
        elif mb_type != 0 or coded_block_pattern_chroma not in (0, 1, 2):
            raise CABACError("chroma coded block pattern is outside [0,2]")
        if mode_already_decoded:
            if not isinstance(mode, int) or isinstance(mode, bool) or not 0 <= mode <= 3:
                raise CABACError("chroma prediction mode is outside [0,3]")
        elif mode != 0:
            raise CABACError("undecoded chroma prediction mode must be zero")
        if any(len(values) != 2 for values in (qp_offsets, scaling_lists, references, left_edges, top_edges)):
            raise CABACError("chroma component inputs must contain Cb and Cr state")
        if any(
            not isinstance(reference, CABACChroma420References)
            or len(reference.top) != 8
            or len(reference.left) != 8
            or any(not isinstance(sample, int) or isinstance(sample, bool) or not 0 <= sample <= 255 for sample in (*reference.top, *reference.left, reference.top_left))
            for reference in references
        ):
            raise CABACError("chroma prediction references are invalid")
        if any(
            not isinstance(edge, CABACChroma420EdgeState)
            or len(edge.ac_block_available) != 2
            or len(edge.ac_block_coded) != 2
            for edge in (*left_edges, *top_edges)
        ):
            raise CABACError("chroma coded-block edge state is invalid")

        from .h264_reconstruction import (
            assemble_chroma420_residual_macroblock,
            derive_chroma_qpc,
            inverse_scale_chroma_dc2x2,
            inverse_transform_chroma_dc2x2,
            predict_chroma_intra8x8,
            reconstruct_chroma420_macroblock,
            reconstruct_chroma4x4_residual,
        )

        trial, trial_contexts = self._slice_trial(contexts)
        if not mode_already_decoded:
            mode = trial.decode_intra_chroma_pred_mode(
                trial_contexts[64:68], left_mode_nonzero, top_mode_nonzero
            )

        qpc_values: list[int] = []
        planes: list[
            tuple[bool, tuple[bool, ...], tuple[int, ...], tuple[int, ...]]
        ] = []
        component_dc_samples = [[0] * 4 for _ in range(2)]
        component_dc_coded = [False, False]
        for component in range(2):
            qpc = derive_chroma_qpc(qpy, qp_offsets[component])
            qpc_values.append(qpc)
            if coded_block_pattern_chroma:
                left, top = left_edges[component], top_edges[component]
                cond_left = derive_coded_block_flag_cond_term(
                    left.available, True, left.is_ipcm,
                    left.dc_block_available, left.dc_block_coded,
                )
                cond_top = derive_coded_block_flag_cond_term(
                    top.available, True, top.is_ipcm,
                    top.dc_block_available, top.dc_block_coded,
                )
                dc_levels, dc_coded = trial.decode_residual_block(
                    3, cond_left, cond_top, trial_contexts
                )
                component_dc_coded[component] = dc_coded
                if dc_coded:
                    component_dc_samples[component] = inverse_scale_chroma_dc2x2(
                        inverse_transform_chroma_dc2x2(dc_levels[:4]), qpc
                    )

        for component in range(2):
            qpc = qpc_values[component]
            dc_samples = component_dc_samples[component]
            residual_blocks = []
            ac_flags = [False] * 4
            for block_index in range(4):
                block_x, block_y = block_index % 2, block_index // 2
                ac_levels = [0] * 15
                if coded_block_pattern_chroma == 2:
                    for is_left, edge in (
                        (True, left_edges[component]),
                        (False, top_edges[component]),
                    ):
                        neighbor_coordinate = block_x if is_left else block_y
                        edge_coordinate = block_y if is_left else block_x
                        if neighbor_coordinate:
                            neighbor_index = block_index - (1 if is_left else 2)
                            cond_term = derive_coded_block_flag_cond_term(
                                True, True, False, True, ac_flags[neighbor_index]
                            )
                        elif not edge.available or edge.is_ipcm:
                            cond_term = True
                        else:
                            cond_term = derive_coded_block_flag_cond_term(
                                True, True, False,
                                edge.ac_block_available[edge_coordinate],
                                edge.ac_block_coded[edge_coordinate],
                            )
                        if is_left:
                            cond_left = cond_term
                        else:
                            cond_top = cond_term
                    decoded_levels, ac_coded = trial.decode_residual_block(
                        4, cond_left, cond_top, trial_contexts
                    )
                    ac_flags[block_index] = ac_coded
                    if ac_coded:
                        ac_levels = decoded_levels[:15]
                residual_blocks.append(
                    reconstruct_chroma4x4_residual(
                        dc_samples[block_index], ac_levels,
                        scaling_lists[component], qpc,
                    )
                )

            residual = assemble_chroma420_residual_macroblock(residual_blocks)
            reference = references[component]
            prediction = predict_chroma_intra8x8(
                mode,
                reference.top if reference.top_available else None,
                reference.left if reference.left_available else None,
                reference.top_left if reference.top_left_available else None,
            )
            samples = reconstruct_chroma420_macroblock(prediction, residual)
            planes.append((component_dc_coded[component], tuple(ac_flags), tuple(residual), tuple(samples)))

        self._commit_slice_trial(trial, contexts, trial_contexts)
        return IntraChroma420MacroblockResult(
            mode,
            tuple(qpc_values),
            (planes[0][0], planes[1][0]),
            (planes[0][1], planes[1][1]),
            planes[0][3],
            planes[1][3],
            planes[0][2],
            planes[1][2],
        )

    def decode_i_intra_macroblock(
        self,
        input: CABACIIntraMacroblockInput,
        contexts: list[CABACContextModel],
        builder,
        address: int,
    ) -> CABACIIntraMacroblockResult:
        return self._decode_i_intra_macroblock_with_type(
            input, contexts, builder, address, None
        )

    def _decode_i_intra_macroblock_with_type(
        self,
        input: CABACIIntraMacroblockInput,
        contexts: list[CABACContextModel],
        builder,
        address: int,
        predecoded_type: int | None,
    ) -> CABACIIntraMacroblockResult:
        """Decode one I macroblock in syntax order; edge samples and neighbor facts are caller-gathered."""
        _validate_slice_contexts(contexts)
        if not isinstance(input, CABACIIntraMacroblockInput) or not 0 <= input.previous_qpy <= 51:
            raise CABACError("I macroblock input or previous QPY is invalid")
        if builder is None:
            raise CABACError("I macroblock frame builder is required")
        trial, trial_contexts = self._slice_trial(contexts)
        if predecoded_type is None:
            mb_type = trial.decode_i_intra_mb_type(
                input.slice_type,
                trial_contexts[3:11],
                input.left_available,
                input.left_intra16_or_pcm,
                input.top_available,
                input.top_intra16_or_pcm,
            )
        elif (
            not isinstance(predecoded_type, int)
            or isinstance(predecoded_type, bool)
            or not 0 <= predecoded_type <= 25
        ):
            raise CABACError("predecoded intra mb_type is outside [0,25]")
        else:
            mb_type = predecoded_type
        qpy = input.previous_qpy
        if mb_type == 25:
            while trial._bits._bit_offset % 8:
                if trial._bits.read_bit():
                    raise CABACError("I_PCM alignment bit is not zero")
            try:
                y_block = bytes(trial._bits.read_bits(8) for _ in range(256))
                cb_block = bytes(trial._bits.read_bits(8) for _ in range(64))
                cr_block = bytes(trial._bits.read_bits(8) for _ in range(64))
                offset = trial._bits.read_bits(9) if trial._cavlc is None else 0
            except BitstreamError as error:
                raise CABACError(f"truncated I_PCM macroblock: {error}") from error
            if offset >= 510:
                raise CABACError("I_PCM CABAC offset is outside the arithmetic range")
            builder.place_macroblock(address, y_block, cb_block, cr_block)
            trial._code_range = 510
            trial._code_offset = offset
            trial._terminated = False
            self._commit_slice_trial(trial, contexts, trial_contexts)
            return CABACIIntraMacroblockResult(
                macroblock_type=mb_type,
                coded_block_pattern_luma=0,
                coded_block_pattern_chroma=0,
                transform_size_8x8=False,
                intra16x16_luma_mode=0,
                chroma_prediction_mode=0,
                luma_4x4_modes=(),
                luma_8x8_modes=(),
                qp_delta=0,
                qpy=qpy,
                luma=tuple(y_block),
                cb=tuple(cb_block),
                cr=tuple(cr_block),
                luma_4x4_coded_block_flags=(),
                chroma_dc_coded=(False, False),
                chroma_ac_coded_flags=((False,) * 4, (False,) * 4),
            )

        from .h264_reconstruction import (
            LumaIntra4x4Block,
            LumaIntra8x8Block,
            reconstruct_luma_intra4x4_macroblock,
            reconstruct_luma_intra8x8_macroblock,
        )

        cbp_luma = 0
        cbp_chroma = 0
        transform_8x8 = False
        modes_4x4: list[int] = []
        modes_8x8: list[int] = []
        chroma_mode = 0
        intra16x16_luma_mode = 0
        if mb_type == 0:
            if input.transform_8x8_mode_enabled:
                transform_8x8 = trial.decode_transform_size_8x8_flag(
                    trial_contexts[399:402],
                    input.left_has_8x8_transform,
                    input.top_has_8x8_transform,
                )
            mode_contexts = trial_contexts[68:70]
            if transform_8x8:
                for block_index in range(4):
                    block_x, block_y = block_index % 2, block_index // 2
                    left_available, top_available = input.left_mode_available, input.top_mode_available
                    left_mode, top_mode = input.left_modes_8x8[block_y], input.top_modes_8x8[block_x]
                    if block_x:
                        left_available, left_mode = True, modes_8x8[block_index - 1]
                    if block_y:
                        top_available, top_mode = True, modes_8x8[block_index - 2]
                    predicted_mode = min(left_mode, top_mode) if left_available and top_available else 2
                    modes_8x8.append(trial.decode_intra4x4_pred_mode(predicted_mode, mode_contexts))
            else:
                modes_4x4 = trial.decode_intra4x4_pred_modes(
                    mode_contexts,
                    input.top_modes_4x4,
                    input.left_modes_4x4,
                    input.top_mode_available,
                    input.left_mode_available,
                )
            chroma_mode = trial.decode_intra_chroma_pred_mode(
                trial_contexts[64:68],
                input.chroma_left_mode_nonzero,
                input.chroma_top_mode_nonzero,
            )
            cbp_luma = trial.decode_luma_coded_block_pattern(
                input.left_luma_cbp, input.top_luma_cbp, trial_contexts[73:77]
            )
            cbp_chroma = trial.decode_chroma_coded_block_pattern(
                input.left_chroma_cbp, input.top_chroma_cbp, trial_contexts[77:85]
            )
        else:
            cbp_luma = 15 if mb_type >= 13 else 0
            cbp_chroma = ((mb_type - 1) // 4) % 3
            intra16x16_luma_mode = (mb_type - 1) % 4
            chroma_mode = trial.decode_intra_chroma_pred_mode(
                trial_contexts[64:68],
                input.chroma_left_mode_nonzero,
                input.chroma_top_mode_nonzero,
            )

        qp_delta = 0
        if cbp_luma or cbp_chroma or mb_type != 0:
            qp_delta = trial.decode_mb_qp_delta(trial_contexts[60:64], input.previous_qp_delta)
            qpy = (qpy + qp_delta + 52) % 52

        luma_samples: Sequence[int]
        luma_coded_flags: tuple[bool, ...] = ()
        luma_dc_coded = False
        luma_ac_coded_flags: tuple[bool, ...] = ()
        luma_8x8_coded_flags = [False] * 4
        luma_8x8_nonzero = [False] * 4
        if mb_type == 0 and transform_8x8:
            if len(input.luma_8x8_blocks) != 4:
                raise CABACError("Intra_8x8 macroblock requires four gathered blocks")
            residuals = [[0] * 64 for _ in range(4)]
            blocks = []
            for block_index, block in enumerate(input.luma_8x8_blocks):
                if cbp_luma & (1 << block_index):
                    luma_8x8_coded_flags[block_index] = True
                    levels = trial.decode_luma8x8_residual_block(trial_contexts)
                    luma_8x8_nonzero[block_index] = any(levels)
                    raster = [0] * 64
                    for scan_index, raster_index in enumerate(_LUMA8X8_SCAN_TO_RASTER):
                        raster[raster_index] = levels[scan_index]
                    from .h264_reconstruction import inverse_scale_luma8x8, inverse_transform_luma8x8

                    residuals[block_index] = inverse_transform_luma8x8(
                        inverse_scale_luma8x8(raster, input.luma_8x8_scaling_list, qpy)
                    )
                blocks.append(
                    LumaIntra8x8Block(
                        modes_8x8[block_index], residuals[block_index],
                        top=block.top, left=block.left, top_left=block.top_left,
                        references_filtered=block.references_filtered,
                    )
                )
            luma_samples = reconstruct_luma_intra8x8_macroblock(blocks)
        elif mb_type == 0:
            if len(input.luma_4x4_blocks) != 16:
                raise CABACError("Intra_4x4 macroblock requires sixteen gathered blocks")
            coded_flags = [False] * 16
            blocks = []
            for block_index, block in enumerate(input.luma_4x4_blocks):
                raster_index = _LUMA4X4_BLOCK_SCAN_TO_RASTER[block_index]
                block_x, block_y = raster_index % 4, raster_index // 4
                residual = [0] * 16
                if cbp_luma & (1 << ((block_y // 2) * 2 + block_x // 2)):
                    cond_left = _intra4x4_luma_cond_term(
                        block_index, block_x, block_y, True, coded_flags, input.luma_4x4_left_edge
                    )
                    cond_top = _intra4x4_luma_cond_term(
                        block_index, block_x, block_y, False, coded_flags, input.luma_4x4_top_edge
                    )
                    levels, coded = trial.decode_residual_block(2, cond_left, cond_top, trial_contexts)
                    coded_flags[block_index] = coded
                    if coded:
                        from .h264_reconstruction import inverse_scale_luma4x4, inverse_transform_luma4x4

                        coefficients = inverse_scale_luma4x4(
                            place_luma4x4_scan_levels(levels), input.luma_4x4_scaling_list, qpy
                        )
                        residual = inverse_transform_luma4x4(coefficients)
                blocks.append(
                    LumaIntra4x4Block(
                        modes_4x4[block_index], residual,
                        top=block.top, left=block.left, top_left=block.top_left,
                    )
                )
            luma_samples = reconstruct_luma_intra4x4_macroblock(blocks)
            luma_coded_flags = tuple(coded_flags)
        else:
            luma_top = input.intra16x16_top if input.intra16x16_top_available else None
            luma_left = input.intra16x16_left if input.intra16x16_left_available else None
            luma_result = trial.decode_intra16x16_luma_macroblock(
                mb_type,
                trial_contexts,
                qpy,
                input.luma_4x4_scaling_list,
                luma_top,
                luma_left,
                input.intra16x16_top_left,
                input.intra16x16_top_left_available,
                input.luma_16x16_top_edge,
                input.luma_16x16_left_edge,
            )
            luma_samples = luma_result.samples
            luma_dc_coded = luma_result.dc_coded
            luma_ac_coded_flags = luma_result.ac_coded_block_flags

        chroma_result = trial.decode_intra_chroma420_macroblock(
            trial_contexts,
            chroma_mode,
            True,
            mb_type if mb_type != 0 else 0,
            mb_type != 0,
            input.chroma_left_mode_nonzero,
            input.chroma_top_mode_nonzero,
            cbp_chroma,
            qpy,
            input.chroma_qp_index_offsets,
            input.chroma_scaling_lists,
            input.chroma_references,
            input.chroma_left_edges,
            input.chroma_top_edges,
        )
        builder.place_macroblock(address, luma_samples, chroma_result.cb, chroma_result.cr)
        self._commit_slice_trial(trial, contexts, trial_contexts)
        return CABACIIntraMacroblockResult(
            macroblock_type=mb_type,
            coded_block_pattern_luma=cbp_luma,
            coded_block_pattern_chroma=cbp_chroma,
            transform_size_8x8=transform_8x8,
            intra16x16_luma_mode=intra16x16_luma_mode,
            chroma_prediction_mode=chroma_mode,
            luma_4x4_modes=tuple(modes_4x4),
            luma_8x8_modes=tuple(modes_8x8),
            qp_delta=qp_delta,
            qpy=qpy,
            luma=tuple(luma_samples),
            cb=chroma_result.cb,
            cr=chroma_result.cr,
            luma_4x4_coded_block_flags=luma_coded_flags,
            chroma_dc_coded=chroma_result.dc_coded,
            chroma_ac_coded_flags=chroma_result.ac_coded_block_flags,
            luma_dc_coded=luma_dc_coded,
            luma_ac_coded_block_flags=luma_ac_coded_flags,
            luma_8x8_coded_block_flags=tuple(luma_8x8_coded_flags),
            luma_8x8_nonzero=tuple(luma_8x8_nonzero),
        )

    def decode_ipcm_intra_macroblock(
        self,
        slice_type: int,
        contexts: list[CABACContextModel],
        left_available: bool,
        left_intra16_or_pcm: bool,
        top_available: bool,
        top_intra16_or_pcm: bool,
        builder,
        address: int,
    ) -> None:
        """Decode and place one 8-bit 4:2:0 I_PCM macroblock, then restart CABAC."""
        if not isinstance(contexts, list) or len(contexts) != 8:
            raise CABACError("I-slice mb_type requires context models 3 through 10")
        if len({id(model) for model in contexts}) != 8:
            raise CABACError("I-slice mb_type context models must be distinct")
        trial, trial_contexts = self._slice_trial(contexts)
        mb_type = trial.decode_i_intra_mb_type(
            slice_type,
            trial_contexts,
            left_available,
            left_intra16_or_pcm,
            top_available,
            top_intra16_or_pcm,
        )
        if mb_type != 25:
            raise CABACError("only I_PCM macroblocks are implemented by this entry point")

        while trial._bits._bit_offset % 8:
            if trial._bits.read_bit():
                raise CABACError("I_PCM alignment bit is not zero")
        try:
            y_block = bytes(trial._bits.read_bits(8) for _ in range(256))
            u_block = bytes(trial._bits.read_bits(8) for _ in range(64))
            v_block = bytes(trial._bits.read_bits(8) for _ in range(64))
        except BitstreamError as error:
            raise CABACError(f"truncated I_PCM samples: {error}") from error
        try:
            offset = trial._bits.read_bits(9) if trial._cavlc is None else 0
        except BitstreamError as error:
            raise CABACError(f"truncated I_PCM CABAC reinitialization: {error}") from error
        if offset >= self.INITIAL_RANGE:
            raise CABACError("CABAC initial offset is outside the arithmetic range")

        builder.place_macroblock(address, y_block, u_block, v_block)
        self._bits._bit_offset = trial._bits._bit_offset
        self._code_range = self.INITIAL_RANGE
        self._code_offset = offset
        self._terminated = False
        for target, source in zip(contexts, trial_contexts):
            target._state_index = source._state_index
            target._value_mps = source._value_mps

    def decode_inter_mb_type(
        self,
        slice_type: int,
        contexts: list[CABACContextModel],
        left_available: bool,
        top_available: bool,
        left_b_skip_or_direct: bool = False,
        top_b_skip_or_direct: bool = False,
    ) -> int:
        if getattr(self, "_cavlc", None) is not None:
            return self._cavlc.mb_type(self._bits, slice_type)
        """Decode P/SP mb_type (0-3, intra 5-30) or B mb_type (0-22, intra 23-48).

        Contexts are the whole-slice ctxIdx 0-459 list (Tables 9-37, 9-39, 9-41).
        B neighbor flags report available B_Skip/B_Direct_16x16 neighbors
        (clause 9.3.3.1.1.3); P slices ignore neighbors.
        """
        if not isinstance(slice_type, int) or not 0 <= slice_type <= 9 or slice_type % 5 not in (0, 1):
            raise CABACError("CABAC inter mb_type is unsupported for this slice type")
        _validate_slice_contexts(contexts)
        trial, trial_contexts = self._slice_trial(contexts)
        if slice_type % 5 == 0:
            mb_type = trial._decode_p_mb_type(trial_contexts)
        else:
            increment = int(bool(left_available) and not left_b_skip_or_direct)
            increment += int(bool(top_available) and not top_b_skip_or_direct)
            mb_type = trial._decode_b_mb_type(trial_contexts, increment)
        self._commit_slice_trial(trial, contexts, trial_contexts)
        return mb_type

    def _decode_p_mb_type(self, contexts: list[CABACContextModel]) -> int:
        if self.decode_bin(contexts[14]):
            return 5 + self._decode_intra_mb_type_suffix(contexts, 17)
        second = self.decode_bin(contexts[15])
        third = self.decode_bin(contexts[17 if second else 16])
        return 1 if second and third else 2 if second else 3 if third else 0

    def _decode_b_mb_type(self, contexts: list[CABACContextModel], increment: int) -> int:
        def bins(ctx_idx: int, count: int) -> int:
            value = 0
            for _ in range(count):
                value = (value << 1) | int(self.decode_bin(contexts[ctx_idx]))
            return value

        if not bins(27 + increment, 1):
            return 0
        if not bins(30, 1):
            return 1 + bins(32, 1)
        third = bins(31, 1)
        rest = bins(32, 3)
        if not third:
            return 3 + rest
        if rest == 0b101:
            return 23 + self._decode_intra_mb_type_suffix(contexts, 32)
        if rest == 0b110:
            return 11
        if rest == 0b111:
            return 22
        return 12 + (rest << 1) + bins(32, 1)

    def _decode_intra_mb_type_suffix(self, contexts: list[CABACContextModel], offset: int) -> int:
        """Decode the Table 9-36 suffix of an intra mb_type in P (17) or B (32) slices."""
        if not self.decode_bin(contexts[offset]):
            return 0
        if self.decode_terminate_bin():
            return 25
        mb_type = 1 + 12 * int(self.decode_bin(contexts[offset + 1]))
        if self.decode_bin(contexts[offset + 2]):
            mb_type += 4 + 4 * int(self.decode_bin(contexts[offset + 2]))
        mb_type += 2 * int(self.decode_bin(contexts[offset + 3]))
        return mb_type + int(self.decode_bin(contexts[offset + 3]))

    def decode_sub_mb_type(self, slice_type: int, contexts: list[CABACContextModel]) -> int:
        if getattr(self, "_cavlc", None) is not None:
            return CAVLCSyntax.unsigned(self._bits, 3 if slice_type % 5 == 0 else 12)
        """Decode sub_mb_type for P/SP (0-3, ctxIdx 21-23) or B (0-12, ctxIdx 36-39)."""
        if not isinstance(slice_type, int) or not 0 <= slice_type <= 9 or slice_type % 5 not in (0, 1):
            raise CABACError("CABAC sub_mb_type is unsupported for this slice type")
        _validate_slice_contexts(contexts)
        trial, trial_contexts = self._slice_trial(contexts)

        def bin_(ctx_idx: int) -> int:
            return int(trial.decode_bin(trial_contexts[ctx_idx]))

        if slice_type % 5 == 0:
            if bin_(21):
                sub_type = 0
            elif not bin_(22):
                sub_type = 1
            else:
                sub_type = 3 - bin_(23)
        elif not bin_(36):
            sub_type = 0
        elif not bin_(37):
            sub_type = 1 + bin_(39)
        elif not bin_(38):
            high = bin_(39)
            sub_type = 3 + 2 * high + bin_(39)
        elif bin_(39):
            sub_type = 11 + bin_(39)
        else:
            high = bin_(39)
            sub_type = 7 + 2 * high + bin_(39)
        self._commit_slice_trial(trial, contexts, trial_contexts)
        return sub_type

    def decode_inter_macroblock(
        self,
        input: CABACInterMacroblockInput,
        contexts: list[CABACContextModel],
        builder,
        address: int,
    ) -> CABACInterMacroblockResult:
        """Decode non-skip P/B partition syntax and motion-compensate its predictions."""
        return self._decode_inter_macroblock(input, None, contexts, builder, address)

    def decode_inter_macroblock_layer(
        self,
        input: CABACInterMacroblockInput,
        residual_input: CABACInterMacroblockResidualInput,
        contexts: list[CABACContextModel],
        builder,
        address: int,
    ) -> CABACInterMacroblockResult:
        """Decode, reconstruct, and place a complete inter macroblock including residuals."""
        if not isinstance(residual_input, CABACInterMacroblockResidualInput):
            raise CABACError("inter macroblock residual input is invalid")
        return self._decode_inter_macroblock(input, residual_input, contexts, builder, address)

    def decode_p_b_macroblock_layer(
        self,
        input: CABACPBMacroblockLayerInput,
        contexts: list[CABACContextModel],
        builder,
        address: int,
    ) -> CABACPBMacroblockLayerResult:
        """Dispatch mb_skip_flag, then complete skip or non-skip P/B reconstruction atomically."""
        _validate_slice_contexts(contexts)
        if not isinstance(input, CABACPBMacroblockLayerInput) or not isinstance(
            input.inter_input, CABACInterMacroblockInput
        ) or not isinstance(input.residual_input, CABACInterMacroblockResidualInput):
            raise CABACError("P/B macroblock-layer input is invalid")
        inter_input = input.inter_input
        trial, trial_contexts = self._slice_trial(contexts)
        if inter_input.slice_type % 5 == 0:
            if not inter_input.reference_lists or not inter_input.reference_lists[0]:
                raise CABACError("P_Skip reference picture is required")
            if not inter_input.partition_neighbors:
                raise CABACError("P_Skip motion neighbors are required")
            list_neighbors = inter_input.partition_neighbors[0].list0
            top_right_candidate = (
                inter_input.partition_neighbors[3].list0.top_right_candidate
                if len(inter_input.partition_neighbors) == 16
                else list_neighbors.top_right_candidate
            )
            skip_input = CABACPSkipMacroblockInput(
                slice_type=inter_input.slice_type,
                macroblock_x=inter_input.macroblock_x,
                macroblock_y=inter_input.macroblock_y,
                left_available=inter_input.left_available,
                left_skipped=input.left_skipped,
                top_available=inter_input.top_available,
                top_skipped=input.top_skipped,
                left_candidate=list_neighbors.left_candidate,
                top_candidate=list_neighbors.top_candidate,
                top_right_candidate=top_right_candidate,
                top_left_candidate=list_neighbors.top_left_candidate,
                reference_frame=inter_input.reference_lists[0][0].frame,
            )
            skip_result = trial.decode_p_skip_macroblock(
                skip_input, trial_contexts, builder, address
            )
        elif inter_input.slice_type % 5 == 1:
            if not isinstance(input.b_skip_input, CABACBDirectMacroblockInput):
                raise CABACError("B_Skip direct-prediction input is required")
            skip_result = trial.decode_b_skip_macroblock(
                input.b_skip_input, trial_contexts, builder, address
            )
        else:
            raise CABACError("P/B macroblock layer requires a P- or B-slice")

        if skip_result.skipped:
            self._commit_slice_trial(trial, contexts, trial_contexts)
            return CABACPBMacroblockLayerResult(
                True, skip_result, input.residual_input.previous_qpy
            )
        macroblock = trial._decode_inter_macroblock(
            inter_input,
            input.residual_input,
            trial_contexts,
            builder,
            address,
            input.b_skip_input,
            input.intra_input,
        )
        self._commit_slice_trial(trial, contexts, trial_contexts)
        return CABACPBMacroblockLayerResult(False, macroblock, macroblock.qpy)

    def decode_slice_data(
        self,
        slice_type: int,
        first_macroblock_address: int,
        picture_macroblock_count: int,
        contexts: list[CABACContextModel],
        builder,
        input_provider: Callable[
            [int, Sequence[CABACSliceMacroblockResult]],
            CABACIIntraMacroblockInput | CABACPBMacroblockLayerInput,
        ],
        progress_callback: Callable[[int], None] | None = None,
        threads: int = 1,
        reference_frames: Sequence[object] = (),
    ) -> tuple[CABACSliceMacroblockResult, ...]:
        """Run CABAC slice_data over progressive raster macroblocks through end_of_slice_flag.

        With threads > 1, P/B inter reconstruction runs in worker processes that share
        reference_frames; results are identical to the single-threaded decode.
        """
        _validate_slice_contexts(contexts)
        if (
            not isinstance(slice_type, int)
            or isinstance(slice_type, bool)
            or not 0 <= slice_type <= 9
            or slice_type % 5 not in (0, 1, 2)
            or not isinstance(first_macroblock_address, int)
            or isinstance(first_macroblock_address, bool)
            or not isinstance(picture_macroblock_count, int)
            or isinstance(picture_macroblock_count, bool)
            or picture_macroblock_count <= 0
            or not 0 <= first_macroblock_address < picture_macroblock_count
            or builder is None
            or not callable(input_provider)
            or progress_callback is not None and not callable(progress_callback)
        ):
            raise CABACError("CABAC slice_data inputs are invalid or unsupported")

        if slice_type % 5 == 2 or not _parallel_reconstruction_available(threads):
            return self._decode_slice_macroblocks(
                slice_type, first_macroblock_address, picture_macroblock_count,
                contexts, builder, input_provider, progress_callback, None,
            )
        with _SliceReconstructionDriver(
            threads, builder.picture_width_in_mbs, first_macroblock_address, reference_frames
        ) as driver:
            return self._decode_slice_macroblocks(
                slice_type, first_macroblock_address, picture_macroblock_count,
                contexts, builder, input_provider, progress_callback, driver,
            )

    def _decode_slice_macroblocks(
        self,
        slice_type,
        first_macroblock_address,
        picture_macroblock_count,
        contexts,
        builder,
        input_provider,
        progress_callback,
        driver,
    ) -> tuple[CABACSliceMacroblockResult, ...]:
        decoded = []
        coefficient_counts = []
        macroblock_address = first_macroblock_address
        while True:
            if driver is not None:
                driver.apply_ready(decoded, builder)
            if getattr(self, "_cavlc", None) is not None:
                width = builder.picture_width_in_mbs
                left = coefficient_counts[-1] if macroblock_address % width and coefficient_counts else None
                top_index = macroblock_address - first_macroblock_address - width
                top = coefficient_counts[top_index] if top_index >= 0 else None
                self._cavlc.begin(left, top)
            while True:
                macroblock_input = input_provider(macroblock_address, decoded)
                try:
                    macroblock_result = self._decode_slice_macroblock(
                        slice_type, macroblock_input, contexts, builder, macroblock_address
                    )
                    break
                except _IntraNeighborsPending:
                    driver.wait_for_intra_neighbors(macroblock_address, decoded, builder)
            if (
                isinstance(macroblock_result, CABACPBMacroblockLayerResult)
                and isinstance(macroblock_result.macroblock, CABACIIntraMacroblockResult)
            ):
                result = macroblock_result.macroblock
            else:
                result = macroblock_result
            if getattr(self, "_cavlc", None) is not None:
                coefficient_counts.append(tuple(self._cavlc.counts))
            decoded.append(CABACSliceMacroblockResult(macroblock_address, result))
            if driver is not None:
                job = driver.take_job()
                if job is not None:
                    driver.submit(macroblock_address, job, decoded, builder)
            if progress_callback is not None:
                progress_callback(macroblock_address)
            if self.decode_terminate_bin():
                if driver is not None:
                    driver.finish(decoded, builder)
                return tuple(decoded)
            if macroblock_address + 1 >= picture_macroblock_count:
                raise CABACError("slice_data reached the picture end without end_of_slice_flag")
            macroblock_address += 1

    def _decode_slice_macroblock(self, slice_type, macroblock_input, contexts, builder, macroblock_address):
        if slice_type % 5 == 2:
            if not isinstance(macroblock_input, CABACIIntraMacroblockInput):
                raise CABACError("I-slice input provider returned a non-intra macroblock")
            return self.decode_i_intra_macroblock(
                macroblock_input, contexts, builder, macroblock_address
            )
        if not isinstance(macroblock_input, CABACPBMacroblockLayerInput):
            raise CABACError("P/B input provider returned a non-inter macroblock")
        if macroblock_input.inter_input.slice_type % 5 != slice_type % 5:
            raise CABACError("macroblock slice type does not match its slice_data")
        return self.decode_p_b_macroblock_layer(
            macroblock_input, contexts, builder, macroblock_address
        )

    def _decode_inter_macroblock(
        self,
        input: CABACInterMacroblockInput,
        residual_input: CABACInterMacroblockResidualInput | None,
        contexts: list[CABACContextModel],
        builder,
        address: int,
        direct_input: CABACBDirectMacroblockInput | None = None,
        intra_input: CABACIIntraMacroblockInput | None = None,
    ) -> CABACInterMacroblockResult | CABACIIntraMacroblockResult:
        _validate_slice_contexts(contexts)
        if (
            not isinstance(input, CABACInterMacroblockInput)
            or not isinstance(input.slice_type, int)
            or isinstance(input.slice_type, bool)
            or input.slice_type > 9
            or input.slice_type % 5 not in (0, 1)
            or any(
                not isinstance(value, int) or isinstance(value, bool) or value < 0
                for value in (input.macroblock_x, input.macroblock_y)
            )
            or builder is None
        ):
            raise CABACError("inter macroblock input is invalid")
        is_b_slice = input.slice_type % 5 == 1
        if (
            not isinstance(input.max_reference_index_minus1, tuple)
            or len(input.max_reference_index_minus1) != 2
            or any(
                not isinstance(value, int)
                or isinstance(value, bool)
                or not 0 <= value <= 31
                for value in input.max_reference_index_minus1
            )
            or not isinstance(input.reference_lists, tuple)
            or len(input.reference_lists) != 2
            or any(not isinstance(references, tuple) for references in input.reference_lists)
        ):
            raise CABACError("inter macroblock reference lists are invalid")
        from .h264_reconstruction import (
            MotionVector,
            MotionVectorCandidate,
            apply_motion_vector_difference,
            predict_motion_vector,
            Yuv420Frame,
        )

        for references in input.reference_lists:
            if any(
                not isinstance(reference, CABACBReferencePicture)
                or not isinstance(reference.frame, Yuv420Frame)
                for reference in references
            ):
                raise CABACError("inter macroblock reference picture is invalid")
        if not input.reference_lists[0] or not input.reference_lists[1] and is_b_slice:
            raise CABACError("inter macroblock reference list is empty")

        trial, trial_contexts = self._slice_trial(contexts)
        macroblock_type = trial.decode_inter_mb_type(
            input.slice_type,
            trial_contexts,
            input.left_available,
            input.top_available,
            input.left_b_skip_or_direct,
            input.top_b_skip_or_direct,
        )
        intra_type_start = 23 if is_b_slice else 5
        if macroblock_type >= intra_type_start:
            _require_intra_neighbors(address)
            if not isinstance(intra_input, CABACIIntraMacroblockInput):
                raise CABACError("P/B intra macroblock references are unavailable")
            intra_result = trial._decode_i_intra_macroblock_with_type(
                intra_input,
                trial_contexts,
                builder,
                address,
                macroblock_type - intra_type_start,
            )
            self._commit_slice_trial(trial, contexts, trial_contexts)
            return intra_result
        sub_types: list[int] = []
        if (not is_b_slice and macroblock_type in (3, 4)) or (is_b_slice and macroblock_type == 22):
            sub_types = [trial.decode_sub_mb_type(input.slice_type, trial_contexts) for _ in range(4)]

        specifications: list[tuple[int, int, int, int, tuple[bool, bool], bool]] = []
        if not is_b_slice:
            if macroblock_type == 0:
                specifications.append((0, 0, 16, 16, (True, False), False))
            elif macroblock_type in (1, 2):
                for partition_index in range(2):
                    x = 0 if macroblock_type == 1 else partition_index * 8
                    y = partition_index * 8 if macroblock_type == 1 else 0
                    width, height = (16, 8) if macroblock_type == 1 else (8, 16)
                    specifications.append((x, y, width, height, (True, False), False))
            elif macroblock_type in (3, 4):
                for sub_index, sub_type in enumerate(sub_types):
                    base_x, base_y = (sub_index % 2) * 8, (sub_index // 2) * 8
                    sub_width, sub_height, count = (
                        (8, 8, 1) if sub_type == 0 else
                        (8, 4, 2) if sub_type == 1 else
                        (4, 8, 2) if sub_type == 2 else
                        (4, 4, 4)
                    )
                    for sub_part in range(count):
                        x = base_x + (sub_part % (8 // sub_width)) * sub_width
                        y = base_y + (sub_part // (8 // sub_width)) * sub_height
                        specifications.append((x, y, sub_width, sub_height, (True, False), False))
            else:
                raise CABACError("intra P macroblock syntax is not an inter macroblock")
        else:
            if macroblock_type == 0:
                specifications.append((0, 0, 16, 16, (True, True), True))
            elif macroblock_type in (1, 2, 3):
                flags = ((True, False), (False, True), (True, True))[macroblock_type - 1]
                specifications.append((0, 0, 16, 16, flags, False))
            elif 4 <= macroblock_type <= 21:
                partition_modes = (
                    ((True, False), (True, False)),
                    ((True, False), (True, False)),
                    ((False, True), (False, True)),
                    ((False, True), (False, True)),
                    ((True, False), (False, True)),
                    ((True, False), (False, True)),
                    ((False, True), (True, False)),
                    ((False, True), (True, False)),
                    ((True, False), (True, True)),
                    ((True, False), (True, True)),
                    ((False, True), (True, True)),
                    ((False, True), (True, True)),
                    ((True, True), (True, False)),
                    ((True, True), (True, False)),
                    ((True, True), (False, True)),
                    ((True, True), (False, True)),
                    ((True, True), (True, True)),
                    ((True, True), (True, True)),
                )
                flags = partition_modes[macroblock_type - 4]
                vertical_split = macroblock_type % 2 == 0
                for partition_index in range(2):
                    x = 0 if vertical_split else partition_index * 8
                    y = partition_index * 8 if vertical_split else 0
                    width, height = (16, 8) if vertical_split else (8, 16)
                    specifications.append((x, y, width, height, flags[partition_index], False))
            elif macroblock_type == 22:
                sub_modes = (
                    (False, False), (True, False), (False, True), (True, True),
                    (True, False), (True, False), (False, True), (False, True),
                    (True, True), (True, True), (True, False), (False, True), (True, True),
                )
                for sub_index, sub_type in enumerate(sub_types):
                    base_x, base_y = (sub_index % 2) * 8, (sub_index // 2) * 8
                    if sub_type == 0:
                        sub_width, sub_height, count = 4, 4, 4
                        flags, direct = (True, True), True
                    else:
                        flags = sub_modes[sub_type]
                        sub_width, sub_height, count = (
                            (8, 8, 1) if sub_type <= 3 else
                            (8, 4, 2) if sub_type in (4, 6, 8) else
                            (4, 8, 2) if sub_type in (5, 7, 9) else
                            (4, 4, 4)
                        )
                        direct = False
                    for sub_part in range(count):
                        x = base_x + (sub_part % (8 // sub_width)) * sub_width
                        y = base_y + (sub_part // (8 // sub_width)) * sub_height
                        specifications.append((x, y, sub_width, sub_height, flags, direct))
            else:
                raise CABACError("intra B macroblock syntax is not an inter macroblock")

        if len(input.partition_neighbors) not in (len(specifications), 16):
            raise CABACError("inter macroblock partition neighbor count does not match its type")
        if any(
            not isinstance(neighbors, CABACInterPartitionNeighbors)
            for neighbors in input.partition_neighbors
        ):
            raise CABACError("inter macroblock partition neighbor state is invalid")
        frame_dimensions = {
            (reference.frame.width, reference.frame.height)
            for reference_list in input.reference_lists
            for reference in reference_list
        }
        if len(frame_dimensions) != 1:
            raise CABACError("inter macroblock reference picture dimensions do not match")
        frame_width, frame_height = next(iter(frame_dimensions))
        partition_results = []

        prepared = []
        for partition_index, (x, y, width, height, flags, direct) in enumerate(specifications):
            neighbors = (
                input.partition_neighbors[(y // 4) * 4 + x // 4]
                if len(input.partition_neighbors) == 16
                else input.partition_neighbors[partition_index]
            )
            if not isinstance(neighbors.list0, CABACInterListNeighbors) or not isinstance(
                neighbors.list1, CABACInterListNeighbors
            ):
                raise CABACError("inter prediction list neighbor state is invalid")
            if any(
                not isinstance(candidate, MotionVectorCandidate)
                for list_neighbors in (neighbors.list0, neighbors.list1)
                for candidate in (
                    list_neighbors.left_candidate,
                    list_neighbors.top_candidate,
                    list_neighbors.top_right_candidate,
                    list_neighbors.top_left_candidate,
                )
                if candidate is not None
            ):
                raise CABACError("inter prediction motion-vector candidate is invalid")
            ref_indices = [-1, -1]
            motion_vectors = [MotionVector(0, 0), MotionVector(0, 0)]
            motion_differences = [(0, 0), (0, 0)]
            if direct:
                prediction = neighbors.direct_prediction
                if prediction is None and direct_input is None:
                    raise CABACError("direct inter partition prediction metadata is required")
                if prediction is not None:
                    if not isinstance(prediction, CABACBDirectMacroblockResult):
                        raise CABACError("direct inter partition prediction metadata is invalid")
                    ref_indices = list(prediction.reference_indices)
                    motion_vectors = [MotionVector(*vector) for vector in prediction.motion_vectors]
                    flags = prediction.prediction_flags
            prepared.append(
                {
                    "x": x,
                    "y": y,
                    "width": width,
                    "height": height,
                    "flags": list(flags),
                    "direct": direct,
                    "neighbors": neighbors,
                    "references": ref_indices,
                    "vectors": motion_vectors,
                    "motion_differences": motion_differences,
                }
            )

        def neighbors_for_partition(partition_index: int) -> CABACInterPartitionNeighbors:
            state = prepared[partition_index]

            def current_fact(sample_x: int, sample_y: int, list_index: int):
                if not 0 <= sample_x < 16 or not 0 <= sample_y < 16:
                    return None
                for previous in reversed(prepared[:partition_index]):
                    if (
                        previous["x"] <= sample_x < previous["x"] + previous["width"]
                        and previous["y"] <= sample_y < previous["y"] + previous["height"]
                    ):
                        active = previous["flags"][list_index]
                        reference_index = previous["references"][list_index] if active else 0
                        difference = previous["motion_differences"][list_index]
                        candidate = (
                            MotionVectorCandidate(
                                reference_index,
                                previous["vectors"][list_index],
                            )
                            if active and reference_index >= 0
                            else None
                        )
                        return (
                            CABACInterNeighbor(
                                available=True,
                                skip=previous["direct"],
                                prediction_mode_matches=active,
                                reference_index=max(0, reference_index),
                                motion_vector_difference=tuple(difference),
                            ),
                            candidate,
                        )
                return CABACInterNeighbor(), None

            list_states = []
            for list_index, external in enumerate((state["neighbors"].list0, state["neighbors"].list1)):
                x, y = state["x"], state["y"]
                width = state["width"]

                def resolved(sample_x: int, sample_y: int, name: str):
                    if name in ("top_right", "top_left"):
                        if 0 <= sample_x < 16 and 0 <= sample_y < 16:
                            fact = current_fact(sample_x, sample_y, list_index)
                            return CABACInterNeighbor(), None if fact is None else fact[1]
                        if name == "top_right":
                            handled, candidate = _aligned_partition_top_candidate(
                                input.partition_neighbors, list_index, sample_x, sample_y
                            )
                            if handled:
                                return CABACInterNeighbor(), candidate
                        if name == "top_right" and sample_x >= 16 and len(input.partition_neighbors) == 16:
                            edge_row = min(max(y // 4, 0), 3)
                            edge = input.partition_neighbors[edge_row * 4 + 3]
                            edge_list = edge.list0 if list_index == 0 else edge.list1
                            return CABACInterNeighbor(), edge_list.top_right_candidate
                        return CABACInterNeighbor(), getattr(
                            external, f"{name}_candidate"
                        )
                    if 0 <= sample_x < 16 and 0 <= sample_y < 16:
                        return current_fact(sample_x, sample_y, list_index)
                    return getattr(external, name), getattr(external, f"{name}_candidate")

                left, left_candidate = resolved(x - 1, y, "left")
                top, top_candidate = resolved(x, y - 1, "top")
                top_right, top_right_candidate = resolved(x + width, y - 1, "top_right")
                top_left, top_left_candidate = resolved(x - 1, y - 1, "top_left")
                list_states.append(
                    CABACInterListNeighbors(
                        left=left,
                        top=top,
                        left_candidate=left_candidate,
                        top_candidate=top_candidate,
                        top_right_candidate=top_right_candidate,
                        top_left_candidate=top_left_candidate,
                    )
                )
            return CABACInterPartitionNeighbors(
                list0=list_states[0],
                list1=list_states[1],
                direct_prediction=state["neighbors"].direct_prediction,
            )

        if direct_input is not None:
            direct_8x8_inference = bool(
                residual_input and residual_input.direct_8x8_inference_flag
            )
            for partition_index, state in enumerate(prepared):
                if not state["direct"] or state["neighbors"].direct_prediction is not None:
                    continue
                if direct_8x8_inference:
                    group = (state["y"] // 8) * 2 + state["x"] // 8
                    previous_direct = next(
                        (
                            previous
                            for previous in reversed(prepared[:partition_index])
                            if previous["direct"]
                            and (previous["y"] // 8) * 2 + previous["x"] // 8 == group
                        ),
                        None,
                    )
                    if previous_direct is not None:
                        state["flags"] = previous_direct["flags"][:]
                        state["references"] = previous_direct["references"][:]
                        state["vectors"] = previous_direct["vectors"][:]
                        continue
                prediction = _derive_spatial_b_direct_partition(
                    direct_input,
                    neighbors_for_partition(partition_index),
                    state["x"],
                    state["y"],
                    direct_8x8_inference,
                )
                state["flags"] = list(prediction.prediction_flags)
                state["references"] = list(prediction.reference_indices)
                state["vectors"] = [MotionVector(*vector) for vector in prediction.motion_vectors]

        groups = {}
        for index, state in enumerate(prepared):
            group = (state["y"] // 8) * 2 + state["x"] // 8
            groups.setdefault(group, []).append(index)
        for list_index in range(2):
            for group_indices in groups.values():
                first_index = group_indices[0]
                first_state = prepared[first_index]
                if first_state["direct"] or not first_state["flags"][list_index]:
                    continue
                neighbors = neighbors_for_partition(first_index)
                list_neighbors = neighbors.list0 if list_index == 0 else neighbors.list1
                if not isinstance(list_neighbors, CABACInterListNeighbors):
                    raise CABACError("inter prediction list neighbor state is invalid")
                if not (not is_b_slice and macroblock_type == 4):
                    decoded_reference_index = trial.decode_reference_index_for_partition(
                        input.max_reference_index_minus1[list_index],
                        list_neighbors.left,
                        list_neighbors.top,
                        trial_contexts[54:60],
                    )
                else:
                    decoded_reference_index = 0
                for index in group_indices:
                    state = prepared[index]
                    if not state["direct"] and state["flags"][list_index]:
                        state["references"][list_index] = decoded_reference_index

        for list_index in range(2):
            for partition_index, state in enumerate(prepared):
                if state["direct"] or not state["flags"][list_index]:
                    continue
                neighbors = neighbors_for_partition(partition_index)
                list_neighbors = neighbors.list0 if list_index == 0 else neighbors.list1
                difference = [
                    trial.decode_motion_vector_difference_for_partition(
                        component,
                        list_neighbors.left,
                        list_neighbors.top,
                        trial_contexts[context_base : context_base + 7],
                    )
                    for component, context_base in ((0, 40), (1, 47))
                ]
                state["motion_differences"][list_index] = tuple(difference)
                left = list_neighbors.left_candidate
                top = list_neighbors.top_candidate
                right = list_neighbors.top_right_candidate
                if right is None:
                    right = list_neighbors.top_left_candidate
                preferred = None
                if state["width"] == 16 and state["height"] == 8:
                    preferred = top if state["y"] == 0 else left
                elif state["width"] == 8 and state["height"] == 16:
                    preferred = left if state["x"] == 0 else right
                reference_index = state["references"][list_index]
                if preferred is not None and preferred.reference_index == reference_index:
                    predicted = preferred.vector
                elif top is None and right is None and left is not None:
                    predicted = left.vector
                else:
                    predicted = predict_motion_vector(reference_index, left, top, right, None)
                state["vectors"][list_index] = apply_motion_vector_difference(
                    predicted, MotionVector(*difference)
                )

        job_frames = []
        job_partitions = []
        for state in prepared:
            x, y = state["x"], state["y"]
            width, height = state["width"], state["height"]
            flags = state["flags"]
            ref_indices = state["references"]
            motion_vectors = state["vectors"]

            selected = []
            for list_index, active in enumerate(flags):
                if not active:
                    selected.append(None)
                    continue
                if not 0 <= ref_indices[list_index] < len(input.reference_lists[list_index]):
                    raise CABACError("inter macroblock reference index is unavailable")
                reference = input.reference_lists[list_index][ref_indices[list_index]].frame
                if not any(reference is frame for frame in job_frames):
                    job_frames.append(reference)
                frame_index = next(
                    index for index, frame in enumerate(job_frames) if frame is reference
                )
                vector = motion_vectors[list_index]
                selected.append((frame_index, (vector.x, vector.y)))
            job_partitions.append((x, y, width, height, tuple(selected)))
            partition_results.append(
                CABACInterMacroblockPartition(
                    x, y, width, height, tuple(flags), tuple(ref_indices),
                    tuple((vector.x, vector.y) for vector in motion_vectors),
                    tuple(state["motion_differences"]),
                )
            )

        residual = _InterResidual()
        residual_result = (0, 0, False, 0, 26, (), (False, False), ((False,) * 4, (False,) * 4), (False,) * 4)
        if residual_input is not None:
            residual_result = trial._decode_inter_residuals(
                macroblock_type,
                input.slice_type,
                tuple(sub_types),
                residual_input,
                trial_contexts,
                residual,
            )
        samples = _reconstruct_or_defer(
            "inter",
            tuple(job_frames),
            (
                tuple(job_partitions),
                input.macroblock_x,
                input.macroblock_y,
                frame_width,
                frame_height,
                residual,
            ),
            getattr(self, "_timeout_check", None),
        )
        if samples is not None:
            builder.place_macroblock(address, *samples)
        luma, cb, cr = samples or _PLACEHOLDER_SAMPLES
        self._commit_slice_trial(trial, contexts, trial_contexts)
        return CABACInterMacroblockResult(
            macroblock_type=macroblock_type,
            sub_macroblock_types=tuple(sub_types),
            partitions=tuple(partition_results),
            luma=tuple(luma),
            cb=tuple(cb),
            cr=tuple(cr),
            coded_block_pattern_luma=residual_result[0],
            coded_block_pattern_chroma=residual_result[1],
            transform_size_8x8=residual_result[2],
            qp_delta=residual_result[3],
            qpy=residual_result[4],
            luma_4x4_coded_block_flags=residual_result[5],
            chroma_dc_coded=residual_result[6],
            chroma_ac_coded_flags=residual_result[7],
            luma_8x8_nonzero=residual_result[8],
        )

    def _decode_inter_residuals(
        self,
        macroblock_type: int,
        slice_type: int,
        sub_types: tuple[int, ...],
        input: CABACInterMacroblockResidualInput,
        contexts: list[CABACContextModel],
        residual_output: "_InterResidual",
    ):
        if (
            not 0 <= input.previous_qpy <= 51
            or not -51 <= input.previous_qp_delta <= 51
            or not 0 <= input.left_luma_cbp <= 15
            or not 0 <= input.top_luma_cbp <= 15
            or not 0 <= input.left_chroma_cbp <= 2
            or not 0 <= input.top_chroma_cbp <= 2
            or len(input.luma_4x4_scaling_list) != 16
            or len(input.luma_8x8_scaling_list) != 64
            or len(input.chroma_scaling_lists) != 2
            or len(input.chroma_qp_index_offsets) != 2
            or len(input.chroma_left_edges) != 2
            or len(input.chroma_top_edges) != 2
            or not isinstance(input.luma_4x4_top_edge, CABACIntra4x4EdgeState)
            or not isinstance(input.luma_4x4_left_edge, CABACIntra4x4EdgeState)
        ):
            raise CABACError("inter macroblock residual inputs are invalid")
        from .h264_reconstruction import (
            assemble_chroma420_residual_macroblock,
            derive_chroma_qpc,
            inverse_scale_chroma_dc2x2,
            inverse_scale_luma4x4,
            inverse_scale_luma8x8,
            inverse_transform_chroma_dc2x2,
            inverse_transform_luma4x4,
            inverse_transform_luma8x8,
            reconstruct_chroma4x4_residual,
        )

        cbp_luma = self.decode_luma_coded_block_pattern(
            input.left_luma_cbp, input.top_luma_cbp, contexts[73:77]
        )
        cbp_chroma = self.decode_chroma_coded_block_pattern(
            input.left_chroma_cbp, input.top_chroma_cbp, contexts[77:85]
        )
        no_sub_mb_part_size_less_than_8x8 = True
        if (macroblock_type in (3, 4) and not (macroblock_type == 4 and not sub_types)) or (
            macroblock_type == 22
        ):
            for sub_type in sub_types:
                if macroblock_type == 22 and sub_type == 0:
                    if not input.direct_8x8_inference_flag:
                        no_sub_mb_part_size_less_than_8x8 = False
                elif (macroblock_type != 22 and sub_type in (1, 2, 3)) or (
                    macroblock_type == 22 and sub_type in (4, 5, 6, 7, 8, 9, 10, 11, 12)
                ):
                    no_sub_mb_part_size_less_than_8x8 = False
        transform_allowed = (
            input.transform_8x8_mode_enabled
            and cbp_luma != 0
            and no_sub_mb_part_size_less_than_8x8
            and not (
                slice_type % 5 == 1
                and macroblock_type == 0
                and not input.direct_8x8_inference_flag
            )
        )
        transform_8x8 = False
        if transform_allowed:
            transform_8x8 = self.decode_transform_size_8x8_flag(
                contexts[399:402],
                input.left_has_8x8_transform,
                input.top_has_8x8_transform,
            )

        qp_delta = 0
        qpy = input.previous_qpy
        if cbp_luma or cbp_chroma:
            qp_delta = self.decode_mb_qp_delta(contexts[60:64], input.previous_qp_delta)
            qpy = (qpy + qp_delta + 52) % 52

        luma_coded_flags = [False] * 16
        luma_8x8_nonzero = [False] * 4
        if transform_8x8:
            for block_index in range(4):
                if not cbp_luma & (1 << block_index):
                    continue
                scan_levels = self.decode_luma8x8_residual_block(contexts)
                luma_8x8_nonzero[block_index] = any(scan_levels)
                if any(scan_levels):
                    block_x, block_y = (block_index % 2) * 2, (block_index // 2) * 2
                    for row in range(2):
                        for column in range(2):
                            raster_index = (block_y + row) * 4 + block_x + column
                            scan_index = _LUMA4X4_BLOCK_RASTER_TO_SCAN[raster_index]
                            luma_coded_flags[scan_index] = True
                raster_levels = [0] * 64
                for scan_index, raster_index in enumerate(_LUMA8X8_SCAN_TO_RASTER):
                    raster_levels[raster_index] = scan_levels[scan_index]
                residual = inverse_transform_luma8x8(
                    inverse_scale_luma8x8(raster_levels, input.luma_8x8_scaling_list, qpy)
                )
                block_x, block_y = (block_index % 2) * 8, (block_index // 2) * 8
                for row in range(8):
                    for column in range(8):
                        destination = (block_y + row) * 16 + block_x + column
                        residual_output.luma[destination] = residual[row * 8 + column]
        else:
            for block_index in range(16):
                raster_index = _LUMA4X4_BLOCK_SCAN_TO_RASTER[block_index]
                block_x, block_y = raster_index % 4, raster_index // 4
                cbp_index = (block_y // 2) * 2 + block_x // 2
                if not cbp_luma & (1 << cbp_index):
                    continue
                cond_left = _inter4x4_luma_cond_term(
                    block_index, block_x, block_y, True,
                    luma_coded_flags, input.luma_4x4_left_edge,
                )
                cond_top = _inter4x4_luma_cond_term(
                    block_index, block_x, block_y, False,
                    luma_coded_flags, input.luma_4x4_top_edge,
                )
                levels, coded = self.decode_residual_block(2, cond_left, cond_top, contexts)
                luma_coded_flags[block_index] = coded
                if not coded:
                    continue
                residual = inverse_transform_luma4x4(
                    inverse_scale_luma4x4(
                        place_luma4x4_scan_levels(levels), input.luma_4x4_scaling_list, qpy
                    )
                )
                for row in range(4):
                    for column in range(4):
                        destination = (block_y * 4 + row) * 16 + block_x * 4 + column
                        residual_output.luma[destination] = residual[row * 4 + column]

        chroma_dc_coded = [False, False]
        chroma_dc_samples = [[0] * 4 for _ in range(2)]
        qpc_values = [derive_chroma_qpc(qpy, offset) for offset in input.chroma_qp_index_offsets]
        if cbp_chroma:
            for component in range(2):
                left_edge = input.chroma_left_edges[component]
                top_edge = input.chroma_top_edges[component]
                if not isinstance(left_edge, CABACChroma420EdgeState) or not isinstance(
                    top_edge, CABACChroma420EdgeState
                ):
                    raise CABACError("inter chroma CBF edge state is invalid")
                cond_left = derive_coded_block_flag_cond_term(
                    left_edge.available, False, left_edge.is_ipcm,
                    left_edge.dc_block_available, left_edge.dc_block_coded,
                )
                cond_top = derive_coded_block_flag_cond_term(
                    top_edge.available, False, top_edge.is_ipcm,
                    top_edge.dc_block_available, top_edge.dc_block_coded,
                )
                dc_levels, coded = self.decode_residual_block(3, cond_left, cond_top, contexts)
                chroma_dc_coded[component] = coded
                if coded:
                    chroma_dc_samples[component] = inverse_scale_chroma_dc2x2(
                        inverse_transform_chroma_dc2x2(dc_levels[:4]), qpc_values[component]
                    )

        chroma_ac_flags = [[False] * 4 for _ in range(2)]
        for component in range(2):
            residual_blocks = []
            for block_index in range(4):
                block_x, block_y = block_index % 2, block_index // 2
                ac_levels = [0] * 15
                if cbp_chroma == 2:
                    left_edge = input.chroma_left_edges[component]
                    top_edge = input.chroma_top_edges[component]
                    if block_x:
                        cond_left = derive_coded_block_flag_cond_term(
                            True, False, False, True, chroma_ac_flags[component][block_index - 1]
                        )
                    elif not left_edge.available or left_edge.is_ipcm:
                        cond_left = derive_coded_block_flag_cond_term(
                            left_edge.available, False, left_edge.is_ipcm, False, False
                        )
                    else:
                        cond_left = derive_coded_block_flag_cond_term(
                            True, False, False,
                            left_edge.ac_block_available[block_y],
                            left_edge.ac_block_coded[block_y],
                        )
                    if block_y:
                        cond_top = derive_coded_block_flag_cond_term(
                            True, False, False, True, chroma_ac_flags[component][block_index - 2]
                        )
                    elif not top_edge.available or top_edge.is_ipcm:
                        cond_top = derive_coded_block_flag_cond_term(
                            top_edge.available, False, top_edge.is_ipcm, False, False
                        )
                    else:
                        cond_top = derive_coded_block_flag_cond_term(
                            True, False, False,
                            top_edge.ac_block_available[block_x],
                            top_edge.ac_block_coded[block_x],
                        )
                    levels, coded = self.decode_residual_block(4, cond_left, cond_top, contexts)
                    chroma_ac_flags[component][block_index] = coded
                    if coded:
                        ac_levels = levels[:15]
                residual_blocks.append(
                    reconstruct_chroma4x4_residual(
                        chroma_dc_samples[component][block_index],
                        ac_levels,
                        input.chroma_scaling_lists[component],
                        qpc_values[component],
                    )
                )
            residual_plane = assemble_chroma420_residual_macroblock(residual_blocks)
            residual_output.chroma[component] = residual_plane

        return (
            cbp_luma,
            cbp_chroma,
            transform_8x8,
            qp_delta,
            qpy,
            tuple(luma_coded_flags),
            tuple(chroma_dc_coded),
            (tuple(chroma_ac_flags[0]), tuple(chroma_ac_flags[1])),
            tuple(luma_8x8_nonzero),
        )

    def decode_intra_chroma_pred_mode(
        self,
        contexts: list[CABACContextModel],
        left_has_nonzero_mode: bool,
        top_has_nonzero_mode: bool,
    ) -> int:
        if getattr(self, "_cavlc", None) is not None:
            return CAVLCSyntax.unsigned(self._bits, 3)
        if not isinstance(contexts, list) or len(contexts) != 4 or any(
            not isinstance(model, CABACContextModel) or not 0 <= model._state_index < 64
            for model in contexts
        ):
            raise CABACError("intra_chroma_pred_mode requires context models 64 through 67")
        if len({id(model) for model in contexts}) != 4:
            raise CABACError("intra_chroma_pred_mode context models must be distinct")

        trial_bits = BitReader(self._bits._data)
        trial_bits._bit_offset = self._bits._bit_offset
        trial = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
        trial._bits = trial_bits
        trial._code_range = self._code_range
        trial._code_offset = self._code_offset
        trial._terminated = self._terminated
        trial._cavlc = self._cavlc.clone() if getattr(self, "_cavlc", None) is not None else None
        trial_contexts = [CABACContextModel.__new__(CABACContextModel) for _ in range(4)]
        for target, source in zip(trial_contexts, contexts):
            target._state_index = source._state_index
            target._value_mps = source._value_mps

        context_index = int(left_has_nonzero_mode) + int(top_has_nonzero_mode)
        if not trial.decode_bin(trial_contexts[context_index]):
            mode = 0
        elif not trial.decode_bin(trial_contexts[3]):
            mode = 1
        elif not trial.decode_bin(trial_contexts[3]):
            mode = 2
        else:
            mode = 3

        self._bits._bit_offset = trial._bits._bit_offset
        self._code_range = trial._code_range
        self._code_offset = trial._code_offset
        self._terminated = trial._terminated
        self._cavlc = getattr(trial, "_cavlc", None)
        for target, source in zip(contexts, trial_contexts):
            target._state_index = source._state_index
            target._value_mps = source._value_mps
        return mode

    def decode_intra4x4_pred_mode(
        self, predicted_mode: int, contexts: list[CABACContextModel]
    ) -> int:
        if getattr(self, "_cavlc", None) is not None:
            return CAVLCSyntax.prediction_mode(self._bits, predicted_mode)
        if not 0 <= predicted_mode <= 8:
            raise CABACError("CABAC intra4x4 prediction mode is outside [0,8]")
        if not isinstance(contexts, list) or len(contexts) != 2 or any(
            not isinstance(model, CABACContextModel) or not 0 <= model._state_index < 64
            for model in contexts
        ):
            raise CABACError("intra4x4 prediction mode requires context models 68 and 69")
        if contexts[0] is contexts[1]:
            raise CABACError("intra4x4 prediction mode contexts must be distinct")

        trial_bits = BitReader(self._bits._data)
        trial_bits._bit_offset = self._bits._bit_offset
        trial = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
        trial._bits = trial_bits
        trial._code_range = self._code_range
        trial._code_offset = self._code_offset
        trial._terminated = self._terminated
        trial._cavlc = self._cavlc.clone() if getattr(self, "_cavlc", None) is not None else None
        trial_contexts = [CABACContextModel.__new__(CABACContextModel) for _ in range(2)]
        for target, source in zip(trial_contexts, contexts):
            target._state_index = source._state_index
            target._value_mps = source._value_mps

        if trial.decode_bin(trial_contexts[0]):
            mode = predicted_mode
        else:
            remaining_mode = sum(
                int(trial.decode_bin(trial_contexts[1])) << bit_index
                for bit_index in range(3)
            )
            mode = remaining_mode + int(remaining_mode >= predicted_mode)

        self._bits._bit_offset = trial._bits._bit_offset
        self._code_range = trial._code_range
        self._code_offset = trial._code_offset
        self._terminated = trial._terminated
        self._cavlc = getattr(trial, "_cavlc", None)
        for target, source in zip(contexts, trial_contexts):
            target._state_index = source._state_index
            target._value_mps = source._value_mps
        return mode

    def decode_intra4x4_pred_modes(
        self,
        contexts: list[CABACContextModel],
        top_modes: Sequence[int],
        left_modes: Sequence[int],
        top_available: bool,
        left_available: bool,
    ) -> list[int]:
        if not isinstance(contexts, list) or len(contexts) != 2 or any(
            not isinstance(model, CABACContextModel) or not 0 <= model._state_index < 64
            for model in contexts
        ):
            raise CABACError("Intra_4x4 mode parsing requires contexts 68 and 69")
        if contexts[0] is contexts[1]:
            raise CABACError("Intra_4x4 prediction mode contexts must be distinct")

        trial_bits = BitReader(self._bits._data)
        trial_bits._bit_offset = self._bits._bit_offset
        trial = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
        trial._bits = trial_bits
        trial._code_range = self._code_range
        trial._code_offset = self._code_offset
        trial._terminated = self._terminated
        trial._cavlc = self._cavlc.clone() if getattr(self, "_cavlc", None) is not None else None
        trial_contexts = [CABACContextModel.__new__(CABACContextModel) for _ in range(2)]
        for target, source in zip(trial_contexts, contexts):
            target._state_index = source._state_index
            target._value_mps = source._value_mps

        modes = [0] * 16
        for block_index in range(16):
            predicted_mode = derive_intra4x4_predicted_mode(
                modes, block_index, top_modes, left_modes, top_available, left_available
            )
            modes[block_index] = trial.decode_intra4x4_pred_mode(
                predicted_mode, trial_contexts
            )

        self._bits._bit_offset = trial._bits._bit_offset
        self._code_range = trial._code_range
        self._code_offset = trial._code_offset
        self._terminated = trial._terminated
        self._cavlc = getattr(trial, "_cavlc", None)
        for target, source in zip(contexts, trial_contexts):
            target._state_index = source._state_index
            target._value_mps = source._value_mps
        return modes

    def decode_transform_size_8x8_flag(
        self,
        contexts: list[CABACContextModel],
        left_has_8x8_transform: bool,
        top_has_8x8_transform: bool,
    ) -> bool:
        if getattr(self, "_cavlc", None) is not None:
            return self._bits.read_bit()
        if not isinstance(contexts, list) or len(contexts) != 3:
            raise CABACError("transform_size_8x8_flag requires context models 399 through 401")
        context_index = int(left_has_8x8_transform) + int(top_has_8x8_transform)
        return self.decode_bin(contexts[context_index])

    def decode_luma_coded_block_pattern(
        self,
        left_cbp: int,
        top_cbp: int,
        contexts: list[CABACContextModel],
    ) -> int:
        if getattr(self, "_cavlc", None) is not None:
            return self._cavlc.luma_pattern(self._bits)
        if not 0 <= left_cbp <= 15 or not 0 <= top_cbp <= 15:
            raise CABACError("CABAC luma coded_block_pattern is outside [0,15]")
        if not isinstance(contexts, list) or len(contexts) != 4 or any(
            not isinstance(model, CABACContextModel) or not 0 <= model._state_index < 64
            for model in contexts
        ):
            raise CABACError("luma coded_block_pattern requires context models 73 through 76")
        if len({id(model) for model in contexts}) != 4:
            raise CABACError("luma coded_block_pattern context models must be distinct")

        trial_bits = BitReader(self._bits._data)
        trial_bits._bit_offset = self._bits._bit_offset
        trial = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
        trial._bits = trial_bits
        trial._code_range = self._code_range
        trial._code_offset = self._code_offset
        trial._terminated = self._terminated
        trial._cavlc = self._cavlc.clone() if getattr(self, "_cavlc", None) is not None else None
        trial_contexts = [CABACContextModel.__new__(CABACContextModel) for _ in range(4)]
        for target, source in zip(trial_contexts, contexts):
            target._state_index = source._state_index
            target._value_mps = source._value_mps

        pattern = 0
        context_index = int(not left_cbp & 0x02) + 2 * int(not top_cbp & 0x04)
        pattern |= int(trial.decode_bin(trial_contexts[context_index]))
        context_index = int(not pattern & 0x01) + 2 * int(not top_cbp & 0x08)
        pattern |= int(trial.decode_bin(trial_contexts[context_index])) << 1
        context_index = int(not left_cbp & 0x08) + 2 * int(not pattern & 0x01)
        pattern |= int(trial.decode_bin(trial_contexts[context_index])) << 2
        context_index = int(not pattern & 0x04) + 2 * int(not pattern & 0x02)
        pattern |= int(trial.decode_bin(trial_contexts[context_index])) << 3

        self._bits._bit_offset = trial._bits._bit_offset
        self._code_range = trial._code_range
        self._code_offset = trial._code_offset
        self._terminated = trial._terminated
        self._cavlc = getattr(trial, "_cavlc", None)
        for target, source in zip(contexts, trial_contexts):
            target._state_index = source._state_index
            target._value_mps = source._value_mps
        return pattern

    def decode_chroma_coded_block_pattern(
        self,
        left_cbp: int,
        top_cbp: int,
        contexts: list[CABACContextModel],
    ) -> int:
        if getattr(self, "_cavlc", None) is not None:
            return self._cavlc.chroma_cbp
        if not 0 <= left_cbp <= 2 or not 0 <= top_cbp <= 2:
            raise CABACError("CABAC chroma coded_block_pattern is outside [0,2]")
        if not isinstance(contexts, list) or len(contexts) != 8 or any(
            not isinstance(model, CABACContextModel) or not 0 <= model._state_index < 64
            for model in contexts
        ):
            raise CABACError("chroma coded_block_pattern requires context models 77 through 84")
        if len({id(model) for model in contexts}) != 8:
            raise CABACError("chroma coded_block_pattern context models must be distinct")

        trial_bits = BitReader(self._bits._data)
        trial_bits._bit_offset = self._bits._bit_offset
        trial = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
        trial._bits = trial_bits
        trial._code_range = self._code_range
        trial._code_offset = self._code_offset
        trial._terminated = self._terminated
        trial._cavlc = self._cavlc.clone() if getattr(self, "_cavlc", None) is not None else None
        trial_contexts = [CABACContextModel.__new__(CABACContextModel) for _ in range(8)]
        for target, source in zip(trial_contexts, contexts):
            target._state_index = source._state_index
            target._value_mps = source._value_mps

        context_index = int(left_cbp > 0) + 2 * int(top_cbp > 0)
        if not trial.decode_bin(trial_contexts[context_index]):
            pattern = 0
        else:
            context_index = 4 + int(left_cbp == 2) + 2 * int(top_cbp == 2)
            pattern = 1 + int(trial.decode_bin(trial_contexts[context_index]))

        self._bits._bit_offset = trial._bits._bit_offset
        self._code_range = trial._code_range
        self._code_offset = trial._code_offset
        self._terminated = trial._terminated
        self._cavlc = getattr(trial, "_cavlc", None)
        for target, source in zip(contexts, trial_contexts):
            target._state_index = source._state_index
            target._value_mps = source._value_mps
        return pattern

    def decode_luma4x4_coded_block_flag(
        self,
        left_nonzero: int,
        top_nonzero: int,
        contexts: list[CABACContextModel],
    ) -> bool:
        if not 0 <= left_nonzero <= 16 or not 0 <= top_nonzero <= 16:
            raise CABACError("CABAC luma 4x4 nonzero count is outside [0,16]")
        if not isinstance(contexts, list) or len(contexts) != 4:
            raise CABACError("luma 4x4 coded_block_flag requires context models 93 through 96")
        context_index = int(left_nonzero > 0) + 2 * int(top_nonzero > 0)
        return self.decode_bin(contexts[context_index])

    def decode_luma4x4_significance_map(
        self,
        significant_contexts: list[CABACContextModel],
        last_contexts: list[CABACContextModel],
    ) -> list[bool]:
        for contexts, name in (
            (significant_contexts, "significant_coeff_flag"),
            (last_contexts, "last_significant_coeff_flag"),
        ):
            if not isinstance(contexts, list) or len(contexts) != 15 or any(
                not isinstance(model, CABACContextModel) or not 0 <= model._state_index < 64
                for model in contexts
            ):
                raise CABACError(f"luma4x4 {name} requires 15 valid frame-scan contexts")
            if len({id(model) for model in contexts}) != 15:
                raise CABACError(f"luma4x4 {name} contexts must be distinct")

        trial_bits = BitReader(self._bits._data)
        trial_bits._bit_offset = self._bits._bit_offset
        trial = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
        trial._bits = trial_bits
        trial._code_range = self._code_range
        trial._code_offset = self._code_offset
        trial._terminated = self._terminated
        trial._cavlc = self._cavlc.clone() if getattr(self, "_cavlc", None) is not None else None
        trial_significant = [CABACContextModel.__new__(CABACContextModel) for _ in range(15)]
        trial_last = [CABACContextModel.__new__(CABACContextModel) for _ in range(15)]
        for target, source in zip(trial_significant, significant_contexts):
            target._state_index = source._state_index
            target._value_mps = source._value_mps
        for target, source in zip(trial_last, last_contexts):
            target._state_index = source._state_index
            target._value_mps = source._value_mps

        significance = [False] * 16
        last_found = False
        for scan_index in range(15):
            if not trial.decode_bin(trial_significant[scan_index]):
                continue
            significance[scan_index] = True
            if trial.decode_bin(trial_last[scan_index]):
                last_found = True
                break
        if not last_found:
            significance[15] = True

        self._bits._bit_offset = trial._bits._bit_offset
        self._code_range = trial._code_range
        self._code_offset = trial._code_offset
        self._terminated = trial._terminated
        self._cavlc = getattr(trial, "_cavlc", None)
        for target, source in zip(significant_contexts, trial_significant):
            target._state_index = source._state_index
            target._value_mps = source._value_mps
        for target, source in zip(last_contexts, trial_last):
            target._state_index = source._state_index
            target._value_mps = source._value_mps
        return significance

    def decode_coeff_abs_level_minus1(
        self,
        first_context: CABACContextModel,
        greater_one_context: CABACContextModel,
    ) -> int:
        if (
            not isinstance(first_context, CABACContextModel)
            or not isinstance(greater_one_context, CABACContextModel)
            or first_context is greater_one_context
            or not 0 <= first_context._state_index < 64
            or not 0 <= greater_one_context._state_index < 64
        ):
            raise CABACError("coefficient level requires two distinct valid contexts")

        trial_bits = BitReader(self._bits._data)
        trial_bits._bit_offset = self._bits._bit_offset
        trial = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
        trial._bits = trial_bits
        trial._code_range = self._code_range
        trial._code_offset = self._code_offset
        trial._terminated = self._terminated
        trial._cavlc = self._cavlc.clone() if getattr(self, "_cavlc", None) is not None else None
        trial_first = CABACContextModel.__new__(CABACContextModel)
        trial_greater = CABACContextModel.__new__(CABACContextModel)
        trial_first._state_index = first_context._state_index
        trial_first._value_mps = first_context._value_mps
        trial_greater._state_index = greater_one_context._state_index
        trial_greater._value_mps = greater_one_context._value_mps

        if not trial.decode_bin(trial_first):
            absolute_level = 1
        else:
            absolute_level = 2
            while absolute_level < 15 and trial.decode_bin(trial_greater):
                absolute_level += 1
            if absolute_level == 15:
                prefix_length = 0
                while trial.decode_bypass_bin():
                    prefix_length += 1
                    if prefix_length >= 23:
                        raise CABACError("CABAC coefficient level exceeds supported bypass prefix")
                suffix = 0
                for _ in range(prefix_length):
                    suffix = (suffix << 1) | int(trial.decode_bypass_bin())
                absolute_level = 14 + (1 << prefix_length) + suffix

        self._bits._bit_offset = trial._bits._bit_offset
        self._code_range = trial._code_range
        self._code_offset = trial._code_offset
        self._terminated = trial._terminated
        self._cavlc = getattr(trial, "_cavlc", None)
        first_context._state_index = trial_first._state_index
        first_context._value_mps = trial_first._value_mps
        greater_one_context._state_index = trial_greater._state_index
        greater_one_context._value_mps = trial_greater._value_mps
        return absolute_level - 1

    def decode_coeff_sign(self, abs_level_minus1: int) -> int:
        if not 0 <= abs_level_minus1 < 0x7FFFFFFF:
            raise CABACError("CABAC coefficient magnitude exceeds the signed output range")
        negative = self.decode_bypass_bin()
        magnitude = abs_level_minus1 + 1
        return -magnitude if negative else magnitude

    def decode_motion_vector_difference(
        self, neighbor_magnitude: int, contexts: list[CABACContextModel]
    ) -> int:
        if getattr(self, "_cavlc", None) is not None:
            return CAVLCSyntax.signed(self._bits)
        """Decode one MVD component using seven contexts starting at 40 or 47."""
        if (
            not isinstance(neighbor_magnitude, int)
            or isinstance(neighbor_magnitude, bool)
            or neighbor_magnitude < 0
        ):
            raise CABACError("MVD neighbor magnitude must be a nonnegative integer")
        if not isinstance(contexts, list) or len(contexts) != 7 or any(
            not isinstance(model, CABACContextModel)
            or not 0 <= model._state_index < 64
            for model in contexts
        ):
            raise CABACError("MVD decoding requires seven valid consecutive contexts")
        if len({id(model) for model in contexts}) != 7:
            raise CABACError("MVD contexts must be distinct")

        context_index = int(neighbor_magnitude >= 3) + int(neighbor_magnitude >= 33)
        return self._decode_motion_vector_difference_with_context(
            context_index, contexts
        )

    def decode_motion_vector_difference_for_partition(
        self,
        component: int,
        left: CABACInterNeighbor,
        top: CABACInterNeighbor,
        contexts: list[CABACContextModel],
        mbaff_frame: bool = False,
        current_is_field: bool = False,
    ) -> int:
        context_increment = derive_cabac_mvd_context_increment(
            left, top, component, mbaff_frame, current_is_field
        )
        return self._decode_motion_vector_difference_with_context(
            context_increment, contexts
        )

    def _decode_motion_vector_difference_with_context(
        self,
        context_index: int,
        contexts: list[CABACContextModel],
    ) -> int:
        if getattr(self, "_cavlc", None) is not None:
            return CAVLCSyntax.signed(self._bits)
        if not isinstance(contexts, list) or len(contexts) != 7 or any(
            not isinstance(model, CABACContextModel)
            or not 0 <= model._state_index < 64
            for model in contexts
        ):
            raise CABACError("MVD decoding requires seven valid consecutive contexts")
        if len({id(model) for model in contexts}) != 7:
            raise CABACError("MVD contexts must be distinct")
        trial_bits = BitReader(self._bits._data)
        trial_bits._bit_offset = self._bits._bit_offset
        trial = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
        trial._bits = trial_bits
        trial._code_range = self._code_range
        trial._code_offset = self._code_offset
        trial._terminated = self._terminated
        trial._cavlc = self._cavlc.clone() if getattr(self, "_cavlc", None) is not None else None
        trial_contexts = [CABACContextModel.__new__(CABACContextModel) for _ in range(7)]
        for target, source in zip(trial_contexts, contexts):
            target._state_index = source._state_index
            target._value_mps = source._value_mps

        if not trial.decode_bin(trial_contexts[context_index]):
            magnitude = 0
        else:
            magnitude = 1
            context_index = 3
            while magnitude < 9 and trial.decode_bin(trial_contexts[context_index]):
                if magnitude < 4:
                    context_index += 1
                magnitude += 1

            if magnitude >= 9:
                suffix_length = 3
                while trial.decode_bypass_bin():
                    if suffix_length > 30:
                        raise CABACError("CABAC MVD exceeds the signed output range")
                    increment = 1 << suffix_length
                    if magnitude > _MAX_MOTION_VECTOR_DIFFERENCE - increment:
                        raise CABACError("CABAC MVD exceeds the signed output range")
                    magnitude += increment
                    suffix_length += 1
                if suffix_length > 30:
                    raise CABACError("CABAC MVD exceeds the signed output range")
                for bit_index in range(suffix_length - 1, -1, -1):
                    if trial.decode_bypass_bin():
                        increment = 1 << bit_index
                        if magnitude > _MAX_MOTION_VECTOR_DIFFERENCE - increment:
                            raise CABACError("CABAC MVD exceeds the signed output range")
                        magnitude += increment

            negative = trial.decode_bypass_bin()
            if negative:
                magnitude = -magnitude

        self._bits._bit_offset = trial._bits._bit_offset
        self._code_range = trial._code_range
        self._code_offset = trial._code_offset
        self._terminated = trial._terminated
        self._cavlc = getattr(trial, "_cavlc", None)
        for target, source in zip(contexts, trial_contexts):
            target._state_index = source._state_index
            target._value_mps = source._value_mps
        return magnitude

    def decode_luma4x4_residual_levels(
        self,
        significance: list[bool],
        contexts: list[CABACContextModel],
    ) -> list[int]:
        if not isinstance(significance, list) or len(significance) != 16:
            raise CABACError("luma4x4 significance map must contain 16 scan positions")
        if not any(significance):
            raise CABACError("CABAC residual block has no significant coefficients")
        if not isinstance(contexts, list) or len(contexts) != 10 or any(
            not isinstance(model, CABACContextModel) or not 0 <= model._state_index < 64
            for model in contexts
        ):
            raise CABACError("luma4x4 residual decoding requires ten valid coefficient contexts")
        if len({id(model) for model in contexts}) != 10:
            raise CABACError("luma4x4 coefficient contexts must be distinct")

        trial_bits = BitReader(self._bits._data)
        trial_bits._bit_offset = self._bits._bit_offset
        trial = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
        trial._bits = trial_bits
        trial._code_range = self._code_range
        trial._code_offset = self._code_offset
        trial._terminated = self._terminated
        trial._cavlc = self._cavlc.clone() if getattr(self, "_cavlc", None) is not None else None
        trial_contexts = [CABACContextModel.__new__(CABACContextModel) for _ in range(10)]
        for target, source in zip(trial_contexts, contexts):
            target._state_index = source._state_index
            target._value_mps = source._value_mps

        levels = [0] * 16
        node_context = 0
        for scan_index in range(15, -1, -1):
            if not significance[scan_index]:
                continue
            level_minus1 = trial.decode_coeff_abs_level_minus1(
                trial_contexts[_COEFF_ABS_LEVEL1_CONTEXT[node_context]],
                trial_contexts[_COEFF_ABS_LEVEL_GREATER1_CONTEXT[node_context]],
            )
            levels[scan_index] = trial.decode_coeff_sign(level_minus1)
            if level_minus1 == 0:
                node_context = _COEFF_LEVEL1_TRANSITION[node_context]
            else:
                node_context = _COEFF_LEVEL_GREATER1_TRANSITION[node_context]

        self._bits._bit_offset = trial._bits._bit_offset
        self._code_range = trial._code_range
        self._code_offset = trial._code_offset
        self._terminated = trial._terminated
        self._cavlc = getattr(trial, "_cavlc", None)
        for target, source in zip(contexts, trial_contexts):
            target._state_index = source._state_index
            target._value_mps = source._value_mps
        return levels

    def decode_luma4x4_residual_block(
        self,
        significant_contexts: list[CABACContextModel],
        last_contexts: list[CABACContextModel],
        coefficient_contexts: list[CABACContextModel],
    ) -> list[int]:
        context_banks = (significant_contexts, last_contexts, coefficient_contexts)
        expected_lengths = (15, 15, 10)
        if any(
            not isinstance(bank, list)
            or len(bank) != expected_length
            or any(not isinstance(model, CABACContextModel) for model in bank)
            for bank, expected_length in zip(context_banks, expected_lengths)
        ):
            raise CABACError("luma4x4 residual block requires 15, 15, and 10 context models")
        if len({id(model) for bank in context_banks for model in bank}) != 40:
            raise CABACError("luma4x4 residual block contexts must be distinct")

        trial_bits = BitReader(self._bits._data)
        trial_bits._bit_offset = self._bits._bit_offset
        trial = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
        trial._bits = trial_bits
        trial._code_range = self._code_range
        trial._code_offset = self._code_offset
        trial._terminated = self._terminated
        trial._cavlc = self._cavlc.clone() if getattr(self, "_cavlc", None) is not None else None
        trial_banks = [
            [CABACContextModel.__new__(CABACContextModel) for _ in bank]
            for bank in context_banks
        ]
        for targets, sources in zip(trial_banks, context_banks):
            for target, source in zip(targets, sources):
                target._state_index = source._state_index
                target._value_mps = source._value_mps

        significance = trial.decode_luma4x4_significance_map(trial_banks[0], trial_banks[1])
        scan_levels = trial.decode_luma4x4_residual_levels(significance, trial_banks[2])
        raster_levels = place_luma4x4_scan_levels(scan_levels)

        self._bits._bit_offset = trial._bits._bit_offset
        self._code_range = trial._code_range
        self._code_offset = trial._code_offset
        self._terminated = trial._terminated
        self._cavlc = getattr(trial, "_cavlc", None)
        for targets, sources in zip(context_banks, trial_banks):
            for target, source in zip(targets, sources):
                target._state_index = source._state_index
                target._value_mps = source._value_mps
        return raster_levels

    def decode_luma4x4_residual_block_with_flag(
        self,
        left_nonzero: int,
        top_nonzero: int,
        coded_flag_contexts: list[CABACContextModel],
        significant_contexts: list[CABACContextModel],
        last_contexts: list[CABACContextModel],
        coefficient_contexts: list[CABACContextModel],
    ) -> tuple[list[int], bool]:
        context_banks = (
            coded_flag_contexts,
            significant_contexts,
            last_contexts,
            coefficient_contexts,
        )
        expected_lengths = (4, 15, 15, 10)
        if any(
            not isinstance(bank, list)
            or len(bank) != expected_length
            or any(
                not isinstance(model, CABACContextModel)
                or not 0 <= model._state_index < 64
                for model in bank
            )
            for bank, expected_length in zip(context_banks, expected_lengths)
        ):
            raise CABACError("luma4x4 residual syntax requires 4, 15, 15, and 10 valid contexts")
        if len({id(model) for bank in context_banks for model in bank}) != 44:
            raise CABACError("luma4x4 residual syntax contexts must be distinct")

        trial_bits = BitReader(self._bits._data)
        trial_bits._bit_offset = self._bits._bit_offset
        trial = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
        trial._bits = trial_bits
        trial._code_range = self._code_range
        trial._code_offset = self._code_offset
        trial._terminated = self._terminated
        trial._cavlc = self._cavlc.clone() if getattr(self, "_cavlc", None) is not None else None
        trial_banks = [
            [CABACContextModel.__new__(CABACContextModel) for _ in bank]
            for bank in context_banks
        ]
        for targets, sources in zip(trial_banks, context_banks):
            for target, source in zip(targets, sources):
                target._state_index = source._state_index
                target._value_mps = source._value_mps

        coded = trial.decode_luma4x4_coded_block_flag(
            left_nonzero, top_nonzero, trial_banks[0]
        )
        levels = (
            trial.decode_luma4x4_residual_block(*trial_banks[1:])
            if coded
            else [0] * 16
        )

        self._bits._bit_offset = trial._bits._bit_offset
        self._code_range = trial._code_range
        self._code_offset = trial._code_offset
        self._terminated = trial._terminated
        self._cavlc = getattr(trial, "_cavlc", None)
        for targets, sources in zip(context_banks, trial_banks):
            for target, source in zip(targets, sources):
                target._state_index = source._state_index
                target._value_mps = source._value_mps
        return levels, coded

    def decode_chroma4x4_ac_residual_block(
        self,
        left_has_nonzero: bool,
        top_has_nonzero: bool,
        coded_flag_contexts: list[CABACContextModel],
        significant_contexts: list[CABACContextModel],
        last_contexts: list[CABACContextModel],
        coefficient_contexts: list[CABACContextModel],
    ) -> tuple[list[int], bool]:
        """Decode one 4:2:0 chroma AC block, scanning coefficient positions 1-15."""
        context_banks = (
            coded_flag_contexts,
            significant_contexts,
            last_contexts,
            coefficient_contexts,
        )
        expected_lengths = (4, 15, 15, 10)
        if any(
            not isinstance(bank, list)
            or len(bank) != expected_length
            or any(not isinstance(model, CABACContextModel) or not 0 <= model._state_index < 64 for model in bank)
            for bank, expected_length in zip(context_banks, expected_lengths)
        ):
            raise CABACError("chroma AC residual syntax requires 4, 15, 15, and 10 valid contexts")
        if len({id(model) for bank in context_banks for model in bank}) != 44:
            raise CABACError("chroma AC residual syntax contexts must be distinct")

        trial_bits = BitReader(self._bits._data)
        trial_bits._bit_offset = self._bits._bit_offset
        trial = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
        trial._bits = trial_bits
        trial._code_range = self._code_range
        trial._code_offset = self._code_offset
        trial._terminated = self._terminated
        trial._cavlc = self._cavlc.clone() if getattr(self, "_cavlc", None) is not None else None
        trial_banks = [
            [CABACContextModel.__new__(CABACContextModel) for _ in bank]
            for bank in context_banks
        ]
        for targets, sources in zip(trial_banks, context_banks):
            for target, source in zip(targets, sources):
                target._state_index = source._state_index
                target._value_mps = source._value_mps

        coded = trial.decode_luma4x4_coded_block_flag(
            int(left_has_nonzero), int(top_has_nonzero), trial_banks[0]
        )
        raster_levels = [0] * 16
        if coded:
            significance = [False] * 16
            last_found = False
            for scan_index in range(14):
                if not trial.decode_bin(trial_banks[1][scan_index]):
                    continue
                significance[scan_index + 1] = True
                if trial.decode_bin(trial_banks[2][scan_index]):
                    last_found = True
                    break
            if not last_found:
                significance[15] = True
            scan_levels = trial.decode_luma4x4_residual_levels(
                significance, trial_banks[3]
            )
            raster_levels = place_chroma4x4_scan_levels(0, scan_levels[1:])

        self._bits._bit_offset = trial._bits._bit_offset
        self._code_range = trial._code_range
        self._code_offset = trial._code_offset
        self._terminated = trial._terminated
        self._cavlc = getattr(trial, "_cavlc", None)
        for targets, sources in zip(context_banks, trial_banks):
            for target, source in zip(targets, sources):
                target._state_index = source._state_index
                target._value_mps = source._value_mps
        return raster_levels, coded

    def decode_and_reconstruct_luma4x4_residual(
        self,
        left_nonzero: int,
        top_nonzero: int,
        coded_flag_contexts: list[CABACContextModel],
        significant_contexts: list[CABACContextModel],
        last_contexts: list[CABACContextModel],
        coefficient_contexts: list[CABACContextModel],
        scaling_list: Sequence[int],
        qpy: int,
    ) -> tuple[list[int], bool]:
        from .h264_reconstruction import inverse_scale_luma4x4, reconstruct_luma4x4_residual

        inverse_scale_luma4x4([0] * 16, scaling_list, qpy)
        levels, coded = self.decode_luma4x4_residual_block_with_flag(
            left_nonzero,
            top_nonzero,
            coded_flag_contexts,
            significant_contexts,
            last_contexts,
            coefficient_contexts,
        )
        if not coded:
            return [0] * 16, False
        return reconstruct_luma4x4_residual(levels, scaling_list, qpy), True

    def decode_residual_block(
        self,
        ctx_block_cat: int,
        cond_term_flag_a: bool,
        cond_term_flag_b: bool,
        contexts: list[CABACContextModel],
    ) -> tuple[list[int], bool]:
        if getattr(self, "_cavlc", None) is not None:
            return self._cavlc.residual(self._bits, ctx_block_cat)
        """Decode coded_block_flag and residual_block_cabac for ctxBlockCat 0-4 (4:2:0).

        Contexts are the whole-slice ctxIdx 0-459 list and update transactionally.
        Levels are in coefficient-list order: index i is scan position i for
        categories 0, 2, and 3, and i+1 for AC categories 1 and 4. Entries past
        maxNumCoeff are zero.
        """
        if isinstance(ctx_block_cat, int) and ctx_block_cat == 5:
            raise CABACError("CABAC syntax is unsupported for this ctxBlockCat")
        coded_base, significant_base, last_base, abs_base = residual_context_bases(ctx_block_cat)
        if coded_base is None:
            raise CABACError("coded_block_flag context is unavailable for this ctxBlockCat")
        max_num_coeff = _RESIDUAL_MAX_NUM_COEFF[ctx_block_cat]
        significant_context_count = 3 if ctx_block_cat == 3 else max_num_coeff - 1
        context_indices = (
            *range(coded_base, coded_base + 4),
            *range(significant_base, significant_base + significant_context_count),
            *range(last_base, last_base + significant_context_count),
            *range(abs_base, abs_base + 10),
        )
        _validate_slice_contexts(contexts, context_indices)
        trial, trial_contexts = self._slice_trial(contexts, context_indices)

        levels = [0] * 16
        coded_inc = int(bool(cond_term_flag_a)) + 2 * int(bool(cond_term_flag_b))
        coded = trial.decode_bin(trial_contexts[coded_base + coded_inc])
        if coded:
            significant = [False] * 16
            last_found = False
            for index in range(max_num_coeff - 1):
                # Chroma DC uses Min(numDecodAbsLevel / NumC8x8, 2) with NumC8x8 = 1.
                increment = min(index, 2) if ctx_block_cat == 3 else index
                if not trial.decode_bin(trial_contexts[significant_base + increment]):
                    continue
                significant[index] = True
                if trial.decode_bin(trial_contexts[last_base + increment]):
                    last_found = True
                    break
            if not last_found:
                significant[max_num_coeff - 1] = True
            trial._decode_coefficient_levels(
                trial_contexts[abs_base:abs_base + 10],
                3 if ctx_block_cat == 3 else 4,
                significant[:max_num_coeff],
                levels,
            )
        self._commit_slice_trial(trial, contexts, trial_contexts, context_indices)
        return levels, coded

    def decode_luma8x8_residual_block(self, contexts: list[CABACContextModel]) -> list[int]:
        if getattr(self, "_cavlc", None) is not None:
            return self._cavlc.residual8x8(self._bits)
        """Decode residual_block_cabac for ctxBlockCat 5 in a frame macroblock.

        coded_block_flag is not parsed because it is inferred to be 1 when
        ChromaArrayType != 3; call this only for 8x8 blocks whose coded_block_pattern
        bit is set. Returns 64 levels in 8x8 frame-scan list order.
        """
        _, significant_base, last_base, abs_base = residual_context_bases(5)
        context_indices = (
            *range(significant_base, significant_base + 15),
            *range(last_base, last_base + 9),
            *range(abs_base, abs_base + 10),
        )
        _validate_slice_contexts(contexts, context_indices)
        trial, trial_contexts = self._slice_trial(contexts)
        significant = [False] * 64
        last_found = False
        for index in range(63):
            if not trial.decode_bin(
                trial_contexts[significant_base + _LUMA8X8_FRAME_SIGNIFICANT_INC[index]]
            ):
                continue
            significant[index] = True
            if trial.decode_bin(trial_contexts[last_base + _LUMA8X8_LAST_INC[index]]):
                last_found = True
                break
        if not last_found:
            significant[63] = True
        levels = [0] * 64
        trial._decode_coefficient_levels(trial_contexts[abs_base:abs_base + 10], 4, significant, levels)
        self._commit_slice_trial(trial, contexts, trial_contexts)
        return levels

    def _decode_coefficient_levels(
        self,
        abs_contexts: list[CABACContextModel],
        greater_cap: int,
        significant: list[bool],
        levels: list[int],
    ) -> None:
        """Decode signed levels in reverse list order with clause 9.3.3.1.3 contexts."""
        equal_one = greater_one = 0
        for index in range(len(significant) - 1, -1, -1):
            if not significant[index]:
                continue
            first_inc = 0 if greater_one else min(4, 1 + equal_one)
            greater_inc = 5 + min(greater_cap, greater_one)
            level_minus1 = self.decode_coeff_abs_level_minus1(
                abs_contexts[first_inc], abs_contexts[greater_inc]
            )
            levels[index] = self.decode_coeff_sign(level_minus1)
            if level_minus1 == 0:
                equal_one += 1
            else:
                greater_one += 1

    def _slice_trial(
        self,
        contexts: list[CABACContextModel],
        context_indices: Sequence[int] | None = None,
    ) -> tuple["CABACArithmeticDecoder", list[CABACContextModel]]:
        timeout_check = getattr(self, "_timeout_check", None)
        if timeout_check is not None:
            timeout_check()
        trial_bits = BitReader(self._bits._data)
        trial_bits._bit_offset = self._bits._bit_offset
        trial = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
        trial._bits = trial_bits
        trial._timeout_check = timeout_check
        trial._code_range = self._code_range
        trial._code_offset = self._code_offset
        trial._terminated = self._terminated
        trial._cavlc = self._cavlc.clone() if getattr(self, "_cavlc", None) is not None else None
        trial_contexts = contexts.copy()
        indices = range(len(contexts)) if context_indices is None else context_indices
        for index in indices:
            source = contexts[index]
            model = CABACContextModel.__new__(CABACContextModel)
            model._state_index = source._state_index
            model._value_mps = source._value_mps
            trial_contexts[index] = model
        return trial, trial_contexts

    def _commit_slice_trial(
        self,
        trial: "CABACArithmeticDecoder",
        contexts: list[CABACContextModel],
        trial_contexts: list[CABACContextModel],
        context_indices: Sequence[int] | None = None,
    ) -> None:
        self._bits._bit_offset = trial._bits._bit_offset
        self._code_range = trial._code_range
        self._code_offset = trial._code_offset
        self._terminated = trial._terminated
        self._cavlc = getattr(trial, "_cavlc", None)
        if context_indices is None:
            context_pairs = zip(contexts, trial_contexts)
        else:
            context_pairs = ((contexts[index], trial_contexts[index]) for index in context_indices)
        for target, source in context_pairs:
            target._state_index = source._state_index
            target._value_mps = source._value_mps

    def decode_bypass_bin(self) -> bool:
        self._validate_bin_state()
        start_offset = self._bits._bit_offset
        try:
            bit = self._bits.read_bit()
        except BitstreamError as error:
            self._bits._bit_offset = start_offset
            raise CABACError(f"CABAC bypass bin: {error}") from error
        code_offset = (self._code_offset << 1) | int(bit)
        decoded = code_offset >= self._code_range
        if decoded:
            code_offset -= self._code_range
        self._code_offset = code_offset
        return decoded

    def decode_terminate_bin(self) -> bool:
        if getattr(self, "_cavlc", None) is not None:
            return self._cavlc.finished(self._bits)
        self._validate_bin_state()
        start_offset = self._bits._bit_offset
        original_range, original_offset = self._code_range, self._code_offset
        self._code_range -= 2
        if self._code_offset >= self._code_range:
            self._terminated = True
            return True
        try:
            self.renormalize()
        except CABACError:
            self._code_range, self._code_offset = original_range, original_offset
            self._bits._bit_offset = start_offset
            raise
        return False

    def _validate_bin_state(self) -> None:
        if self._terminated:
            raise CABACTerminatedError("CABAC decoder is already terminated")
        if not 256 <= self._code_range <= self.INITIAL_RANGE:
            raise CABACError("CABAC bin decoding range is outside [256,510]")
        if self._code_offset >= self._code_range:
            raise CABACError("CABAC code offset is outside the arithmetic range")

    def renormalize(self) -> None:
        if getattr(self, "_terminated", False):
            raise CABACTerminatedError("CABAC decoder is already terminated")
        if not 0 < self._code_range <= self.INITIAL_RANGE:
            raise CABACError("CABAC arithmetic range is outside [1,510]")
        start_offset = self._bits._bit_offset
        code_range = self._code_range
        code_offset = self._code_offset
        try:
            while code_range < 256:
                bit = self._bits.read_bit()
                code_range <<= 1
                code_offset = (code_offset << 1) | int(bit)
                if code_offset >= code_range:
                    raise CABACError("CABAC code offset is outside the arithmetic range")
        except (BitstreamError, CABACError) as error:
            self._bits._bit_offset = start_offset
            if isinstance(error, CABACError):
                raise
            raise CABACError(f"CABAC renormalization: {error}") from error
        self._code_range = code_range
        self._code_offset = code_offset


def place_luma4x4_scan_levels(scan_levels: list[int]) -> list[int]:
    """Map frame-scan luma 4x4 coefficient levels into raster order."""
    if not isinstance(scan_levels, list) or len(scan_levels) != 16:
        raise CABACError("luma4x4 scan levels must contain 16 positions")
    raster_levels = [0] * 16
    for scan_index, raster_index in enumerate(_LUMA4X4_SCAN_TO_RASTER):
        raster_levels[raster_index] = scan_levels[scan_index]
    return raster_levels


def place_chroma4x4_scan_levels(dc_level: int, ac_scan_levels: list[int]) -> list[int]:
    """Insert scaled chroma DC before inverse-scanning 15 AC levels to raster order."""
    if (
        not isinstance(dc_level, int)
        or isinstance(dc_level, bool)
        or not isinstance(ac_scan_levels, list)
        or len(ac_scan_levels) != 15
        or any(not isinstance(level, int) or isinstance(level, bool) for level in ac_scan_levels)
    ):
        raise CABACError("chroma4x4 levels require one integer DC and 15 integer AC positions")
    return place_luma4x4_scan_levels([dc_level, *ac_scan_levels])