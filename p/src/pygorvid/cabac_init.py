"""Compatibility alias for H.264 CABAC initialization tables."""

import sys
from . import h264_cabac_init as _implementation

sys.modules[__name__] = _implementation