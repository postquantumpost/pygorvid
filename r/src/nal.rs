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

/// Splits one length-prefixed AVC sample using its avcC NAL-length field width.
pub fn split_length_prefixed_nals(data: &[u8], length_size: usize) -> io::Result<Vec<&[u8]>> {
    if !matches!(length_size, 1 | 2 | 4) {
        return Err(io::Error::new(
            ErrorKind::InvalidInput,
            "AVC NAL length size must be 1, 2, or 4",
        ));
    }
    let mut nals = Vec::new();
    let mut offset = 0;
    while offset < data.len() {
        if data.len() - offset < length_size {
            return Err(io::Error::new(
                ErrorKind::UnexpectedEof,
                "truncated NAL length prefix",
            ));
        }
        let mut nal_size = 0_usize;
        for value in &data[offset..offset + length_size] {
            nal_size = nal_size
                .checked_mul(256)
                .and_then(|size| size.checked_add(usize::from(*value)))
                .ok_or_else(|| {
                    io::Error::new(
                        ErrorKind::InvalidData,
                        "NAL unit length exceeds supported range",
                    )
                })?;
        }
        offset += length_size;
        if nal_size == 0 {
            return Err(io::Error::new(
                ErrorKind::InvalidData,
                "NAL unit has zero length",
            ));
        }
        let end = offset
            .checked_add(nal_size)
            .filter(|end| *end <= data.len())
            .ok_or_else(|| io::Error::new(ErrorKind::UnexpectedEof, "NAL unit is truncated"))?;
        nals.push(&data[offset..end]);
        offset = end;
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
