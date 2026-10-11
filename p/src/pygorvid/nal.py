"""H.264 NAL-header parsing and EBSP-to-RBSP conversion."""

from dataclasses import dataclass


class NALUnitError(ValueError):
    """Raised for an invalid NAL header or EBSP payload."""


@dataclass(frozen=True)
class NALHeader:
    reference_idc: int
    unit_type: int


def parse_nal_header(nal: bytes) -> NALHeader:
    if not nal:
        raise NALUnitError("NAL unit is empty")
    if nal[0] & 0x80:
        raise NALUnitError("NAL forbidden_zero_bit is set")
    return NALHeader(reference_idc=(nal[0] >> 5) & 0x03, unit_type=nal[0] & 0x1F)


def split_length_prefixed_nals(data: bytes, length_size: int) -> tuple[bytes, ...]:
    """Split one AVC sample into owned NAL units using its avcC prefix width."""
    if not isinstance(data, bytes):
        raise NALUnitError("AVC sample data must be bytes")
    if (
        not isinstance(length_size, int)
        or isinstance(length_size, bool)
        or length_size not in (1, 2, 4)
    ):
        raise NALUnitError("AVC NAL length size must be 1, 2, or 4")
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


def ebsp_to_rbsp(ebsp: bytes) -> bytes:
    rbsp = bytearray()
    zero_count = 0
    for index, value in enumerate(ebsp):
        if zero_count == 2:
            if value == 0x03:
                if index + 1 == len(ebsp) or ebsp[index + 1] > 0x03:
                    raise NALUnitError("malformed emulation-prevention sequence")
                zero_count = 0
                continue
            if value <= 0x02:
                raise NALUnitError("malformed emulation-prevention sequence")
        rbsp.append(value)
        zero_count = zero_count + 1 if value == 0 else 0
    return bytes(rbsp)