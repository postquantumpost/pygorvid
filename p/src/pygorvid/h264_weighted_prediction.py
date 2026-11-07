"""H.264 clauses 7.3.3.2 and 8.4.3 prediction weights."""

from dataclasses import dataclass
from typing import Protocol

from .bitreader import BitReader
from .h264_sps import SPSInfo


class PredictionWeightError(ValueError):
    """Raised when prediction weights or reference indices are invalid."""


@dataclass(frozen=True)
class PredictionWeight:
    weights: tuple[int, int, int]
    offsets: tuple[int, int, int]


@dataclass(frozen=True)
class PredictionWeightTable:
    denominators: tuple[int, int, int]
    lists: tuple[tuple[PredictionWeight, ...], tuple[PredictionWeight, ...]]


def read_prediction_weight_table(
    reader: BitReader, sps: SPSInfo, maxima: tuple[int, int], b_slice: bool
) -> PredictionWeightTable:
    luma = reader.read_ue()
    if luma > 7:
        raise PredictionWeightError("luma_log2_weight_denom exceeds 7")
    chroma_present = sps.chroma_format_idc != 0 and not sps.separate_colour_plane
    chroma = reader.read_ue() if chroma_present else 0
    if chroma > 7:
        raise PredictionWeightError("chroma_log2_weight_denom exceeds 7")
    lists: list[tuple[PredictionWeight, ...]] = [(), ()]
    for list_index in range(2 if b_slice else 1):
        if not 0 <= maxima[list_index] <= 31:
            raise PredictionWeightError("prediction weight reference maximum exceeds 31")
        entries = []
        for _ in range(maxima[list_index] + 1):
            weights = [1 << luma, 1 << chroma, 1 << chroma]
            offsets = [0, 0, 0]
            luma_present = reader.read_bit()
            if luma_present:
                weights[0], offsets[0] = reader.read_se(), reader.read_se()
                if not -128 <= weights[0] <= 127 or not -128 <= offsets[0] <= 127:
                    raise PredictionWeightError("prediction weight or offset exceeds [-128,127]")
            if chroma_present and reader.read_bit():
                for component in (1, 2):
                    weights[component], offsets[component] = reader.read_se(), reader.read_se()
                    if not -128 <= weights[component] <= 127 or not -128 <= offsets[component] <= 127:
                        raise PredictionWeightError("prediction weight or offset exceeds [-128,127]")
            entries.append(PredictionWeight(
                (weights[0], weights[1], weights[2]),
                (offsets[0], offsets[1], offsets[2]),
            ))
        lists[list_index] = tuple(entries)
    return PredictionWeightTable((luma, chroma, chroma), (lists[0], lists[1]))


class WeightedReference(Protocol):
    @property
    def picture_order_count(self) -> int: ...

    @property
    def long_term(self) -> bool: ...


@dataclass(frozen=True)
class SampleWeights:
    denominators: tuple[int, int, int]
    lists: tuple[PredictionWeight, PredictionWeight]


@dataclass(frozen=True)
class InterWeighting:
    table: PredictionWeightTable | None = None
    implicit: bool = False
    current_picture_order_count: int = 0

    def select(
        self,
        indices: tuple[int, int],
        references: tuple[WeightedReference | None, WeightedReference | None],
    ) -> SampleWeights | None:
        if self.table is not None:
            lists = [PredictionWeight((1, 1, 1), (0, 0, 0))] * 2
            for list_index, index in enumerate(indices):
                if index >= 0:
                    if index >= len(self.table.lists[list_index]):
                        raise PredictionWeightError("prediction weight reference is unavailable")
                    lists[list_index] = self.table.lists[list_index][index]
            return SampleWeights(self.table.denominators, (lists[0], lists[1]))
        first, second = references
        if self.implicit and first is not None and second is not None:
            td = min(127, max(-128, second.picture_order_count - first.picture_order_count))
            w1 = 32
            if td and not first.long_term and not second.long_term:
                tb = min(127, max(-128, self.current_picture_order_count - first.picture_order_count))
                tx = (16384 + abs(td) // 2) // abs(td) * (1 if td > 0 else -1)
                candidate = min(1023, max(-1024, (tb * tx + 32) >> 6)) >> 2
                if -64 <= candidate <= 128:
                    w1 = candidate
            return SampleWeights((5, 5, 5), (
                PredictionWeight((64 - w1,) * 3, (0, 0, 0)),
                PredictionWeight((w1,) * 3, (0, 0, 0)),
            ))
        return None


def combine(
    first: int | None, second: int | None, weights: SampleWeights | None, component: int
) -> int:
    if first is None and second is None:
        raise PredictionWeightError("inter partition has no active prediction list")
    if weights is None:
        if first is not None and second is not None:
            return (first + second + 1) >> 1
        if first is not None:
            return first
        if second is not None:
            return second
        raise PredictionWeightError("inter partition has no active prediction list")
    denominator = weights.denominators[component]
    a, b = weights.lists
    if first is not None and second is not None:
        value = ((first * a.weights[component] + second * b.weights[component]
                  + (1 << denominator)) >> (denominator + 1))
        value += (a.offsets[component] + b.offsets[component] + 1) >> 1
    else:
        entry = a if first is not None else b
        sample = first if first is not None else second
        if sample is None:
            raise PredictionWeightError("inter partition has no active prediction list")
        rounding = (1 << (denominator - 1)) if denominator else 0
        value = ((sample * entry.weights[component] + rounding) >> denominator) + entry.offsets[component]
    return min(255, max(0, value))
