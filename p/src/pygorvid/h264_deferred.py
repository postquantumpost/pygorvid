"""Synchronous inter-macroblock reconstruction helpers."""

import numpy as np

from .h264_weighted_prediction import PredictionWeightError, combine


_NUMPY_BATCH_MIN_SAMPLES = 64


class InterResidual:
    """Inter residual samples captured during parsing and added after prediction."""

    def __init__(self) -> None:
        self.luma = [0] * 256
        self.chroma = [[0] * 64, [0] * 64]

    def apply(self, luma: bytearray, cb: bytearray, cr: bytearray) -> None:
        from .h264_reconstruction import reconstruct_chroma420_macroblock

        for index, residual in enumerate(self.luma):
            if residual:
                luma[index] = min(255, max(0, luma[index] + residual))
        cb[:] = reconstruct_chroma420_macroblock(cb, self.chroma[0])
        cr[:] = reconstruct_chroma420_macroblock(cr, self.chroma[1])


def _luma_block(frame, macroblock_x, macroblock_y, x, y, width, height, motion, timeout_check):
    from .h264_reconstruction import _LumaFractionalSampleCache

    mv_x, mv_y = motion
    frame._validate_plane(frame.y, frame.width, frame.height, frame.y_stride)
    interpolator = _LumaFractionalSampleCache(
        frame.y, frame.width, frame.height, frame.y_stride, timeout_check
    )
    return [
        interpolator.sample(
            macroblock_x * 16 + x + column + (mv_x >> 2),
            macroblock_y * 16 + y + row + (mv_y >> 2),
            mv_x & 3,
            mv_y & 3,
        )
        for row in range(height)
        for column in range(width)
    ]


def _chroma_blocks(frame, macroblock_x, macroblock_y, x, y, width, height, motion):
    from .h264_reconstruction import interpolate_chroma_fractional_sample

    mv_x, mv_y = motion
    chroma_width = (frame.width + 1) // 2
    chroma_height = (frame.height + 1) // 2
    planes = []
    for plane, stride in ((frame.u, frame.u_stride), (frame.v, frame.v_stride)):
        frame._validate_plane(plane, chroma_width, chroma_height, stride)
        planes.append([
            interpolate_chroma_fractional_sample(
                plane,
                chroma_width,
                chroma_height,
                stride,
                macroblock_x * 8 + x // 2 + column + (mv_x >> 3),
                macroblock_y * 8 + y // 2 + row + (mv_y >> 3),
                mv_x & 7,
                mv_y & 7,
            )
            for row in range(height // 2)
            for column in range(width // 2)
        ])
    return planes[0], planes[1]


def _prediction(frame, macroblock_x, macroblock_y, x, y, width, height, motion, timeout_check):
    luma = _luma_block(
        frame, macroblock_x, macroblock_y, x, y, width, height, motion, timeout_check
    )
    cb, cr = _chroma_blocks(
        frame, macroblock_x, macroblock_y, x, y, width, height, motion
    )
    return luma, cb, cr


def _average(first, second):
    return [(a + b + 1) >> 1 for a, b in zip(first, second)]


def _p_skip_samples(frames, params, timeout_check):
    macroblock_x, macroblock_y, motion, weights = params
    samples = _prediction(
        frames[0], macroblock_x, macroblock_y, 0, 0, 16, 16, motion, timeout_check
    )
    if weights is None:
        return samples
    return tuple(
        [combine(sample, None, weights, component) for sample in plane]
        for component, plane in enumerate(samples)
    )


def _b_direct_samples(frames, params, timeout_check):
    motion_vectors, macroblock_x, macroblock_y, weights = params
    predictions = [
        None if frame is None else _prediction(
            frame, macroblock_x, macroblock_y, 0, 0, 16, 16, motion, timeout_check
        )
        for frame, motion in zip(frames, motion_vectors)
    ]
    return _combine_predictions(predictions, weights)


def _combine_predictions(predictions, weights):
    first, second = predictions
    if first is None and second is None:
        raise PredictionWeightError("inter partition has no active prediction list")
    if weights is None:
        if first is None:
            return second
        if second is None:
            return first
        return tuple(_average(a, b) for a, b in zip(first, second))
    lengths = tuple(len(plane) for plane in (first if first is not None else second))
    return tuple([
        combine(
            first[component][index] if first is not None else None,
            second[component][index] if second is not None else None,
            weights,
            component,
        )
        for index in range(length)
    ] for component, length in enumerate(lengths))


def _combine_prediction_batch(partition_predictions):
    if len(partition_predictions) < 2:
        return [_combine_predictions(predictions, weights)
                for predictions, weights in partition_predictions]

    total_samples = sum(
        len(predictions[0][component] if predictions[0] is not None
            else predictions[1][component])
        for predictions, _weights in partition_predictions
        for component in range(3)
    )
    if total_samples < _NUMPY_BATCH_MIN_SAMPLES:
        return [_combine_predictions(predictions, weights)
                for predictions, weights in partition_predictions]

    combined = [tuple() for _ in partition_predictions]
    for component in range(3):
        first_chunks = []
        second_chunks = []
        first_active_chunks = []
        second_active_chunks = []
        weighted_chunks = []
        denominator_chunks = []
        first_weight_chunks = []
        second_weight_chunks = []
        first_offset_chunks = []
        second_offset_chunks = []
        lengths = []
        for predictions, weights in partition_predictions:
            first, second = predictions
            active = first if first is not None else second
            if active is None:
                raise PredictionWeightError(
                    "inter partition has no active prediction list"
                )
            length = len(active[component])
            lengths.append(length)
            first_values = np.zeros(length, dtype=np.int64)
            second_values = np.zeros(length, dtype=np.int64)
            if first is not None:
                first_values[:] = first[component]
            if second is not None:
                second_values[:] = second[component]
            first_chunks.append(first_values)
            second_chunks.append(second_values)
            first_active_chunks.append(
                np.full(length, first is not None, dtype=np.bool_)
            )
            second_active_chunks.append(
                np.full(length, second is not None, dtype=np.bool_)
            )
            weighted_chunks.append(
                np.full(length, weights is not None, dtype=np.bool_)
            )
            if weights is None:
                denominator, first_weight, second_weight = 0, 0, 0
                first_offset, second_offset = 0, 0
            else:
                denominator = weights.denominators[component]
                first_weight = weights.lists[0].weights[component]
                second_weight = weights.lists[1].weights[component]
                first_offset = weights.lists[0].offsets[component]
                second_offset = weights.lists[1].offsets[component]
            denominator_chunks.append(np.full(length, denominator, dtype=np.int64))
            first_weight_chunks.append(
                np.full(length, first_weight, dtype=np.int64)
            )
            second_weight_chunks.append(
                np.full(length, second_weight, dtype=np.int64)
            )
            first_offset_chunks.append(
                np.full(length, first_offset, dtype=np.int64)
            )
            second_offset_chunks.append(
                np.full(length, second_offset, dtype=np.int64)
            )

        first = np.concatenate(first_chunks)
        second = np.concatenate(second_chunks)
        first_active = np.concatenate(first_active_chunks)
        second_active = np.concatenate(second_active_chunks)
        weighted = np.concatenate(weighted_chunks)
        denominator = np.concatenate(denominator_chunks)
        first_weight = np.concatenate(first_weight_chunks)
        second_weight = np.concatenate(second_weight_chunks)
        first_offset = np.concatenate(first_offset_chunks)
        second_offset = np.concatenate(second_offset_chunks)
        both_active = first_active & second_active

        values = np.where(first_active, first, second)
        values = np.where(
            (~weighted) & both_active,
            (first + second + 1) >> 1,
            values,
        )
        weighted_both = weighted & both_active
        values = np.where(
            weighted_both,
            (
                (first * first_weight + second * second_weight
                 + np.left_shift(1, denominator))
                >> (denominator + 1)
            ) + ((first_offset + second_offset + 1) >> 1),
            values,
        )
        weighted_single = weighted & ~both_active
        active_weight = np.where(first_active, first_weight, second_weight)
        active_offset = np.where(first_active, first_offset, second_offset)
        rounding = np.where(
            denominator > 0,
            np.left_shift(1, np.maximum(denominator - 1, 0)),
            0,
        )
        values = np.where(
            weighted_single,
            ((values * active_weight + rounding) >> denominator) + active_offset,
            values,
        )
        values = np.clip(values, 0, 255)

        offset = 0
        for index, length in enumerate(lengths):
            combined[index] = (*combined[index], values[offset:offset + length].tolist())
            offset += length
    return combined


def _inter_samples(frames, params, timeout_check):
    partitions, macroblock_x, macroblock_y, _frame_width, _frame_height, residual = params
    luma, cb, cr = bytearray(256), bytearray(64), bytearray(64)
    pending = []
    for x, y, width, height, selected, weights in partitions:
        predictions = [
            None if entry is None else _prediction(
                frames[entry[0]], macroblock_x, macroblock_y, x, y, width, height,
                entry[1], timeout_check,
            )
            for entry in selected
        ]
        pending.append((x, y, width, height, predictions, weights))
    combined = _combine_prediction_batch([
        (predictions, weights)
        for _x, _y, _width, _height, predictions, weights in pending
    ])
    for (x, y, width, height, _predictions, _weights), samples in zip(
        pending, combined
    ):
        for row in range(height):
            luma[(y + row) * 16 + x:(y + row) * 16 + x + width] = bytes(
                samples[0][row * width:(row + 1) * width]
            )
        half = width // 2
        for row in range(height // 2):
            destination = (y // 2 + row) * 8 + x // 2
            cb[destination:destination + half] = bytes(
                samples[1][row * half:(row + 1) * half]
            )
            cr[destination:destination + half] = bytes(
                samples[2][row * half:(row + 1) * half]
            )
    residual.apply(luma, cb, cr)
    return luma, cb, cr


_JOBS = {"pskip": _p_skip_samples, "bdirect": _b_direct_samples, "inter": _inter_samples}


def run_job(kind, frames, params, timeout_check=None):
    """Compute the reconstructed samples for one macroblock."""
    return _JOBS[kind](frames, params, timeout_check)
