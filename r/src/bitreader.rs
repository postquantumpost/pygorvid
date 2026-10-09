use std::io::{self, ErrorKind};

#[derive(Clone)]
pub struct BitReader<'a> {
    data: &'a [u8],
    bit_offset: usize,
}

impl<'a> BitReader<'a> {
    pub fn new(data: &'a [u8]) -> Self {
        Self {
            data,
            bit_offset: 0,
        }
    }

    pub fn read_bits(&mut self, count: u8) -> io::Result<u32> {
        if count > 32 {
            return Err(io::Error::new(
                ErrorKind::InvalidInput,
                "bit read width must be between 0 and 32",
            ));
        }
        let available = self.data.len() * 8 - self.bit_offset;
        if usize::from(count) > available {
            return Err(io::Error::new(
                ErrorKind::UnexpectedEof,
                "truncated bitstream",
            ));
        }
        let mut value = 0u32;
        for _ in 0..count {
            let byte_index = self.bit_offset / 8;
            let shift = 7 - self.bit_offset % 8;
            value = (value << 1) | u32::from((self.data[byte_index] >> shift) & 1);
            self.bit_offset += 1;
        }
        Ok(value)
    }

    pub fn read_bit(&mut self) -> io::Result<bool> {
        Ok(self.read_bits(1)? != 0)
    }

    pub fn align_to_byte(&mut self) {
        let remainder = self.bit_offset % 8;
        if remainder != 0 {
            self.bit_offset += 8 - remainder;
        }
    }

    pub fn more_rbsp_data(&self) -> bool {
        let bit_count = self.data.len() * 8;
        if self.bit_offset >= bit_count {
            return false;
        }
        let read_at = |offset: usize| (self.data[offset / 8] >> (7 - offset % 8)) & 1 != 0;
        if !read_at(self.bit_offset) {
            return true;
        }
        ((self.bit_offset + 1)..bit_count).any(read_at)
    }

    pub fn read_ue(&mut self) -> io::Result<u32> {
        let start = self.bit_offset;
        let mut leading_zeros = 0u8;
        loop {
            let bit = match self.read_bit() {
                Ok(bit) => bit,
                Err(error) => {
                    self.bit_offset = start;
                    return Err(error);
                }
            };
            if bit {
                break;
            }
            leading_zeros += 1;
            if leading_zeros > 32 {
                self.bit_offset = start;
                return Err(io::Error::new(
                    ErrorKind::InvalidData,
                    "Exp-Golomb value exceeds uint32",
                ));
            }
        }
        let suffix = match self.read_bits(leading_zeros) {
            Ok(suffix) => suffix,
            Err(error) => {
                self.bit_offset = start;
                return Err(error);
            }
        };
        let value = ((1u64 << leading_zeros) - 1) + u64::from(suffix);
        if value > u64::from(u32::MAX) {
            self.bit_offset = start;
            return Err(io::Error::new(
                ErrorKind::InvalidData,
                "Exp-Golomb value exceeds uint32",
            ));
        }
        Ok(value as u32)
    }

    pub fn read_se(&mut self) -> io::Result<i64> {
        let code_number = self.read_ue()?;
        let magnitude = (u64::from(code_number) + 1) / 2;
        if code_number & 1 == 0 {
            Ok(-(magnitude as i64))
        } else {
            Ok(magnitude as i64)
        }
    }
}
