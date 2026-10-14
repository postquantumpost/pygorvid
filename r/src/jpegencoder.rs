use std::io;

use crate::imageencoder::RgbImage;

const ZIGZAG: [usize; 64] = [
    0, 1, 8, 16, 9, 2, 3, 10, 17, 24, 32, 25, 18, 11, 4, 5, 12, 19, 26, 33, 40, 48, 41, 34, 27, 20,
    13, 6, 7, 14, 21, 28, 35, 42, 49, 56, 57, 50, 43, 36, 29, 22, 15, 23, 30, 37, 44, 51, 58, 59,
    52, 45, 38, 31, 39, 46, 53, 60, 61, 54, 47, 55, 62, 63,
];

const LUMA_QUANTIZATION: [u8; 64] = [
    16, 11, 10, 16, 24, 40, 51, 61, 12, 12, 14, 19, 26, 58, 60, 55, 14, 13, 16, 24, 40, 57, 69, 56,
    14, 17, 22, 29, 51, 87, 80, 62, 18, 22, 37, 56, 68, 109, 103, 77, 24, 35, 55, 64, 81, 104, 113,
    92, 49, 64, 78, 87, 103, 121, 120, 101, 72, 92, 95, 98, 112, 100, 103, 99,
];

const CHROMA_QUANTIZATION: [u8; 64] = [
    17, 18, 24, 47, 99, 99, 99, 99, 18, 21, 26, 66, 99, 99, 99, 99, 24, 26, 56, 99, 99, 99, 99, 99,
    47, 66, 99, 99, 99, 99, 99, 99, 99, 99, 99, 99, 99, 99, 99, 99, 99, 99, 99, 99, 99, 99, 99, 99,
    99, 99, 99, 99, 99, 99, 99, 99, 99, 99, 99, 99, 99, 99, 99, 99,
];

pub(crate) fn encode(rgb: &RgbImage) -> io::Result<Vec<u8>> {
    let width = u16::try_from(rgb.width).map_err(|_| invalid("JPEG width exceeds 65535"))?;
    let height = u16::try_from(rgb.height).map_err(|_| invalid("JPEG height exceeds 65535"))?;
    if width == 0 || height == 0 {
        return Err(invalid("JPEG dimensions must be positive"));
    }
    let quantization = [LUMA_QUANTIZATION, CHROMA_QUANTIZATION]
        .map(|table| table.map(|value| ((u16::from(value) * 50 + 50) / 100).max(1) as u8));
    let mut output = vec![0xff, 0xd8];
    marker(&mut output, 0xe0, b"JFIF\0\x01\x01\0\0\x01\0\x01\0\0");
    let mut tables = Vec::new();
    for (table_index, table) in quantization.iter().enumerate() {
        tables.push(table_index as u8);
        tables.extend(ZIGZAG.iter().map(|index| table[*index]));
    }
    marker(&mut output, 0xdb, &tables);
    let mut header = vec![8];
    header.extend_from_slice(&height.to_be_bytes());
    header.extend_from_slice(&width.to_be_bytes());
    header.extend_from_slice(&[3, 1, 0x11, 0, 2, 0x11, 1, 3, 0x11, 1]);
    marker(&mut output, 0xc0, &header);
    let mut huffman = Vec::new();
    for table_index in 0..2 {
        huffman.push(table_index);
        let mut counts = [0; 16];
        counts[3] = 12;
        huffman.extend_from_slice(&counts);
        huffman.extend(0..12);
        huffman.push(0x10 | table_index);
        counts = [0; 16];
        counts[7] = 162;
        huffman.extend_from_slice(&counts);
        huffman.extend_from_slice(&[0, 0xf0]);
        for run in 0..16 {
            for size in 1..11 {
                huffman.push((run << 4) | size);
            }
        }
    }
    marker(&mut output, 0xc4, &huffman);
    marker(&mut output, 0xda, &[3, 1, 0, 2, 0x11, 3, 0x11, 0, 63, 0]);
    let pixels = rgb.width * rgb.height;
    let mut planes = [vec![0; pixels], vec![0; pixels], vec![0; pixels]];
    for (index, channels) in rgb.data.chunks_exact(3).enumerate() {
        let red = i32::from(channels[0]);
        let green = i32::from(channels[1]);
        let blue = i32::from(channels[2]);
        planes[0][index] =
            ((19595 * red + 38470 * green + 7471 * blue + 32768) >> 16).clamp(0, 255) as u8;
        planes[1][index] = ((-11059 * red - 21709 * green + 32768 * blue + (128 << 16) + 32768)
            >> 16)
            .clamp(0, 255) as u8;
        planes[2][index] = ((32768 * red - 27439 * green - 5329 * blue + (128 << 16) + 32768) >> 16)
            .clamp(0, 255) as u8;
    }
    let mut basis = [[0.0; 8]; 8];
    for (frequency, row) in basis.iter_mut().enumerate() {
        let normalization = if frequency == 0 {
            0.5 / 2.0_f64.sqrt()
        } else {
            0.5
        };
        for (position, value) in row.iter_mut().enumerate() {
            *value = normalization
                * (((2 * position + 1) * frequency) as f64 * std::f64::consts::PI / 16.0).cos();
        }
    }
    let mut writer = BitWriter::default();
    let mut previous_dc = [0; 3];
    for block_row in 0..rgb.height.div_ceil(8) {
        for block_column in 0..rgb.width.div_ceil(8) {
            for component in 0..3 {
                let mut samples = [0; 64];
                for row in 0..8 {
                    let source_row = (block_row * 8 + row).min(rgb.height - 1);
                    for column in 0..8 {
                        let source_column = (block_column * 8 + column).min(rgb.width - 1);
                        samples[row * 8 + column] =
                            planes[component][source_row * rgb.width + source_column];
                    }
                }
                let table = &quantization[usize::from(component != 0)];
                let coefficients = transform(&samples, &basis, table);
                write_block(&mut writer, &coefficients, previous_dc[component])?;
                previous_dc[component] = coefficients[0];
            }
        }
    }
    output.extend_from_slice(&writer.finish());
    output.extend_from_slice(&[0xff, 0xd9]);
    Ok(output)
}

fn invalid(message: &str) -> io::Error {
    io::Error::new(io::ErrorKind::InvalidInput, message)
}

fn marker(output: &mut Vec<u8>, code: u8, payload: &[u8]) {
    output.extend_from_slice(&[0xff, code]);
    output.extend_from_slice(&((payload.len() + 2) as u16).to_be_bytes());
    output.extend_from_slice(payload);
}

fn transform(samples: &[u8; 64], basis: &[[f64; 8]; 8], quantization: &[u8; 64]) -> [i32; 64] {
    let mut horizontal = [0.0; 64];
    for row in 0..8 {
        for frequency in 0..8 {
            for column in 0..8 {
                horizontal[row * 8 + frequency] +=
                    (f64::from(samples[row * 8 + column]) - 128.0) * basis[frequency][column];
            }
        }
    }
    let mut raster = [0; 64];
    for vertical_frequency in 0..8 {
        for horizontal_frequency in 0..8 {
            let mut coefficient = 0.0;
            for row in 0..8 {
                coefficient +=
                    horizontal[row * 8 + horizontal_frequency] * basis[vertical_frequency][row];
            }
            let index = vertical_frequency * 8 + horizontal_frequency;
            raster[index] = (coefficient / f64::from(quantization[index])).round() as i32;
        }
    }
    ZIGZAG.map(|index| raster[index])
}

fn write_block(writer: &mut BitWriter, coefficients: &[i32; 64], previous: i32) -> io::Result<()> {
    let difference = coefficients[0] - previous;
    let dc_category = category(difference);
    if dc_category > 11 {
        return Err(invalid("JPEG DC difference exceeds baseline category 11"));
    }
    writer.write(u32::from(dc_category), 4);
    amplitude(writer, difference, dc_category);
    let mut zero_run = 0;
    for coefficient in &coefficients[1..] {
        if *coefficient == 0 {
            zero_run += 1;
            continue;
        }
        let category = category(*coefficient);
        if category > 10 {
            return Err(invalid("JPEG AC coefficient exceeds baseline category 10"));
        }
        while zero_run >= 16 {
            writer.write(1, 8);
            zero_run -= 16;
        }
        writer.write(2 + zero_run * 10 + u32::from(category) - 1, 8);
        amplitude(writer, *coefficient, category);
        zero_run = 0;
    }
    if zero_run != 0 {
        writer.write(0, 8);
    }
    Ok(())
}

fn category(value: i32) -> u8 {
    (32 - value.unsigned_abs().leading_zeros()) as u8
}

fn amplitude(writer: &mut BitWriter, value: i32, category: u8) {
    if category != 0 {
        let encoded = if value < 0 {
            value + (1 << category) - 1
        } else {
            value
        };
        writer.write(encoded as u32, category);
    }
}

#[derive(Default)]
struct BitWriter {
    data: Vec<u8>,
    current: u8,
    count: u8,
}

impl BitWriter {
    fn write(&mut self, value: u32, count: u8) {
        for shift in (0..count).rev() {
            self.current = (self.current << 1) | ((value >> shift) & 1) as u8;
            self.count += 1;
            if self.count == 8 {
                self.flush();
            }
        }
    }

    fn flush(&mut self) {
        self.data.push(self.current);
        if self.current == 0xff {
            self.data.push(0);
        }
        self.current = 0;
        self.count = 0;
    }

    fn finish(mut self) -> Vec<u8> {
        if self.count != 0 {
            let padding = 8 - self.count;
            self.write((1 << padding) - 1, padding);
        }
        self.data
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn encodes_odd_sized_jfif_and_stuffs_entropy_bytes() {
        let image = RgbImage {
            width: 3,
            height: 5,
            data: vec![128; 45],
        };
        let payload = encode(&image).unwrap();
        assert_eq!(&payload[..2], &[0xff, 0xd8]);
        assert_eq!(&payload[payload.len() - 2..], &[0xff, 0xd9]);
        assert!(payload.windows(5).any(|window| window == b"JFIF\0"));
        let mut writer = BitWriter::default();
        writer.write(255, 8);
        assert_eq!(writer.finish(), [255, 0]);
    }
}
