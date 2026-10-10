"""H.264 coefficient reconstruction primitives."""

from collections.abc import Sequence
from dataclasses import dataclass
from enum import Enum
from typing import Optional, Sequence


from .cabac import place_chroma4x4_scan_levels
from .slice import RefPicListModification


class InverseScaleError(ValueError):
    """Raised when inverse-scaling inputs are invalid."""


class ChromaQPError(ValueError):
    """Raised when chroma quantization-parameter inputs are invalid."""


class InverseTransformError(ValueError):
    """Raised when inverse-transform inputs are invalid."""


class IntraPredictionError(ValueError):
    """Raised when intra-prediction reference samples are invalid."""


class InterPredictionError(ValueError):
    """Raised when inter-prediction reference samples are invalid."""


class PictureOrderCountError(ValueError):
    """Raised when picture-order-count inputs or arithmetic are invalid."""


class DeblockingError(ValueError):
    """Raised when luma deblocking parameters are invalid."""


class LumaDeblockingMode(Enum):
    ALL_EDGES = "all_edges"
    DISABLED = "disabled"
    ALL_EXCEPT_SLICE_BOUNDARIES = "all_except_slice_boundaries"


@dataclass(frozen=True)
class LumaDeblockingParameters:
    mode: LumaDeblockingMode
    index_a: int
    index_b: int


@dataclass(frozen=True)
class LumaDeblockingMacroblock:
    x: int
    y: int
    left_neighbor_available: bool
    top_neighbor_available: bool
    transform_size_8x8: bool
    left_slice_boundary: bool
    top_slice_boundary: bool
    left_strengths: tuple[int, int, int, int]
    top_strengths: tuple[int, int, int, int]
    vertical_internal_strengths: tuple[tuple[int, int, int, int], ...]
    horizontal_internal_strengths: tuple[tuple[int, int, int, int], ...]


@dataclass(frozen=True)
class LumaDeblockingEdgeFlags:
    filter_left: bool
    filter_top: bool
    filter_internal: bool


@dataclass(frozen=True)
class LumaDeblockingNeighbors:
    left_index: Optional[int]
    top_index: Optional[int]
    left_available: bool
    top_available: bool
    left_slice_boundary: bool
    top_slice_boundary: bool


@dataclass(frozen=True)
class LumaDeblockingThresholds:
    alpha: int
    beta: int


@dataclass(frozen=True)
class LumaEdgeSamples:
    p0: int
    p1: int
    p2: int
    q0: int
    q1: int
    q2: int


@dataclass(frozen=True)
class ChromaEdgeSamples:
    p0: int
    p1: int
    q0: int
    q1: int


def filter_chroma_weak_edge(samples: ChromaEdgeSamples, tc0: int) -> ChromaEdgeSamples:
    """Apply the 8-bit chroma weak-edge equations for bS 1-3."""
    if not isinstance(samples, ChromaEdgeSamples):
        raise DeblockingError("chroma weak-edge filter inputs are invalid")
    values = (samples.p0, samples.p1, samples.q0, samples.q1, tc0)
    if any(not isinstance(value, int) or isinstance(value, bool) or not 0 <= value <= 255 for value in values):
        raise DeblockingError("chroma weak-edge filter inputs are invalid")

    tc = tc0 + 1
    delta = (((samples.q0 - samples.p0) << 2) + (samples.p1 - samples.q1) + 4) >> 3
    delta = min(tc, max(-tc, delta))
    p0 = min(255, max(0, samples.p0 + delta))
    q0 = min(255, max(0, samples.q0 - delta))
    return ChromaEdgeSamples(p0, samples.p1, q0, samples.q1)


def filter_luma_weak_edge(
    samples: LumaEdgeSamples, beta: int, tc0: int
) -> LumaEdgeSamples:
    """Apply the 8-bit luma weak-edge equations after filterSamplesFlag passes."""
    if not isinstance(samples, LumaEdgeSamples):
        raise DeblockingError("luma weak-edge filter inputs are invalid")
    values = (samples.p0, samples.p1, samples.p2, samples.q0, samples.q1, samples.q2, beta, tc0)
    if any(not isinstance(value, int) or isinstance(value, bool) or not 0 <= value <= 255 for value in values):
        raise DeblockingError("luma weak-edge filter inputs are invalid")

    ap = abs(samples.p2 - samples.p0)
    aq = abs(samples.q2 - samples.q0)
    tc = tc0 + int(ap < beta) + int(aq < beta)
    delta = (((samples.q0 - samples.p0) << 2) + (samples.p1 - samples.q1) + 4) >> 3
    delta = min(tc, max(-tc, delta))
    p0 = min(255, max(0, samples.p0 + delta))
    q0 = min(255, max(0, samples.q0 - delta))
    p1 = samples.p1
    q1 = samples.q1
    if ap < beta:
        delta_p1 = (samples.p2 + ((samples.p0 + samples.q0 + 1) >> 1) - (samples.p1 << 1)) >> 1
        delta_p1 = min(tc0, max(-tc0, delta_p1))
        p1 = min(255, max(0, samples.p1 + delta_p1))
    if aq < beta:
        delta_q1 = (samples.q2 + ((samples.p0 + samples.q0 + 1) >> 1) - (samples.q1 << 1)) >> 1
        delta_q1 = min(tc0, max(-tc0, delta_q1))
        q1 = min(255, max(0, samples.q1 + delta_q1))
    return LumaEdgeSamples(p0, p1, samples.p2, q0, q1, samples.q2)


@dataclass(frozen=True)
class LumaStrongEdgeSamples:
    p0: int
    p1: int
    p2: int
    p3: int
    q0: int
    q1: int
    q2: int
    q3: int


def filter_luma_strong_edge(
    samples: LumaStrongEdgeSamples, alpha: int, beta: int
) -> LumaEdgeSamples:
    """Apply the 8-bit luma bS=4 filter with independent p- and q-side tests."""
    if not isinstance(samples, LumaStrongEdgeSamples):
        raise DeblockingError("luma strong-edge filter inputs are invalid")
    values = (
        samples.p0, samples.p1, samples.p2, samples.p3,
        samples.q0, samples.q1, samples.q2, samples.q3, alpha, beta,
    )
    if any(not isinstance(value, int) or isinstance(value, bool) or not 0 <= value <= 255 for value in values):
        raise DeblockingError("luma strong-edge filter inputs are invalid")

    strong_limit = (alpha >> 2) + 2
    alpha_passes = abs(samples.p0 - samples.q0) < strong_limit
    p0, p1, p2 = samples.p0, samples.p1, samples.p2
    q0, q1, q2 = samples.q0, samples.q1, samples.q2
    if abs(samples.p2 - samples.p0) < beta and alpha_passes:
        p0 = (samples.p2 + 2 * samples.p1 + 2 * samples.p0 + 2 * samples.q0 + samples.q1 + 4) >> 3
        p1 = (samples.p2 + samples.p1 + samples.p0 + samples.q0 + 2) >> 2
        p2 = (2 * samples.p3 + 3 * samples.p2 + samples.p1 + samples.p0 + samples.q0 + 4) >> 3
    else:
        p0 = (2 * samples.p1 + samples.p0 + samples.q1 + 2) >> 2
    if abs(samples.q2 - samples.q0) < beta and alpha_passes:
        q0 = (samples.p1 + 2 * samples.p0 + 2 * samples.q0 + 2 * samples.q1 + samples.q2 + 4) >> 3
        q1 = (samples.p0 + samples.q0 + samples.q1 + samples.q2 + 2) >> 2
        q2 = (2 * samples.q3 + 3 * samples.q2 + samples.q1 + samples.q0 + samples.p0 + 4) >> 3
    else:
        q0 = (2 * samples.q1 + samples.q0 + samples.p1 + 2) >> 2
    return LumaEdgeSamples(*(min(255, max(0, value)) for value in (p0, p1, p2, q0, q1, q2)))


_LUMA_ALPHA_TABLE = (
    0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
    4, 4, 5, 6, 7, 8, 9, 10, 12, 13, 15, 17, 20, 22, 25, 28,
    32, 36, 40, 45, 50, 56, 63, 71, 80, 90, 101, 113, 127, 144, 162, 182,
    203, 226, 255, 255,
)
_LUMA_BETA_TABLE = (
    0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
    2, 2, 2, 3, 3, 3, 3, 4, 4, 4, 6, 6, 7, 7,
    8, 8, 9, 9, 10, 10, 11, 11, 12, 12, 13, 13, 14, 14, 15, 15,
    16, 16, 17, 18,
)
_LUMA_TC0_TABLE = (
    (
        0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
        1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 2, 2, 2, 2, 3, 3, 3, 4, 4, 4, 5, 6, 6, 7,
        8, 9, 10, 11, 13,
    ),
    (
        0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 1,
        1, 1, 1, 1, 1, 1, 1, 1, 2, 2, 2, 2, 3, 3, 3, 4, 4, 5, 5, 6, 7, 8, 8, 10,
        11, 12, 13, 15, 17,
    ),
    (
        0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 1, 1, 1,
        1, 1, 1, 1, 2, 2, 2, 2, 3, 3, 3, 4, 4, 4, 5, 6, 6, 7, 8, 9, 10, 11, 13,
        14, 16, 18, 20, 23, 25,
    ),
)


def lookup_luma_deblocking_thresholds(index_a: int, index_b: int) -> LumaDeblockingThresholds:
    if (
        not isinstance(index_a, int)
        or isinstance(index_a, bool)
        or not isinstance(index_b, int)
        or isinstance(index_b, bool)
        or not 0 <= index_a <= 51
        or not 0 <= index_b <= 51
    ):
        raise DeblockingError("luma deblocking table index is outside [0,51]")
    return LumaDeblockingThresholds(_LUMA_ALPHA_TABLE[index_a], _LUMA_BETA_TABLE[index_b])


def lookup_luma_tc0(index_a: int, boundary_strength: int) -> int:
    if (
        not isinstance(index_a, int)
        or isinstance(index_a, bool)
        or not 0 <= index_a <= 51
        or not isinstance(boundary_strength, int)
        or isinstance(boundary_strength, bool)
        or not 1 <= boundary_strength <= 3
    ):
        raise DeblockingError("luma tC0 table index is invalid")
    return _LUMA_TC0_TABLE[boundary_strength - 1][index_a]


def should_filter_luma_edge(
    p0: int,
    q0: int,
    p1: int,
    q1: int,
    boundary_strength: int,
    thresholds: LumaDeblockingThresholds,
) -> bool:
    samples = (p0, q0, p1, q1)
    if (
        any(not isinstance(sample, int) or isinstance(sample, bool) or not 0 <= sample <= 255 for sample in samples)
        or not isinstance(boundary_strength, int)
        or isinstance(boundary_strength, bool)
        or not 0 <= boundary_strength <= 4
        or not isinstance(thresholds, LumaDeblockingThresholds)
        or not 0 <= thresholds.alpha <= 255
        or not 0 <= thresholds.beta <= 255
    ):
        raise DeblockingError("luma edge-filter inputs are invalid")
    if boundary_strength == 0:
        return False
    return (
        abs(p0 - q0) < thresholds.alpha
        and abs(p1 - p0) < thresholds.beta
        and abs(q1 - q0) < thresholds.beta
    )


def apply_luma_deblocking_edge(
    samples: LumaStrongEdgeSamples,
    boundary_strength: int,
    parameters: LumaDeblockingParameters,
    slice_boundary: bool,
) -> LumaEdgeSamples:
    if (
        not isinstance(samples, LumaStrongEdgeSamples)
        or not isinstance(parameters, LumaDeblockingParameters)
        or not isinstance(parameters.mode, LumaDeblockingMode)
        or any(
            not isinstance(value, int) or isinstance(value, bool) or not 0 <= value <= 255
            for value in (
                samples.p0,
                samples.p1,
                samples.p2,
                samples.p3,
                samples.q0,
                samples.q1,
                samples.q2,
                samples.q3,
            )
        )
        or not isinstance(slice_boundary, bool)
        or not isinstance(boundary_strength, int)
        or isinstance(boundary_strength, bool)
        or not isinstance(parameters.index_a, int)
        or isinstance(parameters.index_a, bool)
        or not isinstance(parameters.index_b, int)
        or isinstance(parameters.index_b, bool)
        or not 0 <= parameters.index_a <= 51
        or not 0 <= parameters.index_b <= 51
    ):
        raise DeblockingError("luma deblocking edge inputs are invalid")
    if not 0 <= boundary_strength <= 4:
        raise DeblockingError("luma deblocking boundary strength is outside [0,4]")

    unchanged = LumaEdgeSamples(
        samples.p0, samples.p1, samples.p2, samples.q0, samples.q1, samples.q2
    )
    if (
        parameters.mode is LumaDeblockingMode.DISABLED
        or (
            parameters.mode is LumaDeblockingMode.ALL_EXCEPT_SLICE_BOUNDARIES
            and slice_boundary
        )
        or boundary_strength == 0
    ):
        return unchanged

    thresholds = lookup_luma_deblocking_thresholds(parameters.index_a, parameters.index_b)
    if not should_filter_luma_edge(
        samples.p0,
        samples.q0,
        samples.p1,
        samples.q1,
        boundary_strength,
        thresholds,
    ):
        return unchanged
    if boundary_strength == 4:
        return filter_luma_strong_edge(samples, thresholds.alpha, thresholds.beta)
    tc0 = lookup_luma_tc0(parameters.index_a, boundary_strength)
    return filter_luma_weak_edge(unchanged, thresholds.beta, tc0)


def apply_luma_deblocking_edge_segment(
    samples: Sequence[LumaStrongEdgeSamples],
    boundary_strength: int,
    parameters: LumaDeblockingParameters,
    slice_boundary: bool,
) -> tuple[LumaEdgeSamples, LumaEdgeSamples, LumaEdgeSamples, LumaEdgeSamples]:
    if not isinstance(samples, Sequence) or isinstance(samples, (str, bytes)) or len(samples) != 4:
        raise DeblockingError("luma edge segment must contain exactly four sample sets")
    return tuple(
        apply_luma_deblocking_edge(edge_samples, boundary_strength, parameters, slice_boundary)
        for edge_samples in samples
    )


def apply_luma_deblocking_plane_edge_segment(
    plane: bytearray,
    width: int,
    height: int,
    stride: int,
    x: int,
    y: int,
    vertical: bool,
    boundary_strength: int,
    parameters: LumaDeblockingParameters,
    slice_boundary: bool,
) -> None:
    """Filter four adjacent plane samples; (x, y) is the first q0 location."""
    values = (width, height, stride, x, y)
    if (
        not isinstance(plane, bytearray)
        or any(not isinstance(value, int) or isinstance(value, bool) for value in values)
        or not isinstance(vertical, bool)
        or not isinstance(slice_boundary, bool)
        or width <= 0
        or height <= 0
        or stride < width
        or len(plane) < (height - 1) * stride + width
        or x < 0
        or y < 0
    ):
        raise DeblockingError("luma deblocking plane layout or edge coordinates are invalid")
    if vertical:
        if width < 8 or height < 4 or x < 4 or x > width - 4 or y > height - 4:
            raise DeblockingError("luma deblocking plane layout or edge coordinates are invalid")
    elif width < 4 or height < 8 or x > width - 4 or y < 4 or y > height - 4:
        raise DeblockingError("luma deblocking plane layout or edge coordinates are invalid")

    edge_samples = []
    for lane in range(4):
        q0_index = (y + lane) * stride + x if vertical else y * stride + x + lane
        if vertical:
            edge_samples.append(
                LumaStrongEdgeSamples(
                    plane[q0_index - 1],
                    plane[q0_index - 2],
                    plane[q0_index - 3],
                    plane[q0_index - 4],
                    plane[q0_index],
                    plane[q0_index + 1],
                    plane[q0_index + 2],
                    plane[q0_index + 3],
                )
            )
        else:
            edge_samples.append(
                LumaStrongEdgeSamples(
                    plane[q0_index - stride],
                    plane[q0_index - 2 * stride],
                    plane[q0_index - 3 * stride],
                    plane[q0_index - 4 * stride],
                    plane[q0_index],
                    plane[q0_index + stride],
                    plane[q0_index + 2 * stride],
                    plane[q0_index + 3 * stride],
                )
            )

    filtered = apply_luma_deblocking_edge_segment(
        edge_samples, boundary_strength, parameters, slice_boundary
    )
    for lane, result in enumerate(filtered):
        q0_index = (y + lane) * stride + x if vertical else y * stride + x + lane
        if vertical:
            plane[q0_index - 1] = result.p0
            plane[q0_index - 2] = result.p1
            plane[q0_index - 3] = result.p2
            plane[q0_index] = result.q0
            plane[q0_index + 1] = result.q1
            plane[q0_index + 2] = result.q2
        else:
            plane[q0_index - stride] = result.p0
            plane[q0_index - 2 * stride] = result.p1
            plane[q0_index - 3 * stride] = result.p2
            plane[q0_index] = result.q0
            plane[q0_index + stride] = result.q1
            plane[q0_index + 2 * stride] = result.q2


def apply_luma_deblocking_plane_macroblock_edge(
    plane: bytearray,
    width: int,
    height: int,
    stride: int,
    x: int,
    y: int,
    vertical: bool,
    boundary_strengths: Sequence[int],
    parameters: LumaDeblockingParameters,
    slice_boundary: bool,
) -> None:
    values = (width, height, stride, x, y)
    if (
        not isinstance(plane, bytearray)
        or any(not isinstance(value, int) or isinstance(value, bool) for value in values)
        or not isinstance(vertical, bool)
        or not isinstance(slice_boundary, bool)
        or not isinstance(boundary_strengths, Sequence)
        or isinstance(boundary_strengths, (str, bytes))
        or len(boundary_strengths) != 4
        or not isinstance(parameters, LumaDeblockingParameters)
        or not isinstance(parameters.mode, LumaDeblockingMode)
        or not isinstance(parameters.index_a, int)
        or isinstance(parameters.index_a, bool)
        or not isinstance(parameters.index_b, int)
        or isinstance(parameters.index_b, bool)
        or not 0 <= parameters.index_a <= 51
        or not 0 <= parameters.index_b <= 51
        or any(
            not isinstance(strength, int)
            or isinstance(strength, bool)
            or not 0 <= strength <= 4
            for strength in boundary_strengths
        )
        or width <= 0
        or height <= 0
        or stride < width
        or len(plane) < (height - 1) * stride + width
        or x < 0
        or y < 0
    ):
        raise DeblockingError("luma macroblock edge inputs are invalid")
    if vertical:
        if width < 8 or height < 16 or x < 4 or x > width - 4 or y > height - 16:
            raise DeblockingError("luma deblocking plane layout or edge coordinates are invalid")
    elif width < 16 or height < 8 or x > width - 16 or y < 4 or y > height - 4:
        raise DeblockingError("luma deblocking plane layout or edge coordinates are invalid")

    for segment, boundary_strength in enumerate(boundary_strengths):
        segment_x = x if vertical else x + 4 * segment
        segment_y = y + 4 * segment if vertical else y
        apply_luma_deblocking_plane_edge_segment(
            plane,
            width,
            height,
            stride,
            segment_x,
            segment_y,
            vertical,
            boundary_strength,
            parameters,
            slice_boundary,
        )


def apply_luma_deblocking_macroblock(
    plane: bytearray,
    width: int,
    height: int,
    stride: int,
    macroblock: LumaDeblockingMacroblock,
    parameters: LumaDeblockingParameters,
) -> None:
    if not isinstance(macroblock, LumaDeblockingMacroblock):
        raise DeblockingError("luma macroblock edge inputs are invalid")
    if not isinstance(parameters, LumaDeblockingParameters) or not isinstance(parameters.mode, LumaDeblockingMode):
        raise DeblockingError("luma deblocking parameters are invalid")
    edge_flags = derive_luma_deblocking_edge_flags(
        parameters.mode,
        macroblock.left_neighbor_available,
        macroblock.top_neighbor_available,
        macroblock.left_slice_boundary,
        macroblock.top_slice_boundary,
    )
    flags = (
        macroblock.transform_size_8x8,
        macroblock.left_slice_boundary,
        macroblock.top_slice_boundary,
    )
    values = (width, height, stride, macroblock.x, macroblock.y)
    if (
        any(not isinstance(flag, bool) for flag in flags)
        or not isinstance(plane, bytearray)
        or any(not isinstance(value, int) or isinstance(value, bool) for value in values)
        or width <= 0
        or height <= 0
        or stride < width
        or len(plane) < (height - 1) * stride + width
        or macroblock.x < 0
        or macroblock.y < 0
        or macroblock.x > width - 16
        or macroblock.y > height - 16
        or not isinstance(parameters.index_a, int)
        or isinstance(parameters.index_a, bool)
        or not isinstance(parameters.index_b, int)
        or isinstance(parameters.index_b, bool)
        or not 0 <= parameters.index_a <= 51
        or not 0 <= parameters.index_b <= 51
    ):
        raise DeblockingError("luma macroblock edge inputs are invalid")
    if (edge_flags.filter_left and macroblock.x < 4) or (edge_flags.filter_top and macroblock.y < 4):
        raise DeblockingError("luma deblocking plane layout or edge coordinates are invalid")

    active_edges = []
    if edge_flags.filter_left:
        active_edges.append(macroblock.left_strengths)
    if edge_flags.filter_top:
        active_edges.append(macroblock.top_strengths)
    if edge_flags.filter_internal:
        active_edges.extend(macroblock.vertical_internal_strengths)
        active_edges.extend(macroblock.horizontal_internal_strengths)
    if any(
        not isinstance(edge, Sequence)
        or isinstance(edge, (str, bytes))
        or len(edge) != 4
        or any(
            not isinstance(strength, int)
            or isinstance(strength, bool)
            or not 0 <= strength <= 4
            for strength in edge
        )
        for edge in active_edges
    ):
        raise DeblockingError("luma boundary strength is outside [0,4]")
    if edge_flags.filter_internal and (
        len(macroblock.vertical_internal_strengths) != 3
        or len(macroblock.horizontal_internal_strengths) != 3
    ):
        raise DeblockingError("luma macroblock edge inputs are invalid")

    if parameters.mode is LumaDeblockingMode.DISABLED:
        return
    if edge_flags.filter_left:
        apply_luma_deblocking_plane_macroblock_edge(
            plane, width, height, stride, macroblock.x, macroblock.y, True,
            macroblock.left_strengths, parameters, macroblock.left_slice_boundary,
        )
    if edge_flags.filter_internal:
        for edge in range(3):
            if macroblock.transform_size_8x8 and edge != 1:
                continue
            apply_luma_deblocking_plane_macroblock_edge(
                plane, width, height, stride, macroblock.x + 4 * (edge + 1), macroblock.y, True,
                macroblock.vertical_internal_strengths[edge], parameters, False,
            )
    if edge_flags.filter_top:
        apply_luma_deblocking_plane_macroblock_edge(
            plane, width, height, stride, macroblock.x, macroblock.y, False,
            macroblock.top_strengths, parameters, macroblock.top_slice_boundary,
        )
    if edge_flags.filter_internal:
        for edge in range(3):
            if macroblock.transform_size_8x8 and edge != 1:
                continue
            apply_luma_deblocking_plane_macroblock_edge(
                plane, width, height, stride, macroblock.x, macroblock.y + 4 * (edge + 1), False,
                macroblock.horizontal_internal_strengths[edge], parameters, False,
            )


def derive_luma_deblocking_edge_flags(
    mode: LumaDeblockingMode,
    left_neighbor_available: bool,
    top_neighbor_available: bool,
    left_slice_boundary: bool,
    top_slice_boundary: bool,
) -> LumaDeblockingEdgeFlags:
    if (
        not isinstance(mode, LumaDeblockingMode)
        or any(
            not isinstance(flag, bool)
            for flag in (
                left_neighbor_available,
                top_neighbor_available,
                left_slice_boundary,
                top_slice_boundary,
            )
        )
    ):
        raise DeblockingError("luma macroblock edge inputs are invalid")
    if mode is LumaDeblockingMode.DISABLED:
        return LumaDeblockingEdgeFlags(False, False, False)
    suppress_slice_boundaries = mode is LumaDeblockingMode.ALL_EXCEPT_SLICE_BOUNDARIES
    return LumaDeblockingEdgeFlags(
        left_neighbor_available and not (suppress_slice_boundaries and left_slice_boundary),
        top_neighbor_available and not (suppress_slice_boundaries and top_slice_boundary),
        True,
    )


def derive_luma_deblocking_neighbors(
    macroblock_index: int,
    picture_width_in_macroblocks: int,
    picture_height_in_macroblocks: int,
    slice_ids: Sequence[int],
) -> LumaDeblockingNeighbors:
    values = (macroblock_index, picture_width_in_macroblocks, picture_height_in_macroblocks)
    if (
        any(not isinstance(value, int) or isinstance(value, bool) for value in values)
        or macroblock_index < 0
        or picture_width_in_macroblocks <= 0
        or picture_height_in_macroblocks <= 0
        or not isinstance(slice_ids, Sequence)
        or isinstance(slice_ids, (str, bytes))
    ):
        raise DeblockingError("luma deblocking macroblock address or slice map is invalid")
    picture_size = picture_width_in_macroblocks * picture_height_in_macroblocks
    if macroblock_index >= picture_size or len(slice_ids) != picture_size or any(
        not isinstance(slice_id, int) or isinstance(slice_id, bool) or slice_id < 0
        for slice_id in slice_ids
    ):
        raise DeblockingError("luma deblocking macroblock address or slice map is invalid")

    left_index = macroblock_index - 1 if macroblock_index % picture_width_in_macroblocks else None
    top_index = macroblock_index - picture_width_in_macroblocks if macroblock_index >= picture_width_in_macroblocks else None
    return LumaDeblockingNeighbors(
        left_index,
        top_index,
        left_index is not None,
        top_index is not None,
        left_index is not None and slice_ids[left_index] != slice_ids[macroblock_index],
        top_index is not None and slice_ids[top_index] != slice_ids[macroblock_index],
    )

def resolve_luma_deblocking_macroblock(
    macroblock_index: int,
    picture_width_in_macroblocks: int,
    picture_height_in_macroblocks: int,
    slice_ids: Sequence[int],
    macroblock: LumaDeblockingMacroblock,
) -> LumaDeblockingMacroblock:
    neighbors = derive_luma_deblocking_neighbors(
        macroblock_index,
        picture_width_in_macroblocks,
        picture_height_in_macroblocks,
        slice_ids,
    )
    if not isinstance(macroblock, LumaDeblockingMacroblock):
        raise DeblockingError("luma macroblock edge inputs are invalid")
    column = macroblock_index % picture_width_in_macroblocks
    row = macroblock_index // picture_width_in_macroblocks
    return LumaDeblockingMacroblock(
        x=column * 16,
        y=row * 16,
        left_neighbor_available=neighbors.left_available,
        top_neighbor_available=neighbors.top_available,
        transform_size_8x8=macroblock.transform_size_8x8,
        left_slice_boundary=neighbors.left_slice_boundary,
        top_slice_boundary=neighbors.top_slice_boundary,
        left_strengths=macroblock.left_strengths,
        top_strengths=macroblock.top_strengths,
        vertical_internal_strengths=macroblock.vertical_internal_strengths,
        horizontal_internal_strengths=macroblock.horizontal_internal_strengths,
    )


def derive_luma_boundary_strength(
    macroblock_edge: bool,
    either_intra: bool,
    either_has_nonzero_coefficients: bool,
    inter_prediction_differs: bool,
) -> int:
    values = (
        macroblock_edge,
        either_intra,
        either_has_nonzero_coefficients,
        inter_prediction_differs,
    )
    if any(not isinstance(value, bool) for value in values):
        raise DeblockingError("luma boundary-strength inputs are invalid")
    if either_intra:
        return 4 if macroblock_edge else 3
    if either_has_nonzero_coefficients:
        return 2
    if inter_prediction_differs:
        return 1
    return 0


def luma_inter_prediction_differs(
    prediction_p: Sequence["LumaPredictionVector"],
    prediction_q: Sequence["LumaPredictionVector"],
) -> bool:
    predictions = (prediction_p, prediction_q)
    if any(not isinstance(items, Sequence) for items in predictions):
        raise DeblockingError("luma inter-prediction input is invalid")
    if any(len(items) > 2 for items in predictions):
        raise DeblockingError("luma inter-prediction supports at most two vectors")
    for items in predictions:
        for item in items:
            if (
                not isinstance(item, LumaPredictionVector)
                or not isinstance(item.reference_picture_id, int)
                or isinstance(item.reference_picture_id, bool)
                or not 0 <= item.reference_picture_id <= 0xFFFFFFFF
                or not isinstance(item.motion_vector, MotionVector)
                or any(
                    not isinstance(component, int)
                    or isinstance(component, bool)
                    or not -(1 << 31) <= component < (1 << 31)
                    for component in (item.motion_vector.x, item.motion_vector.y)
                )
            ):
                raise DeblockingError("luma inter-prediction input is invalid")
    if len(prediction_p) != len(prediction_q):
        return True
    if not prediction_p:
        return False

    def matches(first: LumaPredictionVector, second: LumaPredictionVector) -> bool:
        return (
            first.reference_picture_id == second.reference_picture_id
            and abs(first.motion_vector.x - second.motion_vector.x) < 4
            and abs(first.motion_vector.y - second.motion_vector.y) < 4
        )

    if len(prediction_p) == 1:
        return not matches(prediction_p[0], prediction_q[0])
    direct_match = matches(prediction_p[0], prediction_q[0]) and matches(
        prediction_p[1], prediction_q[1]
    )
    swapped_match = matches(prediction_p[0], prediction_q[1]) and matches(
        prediction_p[1], prediction_q[0]
    )
    return not (direct_match or swapped_match)


def derive_luma_deblocking_parameters(
    qp_p: int,
    qp_q: int,
    disable_deblocking_filter_idc: int,
    slice_alpha_c0_offset_div2: int,
    slice_beta_offset_div2: int,
) -> LumaDeblockingParameters:
    values = (
        qp_p,
        qp_q,
        disable_deblocking_filter_idc,
        slice_alpha_c0_offset_div2,
        slice_beta_offset_div2,
    )
    if any(not isinstance(value, int) or isinstance(value, bool) for value in values):
        raise DeblockingError("luma deblocking parameters are invalid")
    if (
        not 0 <= qp_p <= 51
        or not 0 <= qp_q <= 51
        or disable_deblocking_filter_idc not in (0, 1, 2)
        or not -6 <= slice_alpha_c0_offset_div2 <= 6
        or not -6 <= slice_beta_offset_div2 <= 6
    ):
        raise DeblockingError("luma deblocking parameters are invalid")
    mode = (
        LumaDeblockingMode.ALL_EDGES,
        LumaDeblockingMode.DISABLED,
        LumaDeblockingMode.ALL_EXCEPT_SLICE_BOUNDARIES,
    )[disable_deblocking_filter_idc]
    qp_average = (qp_p + qp_q + 1) >> 1
    index_a = min(51, max(0, qp_average + 2 * slice_alpha_c0_offset_div2))
    index_b = min(51, max(0, qp_average + 2 * slice_beta_offset_div2))
    return LumaDeblockingParameters(mode, index_a, index_b)


@dataclass(frozen=True)
class MotionVector:
    x: int
    y: int


@dataclass(frozen=True)
class LumaPredictionVector:
    reference_picture_id: int
    motion_vector: MotionVector


def derive_luma_boundary_strength_from_predictions(
    macroblock_edge: bool,
    either_intra: bool,
    either_has_nonzero_coefficients: bool,
    prediction_p: Sequence[LumaPredictionVector],
    prediction_q: Sequence[LumaPredictionVector],
) -> int:
    """Derive bS while comparing inter predictions only when earlier rules do not decide it."""
    if either_intra or either_has_nonzero_coefficients:
        return derive_luma_boundary_strength(
            macroblock_edge, either_intra, either_has_nonzero_coefficients, False
        )
    inter_prediction_differs = luma_inter_prediction_differs(prediction_p, prediction_q)
    return derive_luma_boundary_strength(
        macroblock_edge, False, False, inter_prediction_differs
    )


@dataclass(frozen=True)
class MotionVectorCandidate:
    reference_index: Optional[int]
    vector: MotionVector


@dataclass(frozen=True)
class ReferencePicture:
    identifier: int
    frame_num: int = 0
    picture_order_cnt: int = 0
    long_term_frame_idx: Optional[int] = None


@dataclass
class POCType0State:
    previous_pic_order_cnt_msb: int = 0
    previous_pic_order_cnt_lsb: int = 0

    def calculate(
        self,
        max_pic_order_cnt_lsb: int,
        pic_order_cnt_lsb: int,
        delta_pic_order_bottom: int = 0,
        nal_ref_idc: int = 1,
        idr: bool = False,
    ) -> int:
        values = (
            max_pic_order_cnt_lsb,
            pic_order_cnt_lsb,
            delta_pic_order_bottom,
            nal_ref_idc,
            self.previous_pic_order_cnt_msb,
            self.previous_pic_order_cnt_lsb,
        )
        min_int64 = -(1 << 63)
        max_int64 = (1 << 63) - 1
        if any(not isinstance(value, int) or isinstance(value, bool) for value in values):
            raise PictureOrderCountError("picture order count input or arithmetic is invalid")
        if (
            max_pic_order_cnt_lsb < 16
            or max_pic_order_cnt_lsb > 1 << 16
            or max_pic_order_cnt_lsb & (max_pic_order_cnt_lsb - 1)
            or not 0 <= pic_order_cnt_lsb < max_pic_order_cnt_lsb
            or (not idr and not 0 <= self.previous_pic_order_cnt_lsb < max_pic_order_cnt_lsb)
            or not 0 <= nal_ref_idc <= 3
            or not isinstance(idr, bool)
            or (idr and nal_ref_idc == 0)
            or not min_int64 <= delta_pic_order_bottom <= max_int64
            or (not idr and not min_int64 <= self.previous_pic_order_cnt_msb <= max_int64)
        ):
            raise PictureOrderCountError("picture order count input or arithmetic is invalid")

        previous_msb = 0 if idr else self.previous_pic_order_cnt_msb
        previous_lsb = 0 if idr else self.previous_pic_order_cnt_lsb
        current_msb = previous_msb
        half_range = max_pic_order_cnt_lsb // 2
        if pic_order_cnt_lsb < previous_lsb and previous_lsb - pic_order_cnt_lsb >= half_range:
            current_msb += max_pic_order_cnt_lsb
        elif pic_order_cnt_lsb > previous_lsb and pic_order_cnt_lsb - previous_lsb > half_range:
            current_msb -= max_pic_order_cnt_lsb

        top_field_order_cnt = current_msb + pic_order_cnt_lsb
        bottom_field_order_cnt = top_field_order_cnt + delta_pic_order_bottom
        if not (
            min_int64 <= current_msb <= max_int64
            and min_int64 <= top_field_order_cnt <= max_int64
            and min_int64 <= bottom_field_order_cnt <= max_int64
        ):
            raise PictureOrderCountError("picture order count input or arithmetic is invalid")
        if nal_ref_idc != 0:
            self.previous_pic_order_cnt_msb = current_msb
            self.previous_pic_order_cnt_lsb = pic_order_cnt_lsb
        return min(top_field_order_cnt, bottom_field_order_cnt)


@dataclass
class POCType12State:
    previous_frame_num: int = 0
    previous_frame_num_offset: int = 0

    def calculate_type1(
        self,
        max_frame_num: int,
        frame_num: int,
        delta_pic_order_cnt0: int = 0,
        delta_pic_order_cnt1: int = 0,
        offset_for_non_ref_pic: int = 0,
        offset_for_top_to_bottom_field: int = 0,
        offset_for_ref_frame: Sequence[int] = (),
        nal_ref_idc: int = 1,
        idr: bool = False,
    ) -> int:
        values = (
            max_frame_num,
            frame_num,
            delta_pic_order_cnt0,
            delta_pic_order_cnt1,
            offset_for_non_ref_pic,
            offset_for_top_to_bottom_field,
            *offset_for_ref_frame,
            self.previous_frame_num,
            self.previous_frame_num_offset,
            nal_ref_idc,
        )
        if (
            any(not isinstance(value, int) or isinstance(value, bool) for value in values)
            or any(not _within_int64(value) for value in values)
            or any(not _within_int64(offset) for offset in offset_for_ref_frame)
            or not isinstance(idr, bool)
            or max_frame_num < 16
            or max_frame_num > 1 << 16
            or max_frame_num & (max_frame_num - 1)
            or not 0 <= frame_num < max_frame_num
            or len(offset_for_ref_frame) > 255
            or not 0 <= nal_ref_idc <= 3
            or (idr and (nal_ref_idc == 0 or frame_num != 0))
        ):
            raise PictureOrderCountError("picture order count input or arithmetic is invalid")
        frame_num_offset = self._frame_num_offset(max_frame_num, frame_num, idr)
        abs_frame_num = frame_num_offset + frame_num if offset_for_ref_frame else 0
        if abs_frame_num < 0:
            raise PictureOrderCountError("picture order count input or arithmetic is invalid")
        if nal_ref_idc == 0 and abs_frame_num > 0:
            abs_frame_num -= 1

        expected_poc = 0
        if abs_frame_num > 0:
            cycle_length = len(offset_for_ref_frame)
            cycle_count, frame_in_cycle = divmod(abs_frame_num - 1, cycle_length)
            cycle_delta = sum(offset_for_ref_frame)
            cycle_prefix = sum(offset_for_ref_frame[: frame_in_cycle + 1])
            if not _within_int64(cycle_delta) or not _within_int64(cycle_prefix):
                raise PictureOrderCountError("picture order count input or arithmetic is invalid")
            expected_poc = cycle_count * cycle_delta + cycle_prefix
        if nal_ref_idc == 0:
            expected_poc += offset_for_non_ref_pic
        top_field_order_cnt = expected_poc + delta_pic_order_cnt0
        bottom_field_order_cnt = (
            top_field_order_cnt
            + offset_for_top_to_bottom_field
            + delta_pic_order_cnt1
        )
        if not all(
            _within_int64(value)
            for value in (
                frame_num_offset,
                abs_frame_num,
                expected_poc,
                top_field_order_cnt,
                bottom_field_order_cnt,
            )
        ):
            raise PictureOrderCountError("picture order count input or arithmetic is invalid")
        self._advance(frame_num, frame_num_offset, nal_ref_idc)
        return min(top_field_order_cnt, bottom_field_order_cnt)

    def calculate_type2(
        self, max_frame_num: int, frame_num: int, nal_ref_idc: int = 1, idr: bool = False
    ) -> int:
        if (
            any(
                not isinstance(value, int) or isinstance(value, bool)
                for value in (
                    max_frame_num,
                    frame_num,
                    nal_ref_idc,
                    self.previous_frame_num,
                    self.previous_frame_num_offset,
                )
            )
            or not isinstance(idr, bool)
            or max_frame_num < 16
            or max_frame_num > 1 << 16
            or max_frame_num & (max_frame_num - 1)
            or not 0 <= frame_num < max_frame_num
            or not 0 <= nal_ref_idc <= 3
            or (idr and (nal_ref_idc == 0 or frame_num != 0))
        ):
            raise PictureOrderCountError("picture order count input or arithmetic is invalid")
        frame_num_offset = self._frame_num_offset(max_frame_num, frame_num, idr)
        picture_order_cnt = 0
        if not idr:
            picture_order_cnt = 2 * (frame_num_offset + frame_num)
            if nal_ref_idc == 0:
                picture_order_cnt -= 1
        if not _within_int64(picture_order_cnt):
            raise PictureOrderCountError("picture order count input or arithmetic is invalid")
        self._advance(frame_num, frame_num_offset, nal_ref_idc)
        return picture_order_cnt

    def _frame_num_offset(self, max_frame_num: int, frame_num: int, idr: bool) -> int:
        if idr:
            return 0
        if (
            not 0 <= self.previous_frame_num < max_frame_num
            or not _within_int64(self.previous_frame_num_offset)
            or self.previous_frame_num_offset < 0
        ):
            raise PictureOrderCountError("picture order count input or arithmetic is invalid")
        offset = self.previous_frame_num_offset
        if self.previous_frame_num > frame_num:
            offset += max_frame_num
        if not _within_int64(offset):
            raise PictureOrderCountError("picture order count input or arithmetic is invalid")
        return offset

    def _advance(self, frame_num: int, frame_num_offset: int, nal_ref_idc: int) -> None:
        if nal_ref_idc != 0:
            self.previous_frame_num = frame_num
            self.previous_frame_num_offset = frame_num_offset


def _within_int64(value: int) -> bool:
    return -(1 << 63) <= value <= (1 << 63) - 1


@dataclass(frozen=True)
class Yuv420Frame:
    width: int
    height: int
    y_stride: int
    u_stride: int
    v_stride: int
    y: bytes
    u: bytes
    v: bytes

    def _plane_bytes(self, plane: bytes, width: int, height: int, stride: int) -> bytes:
        if any(not isinstance(value, int) or isinstance(value, bool) for value in (width, height, stride)):
            raise ValueError("reference picture frame layout is invalid or truncated")
        if stride <= 0 or stride < width:
            raise ValueError("reference picture frame layout is invalid or truncated")
        if len(plane) < stride * height:
            raise ValueError("reference picture frame layout is invalid or truncated")
        return b"".join(plane[row * stride : row * stride + width] for row in range(height))

    def luma_plane_bytes(self) -> bytes:
        return self._plane_bytes(self.y, self.width, self.height, self.y_stride)

    def u_plane_bytes(self) -> bytes:
        chroma_width = self.width // 2 + self.width % 2
        chroma_height = self.height // 2 + self.height % 2
        return self._plane_bytes(self.u, chroma_width, chroma_height, self.u_stride)

    def v_plane_bytes(self) -> bytes:
        chroma_width = self.width // 2 + self.width % 2
        chroma_height = self.height // 2 + self.height % 2
        return self._plane_bytes(self.v, chroma_width, chroma_height, self.v_stride)

    def luma_plane_matches(self, reference: bytes) -> bool:
        return self.luma_plane_bytes() == bytes(reference)


class Yuv420FrameBuilder:
    def __init__(
        self,
        picture_width_in_mbs: int,
        picture_height_in_mbs: int,
        crop_left: int = 0,
        crop_top: int = 0,
        crop_right: int = 0,
        crop_bottom: int = 0,
    ) -> None:
        values = (
            picture_width_in_mbs,
            picture_height_in_mbs,
            crop_left,
            crop_top,
            crop_right,
            crop_bottom,
        )
        if any(not isinstance(value, int) or isinstance(value, bool) for value in values):
            raise InterPredictionError("YUV 4:2:0 macroblock assembly is invalid")
        if (
            picture_width_in_mbs <= 0
            or picture_height_in_mbs <= 0
            or min(crop_left, crop_top, crop_right, crop_bottom) < 0
            or any(offset % 2 for offset in (crop_left, crop_top, crop_right, crop_bottom))
        ):
            raise InterPredictionError("YUV 4:2:0 macroblock assembly is invalid")
        coded_width = picture_width_in_mbs * 16
        coded_height = picture_height_in_mbs * 16
        if crop_left + crop_right >= coded_width or crop_top + crop_bottom >= coded_height:
            raise InterPredictionError("YUV 4:2:0 macroblock assembly is invalid")
        self._picture_width_in_mbs = picture_width_in_mbs
        self._crop_left = crop_left
        self._crop_top = crop_top
        width = coded_width - crop_left - crop_right
        height = coded_height - crop_top - crop_bottom
        chroma_size = (width // 2) * (height // 2)
        self._frame_width = width
        self._frame_height = height
        self._y = bytearray(width * height)
        self._u = bytearray(chroma_size)
        self._v = bytearray(chroma_size)
        self._written = bytearray(picture_width_in_mbs * picture_height_in_mbs)

    @staticmethod
    def _samples(block: Sequence[int], expected_length: int) -> bytes:
        if not isinstance(block, Sequence) or len(block) != expected_length:
            raise InterPredictionError("YUV 4:2:0 macroblock sample block has an invalid length")
        if any(
            not isinstance(value, int) or isinstance(value, bool) or not 0 <= value <= 255
            for value in block
        ):
            raise InterPredictionError("YUV 4:2:0 macroblock samples must be 8-bit integers")
        return bytes(block)

    def place_macroblock(
        self,
        address: int,
        y_block: Sequence[int],
        u_block: Sequence[int],
        v_block: Sequence[int],
    ) -> None:
        if (
            not isinstance(address, int)
            or isinstance(address, bool)
            or not 0 <= address < len(self._written)
            or self._written[address]
        ):
            raise InterPredictionError("YUV 4:2:0 macroblock address is invalid or duplicated")
        y_samples = self._samples(y_block, 256)
        u_samples = self._samples(u_block, 64)
        v_samples = self._samples(v_block, 64)
        mb_x = address % self._picture_width_in_mbs
        mb_y = address // self._picture_width_in_mbs
        for row in range(16):
            dst_y = mb_y * 16 + row - self._crop_top
            if not 0 <= dst_y < self._frame_height:
                continue
            for column in range(16):
                dst_x = mb_x * 16 + column - self._crop_left
                if 0 <= dst_x < self._frame_width:
                    self._y[dst_y * self._frame_width + dst_x] = y_samples[row * 16 + column]
        for row in range(8):
            dst_y = mb_y * 8 + row - self._crop_top // 2
            if not 0 <= dst_y < self._frame_height // 2:
                continue
            for column in range(8):
                dst_x = mb_x * 8 + column - self._crop_left // 2
                if 0 <= dst_x < self._frame_width // 2:
                    block_index = row * 8 + column
                    self._u[dst_y * (self._frame_width // 2) + dst_x] = u_samples[block_index]
                    self._v[dst_y * (self._frame_width // 2) + dst_x] = v_samples[block_index]
        self._written[address] = 1

    def finish(self) -> Yuv420Frame:
        if not all(self._written):
            raise InterPredictionError("YUV 4:2:0 macroblock assembly is incomplete")
        chroma_width = self._frame_width // 2
        return Yuv420Frame(
            self._frame_width,
            self._frame_height,
            self._frame_width,
            chroma_width,
            chroma_width,
            bytes(self._y),
            bytes(self._u),
            bytes(self._v),
        )


@dataclass(frozen=True)
class PresentationPicture:
    picture_order_cnt: int
    frame: Yuv420Frame


class PresentationOrderBuffer:
    def __init__(self, max_reorder_pictures: int) -> None:
        if (
            not isinstance(max_reorder_pictures, int)
            or isinstance(max_reorder_pictures, bool)
            or max_reorder_pictures < 0
        ):
            raise PictureOrderCountError("maximum reorder count must be a nonnegative integer")
        self._max_reorder_pictures = max_reorder_pictures
        self._pending: list[PresentationPicture] = []

    def push(self, picture: PresentationPicture) -> Optional[PresentationPicture]:
        if (
            not isinstance(picture.picture_order_cnt, int)
            or isinstance(picture.picture_order_cnt, bool)
            or not _within_int64(picture.picture_order_cnt)
        ):
            raise PictureOrderCountError("presentation picture POC must be a signed 64-bit integer")
        frame = picture.frame
        chroma_width = frame.width // 2 + frame.width % 2
        chroma_height = frame.height // 2 + frame.height % 2
        if not (
            _valid_yuv_plane(frame.y, frame.width, frame.height, frame.y_stride)
            and _valid_yuv_plane(frame.u, chroma_width, chroma_height, frame.u_stride)
            and _valid_yuv_plane(frame.v, chroma_width, chroma_height, frame.v_stride)
        ):
            raise InterPredictionError("reference picture frame layout is invalid or truncated")
        owned_frame = Yuv420Frame(
            frame.width,
            frame.height,
            frame.y_stride,
            frame.u_stride,
            frame.v_stride,
            bytes(frame.y),
            bytes(frame.u),
            bytes(frame.v),
        )
        self._pending.append(PresentationPicture(picture.picture_order_cnt, owned_frame))
        self._pending.sort(key=lambda item: item.picture_order_cnt)
        if len(self._pending) <= self._max_reorder_pictures:
            return None
        return self._pending.pop(0)

    def drain(self) -> list[PresentationPicture]:
        self._pending.sort(key=lambda item: item.picture_order_cnt)
        ready = self._pending
        self._pending = []
        return ready


@dataclass(frozen=True)
class DecodedReferencePicture:
    reference: ReferencePicture
    frame: Yuv420Frame


class ReferencePictureBuffer:
    def __init__(self) -> None:
        self._pictures: dict[int, DecodedReferencePicture] = {}

    def store(self, reference: ReferencePicture, frame: Yuv420Frame) -> None:
        chroma_width = frame.width // 2 + frame.width % 2
        chroma_height = frame.height // 2 + frame.height % 2
        if not (
            _valid_yuv_plane(frame.y, frame.width, frame.height, frame.y_stride)
            and _valid_yuv_plane(frame.u, chroma_width, chroma_height, frame.u_stride)
            and _valid_yuv_plane(frame.v, chroma_width, chroma_height, frame.v_stride)
        ):
            raise InterPredictionError("reference picture frame layout is invalid or truncated")
        owned_frame = Yuv420Frame(
            width=frame.width,
            height=frame.height,
            y_stride=frame.y_stride,
            u_stride=frame.u_stride,
            v_stride=frame.v_stride,
            y=bytes(frame.y),
            u=bytes(frame.u),
            v=bytes(frame.v),
        )
        self._pictures[reference.identifier] = DecodedReferencePicture(reference, owned_frame)

    def get(self, identifier: int) -> Optional[DecodedReferencePicture]:
        return self._pictures.get(identifier)

    def remove(self, identifier: int) -> bool:
        return self._pictures.pop(identifier, None) is not None

    def references(self) -> tuple[ReferencePicture, ...]:
        return tuple(picture.reference for picture in self._pictures.values())


def _valid_yuv_plane(plane: bytes, width: int, height: int, stride: int) -> bool:
    values = (width, height, stride)
    if any(not isinstance(value, int) or isinstance(value, bool) for value in values):
        return False
    if width <= 0 or height <= 0 or stride < width:
        return False
    return len(plane) >= (height - 1) * stride + width


def interpolate_luma_half_sample_horizontal(samples: Sequence[int]) -> int:
    """Apply the horizontal six-tap filter to six 8-bit luma samples."""
    return _interpolate_luma_half_sample(samples, "horizontal")


def interpolate_luma_half_sample_vertical(samples: Sequence[int]) -> int:
    """Apply the vertical six-tap filter to six 8-bit luma samples."""
    return _interpolate_luma_half_sample(samples, "vertical")


def _interpolate_luma_half_sample(samples: Sequence[int], direction: str) -> int:
    if len(samples) != 6 or any(
        not isinstance(sample, int)
        or isinstance(sample, bool)
        or not 0 <= sample <= 255
        for sample in samples
    ):
        raise InterPredictionError(
            f"{direction} luma interpolation requires six 8-bit reference samples"
        )
    value = (_filter_luma_six_tap(samples) + 16) >> 5
    return min(255, max(0, value))


def interpolate_luma_half_sample_diagonal(
    samples: Sequence[Sequence[int]],
) -> int:
    """Apply the unrounded two-pass six-tap filter to a 6x6 luma neighborhood."""
    if len(samples) != 6 or any(
        len(row) != 6
        or any(
            not isinstance(sample, int)
            or isinstance(sample, bool)
            or not 0 <= sample <= 255
            for sample in row
        )
        for row in samples
    ):
        raise InterPredictionError(
            "diagonal luma interpolation requires a 6x6 block of 8-bit reference samples"
        )

    vertical = [
        _filter_luma_six_tap([samples[row][column] for row in range(6)])
        for column in range(6)
    ]
    value = (_filter_luma_six_tap(vertical) + 512) >> 10
    return min(255, max(0, value))


def _filter_luma_six_tap(samples: Sequence[int]) -> int:
    return (
        samples[0]
        - 5 * samples[1]
        + 20 * samples[2]
        + 20 * samples[3]
        - 5 * samples[4]
        + samples[5]
    )


def interpolate_luma_quarter_sample_average(first: int, second: int) -> int:
    """Average two 8-bit luma samples with upward rounding."""
    if any(
        not isinstance(sample, int)
        or isinstance(sample, bool)
        or not 0 <= sample <= 255
        for sample in (first, second)
    ):
        raise InterPredictionError(
            "quarter-sample averaging requires two 8-bit luma samples"
        )
    return (first + second + 1) >> 1


def interpolate_luma_quarter_sample_pair(
    first: int, half: int, second: int
) -> tuple[int, int]:
    """Derive the two quarter-sample positions surrounding a half-sample."""
    return (
        interpolate_luma_quarter_sample_average(first, half),
        interpolate_luma_quarter_sample_average(half, second),
    )


def interpolate_luma_quarter_sample_diagonal(
    b: int, h: int, m: int, s: int
) -> tuple[int, int, int, int]:
    """Derive the diagonal quarter-sample positions e, g, p, and r."""
    return (
        interpolate_luma_quarter_sample_average(b, h),
        interpolate_luma_quarter_sample_average(b, m),
        interpolate_luma_quarter_sample_average(h, s),
        interpolate_luma_quarter_sample_average(m, s),
    )


def interpolate_luma_quarter_sample_axial(
    center: int,
    right: int,
    lower: int,
    horizontal_half: int,
    vertical_half: int,
) -> tuple[int, int, int, int]:
    """Derive axial quarter-sample positions a, c, d, and n around a pixel."""
    return (
        interpolate_luma_quarter_sample_average(center, horizontal_half),
        interpolate_luma_quarter_sample_average(right, horizontal_half),
        interpolate_luma_quarter_sample_average(center, vertical_half),
        interpolate_luma_quarter_sample_average(lower, vertical_half),
    )


def interpolate_luma_quarter_sample_around_j(
    b: int, h: int, j: int, m: int, s: int
) -> tuple[int, int, int, int]:
    """Derive quarter-sample positions f, i, k, and q around diagonal half-sample j."""
    return (
        interpolate_luma_quarter_sample_average(b, j),
        interpolate_luma_quarter_sample_average(h, j),
        interpolate_luma_quarter_sample_average(j, m),
        interpolate_luma_quarter_sample_average(j, s),
    )


def gather_luma_quarter_sample_neighborhood(
    plane: Sequence[int],
    width: int,
    height: int,
    stride: int,
    x_int: int,
    y_int: int,
) -> tuple[tuple[int, ...], ...]:
    """Gather a stride-aware 6x6 luma neighborhood with per-coordinate edge clipping."""
    integer_values = (width, height, stride, x_int, y_int)
    if any(not isinstance(value, int) or isinstance(value, bool) for value in integer_values):
        raise InterPredictionError("luma reference dimensions, stride, and coordinates must be integers")
    if width <= 0 or height <= 0 or stride < width:
        raise InterPredictionError("luma reference plane layout is invalid or truncated")
    required_length = (height - 1) * stride + width
    if len(plane) < required_length:
        raise InterPredictionError("luma reference plane layout is invalid or truncated")
    if any(
        not isinstance(sample, int)
        or isinstance(sample, bool)
        or not 0 <= sample <= 255
        for sample in plane[:required_length]
    ):
        raise InterPredictionError("luma reference plane must contain 8-bit samples")

    return tuple(
        tuple(
            plane[
                min(max(y_int + row_offset, 0), height - 1) * stride
                + min(max(x_int + column_offset, 0), width - 1)
            ]
            for column_offset in range(-2, 4)
        )
        for row_offset in range(-2, 4)
    )


def interpolate_luma_quarter_sample_grid(
    samples: Sequence[Sequence[int]],
) -> tuple[tuple[int, ...], ...]:
    """Build the Table 8-12 grid indexed by xFracL then yFracL from a 6x6 neighborhood."""
    j = interpolate_luma_half_sample_diagonal(samples)
    b = interpolate_luma_half_sample_horizontal(samples[2])
    h = interpolate_luma_half_sample_vertical([row[2] for row in samples])
    m = interpolate_luma_half_sample_vertical([row[3] for row in samples])
    s = interpolate_luma_half_sample_horizontal(samples[3])

    axial = interpolate_luma_quarter_sample_axial(
        samples[2][2], samples[2][3], samples[3][2], b, h
    )
    diagonal = interpolate_luma_quarter_sample_diagonal(b, h, m, s)
    around_j = interpolate_luma_quarter_sample_around_j(b, h, j, m, s)
    return (
        (samples[2][2], axial[2], h, axial[3]),
        (axial[0], diagonal[0], around_j[1], diagonal[2]),
        (b, around_j[0], j, around_j[3]),
        (axial[1], diagonal[1], around_j[2], diagonal[3]),
    )


def select_luma_fractional_sample(
    samples: Sequence[Sequence[int]], x_frac_l: int, y_frac_l: int
) -> int:
    """Select a luma sample from a grid indexed by xFracL then yFracL."""
    if (
        not isinstance(x_frac_l, int)
        or isinstance(x_frac_l, bool)
        or not 0 <= x_frac_l <= 3
        or not isinstance(y_frac_l, int)
        or isinstance(y_frac_l, bool)
        or not 0 <= y_frac_l <= 3
    ):
        raise InterPredictionError(
            "luma fractional-sample offset is outside [0,3]"
        )
    if len(samples) != 4 or any(len(row) != 4 for row in samples):
        raise InterPredictionError("luma fractional-sample grid must be 4x4")
    if any(
        not isinstance(sample, int)
        or isinstance(sample, bool)
        or not 0 <= sample <= 255
        for row in samples
        for sample in row
    ):
        raise InterPredictionError("luma fractional-sample grid must contain 8-bit samples")
    return samples[x_frac_l][y_frac_l]


def interpolate_luma_fractional_sample(
    plane: Sequence[int],
    width: int,
    height: int,
    stride: int,
    x_int: int,
    y_int: int,
    x_frac_l: int,
    y_frac_l: int,
) -> int:
    """Interpolate and select one luma sample from a reference plane."""
    neighborhood = gather_luma_quarter_sample_neighborhood(
        plane, width, height, stride, x_int, y_int
    )
    grid = interpolate_luma_quarter_sample_grid(neighborhood)
    return select_luma_fractional_sample(grid, x_frac_l, y_frac_l)


def build_p_reference_list(
    pictures: Sequence[ReferencePicture],
    current_frame_num: int,
    maximum_frame_num: int,
) -> list[ReferencePicture]:
    """Build the initial progressive-frame P list from short/long-term references."""
    if (
        not isinstance(current_frame_num, int)
        or isinstance(current_frame_num, bool)
        or not isinstance(maximum_frame_num, int)
        or isinstance(maximum_frame_num, bool)
        or maximum_frame_num <= 0
        or not 0 <= current_frame_num < maximum_frame_num
    ):
        raise ValueError("current frame number must be within a positive MaxFrameNum")

    short_term = []
    long_term = []
    for picture in pictures:
        if not isinstance(picture, ReferencePicture):
            raise ValueError("reference list contains an invalid picture")
        if picture.long_term_frame_idx is not None:
            if (
                not isinstance(picture.long_term_frame_idx, int)
                or isinstance(picture.long_term_frame_idx, bool)
                or picture.long_term_frame_idx < 0
            ):
                raise ValueError("long-term frame index must be nonnegative")
            long_term.append(picture)
            continue
        if (
            not isinstance(picture.frame_num, int)
            or isinstance(picture.frame_num, bool)
            or not 0 <= picture.frame_num < maximum_frame_num
        ):
            raise ValueError("short-term frame number is outside MaxFrameNum")
        frame_num_wrap = picture.frame_num
        if picture.frame_num > current_frame_num:
            frame_num_wrap -= maximum_frame_num
        short_term.append((frame_num_wrap, picture))

    short_term.sort(key=lambda item: item[0], reverse=True)
    long_term.sort(key=lambda picture: picture.long_term_frame_idx)
    return [picture for _, picture in short_term] + long_term


def build_b_reference_lists(
    pictures: Sequence[ReferencePicture], current_picture_order_cnt: int
) -> tuple[list[ReferencePicture], list[ReferencePicture]]:
    """Build initial progressive-frame B lists using POC order and long-term indices."""
    before_or_equal = [
        picture
        for picture in pictures
        if picture.long_term_frame_idx is None
        and picture.picture_order_cnt <= current_picture_order_cnt
    ]
    after = [
        picture
        for picture in pictures
        if picture.long_term_frame_idx is None
        and picture.picture_order_cnt > current_picture_order_cnt
    ]
    long_term = [
        picture for picture in pictures if picture.long_term_frame_idx is not None
    ]
    before_or_equal.sort(key=lambda picture: picture.picture_order_cnt, reverse=True)
    after.sort(key=lambda picture: picture.picture_order_cnt)
    long_term.sort(key=lambda picture: picture.long_term_frame_idx)

    list0 = before_or_equal + after + long_term
    list1 = after + before_or_equal + long_term
    if len(list1) > 1 and list0 == list1:
        list1[0], list1[1] = list1[1], list1[0]
    return list0, list1


def apply_reference_list_modifications(
    initial: Sequence[ReferencePicture],
    references: Sequence[ReferencePicture],
    current_frame_num: int,
    maximum_frame_num: int,
    modifications: Sequence[RefPicListModification],
) -> list[ReferencePicture]:
    """Apply parsed frame-coded short- and long-term reference-list commands."""
    if (
        not isinstance(current_frame_num, int)
        or isinstance(current_frame_num, bool)
        or not isinstance(maximum_frame_num, int)
        or isinstance(maximum_frame_num, bool)
        or maximum_frame_num <= 0
        or not 0 <= current_frame_num < maximum_frame_num
    ):
        raise ValueError("current frame number must be within a positive MaxFrameNum")
    result = list(initial)
    active_count = len(result)
    pic_num_pred = current_frame_num
    ref_index = 0
    for modification in modifications:
        if ref_index >= active_count:
            raise ValueError("reference-list modification exceeds the active reference count")
        if modification.modification_of_pic_nums_idc in (0, 1):
            abs_diff_pic_num = modification.value + 1
            if abs_diff_pic_num > maximum_frame_num:
                raise ValueError("absolute picture-number difference exceeds MaxPicNum")
            if modification.modification_of_pic_nums_idc == 0:
                pic_num_pred = (pic_num_pred + maximum_frame_num - abs_diff_pic_num) % maximum_frame_num
            else:
                pic_num_pred = (pic_num_pred + abs_diff_pic_num) % maximum_frame_num
            frame_num_wrap = pic_num_pred
            if pic_num_pred > current_frame_num:
                frame_num_wrap -= maximum_frame_num
            target = next(
                (
                    picture
                    for picture in references
                    if picture.long_term_frame_idx is None
                    and 0 <= picture.frame_num < maximum_frame_num
                    and (
                        picture.frame_num - maximum_frame_num
                        if picture.frame_num > current_frame_num
                        else picture.frame_num
                    )
                    == frame_num_wrap
                ),
                None,
            )
        elif modification.modification_of_pic_nums_idc == 2:
            target = next(
                (
                    picture
                    for picture in references
                    if picture.long_term_frame_idx == modification.value
                ),
                None,
            )
        else:
            raise ValueError("reference-list modification idc is invalid")
        if target is None:
            raise ValueError("reference-list modification target is unavailable")

        for duplicate_index in range(ref_index, len(result)):
            if result[duplicate_index].identifier == target.identifier:
                del result[duplicate_index]
                break
        result.insert(ref_index, target)
        del result[active_count:]
        ref_index += 1
    return result


def motion_vector_difference_neighbor_magnitudes(
    left: Optional[MotionVector], top: Optional[MotionVector]
) -> tuple[int, int]:
    """Sum absolute left/top MVD components; absent neighbors contribute zero."""
    left_x, left_y = (left.x, left.y) if left is not None else (0, 0)
    top_x, top_y = (top.x, top.y) if top is not None else (0, 0)
    return abs(left_x) + abs(top_x), abs(left_y) + abs(top_y)


class MotionVectorPartitionShape(Enum):
    OTHER = "other"
    SIXTEEN_BY_EIGHT = "16x8"
    EIGHT_BY_SIXTEEN = "8x16"


def predict_motion_vector_for_partition(
    shape: MotionVectorPartitionShape,
    current_reference_index: int,
    left: Optional[MotionVectorCandidate],
    top: Optional[MotionVectorCandidate],
    top_right: Optional[MotionVectorCandidate],
    top_left: Optional[MotionVectorCandidate],
) -> MotionVector:
    """Apply H.264's partition-specific reference matches before the median rule."""
    top_right_candidate = top_right if top_right is not None else top_left
    if current_reference_index >= 0:
        if shape is MotionVectorPartitionShape.SIXTEEN_BY_EIGHT:
            for candidate in (left, top):
                if (
                    candidate is not None
                    and candidate.reference_index == current_reference_index
                ):
                    return candidate.vector
        elif shape is MotionVectorPartitionShape.EIGHT_BY_SIXTEEN:
            for candidate in (left, top_right_candidate):
                if (
                    candidate is not None
                    and candidate.reference_index == current_reference_index
                ):
                    return candidate.vector
    return predict_motion_vector(
        current_reference_index, left, top, top_right, top_left
    )


def predict_motion_vector(
    current_reference_index: int,
    left: Optional[MotionVectorCandidate],
    top: Optional[MotionVectorCandidate],
    top_right: Optional[MotionVectorCandidate],
    top_left: Optional[MotionVectorCandidate],
) -> MotionVector:
    """Select the unique matching reference vector, or the component median."""
    if top_right is None:
        top_right = top_left

    candidates = (left, top, top_right)
    vectors = tuple(
        candidate.vector
        if candidate is not None
        and candidate.reference_index is not None
        and candidate.reference_index >= 0
        else MotionVector(0, 0)
        for candidate in candidates
    )
    matching = [
        candidate
        for candidate in candidates
        if candidate is not None
        and current_reference_index >= 0
        and candidate.reference_index == current_reference_index
    ]
    if len(matching) == 1:
        return matching[0].vector

    horizontal = sorted(vector.x for vector in vectors)
    vertical = sorted(vector.y for vector in vectors)
    return MotionVector(horizontal[1], vertical[1])


def apply_motion_vector_difference(
    predicted: MotionVector, difference: MotionVector
) -> MotionVector:
    """Add a decoded MVD and wrap both components to signed 16-bit range."""
    return MotionVector(
        (predicted.x + difference.x + (1 << 15)) % (1 << 16) - (1 << 15),
        (predicted.y + difference.y + (1 << 15)) % (1 << 16) - (1 << 15),
    )


def derive_motion_vector(
    shape: MotionVectorPartitionShape,
    current_reference_index: int,
    left: Optional[MotionVectorCandidate],
    top: Optional[MotionVectorCandidate],
    top_right: Optional[MotionVectorCandidate],
    top_left: Optional[MotionVectorCandidate],
    difference: MotionVector,
) -> MotionVector:
    """Derive a partition vector from neighbor prediction and a decoded difference."""
    predicted = predict_motion_vector_for_partition(
        shape, current_reference_index, left, top, top_right, top_left
    )
    return apply_motion_vector_difference(predicted, difference)


_INVERSE_SCALE_4X4_FACTORS = (
    (10, 13, 16),
    (11, 14, 18),
    (13, 16, 20),
    (14, 18, 23),
    (16, 20, 25),
    (18, 23, 29),
)
_CHROMA_QPC_FROM_QPI = (
    29, 30, 31, 32, 32, 33, 34, 34, 35, 35, 36,
    36, 37, 37, 37, 38, 38, 38, 38, 39, 39, 39,
)
_INVERSE_SCALE_8X8_FACTORS = (
    (20, 18, 32, 19, 25, 24),
    (22, 19, 35, 21, 28, 26),
    (26, 23, 42, 24, 33, 31),
    (28, 25, 45, 26, 35, 33),
    (32, 28, 51, 30, 40, 38),
    (36, 32, 58, 34, 46, 43),
)
_INVERSE_SCALE_8X8_CLASSES = (
    0, 3, 4, 3, 0, 3, 4, 3,
    3, 1, 5, 1, 3, 1, 5, 1,
    4, 5, 2, 5, 4, 5, 2, 5,
    3, 1, 5, 1, 3, 1, 5, 1,
    0, 3, 4, 3, 0, 3, 4, 3,
    3, 1, 5, 1, 3, 1, 5, 1,
    4, 5, 2, 5, 4, 5, 2, 5,
    3, 1, 5, 1, 3, 1, 5, 1,
)


def derive_chroma_qpc(qpy: int, qp_offset: int) -> int:
    """Derive 8-bit chroma QPC from luma QPY and a PPS component offset."""
    if not isinstance(qpy, int) or isinstance(qpy, bool) or not 0 <= qpy <= 51:
        raise ChromaQPError("QPY is outside [0,51]")
    if (
        not isinstance(qp_offset, int)
        or isinstance(qp_offset, bool)
        or not -12 <= qp_offset <= 12
    ):
        raise ChromaQPError("chroma QP index offset is outside [-12,12]")

    qpi = min(51, max(0, qpy + qp_offset))
    if qpi < 30:
        return qpi
    return _CHROMA_QPC_FROM_QPI[qpi - 30]


def inverse_scale_luma4x4(
    levels: Sequence[int], scaling_list: Sequence[int], qpy: int
) -> list[int]:
    """Apply H.264 4x4 luma inverse scaling to raster-order coefficient levels."""
    if not isinstance(qpy, int) or isinstance(qpy, bool) or not 0 <= qpy <= 51:
        raise InverseScaleError("inverse scaling QPY is outside [0,51]")
    if len(levels) != 16 or any(
        not isinstance(level, int)
        or isinstance(level, bool)
        or not -(1 << 31) <= level < (1 << 31)
        for level in levels
    ):
        raise InverseScaleError("inverse scaling levels must contain 16 signed 32-bit integers")
    if len(scaling_list) != 16 or any(
        not isinstance(weight, int)
        or isinstance(weight, bool)
        or not 1 <= weight <= 255
        for weight in scaling_list
    ):
        raise InverseScaleError("inverse scaling list must contain 16 values in [1,255]")

    scaled = []
    for index, (level, weight) in enumerate(zip(levels, scaling_list)):
        row, column = divmod(index, 4)
        factor_class = row % 2 + column % 2
        value = level * _INVERSE_SCALE_4X4_FACTORS[qpy % 6][factor_class] * weight
        if qpy >= 24:
            scaled.append(value << (qpy // 6 - 4))
        else:
            shift = 4 - qpy // 6
            rounding = 1 << (shift - 1)
            scaled.append((value + rounding) >> shift)
    return scaled


def inverse_scale_chroma_dc2x2(transformed: Sequence[int], qpc: int) -> list[int]:
    """Scale 4:2:0 chroma DC values for an 8-bit QP-prime C."""
    if not isinstance(qpc, int) or isinstance(qpc, bool) or not 0 <= qpc <= 39:
        raise InverseScaleError("inverse scaling QPC is outside [0,39]")
    if len(transformed) != 4 or any(
        not isinstance(coefficient, int)
        or isinstance(coefficient, bool)
        or not -(1 << 15) <= coefficient < (1 << 15)
        for coefficient in transformed
    ):
        raise InverseScaleError("chroma DC values must contain four signed 16-bit integers")

    factor = _INVERSE_SCALE_4X4_FACTORS[qpc % 6][0]
    shift = qpc // 6
    scaled = [(coefficient * factor << shift) >> 5 for coefficient in transformed]
    if any(not -(1 << 15) <= coefficient < (1 << 15) for coefficient in scaled):
        raise InverseScaleError("scaled chroma DC value is outside the signed 16-bit range")
    return scaled


def inverse_scale_chroma4x4(
    levels: Sequence[int], scaling_list: Sequence[int], qpc: int
) -> list[int]:
    """Preserve scaled chroma DC and inverse-scale 4x4 chroma AC levels."""
    if not isinstance(qpc, int) or isinstance(qpc, bool) or not 0 <= qpc <= 39:
        raise InverseScaleError("inverse scaling QPC is outside [0,39]")
    if len(levels) != 16 or any(
        not isinstance(level, int)
        or isinstance(level, bool)
        or not -(1 << 15) <= level < (1 << 15)
        for level in levels
    ):
        raise InverseScaleError("chroma block levels must contain 16 signed 16-bit integers")
    if len(scaling_list) != 16 or any(
        not isinstance(weight, int)
        or isinstance(weight, bool)
        or not 1 <= weight <= 255
        for weight in scaling_list
    ):
        raise InverseScaleError("inverse scaling list must contain 16 values in [1,255]")

    scaled = [levels[0]] + [0] * 15
    for index in range(1, 16):
        row, column = divmod(index, 4)
        factor_class = row % 2 + column % 2
        value = levels[index] * _INVERSE_SCALE_4X4_FACTORS[qpc % 6][factor_class] * scaling_list[index]
        if qpc >= 24:
            scaled[index] = value << (qpc // 6 - 4)
        else:
            shift = 4 - qpc // 6
            rounding = 1 << (shift - 1)
            scaled[index] = (value + rounding) >> shift
        if not -(1 << 15) <= scaled[index] < (1 << 15):
            raise InverseScaleError("scaled chroma block value is outside the signed 16-bit range")
    return scaled


def reconstruct_chroma4x4_residual(
    dc_c: int,
    ac_scan_levels: Sequence[int],
    scaling_list: Sequence[int],
    qpc: int,
) -> list[int]:
    """Assemble, scale, and inverse-transform one 4x4 chroma residual block."""
    if not isinstance(qpc, int) or isinstance(qpc, bool) or not 0 <= qpc <= 39:
        raise InverseScaleError("inverse scaling QPC is outside [0,39]")
    if (
        not isinstance(dc_c, int)
        or isinstance(dc_c, bool)
        or not -(1 << 15) <= dc_c < (1 << 15)
    ):
        raise InverseScaleError("chroma block DC must be a signed 16-bit integer")

    scan_levels = place_chroma4x4_scan_levels(dc_c, list(ac_scan_levels))
    scaled = inverse_scale_chroma4x4(scan_levels, scaling_list, qpc)
    return inverse_transform_luma4x4(scaled)


def assemble_chroma420_residual_macroblock(blocks: Sequence[Sequence[int]]) -> list[int]:
    """Place four raster-ordered 4x4 residual blocks into an 8x8 chroma macroblock."""
    if not isinstance(blocks, Sequence) or isinstance(blocks, (str, bytes)) or len(blocks) != 4:
        raise InverseTransformError("4:2:0 chroma macroblock requires four 4x4 residual blocks")
    if any(
        not isinstance(block, Sequence)
        or isinstance(block, (str, bytes))
        or len(block) != 16
        or any(not isinstance(sample, int) or isinstance(sample, bool) for sample in block)
        for block in blocks
    ):
        raise InverseTransformError("each chroma residual block must contain 16 integer samples")

    macroblock = [0] * 64
    for block_index, block in enumerate(blocks):
        x_offset = block_index % 2 * 4
        y_offset = block_index // 2 * 4
        for row in range(4):
            destination = (y_offset + row) * 8 + x_offset
            macroblock[destination : destination + 4] = block[row * 4 : row * 4 + 4]
    return macroblock


def reconstruct_chroma420_macroblock(
    prediction: Sequence[int], residual: Sequence[int]
) -> list[int]:
    """Add chroma residuals to prediction samples and apply 8-bit Clip1C."""
    if len(prediction) != 64 or any(
        not isinstance(sample, int)
        or isinstance(sample, bool)
        or not 0 <= sample <= 255
        for sample in prediction
    ):
        raise IntraPredictionError("chroma prediction must contain 64 8-bit samples")
    if len(residual) != 64 or any(
        not isinstance(sample, int) or isinstance(sample, bool) for sample in residual
    ):
        raise InverseTransformError("chroma residual must contain 64 integer samples")
    return [min(255, max(0, predicted + value)) for predicted, value in zip(prediction, residual)]


def inverse_scale_luma8x8(
    levels: Sequence[int], scaling_list: Sequence[int], qpy: int
) -> list[int]:
    """Apply H.264 8x8 luma inverse scaling to raster-order coefficient levels."""
    if not isinstance(qpy, int) or isinstance(qpy, bool) or not 0 <= qpy <= 51:
        raise InverseScaleError("inverse scaling QPY is outside [0,51]")
    if len(levels) != 64 or any(
        not isinstance(level, int)
        or isinstance(level, bool)
        or not -(1 << 31) <= level < (1 << 31)
        for level in levels
    ):
        raise InverseScaleError("inverse scaling levels must contain 64 signed 32-bit integers")
    if len(scaling_list) != 64 or any(
        not isinstance(weight, int)
        or isinstance(weight, bool)
        or not 1 <= weight <= 255
        for weight in scaling_list
    ):
        raise InverseScaleError("inverse scaling list must contain 64 values in [1,255]")

    scaled = []
    for index, (level, weight) in enumerate(zip(levels, scaling_list)):
        factor_class = _INVERSE_SCALE_8X8_CLASSES[index]
        value = level * _INVERSE_SCALE_8X8_FACTORS[qpy % 6][factor_class] * weight
        if qpy >= 24:
            scaled.append(value << (qpy // 6 - 4))
        else:
            shift = 4 - qpy // 6
            rounding = 1 << (shift - 1)
            scaled.append((value + rounding) >> shift)
    return scaled


def predict_luma_intra8x8_vertical(top: Sequence[int]) -> list[int]:
    """Repeat eight filtered top samples across an 8x8 luma block."""
    if len(top) != 8 or any(
        not isinstance(sample, int)
        or isinstance(sample, bool)
        or not 0 <= sample <= 255
        for sample in top
    ):
        raise IntraPredictionError(
            "vertical 8x8 prediction requires eight 8-bit filtered top samples"
        )
    return list(top) * 8


def predict_luma_intra16x16_vertical(top: Sequence[int]) -> list[int]:
    """Repeat 16 filtered top samples across a 16x16 luma block."""
    if len(top) != 16 or any(
        not isinstance(sample, int)
        or isinstance(sample, bool)
        or not 0 <= sample <= 255
        for sample in top
    ):
        raise IntraPredictionError(
            "vertical 16x16 prediction requires sixteen 8-bit filtered top samples"
        )
    return list(top) * 16


def predict_luma_intra16x16_horizontal(left: Sequence[int]) -> list[int]:
    """Repeat each filtered left sample across one row of a 16x16 luma block."""
    if len(left) != 16 or any(
        not isinstance(sample, int)
        or isinstance(sample, bool)
        or not 0 <= sample <= 255
        for sample in left
    ):
        raise IntraPredictionError(
            "horizontal 16x16 prediction requires sixteen 8-bit filtered left samples"
        )
    return [sample for sample in left for _ in range(16)]


def predict_luma_intra16x16_dc(
    top: Optional[Sequence[int]] = None, left: Optional[Sequence[int]] = None
) -> list[int]:
    """Predict a 16x16 luma block from whichever filtered edges are available."""
    for name, references in (("top", top), ("left", left)):
        if references is not None and (
            len(references) != 16
            or any(
                not isinstance(sample, int)
                or isinstance(sample, bool)
                or not 0 <= sample <= 255
                for sample in references
            )
        ):
            raise IntraPredictionError(
                f"DC 16x16 prediction requires sixteen 8-bit filtered {name} samples"
            )

    if top is not None and left is not None:
        dc_value = (sum(top) + sum(left) + 16) >> 5
    elif top is not None:
        dc_value = (sum(top) + 8) >> 4
    elif left is not None:
        dc_value = (sum(left) + 8) >> 4
    else:
        dc_value = 128
    return [dc_value] * 256


def predict_luma_intra16x16_plane(
    top: Sequence[int], left: Sequence[int], top_left: int
) -> list[int]:
    """Predict a 16x16 luma block from filtered top, left, and top-left references."""
    for name, references in (("top", top), ("left", left)):
        if len(references) != 16 or any(
            not isinstance(sample, int)
            or isinstance(sample, bool)
            or not 0 <= sample <= 255
            for sample in references
        ):
            raise IntraPredictionError(
                f"plane 16x16 prediction requires sixteen 8-bit filtered {name} samples"
            )
    if (
        not isinstance(top_left, int)
        or isinstance(top_left, bool)
        or not 0 <= top_left <= 255
    ):
        raise IntraPredictionError(
            "plane 16x16 prediction requires an 8-bit filtered top-left sample"
        )

    horizontal_gradient = sum(
        index * (top[7 + index] - top[7 - index]) for index in range(1, 8)
    ) + 8 * (top[15] - top_left)
    vertical_gradient = sum(
        index * (left[7 + index] - left[7 - index]) for index in range(1, 8)
    ) + 8 * (left[15] - top_left)
    a = 16 * (top[15] + left[15])
    b = (5 * horizontal_gradient + 32) >> 6
    c = (5 * vertical_gradient + 32) >> 6

    prediction = [0] * 256
    for row in range(16):
        for column in range(16):
            value = (a + b * (column - 7) + c * (row - 7) + 16) >> 5
            prediction[row * 16 + column] = min(255, max(0, value))
    return prediction


def predict_luma_intra8x8_horizontal(left: Sequence[int]) -> list[int]:
    """Repeat each filtered left sample across one row of an 8x8 luma block."""
    if len(left) != 8 or any(
        not isinstance(sample, int)
        or isinstance(sample, bool)
        or not 0 <= sample <= 255
        for sample in left
    ):
        raise IntraPredictionError(
            "horizontal 8x8 prediction requires eight 8-bit filtered left samples"
        )
    return [sample for sample in left for _ in range(8)]


def predict_luma_intra8x8_dc(
    top: Optional[Sequence[int]] = None, left: Optional[Sequence[int]] = None
) -> list[int]:
    """Predict an 8x8 luma block from whichever filtered edges are available."""
    for name, references in (("top", top), ("left", left)):
        if references is not None and (
            len(references) != 8
            or any(
                not isinstance(sample, int)
                or isinstance(sample, bool)
                or not 0 <= sample <= 255
                for sample in references
            )
        ):
            raise IntraPredictionError(
                f"DC 8x8 prediction requires eight 8-bit filtered {name} samples"
            )

    if top is not None and left is not None:
        dc_value = (sum(top) + sum(left) + 8) >> 4
    elif top is not None:
        dc_value = (sum(top) + 4) >> 3
    elif left is not None:
        dc_value = (sum(left) + 4) >> 3
    else:
        dc_value = 128
    return [dc_value] * 64


def predict_chroma_intra8x8_dc(
    top: Optional[Sequence[int]] = None, left: Optional[Sequence[int]] = None
) -> list[int]:
    """Predict 4:2:0 chroma DC with the normative 4x4 quadrant edge fallbacks."""
    for name, references in (("top", top), ("left", left)):
        if references is not None and (
            len(references) != 8
            or any(
                not isinstance(sample, int)
                or isinstance(sample, bool)
                or not 0 <= sample <= 255
                for sample in references
            )
        ):
            raise IntraPredictionError(
                f"chroma DC 8x8 prediction requires eight 8-bit filtered {name} samples"
            )

    def edge_average(samples: Sequence[int], offset: int) -> int:
        return (sum(samples[offset : offset + 4]) + 2) >> 2

    quadrant_values = [128] * 4
    for block_row in range(2):
        for block_column in range(2):
            index = block_row * 2 + block_column
            top_offset = block_column * 4
            left_offset = block_row * 4
            if block_row == 0 and block_column == 1:
                if top is not None:
                    quadrant_values[index] = edge_average(top, top_offset)
                elif left is not None:
                    quadrant_values[index] = edge_average(left, left_offset)
            elif block_row == 1 and block_column == 0:
                if left is not None:
                    quadrant_values[index] = edge_average(left, left_offset)
                elif top is not None:
                    quadrant_values[index] = edge_average(top, top_offset)
            elif top is not None and left is not None:
                quadrant_values[index] = (
                    sum(top[top_offset : top_offset + 4])
                    + sum(left[left_offset : left_offset + 4])
                    + 4
                ) >> 3
            elif left is not None:
                quadrant_values[index] = edge_average(left, left_offset)
            elif top is not None:
                quadrant_values[index] = edge_average(top, top_offset)

    prediction = [0] * 64
    for row in range(8):
        for column in range(8):
            quadrant_index = (row // 4) * 2 + column // 4
            prediction[row * 8 + column] = quadrant_values[quadrant_index]
    return prediction


def predict_chroma_intra8x8_horizontal(left: Sequence[int]) -> list[int]:
    """Repeat each filtered left sample across one row of an 8x8 chroma block."""
    if len(left) != 8 or any(
        not isinstance(sample, int)
        or isinstance(sample, bool)
        or not 0 <= sample <= 255
        for sample in left
    ):
        raise IntraPredictionError(
            "chroma horizontal 8x8 prediction requires eight 8-bit filtered left samples"
        )
    return [sample for sample in left for _ in range(8)]


def predict_luma_intra8x8_diagonal_down_left(top: Sequence[int]) -> list[int]:
    """Interpolate an 8x8 luma block from 16 filtered top references."""
    if len(top) != 16 or any(
        not isinstance(sample, int)
        or isinstance(sample, bool)
        or not 0 <= sample <= 255
        for sample in top
    ):
        raise IntraPredictionError(
            "diagonal-down-left 8x8 prediction requires sixteen 8-bit filtered top samples"
        )

    prediction = [0] * 64
    for row in range(8):
        for column in range(8):
            index = row + column
            last_index = min(index + 2, 15)
            prediction[row * 8 + column] = (
                top[index] + 2 * top[index + 1] + top[last_index] + 2
            ) >> 2
    return prediction


def predict_luma_intra8x8_diagonal_down_right(
    top: Sequence[int], left: Sequence[int], top_left: int
) -> list[int]:
    """Interpolate an 8x8 luma block from filtered top, left, and top-left references."""
    for name, references, expected_length in (("top", top, 16), ("left", left, 8)):
        if len(references) != expected_length or any(
            not isinstance(sample, int)
            or isinstance(sample, bool)
            or not 0 <= sample <= 255
            for sample in references
        ):
            sample_count = "sixteen" if expected_length == 16 else "eight"
            raise IntraPredictionError(
                f"diagonal-down-right 8x8 prediction requires {sample_count} 8-bit filtered {name} samples"
            )
    if (
        not isinstance(top_left, int)
        or isinstance(top_left, bool)
        or not 0 <= top_left <= 255
    ):
        raise IntraPredictionError(
            "diagonal-down-right 8x8 prediction requires an 8-bit filtered top-left sample"
        )

    def reference_at(position: int) -> int:
        if position == -1:
            return top_left
        if position < -1:
            return left[-position - 2]
        return top[position]

    prediction = [0] * 64
    for row in range(8):
        for column in range(8):
            position = column - row
            prediction[row * 8 + column] = (
                reference_at(position - 1)
                + 2 * reference_at(position)
                + reference_at(position + 1)
                + 2
            ) >> 2
    return prediction


def predict_luma_intra8x8_vertical_right(
    top: Sequence[int], left: Sequence[int], top_left: int
) -> list[int]:
    """Interpolate an 8x8 luma block across vertical-right reference phases."""
    for name, references, expected_length in (("top", top, 16), ("left", left, 8)):
        if len(references) != expected_length or any(
            not isinstance(sample, int)
            or isinstance(sample, bool)
            or not 0 <= sample <= 255
            for sample in references
        ):
            sample_count = "sixteen" if expected_length == 16 else "eight"
            raise IntraPredictionError(
                f"vertical-right 8x8 prediction requires {sample_count} 8-bit filtered {name} samples"
            )
    if (
        not isinstance(top_left, int)
        or isinstance(top_left, bool)
        or not 0 <= top_left <= 255
    ):
        raise IntraPredictionError(
            "vertical-right 8x8 prediction requires an 8-bit filtered top-left sample"
        )

    def top_reference_at(position: int) -> int:
        return top_left if position == -1 else top[position]

    prediction = [0] * 64
    for row in range(8):
        for column in range(8):
            phase = 2 * column - row
            if phase >= 0 and phase % 2 == 0:
                center = phase // 2
                value = (
                    top_reference_at(center - 1)
                    + 2 * top_reference_at(center)
                    + top_reference_at(center + 1)
                    + 2
                ) >> 2
            elif phase >= 0:
                center = (phase - 1) // 2
                value = (
                    top_reference_at(center - 1) + top_reference_at(center) + 1
                ) >> 1
            elif phase == -1:
                value = (left[0] + top_left + 1) >> 1
            elif phase == -2:
                value = (left[0] + 2 * top_left + top[0] + 2) >> 2
            elif -phase % 2 == 1:
                center = (-phase - 3) // 2
                value = (left[center] + left[center + 1] + 1) >> 1
            else:
                center = (-phase - 4) // 2
                value = (
                    left[center] + 2 * left[center + 1] + left[center + 2] + 2
                ) >> 2
            prediction[row * 8 + column] = value
    return prediction


def predict_luma_intra8x8_horizontal_down(
    top: Sequence[int], left: Sequence[int], top_left: int
) -> list[int]:
    """Predict an 8x8 luma block by transposing vertical-right prediction."""
    for name, references, expected_length in (("top", top, 8), ("left", left, 16)):
        if len(references) != expected_length or any(
            not isinstance(sample, int)
            or isinstance(sample, bool)
            or not 0 <= sample <= 255
            for sample in references
        ):
            sample_count = "eight" if expected_length == 8 else "sixteen"
            raise IntraPredictionError(
                f"horizontal-down 8x8 prediction requires {sample_count} 8-bit filtered {name} samples"
            )
    if (
        not isinstance(top_left, int)
        or isinstance(top_left, bool)
        or not 0 <= top_left <= 255
    ):
        raise IntraPredictionError(
            "horizontal-down 8x8 prediction requires an 8-bit filtered top-left sample"
        )

    vertical_prediction = predict_luma_intra8x8_vertical_right(left, top, top_left)
    return [
        vertical_prediction[column * 8 + row]
        for row in range(8)
        for column in range(8)
    ]


def predict_luma_intra8x8_vertical_left(top: Sequence[int]) -> list[int]:
    """Interpolate an 8x8 luma block across vertical-left top-reference phases."""
    if len(top) != 16 or any(
        not isinstance(sample, int)
        or isinstance(sample, bool)
        or not 0 <= sample <= 255
        for sample in top
    ):
        raise IntraPredictionError(
            "vertical-left 8x8 prediction requires sixteen 8-bit filtered top samples"
        )

    prediction = [0] * 64
    for row in range(8):
        for column in range(8):
            phase = 2 * column + row
            if phase % 2 == 0:
                center = phase // 2
                value = (top[center] + top[center + 1] + 1) >> 1
            else:
                center = (phase - 1) // 2
                value = (
                    top[center] + 2 * top[center + 1] + top[center + 2] + 2
                ) >> 2
            prediction[row * 8 + column] = value
    return prediction


def predict_luma_intra8x8_horizontal_up(left: Sequence[int]) -> list[int]:
    """Predict an 8x8 luma block by transposing vertical-left prediction."""
    if len(left) != 16 or any(
        not isinstance(sample, int)
        or isinstance(sample, bool)
        or not 0 <= sample <= 255
        for sample in left
    ):
        raise IntraPredictionError(
            "horizontal-up 8x8 prediction requires sixteen 8-bit filtered left samples"
        )

    vertical_prediction = predict_luma_intra8x8_vertical_left(left)
    return [
        vertical_prediction[column * 8 + row]
        for row in range(8)
        for column in range(8)
    ]


def predict_luma_intra8x8_plane(
    top: Sequence[int], left: Sequence[int]
) -> list[int]:
    """Predict an 8x8 luma block from filtered top and left references."""
    for name, references in (("top", top), ("left", left)):
        if len(references) != 16 or any(
            not isinstance(sample, int)
            or isinstance(sample, bool)
            or not 0 <= sample <= 255
            for sample in references
        ):
            raise IntraPredictionError(
                f"plane 8x8 prediction requires sixteen 8-bit filtered {name} samples"
            )
    horizontal_gradient = sum(
        index * (top[4 + index] - top[4 - index])
        for index in range(1, 5)
    )
    vertical_gradient = sum(
        index * (left[4 + index] - left[4 - index])
        for index in range(1, 5)
    )
    a = 16 * (top[8] + left[8])
    b = (17 * horizontal_gradient + 16) >> 5
    c = (17 * vertical_gradient + 16) >> 5

    prediction = [0] * 64
    for row in range(8):
        for column in range(8):
            value = (a + b * (column - 3) + c * (row - 3) + 16) >> 5
            prediction[row * 8 + column] = min(255, max(0, value))
    return prediction


def predict_luma_intra4x4_vertical(top: Sequence[int]) -> list[int]:
    """Repeat four available top reference samples across a 4x4 luma block."""
    if len(top) != 4 or any(
        not isinstance(sample, int)
        or isinstance(sample, bool)
        or not 0 <= sample <= 255
        for sample in top
    ):
        raise IntraPredictionError("vertical 4x4 prediction requires four 8-bit top samples")
    return list(top) * 4


def predict_luma_intra4x4_horizontal(left: Sequence[int]) -> list[int]:
    """Repeat each of four available left reference samples across one block row."""
    if len(left) != 4 or any(
        not isinstance(sample, int)
        or isinstance(sample, bool)
        or not 0 <= sample <= 255
        for sample in left
    ):
        raise IntraPredictionError("horizontal 4x4 prediction requires four 8-bit left samples")
    return [sample for sample in left for _ in range(4)]

def predict_chroma_intra8x8_vertical(top: Sequence[int]) -> list[int]:
    """Repeat each filtered top sample down one column of an 8x8 chroma block."""
    if len(top) != 8 or any(
        not isinstance(sample, int)
        or isinstance(sample, bool)
        or not 0 <= sample <= 255
        for sample in top
    ):
        raise IntraPredictionError(
            "chroma vertical 8x8 prediction requires eight 8-bit filtered top samples"
        )
    return list(top) * 8


def predict_chroma_intra8x8_plane(
    top: Sequence[int], left: Sequence[int], top_left: int
) -> list[int]:
    """Predict 4:2:0 chroma with the normative 8x8 plane gradients and clipping."""
    for name, references in (("top", top), ("left", left)):
        if len(references) != 8 or any(
            not isinstance(sample, int)
            or isinstance(sample, bool)
            or not 0 <= sample <= 255
            for sample in references
        ):
            raise IntraPredictionError(
                f"chroma plane prediction requires eight 8-bit filtered {name} samples"
            )
    if (
        not isinstance(top_left, int)
        or isinstance(top_left, bool)
        or not 0 <= top_left <= 255
    ):
        raise IntraPredictionError(
            "chroma plane prediction requires an 8-bit filtered top-left sample"
        )

    horizontal_gradient = 0
    vertical_gradient = 0
    for index in range(4):
        top_reference = top_left if index == 3 else top[2 - index]
        left_reference = top_left if index == 3 else left[2 - index]
        horizontal_gradient += (index + 1) * (top[4 + index] - top_reference)
        vertical_gradient += (index + 1) * (left[4 + index] - left_reference)
    a = 16 * (top[7] + left[7])
    b = (34 * horizontal_gradient + 32) >> 6
    c = (34 * vertical_gradient + 32) >> 6

    prediction = [0] * 64
    for row in range(8):
        for column in range(8):
            value = (a + b * (column - 3) + c * (row - 3) + 16) >> 5
            prediction[row * 8 + column] = min(255, max(0, value))
    return prediction


def predict_chroma_intra8x8(
    mode: int,
    top: Optional[Sequence[int]] = None,
    left: Optional[Sequence[int]] = None,
    top_left: Optional[int] = None,
) -> list[int]:
    """Dispatch 4:2:0 chroma intra modes 0=DC, 1=Horizontal, 2=Vertical, 3=Plane."""
    if not isinstance(mode, int) or isinstance(mode, bool) or not 0 <= mode <= 3:
        raise IntraPredictionError("chroma intra prediction mode is outside [0,3]")
    if mode == 0:
        return predict_chroma_intra8x8_dc(top, left)
    if mode == 1:
        if left is None:
            raise IntraPredictionError("left edge is required for chroma horizontal prediction")
        return predict_chroma_intra8x8_horizontal(left)
    if mode == 2:
        if top is None:
            raise IntraPredictionError("top edge is required for chroma vertical prediction")
        return predict_chroma_intra8x8_vertical(top)
    if top is None or left is None or top_left is None:
        raise IntraPredictionError("top, left, and top-left edges are required for chroma plane prediction")
    return predict_chroma_intra8x8_plane(top, left, top_left)


def predict_luma_intra4x4_dc(
    top: Optional[Sequence[int]] = None, left: Optional[Sequence[int]] = None
) -> list[int]:
    """Predict a 4x4 luma block from the available top and left references."""
    for name, references in (("top", top), ("left", left)):
        if references is not None and (
            len(references) != 4
            or any(
                not isinstance(sample, int)
                or isinstance(sample, bool)
                or not 0 <= sample <= 255
                for sample in references
            )
        ):
            raise IntraPredictionError(f"DC 4x4 prediction requires four 8-bit {name} samples")

    if top is not None and left is not None:
        dc_value = (sum(top) + sum(left) + 4) >> 3
    elif top is not None:
        dc_value = (sum(top) + 2) >> 2
    elif left is not None:
        dc_value = (sum(left) + 2) >> 2
    else:
        dc_value = 128
    return [dc_value] * 16


def predict_luma_intra4x4_diagonal_down_left(top: Sequence[int]) -> list[int]:
    """Interpolate a 4x4 luma block from four top and four top-right samples."""
    if len(top) != 8 or any(
        not isinstance(sample, int)
        or isinstance(sample, bool)
        or not 0 <= sample <= 255
        for sample in top
    ):
        raise IntraPredictionError(
            "diagonal-down-left 4x4 prediction requires eight 8-bit top samples"
        )

    prediction = [0] * 16
    for row in range(4):
        for column in range(4):
            index = row + column
            last_index = min(index + 2, 7)
            prediction[row * 4 + column] = (
                top[index] + 2 * top[index + 1] + top[last_index] + 2
            ) >> 2
    return prediction


def predict_luma_intra4x4_diagonal_down_right(
    top: Sequence[int], left: Sequence[int], top_left: int
) -> list[int]:
    """Interpolate a 4x4 luma block from top, left, and top-left references."""
    for name, references, expected_length in (
        ("top", top, 8),
        ("left", left, 4),
    ):
        if len(references) != expected_length or any(
            not isinstance(sample, int)
            or isinstance(sample, bool)
            or not 0 <= sample <= 255
            for sample in references
        ):
            sample_count = "eight" if expected_length == 8 else "four"
            raise IntraPredictionError(
                f"diagonal-down-right 4x4 prediction requires {sample_count} 8-bit {name} samples"
            )
    if (
        not isinstance(top_left, int)
        or isinstance(top_left, bool)
        or not 0 <= top_left <= 255
    ):
        raise IntraPredictionError("diagonal-down-right 4x4 prediction requires an 8-bit top-left sample")

    def reference_at(position: int) -> int:
        if position == -1:
            return top_left
        if position < -1:
            return left[-position - 2]
        return top[position]

    prediction = [0] * 16
    for row in range(4):
        for column in range(4):
            position = column - row
            prediction[row * 4 + column] = (
                reference_at(position - 1)
                + 2 * reference_at(position)
                + reference_at(position + 1)
                + 2
            ) >> 2
    return prediction


def predict_luma_intra4x4_vertical_right(
    top: Sequence[int], left: Sequence[int], top_left: int
) -> list[int]:
    """Interpolate a 4x4 luma block from vertical-right reference phases."""
    for name, references, expected_length in (("top", top, 8), ("left", left, 4)):
        if len(references) != expected_length or any(
            not isinstance(sample, int)
            or isinstance(sample, bool)
            or not 0 <= sample <= 255
            for sample in references
        ):
            sample_count = "eight" if expected_length == 8 else "four"
            raise IntraPredictionError(
                f"vertical-right 4x4 prediction requires {sample_count} 8-bit {name} samples"
            )
    if (
        not isinstance(top_left, int)
        or isinstance(top_left, bool)
        or not 0 <= top_left <= 255
    ):
        raise IntraPredictionError("vertical-right 4x4 prediction requires an 8-bit top-left sample")

    def top_reference_at(position: int) -> int:
        return top_left if position == -1 else top[position]

    prediction = [0] * 16
    for row in range(4):
        for column in range(4):
            phase = 2 * column - row
            if phase >= 0 and phase % 2 == 0:
                center = phase // 2
                value = (
                    top_reference_at(center - 1)
                    + 2 * top_reference_at(center)
                    + top_reference_at(center + 1)
                    + 2
                ) >> 2
            elif phase >= 0:
                center = (phase - 1) // 2
                value = (top_reference_at(center - 1) + top_reference_at(center) + 1) >> 1
            elif phase == -1:
                value = (left[0] + top_left + 1) >> 1
            elif phase == -2:
                value = (left[0] + 2 * top_left + top[0] + 2) >> 2
            else:
                value = (left[0] + left[1] + 1) >> 1
            prediction[row * 4 + column] = value
    return prediction


def predict_luma_intra4x4_horizontal_down(
    top: Sequence[int], left: Sequence[int], top_left: int
) -> list[int]:
    """Transpose vertical-right prediction, extending the final left sample."""
    for name, references in (("top", top), ("left", left)):
        if len(references) != 4 or any(
            not isinstance(sample, int)
            or isinstance(sample, bool)
            or not 0 <= sample <= 255
            for sample in references
        ):
            raise IntraPredictionError(f"horizontal-down 4x4 prediction requires four 8-bit {name} samples")
    if (
        not isinstance(top_left, int)
        or isinstance(top_left, bool)
        or not 0 <= top_left <= 255
    ):
        raise IntraPredictionError("horizontal-down 4x4 prediction requires an 8-bit top-left sample")

    vertical_top = list(left) + [left[-1]] * 4
    vertical_prediction = predict_luma_intra4x4_vertical_right(
        vertical_top, top, top_left
    )
    return [
        vertical_prediction[column * 4 + row]
        for row in range(4)
        for column in range(4)
    ]


def predict_luma_intra4x4_vertical_left(top: Sequence[int]) -> list[int]:
    """Interpolate a 4x4 luma block using alternating top-reference phases."""
    if len(top) != 8 or any(
        not isinstance(sample, int)
        or isinstance(sample, bool)
        or not 0 <= sample <= 255
        for sample in top
    ):
        raise IntraPredictionError("vertical-left 4x4 prediction requires eight 8-bit top samples")

    prediction = [0] * 16
    for row in range(4):
        for column in range(4):
            phase = 2 * column + row
            if phase % 2 == 0:
                center = phase // 2
                value = (top[center] + top[center + 1] + 1) >> 1
            else:
                center = (phase - 1) // 2
                value = (top[center] + 2 * top[center + 1] + top[center + 2] + 2) >> 2
            prediction[row * 4 + column] = value
    return prediction


def predict_luma_intra4x4_horizontal_up(left: Sequence[int]) -> list[int]:
    """Interpolate a 4x4 luma block using alternating left-reference phases."""
    if len(left) != 8 or any(
        not isinstance(sample, int)
        or isinstance(sample, bool)
        or not 0 <= sample <= 255
        for sample in left
    ):
        raise IntraPredictionError("horizontal-up 4x4 prediction requires eight 8-bit left samples")

    prediction = [0] * 16
    for row in range(4):
        for column in range(4):
            phase = 2 * row + column
            if phase % 2 == 0:
                center = phase // 2
                value = (left[center] + left[center + 1] + 1) >> 1
            else:
                center = (phase - 1) // 2
                value = (left[center] + 2 * left[center + 1] + left[center + 2] + 2) >> 2
            prediction[row * 4 + column] = value
    return prediction


def inverse_transform_luma4x4(coefficients: Sequence[int]) -> list[int]:
    """Transform dequantized raster-order coefficients into residual samples."""
    if len(coefficients) != 16 or any(
        not isinstance(coefficient, int) or isinstance(coefficient, bool)
        for coefficient in coefficients
    ):
        raise InverseTransformError("inverse transform requires 16 integer coefficients")

    horizontal = [0] * 16
    for row in range(4):
        row_offset = row * 4
        transformed = _inverse_transform_4x4_line(coefficients[row_offset : row_offset + 4])
        horizontal[row_offset : row_offset + 4] = transformed

    residual = [0] * 16
    for column in range(4):
        transformed = _inverse_transform_4x4_line(
            [horizontal[row * 4 + column] for row in range(4)]
        )
        for row, value in enumerate(transformed):
            residual[row * 4 + column] = (value + 32) >> 6
    return residual


def inverse_transform_chroma_dc2x2(coefficients: Sequence[int]) -> list[int]:
    """Apply the 4:2:0 2x2 inverse Hadamard transform to chroma DC levels."""
    if len(coefficients) != 4 or any(
        not isinstance(coefficient, int) or isinstance(coefficient, bool)
        for coefficient in coefficients
    ):
        raise InverseTransformError("inverse chroma DC transform requires four integer coefficients")

    c00, c01, c10, c11 = coefficients
    return [
        c00 + c01 + c10 + c11,
        c00 - c01 + c10 - c11,
        c00 + c01 - c10 - c11,
        c00 - c01 - c10 + c11,
    ]


def inverse_transform_luma8x8(coefficients: Sequence[int]) -> list[int]:
    """Transform 8x8 dequantized raster-order coefficients into residual samples."""
    if len(coefficients) != 64 or any(
        not isinstance(coefficient, int) or isinstance(coefficient, bool)
        for coefficient in coefficients
    ):
        raise InverseTransformError("inverse transform requires 64 integer coefficients")

    horizontal = [0] * 64
    for row in range(8):
        row_offset = row * 8
        horizontal[row_offset : row_offset + 8] = _inverse_transform_8x8_line(
            coefficients[row_offset : row_offset + 8]
        )

    residual = [0] * 64
    for column in range(8):
        transformed = _inverse_transform_8x8_line(
            [horizontal[row * 8 + column] for row in range(8)]
        )
        for row, value in enumerate(transformed):
            residual[row * 8 + column] = (value + 32) >> 6
    return residual


def _inverse_transform_4x4_line(coefficients: Sequence[int]) -> list[int]:
    even_sum = coefficients[0] + coefficients[2]
    even_difference = coefficients[0] - coefficients[2]
    odd_difference = (coefficients[1] >> 1) - coefficients[3]
    odd_sum = coefficients[1] + (coefficients[3] >> 1)
    return [
        even_sum + odd_sum,
        even_difference + odd_difference,
        even_difference - odd_difference,
        even_sum - odd_sum,
    ]


def _inverse_transform_8x8_line(coefficients: Sequence[int]) -> list[int]:
    a0 = coefficients[0] + coefficients[4]
    a2 = coefficients[0] - coefficients[4]
    a4 = (coefficients[2] >> 1) - coefficients[6]
    a6 = coefficients[2] + (coefficients[6] >> 1)
    b0 = a0 + a6
    b2 = a2 + a4
    b4 = a2 - a4
    b6 = a0 - a6

    a1 = -coefficients[3] + coefficients[5] - coefficients[7] - (coefficients[7] >> 1)
    a3 = coefficients[1] + coefficients[7] - coefficients[3] - (coefficients[3] >> 1)
    a5 = -coefficients[1] + coefficients[7] + coefficients[5] + (coefficients[5] >> 1)
    a7 = coefficients[3] + coefficients[5] + coefficients[1] + (coefficients[1] >> 1)
    b1 = a1 + (a7 >> 2)
    b3 = a3 + (a5 >> 2)
    b5 = a5 - (a3 >> 2)
    b7 = a7 - (a1 >> 2)

    return [
        b0 + b7,
        b2 + b5,
        b4 + b3,
        b6 + b1,
        b6 - b1,
        b4 - b3,
        b2 - b5,
        b0 - b7,
    ]