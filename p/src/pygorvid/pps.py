"""Compatibility alias for H.264 picture parameter sets."""

import sys
from . import h264_pps as _implementation

sys.modules[__name__] = _implementation