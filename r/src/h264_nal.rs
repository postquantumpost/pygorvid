use std::io::{self, ErrorKind};

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct NalHeader {
    pub reference_idc: u8,
    pub unit_type: u8,
}

pub fn parse_nal_header(nal: &[u8]) -> io::Result<NalHeader> {
    let header = *nal
        .first()
        .ok_or_else(|| io::Error::new(ErrorKind::InvalidInput, "NAL unit is empty"))?;
    if header & 0x80 != 0 {
        return Err(io::Error::new(
            ErrorKind::InvalidData,
            "NAL forbidden_zero_bit is set",
        ));
    }
    Ok(NalHeader {
        reference_idc: (header >> 5) & 0x03,
        unit_type: header & 0x1f,
    })
}

pub use crate::codec_sample::split_length_prefixed_nals;

/// Splits H.264 samples, recovering an Annex B end marker appended to the final VCL NAL.
pub fn split_h264_sample_nals(data: &[u8], length_size: usize) -> io::Result<Vec<&[u8]>> {
    let mut nals = split_length_prefixed_nals(data, length_size)?;
    if let Some(&nal) = nals.last() {
        if matches!(nal[0] & 0x1f, 1 | 5)
            && nal.len() >= 4
            && matches!(nal[nal.len() - 1], 0x0a | 0x0b)
            && nal[nal.len() - 4..nal.len() - 1] == [0, 0, 1]
        {
            let mut end = nal.len() - 4;
            if end > 0 && nal[end - 1] == 0 {
                end -= 1;
            }
            if end > 1 {
                // The start code is framing, not EBSP; preserve the empty end-marker NAL.
                nals.pop();
                nals.push(&nal[..end]);
                nals.push(&nal[nal.len() - 1..]);
            }
        }
    }
    Ok(nals)
}

pub fn ebsp_to_rbsp(ebsp: &[u8]) -> io::Result<Vec<u8>> {
    let mut rbsp = Vec::with_capacity(ebsp.len());
    let mut zero_count = 0;
    for (index, value) in ebsp.iter().copied().enumerate() {
        if zero_count == 2 {
            if value == 0x03 {
                if index + 1 == ebsp.len() || ebsp[index + 1] > 0x03 {
                    return Err(io::Error::new(
                        ErrorKind::InvalidData,
                        "malformed emulation-prevention sequence",
                    ));
                }
                zero_count = 0;
                continue;
            }
            if value <= 0x02 {
                return Err(io::Error::new(
                    ErrorKind::InvalidData,
                    "malformed emulation-prevention sequence",
                ));
            }
        }
        rbsp.push(value);
        zero_count = if value == 0 { zero_count + 1 } else { 0 };
    }
    Ok(rbsp)
}
