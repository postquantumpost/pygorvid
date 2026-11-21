"""Synchronous inter-macroblock reconstruction helpers."""

from .h264_weighted_prediction import PredictionWeightError, combine


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


def _inter_samples(frames, params, timeout_check):
    partitions, macroblock_x, macroblock_y, _frame_width, _frame_height, residual = params
    luma, cb, cr = bytearray(256), bytearray(64), bytearray(64)
    for x, y, width, height, selected, weights in partitions:
        predictions = [
            None if entry is None else _prediction(
                frames[entry[0]], macroblock_x, macroblock_y, x, y, width, height,
                entry[1], timeout_check,
            )
            for entry in selected
        ]
        samples = _combine_predictions(predictions, weights)
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
