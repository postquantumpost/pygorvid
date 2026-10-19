use std::io::{self, ErrorKind};

use crate::{decode_cavlc_residual_block, BitReader};

const INTRA_CBP: [u8; 48] = [
    47, 31, 15, 0, 23, 27, 29, 30, 7, 11, 13, 14, 39, 43, 45, 46, 16, 3, 5, 10, 12, 19, 21, 26, 28,
    35, 37, 42, 44, 1, 2, 4, 8, 17, 18, 20, 24, 6, 9, 22, 25, 32, 33, 34, 36, 40, 38, 41,
];
const INTER_CBP: [u8; 48] = [
    0, 16, 1, 2, 4, 8, 32, 3, 5, 10, 12, 15, 47, 7, 11, 13, 14, 6, 9, 31, 35, 37, 42, 44, 33, 34,
    36, 40, 39, 43, 45, 46, 17, 18, 20, 24, 19, 21, 26, 28, 23, 27, 29, 30, 22, 25, 38, 41,
];
const BLOCK_SCAN: [usize; 16] = [0, 1, 4, 5, 2, 3, 6, 7, 8, 9, 12, 13, 10, 11, 14, 15];

fn invalid(message: &str) -> io::Error {
    io::Error::new(ErrorKind::InvalidData, message)
}

#[derive(Clone, Default)]
pub(crate) struct CavlcSyntax {
    pub(crate) counts: [u8; 24],
    left: Option<[u8; 24]>,
    top: Option<[u8; 24]>,
    luma_index: usize,
    chroma_index: usize,
    chroma_dc_index: usize,
    luma_cbp: u8,
    chroma_cbp: u8,
    intra: bool,
    skip_remaining: u32,
    coded_after_skip: bool,
}

impl CavlcSyntax {
    pub(crate) fn begin(&mut self, left: Option<[u8; 24]>, top: Option<[u8; 24]>) {
        self.counts = [0; 24];
        self.left = left;
        self.top = top;
        self.luma_index = 0;
        self.chroma_index = 0;
        self.chroma_dc_index = 0;
        self.luma_cbp = 0;
        self.chroma_cbp = 0;
        self.intra = false;
    }

    pub(crate) fn skipped(&mut self, reader: &mut BitReader<'_>) -> io::Result<bool> {
        if self.skip_remaining > 0 {
            self.skip_remaining -= 1;
            return Ok(true);
        }
        if self.coded_after_skip {
            self.coded_after_skip = false;
            return Ok(false);
        }
        let run = reader.read_ue()?;
        if run == 0 {
            return Ok(false);
        }
        self.skip_remaining = run - 1;
        self.coded_after_skip = true;
        Ok(true)
    }

    pub(crate) fn finished(&self, reader: &BitReader<'_>) -> bool {
        self.skip_remaining == 0 && !reader.more_rbsp_data()
    }

    pub(crate) fn mb_type(&mut self, reader: &mut BitReader<'_>, slice_type: u8) -> io::Result<u8> {
        let value = reader.read_ue()?;
        let base = match slice_type % 5 {
            2 => 0,
            0 => 5,
            1 => 23,
            _ => return Err(invalid("unsupported CAVLC slice type")),
        };
        if value > base + 25 {
            return Err(invalid("CAVLC mb_type exceeds slice range"));
        }
        self.intra = value >= base;
        if self.intra {
            let intra_type = value - base;
            if intra_type == 25 {
                self.counts.fill(16);
            } else if intra_type > 0 {
                self.luma_cbp = if intra_type >= 13 { 15 } else { 0 };
                self.chroma_cbp = ((intra_type - 1) / 4 % 3) as u8;
            }
        }
        Ok(value as u8)
    }

    pub(crate) fn luma_cbp(&mut self, reader: &mut BitReader<'_>) -> io::Result<u8> {
        let code = reader.read_ue()? as usize;
        let pattern = *(if self.intra { &INTRA_CBP } else { &INTER_CBP })
            .get(code)
            .ok_or_else(|| invalid("CAVLC coded_block_pattern exceeds [0,47]"))?;
        self.luma_cbp = pattern & 15;
        self.chroma_cbp = pattern >> 4;
        Ok(self.luma_cbp)
    }

    pub(crate) fn chroma_cbp(&self) -> u8 {
        self.chroma_cbp
    }

    pub(crate) fn unsigned(reader: &mut BitReader<'_>, maximum: u32) -> io::Result<u8> {
        let value = reader.read_ue()?;
        if value > maximum {
            return Err(invalid("CAVLC syntax value exceeds supported range"));
        }
        Ok(value as u8)
    }

    pub(crate) fn signed(reader: &mut BitReader<'_>) -> io::Result<i32> {
        i32::try_from(reader.read_se()?).map_err(|_| invalid("CAVLC signed value exceeds int32"))
    }

    pub(crate) fn reference(reader: &mut BitReader<'_>, maximum: u32) -> io::Result<u8> {
        if maximum == 0 {
            Ok(0)
        } else if maximum == 1 {
            Ok(u8::from(!reader.read_bit()?))
        } else {
            Self::unsigned(reader, maximum)
        }
    }

    pub(crate) fn prediction_mode(reader: &mut BitReader<'_>, predicted: u8) -> io::Result<u8> {
        if reader.read_bit()? {
            return Ok(predicted);
        }
        let remaining = reader.read_bits(3)? as u8;
        Ok(remaining + u8::from(remaining >= predicted))
    }

    fn nc(&self, plane: usize, block: usize) -> i32 {
        let (offset, width) = if plane == 0 {
            (0, 4)
        } else {
            (16 + (plane - 1) * 4, 2)
        };
        let x = block % width;
        let y = block / width;
        let left = if x > 0 {
            Some(self.counts[offset + block - 1])
        } else {
            self.left
                .map(|counts| counts[offset + y * width + width - 1])
        };
        let top = if y > 0 {
            Some(self.counts[offset + block - width])
        } else {
            self.top
                .map(|counts| counts[offset + (width - 1) * width + x])
        };
        match (left, top) {
            (Some(left), Some(top)) => (i32::from(left) + i32::from(top) + 1) / 2,
            (Some(value), None) | (None, Some(value)) => i32::from(value),
            _ => 0,
        }
    }

    pub(crate) fn residual(
        &mut self,
        reader: &mut BitReader<'_>,
        category: u8,
    ) -> io::Result<([i32; 16], bool)> {
        let (plane, block, capacity, nc) = match category {
            0 => (0, 0, 16, self.nc(0, 0)),
            1 | 2 => {
                while self.luma_index < 16 && self.luma_cbp & (1 << (self.luma_index / 4)) == 0 {
                    self.luma_index += 1;
                }
                if self.luma_index >= 16 {
                    return Err(invalid("CAVLC luma residual exceeds coded block pattern"));
                }
                let block = BLOCK_SCAN[self.luma_index];
                self.luma_index += 1;
                (
                    0,
                    block,
                    if category == 1 { 15 } else { 16 },
                    self.nc(0, block),
                )
            }
            3 => {
                let plane = self.chroma_dc_index + 1;
                self.chroma_dc_index += 1;
                if plane > 2 {
                    return Err(invalid("CAVLC chroma DC plane exceeds 4:2:0"));
                }
                (plane, 0, 4, -1)
            }
            4 => {
                let plane = self.chroma_index / 4 + 1;
                let block = self.chroma_index % 4;
                self.chroma_index += 1;
                if plane > 2 {
                    return Err(invalid("CAVLC chroma AC plane exceeds 4:2:0"));
                }
                (plane, block, 15, self.nc(plane, block))
            }
            _ => return Err(invalid("unsupported CAVLC residual category")),
        };
        let decoded = decode_cavlc_residual_block(reader, nc, capacity)?;
        if category != 0 && category != 3 {
            let offset = if plane == 0 { 0 } else { 16 + (plane - 1) * 4 };
            self.counts[offset + block] = decoded.total_coeff;
        }
        let mut levels = [0; 16];
        levels[..capacity].copy_from_slice(&decoded.coefficients);
        Ok((levels, decoded.total_coeff != 0))
    }

    pub(crate) fn residual8x8(&mut self, reader: &mut BitReader<'_>) -> io::Result<[i32; 64]> {
        let mut levels = [0; 64];
        for group in 0..4 {
            let (block, _) = self.residual(reader, 2)?;
            for index in 0..16 {
                levels[4 * index + group] = block[index];
            }
        }
        Ok(levels)
    }
}
