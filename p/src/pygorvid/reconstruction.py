"""Compatibility alias for H.264 reconstruction."""

import sys
from . import h264_reconstruction as _implementation

sys.modules[__name__] = _implementation