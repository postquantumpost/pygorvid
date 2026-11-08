"""H.264 NAL-header parsing and EBSP-to-RBSP conversion."""

from dataclasses import dataclass


from .codec_sample import NALUnitError, split_length_prefixed_nals


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


def split_h264_sample_nals(data: bytes, length_size: int) -> tuple[bytes, ...]:
    """Recover an Annex B end marker appended to the sample's final VCL NAL."""
    nals = split_length_prefixed_nals(data, length_size)
    if nals:
        nal = nals[-1]
        if (
            nal[0] & 0x1F in (1, 5)
            and len(nal) >= 4
            and nal[-1] in (0x0A, 0x0B)
            and nal[-4:-1] == b"\x00\x00\x01"
        ):
            end = len(nal) - 4
            if end > 0 and nal[end - 1] == 0:
                end -= 1
            if end > 1:
                # The start code is framing, not EBSP; preserve the empty end-marker NAL.
                return (*nals[:-1], nal[:end], nal[-1:])
    return nals


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