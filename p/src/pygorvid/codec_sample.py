"""Codec-neutral MP4 length-prefix framing."""

class NALUnitError(ValueError):
    """Raised for an invalid NAL header or EBSP payload."""


def split_length_prefixed_nals(data: bytes, length_size: int) -> tuple[bytes, ...]:
    """Split a compressed sample using its configured NAL prefix width."""
    if not isinstance(data, bytes):
        raise NALUnitError("sample data must be bytes")
    if (
        not isinstance(length_size, int)
        or isinstance(length_size, bool)
        or length_size not in (1, 2, 4)
    ):
        raise NALUnitError("NAL length size must be 1, 2, or 4")
    nals = []
    offset = 0
    while offset < len(data):
        if len(data) - offset < length_size:
            raise NALUnitError("truncated NAL length prefix")
        nal_size = int.from_bytes(data[offset : offset + length_size], "big")
        offset += length_size
        if nal_size == 0:
            raise NALUnitError("NAL unit has zero length")
        end = offset + nal_size
        if end > len(data):
            raise NALUnitError("NAL unit is truncated")
        nals.append(data[offset:end])
        offset = end
    return tuple(nals)


