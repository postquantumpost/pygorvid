use std::io::{self, ErrorKind};
use std::sync::OnceLock;

use crate::BitReader;

const COEFF_TOKEN: &str = "\
0 0 1 11 1111 01 1
1 0 000101 001011 001111 000111 0001111
1 1 01 10 1110 1 01
2 0 00000111 000111 001011 000100 0001110
2 1 000100 00111 01111 000110 0001101
2 2 001 011 1101 001 001
3 0 000000111 0000111 001000 000011 000000111
3 1 00000110 001010 01100 0000011 0001100
3 2 0000101 001001 01110 0000010 0001011
3 3 00011 0101 1100 000101 00001
4 0 0000000111 00000111 0001111 000010 000000110
4 1 000000110 000110 01010 00000011 000000101
4 2 00000101 000101 01011 00000010 0001010
4 3 000011 0100 1011 0000000 000001
5 0 00000000111 00000100 0001011 - 0000000111
5 1 0000000110 0000110 01000 - 0000000110
5 2 000000101 0000101 01001 - 000000100
5 3 0000100 00110 1010 - 0001001
6 0 0000000001111 000000111 0001001 - 00000000111
6 1 00000000110 00000110 001110 - 00000000110
6 2 0000000101 00000101 001101 - 0000000101
6 3 00000100 001000 1001 - 0001000
7 0 0000000001011 00000001111 0001000 - 000000000111
7 1 0000000001110 000000110 001010 - 000000000110
7 2 00000000101 000000101 001001 - 00000000101
7 3 000000100 000100 1000 - 0000000100
8 0 0000000001000 00000001011 00001111 - 0000000000111
8 1 0000000001010 00000001110 0001110 - 000000000101
8 2 0000000001101 00000001101 0001101 - 000000000100
8 3 0000000100 0000100 01101 - 00000000100
9 0 00000000001111 000000001111 00001011 - -
9 1 00000000001110 00000001010 00001110 - -
9 2 0000000001001 00000001001 0001010 - -
9 3 00000000100 000000100 001100 - -
10 0 00000000001011 000000001011 000001111 - -
10 1 00000000001010 000000001110 00001010 - -
10 2 00000000001101 000000001101 00001101 - -
10 3 0000000001100 00000001100 0001100 - -
11 0 000000000001111 000000001000 000001011 - -
11 1 000000000001110 000000001010 000001110 - -
11 2 00000000001001 000000001001 00001001 - -
11 3 00000000001100 00000001000 00001100 - -
12 0 000000000001011 0000000001111 000001000 - -
12 1 000000000001010 0000000001110 000001010 - -
12 2 000000000001101 0000000001101 000001101 - -
12 3 00000000001000 000000001100 00001000 - -
13 0 0000000000001111 0000000001011 0000001101 - -
13 1 000000000000001 0000000001010 000000111 - -
13 2 000000000001001 0000000001001 000001001 - -
13 3 000000000001100 0000000001100 000001100 - -
14 0 0000000000001011 0000000000111 0000001001 - -
14 1 0000000000001110 00000000001011 0000001100 - -
14 2 0000000000001101 0000000000110 0000001011 - -
14 3 000000000001000 0000000001000 0000001010 - -
15 0 0000000000000111 00000000001001 0000000101 - -
15 1 0000000000001010 00000000001000 0000001000 - -
15 2 0000000000001001 00000000001010 0000000111 - -
15 3 0000000000001100 0000000000001 0000000110 - -
16 0 0000000000000100 00000000000111 0000000001 - -
16 1 0000000000000110 00000000000110 0000000100 - -
16 2 0000000000000101 00000000000101 0000000011 - -
16 3 0000000000001000 00000000000100 0000000010 - -";

const TOTAL_ZEROS: &[&str] = &[
    "1 011 010 0011 0010 00011 00010 000011 000010 0000011 0000010 00000011 00000010 000000011 000000010 000000001",
    "111 110 101 100 011 0101 0100 0011 0010 00011 00010 000011 000010 000001 000000",
    "0101 111 110 101 0100 0011 100 011 0010 00011 00010 000001 00001 000000",
    "00011 111 0101 0100 110 101 100 0011 011 0010 00010 00001 00000",
    "0101 0100 0011 111 110 101 100 011 0010 00001 0001 00000",
    "000001 00001 111 110 101 100 011 010 0001 001 000000",
    "000001 00001 101 100 011 11 010 0001 001 000000",
    "000001 0001 00001 011 11 10 010 001 000000",
    "000001 000000 0001 11 10 001 01 00001",
    "00001 00000 001 11 10 01 0001",
    "0000 0001 001 010 1 011",
    "0000 0001 01 1 001",
    "000 001 1 01",
    "00 01 1",
    "0 1",
];

const CHROMA_ZEROS_4: &[&str] = &["1 01 001 000", "1 01 00", "1 0"];
const CHROMA_ZEROS_8: &[&str] = &[
    "1 010 011 0010 0011 0001 00001 00000",
    "000 01 001 100 101 110 111",
    "000 001 01 10 110 111",
    "110 00 01 10 111",
    "00 01 10 11",
    "00 01 1",
    "0 1",
];
const RUN_BEFORE: &[&str] = &[
    "1 0", "1 01 00", "11 10 01 00", "11 10 01 001 000", "11 10 011 010 001 000",
    "11 000 001 011 010 101 100",
    "111 110 101 100 011 010 001 0001 00001 000001 0000001 00000001 000000001 0000000001 00000000001",
];

#[derive(Default)]
struct Node {
    next: [Option<usize>; 2],
    value: Option<u8>,
}

struct Vlc(Vec<Node>);

impl Vlc {
    fn new() -> Self {
        Self(vec![Node::default()])
    }

    fn insert(&mut self, code: &str, value: u8) {
        let mut index = 0;
        for bit in code.bytes() {
            let branch = usize::from(bit - b'0');
            index = match self.0[index].next[branch] {
                Some(next) => next,
                None => {
                    let next = self.0.len();
                    self.0.push(Node::default());
                    self.0[index].next[branch] = Some(next);
                    next
                }
            };
        }
        assert!(self.0[index].value.replace(value).is_none());
    }

    fn read(&self, reader: &mut BitReader<'_>) -> io::Result<u8> {
        let mut index = 0;
        loop {
            if let Some(value) = self.0[index].value {
                return Ok(value);
            }
            index = self.0[index].next[usize::from(reader.read_bit()?)]
                .ok_or_else(|| invalid("invalid CAVLC codeword"))?;
        }
    }
}

struct Tables {
    tokens: Vec<Vlc>,
    zeros: Vec<Vlc>,
    chroma4: Vec<Vlc>,
    chroma8: Vec<Vlc>,
    runs: Vec<Vlc>,
}

fn tables() -> &'static Tables {
    static TABLES: OnceLock<Tables> = OnceLock::new();
    TABLES.get_or_init(|| {
        let mut tokens: Vec<_> = (0..5).map(|_| Vlc::new()).collect();
        for row in COEFF_TOKEN.lines() {
            let mut fields = row.split_whitespace();
            let total: u8 = fields.next().unwrap().parse().unwrap();
            let trailing: u8 = fields.next().unwrap().parse().unwrap();
            for (table, code) in tokens.iter_mut().zip(fields) {
                if code != "-" {
                    table.insert(code, total * 4 + trailing);
                }
            }
        }
        let make = |rows: &[&str]| {
            rows.iter()
                .map(|row| {
                    let mut table = Vlc::new();
                    for (value, code) in row.split_whitespace().enumerate() {
                        table.insert(code, value as u8);
                    }
                    table
                })
                .collect()
        };
        Tables {
            tokens,
            zeros: make(TOTAL_ZEROS),
            chroma4: make(CHROMA_ZEROS_4),
            chroma8: make(CHROMA_ZEROS_8),
            runs: make(RUN_BEFORE),
        }
    })
}

fn invalid(message: &str) -> io::Error {
    io::Error::new(ErrorKind::InvalidData, message)
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct CavlcResidualBlock {
    pub coefficients: Vec<i32>,
    pub total_coeff: u8,
}

pub fn decode_cavlc_residual_block(
    reader: &mut BitReader<'_>,
    nc: i32,
    max_coeff: usize,
) -> io::Result<CavlcResidualBlock> {
    if !matches!((nc, max_coeff), (-1, 4) | (-2, 8) | (0..=16, 15 | 16)) {
        return Err(invalid("invalid CAVLC block context or coefficient count"));
    }
    let mut trial = reader.clone();
    let block = decode_block(&mut trial, nc, max_coeff)?;
    *reader = trial;
    Ok(block)
}

fn decode_block(
    reader: &mut BitReader<'_>,
    nc: i32,
    max_coeff: usize,
) -> io::Result<CavlcResidualBlock> {
    let tables = tables();
    let token = if nc >= 8 {
        let bits = reader.read_bits(6)? as u8;
        if bits == 3 {
            0
        } else {
            (bits / 4 + 1) * 4 + bits % 4
        }
    } else {
        let column = match nc {
            -2 => 4,
            -1 => 3,
            0..=1 => 0,
            2..=3 => 1,
            _ => 2,
        };
        tables.tokens[column].read(reader)?
    };
    let total = usize::from(token / 4);
    let trailing = usize::from(token % 4);
    if total > max_coeff || trailing > total {
        return Err(invalid("CAVLC coeff_token exceeds block capacity"));
    }
    let mut coefficients = vec![0; max_coeff];
    if total == 0 {
        return Ok(CavlcResidualBlock {
            coefficients,
            total_coeff: 0,
        });
    }
    let mut levels = Vec::with_capacity(total);
    for _ in 0..trailing {
        levels.push(if reader.read_bit()? { -1i64 } else { 1 });
    }
    let mut suffix_length = u8::from(total > 10 && trailing < 3);
    for index in trailing..total {
        let mut prefix = 0u8;
        while !reader.read_bit()? {
            prefix += 1;
            if prefix > 31 {
                return Err(invalid("CAVLC level prefix exceeds supported range"));
            }
        }
        let suffix_size = if prefix == 14 && suffix_length == 0 {
            4
        } else if prefix >= 15 {
            prefix - 3
        } else {
            suffix_length
        };
        let mut code = (i64::from(prefix.min(15)) << suffix_length)
            + i64::from(reader.read_bits(suffix_size)?);
        if prefix >= 15 && suffix_length == 0 {
            code += 15;
        }
        if prefix >= 16 {
            code += (1i64 << (prefix - 3)) - 4096;
        }
        if index == trailing && trailing < 3 {
            code += 2;
        }
        let level = if code & 1 == 0 {
            (code + 2) / 2
        } else {
            -(code + 1) / 2
        };
        if i32::try_from(level).is_err() {
            return Err(invalid("CAVLC coefficient exceeds int32"));
        }
        levels.push(level);
        suffix_length = suffix_length.max(1);
        if suffix_length < 6 && level.abs() > (3 << (suffix_length - 1)) {
            suffix_length += 1;
        }
    }
    let mut zeros_left = if total < max_coeff {
        let zeros = match max_coeff {
            4 => &tables.chroma4,
            8 => &tables.chroma8,
            _ => &tables.zeros,
        };
        usize::from(zeros[total - 1].read(reader)?)
    } else {
        0
    };
    if zeros_left > max_coeff - total {
        return Err(invalid("CAVLC total_zeros exceeds block capacity"));
    }
    let mut runs = vec![0; total];
    for run in &mut runs[..total - 1] {
        if zeros_left != 0 {
            *run = usize::from(tables.runs[zeros_left.min(7) - 1].read(reader)?);
            if *run > zeros_left {
                return Err(invalid("CAVLC run_before exceeds zeros left"));
            }
            zeros_left -= *run;
        }
    }
    runs[total - 1] = zeros_left;
    let mut position = 0;
    for index in (0..total).rev() {
        position += runs[index];
        coefficients[position] = levels[index] as i32;
        position += 1;
    }
    Ok(CavlcResidualBlock {
        coefficients,
        total_coeff: total as u8,
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    fn bits(value: &str) -> Vec<u8> {
        let mut data = vec![0; value.len().div_ceil(8)];
        for (index, bit) in value.bytes().enumerate() {
            data[index / 8] |= (bit - b'0') << (7 - index % 8);
        }
        data
    }

    #[test]
    fn cavlc_residual_vectors() {
        for (code, nc, count, expected) in [
            ("1", 0, 16, vec![0; 16]),
            ("0101", 0, 16, {
                let mut block = vec![0; 16];
                block[0] = 1;
                block
            }),
            ("011011", 0, 16, {
                let mut block = vec![0; 16];
                block[1] = -1;
                block
            }),
            ("00101111", 0, 16, {
                let mut block = vec![0; 16];
                block[0] = -1;
                block[1] = 1;
                block
            }),
            ("101", -1, 4, vec![1, 0, 0, 0]),
            ("000011", 8, 16, vec![0; 16]),
        ] {
            let data = bits(code);
            let mut reader = BitReader::new(&data);
            let block = decode_cavlc_residual_block(&mut reader, nc, count)
                .unwrap_or_else(|error| panic!("{code}: {error}"));
            assert_eq!(block.coefficients, expected, "{code}");
            assert_eq!(reader.bit_offset(), code.len(), "{code}");
        }
        let mut reader = BitReader::new(&[0]);
        assert!(decode_cavlc_residual_block(&mut reader, 0, 16).is_err());
        assert_eq!(reader.bit_offset(), 0);
    }
}
