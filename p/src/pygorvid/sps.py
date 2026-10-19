"""Compatibility alias for H.264 sequence parameter sets."""

import sys
from . import h264_sps as _implementation

sys.modules[__name__] = _implementation