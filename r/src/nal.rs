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
