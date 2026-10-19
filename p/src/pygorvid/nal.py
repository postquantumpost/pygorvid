"""Compatibility alias for H.264 NAL parsing."""

import sys
from . import h264_nal as _implementation

sys.modules[__name__] = _implementation