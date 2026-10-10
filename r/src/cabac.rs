use std::io::{self, ErrorKind};

use crate::cabac_init::CONTEXT_INIT_TABLE;
use crate::BitReader;

const INITIAL_RANGE: u32 = 510;
const MAX_QPY: u32 = 51;
const MAX_MOTION_VECTOR_DIFFERENCE: u32 = i32::MAX as u32;
const MAX_COEFF_LEVEL_PREFIX: u32 = 23;
const COEFF_ABS_LEVEL1_CONTEXT: [usize; 8] = [1, 2, 3, 4, 0, 0, 0, 0];
const COEFF_ABS_LEVEL_GREATER1_CONTEXT: [usize; 8] = [5, 5, 5, 5, 6, 7, 8, 9];
const COEFF_LEVEL1_TRANSITION: [usize; 8] = [1, 2, 3, 3, 4, 5, 6, 7];
const COEFF_LEVEL_GREATER1_TRANSITION: [usize; 8] = [4, 4, 4, 4, 5, 6, 7, 7];
const I_INTRA_MB_TYPE_INIT: [(i32, i32); 8] = [
    (20, -15),
    (2, 54),
    (3, 74),
    (-28, 127),
    (-23, 104),
    (-6, 53),
    (-1, 54),
    (7, 51),
];
const P_INTER_MB_TYPE_INIT: [[(i32, i32); 4]; 3] = [
    [(1, 9), (0, 49), (-37, 118), (5, 57)],
    [(-2, 9), (4, 41), (-29, 118), (2, 65)],
    [(-10, 51), (-3, 62), (-27, 99), (26, 16)],
];
const INTER_MVD_INIT: [[(i32, i32); 14]; 3] = [
    [
        (-3, 69),
        (-6, 81),
        (-11, 96),
        (6, 55),
        (7, 67),
        (-5, 86),
        (2, 88),
        (0, 58),
        (-3, 76),
        (-10, 94),
        (5, 54),
        (4, 69),
        (-3, 81),
        (0, 88),
    ],
    [
        (-2, 69),
        (-5, 82),
        (-10, 96),
        (2, 59),
        (2, 75),
        (-3, 87),
        (-3, 100),
        (1, 56),
        (-3, 74),
        (-6, 85),
        (0, 59),
        (-3, 81),
        (-7, 86),
        (-5, 95),
    ],
    [
        (-11, 89),
        (-15, 103),
        (-21, 116),
        (19, 57),
        (20, 58),
        (4, 84),
        (6, 96),
        (1, 63),
        (-5, 85),
        (-13, 106),
        (5, 63),
        (6, 75),
        (-3, 90),
        (-1, 101),
    ],
];
const INTER_REF_IDX_INIT: [[(i32, i32); 6]; 3] = [
    [(-7, 67), (-5, 74), (-4, 74), (-5, 80), (-7, 72), (1, 58)],
    [(-1, 66), (-1, 77), (1, 70), (-2, 86), (-5, 72), (0, 61)],
    [(3, 55), (-4, 79), (-2, 75), (-12, 97), (-7, 50), (1, 60)],
];
const I_MB_QP_DELTA_INIT: [(i32, i32); 4] = [(0, 41), (0, 63), (0, 63), (0, 63)];
const I_INTRA_CHROMA_PRED_MODE_INIT: [(i32, i32); 4] = [(-9, 83), (4, 86), (0, 97), (-7, 72)];
const I_INTRA4X4_PRED_MODE_INIT: [(i32, i32); 2] = [(13, 41), (3, 62)];
const I_TRANSFORM_SIZE_8X8_INIT: [(i32, i32); 3] = [(31, 21), (31, 31), (25, 50)];
const I_LUMA_CODED_BLOCK_PATTERN_INIT: [(i32, i32); 4] =
    [(-17, 127), (-13, 102), (0, 82), (-7, 74)];
const I_CHROMA_CODED_BLOCK_PATTERN_INIT: [(i32, i32); 8] = [
    (-21, 107),
    (-27, 127),
    (-31, 127),
    (-24, 127),
    (-18, 95),
    (-27, 127),
    (-21, 114),
    (-30, 127),
];
const I_LUMA4X4_CODED_BLOCK_FLAG_INIT: [(i32, i32); 4] =
    [(-3, 70), (-8, 93), (-10, 90), (-30, 127)];
const LUMA4X4_BLOCK_SCAN_TO_RASTER: [usize; 16] =
    [0, 1, 4, 5, 2, 3, 6, 7, 8, 9, 12, 13, 10, 11, 14, 15];
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

/// Parsed neighbour facts used to derive per-partition ref_idx and MVD CABAC context bands.
#[derive(Clone, Copy, Debug, Default, Eq, PartialEq)]
pub struct CabacInterNeighbor {
    pub available: bool,
    pub skip: bool,
    pub intra: bool,
    pub prediction_mode_matches: bool,
    pub reference_index: u8,
    pub motion_vector_difference: [i32; 2],
    pub is_field: bool,
}

/// External top/left edge CBF state needed by Intra_4x4 coded_block_flag contexts.
#[derive(Clone, Copy, Debug, Default, Eq, PartialEq)]
pub struct CabacIntra4x4EdgeState {
    pub available: bool,
    pub is_ipcm: bool,
    pub transform_block_available: [bool; 4],
    pub transform_block_coded: [bool; 4],
}

/// Decoded syntax and reconstructed luma samples for one Intra_NxN 4x4 macroblock.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Intra4x4LumaMacroblockResult {
    pub modes: [u8; 16],
    pub coded_block_flags: [bool; 16],
    pub residuals: [[i64; 16]; 16],
    pub samples: [u8; 256],
}

impl Default for Intra4x4LumaMacroblockResult {
    fn default() -> Self {
        Self {
            modes: [0; 16],
            coded_block_flags: [false; 16],
            residuals: [[0; 16]; 16],
            samples: [0; 256],
        }
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Intra8x8LumaMacroblockResult {
    pub transform_size_8x8: bool,
    pub modes: [u8; 4],
    pub residuals: [[i64; 64]; 4],
    pub samples: [u8; 256],
}

#[derive(Clone, Copy, Debug, Default, Eq, PartialEq)]
pub struct CabacIntra16x16EdgeState {
    pub available: bool,
    pub is_ipcm: bool,
    pub dc_transform_block_available: bool,
    pub dc_transform_block_coded: bool,
    pub ac_transform_block_available: [bool; 4],
    pub ac_transform_block_coded: [bool; 4],
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Intra16x16LumaMacroblockResult {
    pub prediction_mode: u8,
    pub coded_block_pattern_luma: u8,
    pub dc_coded: bool,
    pub dc_levels: [i64; 16],
    pub ac_coded_block_flags: [bool; 16],
    pub residual: [i64; 256],
    pub samples: [u8; 256],
}

impl Default for Intra16x16LumaMacroblockResult {
    fn default() -> Self {
        Self {
            prediction_mode: 0,
            coded_block_pattern_luma: 0,
            dc_coded: false,
            dc_levels: [0; 16],
            ac_coded_block_flags: [false; 16],
            residual: [0; 256],
            samples: [0; 256],
        }
    }
}

#[derive(Clone, Copy, Debug, Default, Eq, PartialEq)]
pub struct CabacChroma420References {
    pub top_available: bool,
    pub left_available: bool,
    pub top_left_available: bool,
    pub top: [u8; 8],
    pub left: [u8; 8],
    pub top_left: u8,
}

#[derive(Clone, Copy, Debug, Default, Eq, PartialEq)]
pub struct CabacChroma420EdgeState {
    pub available: bool,
    pub is_ipcm: bool,
    pub dc_block_available: bool,
    pub dc_block_coded: bool,
    pub ac_block_available: [bool; 2],
    pub ac_block_coded: [bool; 2],
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct IntraChroma420MacroblockResult {
    pub prediction_mode: u8,
    pub qpc: [i32; 2],
    pub dc_coded: [bool; 2],
    pub ac_coded_block_flags: [[bool; 4]; 2],
    pub cb: [u8; 64],
    pub cr: [u8; 64],
    pub cb_residual: [i64; 64],
    pub cr_residual: [i64; 64],
}

#[derive(Clone, Copy, Debug)]
pub struct CabacIIntraMacroblockInput {
    pub slice_type: u8,
    pub left_available: bool,
    pub left_intra16_or_pcm: bool,
    pub top_available: bool,
    pub top_intra16_or_pcm: bool,
    pub previous_qpy: i32,
    pub previous_qp_delta: i32,
    pub left_luma_cbp: u8,
    pub top_luma_cbp: u8,
    pub left_chroma_cbp: u8,
    pub top_chroma_cbp: u8,
    pub transform_8x8_mode_enabled: bool,
    pub left_has_8x8_transform: bool,
    pub top_has_8x8_transform: bool,
    pub top_modes_4x4: [u8; 4],
    pub left_modes_4x4: [u8; 4],
    pub top_modes_8x8: [u8; 2],
    pub left_modes_8x8: [u8; 2],
    pub top_mode_available: bool,
    pub left_mode_available: bool,
    pub luma_4x4_top_edge: CabacIntra4x4EdgeState,
    pub luma_4x4_left_edge: CabacIntra4x4EdgeState,
    pub luma_16x16_top_edge: CabacIntra16x16EdgeState,
    pub luma_16x16_left_edge: CabacIntra16x16EdgeState,
    pub luma_4x4_blocks: [crate::LumaIntra4x4Block; 16],
    pub luma_8x8_blocks: [crate::LumaIntra8x8Block; 4],
    pub intra16x16_top: [u8; 16],
    pub intra16x16_left: [u8; 16],
    pub intra16x16_top_available: bool,
    pub intra16x16_left_available: bool,
    pub intra16x16_top_left: u8,
    pub intra16x16_top_left_available: bool,
    pub chroma_left_mode_nonzero: bool,
    pub chroma_top_mode_nonzero: bool,
    pub chroma_references: [CabacChroma420References; 2],
    pub chroma_left_edges: [CabacChroma420EdgeState; 2],
    pub chroma_top_edges: [CabacChroma420EdgeState; 2],
    pub luma_4x4_scaling_list: [u8; 16],
    pub luma_8x8_scaling_list: [u8; 64],
    pub chroma_scaling_lists: [[u8; 16]; 2],
    pub chroma_qp_index_offsets: [i32; 2],
}

impl Default for CabacIIntraMacroblockInput {
    fn default() -> Self {
        Self {
            slice_type: 2,
            left_available: false,
            left_intra16_or_pcm: false,
            top_available: false,
            top_intra16_or_pcm: false,
            previous_qpy: 26,
            previous_qp_delta: 0,
            left_luma_cbp: 0,
            top_luma_cbp: 0,
            left_chroma_cbp: 0,
            top_chroma_cbp: 0,
            transform_8x8_mode_enabled: false,
            left_has_8x8_transform: false,
            top_has_8x8_transform: false,
            top_modes_4x4: [0; 4],
            left_modes_4x4: [0; 4],
            top_modes_8x8: [0; 2],
            left_modes_8x8: [0; 2],
            top_mode_available: false,
            left_mode_available: false,
            luma_4x4_top_edge: CabacIntra4x4EdgeState::default(),
            luma_4x4_left_edge: CabacIntra4x4EdgeState::default(),
            luma_16x16_top_edge: CabacIntra16x16EdgeState::default(),
            luma_16x16_left_edge: CabacIntra16x16EdgeState::default(),
            luma_4x4_blocks: [crate::LumaIntra4x4Block::default(); 16],
            luma_8x8_blocks: [crate::LumaIntra8x8Block::default(); 4],
            intra16x16_top: [0; 16],
            intra16x16_left: [0; 16],
            intra16x16_top_available: false,
            intra16x16_left_available: false,
            intra16x16_top_left: 0,
            intra16x16_top_left_available: false,
            chroma_left_mode_nonzero: false,
            chroma_top_mode_nonzero: false,
            chroma_references: [CabacChroma420References::default(); 2],
            chroma_left_edges: [CabacChroma420EdgeState::default(); 2],
            chroma_top_edges: [CabacChroma420EdgeState::default(); 2],
            luma_4x4_scaling_list: [16; 16],
            luma_8x8_scaling_list: [16; 64],
            chroma_scaling_lists: [[16; 16]; 2],
            chroma_qp_index_offsets: [0; 2],
        }
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct CabacIIntraMacroblockResult {
    pub macroblock_type: u8,
    pub coded_block_pattern_luma: u8,
    pub coded_block_pattern_chroma: u8,
    pub transform_size_8x8: bool,
    pub intra16x16_luma_mode: u8,
    pub chroma_prediction_mode: u8,
    pub luma_4x4_modes: [u8; 16],
    pub luma_8x8_modes: [u8; 4],
    pub qp_delta: i32,
    pub qpy: i32,
    pub luma: [u8; 256],
    pub cb: [u8; 64],
    pub cr: [u8; 64],
    pub luma_4x4_coded_block_flags: [bool; 16],
    pub chroma_dc_coded: [bool; 2],
    pub chroma_ac_coded_flags: [[bool; 4]; 2],
}

impl Default for CabacIIntraMacroblockResult {
    fn default() -> Self {
        Self {
            macroblock_type: 0,
            coded_block_pattern_luma: 0,
            coded_block_pattern_chroma: 0,
            transform_size_8x8: false,
            intra16x16_luma_mode: 0,
            chroma_prediction_mode: 0,
            luma_4x4_modes: [0; 16],
            luma_8x8_modes: [0; 4],
            qp_delta: 0,
            qpy: 26,
            luma: [0; 256],
            cb: [0; 64],
            cr: [0; 64],
            luma_4x4_coded_block_flags: [false; 16],
            chroma_dc_coded: [false; 2],
            chroma_ac_coded_flags: [[false; 4]; 2],
        }
    }
}

impl Default for IntraChroma420MacroblockResult {
    fn default() -> Self {
        Self {
            prediction_mode: 0,
            qpc: [0; 2],
            dc_coded: [false; 2],
            ac_coded_block_flags: [[false; 4]; 2],
            cb: [0; 64],
            cr: [0; 64],
            cb_residual: [0; 64],
            cr_residual: [0; 64],
        }
    }
}

impl Default for Intra8x8LumaMacroblockResult {
    fn default() -> Self {
        Self {
            transform_size_8x8: false,
            modes: [0; 4],
            residuals: [[0; 64]; 4],
            samples: [0; 256],
        }
    }
}

#[rustfmt::skip]
const LUMA8X8_SCAN_TO_RASTER: [usize; 64] = [
    0, 1, 8, 16, 9, 2, 3, 10, 17, 24, 32, 25, 18, 11, 4, 5,
    12, 19, 26, 33, 40, 48, 41, 34, 27, 20, 13, 6, 7, 14, 21, 28,
    35, 42, 49, 56, 57, 50, 43, 36, 29, 22, 15, 23, 30, 37, 44, 51,
    58, 59, 52, 45, 38, 31, 39, 46, 53, 60, 61, 54, 47, 55, 62, 63,
];

fn derive_intra4x4_luma_cond_term(
    block_index: usize,
    block_x: usize,
    block_y: usize,
    is_left: bool,
    coded_flags: &[bool; 16],
    edge: CabacIntra4x4EdgeState,
) -> bool {
    let neighbor_coordinate = if is_left { block_x } else { block_y };
    let edge_coordinate = if is_left { block_y } else { block_x };
    if neighbor_coordinate > 0 {
        let raster = LUMA4X4_BLOCK_SCAN_TO_RASTER[block_index];
        let neighbor_raster = raster - if is_left { 1 } else { 4 };
        let neighbor_index = LUMA4X4_BLOCK_SCAN_TO_RASTER
            .iter()
            .position(|candidate| *candidate == neighbor_raster)
            .expect("valid intra4x4 raster neighbour");
        return derive_coded_block_flag_cond_term(
            true,
            true,
            false,
            true,
            coded_flags[neighbor_index],
        );
    }
    if !edge.available || edge.is_ipcm {
        return true;
    }
    derive_coded_block_flag_cond_term(
        true,
        true,
        false,
        edge.transform_block_available[edge_coordinate],
        edge.transform_block_coded[edge_coordinate],
    )
}

fn derive_intra16x16_ac_cond_term(
    block_index: usize,
    block_x: usize,
    block_y: usize,
    is_left: bool,
    coded_flags: &[bool; 16],
    edge: CabacIntra16x16EdgeState,
) -> bool {
    let neighbor_coordinate = if is_left { block_x } else { block_y };
    let edge_coordinate = if is_left { block_y } else { block_x };
    if neighbor_coordinate > 0 {
        let raster = LUMA4X4_BLOCK_SCAN_TO_RASTER[block_index];
        let neighbor_raster = raster - if is_left { 1 } else { 4 };
        let neighbor_index = LUMA4X4_BLOCK_SCAN_TO_RASTER
            .iter()
            .position(|candidate| *candidate == neighbor_raster)
            .expect("valid I16x16 AC neighbour");
        return derive_coded_block_flag_cond_term(
            true,
            true,
            false,
            true,
            coded_flags[neighbor_index],
        );
    }
    if !edge.available || edge.is_ipcm {
        return true;
    }
    derive_coded_block_flag_cond_term(
        true,
        true,
        false,
        edge.ac_transform_block_available[edge_coordinate],
        edge.ac_transform_block_coded[edge_coordinate],
    )
}

fn derive_chroma_ac_cond_term(
    block_index: usize,
    is_left: bool,
    coded_flags: &[bool; 4],
    edge: CabacChroma420EdgeState,
) -> bool {
    let block_x = block_index % 2;
    let block_y = block_index / 2;
    let neighbor_coordinate = if is_left { block_x } else { block_y };
    let edge_coordinate = if is_left { block_y } else { block_x };
    if neighbor_coordinate > 0 {
        let neighbor_index = block_index - if is_left { 1 } else { 2 };
        return derive_coded_block_flag_cond_term(
            true,
            true,
            false,
            true,
            coded_flags[neighbor_index],
        );
    }

    if !edge.available || edge.is_ipcm {
        return true;
    }
    derive_coded_block_flag_cond_term(
        true,
        true,
        false,
        edge.ac_block_available[edge_coordinate],
        edge.ac_block_coded[edge_coordinate],
    )
}

fn decode_intra_macroblock_qp(
    decoder: &mut CabacArithmeticDecoder,
    contexts: &mut [CabacContextModel; CABAC_CONTEXT_COUNT],
    input: CabacIIntraMacroblockInput,
    cbp_luma: u8,
    cbp_chroma: u8,
    mb_type: u8,
) -> io::Result<(i32, i32)> {
    if cbp_luma == 0 && cbp_chroma == 0 && mb_type == 0 {
        return Ok((input.previous_qpy, 0));
    }
    let mut qp_contexts = [contexts[60], contexts[61], contexts[62], contexts[63]];
    let delta = decoder.decode_mb_qp_delta(&mut qp_contexts, input.previous_qp_delta)?;
    contexts[60..64].copy_from_slice(&qp_contexts);
    Ok(((input.previous_qpy + delta + 52).rem_euclid(52), delta))
}

/// Derives ref_idx_lX ctxIdxInc from clause 9.3.3.1.1.6 neighbour facts.
pub fn derive_cabac_reference_index_context_increment(
    left: CabacInterNeighbor,
    top: CabacInterNeighbor,
    mbaff_frame: bool,
    current_is_field: bool,
) -> u8 {
    let mut increment = 0;
    for (neighbor, bit) in [(left, 1), (top, 2)] {
        if !neighbor.available
            || neighbor.skip
            || neighbor.intra
            || !neighbor.prediction_mode_matches
        {
            continue;
        }
        let zero_threshold = u8::from(mbaff_frame && !current_is_field && neighbor.is_field);
        if neighbor.reference_index > zero_threshold {
            increment |= bit;
        }
    }
    increment
}

/// Derives mvd_lX ctxIdxInc from clause 9.3.3.1.1.7 neighbour facts.
pub fn derive_cabac_mvd_context_increment(
    left: CabacInterNeighbor,
    top: CabacInterNeighbor,
    component: u8,
    mbaff_frame: bool,
    current_is_field: bool,
) -> io::Result<u8> {
    if component > 1 {
        return Err(invalid("MVD component index must be 0 or 1"));
    }
    let left_abs = cabac_mvd_neighbor_magnitude(left, component, mbaff_frame, current_is_field);
    let top_abs = cabac_mvd_neighbor_magnitude(top, component, mbaff_frame, current_is_field);
    if left_abs > 32 || top_abs > 32 || left_abs + top_abs > 32 {
        return Ok(2);
    }
    Ok(u8::from(left_abs + top_abs > 2))
}

fn cabac_mvd_neighbor_magnitude(
    neighbor: CabacInterNeighbor,
    component: u8,
    mbaff_frame: bool,
    current_is_field: bool,
) -> u64 {
    if !neighbor.available || neighbor.skip || neighbor.intra || !neighbor.prediction_mode_matches {
        return 0;
    }
    let mut magnitude =
        i64::from(neighbor.motion_vector_difference[component as usize]).unsigned_abs();
    if component == 1 && mbaff_frame {
        if !current_is_field && neighbor.is_field {
            magnitude *= 2;
        } else if current_is_field && !neighbor.is_field {
            magnitude /= 2;
        }
    }
    magnitude
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

/// Initializes CABAC context indices 3 through 10 from H.264 Table 9-12.
pub fn new_cabac_i_intra_mb_type_contexts(slice_qpy: i32) -> io::Result<[CabacContextModel; 8]> {
    let mut contexts = [CabacContextModel::new(0, 0, slice_qpy)?; 8];
    for (context, (m, n)) in contexts.iter_mut().zip(I_INTRA_MB_TYPE_INIT) {
        *context = CabacContextModel::new(m, n, slice_qpy)?;
    }
    Ok(contexts)
}

/// Initializes P-slice mb_type contexts 14 through 17 from H.264 Table 9-13.
pub fn new_cabac_p_inter_mb_type_contexts(
    cabac_init_idc: u8,
    slice_qpy: i32,
) -> io::Result<[CabacContextModel; 4]> {
    let parameters = P_INTER_MB_TYPE_INIT
        .get(cabac_init_idc as usize)
        .ok_or_else(|| invalid("CABAC init idc is outside [0,2]"))?;
    let mut contexts = [CabacContextModel::new(0, 0, slice_qpy)?; 4];
    for (context, (m, n)) in contexts.iter_mut().zip(parameters) {
        *context = CabacContextModel::new(*m, *n, slice_qpy)?;
    }
    Ok(contexts)
}

/// Initializes MVD contexts 40-53 and the ref_idx contexts 54-59 that ref_idx_l0 and ref_idx_l1 share.
pub fn new_cabac_inter_prediction_contexts(
    cabac_init_idc: u8,
    slice_qpy: i32,
) -> io::Result<(
    [CabacContextModel; 7],
    [CabacContextModel; 7],
    [CabacContextModel; 6],
)> {
    let mvd_parameters = INTER_MVD_INIT
        .get(cabac_init_idc as usize)
        .ok_or_else(|| invalid("CABAC init idc is outside [0,2]"))?;
    let ref_idx_parameters = INTER_REF_IDX_INIT[cabac_init_idc as usize];
    let mut mvd_x = [CabacContextModel::new(0, 0, slice_qpy)?; 7];
    let mut mvd_y = [CabacContextModel::new(0, 0, slice_qpy)?; 7];
    let mut ref_idx = [CabacContextModel::new(0, 0, slice_qpy)?; 6];
    for index in 0..7 {
        mvd_x[index] =
            CabacContextModel::new(mvd_parameters[index].0, mvd_parameters[index].1, slice_qpy)?;
        mvd_y[index] = CabacContextModel::new(
            mvd_parameters[index + 7].0,
            mvd_parameters[index + 7].1,
            slice_qpy,
        )?;
    }
    for (context, (m, n)) in ref_idx.iter_mut().zip(ref_idx_parameters) {
        *context = CabacContextModel::new(m, n, slice_qpy)?;
    }
    Ok((mvd_x, mvd_y, ref_idx))
}

/// Initializes I-slice mb_qp_delta contexts 60-63 from H.264 Table 9-17.
pub fn new_cabac_i_mb_qp_delta_contexts(slice_qpy: i32) -> io::Result<[CabacContextModel; 4]> {
    let mut contexts = [CabacContextModel::new(0, 0, slice_qpy)?; 4];
    for (context, (m, n)) in contexts.iter_mut().zip(I_MB_QP_DELTA_INIT) {
        *context = CabacContextModel::new(m, n, slice_qpy)?;
    }
    Ok(contexts)
}

/// Initializes I-slice chroma prediction contexts 64 through 67 from H.264 Table 9-17.
pub fn new_cabac_i_intra_chroma_pred_mode_contexts(
    slice_qpy: i32,
) -> io::Result<[CabacContextModel; 4]> {
    Ok([
        CabacContextModel::new(
            I_INTRA_CHROMA_PRED_MODE_INIT[0].0,
            I_INTRA_CHROMA_PRED_MODE_INIT[0].1,
            slice_qpy,
        )?,
        CabacContextModel::new(
            I_INTRA_CHROMA_PRED_MODE_INIT[1].0,
            I_INTRA_CHROMA_PRED_MODE_INIT[1].1,
            slice_qpy,
        )?,
        CabacContextModel::new(
            I_INTRA_CHROMA_PRED_MODE_INIT[2].0,
            I_INTRA_CHROMA_PRED_MODE_INIT[2].1,
            slice_qpy,
        )?,
        CabacContextModel::new(
            I_INTRA_CHROMA_PRED_MODE_INIT[3].0,
            I_INTRA_CHROMA_PRED_MODE_INIT[3].1,
            slice_qpy,
        )?,
    ])
}

/// Initializes I-slice Intra_NxN mode contexts 68-69 from H.264 Table 9-17.
pub fn new_cabac_i_intra4x4_pred_mode_contexts(
    slice_qpy: i32,
) -> io::Result<[CabacContextModel; 2]> {
    Ok([
        CabacContextModel::new(
            I_INTRA4X4_PRED_MODE_INIT[0].0,
            I_INTRA4X4_PRED_MODE_INIT[0].1,
            slice_qpy,
        )?,
        CabacContextModel::new(
            I_INTRA4X4_PRED_MODE_INIT[1].0,
            I_INTRA4X4_PRED_MODE_INIT[1].1,
            slice_qpy,
        )?,
    ])
}

/// Initializes I-slice transform-size contexts 399-401 from H.264 Table 9-16.
pub fn new_cabac_i_transform_size_8x8_contexts(
    slice_qpy: i32,
) -> io::Result<[CabacContextModel; 3]> {
    Ok([
        CabacContextModel::new(
            I_TRANSFORM_SIZE_8X8_INIT[0].0,
            I_TRANSFORM_SIZE_8X8_INIT[0].1,
            slice_qpy,
        )?,
        CabacContextModel::new(
            I_TRANSFORM_SIZE_8X8_INIT[1].0,
            I_TRANSFORM_SIZE_8X8_INIT[1].1,
            slice_qpy,
        )?,
        CabacContextModel::new(
            I_TRANSFORM_SIZE_8X8_INIT[2].0,
            I_TRANSFORM_SIZE_8X8_INIT[2].1,
            slice_qpy,
        )?,
    ])
}

/// Initializes I-slice luma coded-block-pattern contexts 73-76 from H.264 Table 9-18.
pub fn new_cabac_i_luma_coded_block_pattern_contexts(
    slice_qpy: i32,
) -> io::Result<[CabacContextModel; 4]> {
    let mut contexts = [CabacContextModel::new(0, 0, slice_qpy)?; 4];
    for (context, (m, n)) in contexts.iter_mut().zip(I_LUMA_CODED_BLOCK_PATTERN_INIT) {
        *context = CabacContextModel::new(m, n, slice_qpy)?;
    }
    Ok(contexts)
}

/// Initializes I-slice chroma coded-block-pattern contexts 77-84 from H.264 Table 9-18.
pub fn new_cabac_i_chroma_coded_block_pattern_contexts(
    slice_qpy: i32,
) -> io::Result<[CabacContextModel; 8]> {
    let mut contexts = [CabacContextModel::new(0, 0, slice_qpy)?; 8];
    for (context, (m, n)) in contexts.iter_mut().zip(I_CHROMA_CODED_BLOCK_PATTERN_INIT) {
        *context = CabacContextModel::new(m, n, slice_qpy)?;
    }
    Ok(contexts)
}

/// Initializes I-slice luma 4x4 coded-block-flag contexts 93-96 from H.264 Table 9-18.
pub fn new_cabac_i_luma4x4_coded_block_flag_contexts(
    slice_qpy: i32,
) -> io::Result<[CabacContextModel; 4]> {
    let mut contexts = [CabacContextModel::new(0, 0, slice_qpy)?; 4];
    for (context, (m, n)) in contexts.iter_mut().zip(I_LUMA4X4_CODED_BLOCK_FLAG_INIT) {
        *context = CabacContextModel::new(m, n, slice_qpy)?;
    }
    Ok(contexts)
}

/// Number of CABAC contexts, ctxIdx 0-459, used by 4:2:0 slices.
pub const CABAC_CONTEXT_COUNT: usize = 460;

/// Initializes ctxIdx 0-459 for one slice from H.264 Tables 9-12 to 9-24.
/// I/SI slices ignore `cabac_init_idc`; entries unused by the slice type initialize from (0, 0).
pub fn new_cabac_slice_contexts(
    slice_type: u8,
    cabac_init_idc: u8,
    slice_qpy: i32,
) -> io::Result<Vec<CabacContextModel>> {
    if slice_type > 9 {
        return Err(invalid("CABAC syntax is unsupported for this slice type"));
    }
    let column = if matches!(slice_type % 5, 2 | 4) {
        0
    } else if cabac_init_idc > 2 {
        return Err(invalid("CABAC init idc is outside [0,2]"));
    } else {
        usize::from(cabac_init_idc) + 1
    };
    CONTEXT_INIT_TABLE[column]
        .iter()
        .map(|&(m, n)| CabacContextModel::new(i32::from(m), i32::from(n), slice_qpy))
        .collect()
}

/// Returns frame-coded first ctxIdx for coded_block_flag, significant, last, and abs level.
/// coded_block_flag is `None` for ctxBlockCat 5, whose flag is inferred when ChromaArrayType != 3.
pub fn cabac_residual_context_bases(
    ctx_block_cat: u8,
) -> io::Result<(Option<usize>, usize, usize, usize)> {
    const SIGNIFICANCE_OFFSET: [usize; 5] = [0, 15, 29, 44, 47];
    const ABS_LEVEL_OFFSET: [usize; 5] = [0, 10, 20, 30, 39];
    match ctx_block_cat {
        0..=4 => {
            let category = usize::from(ctx_block_cat);
            Ok((
                Some(85 + 4 * category),
                105 + SIGNIFICANCE_OFFSET[category],
                166 + SIGNIFICANCE_OFFSET[category],
                227 + ABS_LEVEL_OFFSET[category],
            ))
        }
        5 => Ok((None, 402, 417, 426)),
        _ => Err(invalid("CABAC syntax is unsupported for this ctxBlockCat")),
    }
}

/// maxNumCoeff for ctxBlockCat 0-4 with 4:2:0 chroma (4 * NumC8x8 = 4 chroma DC).
const RESIDUAL_MAX_NUM_COEFF: [usize; 5] = [16, 15, 16, 4, 15];

/// Table 9-43 ctxIdxInc by levelListIdx for frame-coded significant/last flags.
#[rustfmt::skip]
const LUMA8X8_FRAME_SIGNIFICANT_INC: [usize; 63] = [
    0, 1, 2, 3, 4, 5, 5, 4, 4, 3, 3, 4, 4, 4, 5, 5, 4, 4, 4, 4, 3, 3, 6, 7, 7, 7, 8, 9, 10, 9, 8, 7,
    7, 6, 11, 12, 13, 11, 6, 7, 8, 9, 14, 10, 9, 8, 6, 11, 12, 13, 11, 6, 9, 14, 10, 9, 11, 12, 13, 11, 14, 10, 12,
];
#[rustfmt::skip]
const LUMA8X8_LAST_INC: [usize; 63] = [
    0, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2,
    3, 3, 3, 3, 3, 3, 3, 3, 4, 4, 4, 4, 4, 4, 4, 4, 5, 5, 5, 5, 6, 6, 6, 6, 7, 7, 7, 7, 8, 8, 8,
];

/// Returns condTermFlagN for coded_block_flag (clause 9.3.3.1.1.9).
/// The slice-data-partitioning rule is omitted because partitioning is unsupported.
pub fn derive_coded_block_flag_cond_term(
    neighbor_available: bool,
    current_intra: bool,
    neighbor_ipcm: bool,
    trans_block_available: bool,
    trans_block_coded: bool,
) -> bool {
    if !neighbor_available {
        current_intra
    } else if neighbor_ipcm {
        true
    } else {
        trans_block_available && trans_block_coded
    }
}

pub struct CabacArithmeticDecoder<'a> {
    bits: BitReader<'a>,
    code_range: u32,
    code_offset: u32,
    terminated: bool,
}

/// Derives predIntra4x4PredMode from A/B neighbors in luma4x4BlkIdx syntax order.
pub fn derive_intra4x4_predicted_mode(
    decoded_modes: &[u8; 16],
    block_index: usize,
    top_modes: &[u8; 4],
    left_modes: &[u8; 4],
    top_available: bool,
    left_available: bool,
) -> io::Result<u8> {
    if block_index >= LUMA4X4_BLOCK_SCAN_TO_RASTER.len() {
        return Err(invalid("Intra_4x4 prediction neighbor state is invalid"));
    }
    let raster_index = LUMA4X4_BLOCK_SCAN_TO_RASTER[block_index];
    let column = raster_index % 4;
    let row = raster_index / 4;
    let mut neighbors = [None; 2];
    for (slot, is_left, coordinate, available, external_modes) in [
        (0, true, column, left_available, left_modes),
        (1, false, row, top_available, top_modes),
    ] {
        if coordinate > 0 {
            let neighbor_raster = raster_index - if is_left { 1 } else { 4 };
            let neighbor_index = LUMA4X4_BLOCK_SCAN_TO_RASTER
                .iter()
                .position(|candidate| *candidate == neighbor_raster)
                .ok_or_else(|| invalid("Intra_4x4 prediction neighbor state is invalid"))?;
            if neighbor_index >= block_index {
                return Err(invalid("Intra_4x4 prediction neighbor is not decoded yet"));
            }
            let mode = decoded_modes[neighbor_index];
            if mode > 8 {
                return Err(invalid(
                    "Intra_4x4 prediction neighbor mode is outside [0,8]",
                ));
            }
            neighbors[slot] = Some(mode);
        } else if available {
            let mode = external_modes[if is_left { row } else { column }];
            if mode > 8 {
                return Err(invalid(
                    "Intra_4x4 prediction neighbor mode is outside [0,8]",
                ));
            }
            neighbors[slot] = Some(mode);
        }
    }
    match (neighbors[0], neighbors[1]) {
        (Some(left), Some(top)) => Ok(left.min(top)),
        _ => Ok(2),
    }
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

    /// Decodes truncated-unary ref_idx_l0/l1 using a six-context offset bank.
    pub fn decode_reference_index(
        &mut self,
        max_ref_idx_minus1: u32,
        neighbor_context_increment: usize,
        contexts: &mut [CabacContextModel; 6],
    ) -> io::Result<u8> {
        if max_ref_idx_minus1 > 31 {
            return Err(invalid("ref_idx maximum is outside [0,31]"));
        }
        if neighbor_context_increment > 3 {
            return Err(invalid(
                "ref_idx neighbor context increment is outside [0,3]",
            ));
        }
        if contexts.iter().any(|model| model.state_index >= 64) {
            return Err(invalid("ref_idx requires valid consecutive contexts"));
        }
        if max_ref_idx_minus1 == 0 {
            return Ok(0);
        }
        let mut trial = Self {
            bits: self.bits.clone(),
            code_range: self.code_range,
            code_offset: self.code_offset,
            terminated: self.terminated,
        };
        let mut trial_contexts = *contexts;
        let mut ref_idx = 0_u8;
        if trial.decode_bin(&mut trial_contexts[neighbor_context_increment])? {
            ref_idx = 1;
            while u32::from(ref_idx) < max_ref_idx_minus1 {
                let context_index = if ref_idx == 1 { 4 } else { 5 };
                if !trial.decode_bin(&mut trial_contexts[context_index])? {
                    break;
                }
                ref_idx += 1;
            }
        }
        self.bits = trial.bits;
        self.code_range = trial.code_range;
        self.code_offset = trial.code_offset;
        self.terminated = trial.terminated;
        *contexts = trial_contexts;
        Ok(ref_idx)
    }

    /// Derives the neighbouring context increment and decodes ref_idx_lX for one partition.
    pub fn decode_reference_index_for_partition(
        &mut self,
        max_ref_idx_minus1: u32,
        left: CabacInterNeighbor,
        top: CabacInterNeighbor,
        mbaff_frame: bool,
        current_is_field: bool,
        contexts: &mut [CabacContextModel; 6],
    ) -> io::Result<u8> {
        let increment = derive_cabac_reference_index_context_increment(
            left,
            top,
            mbaff_frame,
            current_is_field,
        );
        self.decode_reference_index(max_ref_idx_minus1, usize::from(increment), contexts)
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

    /// Decodes I16x16 luma DC/AC residual syntax and reconstructs its luma plane.
    /// I16x16 mb_type/QPY and prediction references are supplied by the enclosing parser.
    pub fn decode_intra16x16_luma_macroblock(
        &mut self,
        mb_type: u8,
        contexts: &mut [CabacContextModel; CABAC_CONTEXT_COUNT],
        qpy: i32,
        scaling_list: &[u8; 16],
        top: Option<&[u8; 16]>,
        left: Option<&[u8; 16]>,
        top_left: u8,
        top_left_available: bool,
        top_edge: CabacIntra16x16EdgeState,
        left_edge: CabacIntra16x16EdgeState,
    ) -> io::Result<Intra16x16LumaMacroblockResult> {
        if !(1..=24).contains(&mb_type) {
            return Err(invalid("I16x16 mb_type is outside [1,24]"));
        }
        crate::reconstruction::reconstruct_intra16x16_luma_dc(&[0; 16], scaling_list, qpy)?;
        let mut trial = Self {
            bits: self.bits.clone(),
            code_range: self.code_range,
            code_offset: self.code_offset,
            terminated: self.terminated,
        };
        let mut trial_contexts = *contexts;
        let mut result = Intra16x16LumaMacroblockResult {
            prediction_mode: (mb_type - 1) % 4,
            coded_block_pattern_luma: if mb_type >= 13 { 15 } else { 0 },
            ..Intra16x16LumaMacroblockResult::default()
        };
        let dc_cond_left = derive_coded_block_flag_cond_term(
            left_edge.available,
            true,
            left_edge.is_ipcm,
            left_edge.dc_transform_block_available,
            left_edge.dc_transform_block_coded,
        );
        let dc_cond_top = derive_coded_block_flag_cond_term(
            top_edge.available,
            true,
            top_edge.is_ipcm,
            top_edge.dc_transform_block_available,
            top_edge.dc_transform_block_coded,
        );
        let (dc_scan, dc_coded) =
            trial.decode_residual_block(2, dc_cond_left, dc_cond_top, &mut trial_contexts)?;
        result.dc_coded = dc_coded;
        if dc_coded {
            let dc_raster = place_luma4x4_scan_levels(&dc_scan);
            result.dc_levels = crate::reconstruction::reconstruct_intra16x16_luma_dc(
                &dc_raster,
                scaling_list,
                qpy,
            )?;
        }
        for block_index in 0..16 {
            let raster = LUMA4X4_BLOCK_SCAN_TO_RASTER[block_index];
            let block_x = raster % 4;
            let block_y = raster / 4;
            if result.coded_block_pattern_luma & (1 << ((block_y / 2) * 2 + block_x / 2)) != 0 {
                let cond_left = derive_intra16x16_ac_cond_term(
                    block_index,
                    block_x,
                    block_y,
                    true,
                    &result.ac_coded_block_flags,
                    left_edge,
                );
                let cond_top = derive_intra16x16_ac_cond_term(
                    block_index,
                    block_x,
                    block_y,
                    false,
                    &result.ac_coded_block_flags,
                    top_edge,
                );
                let (ac_levels, coded) =
                    trial.decode_residual_block(1, cond_left, cond_top, &mut trial_contexts)?;
                result.ac_coded_block_flags[block_index] = coded;
                if coded {
                    let mut scan = [0_i32; 16];
                    scan[1..].copy_from_slice(&ac_levels[..15]);
                    let mut raster_levels = place_luma4x4_scan_levels(&scan);
                    raster_levels[0] = 0;
                    let mut coefficients = crate::reconstruction::inverse_scale_luma4x4(
                        &raster_levels,
                        scaling_list,
                        qpy,
                    )?;
                    coefficients[0] = result.dc_levels[raster];
                    let block_residual =
                        crate::reconstruction::inverse_transform_luma4x4(&coefficients);
                    for row in 0..4 {
                        let destination = (block_y * 4 + row) * 16 + block_x * 4;
                        result.residual[destination..destination + 4]
                            .copy_from_slice(&block_residual[row * 4..row * 4 + 4]);
                    }
                    continue;
                }
            }
            let mut coefficients =
                crate::reconstruction::inverse_scale_luma4x4(&[0; 16], scaling_list, qpy)?;
            coefficients[0] = result.dc_levels[raster];
            let block_residual = crate::reconstruction::inverse_transform_luma4x4(&coefficients);
            for row in 0..4 {
                let destination = (block_y * 4 + row) * 16 + block_x * 4;
                result.residual[destination..destination + 4]
                    .copy_from_slice(&block_residual[row * 4..row * 4 + 4]);
            }
        }
        result.samples = crate::reconstruction::reconstruct_luma_intra16x16_macroblock(
            result.prediction_mode,
            top,
            left,
            top_left,
            top_left_available,
            &result.residual,
        )?;
        self.bits = trial.bits;
        self.code_range = trial.code_range;
        self.code_offset = trial.code_offset;
        self.terminated = trial.terminated;
        *contexts = trial_contexts;
        Ok(result)
    }

    /// Decodes/reconstructs Cb and Cr for an 8-bit 4:2:0 intra macroblock.
    /// `intra_chroma_pred_mode` is separate from mb_type; I16x16 mb_type supplies only chroma CBP.
    pub fn decode_intra_chroma420_macroblock(
        &mut self,
        contexts: &mut [CabacContextModel; CABAC_CONTEXT_COUNT],
        mut mode: u8,
        mode_already_decoded: bool,
        mb_type: u8,
        intra16x16: bool,
        left_mode_nonzero: bool,
        top_mode_nonzero: bool,
        mut coded_block_pattern_chroma: u8,
        qpy: i32,
        qp_offsets: [i32; 2],
        scaling_lists: [[u8; 16]; 2],
        references: [CabacChroma420References; 2],
        left_edges: [CabacChroma420EdgeState; 2],
        top_edges: [CabacChroma420EdgeState; 2],
    ) -> io::Result<IntraChroma420MacroblockResult> {
        if intra16x16 {
            if !(1..=24).contains(&mb_type) {
                return Err(invalid("I16x16 mb_type is outside [1,24]"));
            }
            coded_block_pattern_chroma = ((mb_type - 1) / 4) % 3;
        } else if mb_type != 0 || coded_block_pattern_chroma > 2 {
            return Err(invalid("Intra_NxN chroma mode or CBP is invalid"));
        }
        if mode_already_decoded && mode > 3 {
            return Err(invalid("chroma prediction mode is outside [0,3]"));
        }
        if !mode_already_decoded && mode != 0 {
            return Err(invalid("undecoded chroma prediction mode must be zero"));
        }
        let mut trial = Self {
            bits: self.bits.clone(),
            code_range: self.code_range,
            code_offset: self.code_offset,
            terminated: self.terminated,
        };
        let mut trial_contexts = *contexts;
        if !mode_already_decoded {
            trial
                .decode_intra_chroma_pred_mode(
                    &mut trial_contexts[64..68]
                        .try_into()
                        .map_err(|_| invalid("chroma mode context range is invalid"))?,
                    left_mode_nonzero,
                    top_mode_nonzero,
                )
                .map(|decoded_mode| mode = decoded_mode)?;
        }
        let prediction_mode = mode;

        let mut result = IntraChroma420MacroblockResult {
            prediction_mode,
            ..IntraChroma420MacroblockResult::default()
        };
        for component in 0..2 {
            let qpc = crate::reconstruction::derive_chroma_qpc(qpy, qp_offsets[component])?;
            result.qpc[component] = qpc;
            let mut dc_samples = [0_i64; 4];
            if coded_block_pattern_chroma != 0 {
                let left = left_edges[component];
                let top = top_edges[component];
                let cond_left = derive_coded_block_flag_cond_term(
                    left.available,
                    true,
                    left.is_ipcm,
                    left.dc_block_available,
                    left.dc_block_coded,
                );
                let cond_top = derive_coded_block_flag_cond_term(
                    top.available,
                    true,
                    top.is_ipcm,
                    top.dc_block_available,
                    top.dc_block_coded,
                );
                let (dc_levels, dc_coded) =
                    trial.decode_residual_block(3, cond_left, cond_top, &mut trial_contexts)?;
                if dc_coded {
                    result.dc_coded[component] = true;
                    let levels = [dc_levels[0], dc_levels[1], dc_levels[2], dc_levels[3]];
                    let transformed =
                        crate::reconstruction::inverse_transform_chroma_dc2x2(&levels);
                    dc_samples =
                        crate::reconstruction::inverse_scale_chroma_dc2x2(&transformed, qpc)?;
                }
            }

            let mut residual_blocks = [[0_i64; 16]; 4];
            let mut ac_coded_flags = [false; 4];
            for block_index in 0..4 {
                let mut ac_scan_levels = [0_i32; 15];
                if coded_block_pattern_chroma == 2 {
                    let cond_left = derive_chroma_ac_cond_term(
                        block_index,
                        true,
                        &ac_coded_flags,
                        left_edges[component],
                    );
                    let cond_top = derive_chroma_ac_cond_term(
                        block_index,
                        false,
                        &ac_coded_flags,
                        top_edges[component],
                    );
                    let (levels, coded) =
                        trial.decode_residual_block(4, cond_left, cond_top, &mut trial_contexts)?;
                    ac_coded_flags[block_index] = coded;
                    if coded {
                        ac_scan_levels.copy_from_slice(&levels[..15]);
                    }
                }
                residual_blocks[block_index] =
                    crate::reconstruction::reconstruct_chroma4x4_residual(
                        dc_samples[block_index],
                        &ac_scan_levels,
                        &scaling_lists[component],
                        qpc,
                    )?;
            }
            result.ac_coded_block_flags[component] = ac_coded_flags;
            let residual =
                crate::reconstruction::assemble_chroma420_residual_macroblock(&residual_blocks);
            let references = references[component];
            let prediction = crate::reconstruction::predict_chroma_intra8x8(
                prediction_mode,
                references.top_available.then_some(&references.top),
                references.left_available.then_some(&references.left),
                references.top_left_available.then_some(references.top_left),
            )?;
            let samples =
                crate::reconstruction::reconstruct_chroma420_macroblock(&prediction, &residual);
            if component == 0 {
                result.cb_residual = residual;
                result.cb = samples;
            } else {
                result.cr_residual = residual;
                result.cr = samples;
            }
        }
        self.bits = trial.bits;
        self.code_range = trial.code_range;
        self.code_offset = trial.code_offset;
        self.terminated = trial.terminated;
        *contexts = trial_contexts;
        Ok(result)
    }

    /// Decodes one I macroblock in syntax order, then places reconstructed Y/U/V planes atomically.
    /// Block references and neighbor CBP/CBF facts are gathered by the caller from decoded neighbors.
    pub fn decode_i_intra_macroblock(
        &mut self,
        input: CabacIIntraMacroblockInput,
        contexts: &mut [CabacContextModel; CABAC_CONTEXT_COUNT],
        builder: &mut crate::Yuv420FrameBuilder,
        address: usize,
    ) -> io::Result<CabacIIntraMacroblockResult> {
        if !(0..=51).contains(&input.previous_qpy) {
            return Err(invalid("previous QPY is outside [0,51]"));
        }
        let mut trial = Self {
            bits: self.bits.clone(),
            code_range: self.code_range,
            code_offset: self.code_offset,
            terminated: self.terminated,
        };
        let mut trial_contexts = *contexts;
        let mut mb_type_contexts = [
            trial_contexts[3],
            trial_contexts[4],
            trial_contexts[5],
            trial_contexts[6],
            trial_contexts[7],
            trial_contexts[8],
            trial_contexts[9],
            trial_contexts[10],
        ];
        let mb_type = trial.decode_i_intra_mb_type(
            input.slice_type,
            &mut mb_type_contexts,
            input.left_available,
            input.left_intra16_or_pcm,
            input.top_available,
            input.top_intra16_or_pcm,
        )?;
        trial_contexts[3..11].copy_from_slice(&mb_type_contexts);
        let mut result = CabacIIntraMacroblockResult {
            macroblock_type: mb_type,
            qpy: input.previous_qpy,
            ..CabacIIntraMacroblockResult::default()
        };

        if mb_type == 25 {
            let mut y_block = [0_u8; 256];
            let mut cb_block = [0_u8; 64];
            let mut cr_block = [0_u8; 64];
            while trial.bits.bit_offset() % 8 != 0 {
                if trial.bits.read_bit()? {
                    return Err(invalid("I_PCM alignment bit is not zero"));
                }
            }
            for sample in &mut y_block {
                *sample = trial.bits.read_bits(8)? as u8;
            }
            for sample in &mut cb_block {
                *sample = trial.bits.read_bits(8)? as u8;
            }
            for sample in &mut cr_block {
                *sample = trial.bits.read_bits(8)? as u8;
            }
            let offset = trial.bits.read_bits(9)? as u32;
            if offset >= INITIAL_RANGE {
                return Err(invalid(
                    "I_PCM CABAC offset is outside the arithmetic range",
                ));
            }
            builder.place_macroblock(address, &y_block, &cb_block, &cr_block)?;
            trial.code_range = INITIAL_RANGE;
            trial.code_offset = offset;
            trial.terminated = false;
            result.luma = y_block;
            result.cb = cb_block;
            result.cr = cr_block;
            self.bits = trial.bits;
            self.code_range = trial.code_range;
            self.code_offset = trial.code_offset;
            self.terminated = trial.terminated;
            *contexts = trial_contexts;
            return Ok(result);
        }

        let mut modes_4x4 = [0_u8; 16];
        let mut modes_8x8 = [0_u8; 4];
        if mb_type == 0 {
            if input.transform_8x8_mode_enabled {
                let mut transform_contexts = [
                    trial_contexts[399],
                    trial_contexts[400],
                    trial_contexts[401],
                ];
                result.transform_size_8x8 = trial.decode_transform_size_8x8_flag(
                    &mut transform_contexts,
                    input.left_has_8x8_transform,
                    input.top_has_8x8_transform,
                )?;
                trial_contexts[399..402].copy_from_slice(&transform_contexts);
            }
            let mut mode_contexts = [trial_contexts[68], trial_contexts[69]];
            if result.transform_size_8x8 {
                for block_index in 0..4 {
                    let block_x = block_index % 2;
                    let block_y = block_index / 2;
                    let mut left_available = input.left_mode_available;
                    let mut top_available = input.top_mode_available;
                    let mut left_mode = input.left_modes_8x8[block_y];
                    let mut top_mode = input.top_modes_8x8[block_x];
                    if block_x > 0 {
                        left_available = true;
                        left_mode = modes_8x8[block_index - 1];
                    }
                    if block_y > 0 {
                        top_available = true;
                        top_mode = modes_8x8[block_index - 2];
                    }
                    let predicted = if left_available && top_available {
                        left_mode.min(top_mode)
                    } else {
                        2
                    };
                    modes_8x8[block_index] =
                        trial.decode_intra4x4_pred_mode(predicted, &mut mode_contexts)?;
                }
            } else {
                modes_4x4 = trial.decode_intra4x4_pred_modes(
                    &mut mode_contexts,
                    &input.top_modes_4x4,
                    &input.left_modes_4x4,
                    input.top_mode_available,
                    input.left_mode_available,
                )?;
            }
            trial_contexts[68..70].copy_from_slice(&mode_contexts);
            let mut chroma_mode_contexts = [
                trial_contexts[64],
                trial_contexts[65],
                trial_contexts[66],
                trial_contexts[67],
            ];
            let chroma_mode = trial.decode_intra_chroma_pred_mode(
                &mut chroma_mode_contexts,
                input.chroma_left_mode_nonzero,
                input.chroma_top_mode_nonzero,
            )?;
            result.chroma_prediction_mode = chroma_mode;
            result.luma_4x4_modes = modes_4x4;
            result.luma_8x8_modes = modes_8x8;
            trial_contexts[64..68].copy_from_slice(&chroma_mode_contexts);
            let mut luma_cbp_contexts = [
                trial_contexts[73],
                trial_contexts[74],
                trial_contexts[75],
                trial_contexts[76],
            ];
            result.coded_block_pattern_luma = trial.decode_luma_coded_block_pattern(
                input.left_luma_cbp,
                input.top_luma_cbp,
                &mut luma_cbp_contexts,
            )?;
            trial_contexts[73..77].copy_from_slice(&luma_cbp_contexts);
            let mut chroma_cbp_contexts = [
                trial_contexts[77],
                trial_contexts[78],
                trial_contexts[79],
                trial_contexts[80],
                trial_contexts[81],
                trial_contexts[82],
                trial_contexts[83],
                trial_contexts[84],
            ];
            result.coded_block_pattern_chroma = trial.decode_chroma_coded_block_pattern(
                input.left_chroma_cbp,
                input.top_chroma_cbp,
                &mut chroma_cbp_contexts,
            )?;
            trial_contexts[77..85].copy_from_slice(&chroma_cbp_contexts);
            let (qpy, qp_delta) = decode_intra_macroblock_qp(
                &mut trial,
                &mut trial_contexts,
                input,
                result.coded_block_pattern_luma,
                result.coded_block_pattern_chroma,
                mb_type,
            )?;
            result.qpy = qpy;
            result.qp_delta = qp_delta;

            if result.transform_size_8x8 {
                let mut blocks = input.luma_8x8_blocks;
                for block_index in 0..4 {
                    blocks[block_index].mode = modes_8x8[block_index];
                    if result.coded_block_pattern_luma & (1 << block_index) == 0 {
                        blocks[block_index].residual = [0; 64];
                        continue;
                    }
                    let levels = trial.decode_luma8x8_residual_block(&mut trial_contexts)?;
                    let mut raster = [0_i32; 64];
                    for (scan_index, raster_index) in LUMA8X8_SCAN_TO_RASTER.iter().enumerate() {
                        raster[*raster_index] = levels[scan_index];
                    }
                    let coefficients = crate::reconstruction::inverse_scale_luma8x8(
                        &raster,
                        &input.luma_8x8_scaling_list,
                        qpy,
                    )?;
                    blocks[block_index].residual =
                        crate::reconstruction::inverse_transform_luma8x8(&coefficients);
                }
                result.luma = crate::reconstruction::reconstruct_luma_intra8x8_macroblock(&blocks)?;
            } else {
                let mut blocks = input.luma_4x4_blocks;
                for block_index in 0..16 {
                    let raster = LUMA4X4_BLOCK_SCAN_TO_RASTER[block_index];
                    let block_x = raster % 4;
                    let block_y = raster / 4;
                    blocks[block_index].mode = modes_4x4[block_index];
                    if result.coded_block_pattern_luma & (1 << ((block_y / 2) * 2 + block_x / 2))
                        == 0
                    {
                        blocks[block_index].residual = [0; 16];
                        continue;
                    }
                    let cond_left = derive_intra4x4_luma_cond_term(
                        block_index,
                        block_x,
                        block_y,
                        true,
                        &result.luma_4x4_coded_block_flags,
                        input.luma_4x4_left_edge,
                    );
                    let cond_top = derive_intra4x4_luma_cond_term(
                        block_index,
                        block_x,
                        block_y,
                        false,
                        &result.luma_4x4_coded_block_flags,
                        input.luma_4x4_top_edge,
                    );
                    let (levels, coded) =
                        trial.decode_residual_block(2, cond_left, cond_top, &mut trial_contexts)?;
                    result.luma_4x4_coded_block_flags[block_index] = coded;
                    if coded {
                        let raster_levels = crate::place_luma4x4_scan_levels(&levels);
                        let coefficients = crate::reconstruction::inverse_scale_luma4x4(
                            &raster_levels,
                            &input.luma_4x4_scaling_list,
                            qpy,
                        )?;
                        blocks[block_index].residual =
                            crate::reconstruction::inverse_transform_luma4x4(&coefficients);
                    }
                }
                result.luma = crate::reconstruction::reconstruct_luma_intra4x4_macroblock(&blocks)?;
            }
            let chroma = trial.decode_intra_chroma420_macroblock(
                &mut trial_contexts,
                chroma_mode,
                true,
                0,
                false,
                input.chroma_left_mode_nonzero,
                input.chroma_top_mode_nonzero,
                result.coded_block_pattern_chroma,
                result.qpy,
                input.chroma_qp_index_offsets,
                input.chroma_scaling_lists,
                input.chroma_references,
                input.chroma_left_edges,
                input.chroma_top_edges,
            )?;
            result.cb = chroma.cb;
            result.cr = chroma.cr;
            result.chroma_dc_coded = chroma.dc_coded;
            result.chroma_ac_coded_flags = chroma.ac_coded_block_flags;
        } else {
            result.coded_block_pattern_luma = if mb_type >= 13 { 15 } else { 0 };
            result.coded_block_pattern_chroma = ((mb_type - 1) / 4) % 3;
            let mut chroma_mode_contexts = [
                trial_contexts[64],
                trial_contexts[65],
                trial_contexts[66],
                trial_contexts[67],
            ];
            let chroma_mode = trial.decode_intra_chroma_pred_mode(
                &mut chroma_mode_contexts,
                input.chroma_left_mode_nonzero,
                input.chroma_top_mode_nonzero,
            )?;
            result.intra16x16_luma_mode = (mb_type - 1) % 4;
            result.chroma_prediction_mode = chroma_mode;
            trial_contexts[64..68].copy_from_slice(&chroma_mode_contexts);
            let (qpy, qp_delta) = decode_intra_macroblock_qp(
                &mut trial,
                &mut trial_contexts,
                input,
                result.coded_block_pattern_luma,
                result.coded_block_pattern_chroma,
                mb_type,
            )?;
            result.qpy = qpy;
            result.qp_delta = qp_delta;
            let top = input
                .intra16x16_top_available
                .then_some(&input.intra16x16_top);
            let left = input
                .intra16x16_left_available
                .then_some(&input.intra16x16_left);
            let luma = trial.decode_intra16x16_luma_macroblock(
                mb_type,
                &mut trial_contexts,
                qpy,
                &input.luma_4x4_scaling_list,
                top,
                left,
                input.intra16x16_top_left,
                input.intra16x16_top_left_available,
                input.luma_16x16_top_edge,
                input.luma_16x16_left_edge,
            )?;
            result.luma = luma.samples;
            let chroma = trial.decode_intra_chroma420_macroblock(
                &mut trial_contexts,
                chroma_mode,
                true,
                mb_type,
                true,
                input.chroma_left_mode_nonzero,
                input.chroma_top_mode_nonzero,
                result.coded_block_pattern_chroma,
                qpy,
                input.chroma_qp_index_offsets,
                input.chroma_scaling_lists,
                input.chroma_references,
                input.chroma_left_edges,
                input.chroma_top_edges,
            )?;
            result.cb = chroma.cb;
            result.cr = chroma.cr;
            result.chroma_dc_coded = chroma.dc_coded;
            result.chroma_ac_coded_flags = chroma.ac_coded_block_flags;
        }

        builder.place_macroblock(address, &result.luma, &result.cb, &result.cr)?;
        self.bits = trial.bits;
        self.code_range = trial.code_range;
        self.code_offset = trial.code_offset;
        self.terminated = trial.terminated;
        *contexts = trial_contexts;
        Ok(result)
    }

    /// Decodes and places one 8-bit 4:2:0 I_PCM macroblock, then restarts CABAC per clause 9.3.1.2.
    pub fn decode_ipcm_intra_macroblock(
        &mut self,
        slice_type: u8,
        contexts: &mut [CabacContextModel; 8],
        left_available: bool,
        left_intra16_or_pcm: bool,
        top_available: bool,
        top_intra16_or_pcm: bool,
        builder: &mut crate::Yuv420FrameBuilder,
        address: usize,
    ) -> io::Result<()> {
        let mut trial = Self {
            bits: self.bits.clone(),
            code_range: self.code_range,
            code_offset: self.code_offset,
            terminated: self.terminated,
        };
        let mut trial_contexts = *contexts;
        let mb_type = trial.decode_i_intra_mb_type(
            slice_type,
            &mut trial_contexts,
            left_available,
            left_intra16_or_pcm,
            top_available,
            top_intra16_or_pcm,
        )?;
        if mb_type != 25 {
            return Err(invalid(
                "only I_PCM macroblocks are implemented by this entry point",
            ));
        }
        while trial.bits.bit_offset() % 8 != 0 {
            if trial.bits.read_bit()? {
                return Err(invalid("I_PCM alignment bit is not zero"));
            }
        }
        let mut y_block = [0_u8; 256];
        let mut u_block = [0_u8; 64];
        let mut v_block = [0_u8; 64];
        for sample in &mut y_block {
            *sample = trial.bits.read_bits(8)? as u8;
        }
        for sample in &mut u_block {
            *sample = trial.bits.read_bits(8)? as u8;
        }
        for sample in &mut v_block {
            *sample = trial.bits.read_bits(8)? as u8;
        }
        let offset = trial.bits.read_bits(9)?;
        if offset >= INITIAL_RANGE {
            return Err(invalid(
                "CABAC initial offset is outside the arithmetic range",
            ));
        }
        builder.place_macroblock(address, &y_block, &u_block, &v_block)?;
        self.bits = trial.bits;
        self.code_range = INITIAL_RANGE;
        self.code_offset = offset;
        self.terminated = false;
        *contexts = trial_contexts;
        Ok(())
    }

    /// Decodes Intra_NxN 4x4 modes/CBFs/luma residuals and reconstructs its luma plane.
    /// Per-block sample references are caller-gathered; top/left modes and edge CBF facts are external.
    pub fn decode_intra_nxn_4x4_luma_macroblock(
        &mut self,
        contexts: &mut [CabacContextModel; CABAC_CONTEXT_COUNT],
        coded_block_pattern_luma: u8,
        qpy: i32,
        scaling_list: &[u8; 16],
        top_modes: &[u8; 4],
        left_modes: &[u8; 4],
        top_mode_available: bool,
        left_mode_available: bool,
        top_edge: CabacIntra4x4EdgeState,
        left_edge: CabacIntra4x4EdgeState,
        blocks: &[crate::LumaIntra4x4Block; 16],
    ) -> io::Result<Intra4x4LumaMacroblockResult> {
        if coded_block_pattern_luma > 15 {
            return Err(invalid("luma coded_block_pattern is outside [0,15]"));
        }
        let mut trial = Self {
            bits: self.bits.clone(),
            code_range: self.code_range,
            code_offset: self.code_offset,
            terminated: self.terminated,
        };
        let mut trial_contexts = *contexts;
        let mut mode_contexts = [trial_contexts[68], trial_contexts[69]];
        let modes = trial.decode_intra4x4_pred_modes(
            &mut mode_contexts,
            top_modes,
            left_modes,
            top_mode_available,
            left_mode_available,
        )?;
        trial_contexts[68..70].copy_from_slice(&mode_contexts);

        let mut result = Intra4x4LumaMacroblockResult {
            modes,
            ..Intra4x4LumaMacroblockResult::default()
        };
        let mut reconstructed_blocks = *blocks;
        for block_index in 0..16 {
            let raster_index = LUMA4X4_BLOCK_SCAN_TO_RASTER[block_index];
            let block_x = raster_index % 4;
            let block_y = raster_index / 4;
            reconstructed_blocks[block_index].mode = modes[block_index];
            let cbp_index = (block_y / 2) * 2 + block_x / 2;
            if coded_block_pattern_luma & (1 << cbp_index) == 0 {
                reconstructed_blocks[block_index].residual = [0; 16];
                continue;
            }
            let cond_left = derive_intra4x4_luma_cond_term(
                block_index,
                block_x,
                block_y,
                true,
                &result.coded_block_flags,
                left_edge,
            );
            let cond_top = derive_intra4x4_luma_cond_term(
                block_index,
                block_x,
                block_y,
                false,
                &result.coded_block_flags,
                top_edge,
            );
            let (scan_levels, coded) =
                trial.decode_residual_block(0, cond_left, cond_top, &mut trial_contexts)?;
            result.coded_block_flags[block_index] = coded;
            if !coded {
                reconstructed_blocks[block_index].residual = [0; 16];
                continue;
            }
            let raster_levels = place_luma4x4_scan_levels(&scan_levels);
            let residual = crate::reconstruction::reconstruct_luma4x4_residual(
                &raster_levels,
                scaling_list,
                qpy,
            )?;
            reconstructed_blocks[block_index].residual = residual;
            result.residuals[block_index] = residual;
        }
        result.samples =
            crate::reconstruction::reconstruct_luma_intra4x4_macroblock(&reconstructed_blocks)?;
        self.bits = trial.bits;
        self.code_range = trial.code_range;
        self.code_offset = trial.code_offset;
        self.terminated = trial.terminated;
        *contexts = trial_contexts;
        Ok(result)
    }

    /// Decodes an Intra_NxN macroblock with the 8x8 transform flag set and reconstructs luma.
    /// Per-block sample references must already be gathered and filtered per clause 8.3.2.2.1.
    pub fn decode_intra_nxn_8x8_luma_macroblock(
        &mut self,
        contexts: &mut [CabacContextModel; CABAC_CONTEXT_COUNT],
        transform_8x8_mode_enabled: bool,
        left_has_8x8_transform: bool,
        top_has_8x8_transform: bool,
        coded_block_pattern_luma: u8,
        qpy: i32,
        scaling_list: &[u8; 64],
        top_modes: &[u8; 2],
        left_modes: &[u8; 2],
        top_mode_available: bool,
        left_mode_available: bool,
        blocks: &[crate::LumaIntra8x8Block; 4],
    ) -> io::Result<Intra8x8LumaMacroblockResult> {
        if !transform_8x8_mode_enabled || coded_block_pattern_luma > 15 {
            return Err(invalid(
                "Intra_NxN 8x8 macroblock inputs are unsupported or invalid",
            ));
        }
        crate::reconstruction::inverse_scale_luma8x8(&[0; 64], scaling_list, qpy)?;
        let mut trial = Self {
            bits: self.bits.clone(),
            code_range: self.code_range,
            code_offset: self.code_offset,
            terminated: self.terminated,
        };
        let mut trial_contexts = *contexts;
        let mut transform_contexts = [
            trial_contexts[399],
            trial_contexts[400],
            trial_contexts[401],
        ];
        let transform_size_8x8 = trial.decode_transform_size_8x8_flag(
            &mut transform_contexts,
            left_has_8x8_transform,
            top_has_8x8_transform,
        )?;
        trial_contexts[399..402].copy_from_slice(&transform_contexts);
        if !transform_size_8x8 {
            return Err(invalid(
                "Intra_NxN 8x8 residual path requires transform_size_8x8_flag",
            ));
        }
        let mut mode_contexts = [trial_contexts[68], trial_contexts[69]];
        let mut result = Intra8x8LumaMacroblockResult {
            transform_size_8x8: true,
            ..Intra8x8LumaMacroblockResult::default()
        };
        for block_index in 0..4 {
            let column = block_index % 2;
            let row = block_index / 2;
            let (left_available, left_mode) = if column > 0 {
                (true, result.modes[block_index - 1])
            } else {
                (left_mode_available, left_modes[row])
            };
            let (top_available, top_mode) = if row > 0 {
                (true, result.modes[block_index - 2])
            } else {
                (top_mode_available, top_modes[column])
            };
            let predicted_mode = if left_available && top_available {
                left_mode.min(top_mode)
            } else {
                2
            };
            result.modes[block_index] =
                trial.decode_intra4x4_pred_mode(predicted_mode, &mut mode_contexts)?;
        }
        trial_contexts[68..70].copy_from_slice(&mode_contexts);
        let mut reconstructed_blocks = *blocks;
        for block_index in 0..4 {
            reconstructed_blocks[block_index].mode = result.modes[block_index];
            if coded_block_pattern_luma & (1 << block_index) == 0 {
                reconstructed_blocks[block_index].residual = [0; 64];
                continue;
            }
            let scan_levels = trial.decode_luma8x8_residual_block(&mut trial_contexts)?;
            let raster_levels = place_luma8x8_scan_levels(&scan_levels);
            let scaled =
                crate::reconstruction::inverse_scale_luma8x8(&raster_levels, scaling_list, qpy)?;
            let residual = crate::reconstruction::inverse_transform_luma8x8(&scaled);
            reconstructed_blocks[block_index].residual = residual;
            result.residuals[block_index] = residual;
        }
        result.samples =
            crate::reconstruction::reconstruct_luma_intra8x8_macroblock(&reconstructed_blocks)?;
        self.bits = trial.bits;
        self.code_range = trial.code_range;
        self.code_offset = trial.code_offset;
        self.terminated = trial.terminated;
        *contexts = trial_contexts;
        Ok(result)
    }

    /// Decodes P/SP mb_type (0-3, intra 5-30) or B mb_type (0-22, intra 23-48) from
    /// whole-slice contexts (Tables 9-37, 9-39, 9-41). B neighbor flags report available
    /// B_Skip/B_Direct_16x16 neighbors (clause 9.3.3.1.1.3); P slices ignore neighbors.
    pub fn decode_inter_mb_type(
        &mut self,
        slice_type: u8,
        contexts: &mut [CabacContextModel],
        left_available: bool,
        top_available: bool,
        left_b_skip_or_direct: bool,
        top_b_skip_or_direct: bool,
    ) -> io::Result<u8> {
        if slice_type > 9 || !matches!(slice_type % 5, 0 | 1) {
            return Err(invalid(
                "CABAC inter mb_type is unsupported for this slice type",
            ));
        }
        if contexts.len() != CABAC_CONTEXT_COUNT {
            return Err(invalid("inter mb_type requires 460 slice contexts"));
        }
        let mut trial = Self {
            bits: self.bits.clone(),
            code_range: self.code_range,
            code_offset: self.code_offset,
            terminated: self.terminated,
        };
        let mut trial_contexts = contexts.to_vec();
        let mb_type = if slice_type.is_multiple_of(5) {
            trial.decode_p_mb_type(&mut trial_contexts)?
        } else {
            let increment = usize::from(left_available && !left_b_skip_or_direct)
                + usize::from(top_available && !top_b_skip_or_direct);
            trial.decode_b_mb_type(&mut trial_contexts, increment)?
        };
        self.bits = trial.bits;
        self.code_range = trial.code_range;
        self.code_offset = trial.code_offset;
        self.terminated = trial.terminated;
        contexts.copy_from_slice(&trial_contexts);
        Ok(mb_type)
    }

    fn decode_bins(
        &mut self,
        contexts: &mut [CabacContextModel],
        ctx_idx: usize,
        count: usize,
    ) -> io::Result<u8> {
        let mut value = 0;
        for _ in 0..count {
            value = (value << 1) | u8::from(self.decode_bin(&mut contexts[ctx_idx])?);
        }
        Ok(value)
    }

    fn decode_p_mb_type(&mut self, contexts: &mut [CabacContextModel]) -> io::Result<u8> {
        if self.decode_bin(&mut contexts[14])? {
            return Ok(5 + self.decode_intra_mb_type_suffix(contexts, 17)?);
        }
        let second = self.decode_bin(&mut contexts[15])?;
        let third = self.decode_bin(&mut contexts[if second { 17 } else { 16 }])?;
        Ok(match (second, third) {
            (true, true) => 1,
            (true, false) => 2,
            (false, true) => 3,
            (false, false) => 0,
        })
    }

    fn decode_b_mb_type(
        &mut self,
        contexts: &mut [CabacContextModel],
        increment: usize,
    ) -> io::Result<u8> {
        if self.decode_bins(contexts, 27 + increment, 1)? == 0 {
            return Ok(0);
        }
        if self.decode_bins(contexts, 30, 1)? == 0 {
            return Ok(1 + self.decode_bins(contexts, 32, 1)?);
        }
        let third = self.decode_bins(contexts, 31, 1)?;
        let rest = self.decode_bins(contexts, 32, 3)?;
        Ok(match (third, rest) {
            (0, _) => 3 + rest,
            (_, 0b101) => 23 + self.decode_intra_mb_type_suffix(contexts, 32)?,
            (_, 0b110) => 11,
            (_, 0b111) => 22,
            _ => 12 + (rest << 1) + self.decode_bins(contexts, 32, 1)?,
        })
    }

    /// Decodes the Table 9-36 suffix of an intra mb_type in P (offset 17) or B (offset 32) slices.
    fn decode_intra_mb_type_suffix(
        &mut self,
        contexts: &mut [CabacContextModel],
        offset: usize,
    ) -> io::Result<u8> {
        if self.decode_bins(contexts, offset, 1)? == 0 {
            return Ok(0);
        }
        if self.decode_terminate_bin()? {
            return Ok(25);
        }
        let mut mb_type = 1 + 12 * self.decode_bins(contexts, offset + 1, 1)?;
        if self.decode_bins(contexts, offset + 2, 1)? == 1 {
            mb_type += 4 + 4 * self.decode_bins(contexts, offset + 2, 1)?;
        }
        Ok(mb_type + self.decode_bins(contexts, offset + 3, 2)?)
    }

    /// Decodes sub_mb_type for P/SP (0-3, ctxIdx 21-23) or B (0-12, ctxIdx 36-39) slices.
    pub fn decode_sub_mb_type(
        &mut self,
        slice_type: u8,
        contexts: &mut [CabacContextModel],
    ) -> io::Result<u8> {
        if slice_type > 9 || !matches!(slice_type % 5, 0 | 1) {
            return Err(invalid(
                "CABAC sub_mb_type is unsupported for this slice type",
            ));
        }
        if contexts.len() != CABAC_CONTEXT_COUNT {
            return Err(invalid("sub_mb_type requires 460 slice contexts"));
        }
        let mut trial = Self {
            bits: self.bits.clone(),
            code_range: self.code_range,
            code_offset: self.code_offset,
            terminated: self.terminated,
        };
        let mut c = contexts.to_vec();
        let sub_type = if slice_type.is_multiple_of(5) {
            if trial.decode_bins(&mut c, 21, 1)? == 1 {
                0
            } else if trial.decode_bins(&mut c, 22, 1)? == 0 {
                1
            } else {
                3 - trial.decode_bins(&mut c, 23, 1)?
            }
        } else if trial.decode_bins(&mut c, 36, 1)? == 0 {
            0
        } else if trial.decode_bins(&mut c, 37, 1)? == 0 {
            1 + trial.decode_bins(&mut c, 39, 1)?
        } else if trial.decode_bins(&mut c, 38, 1)? == 0 {
            3 + trial.decode_bins(&mut c, 39, 2)?
        } else if trial.decode_bins(&mut c, 39, 1)? == 1 {
            11 + trial.decode_bins(&mut c, 39, 1)?
        } else {
            7 + trial.decode_bins(&mut c, 39, 2)?
        };
        self.bits = trial.bits;
        self.code_range = trial.code_range;
        self.code_offset = trial.code_offset;
        self.terminated = trial.terminated;
        contexts.copy_from_slice(&c);
        Ok(sub_type)
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

    /// Decodes all luma Intra_4x4 prediction modes in syntax scan order transactionally.
    pub fn decode_intra4x4_pred_modes(
        &mut self,
        contexts: &mut [CabacContextModel; 2],
        top_modes: &[u8; 4],
        left_modes: &[u8; 4],
        top_available: bool,
        left_available: bool,
    ) -> io::Result<[u8; 16]> {
        let mut trial = Self {
            bits: self.bits.clone(),
            code_range: self.code_range,
            code_offset: self.code_offset,
            terminated: self.terminated,
        };
        let mut trial_contexts = *contexts;
        let mut modes = [0; 16];
        for block_index in 0..16 {
            let predicted_mode = derive_intra4x4_predicted_mode(
                &modes,
                block_index,
                top_modes,
                left_modes,
                top_available,
                left_available,
            )?;
            modes[block_index] =
                trial.decode_intra4x4_pred_mode(predicted_mode, &mut trial_contexts)?;
        }
        self.bits = trial.bits;
        self.code_range = trial.code_range;
        self.code_offset = trial.code_offset;
        self.terminated = trial.terminated;
        *contexts = trial_contexts;
        Ok(modes)
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
        let context_index = u8::from(neighbor_magnitude >= 3) + u8::from(neighbor_magnitude >= 33);
        self.decode_motion_vector_difference_with_context(context_index, contexts)
    }

    /// Derives absMvdComp and the CABAC context increment before decoding one partition component.
    pub fn decode_motion_vector_difference_for_partition(
        &mut self,
        component: u8,
        left: CabacInterNeighbor,
        top: CabacInterNeighbor,
        mbaff_frame: bool,
        current_is_field: bool,
        contexts: &mut [CabacContextModel; 7],
    ) -> io::Result<i32> {
        let context_increment = derive_cabac_mvd_context_increment(
            left,
            top,
            component,
            mbaff_frame,
            current_is_field,
        )?;
        self.decode_motion_vector_difference_with_context(context_increment, contexts)
    }

    fn decode_motion_vector_difference_with_context(
        &mut self,
        context_index: u8,
        contexts: &mut [CabacContextModel; 7],
    ) -> io::Result<i32> {
        if context_index > 2 {
            return Err(invalid("MVD context increment is outside [0,2]"));
        }
        let mut context_index = usize::from(context_index);
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

    /// Decodes coded_block_flag and, when set, one luma 4x4 residual block transactionally.
    pub fn decode_luma4x4_residual_block_with_flag(
        &mut self,
        left_nonzero: u8,
        top_nonzero: u8,
        coded_flag_contexts: &mut [CabacContextModel; 4],
        significant_contexts: &mut [CabacContextModel; 15],
        last_contexts: &mut [CabacContextModel; 15],
        coefficient_contexts: &mut [CabacContextModel; 10],
    ) -> io::Result<([i32; 16], bool)> {
        if coded_flag_contexts
            .iter()
            .chain(significant_contexts.iter())
            .chain(last_contexts.iter())
            .chain(coefficient_contexts.iter())
            .any(|model| model.state_index >= 64)
        {
            return Err(invalid("luma4x4 residual syntax requires valid contexts"));
        }
        let mut trial = Self {
            bits: self.bits.clone(),
            code_range: self.code_range,
            code_offset: self.code_offset,
            terminated: self.terminated,
        };
        let mut trial_coded_flag = *coded_flag_contexts;
        let mut trial_significant = *significant_contexts;
        let mut trial_last = *last_contexts;
        let mut trial_coefficients = *coefficient_contexts;
        let coded = trial.decode_luma4x4_coded_block_flag(
            left_nonzero,
            top_nonzero,
            &mut trial_coded_flag,
        )?;
        let levels = if coded {
            trial.decode_luma4x4_residual_block(
                &mut trial_significant,
                &mut trial_last,
                &mut trial_coefficients,
            )?
        } else {
            [0; 16]
        };

        self.bits = trial.bits;
        self.code_range = trial.code_range;
        self.code_offset = trial.code_offset;
        self.terminated = trial.terminated;
        *coded_flag_contexts = trial_coded_flag;
        *significant_contexts = trial_significant;
        *last_contexts = trial_last;
        *coefficient_contexts = trial_coefficients;
        Ok((levels, coded))
    }

    /// Decodes one 4:2:0 chroma AC block, scanning coefficient positions 1 through 15.
    pub fn decode_chroma4x4_ac_residual_block(
        &mut self,
        left_has_nonzero: bool,
        top_has_nonzero: bool,
        coded_flag_contexts: &mut [CabacContextModel; 4],
        significant_contexts: &mut [CabacContextModel; 15],
        last_contexts: &mut [CabacContextModel; 15],
        coefficient_contexts: &mut [CabacContextModel; 10],
    ) -> io::Result<([i32; 16], bool)> {
        if coded_flag_contexts
            .iter()
            .chain(significant_contexts.iter())
            .chain(last_contexts.iter())
            .chain(coefficient_contexts.iter())
            .any(|model| model.state_index >= 64)
        {
            return Err(invalid("chroma AC residual syntax requires valid contexts"));
        }
        let mut trial = Self {
            bits: self.bits.clone(),
            code_range: self.code_range,
            code_offset: self.code_offset,
            terminated: self.terminated,
        };
        let mut trial_coded_flag = *coded_flag_contexts;
        let mut trial_significant = *significant_contexts;
        let mut trial_last = *last_contexts;
        let mut trial_coefficients = *coefficient_contexts;
        let coded = trial.decode_luma4x4_coded_block_flag(
            u8::from(left_has_nonzero),
            u8::from(top_has_nonzero),
            &mut trial_coded_flag,
        )?;
        let mut raster_levels = [0; 16];
        if coded {
            let mut significance = [false; 16];
            let mut last_found = false;
            for scan_index in 0..14 {
                if !trial.decode_bin(&mut trial_significant[scan_index])? {
                    continue;
                }
                significance[scan_index + 1] = true;
                if trial.decode_bin(&mut trial_last[scan_index])? {
                    last_found = true;
                    break;
                }
            }
            if !last_found {
                significance[15] = true;
            }
            let scan_levels =
                trial.decode_luma4x4_residual_levels(&significance, &mut trial_coefficients)?;
            let mut ac_scan_levels = [0; 15];
            ac_scan_levels.copy_from_slice(&scan_levels[1..]);
            raster_levels = place_chroma4x4_scan_levels(0, &ac_scan_levels);
        }
        self.bits = trial.bits;
        self.code_range = trial.code_range;
        self.code_offset = trial.code_offset;
        self.terminated = trial.terminated;
        *coded_flag_contexts = trial_coded_flag;
        *significant_contexts = trial_significant;
        *last_contexts = trial_last;
        *coefficient_contexts = trial_coefficients;
        Ok((raster_levels, coded))
    }

    /// Decodes coded_block_flag and residual_block_cabac for ctxBlockCat 0-4 (4:2:0),
    /// updating the whole-slice ctxIdx 0-459 contexts transactionally.
    /// Levels are in coefficient-list order: index i is scan position i for categories 0, 2,
    /// and 3, and i+1 for AC categories 1 and 4. Entries past maxNumCoeff are zero.
    pub fn decode_residual_block(
        &mut self,
        ctx_block_cat: u8,
        cond_term_flag_a: bool,
        cond_term_flag_b: bool,
        contexts: &mut [CabacContextModel],
    ) -> io::Result<([i32; 16], bool)> {
        if contexts.len() != CABAC_CONTEXT_COUNT {
            return Err(invalid("residual block requires 460 slice contexts"));
        }
        if ctx_block_cat > 4 {
            return Err(invalid("CABAC syntax is unsupported for this ctxBlockCat"));
        }
        let (coded_base, significant_base, last_base, abs_base) =
            cabac_residual_context_bases(ctx_block_cat)?;
        let coded_base = coded_base.ok_or_else(|| invalid("coded_block_flag is not coded"))?;
        let max_num_coeff = RESIDUAL_MAX_NUM_COEFF[usize::from(ctx_block_cat)];
        let mut trial = Self {
            bits: self.bits.clone(),
            code_range: self.code_range,
            code_offset: self.code_offset,
            terminated: self.terminated,
        };
        let mut trial_contexts = contexts.to_vec();
        let mut levels = [0_i32; 16];
        let coded_inc = usize::from(cond_term_flag_a) + 2 * usize::from(cond_term_flag_b);
        let coded = trial.decode_bin(&mut trial_contexts[coded_base + coded_inc])?;
        if coded {
            let mut significant = [false; 16];
            let mut last_found = false;
            for (index, is_significant) in
                significant.iter_mut().enumerate().take(max_num_coeff - 1)
            {
                // Chroma DC uses Min(numDecodAbsLevel / NumC8x8, 2) with NumC8x8 = 1.
                let increment = if ctx_block_cat == 3 {
                    index.min(2)
                } else {
                    index
                };
                if !trial.decode_bin(&mut trial_contexts[significant_base + increment])? {
                    continue;
                }
                *is_significant = true;
                if trial.decode_bin(&mut trial_contexts[last_base + increment])? {
                    last_found = true;
                    break;
                }
            }
            if !last_found {
                significant[max_num_coeff - 1] = true;
            }
            let greater_cap = if ctx_block_cat == 3 { 3 } else { 4 };
            trial.decode_coefficient_levels(
                &mut trial_contexts[abs_base..abs_base + 10],
                greater_cap,
                &significant[..max_num_coeff],
                &mut levels[..max_num_coeff],
            )?;
        }
        self.bits = trial.bits;
        self.code_range = trial.code_range;
        self.code_offset = trial.code_offset;
        self.terminated = trial.terminated;
        contexts.copy_from_slice(&trial_contexts);
        Ok((levels, coded))
    }

    /// Decodes residual_block_cabac for ctxBlockCat 5 in a frame macroblock.
    /// coded_block_flag is not parsed because it is inferred to be 1 when ChromaArrayType != 3;
    /// call this only for 8x8 blocks whose coded_block_pattern bit is set.
    /// Returns 64 levels in 8x8 frame-scan list order; contexts update transactionally.
    pub fn decode_luma8x8_residual_block(
        &mut self,
        contexts: &mut [CabacContextModel],
    ) -> io::Result<[i32; 64]> {
        if contexts.len() != CABAC_CONTEXT_COUNT {
            return Err(invalid("residual block requires 460 slice contexts"));
        }
        let (_, significant_base, last_base, abs_base) = cabac_residual_context_bases(5)?;
        let mut trial = Self {
            bits: self.bits.clone(),
            code_range: self.code_range,
            code_offset: self.code_offset,
            terminated: self.terminated,
        };
        let mut trial_contexts = contexts.to_vec();
        let mut significant = [false; 64];
        let mut last_found = false;
        for (index, is_significant) in significant.iter_mut().enumerate().take(63) {
            let significant_ctx = significant_base + LUMA8X8_FRAME_SIGNIFICANT_INC[index];
            if !trial.decode_bin(&mut trial_contexts[significant_ctx])? {
                continue;
            }
            *is_significant = true;
            if trial.decode_bin(&mut trial_contexts[last_base + LUMA8X8_LAST_INC[index]])? {
                last_found = true;
                break;
            }
        }
        if !last_found {
            significant[63] = true;
        }
        let mut levels = [0_i32; 64];
        trial.decode_coefficient_levels(
            &mut trial_contexts[abs_base..abs_base + 10],
            4,
            &significant,
            &mut levels,
        )?;
        self.bits = trial.bits;
        self.code_range = trial.code_range;
        self.code_offset = trial.code_offset;
        self.terminated = trial.terminated;
        contexts.copy_from_slice(&trial_contexts);
        Ok(levels)
    }

    /// Decodes signed levels in reverse list order with clause 9.3.3.1.3 contexts.
    fn decode_coefficient_levels(
        &mut self,
        abs_contexts: &mut [CabacContextModel],
        greater_cap: usize,
        significant: &[bool],
        levels: &mut [i32],
    ) -> io::Result<()> {
        let (mut equal_one, mut greater_one) = (0_usize, 0_usize);
        for index in (0..significant.len()).rev() {
            if !significant[index] {
                continue;
            }
            let first_inc = if greater_one == 0 {
                (1 + equal_one).min(4)
            } else {
                0
            };
            let greater_inc = 5 + greater_one.min(greater_cap);
            let (first_bank, greater_bank) = abs_contexts.split_at_mut(5);
            let level_minus1 = self.decode_coeff_abs_level_minus1(
                &mut first_bank[first_inc],
                &mut greater_bank[greater_inc - 5],
            )?;
            levels[index] = self.decode_coeff_sign(level_minus1)?;
            if level_minus1 == 0 {
                equal_one += 1;
            } else {
                greater_one += 1;
            }
        }
        Ok(())
    }

    /// Decodes and reconstructs one luma 4x4 residual block in sample space.
    pub fn decode_and_reconstruct_luma4x4_residual(
        &mut self,
        left_nonzero: u8,
        top_nonzero: u8,
        coded_flag_contexts: &mut [CabacContextModel; 4],
        significant_contexts: &mut [CabacContextModel; 15],
        last_contexts: &mut [CabacContextModel; 15],
        coefficient_contexts: &mut [CabacContextModel; 10],
        scaling_list: &[u8; 16],
        qpy: i32,
    ) -> io::Result<([i64; 16], bool)> {
        crate::reconstruction::inverse_scale_luma4x4(&[0; 16], scaling_list, qpy)?;
        let (levels, coded) = self.decode_luma4x4_residual_block_with_flag(
            left_nonzero,
            top_nonzero,
            coded_flag_contexts,
            significant_contexts,
            last_contexts,
            coefficient_contexts,
        )?;
        if !coded {
            return Ok(([0; 16], false));
        }
        Ok((
            crate::reconstruction::reconstruct_luma4x4_residual(&levels, scaling_list, qpy)?,
            true,
        ))
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

/// Maps frame-scan luma 8x8 coefficient levels into raster order.
pub fn place_luma8x8_scan_levels(scan_levels: &[i32; 64]) -> [i32; 64] {
    let mut raster_levels = [0; 64];
    for (scan_index, raster_index) in LUMA8X8_SCAN_TO_RASTER.into_iter().enumerate() {
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
    fn slice_contexts_match_syntax_initializers() {
        let states = |models: &[CabacContextModel]| {
            models
                .iter()
                .map(|model| (model.state_index(), model.mps()))
                .collect::<Vec<_>>()
        };
        let contexts = new_cabac_slice_contexts(7, 3, 26).unwrap();
        assert_eq!(contexts.len(), CABAC_CONTEXT_COUNT);
        for (first, models) in [
            (3, states(&new_cabac_i_intra_mb_type_contexts(26).unwrap())),
            (60, states(&new_cabac_i_mb_qp_delta_contexts(26).unwrap())),
            (
                64,
                states(&new_cabac_i_intra_chroma_pred_mode_contexts(26).unwrap()),
            ),
            (
                68,
                states(&new_cabac_i_intra4x4_pred_mode_contexts(26).unwrap()),
            ),
            (
                73,
                states(&new_cabac_i_luma_coded_block_pattern_contexts(26).unwrap()),
            ),
            (
                77,
                states(&new_cabac_i_chroma_coded_block_pattern_contexts(26).unwrap()),
            ),
            (
                93,
                states(&new_cabac_i_luma4x4_coded_block_flag_contexts(26).unwrap()),
            ),
            (
                399,
                states(&new_cabac_i_transform_size_8x8_contexts(26).unwrap()),
            ),
        ] {
            assert_eq!(states(&contexts[first..first + models.len()]), models);
        }
        for cabac_init_idc in 0..=2 {
            let contexts = new_cabac_slice_contexts(5, cabac_init_idc, 30).unwrap();
            let (mvd_x, mvd_y, ref_idx) =
                new_cabac_inter_prediction_contexts(cabac_init_idc, 30).unwrap();
            let p_mb_type = new_cabac_p_inter_mb_type_contexts(cabac_init_idc, 30).unwrap();
            assert_eq!(states(&contexts[14..18]), states(&p_mb_type));
            assert_eq!(states(&contexts[40..47]), states(&mvd_x));
            assert_eq!(states(&contexts[47..54]), states(&mvd_y));
            assert_eq!(states(&contexts[54..60]), states(&ref_idx));
        }
    }

    #[test]
    fn slice_contexts_use_standard_columns() {
        for (slice_type, cabac_init_idc, ctx_idx, expected) in [
            (2, 0, 85, (31, true)),
            (2, 0, 227, (2, true)),
            (2, 0, 402, (28, true)),
            (0, 0, 105, (17, true)),
            (1, 1, 30, (10, false)),
            (6, 2, 459, (32, true)),
        ] {
            let model = new_cabac_slice_contexts(slice_type, cabac_init_idc, 26).unwrap()[ctx_idx];
            assert_eq!((model.state_index(), model.mps()), expected);
        }
        for (slice_type, cabac_init_idc, slice_qpy) in
            [(10, 0, 26), (0, 3, 26), (1, 3, 26), (2, 0, 52)]
        {
            assert!(new_cabac_slice_contexts(slice_type, cabac_init_idc, slice_qpy).is_err());
        }
    }

    #[test]
    fn residual_context_bases_follow_tables_934_and_940() {
        let bases: Vec<_> = (0..6)
            .map(|category| cabac_residual_context_bases(category).unwrap())
            .collect();
        assert_eq!(
            bases,
            [
                (Some(85), 105, 166, 227),
                (Some(89), 120, 181, 237),
                (Some(93), 134, 195, 247),
                (Some(97), 149, 210, 257),
                (Some(101), 152, 213, 266),
                (None, 402, 417, 426),
            ]
        );
        assert!(cabac_residual_context_bases(6).is_err());
    }

    fn forced_residual_contexts(mps_true: &[usize]) -> Vec<CabacContextModel> {
        (0..CABAC_CONTEXT_COUNT)
            .map(|index| CabacContextModel {
                state_index: 61,
                value_mps: mps_true.contains(&index),
            })
            .collect()
    }

    #[test]
    fn coded_block_flag_cond_term_follows_clause_9_3_3_1_1_9() {
        for (arguments, expected) in [
            ((false, true, false, false, false), true),
            ((false, false, false, false, false), false),
            ((true, false, true, false, false), true),
            ((true, true, false, false, true), false),
            ((true, false, false, true, false), false),
            ((true, false, false, true, true), true),
        ] {
            let (available, intra, ipcm, trans_available, trans_coded) = arguments;
            assert_eq!(
                derive_coded_block_flag_cond_term(
                    available,
                    intra,
                    ipcm,
                    trans_available,
                    trans_coded
                ),
                expected
            );
        }
    }

    // With a zero offset every regular bin decodes as its MPS and every bypass bin as 0.
    #[test]
    fn residual_block_chroma_dc_forced_bins() {
        let data = [0_u8; 16];
        let mut contexts = forced_residual_contexts(&[98, 149, 151, 212, 259]);
        let mut decoder = CabacArithmeticDecoder::new(&data).unwrap();
        let (levels, coded) = decoder
            .decode_residual_block(3, true, false, &mut contexts)
            .unwrap();
        assert!(coded);
        assert_eq!(levels, [2, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0]);
        let used = [98, 149, 210, 150, 151, 212, 258, 259, 262];
        for (index, model) in contexts.iter().enumerate() {
            let expected = if used.contains(&index) { 62 } else { 61 };
            assert_eq!(model.state_index(), expected, "ctxIdx {index}");
        }

        let data = [0_u8; 64];
        let mut mps_true = vec![97, 149, 150, 151];
        mps_true.extend(257..267);
        let mut contexts = forced_residual_contexts(&mps_true);
        let mut decoder = CabacArithmeticDecoder::new(&data).unwrap();
        let (levels, coded) = decoder
            .decode_residual_block(3, false, false, &mut contexts)
            .unwrap();
        assert!(coded);
        assert_eq!(levels[..4], [15, 15, 15, 15]);
        // Category 3 caps the greater-than-one context at ctxIdx 265, leaving 266 to category 4.
        assert_eq!(
            [257, 265, 266].map(|index| contexts[index].state_index()),
            [62, 62, 61]
        );
    }

    #[test]
    fn residual_block_uncoded_and_category_range() {
        let data = [0_u8; 4];
        let mut contexts = forced_residual_contexts(&[]);
        let mut decoder = CabacArithmeticDecoder::new(&data).unwrap();
        assert_eq!(
            decoder
                .decode_residual_block(0, true, true, &mut contexts)
                .unwrap(),
            ([0; 16], false)
        );
        assert_eq!(contexts[88].state_index(), 62);
        assert!(decoder
            .decode_residual_block(5, false, false, &mut contexts)
            .is_err());
        assert!(decoder
            .decode_residual_block(0, false, false, &mut contexts[..459])
            .is_err());
    }

    #[test]
    fn residual_block_truncation_is_transactional() {
        let mut mps_true = vec![85];
        mps_true.extend(105..120);
        mps_true.extend(227..237);
        let mut contexts = forced_residual_contexts(&mps_true);
        let original: Vec<_> = contexts
            .iter()
            .map(|model| (model.state_index(), model.mps()))
            .collect();
        let data = [0_u8; 2];
        let mut decoder = CabacArithmeticDecoder::new(&data).unwrap();
        assert!(decoder
            .decode_residual_block(0, false, false, &mut contexts)
            .is_err());
        let after: Vec<_> = contexts
            .iter()
            .map(|model| (model.state_index(), model.mps()))
            .collect();
        assert_eq!(after, original);
        assert_eq!((decoder.code_range(), decoder.code_offset()), (510, 0));
        assert_eq!(decoder.bits.read_bits(7).unwrap(), 0);
    }

    #[test]
    fn luma8x8_residual_block_follows_table_9_43() {
        // Only significance ctxIdxInc 7 (ctxIdx 409) has MPS 1, so significant positions reveal Table 9-43.
        let positions = [23, 24, 25, 31, 32, 39, 63];
        let data = [0_u8; 64];
        let mut contexts = forced_residual_contexts(&[409]);
        let levels = CabacArithmeticDecoder::new(&data)
            .unwrap()
            .decode_luma8x8_residual_block(&mut contexts)
            .unwrap();
        for (index, level) in levels.iter().enumerate() {
            assert_eq!(
                *level,
                i32::from(positions.contains(&index)),
                "level {index}"
            );
        }
        let mut used: Vec<usize> = (402..417).collect();
        used.extend([419, 420, 427, 428, 429, 430]);
        for (index, model) in contexts.iter().enumerate() {
            let expected = if used.contains(&index) { 62 } else { 61 };
            assert_eq!(model.state_index(), expected, "ctxIdx {index}");
        }

        let data = [0_u8; 256];
        let mut mps_true = vec![409];
        mps_true.extend(426..436);
        let mut contexts = forced_residual_contexts(&mps_true);
        let levels = CabacArithmeticDecoder::new(&data)
            .unwrap()
            .decode_luma8x8_residual_block(&mut contexts)
            .unwrap();
        for (index, level) in levels.iter().enumerate() {
            let expected = if positions.contains(&index) { 15 } else { 0 };
            assert_eq!(*level, expected, "level {index}");
        }
        for (index, model) in contexts.iter().enumerate().take(436).skip(426) {
            let expected = if (428..=430).contains(&index) { 61 } else { 62 };
            assert_eq!(model.state_index(), expected, "ctxIdx {index}");
        }
    }

    #[test]
    fn luma8x8_residual_block_last_flag_and_truncation() {
        let data = [0_u8; 8];
        let mut contexts = forced_residual_contexts(&[402, 417]);
        let levels = CabacArithmeticDecoder::new(&data)
            .unwrap()
            .decode_luma8x8_residual_block(&mut contexts)
            .unwrap();
        let mut expected = [0_i32; 64];
        expected[0] = 1;
        assert_eq!(levels, expected);

        let mut mps_true: Vec<usize> = (402..417).collect();
        mps_true.extend(426..436);
        let mut contexts = forced_residual_contexts(&mps_true);
        let original: Vec<_> = contexts
            .iter()
            .map(|model| (model.state_index(), model.mps()))
            .collect();
        let data = [0_u8; 2];
        let mut decoder = CabacArithmeticDecoder::new(&data).unwrap();
        assert!(decoder
            .decode_luma8x8_residual_block(&mut contexts)
            .is_err());
        let after: Vec<_> = contexts
            .iter()
            .map(|model| (model.state_index(), model.mps()))
            .collect();
        assert_eq!(after, original);
        assert_eq!((decoder.code_range(), decoder.code_offset()), (510, 0));
        assert_eq!(decoder.bits.bit_offset(), 9);
        assert!(decoder
            .decode_luma8x8_residual_block(&mut contexts[..459])
            .is_err());
    }

    #[test]
    fn residual_block_matches_bank_decoders() {
        let states = |models: &[CabacContextModel]| {
            models
                .iter()
                .map(|model| (model.state_index(), model.mps()))
                .collect::<Vec<_>>()
        };
        let slice_contexts = new_cabac_slice_contexts(2, 0, 28).unwrap();
        let patterns: [[u8; 20]; 3] = [
            [
                0x5a, 0x3c, 0x91, 0x07, 0xe2, 0x48, 0xb3, 0x6f, 0x12, 0xc5, 0x7e, 0x29, 0x84, 0xd1,
                0x3b, 0xf6, 0x55, 0xaa, 0x0f, 0xf0,
            ],
            [
                0x00, 0x7f, 0x10, 0x20, 0x40, 0x80, 0xff, 0x01, 0x33, 0xcc, 0x99, 0x66, 0x11, 0xee,
                0x22, 0xdd, 0x44, 0xbb, 0x88, 0x77,
            ],
            [
                0x1f, 0xe0, 0x3e, 0xc1, 0x7c, 0x83, 0xf8, 0x07, 0xab, 0xcd, 0xef, 0x01, 0x23, 0x45,
                0x67, 0x89, 0x9a, 0xbc, 0xde, 0xf0,
            ],
        ];
        for category in [0_u8, 1, 2, 4] {
            let (coded_base, significant_base, last_base, abs_base) =
                cabac_residual_context_bases(category).unwrap();
            let coded_base = coded_base.unwrap();
            let mut coded_cases = 0;
            for data in &patterns {
                for cond_terms in 0..4 {
                    let (cond_a, cond_b) = (cond_terms & 1 != 0, cond_terms & 2 != 0);
                    let mut contexts = slice_contexts.clone();
                    let mut generic = CabacArithmeticDecoder::new(data).unwrap();
                    let generic_result =
                        generic.decode_residual_block(category, cond_a, cond_b, &mut contexts);

                    let mut coded_flag: [CabacContextModel; 4] = slice_contexts
                        [coded_base..coded_base + 4]
                        .try_into()
                        .unwrap();
                    let mut significant: [CabacContextModel; 15] = slice_contexts
                        [significant_base..significant_base + 15]
                        .try_into()
                        .unwrap();
                    let mut last: [CabacContextModel; 15] = slice_contexts
                        [last_base..last_base + 15]
                        .try_into()
                        .unwrap();
                    let mut coefficients: [CabacContextModel; 10] =
                        slice_contexts[abs_base..abs_base + 10].try_into().unwrap();
                    let mut bank = CabacArithmeticDecoder::new(data).unwrap();
                    let bank_result = if category == 0 || category == 2 {
                        bank.decode_luma4x4_residual_block_with_flag(
                            u8::from(cond_a),
                            u8::from(cond_b),
                            &mut coded_flag,
                            &mut significant,
                            &mut last,
                            &mut coefficients,
                        )
                    } else {
                        bank.decode_chroma4x4_ac_residual_block(
                            cond_a,
                            cond_b,
                            &mut coded_flag,
                            &mut significant,
                            &mut last,
                            &mut coefficients,
                        )
                    };
                    assert_eq!(generic_result.is_ok(), bank_result.is_ok());
                    let (Ok((levels, coded)), Ok((want_raster, want_coded))) =
                        (generic_result, bank_result)
                    else {
                        continue;
                    };
                    coded_cases += usize::from(coded);
                    let got_raster = if category == 0 || category == 2 {
                        place_luma4x4_scan_levels(&levels)
                    } else {
                        place_chroma4x4_scan_levels(0, levels[..15].try_into().unwrap())
                    };
                    assert_eq!((got_raster, coded), (want_raster, want_coded));
                    assert_eq!(
                        (generic.code_range(), generic.code_offset()),
                        (bank.code_range(), bank.code_offset())
                    );
                    assert_eq!(generic.bits.bit_offset(), bank.bits.bit_offset());
                    for (base, models) in [
                        (coded_base, &coded_flag[..]),
                        (significant_base, &significant[..]),
                        (last_base, &last[..]),
                        (abs_base, &coefficients[..]),
                    ] {
                        assert_eq!(states(&contexts[base..base + models.len()]), states(models));
                    }
                }
            }
            assert!(
                coded_cases > 0,
                "ctxBlockCat {category} decoded no coded blocks"
            );
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
    fn decodes_reference_index_truncated_unary_values() {
        for (maximum, increment, mps, expected) in [
            (0, 0, [false; 6], 0),
            (3, 2, [false; 6], 0),
            (3, 1, [false, true, false, false, false, false], 1),
            (3, 0, [true, false, false, false, true, false], 2),
            (3, 3, [false, false, false, true, true, true], 3),
        ] {
            let mut decoder = decoder_with_state(&[], 510, 0);
            let mut contexts = [CabacContextModel {
                state_index: 63,
                value_mps: false,
            }; 6];
            for (context, value_mps) in contexts.iter_mut().zip(mps) {
                context.value_mps = value_mps;
            }
            assert_eq!(
                decoder
                    .decode_reference_index(maximum, increment, &mut contexts)
                    .unwrap(),
                expected
            );
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
    fn ipcm_intra_macroblock_places_samples_and_restarts_cabac() {
        let mut data = vec![0_u8; 1 + 256 + 64 + 64 + 2];
        data[0] = 0x80;
        data[1..257].fill(0x11);
        data[257..321].fill(0x22);
        data[321..385].fill(0x33);
        let mut decoder = decoder_with_state(&data, 510, 509);
        let mut contexts = [CabacContextModel::new(0, 63, 26).unwrap(); 8];
        let mut builder = crate::Yuv420FrameBuilder::new(1, 1, 0, 0, 0, 0).unwrap();
        decoder
            .decode_ipcm_intra_macroblock(
                2,
                &mut contexts,
                false,
                false,
                false,
                false,
                &mut builder,
                0,
            )
            .unwrap();
        assert_eq!((decoder.code_range(), decoder.code_offset()), (510, 0));
        assert!(!decoder.terminated);
        assert_eq!(decoder.bits.bit_offset(), 8 + 384 * 8 + 9);
        assert_eq!((contexts[0].state_index(), contexts[0].mps()), (0, true));
        let frame = builder.finish().unwrap();
        assert_eq!(frame.y.len(), 256);
        assert!(frame.y.iter().all(|&sample| sample == 0x11));
        assert_eq!(frame.u.len(), 64);
        assert!(frame.u.iter().all(|&sample| sample == 0x22));
        assert_eq!(frame.v.len(), 64);
        assert!(frame.v.iter().all(|&sample| sample == 0x33));
    }

    #[test]
    fn ipcm_intra_macroblock_failures_are_transactional() {
        let mut invalid_restart = vec![0x80];
        invalid_restart.extend([0x11; 384]);
        invalid_restart.extend([0xff, 0x00]);
        let cases: [(&str, &[u8]); 3] = [
            ("truncated samples", &[0x80, 0x11]),
            ("nonzero alignment", &[0xc0]),
            ("invalid restart offset", &invalid_restart),
        ];
        for (name, data) in cases {
            let mut decoder = decoder_with_state(data, 510, 509);
            let mut contexts = [CabacContextModel::new(0, 63, 26).unwrap(); 8];
            let original_contexts = contexts;
            let original_state = (
                decoder.code_range(),
                decoder.code_offset(),
                decoder.bits.bit_offset(),
            );
            let mut builder = crate::Yuv420FrameBuilder::new(1, 1, 0, 0, 0, 0).unwrap();
            assert!(
                decoder
                    .decode_ipcm_intra_macroblock(
                        2,
                        &mut contexts,
                        false,
                        false,
                        false,
                        false,
                        &mut builder,
                        0,
                    )
                    .is_err(),
                "{name} unexpectedly succeeded"
            );
            assert_eq!(
                (
                    decoder.code_range(),
                    decoder.code_offset(),
                    decoder.bits.bit_offset(),
                ),
                original_state,
                "{name} changed arithmetic state"
            );
            assert!(
                contexts
                    .iter()
                    .zip(original_contexts.iter())
                    .all(|(actual, original)| {
                        (actual.state_index(), actual.mps())
                            == (original.state_index(), original.mps())
                    }),
                "{name} changed contexts"
            );
            assert!(
                builder.finish().is_err(),
                "{name} placed a partial macroblock"
            );
        }
    }

    #[test]
    fn intra_nxn_4x4_luma_modes_and_assembly() {
        let data = [0_u8; 64];
        let mut decoder = decoder_with_state(&data, 510, 0);
        let mut contexts = [CabacContextModel {
            state_index: 63,
            value_mps: false,
        }; CABAC_CONTEXT_COUNT];
        let blocks = [crate::LumaIntra4x4Block {
            top_available: true,
            left_available: true,
            top_left_available: true,
            ..Default::default()
        }; 16];
        let result = decoder
            .decode_intra_nxn_4x4_luma_macroblock(
                &mut contexts,
                0,
                0,
                &[16; 16],
                &[2; 4],
                &[2; 4],
                true,
                true,
                CabacIntra4x4EdgeState::default(),
                CabacIntra4x4EdgeState::default(),
                &blocks,
            )
            .unwrap();
        assert_eq!(result.modes[0], 0);
        assert_eq!(result.coded_block_flags, [false; 16]);
        assert_eq!(result.samples, [0; 256]);
    }

    #[test]
    fn intra_nxn_4x4_luma_decodes_cbf_and_reconstructs_residual() {
        let data = [0_u8; 128];
        let mut decoder = decoder_with_state(&data, 510, 0);
        let mut contexts = [CabacContextModel {
            state_index: 63,
            value_mps: false,
        }; CABAC_CONTEXT_COUNT];
        contexts[88].value_mps = true;
        contexts[105].value_mps = true;
        contexts[166].value_mps = true;
        let blocks = [crate::LumaIntra4x4Block {
            top: [100; 8],
            left: [100; 8],
            top_left: 100,
            top_available: true,
            left_available: true,
            top_left_available: true,
            ..Default::default()
        }; 16];
        let result = decoder
            .decode_intra_nxn_4x4_luma_macroblock(
                &mut contexts,
                1,
                51,
                &[16; 16],
                &[2; 4],
                &[2; 4],
                true,
                true,
                CabacIntra4x4EdgeState::default(),
                CabacIntra4x4EdgeState::default(),
                &blocks,
            )
            .unwrap();
        assert!(result.coded_block_flags[0]);
        assert!(result.residuals[0].iter().any(|&sample| sample != 0));
    }

    #[test]
    fn intra_nxn_4x4_luma_failure_rolls_back() {
        let data = [0_u8; 64];
        let mut decoder = decoder_with_state(&data, 510, 0);
        let mut contexts = [CabacContextModel {
            state_index: 63,
            value_mps: false,
        }; CABAC_CONTEXT_COUNT];
        let original_contexts = contexts;
        let original_state = (
            decoder.code_range(),
            decoder.code_offset(),
            decoder.bits.bit_offset(),
        );
        let blocks = [crate::LumaIntra4x4Block::default(); 16];
        assert!(decoder
            .decode_intra_nxn_4x4_luma_macroblock(
                &mut contexts,
                0,
                0,
                &[16; 16],
                &[2; 4],
                &[2; 4],
                false,
                false,
                CabacIntra4x4EdgeState::default(),
                CabacIntra4x4EdgeState::default(),
                &blocks,
            )
            .is_err());
        assert_eq!(
            (
                decoder.code_range(),
                decoder.code_offset(),
                decoder.bits.bit_offset(),
            ),
            original_state
        );
        assert!(contexts
            .iter()
            .zip(original_contexts.iter())
            .all(|(actual, original)| {
                (actual.state_index(), actual.mps()) == (original.state_index(), original.mps())
            }));
    }

    #[test]
    fn intra_chroma420_macroblock_decodes_dc_and_ac_cbp_for_both_components() {
        for coded_block_pattern_chroma in [1, 2] {
            let mut decoder = decoder_with_state(&[0; 128], 510, 0);
            let mut contexts = [CabacContextModel {
                state_index: 63,
                value_mps: false,
            }; CABAC_CONTEXT_COUNT];
            let result = decoder
                .decode_intra_chroma420_macroblock(
                    &mut contexts,
                    0,
                    false,
                    0,
                    false,
                    false,
                    false,
                    coded_block_pattern_chroma,
                    26,
                    [0, 0],
                    [[16; 16]; 2],
                    [CabacChroma420References::default(); 2],
                    [CabacChroma420EdgeState::default(); 2],
                    [CabacChroma420EdgeState::default(); 2],
                )
                .unwrap();
            assert_eq!(result.prediction_mode, 0);
            assert_eq!(result.qpc, [26, 26]);
            assert_eq!(result.cb, [128; 64]);
            assert_eq!(result.cr, [128; 64]);
            assert_eq!(result.cb_residual, [0; 64]);
            assert_eq!(result.cr_residual, [0; 64]);
        }

        let mut decoder = decoder_with_state(&[0; 128], 510, 0);
        let mut contexts = [CabacContextModel {
            state_index: 61,
            value_mps: false,
        }; CABAC_CONTEXT_COUNT];
        contexts[100].value_mps = true;
        contexts[104].value_mps = true;
        let result = decoder
            .decode_intra_chroma420_macroblock(
                &mut contexts,
                0,
                false,
                0,
                false,
                false,
                false,
                2,
                26,
                [0, 0],
                [[16; 16]; 2],
                [CabacChroma420References::default(); 2],
                [CabacChroma420EdgeState::default(); 2],
                [CabacChroma420EdgeState::default(); 2],
            )
            .unwrap();
        assert!(result.cb_residual.iter().any(|&sample| sample != 0));
        assert!(result.cr_residual.iter().any(|&sample| sample != 0));
        assert_eq!(result.dc_coded, [true, true]);
        assert!(result.ac_coded_block_flags[0][0]);
        assert!(result.ac_coded_block_flags[1][0]);
    }

    #[test]
    fn i16x16_chroma_prediction_uses_encoded_mode_for_both_components() {
        let mut decoder = decoder_with_state(&[0; 16], 510, 0);
        let mut contexts = [CabacContextModel {
            state_index: 63,
            value_mps: false,
        }; CABAC_CONTEXT_COUNT];
        let references = [
            CabacChroma420References {
                left_available: true,
                left: [20, 30, 40, 50, 60, 70, 80, 90],
                ..Default::default()
            },
            CabacChroma420References {
                left_available: true,
                left: [100, 110, 120, 130, 140, 150, 160, 170],
                ..Default::default()
            },
        ];
        let result = decoder
            .decode_intra_chroma420_macroblock(
                &mut contexts,
                1,
                true,
                2,
                true,
                false,
                false,
                0,
                26,
                [0, 0],
                [[16; 16]; 2],
                references,
                [CabacChroma420EdgeState::default(); 2],
                [CabacChroma420EdgeState::default(); 2],
            )
            .unwrap();
        assert_eq!(&result.cb[..8], &[20; 8]);
        assert_eq!(&result.cb[56..], &[90; 8]);
        assert_eq!(&result.cr[..8], &[100; 8]);
        assert_eq!(&result.cr[56..], &[170; 8]);

        let mut mb_type_decoder = decoder_with_state(&[0; 128], 510, 0);
        let mut mb_type_contexts = [CabacContextModel {
            state_index: 61,
            value_mps: false,
        }; CABAC_CONTEXT_COUNT];
        mb_type_contexts[100].value_mps = true;
        mb_type_contexts[104].value_mps = true;
        let mb_type_result = mb_type_decoder
            .decode_intra_chroma420_macroblock(
                &mut mb_type_contexts,
                0,
                true,
                9,
                true,
                false,
                false,
                0,
                26,
                [0, 0],
                [[16; 16]; 2],
                [CabacChroma420References::default(); 2],
                [CabacChroma420EdgeState::default(); 2],
                [CabacChroma420EdgeState::default(); 2],
            )
            .unwrap();
        assert_eq!(mb_type_result.prediction_mode, 0);
        assert!(mb_type_result.cb_residual.iter().any(|&sample| sample != 0));
        assert!(mb_type_result.cr_residual.iter().any(|&sample| sample != 0));
    }

    #[test]
    fn i_intra_macroblock_dispatches_and_places_all_planes() {
        let mut decoder = decoder_with_state(&[0; 128], 510, 0);
        let mut contexts = [CabacContextModel {
            state_index: 63,
            value_mps: false,
        }; CABAC_CONTEXT_COUNT];
        let mut input = CabacIIntraMacroblockInput::default();
        for block in &mut input.luma_4x4_blocks {
            block.top = [90; 8];
            block.left = [90; 8];
            block.top_left = 90;
            block.top_available = true;
            block.left_available = true;
            block.top_left_available = true;
        }
        let mut builder = crate::Yuv420FrameBuilder::new(1, 1, 0, 0, 0, 0).unwrap();
        let result = decoder
            .decode_i_intra_macroblock(input, &mut contexts, &mut builder, 0)
            .unwrap();
        assert_eq!(result.macroblock_type, 0);
        assert_eq!(result.coded_block_pattern_luma, 0);
        assert_eq!(result.coded_block_pattern_chroma, 0);
        assert_eq!(result.qpy, 26);
        let frame = builder.finish().unwrap();
        assert_eq!(frame.y, result.luma);
        assert_eq!(frame.u, result.cb);
        assert_eq!(frame.v, result.cr);
    }

    #[test]
    fn i16x16_dispatch_derives_chroma_cbp_but_decodes_chroma_mode() {
        let mut decoder = decoder_with_state(&[0; 128], 510, 0);
        let mut contexts = [CabacContextModel {
            state_index: 63,
            value_mps: false,
        }; CABAC_CONTEXT_COUNT];
        contexts[3].value_mps = true;
        contexts[7].value_mps = true;
        contexts[8].value_mps = true;
        let mut input = CabacIIntraMacroblockInput {
            intra16x16_top: [90; 16],
            intra16x16_top_available: true,
            ..Default::default()
        };
        input.chroma_scaling_lists = [[16; 16]; 2];
        let mut builder = crate::Yuv420FrameBuilder::new(1, 1, 0, 0, 0, 0).unwrap();
        let result = decoder
            .decode_i_intra_macroblock(input, &mut contexts, &mut builder, 0)
            .unwrap();
        assert_eq!(result.macroblock_type, 9);
        assert_eq!(result.coded_block_pattern_chroma, 2);
        assert_eq!(result.qpy, 26);
        assert_eq!(result.luma, [90; 256]);
        assert_eq!(builder.finish().unwrap().u, [128; 64]);
    }

    #[test]
    fn i_pcm_dispatch_places_planes_and_restarts_cabac() {
        let mut data = vec![0x80];
        data.extend([0x11; 256]);
        data.extend([0x22; 64]);
        data.extend([0x33; 64]);
        data.extend([0; 2]);
        let mut decoder = decoder_with_state(&data, 510, 509);
        let mut contexts = [CabacContextModel {
            state_index: 0,
            value_mps: false,
        }; CABAC_CONTEXT_COUNT];
        let mut builder = crate::Yuv420FrameBuilder::new(1, 1, 0, 0, 0, 0).unwrap();
        let result = decoder
            .decode_i_intra_macroblock(
                CabacIIntraMacroblockInput::default(),
                &mut contexts,
                &mut builder,
                0,
            )
            .unwrap();
        assert_eq!(result.macroblock_type, 25);
        assert_eq!((decoder.code_range(), decoder.code_offset()), (510, 0));
        let frame = builder.finish().unwrap();
        assert_eq!(frame.y, [0x11; 256]);
        assert_eq!(frame.u, [0x22; 64]);
        assert_eq!(frame.v, [0x33; 64]);
    }

    #[test]
    fn i_nxn_dispatch_selects_8x8_transform_branch() {
        let mut decoder = decoder_with_state(&[0; 128], 510, 0);
        let mut contexts = [CabacContextModel {
            state_index: 63,
            value_mps: false,
        }; CABAC_CONTEXT_COUNT];
        contexts[399].value_mps = true;
        let mut input = CabacIIntraMacroblockInput {
            transform_8x8_mode_enabled: true,
            ..Default::default()
        };
        for block in &mut input.luma_8x8_blocks {
            block.top = [90; 16];
            block.left = [90; 16];
            block.top_left = 90;
            block.top_available = true;
            block.left_available = true;
            block.top_left_available = true;
        }
        let mut builder = crate::Yuv420FrameBuilder::new(1, 1, 0, 0, 0, 0).unwrap();
        let result = decoder
            .decode_i_intra_macroblock(input, &mut contexts, &mut builder, 0)
            .unwrap();
        assert!(result.transform_size_8x8);
        assert_eq!(result.luma, [90; 256]);
    }

    #[test]
    fn i_intra_macroblock_builder_failure_rolls_back_cabac_state() {
        let mut decoder = decoder_with_state(&[0; 128], 510, 0);
        let mut contexts = [CabacContextModel {
            state_index: 63,
            value_mps: false,
        }; CABAC_CONTEXT_COUNT];
        let original_contexts = contexts;
        let original_state = (
            decoder.code_range(),
            decoder.code_offset(),
            decoder.bits.bit_offset(),
        );
        let mut input = CabacIIntraMacroblockInput::default();
        for block in &mut input.luma_4x4_blocks {
            block.top_available = true;
            block.left_available = true;
            block.top_left_available = true;
        }
        let mut builder = crate::Yuv420FrameBuilder::new(1, 1, 0, 0, 0, 0).unwrap();
        builder
            .place_macroblock(0, &[0; 256], &[0; 64], &[0; 64])
            .unwrap();
        assert!(decoder
            .decode_i_intra_macroblock(input, &mut contexts, &mut builder, 0)
            .is_err());
        assert_eq!(
            (
                decoder.code_range(),
                decoder.code_offset(),
                decoder.bits.bit_offset()
            ),
            original_state
        );
        assert!(contexts
            .iter()
            .zip(original_contexts.iter())
            .all(|(actual, original)| {
                (actual.state_index(), actual.mps()) == (original.state_index(), original.mps())
            }));
    }

    #[test]
    fn intra_nxn_8x8_luma_modes_and_transform_flag() {
        let data = [0_u8; 128];
        let mut decoder = decoder_with_state(&data, 510, 0);
        let mut contexts = [CabacContextModel {
            state_index: 63,
            value_mps: false,
        }; CABAC_CONTEXT_COUNT];
        contexts[399].value_mps = true;
        let blocks = [crate::LumaIntra8x8Block {
            top_available: true,
            left_available: true,
            top_left_available: true,
            ..Default::default()
        }; 4];
        let result = decoder
            .decode_intra_nxn_8x8_luma_macroblock(
                &mut contexts,
                true,
                false,
                false,
                0,
                0,
                &[16; 64],
                &[2; 2],
                &[2; 2],
                true,
                true,
                &blocks,
            )
            .unwrap();
        assert!(result.transform_size_8x8);
        assert_eq!(result.modes[0], 0);
        assert_eq!(result.samples, [0; 256]);
        assert_eq!(result.residuals, [[0; 64]; 4]);
    }

    #[test]
    fn intra_nxn_8x8_luma_category5_residual_and_rollback() {
        let data = [0_u8; 128];
        let mut decoder = decoder_with_state(&data, 510, 0);
        let mut contexts = [CabacContextModel {
            state_index: 63,
            value_mps: false,
        }; CABAC_CONTEXT_COUNT];
        contexts[399].value_mps = true;
        contexts[402].value_mps = true;
        contexts[417].value_mps = true;
        let blocks = [crate::LumaIntra8x8Block {
            top: [100; 16],
            left: [100; 16],
            top_left: 100,
            top_available: true,
            left_available: true,
            top_left_available: true,
            ..Default::default()
        }; 4];
        let result = decoder
            .decode_intra_nxn_8x8_luma_macroblock(
                &mut contexts,
                true,
                false,
                false,
                1,
                51,
                &[16; 64],
                &[2; 2],
                &[2; 2],
                true,
                true,
                &blocks,
            )
            .unwrap();
        assert!(result.transform_size_8x8);
        assert!(result.residuals[0].iter().any(|&sample| sample != 0));

        let mut truncated = decoder_with_state(&[], 510, 0);
        let mut truncated_contexts = [CabacContextModel {
            state_index: 63,
            value_mps: false,
        }; CABAC_CONTEXT_COUNT];
        truncated_contexts[399].value_mps = true;
        let original_contexts = truncated_contexts;
        let original_state = (
            truncated.code_range(),
            truncated.code_offset(),
            truncated.bits.bit_offset(),
        );
        assert!(truncated
            .decode_intra_nxn_8x8_luma_macroblock(
                &mut truncated_contexts,
                true,
                false,
                false,
                1,
                0,
                &[16; 64],
                &[2; 2],
                &[2; 2],
                true,
                true,
                &blocks,
            )
            .is_err());
        assert_eq!(
            (
                truncated.code_range(),
                truncated.code_offset(),
                truncated.bits.bit_offset(),
            ),
            original_state
        );
        assert!(truncated_contexts.iter().zip(original_contexts.iter()).all(
            |(actual, original)| {
                (actual.state_index(), actual.mps()) == (original.state_index(), original.mps())
            }
        ));
    }

    #[test]
    fn reconstruct_intra16x16_luma_dc_hadamard_and_scaling() {
        let mut levels = [0_i32; 16];
        levels[0] = 1;
        assert_eq!(
            crate::reconstruction::reconstruct_intra16x16_luma_dc(&levels, &[16; 16], 51).unwrap(),
            [896; 16]
        );
        assert!(
            crate::reconstruction::reconstruct_intra16x16_luma_dc(&levels, &[0; 16], 0).is_err()
        );
    }

    #[test]
    fn decodes_intra16x16_luma_dc_and_prediction() {
        let data = [0_u8; 64];
        let mut decoder = decoder_with_state(&data, 510, 0);
        let mut contexts = [CabacContextModel {
            state_index: 63,
            value_mps: false,
        }; CABAC_CONTEXT_COUNT];
        for index in [96, 134, 195] {
            contexts[index].value_mps = true;
        }
        let top = [100_u8; 16];
        let result = decoder
            .decode_intra16x16_luma_macroblock(
                1,
                &mut contexts,
                51,
                &[16; 16],
                Some(&top),
                None,
                0,
                false,
                CabacIntra16x16EdgeState::default(),
                CabacIntra16x16EdgeState::default(),
            )
            .unwrap();
        assert_eq!(result.prediction_mode, 0);
        assert_eq!(result.coded_block_pattern_luma, 0);
        assert!(result.dc_coded);
        assert!(result.residual.iter().any(|&sample| sample != 0));
        assert_ne!(result.samples[0], 100);
    }

    #[test]
    fn decodes_intra16x16_luma_ac_residuals() {
        let data = [0_u8; 128];
        let mut decoder = decoder_with_state(&data, 510, 0);
        let mut contexts = [CabacContextModel {
            state_index: 63,
            value_mps: false,
        }; CABAC_CONTEXT_COUNT];
        for index in [92, 96, 120, 134, 166, 181] {
            contexts[index].value_mps = true;
        }
        let top = [100_u8; 16];
        let result = decoder
            .decode_intra16x16_luma_macroblock(
                13,
                &mut contexts,
                51,
                &[16; 16],
                Some(&top),
                None,
                0,
                false,
                CabacIntra16x16EdgeState::default(),
                CabacIntra16x16EdgeState::default(),
            )
            .unwrap();
        assert_eq!(result.coded_block_pattern_luma, 15);
        assert!(result.ac_coded_block_flags.iter().all(|&coded| coded));
        assert!(result.residual.iter().any(|&sample| sample != 0));
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
    fn derives_intra4x4_predicted_mode_from_syntax_scan_neighbors() {
        let decoded = [7, 5, 4, 6, 3, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0];
        let top = [3, 2, 1, 0];
        let left = [5, 6, 7, 8];
        assert_eq!(
            derive_intra4x4_predicted_mode(&decoded, 0, &top, &left, true, true).unwrap(),
            3
        );
        assert_eq!(
            derive_intra4x4_predicted_mode(&decoded, 1, &top, &left, true, true).unwrap(),
            2
        );
        assert_eq!(
            derive_intra4x4_predicted_mode(&decoded, 2, &top, &left, true, true).unwrap(),
            6
        );
        assert_eq!(
            derive_intra4x4_predicted_mode(&decoded, 4, &top, &left, true, true).unwrap(),
            1
        );
        assert_eq!(
            derive_intra4x4_predicted_mode(&decoded, 0, &top, &left, false, true).unwrap(),
            2
        );
        assert!(derive_intra4x4_predicted_mode(&decoded, 16, &top, &left, true, true).is_err());
    }

    #[test]
    fn decodes_all_intra4x4_modes_and_rolls_back_on_truncation() {
        let data = [0; 128];
        let mut decoder = decoder_with_state(&data, 510, 0);
        let mut contexts = [
            CabacContextModel::new(0, 63, 26).unwrap(),
            CabacContextModel::new(0, 63, 26).unwrap(),
        ];
        let modes = decoder
            .decode_intra4x4_pred_modes(&mut contexts, &[0; 4], &[0; 4], false, false)
            .unwrap();
        assert!(modes.iter().all(|mode| *mode <= 8));

        let mut truncated = decoder_with_state(&[], 510, 0);
        let mut contexts = [
            CabacContextModel::new(0, 63, 26).unwrap(),
            CabacContextModel::new(0, 63, 26).unwrap(),
        ];
        assert_eq!(
            truncated
                .decode_intra4x4_pred_modes(&mut contexts, &[0; 4], &[0; 4], false, false)
                .unwrap_err()
                .kind(),
            ErrorKind::UnexpectedEof
        );
        assert_eq!((truncated.code_range(), truncated.code_offset()), (510, 0));
        assert_eq!(
            (contexts[0].state_index(), contexts[1].state_index()),
            (0, 0)
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
    fn decodes_chroma4x4_ac_residual_block_with_scan_offset_and_rollback() {
        let make_contexts = || {
            (
                [CabacContextModel {
                    state_index: 63,
                    value_mps: false,
                }; 4],
                [CabacContextModel {
                    state_index: 63,
                    value_mps: false,
                }; 15],
                [CabacContextModel {
                    state_index: 63,
                    value_mps: false,
                }; 15],
                [CabacContextModel {
                    state_index: 63,
                    value_mps: false,
                }; 10],
            )
        };

        let mut decoder = decoder_with_state(&[], 510, 0);
        let (mut coded, mut significant, mut last, mut coefficients) = make_contexts();
        let (levels, has_residual) = decoder
            .decode_chroma4x4_ac_residual_block(
                false,
                false,
                &mut coded,
                &mut significant,
                &mut last,
                &mut coefficients,
            )
            .unwrap();
        assert_eq!(levels, [0; 16]);
        assert!(!has_residual);

        let mut decoder = decoder_with_state(&[0], 510, 0);
        let (mut coded, mut significant, mut last, mut coefficients) = make_contexts();
        coded[0].value_mps = true;
        let (levels, has_residual) = decoder
            .decode_chroma4x4_ac_residual_block(
                false,
                false,
                &mut coded,
                &mut significant,
                &mut last,
                &mut coefficients,
            )
            .unwrap();
        assert_eq!(levels, [0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1]);
        assert!(has_residual);

        let mut decoder = decoder_with_state(&[], 510, 0);
        let (mut coded, mut significant, mut last, mut coefficients) = make_contexts();
        coded[0].value_mps = true;
        assert_eq!(
            decoder
                .decode_chroma4x4_ac_residual_block(
                    false,
                    false,
                    &mut coded,
                    &mut significant,
                    &mut last,
                    &mut coefficients,
                )
                .unwrap_err()
                .kind(),
            ErrorKind::UnexpectedEof
        );
        assert_eq!((decoder.code_range(), decoder.code_offset()), (510, 0));
        assert!(coded[0].mps());
        assert_eq!(coded[0].state_index(), 63);
    }

    #[test]
    fn decodes_luma4x4_residual_block_with_coded_flag() {
        let mut decoder = decoder_with_state(&[0; 128], 510, 0);
        let mut coded = [CabacContextModel::new(0, 63, 26).unwrap(); 4];
        let mut significant = [CabacContextModel {
            state_index: 63,
            value_mps: false,
        }; 15];
        let mut last = significant;
        let mut coefficients = [CabacContextModel {
            state_index: 63,
            value_mps: false,
        }; 10];
        let (levels, has_residual) = decoder
            .decode_luma4x4_residual_block_with_flag(
                0,
                0,
                &mut coded,
                &mut significant,
                &mut last,
                &mut coefficients,
            )
            .unwrap();
        assert_eq!(levels, [0; 16]);
        assert!(!has_residual);

        let mut decoder = decoder_with_state(&[0; 128], 510, 0);
        coded[0].value_mps = true;
        let (_, has_residual) = decoder
            .decode_luma4x4_residual_block_with_flag(
                0,
                0,
                &mut coded,
                &mut significant,
                &mut last,
                &mut coefficients,
            )
            .unwrap();
        assert!(has_residual);

        let mut truncated = decoder_with_state(&[], 510, 0);
        let mut coded = [CabacContextModel {
            state_index: 0,
            value_mps: false,
        }; 4];
        coded[0].value_mps = true;
        let mut significant = [CabacContextModel {
            state_index: 63,
            value_mps: false,
        }; 15];
        let mut last = significant;
        let mut coefficients = [CabacContextModel {
            state_index: 63,
            value_mps: false,
        }; 10];
        assert_eq!(
            truncated
                .decode_luma4x4_residual_block_with_flag(
                    0,
                    0,
                    &mut coded,
                    &mut significant,
                    &mut last,
                    &mut coefficients,
                )
                .unwrap_err()
                .kind(),
            ErrorKind::UnexpectedEof
        );
        assert_eq!((truncated.code_range(), truncated.code_offset()), (510, 0));
        assert_eq!(coded[0].state_index(), 0);
        assert!(coded[0].mps());
        assert_eq!(significant[0].state_index(), 63);
    }

    #[test]
    fn decodes_and_reconstructs_luma4x4_residual_before_advancing_on_invalid_qpy() {
        let data = [0; 128];
        let mut decoder = CabacArithmeticDecoder::new(&data).unwrap();
        let mut coded = [CabacContextModel::new(0, 63, 26).unwrap(); 4];
        let mut significant = [CabacContextModel::new(0, 127, 26).unwrap(); 15];
        let mut last = significant;
        let mut coefficients = [CabacContextModel::new(0, 127, 26).unwrap(); 10];
        let (residual, has_residual) = decoder
            .decode_and_reconstruct_luma4x4_residual(
                0,
                0,
                &mut coded,
                &mut significant,
                &mut last,
                &mut coefficients,
                &[16; 16],
                0,
            )
            .unwrap();
        assert_eq!(residual, [0; 16]);
        assert!(!has_residual);

        let range = decoder.code_range();
        let offset = decoder.code_offset();
        assert_eq!(
            decoder
                .decode_and_reconstruct_luma4x4_residual(
                    0,
                    0,
                    &mut coded,
                    &mut significant,
                    &mut last,
                    &mut coefficients,
                    &[16; 16],
                    52,
                )
                .unwrap_err()
                .kind(),
            ErrorKind::InvalidData
        );
        assert_eq!(
            (decoder.code_range(), decoder.code_offset()),
            (range, offset)
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

    /// Clause 9.3.4.2 arithmetic encoder, used only to build decoder vectors.
    struct TestEncoder {
        low: u32,
        code_range: u32,
        outstanding: usize,
        first_bit: bool,
        flushed: bool,
        bits: Vec<bool>,
    }

    impl TestEncoder {
        fn new() -> Self {
            Self {
                low: 0,
                code_range: 510,
                outstanding: 0,
                first_bit: true,
                flushed: false,
                bits: Vec::new(),
            }
        }

        fn put_bit(&mut self, bit: bool) {
            if self.first_bit {
                self.first_bit = false;
            } else {
                self.bits.push(bit);
            }
            self.bits
                .extend(std::iter::repeat_n(!bit, self.outstanding));
            self.outstanding = 0;
        }

        fn renormalize(&mut self) {
            while self.code_range < 256 {
                if self.low < 256 {
                    self.put_bit(false);
                } else if self.low >= 512 {
                    self.low -= 512;
                    self.put_bit(true);
                } else {
                    self.low -= 256;
                    self.outstanding += 1;
                }
                self.code_range <<= 1;
                self.low <<= 1;
            }
        }

        fn decision(&mut self, model: &mut CabacContextModel, bin: bool) {
            let range_lps = u32::from(
                RANGE_LPS[((self.code_range >> 6) & 3) as usize][model.state_index as usize],
            );
            self.code_range -= range_lps;
            if bin != model.value_mps {
                self.low += self.code_range;
                self.code_range = range_lps;
            }
            model.update(bin);
            self.renormalize();
        }

        fn terminate(&mut self, bin: bool) {
            self.code_range -= 2;
            if !bin {
                self.renormalize();
                return;
            }
            self.low += self.code_range;
            self.code_range = 2;
            self.renormalize();
            self.put_bit((self.low >> 9) & 1 == 1);
            self.bits.extend([(self.low >> 8) & 1 == 1, true]);
            self.flushed = true;
        }

        fn encode_bin_string(
            &mut self,
            contexts: &mut [CabacContextModel],
            bins: &str,
            ctx_idx: &dyn Fn(usize, &str) -> usize,
        ) {
            for (index, char) in bins.char_indices() {
                let context = ctx_idx(index, &bins[..index]);
                if context == 276 {
                    self.terminate(char == '1');
                } else {
                    self.decision(&mut contexts[context], char == '1');
                }
            }
        }

        fn data(&mut self) -> Vec<u8> {
            if !self.flushed {
                self.terminate(true);
            }
            let mut data = vec![0_u8; self.bits.len().div_ceil(8) + 4];
            for (index, bit) in self.bits.iter().enumerate() {
                if *bit {
                    data[index / 8] |= 0x80 >> (index % 8);
                }
            }
            data
        }
    }

    // Table 9-36 I mb_type and Table 9-37 B mb_type bin strings.
    const TEST_I_MB_TYPE_BINS: [&str; 26] = [
        "0", "100000", "100001", "100010", "100011", "1001000", "1001001", "1001010", "1001011",
        "1001100", "1001101", "1001110", "1001111", "101000", "101001", "101010", "101011",
        "1011000", "1011001", "1011010", "1011011", "1011100", "1011101", "1011110", "1011111",
        "11",
    ];
    const TEST_B_MB_TYPE_BINS: [&str; 23] = [
        "0", "100", "101", "110000", "110001", "110010", "110011", "110100", "110101", "110110",
        "110111", "111110", "1110000", "1110001", "1110010", "1110011", "1110100", "1110101",
        "1110110", "1110111", "1111000", "1111001", "111111",
    ];

    fn intra_suffix_context(offset: usize) -> impl Fn(usize, &str) -> usize {
        move |bin_idx, prior| match bin_idx {
            0 => offset,
            1 => 276,
            2 | 3 => offset + bin_idx - 1,
            4 if prior.as_bytes()[3] != b'0' => offset + 2,
            _ => offset + 3,
        }
    }

    #[test]
    fn inter_mb_type_round_trip() {
        for (slice_type, left, top, left_direct, increment) in [
            (0_u8, true, true, false, 0_usize),
            (5, false, false, false, 0),
            (1, false, false, false, 0),
            (6, true, false, false, 1),
            (1, true, true, false, 2),
            (1, true, true, true, 1),
        ] {
            let is_p = slice_type % 5 == 0;
            let prefix_context = move |bin_idx: usize, prior: &str| -> usize {
                if is_p {
                    return if bin_idx == 2 && prior.as_bytes()[1] == b'1' {
                        17
                    } else {
                        14 + bin_idx
                    };
                }
                match bin_idx {
                    0 => 27 + increment,
                    1 => 30,
                    2 if prior.as_bytes()[1] != b'0' => 31,
                    _ => 32,
                }
            };
            let (plain, intra_prefix, intra_base, suffix_offset): (Vec<&str>, &str, u8, usize) =
                if is_p {
                    (vec!["000", "011", "010", "001"], "1", 5, 17)
                } else {
                    (TEST_B_MB_TYPE_BINS.to_vec(), "111101", 23, 32)
                };
            let suffix_context = intra_suffix_context(suffix_offset);
            let mut symbols: Vec<(u8, &str, Option<&str>)> = Vec::new();
            for _ in 0..2 {
                symbols.extend(plain.iter().enumerate().map(|(v, b)| (v as u8, *b, None)));
                symbols.extend(
                    TEST_I_MB_TYPE_BINS[..25]
                        .iter()
                        .enumerate()
                        .map(|(v, b)| (intra_base + v as u8, intra_prefix, Some(*b))),
                );
            }
            // I_PCM terminates the stream, so it is coded last.
            symbols.push((intra_base + 25, intra_prefix, Some("11")));
            let slice_contexts = new_cabac_slice_contexts(slice_type, 1, 30).unwrap();
            let mut encoder_contexts = slice_contexts.clone();
            let mut encoder = TestEncoder::new();
            for (_, prefix, suffix) in &symbols {
                encoder.encode_bin_string(&mut encoder_contexts, prefix, &prefix_context);
                if let Some(suffix) = suffix {
                    encoder.encode_bin_string(&mut encoder_contexts, suffix, &suffix_context);
                }
            }
            let data = encoder.data();
            let mut decoder = CabacArithmeticDecoder::new(&data).unwrap();
            let mut decoder_contexts = slice_contexts.clone();
            for (value, _, _) in &symbols {
                let got = decoder
                    .decode_inter_mb_type(
                        slice_type,
                        &mut decoder_contexts,
                        left,
                        top,
                        left_direct,
                        false,
                    )
                    .unwrap();
                assert_eq!(got, *value, "slice {slice_type}");
            }
            let states = |models: &[CabacContextModel]| {
                models
                    .iter()
                    .map(|model| (model.state_index, model.value_mps))
                    .collect::<Vec<_>>()
            };
            assert_eq!(states(&decoder_contexts), states(&encoder_contexts));
            assert!(decoder.terminated);
        }
    }

    #[test]
    fn sub_mb_type_round_trip() {
        let p_context = |bin_idx: usize, _: &str| 21 + bin_idx;
        let b_context = |bin_idx: usize, prior: &str| match bin_idx {
            0 | 1 => 36 + bin_idx,
            2 if prior.as_bytes()[1] != b'0' => 38,
            _ => 39,
        };
        type SubMbRoundTripCase<'a> = (u8, Vec<&'a str>, &'a dyn Fn(usize, &str) -> usize);
        let cases: [SubMbRoundTripCase<'_>; 2] = [
            (0, vec!["1", "00", "011", "010"], &p_context),
            (
                1,
                vec![
                    "0", "100", "101", "11000", "11001", "11010", "11011", "111000", "111001",
                    "111010", "111011", "11110", "11111",
                ],
                &b_context,
            ),
        ];
        for (slice_type, bin_strings, context) in cases {
            let slice_contexts = new_cabac_slice_contexts(slice_type, 2, 33).unwrap();
            let mut encoder_contexts = slice_contexts.clone();
            let mut encoder = TestEncoder::new();
            for _ in 0..2 {
                for bins in &bin_strings {
                    encoder.encode_bin_string(&mut encoder_contexts, bins, context);
                }
            }
            let data = encoder.data();
            let mut decoder = CabacArithmeticDecoder::new(&data).unwrap();
            let mut decoder_contexts = slice_contexts.clone();
            for _ in 0..2 {
                for want in 0..bin_strings.len() {
                    let got = decoder
                        .decode_sub_mb_type(slice_type, &mut decoder_contexts)
                        .unwrap();
                    assert_eq!(usize::from(got), want, "slice {slice_type}");
                }
            }
            for (decoded, encoded) in decoder_contexts.iter().zip(&encoder_contexts) {
                assert_eq!(
                    (decoded.state_index, decoded.value_mps),
                    (encoded.state_index, encoded.value_mps)
                );
            }
        }
    }

    #[test]
    fn inter_mb_type_and_sub_mb_type_reject_invalid_input_transactionally() {
        let mut contexts = new_cabac_slice_contexts(1, 0, 26).unwrap();
        let data = [0x5a_u8, 0x3c];
        let mut decoder = CabacArithmeticDecoder::new(&data).unwrap();
        for slice_type in [2, 4, 10] {
            assert!(decoder
                .decode_inter_mb_type(slice_type, &mut contexts, false, false, false, false)
                .is_err());
            assert!(decoder
                .decode_sub_mb_type(slice_type, &mut contexts)
                .is_err());
        }
        assert!(decoder
            .decode_inter_mb_type(1, &mut contexts[..459], false, false, false, false)
            .is_err());
        let snapshot = |decoder: &CabacArithmeticDecoder, contexts: &[CabacContextModel]| {
            (
                decoder.code_range(),
                decoder.code_offset(),
                decoder.bits.bit_offset(),
                contexts
                    .iter()
                    .map(|model| (model.state_index, model.value_mps))
                    .collect::<Vec<_>>(),
            )
        };
        loop {
            let before = snapshot(&decoder, &contexts);
            if decoder
                .decode_inter_mb_type(1, &mut contexts, false, false, false, false)
                .is_err()
            {
                assert_eq!(snapshot(&decoder, &contexts), before);
                break;
            }
        }
    }

    #[test]
    fn initializes_p_inter_mb_type_contexts_from_table_913() {
        let expected = [
            [(54, false), (14, false), (54, true), (6, false)],
            [(54, false), (22, false), (54, true), (1, true)],
            [(12, false), (1, false), (35, true), (47, false)],
        ];
        for (cabac_init_idc, expected_contexts) in expected.into_iter().enumerate() {
            let contexts = new_cabac_p_inter_mb_type_contexts(cabac_init_idc as u8, 0).unwrap();
            assert_eq!(
                contexts.map(|context| (context.state_index(), context.mps())),
                expected_contexts
            );
        }
        assert!(new_cabac_p_inter_mb_type_contexts(3, 26).is_err());
        assert!(new_cabac_p_inter_mb_type_contexts(0, 52).is_err());
    }

    #[test]
    fn initializes_inter_prediction_contexts_from_tables_915_and_916() {
        let (mvd_x, mvd_y, ref_idx) = new_cabac_inter_prediction_contexts(0, 0).unwrap();
        assert_eq!(
            mvd_x.map(|context| (context.state_index(), context.mps())),
            [
                (5, true),
                (17, true),
                (32, true),
                (8, false),
                (3, true),
                (22, true),
                (24, true),
            ]
        );
        assert_eq!(
            mvd_y.map(|context| (context.state_index(), context.mps())),
            [
                (5, false),
                (12, true),
                (30, true),
                (9, false),
                (5, true),
                (17, true),
                (24, true),
            ]
        );
        assert_eq!(
            ref_idx.map(|context| (context.state_index(), context.mps())),
            [
                (3, true),
                (10, true),
                (10, true),
                (16, true),
                (8, true),
                (5, false)
            ]
        );
        assert!(new_cabac_inter_prediction_contexts(3, 26).is_err());
        assert!(new_cabac_inter_prediction_contexts(0, 52).is_err());
    }

    #[test]
    fn inter_reference_index_context_increment_matches_clause_9_3_3_1_1_6() {
        let neighbor = CabacInterNeighbor {
            available: true,
            prediction_mode_matches: true,
            reference_index: 1,
            ..CabacInterNeighbor::default()
        };
        assert_eq!(
            derive_cabac_reference_index_context_increment(
                neighbor,
                Default::default(),
                false,
                false
            ),
            1
        );
        assert_eq!(
            derive_cabac_reference_index_context_increment(
                Default::default(),
                neighbor,
                false,
                false
            ),
            2
        );
        assert_eq!(
            derive_cabac_reference_index_context_increment(neighbor, neighbor, false, false),
            3
        );
        for blocked in [
            CabacInterNeighbor::default(),
            CabacInterNeighbor {
                available: true,
                skip: true,
                prediction_mode_matches: true,
                reference_index: 1,
                ..Default::default()
            },
            CabacInterNeighbor {
                available: true,
                intra: true,
                prediction_mode_matches: true,
                reference_index: 1,
                ..Default::default()
            },
            CabacInterNeighbor {
                available: true,
                prediction_mode_matches: false,
                reference_index: 1,
                ..Default::default()
            },
            CabacInterNeighbor {
                available: true,
                prediction_mode_matches: true,
                reference_index: 0,
                ..Default::default()
            },
        ] {
            assert_eq!(
                derive_cabac_reference_index_context_increment(
                    blocked,
                    Default::default(),
                    false,
                    false
                ),
                0
            );
        }
        let field_neighbor = CabacInterNeighbor {
            is_field: true,
            ..neighbor
        };
        assert_eq!(
            derive_cabac_reference_index_context_increment(
                field_neighbor,
                Default::default(),
                true,
                false
            ),
            0
        );
        let field_neighbor = CabacInterNeighbor {
            reference_index: 2,
            is_field: true,
            ..neighbor
        };
        assert_eq!(
            derive_cabac_reference_index_context_increment(
                field_neighbor,
                Default::default(),
                true,
                false
            ),
            1
        );

        let data = [0_u8; 8];
        let mut decoder = CabacArithmeticDecoder::new(&data).unwrap();
        let mut contexts = std::array::from_fn(|index| CabacContextModel {
            state_index: index as u8,
            value_mps: false,
        });
        decoder
            .decode_reference_index_for_partition(
                1,
                neighbor,
                neighbor,
                false,
                false,
                &mut contexts,
            )
            .unwrap();
        assert_eq!(
            contexts.map(|model| model.state_index()),
            [0, 1, 2, 4, 4, 5]
        );
    }

    #[test]
    fn inter_mvd_context_increment_matches_clause_9_3_3_1_1_7() {
        let neighbor = |x, y| CabacInterNeighbor {
            available: true,
            prediction_mode_matches: true,
            motion_vector_difference: [x, y],
            ..CabacInterNeighbor::default()
        };
        let cases = [
            (neighbor(1, 1), neighbor(1, 1), 0, false, false, 0),
            (neighbor(2, 2), neighbor(1, 1), 0, false, false, 1),
            (neighbor(20, 20), neighbor(13, 13), 0, false, false, 2),
            (neighbor(33, 33), Default::default(), 0, false, false, 2),
            (
                CabacInterNeighbor {
                    motion_vector_difference: [90, 90],
                    ..Default::default()
                },
                Default::default(),
                0,
                false,
                false,
                0,
            ),
            (
                CabacInterNeighbor {
                    available: true,
                    skip: true,
                    motion_vector_difference: [90, 90],
                    ..Default::default()
                },
                Default::default(),
                0,
                false,
                false,
                0,
            ),
            (
                CabacInterNeighbor {
                    available: true,
                    intra: true,
                    motion_vector_difference: [90, 90],
                    ..Default::default()
                },
                Default::default(),
                0,
                false,
                false,
                0,
            ),
            (
                CabacInterNeighbor {
                    available: true,
                    motion_vector_difference: [90, 90],
                    ..Default::default()
                },
                Default::default(),
                0,
                false,
                false,
                0,
            ),
            (
                CabacInterNeighbor {
                    is_field: true,
                    motion_vector_difference: [0, 2],
                    ..neighbor(0, 2)
                },
                Default::default(),
                1,
                true,
                false,
                1,
            ),
            (neighbor(0, 7), Default::default(), 1, true, true, 1),
            (
                CabacInterNeighbor {
                    is_field: true,
                    motion_vector_difference: [2, 0],
                    ..neighbor(2, 0)
                },
                Default::default(),
                0,
                true,
                false,
                0,
            ),
            (
                CabacInterNeighbor {
                    motion_vector_difference: [i32::MIN, 0],
                    available: true,
                    prediction_mode_matches: true,
                    ..Default::default()
                },
                Default::default(),
                0,
                false,
                false,
                2,
            ),
        ];
        for (left, top, component, mbaff, current_is_field, expected) in cases {
            assert_eq!(
                derive_cabac_mvd_context_increment(left, top, component, mbaff, current_is_field)
                    .unwrap(),
                expected
            );
        }
        assert!(derive_cabac_mvd_context_increment(
            Default::default(),
            Default::default(),
            2,
            false,
            false
        )
        .is_err());

        let data = [0_u8; 8];
        let mut decoder = CabacArithmeticDecoder::new(&data).unwrap();
        let mut contexts = std::array::from_fn(|index| CabacContextModel {
            state_index: index as u8,
            value_mps: false,
        });
        decoder
            .decode_motion_vector_difference_for_partition(
                0,
                neighbor(2, 0),
                neighbor(1, 0),
                false,
                false,
                &mut contexts,
            )
            .unwrap();
        assert_eq!(
            contexts.map(|model| model.state_index()),
            [0, 2, 2, 3, 4, 5, 6]
        );
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
