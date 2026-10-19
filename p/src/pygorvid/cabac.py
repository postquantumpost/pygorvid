"""Compatibility alias for H.264 CABAC decoding."""

import sys
from . import h264_cabac as _implementation

sys.modules[__name__] = _implementation