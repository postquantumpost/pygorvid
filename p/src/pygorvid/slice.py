"""Compatibility alias for H.264 slice parsing."""

import sys
from . import h264_slice as _implementation

sys.modules[__name__] = _implementation