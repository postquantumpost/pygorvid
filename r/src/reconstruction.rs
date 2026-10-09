use std::io::{self, ErrorKind};

const INVERSE_SCALE_4X4_FACTORS: [[i64; 3]; 6] = [
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

/// Applies H.264 4x4 luma inverse scaling to raster-order coefficient levels.
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
        let row = index / 4;
        let column = index % 4;
        let factor_class = row % 2 + column % 2;
        let value = i64::from(levels[index])
            * INVERSE_SCALE_4X4_FACTORS[(qpy % 6) as usize][factor_class]
            * i64::from(scaling_list[index]);
        scaled[index] = if qpy >= 24 {
            value << (qpy / 6 - 4)
        } else {
            let shift = 4 - qpy / 6;
            let rounding = 1_i64 << (shift - 1);
            (value + rounding) >> shift
        };
    }
    Ok(scaled)
}

/// Applies H.264 8x8 luma inverse scaling to raster-order coefficient levels.
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
        let factor_class = INVERSE_SCALE_8X8_CLASSES[index];
        let value = i64::from(levels[index])
            * INVERSE_SCALE_8X8_FACTORS[(qpy % 6) as usize][factor_class]
            * i64::from(scaling_list[index]);
        scaled[index] = if qpy >= 24 {
            value << (qpy / 6 - 4)
        } else {
            let shift = 4 - qpy / 6;
            let rounding = 1_i64 << (shift - 1);
            (value + rounding) >> shift
        };
    }
    Ok(scaled)
}

/// Transforms dequantized raster-order coefficients into luma residual samples.
pub fn inverse_transform_luma4x4(coefficients: &[i64; 16]) -> [i64; 16] {
    let mut horizontal = [0_i64; 16];
    for row in 0..4 {
        let offset = row * 4;
        let transformed = inverse_transform_4x4_line([
            coefficients[offset],
            coefficients[offset + 1],
            coefficients[offset + 2],
            coefficients[offset + 3],
        ]);
        horizontal[offset..offset + 4].copy_from_slice(&transformed);
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

/// Transforms 8x8 dequantized raster-order coefficients into residual samples.
pub fn inverse_transform_luma8x8(coefficients: &[i64; 64]) -> [i64; 64] {
    let mut horizontal = [0_i64; 64];
    for row in 0..8 {
        let offset = row * 8;
        let transformed = inverse_transform_8x8_line([
            coefficients[offset],
            coefficients[offset + 1],
            coefficients[offset + 2],
            coefficients[offset + 3],
            coefficients[offset + 4],
            coefficients[offset + 5],
            coefficients[offset + 6],
            coefficients[offset + 7],
        ]);
        horizontal[offset..offset + 8].copy_from_slice(&transformed);
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

/// Repeats eight filtered top reference samples across an 8x8 luma block.
pub fn predict_luma_intra8x8_vertical(top: &[u8; 8]) -> [u8; 64] {
    let mut prediction = [0_u8; 64];
    for row in 0..8 {
        prediction[row * 8..row * 8 + 8].copy_from_slice(top);
    }
    prediction
}

/// Repeats each filtered left reference sample across one row of an 8x8 luma block.
pub fn predict_luma_intra8x8_horizontal(left: &[u8; 8]) -> [u8; 64] {
    let mut prediction = [0_u8; 64];
    for (row, sample) in left.iter().enumerate() {
        prediction[row * 8..row * 8 + 8].fill(*sample);
    }
    prediction
}

/// Predicts an 8x8 luma block using whichever filtered edges are available.
pub fn predict_luma_intra8x8_dc(top: Option<&[u8; 8]>, left: Option<&[u8; 8]>) -> [u8; 64] {
    let dc_value = match (top, left) {
        (Some(top), Some(left)) => {
            let sum = top
                .iter()
                .chain(left.iter())
                .map(|sample| u32::from(*sample))
                .sum::<u32>();
            (sum + 8) >> 4
        }
        (Some(top), None) => {
            let sum = top.iter().map(|sample| u32::from(*sample)).sum::<u32>();
            (sum + 4) >> 3
        }
        (None, Some(left)) => {
            let sum = left.iter().map(|sample| u32::from(*sample)).sum::<u32>();
            (sum + 4) >> 3
        }
        (None, None) => 128,
    } as u8;
    [dc_value; 64]
}

/// Interpolates an 8x8 luma block from 16 filtered top references.
pub fn predict_luma_intra8x8_diagonal_down_left(top: &[u8; 16]) -> [u8; 64] {
    let mut prediction = [0_u8; 64];
    for row in 0..8 {
        for column in 0..8 {
            let index = row + column;
            let last_index = (index + 2).min(15);
            let value = (u16::from(top[index])
                + 2 * u16::from(top[index + 1])
                + u16::from(top[last_index])
                + 2)
                >> 2;
            prediction[row * 8 + column] = value as u8;
        }
    }
    prediction
}

/// Interpolates an 8x8 luma block from filtered top, left, and top-left references.
pub fn predict_luma_intra8x8_diagonal_down_right(
    top: &[u8; 16],
    left: &[u8; 8],
    top_left: u8,
) -> [u8; 64] {
    let reference_at = |position: i32| -> u16 {
        match position {
            -1 => u16::from(top_left),
            value if value < -1 => u16::from(left[(-value - 2) as usize]),
            value => u16::from(top[value as usize]),
        }
    };

    let mut prediction = [0_u8; 64];
    for row in 0..8 {
        for column in 0..8 {
            let position = column as i32 - row as i32;
            let value = (reference_at(position - 1)
                + 2 * reference_at(position)
                + reference_at(position + 1)
                + 2)
                >> 2;
            prediction[row * 8 + column] = value as u8;
        }
    }
    prediction
}

/// Interpolates an 8x8 luma block across vertical-right reference phases.
pub fn predict_luma_intra8x8_vertical_right(
    top: &[u8; 16],
    left: &[u8; 8],
    top_left: u8,
) -> [u8; 64] {
    let top_reference_at = |position: i32| -> u16 {
        if position == -1 {
            u16::from(top_left)
        } else {
            u16::from(top[position as usize])
        }
    };

    let mut prediction = [0_u8; 64];
    for row in 0..8 {
        for column in 0..8 {
            let phase = 2 * column as i32 - row as i32;
            let value = if phase >= 0 && phase % 2 == 0 {
                let center = phase / 2;
                (top_reference_at(center - 1)
                    + 2 * top_reference_at(center)
                    + top_reference_at(center + 1)
                    + 2)
                    >> 2
            } else if phase >= 0 {
                let center = (phase - 1) / 2;
                (top_reference_at(center - 1) + top_reference_at(center) + 1) >> 1
            } else if phase == -1 {
                (u16::from(left[0]) + u16::from(top_left) + 1) >> 1
            } else if phase == -2 {
                (u16::from(left[0]) + 2 * u16::from(top_left) + u16::from(top[0]) + 2) >> 2
            } else if (-phase) % 2 == 1 {
                let center = ((-phase - 3) / 2) as usize;
                (u16::from(left[center]) + u16::from(left[center + 1]) + 1) >> 1
            } else {
                let center = ((-phase - 4) / 2) as usize;
                (u16::from(left[center])
                    + 2 * u16::from(left[center + 1])
                    + u16::from(left[center + 2])
                    + 2)
                    >> 2
            };
            prediction[row * 8 + column] = value as u8;
        }
    }
    prediction
}

/// Predicts an 8x8 luma block by transposing vertical-right prediction.
pub fn predict_luma_intra8x8_horizontal_down(
    top: &[u8; 8],
    left: &[u8; 16],
    top_left: u8,
) -> [u8; 64] {
    let vertical_prediction = predict_luma_intra8x8_vertical_right(left, top, top_left);
    let mut prediction = [0_u8; 64];
    for row in 0..8 {
        for column in 0..8 {
            prediction[row * 8 + column] = vertical_prediction[column * 8 + row];
        }
    }
    prediction
}

/// Interpolates an 8x8 luma block across vertical-left top-reference phases.
pub fn predict_luma_intra8x8_vertical_left(top: &[u8; 16]) -> [u8; 64] {
    let mut prediction = [0_u8; 64];
    for row in 0..8 {
        for column in 0..8 {
            let phase = 2 * column + row;
            let value = if phase % 2 == 0 {
                let center = phase / 2;
                (u16::from(top[center]) + u16::from(top[center + 1]) + 1) >> 1
            } else {
                let center = (phase - 1) / 2;
                (u16::from(top[center])
                    + 2 * u16::from(top[center + 1])
                    + u16::from(top[center + 2])
                    + 2)
                    >> 2
            };
            prediction[row * 8 + column] = value as u8;
        }
    }
    prediction
}

/// Predicts an 8x8 luma block by transposing vertical-left prediction.
pub fn predict_luma_intra8x8_horizontal_up(left: &[u8; 16]) -> [u8; 64] {
    let vertical_prediction = predict_luma_intra8x8_vertical_left(left);
    let mut prediction = [0_u8; 64];
    for row in 0..8 {
        for column in 0..8 {
            prediction[row * 8 + column] = vertical_prediction[column * 8 + row];
        }
    }
    prediction
}

/// Repeats four available top reference samples across a 4x4 luma block.
pub fn predict_luma_intra4x4_vertical(top: &[u8; 4]) -> [u8; 16] {
    let mut prediction = [0_u8; 16];
    for row in 0..4 {
        prediction[row * 4..row * 4 + 4].copy_from_slice(top);
    }
    prediction
}

/// Repeats each of four available left reference samples across one block row.
pub fn predict_luma_intra4x4_horizontal(left: &[u8; 4]) -> [u8; 16] {
    let mut prediction = [0_u8; 16];
    for (row, sample) in left.iter().enumerate() {
        prediction[row * 4..row * 4 + 4].fill(*sample);
    }
    prediction
}

/// Predicts a 4x4 luma block using whichever top and left references are available.
pub fn predict_luma_intra4x4_dc(top: Option<&[u8; 4]>, left: Option<&[u8; 4]>) -> [u8; 16] {
    let dc_value = match (top, left) {
        (Some(top), Some(left)) => {
            let sum = top
                .iter()
                .chain(left.iter())
                .map(|sample| u32::from(*sample))
                .sum::<u32>();
            (sum + 4) >> 3
        }
        (Some(top), None) => {
            let sum = top.iter().map(|sample| u32::from(*sample)).sum::<u32>();
            (sum + 2) >> 2
        }
        (None, Some(left)) => {
            let sum = left.iter().map(|sample| u32::from(*sample)).sum::<u32>();
            (sum + 2) >> 2
        }
        (None, None) => 128,
    } as u8;
    [dc_value; 16]
}

/// Interpolates a 4x4 luma block from top and top-right references.
pub fn predict_luma_intra4x4_diagonal_down_left(top: &[u8; 8]) -> [u8; 16] {
    let mut prediction = [0_u8; 16];
    for row in 0..4 {
        for column in 0..4 {
            let index = row + column;
            let last_index = (index + 2).min(7);
            let value = (u16::from(top[index])
                + 2 * u16::from(top[index + 1])
                + u16::from(top[last_index])
                + 2)
                >> 2;
            prediction[row * 4 + column] = value as u8;
        }
    }
    prediction
}

/// Interpolates a 4x4 luma block from top, left, and top-left references.
pub fn predict_luma_intra4x4_diagonal_down_right(
    top: &[u8; 8],
    left: &[u8; 4],
    top_left: u8,
) -> [u8; 16] {
    let reference_at = |position: i32| -> u16 {
        match position {
            -1 => u16::from(top_left),
            value if value < -1 => u16::from(left[(-value - 2) as usize]),
            value => u16::from(top[value as usize]),
        }
    };

    let mut prediction = [0_u8; 16];
    for row in 0..4 {
        for column in 0..4 {
            let position = column as i32 - row as i32;
            let value = (reference_at(position - 1)
                + 2 * reference_at(position)
                + reference_at(position + 1)
                + 2)
                >> 2;
            prediction[row * 4 + column] = value as u8;
        }
    }
    prediction
}

/// Interpolates a 4x4 luma block from vertical-right reference phases.
pub fn predict_luma_intra4x4_vertical_right(
    top: &[u8; 8],
    left: &[u8; 4],
    top_left: u8,
) -> [u8; 16] {
    let top_reference_at = |position: i32| -> u16 {
        if position == -1 {
            u16::from(top_left)
        } else {
            u16::from(top[position as usize])
        }
    };

    let mut prediction = [0_u8; 16];
    for row in 0..4 {
        for column in 0..4 {
            let phase = 2 * column as i32 - row as i32;
            let value = if phase >= 0 && phase % 2 == 0 {
                let center = phase / 2;
                (top_reference_at(center - 1)
                    + 2 * top_reference_at(center)
                    + top_reference_at(center + 1)
                    + 2)
                    >> 2
            } else if phase >= 0 {
                let center = (phase - 1) / 2;
                (top_reference_at(center - 1) + top_reference_at(center) + 1) >> 1
            } else if phase == -1 {
                (u16::from(left[0]) + u16::from(top_left) + 1) >> 1
            } else if phase == -2 {
                (u16::from(left[0]) + 2 * u16::from(top_left) + u16::from(top[0]) + 2) >> 2
            } else {
                (u16::from(left[0]) + u16::from(left[1]) + 1) >> 1
            };
            prediction[row * 4 + column] = value as u8;
        }
    }
    prediction
}

/// Predicts Horizontal_Down by transposing Vertical_Right and extending the left edge.
pub fn predict_luma_intra4x4_horizontal_down(
    top: &[u8; 4],
    left: &[u8; 4],
    top_left: u8,
) -> [u8; 16] {
    let vertical_top = [
        left[0], left[1], left[2], left[3], left[3], left[3], left[3], left[3],
    ];
    let vertical_prediction = predict_luma_intra4x4_vertical_right(&vertical_top, top, top_left);
    let mut prediction = [0_u8; 16];
    for row in 0..4 {
        for column in 0..4 {
            prediction[row * 4 + column] = vertical_prediction[column * 4 + row];
        }
    }
    prediction
}

/// Interpolates a 4x4 luma block using alternating top-reference phases.
pub fn predict_luma_intra4x4_vertical_left(top: &[u8; 8]) -> [u8; 16] {
    let mut prediction = [0_u8; 16];
    for row in 0..4 {
        for column in 0..4 {
            let phase = 2 * column + row;
            let value = if phase % 2 == 0 {
                let center = phase / 2;
                (u16::from(top[center]) + u16::from(top[center + 1]) + 1) >> 1
            } else {
                let center = (phase - 1) / 2;
                (u16::from(top[center])
                    + 2 * u16::from(top[center + 1])
                    + u16::from(top[center + 2])
                    + 2)
                    >> 2
            };
            prediction[row * 4 + column] = value as u8;
        }
    }
    prediction
}

/// Interpolates a 4x4 luma block using alternating left-reference phases.
pub fn predict_luma_intra4x4_horizontal_up(left: &[u8; 8]) -> [u8; 16] {
    let mut prediction = [0_u8; 16];
    for row in 0..4 {
        for column in 0..4 {
            let phase = 2 * row + column;
            let value = if phase % 2 == 0 {
                let center = phase / 2;
                (u16::from(left[center]) + u16::from(left[center + 1]) + 1) >> 1
            } else {
                let center = (phase - 1) / 2;
                (u16::from(left[center])
                    + 2 * u16::from(left[center + 1])
                    + u16::from(left[center + 2])
                    + 2)
                    >> 2
            };
            prediction[row * 4 + column] = value as u8;
        }
    }
    prediction
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
    let b5 = a5 - (a3 >> 2);
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

fn invalid(message: &'static str) -> io::Error {
    io::Error::new(ErrorKind::InvalidData, message)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn qp_vectors_match_other_languages() {
        let levels = [1_i32; 16];
        let scaling_list = [16_u8; 16];
        let vectors = [
            (
                0,
                [
                    10, 13, 10, 13, 13, 16, 13, 16, 10, 13, 10, 13, 13, 16, 13, 16,
                ],
            ),
            (
                24,
                [
                    160, 208, 160, 208, 208, 256, 208, 256, 160, 208, 160, 208, 208, 256, 208, 256,
                ],
            ),
            (
                51,
                [
                    3584, 4608, 3584, 4608, 4608, 5888, 4608, 5888, 3584, 4608, 3584, 4608, 4608,
                    5888, 4608, 5888,
                ],
            ),
        ];

        for (qpy, expected) in vectors {
            assert_eq!(
                inverse_scale_luma4x4(&levels, &scaling_list, qpy).unwrap(),
                expected
            );
        }
    }

    #[test]
    fn inverse_scale_8x8_matches_scaling_class_and_qp_vectors() {
        let levels = [1_i32; 64];
        let scaling_list = [16_u8; 64];
        let vectors = [
            (
                0,
                [
                    20, 19, 25, 19, 20, 19, 25, 19, 19, 18, 24, 18, 19, 18, 24, 18, 25, 24, 32, 24,
                    25, 24, 32, 24, 19, 18, 24, 18, 19, 18, 24, 18, 20, 19, 25, 19, 20, 19, 25, 19,
                    19, 18, 24, 18, 19, 18, 24, 18, 25, 24, 32, 24, 25, 24, 32, 24, 19, 18, 24, 18,
                    19, 18, 24, 18,
                ],
            ),
            (
                24,
                [
                    320, 304, 400, 304, 320, 304, 400, 304, 304, 288, 384, 288, 304, 288, 384, 288,
                    400, 384, 512, 384, 400, 384, 512, 384, 304, 288, 384, 288, 304, 288, 384, 288,
                    320, 304, 400, 304, 320, 304, 400, 304, 304, 288, 384, 288, 304, 288, 384, 288,
                    400, 384, 512, 384, 400, 384, 512, 384, 304, 288, 384, 288, 304, 288, 384, 288,
                ],
            ),
            (
                51,
                [
                    7168, 6656, 8960, 6656, 7168, 6656, 8960, 6656, 6656, 6400, 8448, 6400, 6656,
                    6400, 8448, 6400, 8960, 8448, 11520, 8448, 8960, 8448, 11520, 8448, 6656, 6400,
                    8448, 6400, 6656, 6400, 8448, 6400, 7168, 6656, 8960, 6656, 7168, 6656, 8960,
                    6656, 6656, 6400, 8448, 6400, 6656, 6400, 8448, 6400, 8960, 8448, 11520, 8448,
                    8960, 8448, 11520, 8448, 6656, 6400, 8448, 6400, 6656, 6400, 8448, 6400,
                ],
            ),
        ];

        for (qpy, expected) in vectors {
            assert_eq!(
                inverse_scale_luma8x8(&levels, &scaling_list, qpy).unwrap(),
                expected
            );
        }
    }

    #[test]
    fn inverse_scale_8x8_matches_negative_and_custom_weight_vectors() {
        let mut levels = [0_i32; 64];
        levels[0] = -1;
        levels[1] = 1;
        let mut scaling_list = [16_u8; 64];
        scaling_list[1] = 8;

        let scaled = inverse_scale_luma8x8(&levels, &scaling_list, 0).unwrap();

        assert_eq!((scaled[0], scaled[1]), (-20, 10));
    }

    #[test]
    fn inverse_scale_8x8_rejects_out_of_range_qpy_and_zero_weight() {
        let levels = [0_i32; 64];
        let scaling_list = [16_u8; 64];
        for qpy in [-1, 52] {
            assert_eq!(
                inverse_scale_luma8x8(&levels, &scaling_list, qpy)
                    .unwrap_err()
                    .kind(),
                ErrorKind::InvalidData
            );
        }

        let mut zero_weight = scaling_list;
        zero_weight[42] = 0;
        assert_eq!(
            inverse_scale_luma8x8(&levels, &zero_weight, 0)
                .unwrap_err()
                .kind(),
            ErrorKind::InvalidData
        );
    }

    #[test]
    fn negative_level_and_custom_weight_match_other_languages() {
        let mut levels = [0_i32; 16];
        levels[0] = -1;
        levels[1] = 1;
        let mut scaling_list = [16_u8; 16];
        scaling_list[1] = 8;

        let scaled = inverse_scale_luma4x4(&levels, &scaling_list, 0).unwrap();

        assert_eq!((scaled[0], scaled[1]), (-10, 7));
    }

    #[test]
    fn rejects_out_of_range_qpy_and_zero_scaling_weight() {
        let levels = [0_i32; 16];
        let scaling_list = [16_u8; 16];
        for qpy in [-1, 52] {
            assert_eq!(
                inverse_scale_luma4x4(&levels, &scaling_list, qpy)
                    .unwrap_err()
                    .kind(),
                ErrorKind::InvalidData
            );
        }

        let mut zero_weight = scaling_list;
        zero_weight[7] = 0;
        assert_eq!(
            inverse_scale_luma4x4(&levels, &zero_weight, 0)
                .unwrap_err()
                .kind(),
            ErrorKind::InvalidData
        );
    }

    #[test]
    fn inverse_transform_matches_dc_and_frequency_impulse_vectors() {
        let mut dc = [0_i64; 16];
        dc[0] = 64;
        assert_eq!(inverse_transform_luma4x4(&dc), [1_i64; 16]);

        let mut horizontal_frequency = [0_i64; 16];
        horizontal_frequency[1] = 64;
        assert_eq!(
            inverse_transform_luma4x4(&horizontal_frequency),
            [1, 1, 0, -1, 1, 1, 0, -1, 1, 1, 0, -1, 1, 1, 0, -1]
        );
    }

    #[test]
    fn inverse_transform_matches_signed_rounding_vectors() {
        for (dc_level, expected) in [(31, 0), (32, 1), (-33, -1)] {
            let mut coefficients = [0_i64; 16];
            coefficients[0] = dc_level;
            assert_eq!(inverse_transform_luma4x4(&coefficients), [expected; 16]);
        }
    }

    #[test]
    fn inverse_transform_8x8_matches_dc_and_frequency_impulse_vectors() {
        let mut dc = [0_i64; 64];
        dc[0] = 64;
        assert_eq!(inverse_transform_luma8x8(&dc), [1_i64; 64]);

        let frequency = [2, -1, 1, 0, 0, -1, 1, -1];
        let mut horizontal_frequency = [0_i64; 64];
        horizontal_frequency[1] = 64;
        let mut expected_horizontal = [0_i64; 64];
        for row in 0..8 {
            expected_horizontal[row * 8..row * 8 + 8].copy_from_slice(&frequency);
        }
        assert_eq!(
            inverse_transform_luma8x8(&horizontal_frequency),
            expected_horizontal
        );

        let mut vertical_frequency = [0_i64; 64];
        vertical_frequency[8] = 64;
        let mut expected_vertical = [0_i64; 64];
        for (row, value) in frequency.into_iter().enumerate() {
            expected_vertical[row * 8..row * 8 + 8].fill(value);
        }
        assert_eq!(
            inverse_transform_luma8x8(&vertical_frequency),
            expected_vertical
        );
    }

    #[test]
    fn inverse_transform_8x8_matches_signed_rounding_vectors() {
        for (dc_level, expected) in [(31, 0), (32, 1), (-33, -1)] {
            let mut coefficients = [0_i64; 64];
            coefficients[0] = dc_level;
            assert_eq!(inverse_transform_luma8x8(&coefficients), [expected; 64]);
        }
    }

    #[test]
    fn predicts_vertical_intra8x8_from_filtered_top_reference_samples() {
        let top = [0, 17, 63, 129, 190, 220, 254, 255];
        let mut expected = [0_u8; 64];
        for row in 0..8 {
            expected[row * 8..row * 8 + 8].copy_from_slice(&top);
        }
        assert_eq!(predict_luma_intra8x8_vertical(&top), expected);
    }

    #[test]
    fn predicts_horizontal_intra8x8_from_filtered_left_reference_samples() {
        let left = [0, 17, 63, 129, 190, 220, 254, 255];
        let mut expected = [0_u8; 64];
        for (row, sample) in left.iter().enumerate() {
            expected[row * 8..row * 8 + 8].fill(*sample);
        }
        assert_eq!(predict_luma_intra8x8_horizontal(&left), expected);
    }

    #[test]
    fn predicts_dc_intra8x8_for_reference_availability_and_rounding_cases() {
        let top = [10, 40, 90, 160, 20, 50, 80, 110];
        let left = [20, 60, 100, 140, 30, 70, 110, 150];
        for (top_reference, left_reference, expected) in [
            (Some(&top), Some(&left), 78),
            (Some(&top), None, 70),
            (None, Some(&left), 85),
            (None, None, 128),
        ] {
            assert_eq!(
                predict_luma_intra8x8_dc(top_reference, left_reference),
                [expected; 64]
            );
        }
    }

    #[test]
    fn predicts_diagonal_down_left_intra8x8_with_final_reference_extension() {
        let top = [
            10, 20, 30, 40, 50, 60, 70, 80, 90, 100, 110, 120, 130, 140, 150, 160,
        ];
        assert_eq!(
            predict_luma_intra8x8_diagonal_down_left(&top),
            [
                20, 30, 40, 50, 60, 70, 80, 90, 30, 40, 50, 60, 70, 80, 90, 100, 40, 50, 60, 70,
                80, 90, 100, 110, 50, 60, 70, 80, 90, 100, 110, 120, 60, 70, 80, 90, 100, 110, 120,
                130, 70, 80, 90, 100, 110, 120, 130, 140, 80, 90, 100, 110, 120, 130, 140, 150, 90,
                100, 110, 120, 130, 140, 150, 158,
            ]
        );
    }

    #[test]
    fn predicts_diagonal_down_right_intra8x8_from_all_reference_regions() {
        let top = [
            20, 40, 60, 80, 100, 120, 140, 160, 180, 200, 220, 240, 250, 230, 210, 190,
        ];
        let left = [60, 100, 140, 180, 220, 240, 200, 160];
        assert_eq!(
            predict_luma_intra8x8_diagonal_down_right(&top, &left, 30),
            [
                28, 40, 60, 80, 100, 120, 140, 160, 35, 28, 40, 60, 80, 100, 120, 140, 63, 35, 28,
                40, 60, 80, 100, 120, 100, 63, 35, 28, 40, 60, 80, 100, 140, 100, 63, 35, 28, 40,
                60, 80, 180, 140, 100, 63, 35, 28, 40, 60, 215, 180, 140, 100, 63, 35, 28, 40, 225,
                215, 180, 140, 100, 63, 35, 28,
            ]
        );
    }

    #[test]
    fn predicts_vertical_right_intra8x8_from_all_reference_regions() {
        let top = [
            20, 40, 60, 80, 100, 120, 140, 160, 180, 200, 220, 240, 250, 230, 210, 190,
        ];
        let left = [60, 100, 140, 180, 220, 240, 200, 160];
        assert_eq!(
            predict_luma_intra8x8_vertical_right(&top, &left, 30),
            [
                28, 40, 60, 80, 100, 120, 140, 160, 45, 25, 30, 50, 70, 90, 110, 130, 35, 28, 40,
                60, 80, 100, 120, 140, 80, 45, 25, 30, 50, 70, 90, 110, 100, 35, 28, 40, 60, 80,
                100, 120, 120, 80, 45, 25, 30, 50, 70, 90, 140, 100, 35, 28, 40, 60, 80, 100, 160,
                120, 80, 45, 25, 30, 50, 70,
            ]
        );
    }

    #[test]
    fn predicts_horizontal_down_intra8x8_as_vertical_right_transpose() {
        let top = [60, 100, 140, 180, 220, 240, 200, 160];
        let left = [
            20, 40, 60, 80, 100, 120, 140, 160, 180, 200, 220, 240, 250, 230, 210, 190,
        ];
        assert_eq!(
            predict_luma_intra8x8_horizontal_down(&top, &left, 30),
            [
                28, 45, 35, 80, 100, 120, 140, 160, 40, 25, 28, 45, 35, 80, 100, 120, 60, 30, 40,
                25, 28, 45, 35, 80, 80, 50, 60, 30, 40, 25, 28, 45, 100, 70, 80, 50, 60, 30, 40,
                25, 120, 90, 100, 70, 80, 50, 60, 30, 140, 110, 120, 90, 100, 70, 80, 50, 160, 130,
                140, 110, 120, 90, 100, 70,
            ]
        );
    }

    #[test]
    fn predicts_vertical_left_intra8x8_across_top_reference_phases() {
        let top = [
            10, 20, 30, 40, 50, 60, 70, 80, 90, 100, 110, 120, 130, 140, 150, 160,
        ];
        assert_eq!(
            predict_luma_intra8x8_vertical_left(&top),
            [
                15, 25, 35, 45, 55, 65, 75, 85, 20, 30, 40, 50, 60, 70, 80, 90, 25, 35, 45, 55, 65,
                75, 85, 95, 30, 40, 50, 60, 70, 80, 90, 100, 35, 45, 55, 65, 75, 85, 95, 105, 40,
                50, 60, 70, 80, 90, 100, 110, 45, 55, 65, 75, 85, 95, 105, 115, 50, 60, 70, 80, 90,
                100, 110, 120,
            ]
        );
    }

    #[test]
    fn predicts_horizontal_up_intra8x8_as_vertical_left_transpose() {
        let left = [
            10, 20, 30, 40, 50, 60, 70, 80, 90, 100, 110, 120, 130, 140, 150, 160,
        ];
        assert_eq!(
            predict_luma_intra8x8_horizontal_up(&left),
            [
                15, 20, 25, 30, 35, 40, 45, 50, 25, 30, 35, 40, 45, 50, 55, 60, 35, 40, 45, 50, 55,
                60, 65, 70, 45, 50, 55, 60, 65, 70, 75, 80, 55, 60, 65, 70, 75, 80, 85, 90, 65, 70,
                75, 80, 85, 90, 95, 100, 75, 80, 85, 90, 95, 100, 105, 110, 85, 90, 95, 100, 105,
                110, 115, 120,
            ]
        );
    }

    #[test]
    fn predicts_vertical_intra4x4_from_top_reference_samples() {
        for (top, expected) in [
            (
                [10, 40, 90, 160],
                [
                    10, 40, 90, 160, 10, 40, 90, 160, 10, 40, 90, 160, 10, 40, 90, 160,
                ],
            ),
            (
                [0, 255, 0, 255],
                [
                    0, 255, 0, 255, 0, 255, 0, 255, 0, 255, 0, 255, 0, 255, 0, 255,
                ],
            ),
        ] {
            assert_eq!(predict_luma_intra4x4_vertical(&top), expected);
        }
    }

    #[test]
    fn predicts_horizontal_intra4x4_from_left_reference_samples() {
        for (left, expected) in [
            (
                [10, 40, 90, 160],
                [
                    10, 10, 10, 10, 40, 40, 40, 40, 90, 90, 90, 90, 160, 160, 160, 160,
                ],
            ),
            (
                [0, 255, 0, 255],
                [
                    0, 0, 0, 0, 255, 255, 255, 255, 0, 0, 0, 0, 255, 255, 255, 255,
                ],
            ),
        ] {
            assert_eq!(predict_luma_intra4x4_horizontal(&left), expected);
        }
    }

    #[test]
    fn predicts_dc_intra4x4_for_reference_availability_cases() {
        let top = [10, 40, 90, 161];
        let left = [20, 60, 100, 141];
        for (top_reference, left_reference, expected) in [
            (Some(&top), Some(&left), 78),
            (Some(&top), None, 75),
            (None, Some(&left), 80),
            (None, None, 128),
        ] {
            assert_eq!(
                predict_luma_intra4x4_dc(top_reference, left_reference),
                [expected; 16]
            );
        }
    }

    #[test]
    fn predicts_diagonal_down_left_intra4x4_with_final_reference_extension() {
        let top = [10, 20, 30, 40, 50, 60, 70, 80];
        assert_eq!(
            predict_luma_intra4x4_diagonal_down_left(&top),
            [20, 30, 40, 50, 30, 40, 50, 60, 40, 50, 60, 70, 50, 60, 70, 78]
        );
    }

    #[test]
    fn predicts_diagonal_down_right_intra4x4_from_all_reference_regions() {
        let top = [20, 40, 80, 120, 160, 200, 220, 240];
        let left = [60, 100, 140, 180];
        assert_eq!(
            predict_luma_intra4x4_diagonal_down_right(&top, &left, 30),
            [28, 45, 80, 120, 35, 28, 45, 80, 63, 35, 28, 45, 100, 63, 35, 28]
        );
    }

    #[test]
    fn predicts_vertical_right_intra4x4_from_all_reference_regions() {
        let top = [20, 40, 80, 120, 160, 200, 220, 240];
        let left = [60, 100, 140, 180];
        assert_eq!(
            predict_luma_intra4x4_vertical_right(&top, &left, 30),
            [28, 45, 80, 120, 45, 25, 30, 60, 35, 28, 45, 80, 80, 45, 25, 30]
        );
    }

    #[test]
    fn predicts_horizontal_down_intra4x4_as_vertical_right_transpose() {
        let top = [20, 40, 80, 120];
        let left = [60, 100, 140, 180];
        assert_eq!(
            predict_luma_intra4x4_horizontal_down(&top, &left, 30),
            [63, 25, 35, 30, 100, 45, 63, 25, 140, 80, 100, 45, 170, 120, 140, 80]
        );
    }

    #[test]
    fn predicts_vertical_left_intra4x4_with_alternating_top_phases() {
        let top = [10, 20, 30, 40, 50, 60, 70, 80];
        assert_eq!(
            predict_luma_intra4x4_vertical_left(&top),
            [15, 25, 35, 45, 20, 30, 40, 50, 25, 35, 45, 55, 30, 40, 50, 60]
        );
    }

    #[test]
    fn predicts_horizontal_up_intra4x4_with_alternating_left_phases() {
        let left = [10, 20, 30, 40, 50, 60, 70, 80];
        assert_eq!(
            predict_luma_intra4x4_horizontal_up(&left),
            [15, 20, 25, 30, 25, 30, 35, 40, 35, 40, 45, 50, 45, 50, 55, 60]
        );
    }
}
