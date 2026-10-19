use std::io::{self, ErrorKind};

/// Splits a sample using its configured big-endian NAL-length field width.
pub fn split_length_prefixed_nals(data: &[u8], length_size: usize) -> io::Result<Vec<&[u8]>> {
    if !matches!(length_size, 1 | 2 | 4) {
        return Err(io::Error::new(
            ErrorKind::InvalidInput,
            "NAL length size must be 1, 2, or 4",
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
