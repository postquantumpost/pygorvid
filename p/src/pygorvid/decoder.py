"""Compatibility alias for the H.264 decoder module."""

import sys
from . import h264_decoder as _implementation

sys.modules[__name__] = _implementation