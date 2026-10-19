"""Deferred inter-macroblock reconstruction for multi-process slice decoding.

Parsing a P/B slice only needs reconstructed samples when an intra macroblock predicts from
its neighbors. With more than one thread, motion compensation and residual addition run in
forked worker processes (CPython threads would serialize on the GIL), and the parser waits
for results only at intra macroblocks and at the end of the slice. Output is identical to
the single-threaded decode.
"""

import dataclasses
import multiprocessing
import threading


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


class IntraNeighborsPending(Exception):
    """Raised when an intra macroblock would read samples that are still being reconstructed."""


def _luma_block(frame, macroblock_x, macroblock_y, x, y, width, height, motion, timeout_check):
    from .h264_reconstruction import _LumaFractionalSampleCache

    mv_x, mv_y = motion
    interpolator = _LumaFractionalSampleCache(
        frame.luma_plane_bytes(), frame.width, frame.height, frame.width, timeout_check
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
    for plane in (frame.u_plane_bytes(), frame.v_plane_bytes()):
        planes.append([
            interpolate_chroma_fractional_sample(
                plane,
                chroma_width,
                chroma_height,
                chroma_width,
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
    luma = _luma_block(frame, macroblock_x, macroblock_y, x, y, width, height, motion, timeout_check)
    cb, cr = _chroma_blocks(frame, macroblock_x, macroblock_y, x, y, width, height, motion)
    return luma, cb, cr


def _average(first, second):
    return [(a + b + 1) >> 1 for a, b in zip(first, second)]


def _p_skip_samples(frames, params, timeout_check):
    macroblock_x, macroblock_y, motion = params
    return _prediction(frames[0], macroblock_x, macroblock_y, 0, 0, 16, 16, motion, timeout_check)


def _b_direct_samples(frames, params, timeout_check):
    motion_vectors, macroblock_x, macroblock_y = params
    predictions = [
        None if frame is None else _prediction(
            frame, macroblock_x, macroblock_y, 0, 0, 16, 16, motion, timeout_check
        )
        for frame, motion in zip(frames, motion_vectors)
    ]
    if predictions[0] is None:
        return predictions[1]
    if predictions[1] is None:
        return predictions[0]
    return tuple(_average(a, b) for a, b in zip(predictions[0], predictions[1]))


def _inter_samples(frames, params, timeout_check):
    partitions, macroblock_x, macroblock_y, _frame_width, _frame_height, residual = params
    luma, cb, cr = bytearray(256), bytearray(64), bytearray(64)
    for x, y, width, height, selected in partitions:
        predictions = [
            _prediction(
                frames[frame_index], macroblock_x, macroblock_y, x, y, width, height,
                motion, timeout_check,
            )
            for frame_index, motion in (entry for entry in selected if entry is not None)
        ]
        if len(predictions) == 2:
            samples = tuple(_average(a, b) for a, b in zip(predictions[0], predictions[1]))
        else:
            samples = predictions[0]
        for row in range(height):
            luma[(y + row) * 16 + x:(y + row) * 16 + x + width] = bytes(
                samples[0][row * width:(row + 1) * width]
            )
        half = width // 2
        for row in range(height // 2):
            destination = (y // 2 + row) * 8 + x // 2
            cb[destination:destination + half] = bytes(samples[1][row * half:(row + 1) * half])
            cr[destination:destination + half] = bytes(samples[2][row * half:(row + 1) * half])
    residual.apply(luma, cb, cr)
    return luma, cb, cr


_JOBS = {"pskip": _p_skip_samples, "bdirect": _b_direct_samples, "inter": _inter_samples}


def run_job(kind, frames, params, timeout_check=None):
    """Compute (luma, cb, cr) samples for one deferred macroblock job."""
    return _JOBS[kind](frames, params, timeout_check)


class _DeferralState:
    def __init__(self, width_in_mbs: int) -> None:
        self.width_in_mbs = width_in_mbs
        self.job = None
        self.pending: set[int] = set()


_state = threading.local()

# Stand-in samples for deferred macroblocks until their reconstruction is applied.
PLACEHOLDER_SAMPLES = ((0,) * 256, (0,) * 64, (0,) * 64)


def reconstruct_or_defer(kind, frames, params, timeout_check=None):
    """Run a reconstruction job now, or stash it for the active slice driver and return None."""
    state = getattr(_state, "deferral", None)
    if state is None:
        return run_job(kind, frames, params, timeout_check)
    state.job = (kind, frames, params)
    return None


def _intra_neighbor_addresses(address: int, width_in_mbs: int):
    column = address % width_in_mbs
    above = address - width_in_mbs
    neighbors = []
    if column > 0:
        neighbors.append(address - 1)
    if above >= 0:
        neighbors.append(above)
        if column > 0:
            neighbors.append(above - 1)
        if column + 1 < width_in_mbs:
            neighbors.append(above + 1)
    return neighbors


def require_intra_neighbors(address: int) -> None:
    """Raise IntraNeighborsPending if an intra neighbor is still being reconstructed."""
    state = getattr(_state, "deferral", None)
    if state is None:
        return
    if any(neighbor in state.pending for neighbor in _intra_neighbor_addresses(address, state.width_in_mbs)):
        raise IntraNeighborsPending()


# Reference frames shared with forked workers, keyed by id() in the parent.
_FORK_FRAMES: dict = {}


def _run_forked_job(kind, keys, params):
    frames = tuple(None if key is None else _FORK_FRAMES[key] for key in keys)
    return run_job(kind, frames, params)


def parallel_reconstruction_available(threads: int) -> bool:
    return threads > 1 and "fork" in multiprocessing.get_all_start_methods()


class SliceReconstructionDriver:
    """Parser-side owner of the worker pool for one P/B slice."""

    def __init__(self, threads: int, width_in_mbs: int, first_address: int, reference_frames) -> None:
        self._first_address = first_address
        self._state = _DeferralState(width_in_mbs)
        self._results = {}
        _FORK_FRAMES.clear()
        for frame in reference_frames:
            _FORK_FRAMES[id(frame)] = frame
        self._pool = multiprocessing.get_context("fork").Pool(threads)

    def __enter__(self):
        _state.deferral = self._state
        return self

    def __exit__(self, *_):
        _state.deferral = None
        self._pool.terminate()
        self._pool.join()
        _FORK_FRAMES.clear()
        return False

    def take_job(self):
        job, self._state.job = self._state.job, None
        return job

    def submit(self, address, job, decoded, builder) -> None:
        kind, frames, params = job
        keys = []
        for frame in frames:
            if frame is not None and _FORK_FRAMES.get(id(frame)) is not frame:
                self._apply(address, run_job(kind, frames, params), decoded, builder)
                return
            keys.append(None if frame is None else id(frame))
        self._results[address] = self._pool.apply_async(_run_forked_job, (kind, tuple(keys), params))
        self._state.pending.add(address)

    def _apply(self, address, samples, decoded, builder) -> None:
        from .cabac import CABACPBMacroblockLayerResult, CABACSliceMacroblockResult

        self._state.pending.discard(address)
        luma, cb, cr = samples
        builder.place_macroblock(address, luma, cb, cr)
        index = address - self._first_address
        entry = decoded[index]
        layer = entry.macroblock
        if isinstance(layer, CABACPBMacroblockLayerResult):
            macroblock = dataclasses.replace(
                layer.macroblock, luma=tuple(luma), cb=tuple(cb), cr=tuple(cr)
            )
            decoded[index] = CABACSliceMacroblockResult(
                entry.address, dataclasses.replace(layer, macroblock=macroblock)
            )

    def _collect(self, address, decoded, builder) -> None:
        self._apply(address, self._results.pop(address).get(), decoded, builder)

    def apply_ready(self, decoded, builder) -> None:
        for address in [address for address, result in self._results.items() if result.ready()]:
            self._collect(address, decoded, builder)

    def wait_for_intra_neighbors(self, address, decoded, builder) -> None:
        for neighbor in _intra_neighbor_addresses(address, self._state.width_in_mbs):
            if neighbor in self._results:
                self._collect(neighbor, decoded, builder)

    def finish(self, decoded, builder) -> None:
        for address in sorted(self._results):
            self._collect(address, decoded, builder)
