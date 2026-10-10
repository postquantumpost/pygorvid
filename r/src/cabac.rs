use std::io::{self, ErrorKind};

use crate::BitReader;

const INITIAL_RANGE: u32 = 510;
const MAX_QPY: u32 = 51;
const MAX_MOTION_VECTOR_DIFFERENCE: u32 = i32::MAX as u32;
const MAX_COEFF_LEVEL_PREFIX: u32 = 23;
const COEFF_ABS_LEVEL1_CONTEXT: [usize; 8] = [1, 2, 3, 4, 0, 0, 0, 0];
const COEFF_ABS_LEVEL_GREATER1_CONTEXT: [usize; 8] = [5, 5, 5, 5, 6, 7, 8, 9];
const COEFF_LEVEL1_TRANSITION: [usize; 8] = [1, 2, 3, 3, 4, 5, 6, 7];
const COEFF_LEVEL_GREATER1_TRANSITION: [usize; 8] = [4, 4, 4, 4, 5, 6, 7, 7];
const RANGE_LPS: [[u8; 64]; 4] = [
    [
        128, 128, 128, 123, 116, 111, 105, 100, 95, 90, 85, 81, 77, 73, 69, 66, 62, 59, 56, 53, 51,
        48, 46, 43, 41, 39, 37, 35, 33, 32, 30, 29, 27, 26, 24, 23, 22, 21, 20, 19, 18, 17, 16, 15,
        14, 14, 13, 12, 12, 11, 11, 10, 10, 9, 9, 8, 8, 7, 7, 7, 6, 6, 6, 2,
    ],
    [
        176, 167, 158, 150, 142, 135, 128, 122, 116, 110, 104, 99, 94, 89, 85, 80, 76, 72, 69, 65,
        62, 59, 56, 53, 50, 48, 45, 43, 41, 39, 37, 35, 33, 31, 30, 28, 27, 26, 24, 23, 22, 21, 20,
        19, 18, 17, 16, 15, 14, 14, 13, 12, 12, 11, 11, 10, 9, 9, 9, 8, 8, 7, 7, 2,
    ],
    [
        208, 197, 187, 178, 169, 160, 152, 144, 137, 130, 123, 117, 111, 105, 100, 95, 90, 86, 81,
        77, 73, 69, 66, 63, 59, 56, 54, 51, 48, 46, 43, 41, 39, 37, 35, 33, 32, 30, 29, 27, 26, 25,
        23, 22, 21, 20, 19, 18, 17, 16, 15, 15, 14, 13, 12, 12, 11, 11, 10, 10, 9, 9, 8, 2,
    ],
    [
        240, 227, 216, 205, 195, 185, 175, 166, 158, 150, 142, 135, 128, 122, 116, 110, 104, 99,
        94, 89, 85, 80, 76, 72, 69, 65, 62, 59, 56, 53, 50, 48, 45, 43, 41, 39, 37, 35, 33, 31, 30,
        28, 27, 25, 24, 23, 22, 21, 20, 19, 18, 17, 16, 15, 14, 14, 13, 12, 12, 11, 11, 10, 9, 2,
    ],
];
const TRANSITION_LPS: [u8; 64] = [
    0, 0, 1, 2, 2, 4, 4, 5, 6, 7, 8, 9, 9, 11, 11, 12, 13, 13, 15, 15, 16, 16, 18, 18, 19, 19, 21,
    21, 22, 22, 23, 24, 24, 25, 26, 26, 27, 27, 28, 29, 29, 30, 30, 30, 31, 32, 32, 33, 33, 33, 34,
    34, 35, 35, 35, 36, 36, 37, 37, 37, 38, 38, 63, 63,
];

#[derive(Clone, Copy)]
pub struct CabacContextModel {
    state_index: u8,
    value_mps: bool,
}

impl CabacContextModel {
    /// Initializes one context from its normative (m, n) parameters and SliceQPY.
    pub fn new(m: i32, n: i32, slice_qpy: i32) -> io::Result<Self> {
        if !(-128..=127).contains(&m)
            || !(-128..=127).contains(&n)
            || !(0..=51).contains(&slice_qpy)
        {
            return Err(invalid(
                "CABAC context initialization value is outside its valid range",
            ));
        }
        let pre_context_state = ((m * slice_qpy) >> 4) + n;
        let pre_context_state = pre_context_state.clamp(1, 126);
        let (state_index, value_mps) = if pre_context_state <= 63 {
            ((63 - pre_context_state) as u8, false)
        } else {
            ((pre_context_state - 64) as u8, true)
        };
        Ok(Self {
            state_index,
            value_mps,
        })
    }

    pub fn state_index(&self) -> u8 {
        self.state_index
    }

    pub fn mps(&self) -> bool {
        self.value_mps
    }

    /// Adapts the context after decoding one bin.
    pub fn update(&mut self, bin_value: bool) {
        if bin_value == self.value_mps {
            if self.state_index < 62 {
                self.state_index += 1;
            }
            return;
        }
        if self.state_index == 0 {
            self.value_mps = !self.value_mps;
        }
        self.state_index = TRANSITION_LPS[self.state_index as usize];
    }
}

pub struct CabacArithmeticDecoder<'a> {
    bits: BitReader<'a>,
    code_range: u32,
    code_offset: u32,
    terminated: bool,
}

impl<'a> CabacArithmeticDecoder<'a> {
    /// Initializes one byte-aligned CABAC substream after slice-header alignment bits.
    pub fn new(data: &'a [u8]) -> io::Result<Self> {
        let mut bits = BitReader::new(data);
        let code_offset = bits.read_bits(9)?;
        if code_offset >= INITIAL_RANGE {
            return Err(invalid(
                "CABAC initial offset is outside the arithmetic range",
            ));
        }
        Ok(Self {
            bits,
            code_range: INITIAL_RANGE,
            code_offset,
            terminated: false,
        })
    }

    pub fn code_range(&self) -> u32 {
        self.code_range
    }

    pub fn code_offset(&self) -> u32 {
        self.code_offset
    }

    /// Decodes one regular context-coded bin and updates its model.
    pub fn decode_bin(&mut self, model: &mut CabacContextModel) -> io::Result<bool> {
        self.validate_bin_state()?;
        if model.state_index >= 64 {
            return Err(invalid("CABAC context state index is outside [0,63]"));
        }
        let mut bits = self.bits.clone();
        let range_lps =
            u32::from(RANGE_LPS[((self.code_range >> 6) & 3) as usize][model.state_index as usize]);
        let range_mps = self.code_range - range_lps;
        let mut code_range;
        let mut code_offset = self.code_offset;
        let decoded;
        let mut next_model = *model;
        if code_offset >= range_mps {
            decoded = !next_model.value_mps;
            code_offset -= range_mps;
            code_range = range_lps;
        } else {
            decoded = next_model.value_mps;
            code_range = range_mps;
        }
        while code_range < 256 {
            let bit = bits
                .read_bit()
                .map_err(|error| context("regular bin", error))?;
            code_range <<= 1;
            code_offset = (code_offset << 1) | u32::from(bit);
            if code_offset >= code_range {
                return Err(invalid("CABAC code offset is outside the arithmetic range"));
            }
        }
        next_model.update(decoded);
        self.bits = bits;
        self.code_range = code_range;
        self.code_offset = code_offset;
        *model = next_model;
        Ok(decoded)
    }

    /// Decodes mb_qp_delta using caller-initialized contexts 60 through 63.
    pub fn decode_mb_qp_delta(
        &mut self,
        contexts: &mut [CabacContextModel; 4],
        previous_delta: i32,
    ) -> io::Result<i32> {
        self.validate_bin_state()?;
        if contexts.iter().any(|model| model.state_index >= 64) {
            return Err(invalid("mb_qp_delta requires valid contexts 60 through 63"));
        }

        let mut trial = Self {
            bits: self.bits.clone(),
            code_range: self.code_range,
            code_offset: self.code_offset,
            terminated: self.terminated,
        };
        let mut trial_contexts = *contexts;
        let mut context_index = usize::from(previous_delta != 0);
        let first = trial.decode_bin(&mut trial_contexts[context_index])?;
        let delta = if first {
            let mut value = 1u32;
            context_index = 2;
            loop {
                let continued = trial.decode_bin(&mut trial_contexts[context_index])?;
                if !continued {
                    break;
                }
                value += 1;
                if value > 2 * MAX_QPY {
                    return Err(invalid("CABAC mb_qp_delta exceeds the 8-bit QP range"));
                }
                context_index = 3;
            }
            let magnitude = ((value + 1) >> 1) as i32;
            if value & 1 == 0 {
                -magnitude
            } else {
                magnitude
            }
        } else {
            0
        };

        self.bits = trial.bits;
        self.code_range = trial.code_range;
        self.code_offset = trial.code_offset;
        self.terminated = trial.terminated;
        *contexts = trial_contexts;
        Ok(delta)
    }

    /// Decodes mb_skip_flag using three caller-initialized P- or B-slice contexts.
    pub fn decode_mb_skip_flag(
        &mut self,
        slice_type: u8,
        contexts: &mut [CabacContextModel; 3],
        left_available: bool,
        left_skipped: bool,
        top_available: bool,
        top_skipped: bool,
    ) -> io::Result<bool> {
        if slice_type > 9 || !matches!(slice_type % 5, 0 | 1) {
            return Err(invalid(
                "CABAC mb_skip_flag is unsupported for this slice type",
            ));
        }
        let context_index = usize::from(left_available && !left_skipped)
            + usize::from(top_available && !top_skipped);
        self.decode_bin(&mut contexts[context_index])
    }

    /// Decodes I-slice mb_type using caller-initialized contexts 3 through 10.
    pub fn decode_i_intra_mb_type(
        &mut self,
        slice_type: u8,
        contexts: &mut [CabacContextModel; 8],
        left_available: bool,
        left_intra16_or_pcm: bool,
        top_available: bool,
        top_intra16_or_pcm: bool,
    ) -> io::Result<u8> {
        if slice_type > 9 || slice_type % 5 != 2 {
            return Err(invalid(
                "I-slice mb_type is unsupported for this slice type",
            ));
        }
        if contexts.iter().any(|model| model.state_index >= 64) {
            return Err(invalid(
                "I-slice mb_type requires valid contexts 3 through 10",
            ));
        }

        let mut trial = Self {
            bits: self.bits.clone(),
            code_range: self.code_range,
            code_offset: self.code_offset,
            terminated: self.terminated,
        };
        let mut trial_contexts = *contexts;
        let first_context = usize::from(left_available && left_intra16_or_pcm)
            + usize::from(top_available && top_intra16_or_pcm);
        let first = trial.decode_bin(&mut trial_contexts[first_context])?;
        let mb_type = if !first {
            0
        } else if trial.decode_terminate_bin()? {
            25
        } else {
            let mut mb_type = 1 + 12 * u8::from(trial.decode_bin(&mut trial_contexts[3])?);
            if trial.decode_bin(&mut trial_contexts[4])? {
                mb_type += 4 + 4 * u8::from(trial.decode_bin(&mut trial_contexts[5])?);
            }
            mb_type += 2 * u8::from(trial.decode_bin(&mut trial_contexts[6])?);
            mb_type += u8::from(trial.decode_bin(&mut trial_contexts[7])?);
            mb_type
        };

        self.bits = trial.bits;
        self.code_range = trial.code_range;
        self.code_offset = trial.code_offset;
        self.terminated = trial.terminated;
        *contexts = trial_contexts;
        Ok(mb_type)
    }

    /// Decodes intra_chroma_pred_mode using caller-initialized contexts 64 through 67.
    pub fn decode_intra_chroma_pred_mode(
        &mut self,
        contexts: &mut [CabacContextModel; 4],
        left_has_nonzero_mode: bool,
        top_has_nonzero_mode: bool,
    ) -> io::Result<u8> {
        if contexts.iter().any(|model| model.state_index >= 64) {
            return Err(invalid(
                "intra_chroma_pred_mode requires valid contexts 64 through 67",
            ));
        }
        let mut trial = Self {
            bits: self.bits.clone(),
            code_range: self.code_range,
            code_offset: self.code_offset,
            terminated: self.terminated,
        };
        let mut trial_contexts = *contexts;
        let context_index = usize::from(left_has_nonzero_mode) + usize::from(top_has_nonzero_mode);
        let mode = if !trial.decode_bin(&mut trial_contexts[context_index])? {
            0
        } else if !trial.decode_bin(&mut trial_contexts[3])? {
            1
        } else if !trial.decode_bin(&mut trial_contexts[3])? {
            2
        } else {
            3
        };

        self.bits = trial.bits;
        self.code_range = trial.code_range;
        self.code_offset = trial.code_offset;
        self.terminated = trial.terminated;
        *contexts = trial_contexts;
        Ok(mode)
    }

    /// Decodes prev_intra4x4_pred_mode_flag and rem_intra4x4_pred_mode using contexts 68 and 69.
    pub fn decode_intra4x4_pred_mode(
        &mut self,
        predicted_mode: u8,
        contexts: &mut [CabacContextModel; 2],
    ) -> io::Result<u8> {
        if predicted_mode > 8 {
            return Err(invalid("CABAC intra4x4 prediction mode is outside [0,8]"));
        }
        if contexts.iter().any(|model| model.state_index >= 64) {
            return Err(invalid(
                "intra4x4 prediction mode requires valid contexts 68 and 69",
            ));
        }
        let mut trial = Self {
            bits: self.bits.clone(),
            code_range: self.code_range,
            code_offset: self.code_offset,
            terminated: self.terminated,
        };
        let mut trial_contexts = *contexts;
        let mode = if trial.decode_bin(&mut trial_contexts[0])? {
            predicted_mode
        } else {
            let mut remaining_mode = 0u8;
            for bit_index in 0..3 {
                remaining_mode |= u8::from(trial.decode_bin(&mut trial_contexts[1])?) << bit_index;
            }
            remaining_mode + u8::from(remaining_mode >= predicted_mode)
        };

        self.bits = trial.bits;
        self.code_range = trial.code_range;
        self.code_offset = trial.code_offset;
        self.terminated = trial.terminated;
        *contexts = trial_contexts;
        Ok(mode)
    }

    /// Decodes transform_size_8x8_flag using contexts 399 through 401.
    /// Neighbor flags must already account for availability and slice membership.
    pub fn decode_transform_size_8x8_flag(
        &mut self,
        contexts: &mut [CabacContextModel; 3],
        left_has_8x8_transform: bool,
        top_has_8x8_transform: bool,
    ) -> io::Result<bool> {
        let context_index =
            usize::from(left_has_8x8_transform) + usize::from(top_has_8x8_transform);
        self.decode_bin(&mut contexts[context_index])
    }

    /// Decodes the four luma coded_block_pattern bins using contexts 73 through 76.
    pub fn decode_luma_coded_block_pattern(
        &mut self,
        left_cbp: u8,
        top_cbp: u8,
        contexts: &mut [CabacContextModel; 4],
    ) -> io::Result<u8> {
        if left_cbp > 15 || top_cbp > 15 {
            return Err(invalid("CABAC luma coded_block_pattern is outside [0,15]"));
        }
        if contexts.iter().any(|model| model.state_index >= 64) {
            return Err(invalid(
                "luma coded_block_pattern requires valid contexts 73 through 76",
            ));
        }

        let mut trial = Self {
            bits: self.bits.clone(),
            code_range: self.code_range,
            code_offset: self.code_offset,
            terminated: self.terminated,
        };
        let mut trial_contexts = *contexts;
        let mut pattern = 0u8;
        let mut context_index =
            usize::from(left_cbp & 0x02 == 0) + 2 * usize::from(top_cbp & 0x04 == 0);
        pattern |= u8::from(trial.decode_bin(&mut trial_contexts[context_index])?);

        context_index = usize::from(pattern & 0x01 == 0) + 2 * usize::from(top_cbp & 0x08 == 0);
        pattern |= u8::from(trial.decode_bin(&mut trial_contexts[context_index])?) << 1;

        context_index = usize::from(left_cbp & 0x08 == 0) + 2 * usize::from(pattern & 0x01 == 0);
        pattern |= u8::from(trial.decode_bin(&mut trial_contexts[context_index])?) << 2;

        context_index = usize::from(pattern & 0x04 == 0) + 2 * usize::from(pattern & 0x02 == 0);
        pattern |= u8::from(trial.decode_bin(&mut trial_contexts[context_index])?) << 3;

        self.bits = trial.bits;
        self.code_range = trial.code_range;
        self.code_offset = trial.code_offset;
        self.terminated = trial.terminated;
        *contexts = trial_contexts;
        Ok(pattern)
    }

    /// Decodes chroma coded_block_pattern using contexts 77 through 84.
    pub fn decode_chroma_coded_block_pattern(
        &mut self,
        left_cbp: u8,
        top_cbp: u8,
        contexts: &mut [CabacContextModel; 8],
    ) -> io::Result<u8> {
        if left_cbp > 2 || top_cbp > 2 {
            return Err(invalid("CABAC chroma coded_block_pattern is outside [0,2]"));
        }
        if contexts.iter().any(|model| model.state_index >= 64) {
            return Err(invalid(
                "chroma coded_block_pattern requires valid contexts 77 through 84",
            ));
        }
        let mut trial = Self {
            bits: self.bits.clone(),
            code_range: self.code_range,
            code_offset: self.code_offset,
            terminated: self.terminated,
        };
        let mut trial_contexts = *contexts;
        let context_index = usize::from(left_cbp > 0) + 2 * usize::from(top_cbp > 0);
        let pattern = if !trial.decode_bin(&mut trial_contexts[context_index])? {
            0
        } else {
            let context_index = 4 + usize::from(left_cbp == 2) + 2 * usize::from(top_cbp == 2);
            1 + u8::from(trial.decode_bin(&mut trial_contexts[context_index])?)
        };

        self.bits = trial.bits;
        self.code_range = trial.code_range;
        self.code_offset = trial.code_offset;
        self.terminated = trial.terminated;
        *contexts = trial_contexts;
        Ok(pattern)
    }

    /// Decodes a luma 4x4 coded_block_flag using contexts 93 through 96.
    pub fn decode_luma4x4_coded_block_flag(
        &mut self,
        left_nonzero: u8,
        top_nonzero: u8,
        contexts: &mut [CabacContextModel; 4],
    ) -> io::Result<bool> {
        if left_nonzero > 16 || top_nonzero > 16 {
            return Err(invalid("CABAC luma 4x4 nonzero count is outside [0,16]"));
        }
        let context_index = usize::from(left_nonzero > 0) + 2 * usize::from(top_nonzero > 0);
        self.decode_bin(&mut contexts[context_index])
    }

    /// Decodes frame-scan significant/last-significant flags for a luma 4x4 block.
    /// Scan position 15 is implied significant if no earlier last flag terminates the scan.
    pub fn decode_luma4x4_significance_map(
        &mut self,
        significant_contexts: &mut [CabacContextModel; 15],
        last_contexts: &mut [CabacContextModel; 15],
    ) -> io::Result<[bool; 16]> {
        if significant_contexts
            .iter()
            .chain(last_contexts.iter())
            .any(|model| model.state_index >= 64)
        {
            return Err(invalid("luma4x4 significance map requires valid contexts"));
        }
        let mut trial = Self {
            bits: self.bits.clone(),
            code_range: self.code_range,
            code_offset: self.code_offset,
            terminated: self.terminated,
        };
        let mut trial_significant = *significant_contexts;
        let mut trial_last = *last_contexts;
        let mut significance = [false; 16];
        let mut last_found = false;
        for scan_index in 0..15 {
            if !trial.decode_bin(&mut trial_significant[scan_index])? {
                continue;
            }
            significance[scan_index] = true;
            if trial.decode_bin(&mut trial_last[scan_index])? {
                last_found = true;
                break;
            }
        }
        if !last_found {
            significance[15] = true;
        }

        self.bits = trial.bits;
        self.code_range = trial.code_range;
        self.code_offset = trial.code_offset;
        self.terminated = trial.terminated;
        *significant_contexts = trial_significant;
        *last_contexts = trial_last;
        Ok(significance)
    }

    /// Decodes one coeff_abs_level_minus1 with caller-selected regular-bin contexts.
    pub fn decode_coeff_abs_level_minus1(
        &mut self,
        first_context: &mut CabacContextModel,
        greater_one_context: &mut CabacContextModel,
    ) -> io::Result<u32> {
        if std::ptr::eq(first_context, greater_one_context)
            || first_context.state_index >= 64
            || greater_one_context.state_index >= 64
        {
            return Err(invalid(
                "coefficient level requires two distinct valid contexts",
            ));
        }
        let mut trial = Self {
            bits: self.bits.clone(),
            code_range: self.code_range,
            code_offset: self.code_offset,
            terminated: self.terminated,
        };
        let mut trial_first = *first_context;
        let mut trial_greater = *greater_one_context;
        let absolute_level = if !trial.decode_bin(&mut trial_first)? {
            1
        } else {
            let mut absolute_level = 2u32;
            while absolute_level < 15 && trial.decode_bin(&mut trial_greater)? {
                absolute_level += 1;
            }
            if absolute_level == 15 {
                let mut prefix_length = 0u32;
                loop {
                    if !trial.decode_bypass_bin()? {
                        break;
                    }
                    prefix_length += 1;
                    if prefix_length >= MAX_COEFF_LEVEL_PREFIX {
                        return Err(invalid(
                            "CABAC coefficient level exceeds supported bypass prefix",
                        ));
                    }
                }
                let mut suffix = 0u32;
                for _ in 0..prefix_length {
                    suffix = (suffix << 1) | u32::from(trial.decode_bypass_bin()?);
                }
                14 + (1 << prefix_length) + suffix
            } else {
                absolute_level
            }
        };

        self.bits = trial.bits;
        self.code_range = trial.code_range;
        self.code_offset = trial.code_offset;
        self.terminated = trial.terminated;
        *first_context = trial_first;
        *greater_one_context = trial_greater;
        Ok(absolute_level - 1)
    }

    /// Decodes coeff_sign_flag and applies it to coeff_abs_level_minus1.
    pub fn decode_coeff_sign(&mut self, abs_level_minus1: u32) -> io::Result<i32> {
        if abs_level_minus1 >= i32::MAX as u32 {
            return Err(invalid(
                "CABAC coefficient magnitude exceeds the signed output range",
            ));
        }
        let negative = self.decode_bypass_bin()?;
        let magnitude = (abs_level_minus1 + 1) as i32;
        Ok(if negative { -magnitude } else { magnitude })
    }

    /// Decodes one MVD component from seven consecutive contexts starting at 40 or 47.
    /// `neighbor_magnitude` is the sum of the absolute left/top MVD components.
    pub fn decode_motion_vector_difference(
        &mut self,
        neighbor_magnitude: u64,
        contexts: &mut [CabacContextModel; 7],
    ) -> io::Result<i32> {
        if contexts.iter().any(|context| context.state_index >= 64) {
            return Err(invalid("MVD context state index is outside [0,63]"));
        }
        let mut context_index =
            usize::from(neighbor_magnitude >= 3) + usize::from(neighbor_magnitude >= 33);
        let mut trial = Self {
            bits: self.bits.clone(),
            code_range: self.code_range,
            code_offset: self.code_offset,
            terminated: self.terminated,
        };
        let mut trial_contexts = *contexts;
        let greater_than_zero = trial.decode_bin(&mut trial_contexts[context_index])?;
        let signed_magnitude = if !greater_than_zero {
            0
        } else {
            let mut magnitude = 1_u32;
            context_index = 3;
            while magnitude < 9 {
                if !trial.decode_bin(&mut trial_contexts[context_index])? {
                    break;
                }
                if magnitude < 4 {
                    context_index += 1;
                }
                magnitude += 1;
            }

            if magnitude >= 9 {
                let mut suffix_length = 3_u32;
                while trial.decode_bypass_bin()? {
                    if suffix_length > 30 {
                        return Err(invalid("CABAC MVD exceeds the signed output range"));
                    }
                    let increment = 1_u32 << suffix_length;
                    if magnitude > MAX_MOTION_VECTOR_DIFFERENCE - increment {
                        return Err(invalid("CABAC MVD exceeds the signed output range"));
                    }
                    magnitude += increment;
                    suffix_length += 1;
                }
                if suffix_length > 30 {
                    return Err(invalid("CABAC MVD exceeds the signed output range"));
                }
                for bit_index in (0..suffix_length).rev() {
                    if trial.decode_bypass_bin()? {
                        let increment = 1_u32 << bit_index;
                        if magnitude > MAX_MOTION_VECTOR_DIFFERENCE - increment {
                            return Err(invalid("CABAC MVD exceeds the signed output range"));
                        }
                        magnitude += increment;
                    }
                }
            }

            if trial.decode_bypass_bin()? {
                -(magnitude as i32)
            } else {
                magnitude as i32
            }
        };

        self.bits = trial.bits;
        self.code_range = trial.code_range;
        self.code_offset = trial.code_offset;
        self.terminated = trial.terminated;
        *contexts = trial_contexts;
        Ok(signed_magnitude)
    }

    /// Decodes signed levels for a frame-scan luma 4x4 significance map.
    /// Results remain in scan order; coefficient placement and dequantization are separate.
    pub fn decode_luma4x4_residual_levels(
        &mut self,
        significance: &[bool; 16],
        contexts: &mut [CabacContextModel; 10],
    ) -> io::Result<[i32; 16]> {
        if !significance.iter().any(|&value| value) {
            return Err(invalid(
                "CABAC residual block has no significant coefficients",
            ));
        }
        if contexts.iter().any(|model| model.state_index >= 64) {
            return Err(invalid("luma4x4 residual decoding requires valid contexts"));
        }
        let mut trial = Self {
            bits: self.bits.clone(),
            code_range: self.code_range,
            code_offset: self.code_offset,
            terminated: self.terminated,
        };
        let mut trial_contexts = *contexts;
        let mut levels = [0i32; 16];
        let mut node_context = 0usize;
        for scan_index in (0..16).rev() {
            if !significance[scan_index] {
                continue;
            }
            let (level1_contexts, greater_one_contexts) = trial_contexts.split_at_mut(5);
            let level_minus1 = trial.decode_coeff_abs_level_minus1(
                &mut level1_contexts[COEFF_ABS_LEVEL1_CONTEXT[node_context]],
                &mut greater_one_contexts[COEFF_ABS_LEVEL_GREATER1_CONTEXT[node_context] - 5],
            )?;
            levels[scan_index] = trial.decode_coeff_sign(level_minus1)?;
            node_context = if level_minus1 == 0 {
                COEFF_LEVEL1_TRANSITION[node_context]
            } else {
                COEFF_LEVEL_GREATER1_TRANSITION[node_context]
            };
        }

        self.bits = trial.bits;
        self.code_range = trial.code_range;
        self.code_offset = trial.code_offset;
        self.terminated = trial.terminated;
        *contexts = trial_contexts;
        Ok(levels)
    }

    /// Decodes significance, signed levels, and raster placement as one transactional block.
    pub fn decode_luma4x4_residual_block(
        &mut self,
        significant_contexts: &mut [CabacContextModel; 15],
        last_contexts: &mut [CabacContextModel; 15],
        coefficient_contexts: &mut [CabacContextModel; 10],
    ) -> io::Result<[i32; 16]> {
        let mut trial = Self {
            bits: self.bits.clone(),
            code_range: self.code_range,
            code_offset: self.code_offset,
            terminated: self.terminated,
        };
        let mut trial_significant = *significant_contexts;
        let mut trial_last = *last_contexts;
        let mut trial_coefficients = *coefficient_contexts;
        let significance =
            trial.decode_luma4x4_significance_map(&mut trial_significant, &mut trial_last)?;
        let scan_levels =
            trial.decode_luma4x4_residual_levels(&significance, &mut trial_coefficients)?;
        let raster_levels = place_luma4x4_scan_levels(&scan_levels);

        self.bits = trial.bits;
        self.code_range = trial.code_range;
        self.code_offset = trial.code_offset;
        self.terminated = trial.terminated;
        *significant_contexts = trial_significant;
        *last_contexts = trial_last;
        *coefficient_contexts = trial_coefficients;
        Ok(raster_levels)
    }

    /// Decodes a bypass bin without changing the arithmetic range.
    pub fn decode_bypass_bin(&mut self) -> io::Result<bool> {
        self.validate_bin_state()?;
        let mut bits = self.bits.clone();
        let bit = bits
            .read_bit()
            .map_err(|error| context("bypass bin", error))?;
        let mut code_offset = (self.code_offset << 1) | u32::from(bit);
        let decoded = code_offset >= self.code_range;
        if decoded {
            code_offset -= self.code_range;
        }
        self.bits = bits;
        self.code_offset = code_offset;
        Ok(decoded)
    }

    /// Decodes a terminate bin; true means the caller must stop decoding.
    pub fn decode_terminate_bin(&mut self) -> io::Result<bool> {
        self.validate_bin_state()?;
        let mut bits = self.bits.clone();
        let mut code_range = self.code_range - 2;
        let mut code_offset = self.code_offset;
        if code_offset >= code_range {
            self.code_range = code_range;
            self.terminated = true;
            return Ok(true);
        }
        while code_range < 256 {
            let bit = bits
                .read_bit()
                .map_err(|error| context("terminate bin", error))?;
            code_range <<= 1;
            code_offset = (code_offset << 1) | u32::from(bit);
            if code_offset >= code_range {
                return Err(invalid("CABAC code offset is outside the arithmetic range"));
            }
        }
        self.bits = bits;
        self.code_range = code_range;
        self.code_offset = code_offset;
        Ok(false)
    }

    fn validate_bin_state(&self) -> io::Result<()> {
        if self.terminated {
            return Err(invalid("CABAC decoder is already terminated"));
        }
        if !(256..=INITIAL_RANGE).contains(&self.code_range) {
            return Err(invalid("CABAC bin decoding range is outside [256,510]"));
        }
        if self.code_offset >= self.code_range {
            return Err(invalid("CABAC code offset is outside the arithmetic range"));
        }
        Ok(())
    }

    /// Renormalizes state and commits only if all required input bits are available.
    pub fn renormalize(&mut self) -> io::Result<()> {
        if self.terminated {
            return Err(invalid("CABAC decoder is already terminated"));
        }
        if self.code_range == 0 || self.code_range > INITIAL_RANGE {
            return Err(invalid("CABAC arithmetic range is outside [1,510]"));
        }
        let mut bits = self.bits.clone();
        let mut code_range = self.code_range;
        let mut code_offset = self.code_offset;
        while code_range < 256 {
            let bit = bits
                .read_bit()
                .map_err(|error| context("renormalization", error))?;
            code_range <<= 1;
            code_offset = (code_offset << 1) | u32::from(bit);
            if code_offset >= code_range {
                return Err(invalid("CABAC code offset is outside the arithmetic range"));
            }
        }
        self.bits = bits;
        self.code_range = code_range;
        self.code_offset = code_offset;
        Ok(())
    }
}

/// Maps frame-scan luma 4x4 coefficient levels into raster order.
pub fn place_luma4x4_scan_levels(scan_levels: &[i32; 16]) -> [i32; 16] {
    const SCAN_TO_RASTER: [usize; 16] = [0, 1, 4, 8, 5, 2, 3, 6, 9, 12, 13, 10, 7, 11, 14, 15];
    let mut raster_levels = [0; 16];
    for (scan_index, raster_index) in SCAN_TO_RASTER.into_iter().enumerate() {
        raster_levels[raster_index] = scan_levels[scan_index];
    }
    raster_levels
}

/// Inserts pre-scaled chroma DC before inverse-scanning the 15 AC levels.
pub fn place_chroma4x4_scan_levels(dc_level: i32, ac_scan_levels: &[i32; 15]) -> [i32; 16] {
    let mut scan_levels = [0_i32; 16];
    scan_levels[0] = dc_level;
    scan_levels[1..].copy_from_slice(ac_scan_levels);
    place_luma4x4_scan_levels(&scan_levels)
}

fn context(field: &str, error: io::Error) -> io::Error {
    io::Error::new(error.kind(), format!("CABAC {field}: {error}"))
}

fn invalid(message: impl Into<String>) -> io::Error {
    io::Error::new(ErrorKind::InvalidData, message.into())
}

#[cfg(test)]
mod tests {
    use super::*;

    fn decoder_with_state(
        data: &[u8],
        code_range: u32,
        code_offset: u32,
    ) -> CabacArithmeticDecoder<'_> {
        CabacArithmeticDecoder {
            bits: BitReader::new(data),
            code_range,
            code_offset,
            terminated: false,
        }
    }

    #[test]
    fn decodes_bypass_bins_without_changing_range() {
        let mut zero = decoder_with_state(&[0x00], 510, 200);
        assert!(!zero.decode_bypass_bin().unwrap());
        assert_eq!((zero.code_range(), zero.code_offset()), (510, 400));

        let mut one = decoder_with_state(&[0x80], 510, 260);
        assert!(one.decode_bypass_bin().unwrap());
        assert_eq!((one.code_range(), one.code_offset()), (510, 11));
    }

    #[test]
    fn bypass_bin_matches_threshold_vectors() {
        for (code_offset, expected_bin, expected_offset) in [(254, false, 508), (255, true, 0)] {
            let mut decoder = decoder_with_state(&[0x00], 510, code_offset);
            assert_eq!(decoder.decode_bypass_bin().unwrap(), expected_bin);
            assert_eq!(
                (decoder.code_range(), decoder.code_offset()),
                (510, expected_offset)
            );
        }
    }

    #[test]
    fn decodes_regular_mps_and_lps_bins() {
        let mut mps_decoder = decoder_with_state(&[], 510, 0);
        let mut mps_model = CabacContextModel::new(0, 63, 26).unwrap();
        assert!(!mps_decoder.decode_bin(&mut mps_model).unwrap());
        assert_eq!(
            (mps_decoder.code_range(), mps_decoder.code_offset()),
            (270, 0)
        );
        assert_eq!((mps_model.state_index(), mps_model.mps()), (1, false));

        let mut lps_decoder = decoder_with_state(&[0x80], 510, 270);
        let mut lps_model = CabacContextModel::new(0, 63, 26).unwrap();
        assert!(lps_decoder.decode_bin(&mut lps_model).unwrap());
        assert_eq!(
            (lps_decoder.code_range(), lps_decoder.code_offset()),
            (480, 1)
        );
        assert_eq!((lps_model.state_index(), lps_model.mps()), (0, true));
    }

    #[test]
    fn regular_bin_uses_range_class_and_rolls_back_on_truncation() {
        let mut decoder = decoder_with_state(&[0x80], 300, 0);
        let mut model = CabacContextModel::new(0, 63, 26).unwrap();
        assert!(!decoder.decode_bin(&mut model).unwrap());
        assert_eq!((decoder.code_range(), decoder.code_offset()), (344, 1));

        for (code_range, data, expected_range, expected_offset) in [
            (300, &[0x80][..], 344, 1),
            (350, &[0x80][..], 348, 1),
            (410, &[0x00][..], 404, 0),
            (510, &[][..], 270, 0),
        ] {
            let mut class_decoder = decoder_with_state(data, code_range, 0);
            let mut class_model = CabacContextModel::new(0, 63, 26).unwrap();
            assert!(!class_decoder.decode_bin(&mut class_model).unwrap());
            assert_eq!(
                (class_decoder.code_range(), class_decoder.code_offset()),
                (expected_range, expected_offset)
            );
        }

        let mut state_ten = decoder_with_state(&[0x00], 510, 368);
        let mut state_ten_model = CabacContextModel::new(0, 53, 26).unwrap();
        assert!(state_ten.decode_bin(&mut state_ten_model).unwrap());
        assert_eq!((state_ten.code_range(), state_ten.code_offset()), (284, 0));
        assert_eq!(
            (state_ten_model.state_index(), state_ten_model.mps()),
            (8, false)
        );

        let mut truncated = decoder_with_state(&[], 510, 270);
        let mut unchanged = CabacContextModel::new(0, 63, 26).unwrap();
        assert_eq!(
            truncated.decode_bin(&mut unchanged).unwrap_err().kind(),
            ErrorKind::UnexpectedEof
        );
        assert_eq!(
            (truncated.code_range(), truncated.code_offset()),
            (510, 270)
        );
        assert_eq!((unchanged.state_index(), unchanged.mps()), (0, false));
    }

    #[test]
    fn regular_bin_matches_range_mps_boundary_vectors() {
        let mut below = decoder_with_state(&[], 510, 269);
        let mut below_model = CabacContextModel::new(0, 63, 26).unwrap();
        assert!(!below.decode_bin(&mut below_model).unwrap());
        assert_eq!((below.code_range(), below.code_offset()), (270, 269));
        assert_eq!((below_model.state_index(), below_model.mps()), (1, false));

        let mut above = decoder_with_state(&[0x00], 510, 271);
        let mut above_model = CabacContextModel::new(0, 63, 26).unwrap();
        assert!(above.decode_bin(&mut above_model).unwrap());
        assert_eq!((above.code_range(), above.code_offset()), (480, 2));
        assert_eq!((above_model.state_index(), above_model.mps()), (0, true));
    }

    #[test]
    fn range_lps_matches_standard_vectors() {
        let range_classes = [256, 320, 384, 448];
        let range_lps = [
            [
                128, 128, 128, 123, 116, 111, 105, 100, 95, 90, 85, 81, 77, 73, 69, 66, 62, 59, 56,
                53, 51, 48, 46, 43, 41, 39, 37, 35, 33, 32, 30, 29, 27, 26, 24, 23, 22, 21, 20, 19,
                18, 17, 16, 15, 14, 14, 13, 12, 12, 11, 11, 10, 10, 9, 9, 8, 8, 7, 7, 7, 6, 6, 6,
                2,
            ],
            [
                176, 167, 158, 150, 142, 135, 128, 122, 116, 110, 104, 99, 94, 89, 85, 80, 76, 72,
                69, 65, 62, 59, 56, 53, 50, 48, 45, 43, 41, 39, 37, 35, 33, 31, 30, 28, 27, 26, 24,
                23, 22, 21, 20, 19, 18, 17, 16, 15, 14, 14, 13, 12, 12, 11, 11, 10, 9, 9, 9, 8, 8,
                7, 7, 2,
            ],
            [
                208, 197, 187, 178, 169, 160, 152, 144, 137, 130, 123, 117, 111, 105, 100, 95, 90,
                86, 81, 77, 73, 69, 66, 63, 59, 56, 54, 51, 48, 46, 43, 41, 39, 37, 35, 33, 32, 30,
                29, 27, 26, 25, 23, 22, 21, 20, 19, 18, 17, 16, 15, 15, 14, 13, 12, 12, 11, 11, 10,
                10, 9, 9, 8, 2,
            ],
            [
                240, 227, 216, 205, 195, 185, 175, 166, 158, 150, 142, 135, 128, 122, 116, 110,
                104, 99, 94, 89, 85, 80, 76, 72, 69, 65, 62, 59, 56, 53, 50, 48, 45, 43, 41, 39,
                37, 35, 33, 31, 30, 28, 27, 25, 24, 23, 22, 21, 20, 19, 18, 17, 16, 15, 14, 14, 13,
                12, 12, 11, 11, 10, 9, 2,
            ],
        ];
        for (class_index, code_range) in range_classes.into_iter().enumerate() {
            for (context_state, lps_range) in range_lps[class_index].into_iter().enumerate() {
                let data = [0x00, 0x00];
                let mut decoder = decoder_with_state(&data, code_range, code_range - lps_range);
                let mut model = CabacContextModel {
                    state_index: context_state as u8,
                    value_mps: false,
                };
                assert!(decoder.decode_bin(&mut model).unwrap());

                let mut normalized_range = lps_range;
                let mut renormalization_bits = 0;
                while normalized_range < 256 {
                    normalized_range <<= 1;
                    renormalization_bits += 1;
                }
                assert_eq!(
                    (decoder.code_range(), decoder.code_offset()),
                    (normalized_range, 0)
                );
                assert_eq!(decoder.bits.read_bits(renormalization_bits).unwrap(), 0);
                assert_eq!(decoder.bits.read_bits(1).unwrap(), 0);
                assert_eq!(model.mps(), context_state == 0);
            }
        }
    }

    #[test]
    fn decodes_mb_qp_delta_and_selects_context() {
        let make_contexts = || {
            [
                CabacContextModel::new(0, 63, 26).unwrap(),
                CabacContextModel::new(0, 63, 26).unwrap(),
                CabacContextModel::new(0, 64, 26).unwrap(),
                CabacContextModel::new(0, 64, 26).unwrap(),
            ]
        };
        for (data, offset, previous_delta, expected) in [
            (&[][..], 0, 0, 0),
            (&[0x00][..], 390, 0, 1),
            (&[0x00][..], 330, 0, -1),
            (&[][..], 0, 1, 0),
        ] {
            let mut decoder = decoder_with_state(data, 510, offset);
            let mut contexts = make_contexts();
            assert_eq!(
                decoder
                    .decode_mb_qp_delta(&mut contexts, previous_delta)
                    .unwrap(),
                expected
            );
            if previous_delta != 0 {
                assert_eq!((contexts[0].state_index(), contexts[0].mps()), (0, false));
                assert_eq!(contexts[1].state_index(), 1);
            }
        }
    }

    #[test]
    fn truncated_mb_qp_delta_preserves_decoder_and_contexts() {
        let mut decoder = decoder_with_state(&[], 510, 390);
        let mut contexts = [
            CabacContextModel::new(0, 63, 26).unwrap(),
            CabacContextModel::new(0, 63, 26).unwrap(),
            CabacContextModel::new(0, 64, 26).unwrap(),
            CabacContextModel::new(0, 64, 26).unwrap(),
        ];
        assert_eq!(
            decoder
                .decode_mb_qp_delta(&mut contexts, 0)
                .unwrap_err()
                .kind(),
            ErrorKind::UnexpectedEof
        );
        assert_eq!((decoder.code_range(), decoder.code_offset()), (510, 390));
        assert_eq!((contexts[0].state_index(), contexts[0].mps()), (0, false));
        assert_eq!((contexts[2].state_index(), contexts[2].mps()), (0, true));
    }

    #[test]
    fn derives_mb_skip_flag_context_for_p_and_b_slices() {
        for (slice_type, neighbors, expected_context) in [
            (0, (false, false, false, false), 0),
            (5, (true, false, true, true), 1),
            (6, (true, false, true, false), 2),
        ] {
            let mut decoder = decoder_with_state(&[], 510, 0);
            let mut contexts = [
                CabacContextModel::new(0, 64, 26).unwrap(),
                CabacContextModel::new(0, 64, 26).unwrap(),
                CabacContextModel::new(0, 64, 26).unwrap(),
            ];
            contexts[expected_context] = CabacContextModel::new(0, 63, 26).unwrap();
            assert!(!decoder
                .decode_mb_skip_flag(
                    slice_type,
                    &mut contexts,
                    neighbors.0,
                    neighbors.1,
                    neighbors.2,
                    neighbors.3
                )
                .unwrap());
            for (index, model) in contexts.iter().enumerate() {
                assert_eq!(model.state_index(), u8::from(index == expected_context));
            }
        }
    }

    #[test]
    fn rejects_mb_skip_flag_for_non_p_b_slice_types() {
        let mut decoder = decoder_with_state(&[], 510, 0);
        let mut contexts = [CabacContextModel::new(0, 63, 26).unwrap(); 3];
        for slice_type in [2, 3, 4, 7, 8, 9, 10] {
            assert!(decoder
                .decode_mb_skip_flag(slice_type, &mut contexts, false, false, false, false)
                .is_err());
        }
    }

    #[test]
    fn decodes_i_intra_mb_type_branches() {
        let mut contexts = [CabacContextModel::new(0, 63, 26).unwrap(); 8];
        let mut intra_nxn = decoder_with_state(&[], 510, 0);
        assert_eq!(
            intra_nxn
                .decode_i_intra_mb_type(2, &mut contexts, false, false, false, false)
                .unwrap(),
            0
        );

        let mut contexts = [CabacContextModel::new(0, 63, 26).unwrap(); 8];
        contexts[0] = CabacContextModel::new(0, 64, 26).unwrap();
        let mut i16x16 = decoder_with_state(&[0x00], 510, 0);
        assert_eq!(
            i16x16
                .decode_i_intra_mb_type(7, &mut contexts, false, false, false, false)
                .unwrap(),
            1
        );

        let mut contexts = [CabacContextModel::new(0, 63, 26).unwrap(); 8];
        let mut pcm = decoder_with_state(&[0x80], 510, 509);
        assert_eq!(
            pcm.decode_i_intra_mb_type(2, &mut contexts, false, false, false, false)
                .unwrap(),
            25
        );
        assert!(pcm.terminated);
    }

    #[test]
    fn i_intra_mb_type_context_selection_and_truncation_are_transactional() {
        let mut contexts = [CabacContextModel::new(0, 64, 26).unwrap(); 8];
        contexts[2] = CabacContextModel::new(0, 63, 26).unwrap();
        let mut decoder = decoder_with_state(&[], 510, 0);
        assert_eq!(
            decoder
                .decode_i_intra_mb_type(2, &mut contexts, true, true, true, true)
                .unwrap(),
            0
        );
        assert_eq!(contexts[0].state_index(), 0);
        assert_eq!(contexts[2].state_index(), 1);

        let mut contexts = [CabacContextModel::new(0, 63, 26).unwrap(); 8];
        contexts[0] = CabacContextModel::new(0, 64, 26).unwrap();
        let mut truncated = decoder_with_state(&[], 510, 0);
        assert_eq!(
            truncated
                .decode_i_intra_mb_type(2, &mut contexts, false, false, false, false)
                .unwrap_err()
                .kind(),
            ErrorKind::UnexpectedEof
        );
        assert_eq!((truncated.code_range(), truncated.code_offset()), (510, 0));
        assert_eq!((contexts[0].state_index(), contexts[0].mps()), (0, true));
        assert!(truncated
            .decode_i_intra_mb_type(0, &mut contexts, false, false, false, false)
            .is_err());
    }

    #[test]
    fn decodes_intra_chroma_pred_mode_values() {
        for (mode, offset, first_mps, context_mps) in [
            (0, 0, false, false),
            (1, 0, true, false),
            (2, 100, true, true),
            (3, 0, true, true),
        ] {
            let mut decoder = decoder_with_state(&[0x00], 510, offset);
            let mut contexts = [CabacContextModel::new(0, 63, 26).unwrap(); 4];
            if first_mps {
                contexts[0] = CabacContextModel::new(0, 64, 26).unwrap();
            }
            if context_mps {
                contexts[3] = CabacContextModel::new(0, 64, 26).unwrap();
            }
            assert_eq!(
                decoder
                    .decode_intra_chroma_pred_mode(&mut contexts, false, false)
                    .unwrap(),
                mode
            );
        }
    }

    #[test]
    fn intra_chroma_pred_mode_derives_context_and_rolls_back_on_truncation() {
        for context_index in 0..3 {
            let mut decoder = decoder_with_state(&[], 510, 0);
            let mut contexts = [CabacContextModel::new(0, 64, 26).unwrap(); 4];
            contexts[context_index] = CabacContextModel::new(0, 63, 26).unwrap();
            assert_eq!(
                decoder
                    .decode_intra_chroma_pred_mode(
                        &mut contexts,
                        context_index > 0,
                        context_index == 2
                    )
                    .unwrap(),
                0
            );
            assert_eq!(contexts[context_index].state_index(), 1);
        }

        let mut decoder = decoder_with_state(&[], 510, 0);
        let mut contexts = [CabacContextModel::new(0, 63, 26).unwrap(); 4];
        contexts[0] = CabacContextModel::new(0, 64, 26).unwrap();
        assert_eq!(
            decoder
                .decode_intra_chroma_pred_mode(&mut contexts, false, false)
                .unwrap_err()
                .kind(),
            ErrorKind::UnexpectedEof
        );
        assert_eq!((decoder.code_range(), decoder.code_offset()), (510, 0));
        assert_eq!((contexts[0].state_index(), contexts[0].mps()), (0, true));
    }

    #[test]
    fn decodes_intra4x4_pred_mode_flag_and_remainder() {
        let mut previous = decoder_with_state(&[], 510, 0);
        let mut contexts = [
            CabacContextModel::new(0, 64, 26).unwrap(),
            CabacContextModel::new(0, 63, 26).unwrap(),
        ];
        assert_eq!(
            previous
                .decode_intra4x4_pred_mode(6, &mut contexts)
                .unwrap(),
            6
        );

        let mut remainder = decoder_with_state(&[0x00], 510, 0);
        let mut contexts = [
            CabacContextModel::new(0, 63, 26).unwrap(),
            CabacContextModel::new(0, 127, 26).unwrap(),
        ];
        assert_eq!(
            remainder
                .decode_intra4x4_pred_mode(7, &mut contexts)
                .unwrap(),
            8
        );
    }

    #[test]
    fn intra4x4_pred_mode_validates_and_rolls_back_on_truncation() {
        let mut decoder = decoder_with_state(&[], 510, 0);
        let mut contexts = [
            CabacContextModel::new(0, 63, 26).unwrap(),
            CabacContextModel::new(0, 63, 26).unwrap(),
        ];
        assert!(decoder.decode_intra4x4_pred_mode(9, &mut contexts).is_err());
        assert_eq!(
            decoder
                .decode_intra4x4_pred_mode(0, &mut contexts)
                .unwrap_err()
                .kind(),
            ErrorKind::UnexpectedEof
        );
        assert_eq!((decoder.code_range(), decoder.code_offset()), (510, 0));
        assert_eq!(
            (contexts[0].state_index(), contexts[1].state_index()),
            (0, 0)
        );
    }

    #[test]
    fn derives_transform_size_8x8_context_from_neighbors() {
        for context_index in 0..3 {
            let mut decoder = decoder_with_state(&[], 510, 0);
            let mut contexts = [CabacContextModel::new(0, 64, 26).unwrap(); 3];
            contexts[context_index] = CabacContextModel::new(0, 63, 26).unwrap();
            assert!(!decoder
                .decode_transform_size_8x8_flag(
                    &mut contexts,
                    context_index > 0,
                    context_index == 2
                )
                .unwrap());
            assert_eq!(contexts[context_index].state_index(), 1);
        }
    }

    #[test]
    fn decodes_luma_coded_block_pattern_and_reuses_contexts() {
        let mut zero = decoder_with_state(&[0x00], 510, 0);
        let mut zero_contexts = [CabacContextModel::new(0, 63, 26).unwrap(); 4];
        assert_eq!(
            zero.decode_luma_coded_block_pattern(0, 0, &mut zero_contexts)
                .unwrap(),
            0
        );
        assert_eq!(zero_contexts[3].state_index(), 4);

        let mut neighbor = decoder_with_state(&[0x00], 510, 0);
        let mut neighbor_contexts = [
            CabacContextModel::new(0, 63, 26).unwrap(),
            CabacContextModel::new(0, 64, 26).unwrap(),
            CabacContextModel::new(0, 64, 26).unwrap(),
            CabacContextModel::new(0, 64, 26).unwrap(),
        ];
        assert_eq!(
            neighbor
                .decode_luma_coded_block_pattern(2, 4, &mut neighbor_contexts)
                .unwrap(),
            6
        );
        assert_eq!(
            (
                neighbor_contexts[0].state_index(),
                neighbor_contexts[3].state_index()
            ),
            (2, 2)
        );

        let mut all_coded = decoder_with_state(&[], 510, 0);
        let mut all_contexts = [CabacContextModel::new(0, 127, 26).unwrap(); 4];
        assert_eq!(
            all_coded
                .decode_luma_coded_block_pattern(15, 15, &mut all_contexts)
                .unwrap(),
            15
        );
    }

    #[test]
    fn luma_coded_block_pattern_validation_and_truncation_are_transactional() {
        let mut decoder = decoder_with_state(&[], 510, 0);
        let mut contexts = [CabacContextModel::new(0, 63, 26).unwrap(); 4];
        assert!(decoder
            .decode_luma_coded_block_pattern(16, 0, &mut contexts)
            .is_err());
        assert_eq!(
            decoder
                .decode_luma_coded_block_pattern(0, 0, &mut contexts)
                .unwrap_err()
                .kind(),
            ErrorKind::UnexpectedEof
        );
        assert_eq!((decoder.code_range(), decoder.code_offset()), (510, 0));
        assert_eq!(contexts[3].state_index(), 0);
    }

    #[test]
    fn decodes_chroma_coded_block_pattern_values_and_contexts() {
        for (expected, first_mps, second_mps) in
            [(0, false, false), (1, true, false), (2, true, true)]
        {
            let data = if expected == 0 { &[][..] } else { &[0x00][..] };
            let mut decoder = decoder_with_state(data, 510, 0);
            let mut contexts = [CabacContextModel::new(0, 63, 26).unwrap(); 8];
            if first_mps {
                contexts[0] = CabacContextModel::new(0, 64, 26).unwrap();
            }
            if second_mps {
                contexts[4] = CabacContextModel::new(0, 64, 26).unwrap();
            }
            assert_eq!(
                decoder
                    .decode_chroma_coded_block_pattern(0, 0, &mut contexts)
                    .unwrap(),
                expected
            );
        }

        let mut first_context = decoder_with_state(&[], 510, 0);
        let mut contexts = [CabacContextModel::new(0, 64, 26).unwrap(); 8];
        contexts[3] = CabacContextModel::new(0, 63, 26).unwrap();
        assert_eq!(
            first_context
                .decode_chroma_coded_block_pattern(1, 1, &mut contexts)
                .unwrap(),
            0
        );
        assert_eq!(contexts[3].state_index(), 1);

        let mut second_context = decoder_with_state(&[0x00], 510, 0);
        let mut contexts = [CabacContextModel::new(0, 64, 26).unwrap(); 8];
        contexts[7] = CabacContextModel::new(0, 63, 26).unwrap();
        assert_eq!(
            second_context
                .decode_chroma_coded_block_pattern(2, 2, &mut contexts)
                .unwrap(),
            1
        );
        assert_eq!(contexts[7].state_index(), 1);
    }

    #[test]
    fn validates_and_rolls_back_chroma_coded_block_pattern() {
        let mut decoder = decoder_with_state(&[], 510, 0);
        let mut contexts = [CabacContextModel::new(0, 63, 26).unwrap(); 8];
        assert!(decoder
            .decode_chroma_coded_block_pattern(3, 0, &mut contexts)
            .is_err());
        contexts[0] = CabacContextModel::new(0, 64, 26).unwrap();
        assert_eq!(
            decoder
                .decode_chroma_coded_block_pattern(0, 0, &mut contexts)
                .unwrap_err()
                .kind(),
            ErrorKind::UnexpectedEof
        );
        assert_eq!((decoder.code_range(), decoder.code_offset()), (510, 0));
        assert_eq!((contexts[0].state_index(), contexts[0].mps()), (0, true));
    }

    #[test]
    fn derives_luma4x4_coded_block_flag_context_from_nonzero_counts() {
        for (left, top, context_index) in [(0, 0, 0), (1, 0, 1), (0, 16, 2), (3, 7, 3)] {
            let mut decoder = decoder_with_state(&[], 510, 0);
            let mut contexts = [CabacContextModel::new(0, 64, 26).unwrap(); 4];
            contexts[context_index] = CabacContextModel::new(0, 63, 26).unwrap();
            assert!(!decoder
                .decode_luma4x4_coded_block_flag(left, top, &mut contexts)
                .unwrap());
            assert_eq!(contexts[context_index].state_index(), 1);
        }
    }

    #[test]
    fn validates_and_rolls_back_luma4x4_coded_block_flag() {
        let mut decoder = decoder_with_state(&[], 510, 270);
        let mut contexts = [CabacContextModel::new(0, 63, 26).unwrap(); 4];
        assert!(decoder
            .decode_luma4x4_coded_block_flag(17, 0, &mut contexts)
            .is_err());
        assert_eq!(
            decoder
                .decode_luma4x4_coded_block_flag(0, 0, &mut contexts)
                .unwrap_err()
                .kind(),
            ErrorKind::UnexpectedEof
        );
        assert_eq!((decoder.code_range(), decoder.code_offset()), (510, 270));
        assert_eq!(contexts[0].state_index(), 0);
    }

    #[test]
    fn luma4x4_significance_map_implies_last_coefficient() {
        let mut decoder = decoder_with_state(&[], 510, 0);
        let mut significant = [CabacContextModel::new(0, 0, 26).unwrap(); 15];
        let mut last = [CabacContextModel::new(0, 0, 26).unwrap(); 15];
        for model in significant.iter_mut().chain(last.iter_mut()) {
            model.state_index = 62;
            model.value_mps = false;
        }
        let map = decoder
            .decode_luma4x4_significance_map(&mut significant, &mut last)
            .unwrap();
        assert!(map[..15].iter().all(|significant| !significant));
        assert!(map[15]);
    }

    #[test]
    fn luma4x4_significance_map_stops_on_last_flag() {
        let mut decoder = decoder_with_state(&[], 510, 0);
        let mut significant = [CabacContextModel::new(0, 0, 26).unwrap(); 15];
        let mut last = [CabacContextModel::new(0, 0, 26).unwrap(); 15];
        for model in significant.iter_mut().chain(last.iter_mut()) {
            model.state_index = 62;
            model.value_mps = false;
        }
        significant[0].value_mps = true;
        significant[1].value_mps = true;
        last[1].value_mps = true;
        let map = decoder
            .decode_luma4x4_significance_map(&mut significant, &mut last)
            .unwrap();
        assert!(map[0] && map[1] && !map[2] && !map[15]);
    }

    #[test]
    fn luma4x4_significance_map_truncation_is_transactional() {
        let mut decoder = decoder_with_state(&[], 510, 382);
        let mut significant = [CabacContextModel::new(0, 0, 26).unwrap(); 15];
        let mut last = [CabacContextModel::new(0, 0, 26).unwrap(); 15];
        significant[0] = CabacContextModel::new(0, 63, 26).unwrap();
        assert_eq!(
            decoder
                .decode_luma4x4_significance_map(&mut significant, &mut last)
                .unwrap_err()
                .kind(),
            ErrorKind::UnexpectedEof
        );
        assert_eq!((decoder.code_range(), decoder.code_offset()), (510, 382));
        assert_eq!(
            (significant[0].state_index(), significant[0].mps()),
            (0, false)
        );
    }

    #[test]
    fn decodes_motion_vector_difference_context_bands_and_values() {
        for (neighbor_magnitude, expected_context) in
            [(0, 0), (2, 0), (3, 1), (32, 1), (33, 2), (1_u64 << 32, 2)]
        {
            let mut decoder = decoder_with_state(&[], 510, 0);
            let mut contexts = [CabacContextModel {
                state_index: 0,
                value_mps: false,
            }; 7];
            assert_eq!(
                decoder
                    .decode_motion_vector_difference(neighbor_magnitude, &mut contexts)
                    .unwrap(),
                0
            );
            for (index, context) in contexts.iter().enumerate() {
                assert_eq!(
                    context.state_index(),
                    usize::from(index == expected_context) as u8
                );
            }
        }

        let vectors = [
            (
                0,
                [
                    CabacContextModel {
                        state_index: 62,
                        value_mps: true,
                    },
                    CabacContextModel {
                        state_index: 62,
                        value_mps: false,
                    },
                    CabacContextModel {
                        state_index: 62,
                        value_mps: false,
                    },
                    CabacContextModel {
                        state_index: 62,
                        value_mps: false,
                    },
                    CabacContextModel {
                        state_index: 62,
                        value_mps: false,
                    },
                    CabacContextModel {
                        state_index: 62,
                        value_mps: false,
                    },
                    CabacContextModel {
                        state_index: 62,
                        value_mps: false,
                    },
                ],
                1,
            ),
            (
                253,
                [
                    CabacContextModel {
                        state_index: 62,
                        value_mps: true,
                    },
                    CabacContextModel {
                        state_index: 62,
                        value_mps: false,
                    },
                    CabacContextModel {
                        state_index: 62,
                        value_mps: false,
                    },
                    CabacContextModel {
                        state_index: 62,
                        value_mps: false,
                    },
                    CabacContextModel {
                        state_index: 62,
                        value_mps: false,
                    },
                    CabacContextModel {
                        state_index: 62,
                        value_mps: false,
                    },
                    CabacContextModel {
                        state_index: 62,
                        value_mps: false,
                    },
                ],
                -1,
            ),
            (
                0,
                [CabacContextModel {
                    state_index: 62,
                    value_mps: true,
                }; 7],
                9,
            ),
            (
                31,
                [CabacContextModel {
                    state_index: 62,
                    value_mps: true,
                }; 7],
                10,
            ),
        ];
        for (code_offset, mut contexts, expected) in vectors {
            let data = if expected == 10 { [0x08] } else { [0x00] };
            let mut decoder = decoder_with_state(&data, 510, code_offset);
            assert_eq!(
                decoder
                    .decode_motion_vector_difference(0, &mut contexts)
                    .unwrap(),
                expected
            );
        }
    }

    #[test]
    fn truncated_motion_vector_difference_preserves_decoder_and_contexts() {
        let mut decoder = decoder_with_state(&[], 510, 509);
        let mut contexts = [CabacContextModel {
            state_index: 62,
            value_mps: true,
        }; 7];
        assert_eq!(
            decoder
                .decode_motion_vector_difference(0, &mut contexts)
                .unwrap_err()
                .kind(),
            ErrorKind::UnexpectedEof
        );
        assert_eq!((decoder.code_range(), decoder.code_offset()), (510, 509));
        assert_eq!((contexts[0].state_index(), contexts[0].mps()), (62, true));
    }

    #[test]
    fn overlong_motion_vector_difference_escape_is_transactional() {
        let data = [0xff; 6];
        let mut decoder = decoder_with_state(&data, 510, 491);
        let mut contexts = [CabacContextModel {
            state_index: 63,
            value_mps: true,
        }; 7];
        assert!(decoder
            .decode_motion_vector_difference(0, &mut contexts)
            .unwrap_err()
            .to_string()
            .contains("MVD exceeds the signed output range"));
        assert_eq!((decoder.code_range(), decoder.code_offset()), (510, 491));
        assert!(contexts
            .iter()
            .all(|context| context.state_index() == 63 && context.mps()));
    }

    #[test]
    fn decodes_coeff_abs_level_minus1_zero_unit_and_escape() {
        let mut zero_decoder = decoder_with_state(&[], 510, 0);
        let mut zero_first = CabacContextModel::new(0, 0, 26).unwrap();
        let mut zero_greater = CabacContextModel::new(0, 0, 26).unwrap();
        assert_eq!(
            zero_decoder
                .decode_coeff_abs_level_minus1(&mut zero_first, &mut zero_greater)
                .unwrap(),
            0
        );

        let mut unit_decoder = decoder_with_state(&[], 510, 0);
        let mut unit_first = CabacContextModel::new(0, 64, 26).unwrap();
        let mut unit_greater = CabacContextModel::new(0, 0, 26).unwrap();
        assert_eq!(
            unit_decoder
                .decode_coeff_abs_level_minus1(&mut unit_first, &mut unit_greater)
                .unwrap(),
            1
        );

        let mut escape_decoder = decoder_with_state(&[0x40], 510, 0);
        let mut escape_first = CabacContextModel {
            state_index: 62,
            value_mps: true,
        };
        let mut escape_greater = escape_first;
        assert_eq!(
            escape_decoder
                .decode_coeff_abs_level_minus1(&mut escape_first, &mut escape_greater)
                .unwrap(),
            14
        );
    }

    #[test]
    fn truncated_coeff_abs_level_minus1_preserves_decoder_and_contexts() {
        let mut decoder = decoder_with_state(&[], 510, 390);
        let mut first = CabacContextModel {
            state_index: 62,
            value_mps: true,
        };
        let mut greater = first;
        assert_eq!(
            decoder
                .decode_coeff_abs_level_minus1(&mut first, &mut greater)
                .unwrap_err()
                .kind(),
            ErrorKind::UnexpectedEof
        );
        assert_eq!((decoder.code_range(), decoder.code_offset()), (510, 390));
        assert_eq!((first.state_index(), first.mps()), (62, true));
        assert_eq!((greater.state_index(), greater.mps()), (62, true));
    }

    #[test]
    fn rejects_overlong_coeff_escape_prefix_transactionally() {
        let mut decoder = decoder_with_state(&[0xff, 0xff, 0xff, 0xff], 510, 481);
        let mut first = CabacContextModel {
            state_index: 63,
            value_mps: true,
        };
        let mut greater = first;
        let error = decoder
            .decode_coeff_abs_level_minus1(&mut first, &mut greater)
            .unwrap_err();
        assert_eq!(error.kind(), ErrorKind::InvalidData);
        assert!(error
            .to_string()
            .contains("exceeds supported bypass prefix"));
        assert_eq!((decoder.code_range(), decoder.code_offset()), (510, 481));
        assert_eq!(first.state_index(), 63);
        assert!(first.mps());
        assert_eq!(greater.state_index(), 63);
        assert!(greater.mps());
    }

    #[test]
    fn coeff_abs_level_minus1_matches_standard_escape_vectors() {
        for (code_offset, data, expected, expected_offset) in
            [(300, 0x00, 15, 472), (302, 0x20, 16, 7)]
        {
            let input = [data];
            let mut decoder = decoder_with_state(&input, 510, code_offset);
            let mut first = CabacContextModel {
                state_index: 63,
                value_mps: true,
            };
            let mut greater = first;
            assert_eq!(
                decoder
                    .decode_coeff_abs_level_minus1(&mut first, &mut greater)
                    .unwrap(),
                expected
            );
            assert_eq!(
                (decoder.code_range(), decoder.code_offset()),
                (482, expected_offset)
            );
        }
    }

    #[test]
    fn decodes_coeff_sign_and_rolls_back_on_truncation() {
        let mut positive = decoder_with_state(&[0x00], 510, 0);
        assert_eq!(positive.decode_coeff_sign(4).unwrap(), 5);

        let mut negative = decoder_with_state(&[0x00], 510, 255);
        assert_eq!(negative.decode_coeff_sign(4).unwrap(), -5);

        let mut max_positive = decoder_with_state(&[0x00], 510, 0);
        assert_eq!(
            max_positive.decode_coeff_sign(i32::MAX as u32 - 1).unwrap(),
            i32::MAX
        );

        let mut max_negative = decoder_with_state(&[0x00], 510, 255);
        assert_eq!(
            max_negative.decode_coeff_sign(i32::MAX as u32 - 1).unwrap(),
            -i32::MAX
        );

        let mut overflow = decoder_with_state(&[], 510, 0);
        assert_eq!(
            overflow
                .decode_coeff_sign(i32::MAX as u32)
                .unwrap_err()
                .kind(),
            ErrorKind::InvalidData
        );
        assert_eq!((overflow.code_range(), overflow.code_offset()), (510, 0));

        let mut truncated = decoder_with_state(&[], 510, 0);
        assert_eq!(
            truncated.decode_coeff_sign(0).unwrap_err().kind(),
            ErrorKind::UnexpectedEof
        );
        assert_eq!((truncated.code_range(), truncated.code_offset()), (510, 0));
    }

    #[test]
    fn decodes_luma4x4_residual_levels_in_scan_order() {
        let mut decoder = decoder_with_state(&[0x00], 510, 0);
        let mut significance = [false; 16];
        significance[2] = true;
        let mut contexts = [CabacContextModel::new(0, 63, 26).unwrap(); 10];
        let levels = decoder
            .decode_luma4x4_residual_levels(&significance, &mut contexts)
            .unwrap();
        assert_eq!(levels[2], 1);
        assert_eq!(levels.iter().filter(|&&level| level != 0).count(), 1);

        let mut negative = decoder_with_state(&[0x00], 510, 255);
        let mut significance = [false; 16];
        significance[15] = true;
        let mut contexts = [CabacContextModel::new(0, 63, 26).unwrap(); 10];
        assert_eq!(
            negative
                .decode_luma4x4_residual_levels(&significance, &mut contexts)
                .unwrap()[15],
            -1
        );
    }

    #[test]
    fn places_luma4x4_scan_levels_in_raster_order() {
        let scan_levels = [0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15];
        assert_eq!(
            place_luma4x4_scan_levels(&scan_levels),
            [0, 1, 5, 6, 2, 4, 7, 12, 3, 8, 11, 13, 9, 10, 14, 15]
        );
    }

    #[test]
    fn places_chroma4x4_dc_and_ac_scan_levels_in_raster_order() {
        let ac_scan_levels = [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15];
        assert_eq!(
            place_chroma4x4_scan_levels(100, &ac_scan_levels),
            [100, 1, 5, 6, 2, 4, 7, 12, 3, 8, 11, 13, 9, 10, 14, 15]
        );
    }

    #[test]
    fn decodes_places_and_rolls_back_luma4x4_residual_block() {
        let make_significant = || {
            let mut contexts = [CabacContextModel {
                state_index: 63,
                value_mps: false,
            }; 15];
            contexts[2].value_mps = true;
            contexts
        };
        let make_last = make_significant;
        let coefficients = [CabacContextModel {
            state_index: 63,
            value_mps: false,
        }; 10];

        let mut decoder = decoder_with_state(&[0x00], 510, 0);
        let mut significant = make_significant();
        let mut last = make_last();
        let mut coefficient_contexts = coefficients;
        assert_eq!(
            decoder
                .decode_luma4x4_residual_block(
                    &mut significant,
                    &mut last,
                    &mut coefficient_contexts,
                )
                .unwrap(),
            [0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0]
        );

        let mut truncated = decoder_with_state(&[], 510, 0);
        let mut significant = make_significant();
        let mut last = make_last();
        for context in significant.iter_mut().chain(last.iter_mut()) {
            context.state_index = 61;
        }
        let mut coefficient_contexts = coefficients;
        for context in &mut coefficient_contexts {
            context.state_index = 61;
        }
        assert_eq!(
            truncated
                .decode_luma4x4_residual_block(
                    &mut significant,
                    &mut last,
                    &mut coefficient_contexts,
                )
                .unwrap_err()
                .kind(),
            ErrorKind::UnexpectedEof
        );
        assert_eq!((truncated.code_range(), truncated.code_offset()), (510, 0));
        assert_eq!(significant[0].state_index(), 61);
        assert_eq!(significant[2].state_index(), 61);
        assert_eq!(last[2].state_index(), 61);
        assert_eq!(coefficient_contexts[1].state_index(), 61);
    }

    #[test]
    fn residual_level_block_validation_and_rollback() {
        let mut decoder = decoder_with_state(&[], 510, 390);
        let mut significance = [false; 16];
        significance[0] = true;
        let mut contexts = [CabacContextModel::new(0, 64, 26).unwrap(); 10];
        assert_eq!(
            decoder
                .decode_luma4x4_residual_levels(&significance, &mut contexts)
                .unwrap_err()
                .kind(),
            ErrorKind::UnexpectedEof
        );
        assert_eq!((decoder.code_range(), decoder.code_offset()), (510, 390));
        assert!(contexts
            .iter()
            .all(|model| (model.state_index(), model.mps()) == (0, true)));

        assert!(decoder
            .decode_luma4x4_residual_levels(&[false; 16], &mut contexts)
            .is_err());
    }

    #[test]
    fn truncated_bypass_bin_preserves_state_and_input() {
        let mut decoder = decoder_with_state(&[], 510, 200);
        assert_eq!(
            decoder.decode_bypass_bin().unwrap_err().kind(),
            ErrorKind::UnexpectedEof
        );
        assert_eq!((decoder.code_range(), decoder.code_offset()), (510, 200));
    }

    #[test]
    fn terminate_bin_stops_or_renormalizes() {
        let mut terminated = decoder_with_state(&[], 300, 299);
        assert!(terminated.decode_terminate_bin().unwrap());
        assert_eq!(terminated.code_range(), 298);
        assert!(terminated.decode_bypass_bin().is_err());
        let mut model = CabacContextModel {
            state_index: 10,
            value_mps: true,
        };
        assert!(terminated.decode_bin(&mut model).is_err());
        assert!(terminated.decode_terminate_bin().is_err());
        assert_eq!(
            (terminated.code_range(), terminated.code_offset()),
            (298, 299)
        );
        assert_eq!((model.state_index(), model.mps()), (10, true));

        let mut continuing = decoder_with_state(&[0x80], 257, 0);
        assert!(!continuing.decode_terminate_bin().unwrap());
        assert_eq!(
            (continuing.code_range(), continuing.code_offset()),
            (510, 1)
        );
        assert!(!continuing.bits.read_bit().unwrap());
    }

    #[test]
    fn terminate_bin_matches_threshold_vectors() {
        let mut below = decoder_with_state(&[], 300, 297);
        assert!(!below.decode_terminate_bin().unwrap());
        assert_eq!((below.code_range(), below.code_offset()), (298, 297));

        let mut at = decoder_with_state(&[], 300, 298);
        assert!(at.decode_terminate_bin().unwrap());
        assert_eq!((at.code_range(), at.code_offset()), (298, 298));
    }

    #[test]
    fn truncated_terminate_bin_preserves_state_and_input() {
        let mut decoder = decoder_with_state(&[], 257, 0);
        assert_eq!(
            decoder.decode_terminate_bin().unwrap_err().kind(),
            ErrorKind::UnexpectedEof
        );
        assert_eq!((decoder.code_range(), decoder.code_offset()), (257, 0));
    }

    #[test]
    fn initializes_context_state_and_mps_with_clipping() {
        for (n, expected_state, expected_mps) in [
            (63, 0, false),
            (64, 0, true),
            (0, 62, false),
            (127, 62, true),
        ] {
            let model = CabacContextModel::new(0, n, 26).unwrap();
            assert_eq!(model.state_index(), expected_state);
            assert_eq!(model.mps(), expected_mps);
        }
        assert!(CabacContextModel::new(128, 0, 26).is_err());
        assert!(CabacContextModel::new(0, 0, 52).is_err());

        for (m, n, slice_qpy, expected_state, expected_mps) in [
            (1, 63, 15, 0, false),
            (1, 63, 16, 0, true),
            (-1, 64, 15, 0, false),
            (2, 0, 51, 57, false),
            (-2, 64, 51, 6, false),
            (-128, -128, 0, 62, false),
            (127, 127, 51, 62, true),
        ] {
            let model = CabacContextModel::new(m, n, slice_qpy).unwrap();
            assert_eq!(
                (model.state_index(), model.mps()),
                (expected_state, expected_mps)
            );
        }
        for (m, n, slice_qpy) in [
            (-129, 0, 26),
            (128, 0, 26),
            (0, -129, 26),
            (0, 128, 26),
            (0, 0, -1),
            (0, 0, 52),
        ] {
            assert!(CabacContextModel::new(m, n, slice_qpy).is_err());
        }
    }

    #[test]
    fn updates_context_and_saturates_mps_state() {
        let mut model = CabacContextModel::new(0, 64, 26).unwrap();
        model.update(false);
        assert_eq!((model.state_index(), model.mps()), (0, false));
        model.update(false);
        assert_eq!((model.state_index(), model.mps()), (1, false));

        let mut lps = CabacContextModel::new(0, 53, 26).unwrap();
        lps.update(true);
        assert_eq!((lps.state_index(), lps.mps()), (8, false));

        let mut saturated = CabacContextModel::new(0, 0, 26).unwrap();
        saturated.update(false);
        assert_eq!((saturated.state_index(), saturated.mps()), (62, false));
    }

    #[test]
    fn context_transitions_match_standard_vectors() {
        let lps_transitions = [
            0, 0, 1, 2, 2, 4, 4, 5, 6, 7, 8, 9, 9, 11, 11, 12, 13, 13, 15, 15, 16, 16, 18, 18, 19,
            19, 21, 21, 22, 22, 23, 24, 24, 25, 26, 26, 27, 27, 28, 29, 29, 30, 30, 30, 31, 32, 32,
            33, 33, 33, 34, 34, 35, 35, 35, 36, 36, 37, 37, 37, 38, 38, 63, 63,
        ];
        for (state_index, expected_state) in lps_transitions.into_iter().enumerate() {
            let mut model = CabacContextModel {
                state_index: state_index as u8,
                value_mps: false,
            };
            model.update(true);
            assert_eq!(model.state_index(), expected_state);
            assert_eq!(model.mps(), state_index == 0);
        }

        for state_index in 0..64u8 {
            let mut model = CabacContextModel {
                state_index,
                value_mps: false,
            };
            model.update(false);
            let expected_state = if state_index < 62 {
                state_index + 1
            } else {
                state_index
            };
            assert_eq!(model.state_index(), expected_state);
            assert!(!model.mps());
        }
    }

    #[test]
    fn initializes_range_and_offset_from_nine_bits() {
        let decoder = CabacArithmeticDecoder::new(&[0b0101_0101, 0b1000_0000]).unwrap();
        assert_eq!(decoder.code_range(), 510);
        assert_eq!(decoder.code_offset(), 171);
        for (data, expected_offset) in [([0x00, 0x00], 0), ([0xfe, 0x80], 509)] {
            let mut endpoint = CabacArithmeticDecoder::new(&data).unwrap();
            assert_eq!(
                (endpoint.code_range(), endpoint.code_offset()),
                (510, expected_offset)
            );
            assert_eq!(endpoint.bits.read_bits(7).unwrap(), 0);
        }
        assert_eq!(
            CabacArithmeticDecoder::new(&[0xff, 0x80])
                .err()
                .unwrap()
                .kind(),
            ErrorKind::InvalidData
        );
        assert_eq!(
            CabacArithmeticDecoder::new(&[0xff, 0x00])
                .err()
                .unwrap()
                .kind(),
            ErrorKind::InvalidData
        );
        assert_eq!(
            CabacArithmeticDecoder::new(&[0xff]).err().unwrap().kind(),
            ErrorKind::UnexpectedEof
        );
    }

    #[test]
    fn renormalizes_range_and_offset() {
        let data = [0, 0b0101_0000];
        let mut decoder = CabacArithmeticDecoder::new(&data).unwrap();
        decoder.code_range = 100;
        decoder.code_offset = 40;
        decoder.renormalize().unwrap();
        assert_eq!(decoder.code_range(), 400);
        assert_eq!(decoder.code_offset(), 162);
        assert!(decoder.bits.read_bit().unwrap());
    }

    #[test]
    fn failed_renormalization_preserves_state_and_input() {
        let data = [0, 0];
        let mut decoder = CabacArithmeticDecoder::new(&data).unwrap();
        decoder.code_range = 1;
        decoder.code_offset = 0;
        assert_eq!(
            decoder.renormalize().unwrap_err().kind(),
            ErrorKind::UnexpectedEof
        );
        assert_eq!(decoder.code_range(), 1);
        assert_eq!(decoder.code_offset(), 0);
        assert_eq!(decoder.bits.read_bits(7).unwrap(), 0);
        assert_eq!(
            decoder.bits.read_bits(1).unwrap_err().kind(),
            ErrorKind::UnexpectedEof
        );
    }

    #[test]
    fn rejects_invalid_range() {
        let mut decoder = CabacArithmeticDecoder::new(&[0, 0]).unwrap();
        decoder.code_range = 0;
        assert_eq!(
            decoder.renormalize().unwrap_err().kind(),
            ErrorKind::InvalidData
        );
    }
}
