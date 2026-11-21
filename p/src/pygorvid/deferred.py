"""Compatibility alias for H.264 inter-macroblock reconstruction helpers."""

import sys
from . import h264_deferred as _implementation

sys.modules[__name__] = _implementation
