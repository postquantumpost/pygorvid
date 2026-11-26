use std::io;

pub(crate) const INVERSE_SCALE_4X4_FACTORS: [[i64; 3]; 6] = [
    [10, 13, 16],
    [11, 14, 18],
    [13, 16, 20],
    [14, 18, 23],
    [16, 20, 25],
    [18, 23, 29],
];

const INVERSE_SCALE_8X8_FACTORS: [[i64; 6]; 6] = [
    [20, 18, 32, 19, 25, 24],
    [22, 19, 35, 21, 28, 26],
    [26, 23, 42, 24, 33, 31],
    [28, 25, 45, 26, 35, 33],
    [32, 28, 51, 30, 40, 38],
    [36, 32, 58, 34, 46, 43],
];

const INVERSE_SCALE_8X8_CLASSES: [usize; 64] = [
    0, 3, 4, 3, 0, 3, 4, 3, 3, 1, 5, 1, 3, 1, 5, 1, 4, 5, 2, 5, 4, 5, 2, 5, 3, 1, 5, 1, 3, 1, 5, 1,
    0, 3, 4, 3, 0, 3, 4, 3, 3, 1, 5, 1, 3, 1, 5, 1, 4, 5, 2, 5, 4, 5, 2, 5, 3, 1, 5, 1, 3, 1, 5, 1,
];

fn invalid(message: &'static str) -> io::Error {
    io::Error::new(io::ErrorKind::InvalidInput, message)
}

pub fn inverse_scale_luma4x4(
    levels: &[i32; 16],
    scaling_list: &[u8; 16],
    qpy: i32,
) -> io::Result<[i64; 16]> {
    if !(0..=51).contains(&qpy) {
        return Err(invalid("inverse scaling QPY is outside [0,51]"));
    }
    if scaling_list.contains(&0) {
        return Err(invalid("inverse scaling list contains zero"));
    }
    let mut scaled = [0_i64; 16];
    for index in 0..16 {
        let factor_class = (index / 4) % 2 + (index % 4) % 2;
        scaled[index] = inverse_scale_4x4_coefficient(
            i64::from(levels[index]),
            i64::from(scaling_list[index]),
            qpy,
            factor_class,
        );
    }
    Ok(scaled)
}

pub fn inverse_scale_chroma4x4(
    levels: &[i32; 16],
    scaling_list: &[u8; 16],
    qpc: i32,
) -> io::Result<[i64; 16]> {
    if !(0..=39).contains(&qpc) {
        return Err(invalid("inverse scaling QPC is outside [0,39]"));
    }
    if levels
        .iter()
        .any(|level| !(-(1_i32 << 15)..=(1_i32 << 15) - 1).contains(level))
    {
        return Err(invalid(
            "chroma block levels must be in the signed 16-bit range",
        ));
    }
    if scaling_list.contains(&0) {
        return Err(invalid("inverse scaling list contains zero"));
    }
    let mut scaled = [0_i64; 16];
    scaled[0] = i64::from(levels[0]);
    for index in 1..16 {
        let factor_class = (index / 4) % 2 + (index % 4) % 2;
        scaled[index] = inverse_scale_4x4_coefficient(
            i64::from(levels[index]),
            i64::from(scaling_list[index]),
            qpc,
            factor_class,
        );
        if !(-(1_i64 << 15)..=(1_i64 << 15) - 1).contains(&scaled[index]) {
            return Err(invalid(
                "scaled chroma block value is outside the signed 16-bit range",
            ));
        }
    }
    Ok(scaled)
}

pub fn reconstruct_chroma4x4_residual(
    dc_c: i64,
    ac_scan_levels: &[i32; 15],
    scaling_list: &[u8; 16],
    qpc: i32,
) -> io::Result<[i64; 16]> {
    if !(0..=39).contains(&qpc) {
        return Err(invalid("inverse scaling QPC is outside [0,39]"));
    }
    if !(-(1_i64 << 15)..=(1_i64 << 15) - 1).contains(&dc_c) {
        return Err(invalid(
            "chroma block DC must be in the signed 16-bit range",
        ));
    }
    let dc_level = i32::try_from(dc_c)
        .map_err(|_| invalid("chroma block DC must be in the signed 16-bit range"))?;
    let levels = crate::place_chroma4x4_scan_levels(dc_level, ac_scan_levels);
    let scaled = inverse_scale_chroma4x4(&levels, scaling_list, qpc)?;
    Ok(inverse_transform_luma4x4(&scaled))
}

pub fn inverse_transform_luma4x4(coefficients: &[i64; 16]) -> [i64; 16] {
    let mut horizontal = [0_i64; 16];
    for row in 0..4 {
        let offset = row * 4;
        horizontal[offset..offset + 4].copy_from_slice(&inverse_transform_4x4_line([
            coefficients[offset],
            coefficients[offset + 1],
            coefficients[offset + 2],
            coefficients[offset + 3],
        ]));
    }
    let mut residual = [0_i64; 16];
    for column in 0..4 {
        let transformed = inverse_transform_4x4_line([
            horizontal[column],
            horizontal[4 + column],
            horizontal[8 + column],
            horizontal[12 + column],
        ]);
        for (row, value) in transformed.into_iter().enumerate() {
            residual[row * 4 + column] = (value + 32) >> 6;
        }
    }
    residual
}

pub fn reconstruct_luma4x4_residual(
    levels: &[i32; 16],
    scaling_list: &[u8; 16],
    qpy: i32,
) -> io::Result<[i64; 16]> {
    let coefficients = inverse_scale_luma4x4(levels, scaling_list, qpy)?;
    Ok(inverse_transform_luma4x4(&coefficients))
}

pub fn reconstruct_luma4x4_ac_residual(
    ac_scan_levels: &[i32; 15],
    dc_coefficient: i64,
    scaling_list: &[u8; 16],
    qpy: i32,
) -> io::Result<[i64; 16]> {
    let mut scan_levels = [0_i32; 16];
    scan_levels[1..].copy_from_slice(ac_scan_levels);
    let raster_levels = crate::place_luma4x4_scan_levels(&scan_levels);
    let mut coefficients = inverse_scale_luma4x4(&raster_levels, scaling_list, qpy)?;
    coefficients[0] = dc_coefficient;
    Ok(inverse_transform_luma4x4(&coefficients))
}

pub fn hadamard4x4(levels: &[i32; 16]) -> [i64; 16] {
    let mut horizontal = [0_i64; 16];
    for row in 0..4 {
        horizontal[row * 4..row * 4 + 4].copy_from_slice(&hadamard4([
            i64::from(levels[row * 4]),
            i64::from(levels[row * 4 + 1]),
            i64::from(levels[row * 4 + 2]),
            i64::from(levels[row * 4 + 3]),
        ]));
    }
    let mut transformed = [0_i64; 16];
    for column in 0..4 {
        let line = hadamard4([
            horizontal[column],
            horizontal[4 + column],
            horizontal[8 + column],
            horizontal[12 + column],
        ]);
        for row in 0..4 {
            transformed[row * 4 + column] = line[row];
        }
    }
    transformed
}

pub fn inverse_scale_luma8x8(
    levels: &[i32; 64],
    scaling_list: &[u8; 64],
    qpy: i32,
) -> io::Result<[i64; 64]> {
    if !(0..=51).contains(&qpy) {
        return Err(invalid("inverse scaling QPY is outside [0,51]"));
    }
    if scaling_list.contains(&0) {
        return Err(invalid("inverse scaling list contains zero"));
    }
    let mut scaled = [0_i64; 64];
    for index in 0..64 {
        let value = i64::from(levels[index])
            * INVERSE_SCALE_8X8_FACTORS[(qpy % 6) as usize][INVERSE_SCALE_8X8_CLASSES[index]]
            * i64::from(scaling_list[index]);
        scaled[index] = if qpy >= 36 {
            value << (qpy / 6 - 6)
        } else {
            let shift = 6 - qpy / 6;
            (value + (1_i64 << (shift - 1))) >> shift
        };
    }
    Ok(scaled)
}

pub fn inverse_transform_luma8x8(coefficients: &[i64; 64]) -> [i64; 64] {
    let mut horizontal = [0_i64; 64];
    for row in 0..8 {
        let offset = row * 8;
        horizontal[offset..offset + 8].copy_from_slice(&inverse_transform_8x8_line([
            coefficients[offset],
            coefficients[offset + 1],
            coefficients[offset + 2],
            coefficients[offset + 3],
            coefficients[offset + 4],
            coefficients[offset + 5],
            coefficients[offset + 6],
            coefficients[offset + 7],
        ]));
    }
    let mut residual = [0_i64; 64];
    for column in 0..8 {
        let transformed = inverse_transform_8x8_line([
            horizontal[column],
            horizontal[8 + column],
            horizontal[16 + column],
            horizontal[24 + column],
            horizontal[32 + column],
            horizontal[40 + column],
            horizontal[48 + column],
            horizontal[56 + column],
        ]);
        for (row, value) in transformed.into_iter().enumerate() {
            residual[row * 8 + column] = (value + 32) >> 6;
        }
    }
    residual
}

pub fn reconstruct_luma8x8_residual(
    levels: &[i32; 64],
    scaling_list: &[u8; 64],
    qpy: i32,
) -> io::Result<[i64; 64]> {
    let coefficients = inverse_scale_luma8x8(levels, scaling_list, qpy)?;
    Ok(inverse_transform_luma8x8(&coefficients))
}

pub fn block_position_2x2(block_index: usize) -> (usize, usize) {
    (block_index % 2, block_index / 2)
}

pub fn place_residual_block(
    destination: &mut [i64],
    stride: usize,
    origin_x: usize,
    origin_y: usize,
    residual: &[i64],
    block_size: usize,
) {
    for row in 0..block_size {
        let destination_start = (origin_y + row) * stride + origin_x;
        let residual_start = row * block_size;
        destination[destination_start..destination_start + block_size]
            .copy_from_slice(&residual[residual_start..residual_start + block_size]);
    }
}

pub fn reconstruct_chroma_component_residual(
    dc_samples: &[i64; 4],
    ac_scan_blocks: &[[i32; 15]; 4],
    scaling_list: &[u8; 16],
    qpc: i32,
) -> io::Result<[i64; 64]> {
    let mut blocks = [[0_i64; 16]; 4];
    for block_index in 0..4 {
        blocks[block_index] = reconstruct_chroma4x4_residual(
            dc_samples[block_index],
            &ac_scan_blocks[block_index],
            scaling_list,
            qpc,
        )?;
    }
    let mut residual = [0_i64; 64];
    for (block_index, block) in blocks.iter().enumerate() {
        let (block_x, block_y) = block_position_2x2(block_index);
        for row in 0..4 {
            let destination = (block_y * 4 + row) * 8 + block_x * 4;
            residual[destination..destination + 4].copy_from_slice(&block[row * 4..row * 4 + 4]);
        }
    }
    Ok(residual)
}

fn inverse_scale_4x4_coefficient(level: i64, weight: i64, qp: i32, factor_class: usize) -> i64 {
    let value = level * INVERSE_SCALE_4X4_FACTORS[(qp % 6) as usize][factor_class] * weight;
    if qp >= 24 {
        value << (qp / 6 - 4)
    } else {
        let shift = 4 - qp / 6;
        (value + (1_i64 << (shift - 1))) >> shift
    }
}

fn inverse_transform_4x4_line(coefficients: [i64; 4]) -> [i64; 4] {
    let even_sum = coefficients[0] + coefficients[2];
    let even_difference = coefficients[0] - coefficients[2];
    let odd_difference = (coefficients[1] >> 1) - coefficients[3];
    let odd_sum = coefficients[1] + (coefficients[3] >> 1);
    [
        even_sum + odd_sum,
        even_difference + odd_difference,
        even_difference - odd_difference,
        even_sum - odd_sum,
    ]
}

fn hadamard4(values: [i64; 4]) -> [i64; 4] {
    [
        values[0] + values[1] + values[2] + values[3],
        values[0] + values[1] - values[2] - values[3],
        values[0] - values[1] - values[2] + values[3],
        values[0] - values[1] + values[2] - values[3],
    ]
}

fn inverse_transform_8x8_line(coefficients: [i64; 8]) -> [i64; 8] {
    let a0 = coefficients[0] + coefficients[4];
    let a2 = coefficients[0] - coefficients[4];
    let a4 = (coefficients[2] >> 1) - coefficients[6];
    let a6 = coefficients[2] + (coefficients[6] >> 1);
    let b0 = a0 + a6;
    let b2 = a2 + a4;
    let b4 = a2 - a4;
    let b6 = a0 - a6;
    let a1 = -coefficients[3] + coefficients[5] - coefficients[7] - (coefficients[7] >> 1);
    let a3 = coefficients[1] + coefficients[7] - coefficients[3] - (coefficients[3] >> 1);
    let a5 = -coefficients[1] + coefficients[7] + coefficients[5] + (coefficients[5] >> 1);
    let a7 = coefficients[3] + coefficients[5] + coefficients[1] + (coefficients[1] >> 1);
    let b1 = a1 + (a7 >> 2);
    let b3 = a3 + (a5 >> 2);
    let b5 = (a3 >> 2) - a5;
    let b7 = a7 - (a1 >> 2);
    [
        b0 + b7,
        b2 + b5,
        b4 + b3,
        b6 + b1,
        b6 - b1,
        b4 - b3,
        b2 - b5,
        b0 - b7,
    ]
}
