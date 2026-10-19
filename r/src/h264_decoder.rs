use std::{
    collections::{HashMap, VecDeque},
    env, fmt, fs,
    fs::File,
    io,
    path::{Path, PathBuf},
    time::{SystemTime, UNIX_EPOCH},
};

use crate::reconstruction::Yuv420Frame;
use bincode::Options;

const CHECKPOINT_MAGIC: &[u8; 16] = b"PYGORVID-RS-CKPT";
const CHECKPOINT_VERSION: u32 = 1;
const MAX_CHECKPOINT_BYTES: u64 = 2 * 1024 * 1024 * 1024;

#[derive(serde::Serialize, serde::Deserialize)]
struct DecoderCheckpoint {
    magic: [u8; 16],
    version: u32,
    sample_count: Option<usize>,
    sequence_parameter_sets: Vec<Vec<u8>>,
    picture_parameter_sets: Vec<Vec<u8>>,
    gop_sync_sample_index: Option<u64>,
    last_decoded_sample_index: Option<u64>,
    maximum_long_term_frame_idx: Option<u32>,
    poc_type0_state: crate::PocType0State,
    poc_type12_state: crate::PocType12State,
    decoded_frames: Vec<(usize, Yuv420Frame)>,
    reference_pictures: Vec<crate::DecodedReferencePicture>,
}

fn invalid_checkpoint(message: &str) -> io::Error {
    io::Error::new(io::ErrorKind::InvalidData, message)
}

fn validate_checkpoint_frame(frame: &Yuv420Frame) -> io::Result<()> {
    let valid_plane = |plane: &[u8], width: usize, height: usize, stride: usize| {
        width > 0
            && height > 0
            && stride >= width
            && height
                .checked_sub(1)
                .and_then(|rows| rows.checked_mul(stride))
                .and_then(|length| length.checked_add(width))
                .is_some_and(|required| plane.len() >= required)
    };
    let chroma_width = frame.width / 2 + frame.width % 2;
    let chroma_height = frame.height / 2 + frame.height % 2;
    if valid_plane(&frame.y, frame.width, frame.height, frame.y_stride)
        && valid_plane(&frame.u, chroma_width, chroma_height, frame.u_stride)
        && valid_plane(&frame.v, chroma_width, chroma_height, frame.v_stride)
    {
        Ok(())
    } else {
        Err(invalid_checkpoint("checkpoint frame layout is invalid"))
    }
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct DependencySlice {
    pub sample_index: u64,
    pub dts_ticks: i64,
    pub pts_ticks: i64,
    pub is_sync: bool,
    pub nal_unit: Vec<u8>,
    pub header: crate::SliceHeader,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum DecodeError {
    Codec(crate::CodecError),
    MissingSampleReader,
    FrameIndexOutOfRange,
    NotImplemented,
    UnsupportedFeature,
    ReferenceMismatch,
    DecodeFailure(String),
}

impl fmt::Display for DecodeError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::Codec(error) => fmt::Display::fmt(error, f),
            Self::MissingSampleReader => write!(f, "H.264 decoder requires a sample reader"),
            Self::FrameIndexOutOfRange => write!(f, "frame index is out of range"),
            Self::NotImplemented => write!(f, "H.264 decoder is not implemented yet"),
            Self::UnsupportedFeature => {
                write!(
                    f,
                    "unsupported H.264 profile, chroma format, bit depth, or interlace mode"
                )
            }
            Self::ReferenceMismatch => {
                write!(f, "decoded frame does not match the reference frame data")
            }
            Self::DecodeFailure(message) => write!(f, "native H.264 decode failed: {message}"),
        }
    }
}

impl std::error::Error for DecodeError {}

fn invalid(message: impl Into<String>) -> io::Error {
    io::Error::new(io::ErrorKind::InvalidData, message.into())
}

fn apply_adaptive_reference_marking(
    buffer: &mut crate::reconstruction::ReferencePictureBuffer,
    current_frame_num: u32,
    maximum_frame_num: u32,
    operations: &[crate::slice::MemoryManagementOperation],
    maximum_long_term_frame_idx: &mut Option<u32>,
) -> io::Result<(Option<u32>, bool)> {
    if maximum_frame_num == 0 || current_frame_num >= maximum_frame_num {
        return Err(invalid("current frame number is outside MaxFrameNum"));
    }
    let mut current_long_term_frame_idx = None;
    let mut reset_frame_num = false;
    for command in operations {
        let operand = |index: usize| {
            command
                .operands
                .get(index)
                .copied()
                .ok_or_else(|| invalid("decoded-reference marking operand is missing"))
        };
        match command.operation {
            1 | 3 => {
                let difference = i64::from(operand(0)?) + 1;
                let picture_number = i64::from(current_frame_num)
                    .checked_sub(difference)
                    .ok_or_else(|| invalid("decoded-reference picture number overflows"))?;
                let target = buffer.references().into_iter().find(|reference| {
                    if reference.long_term_frame_idx.is_some()
                        || reference.frame_num >= maximum_frame_num
                    {
                        return false;
                    }
                    let frame_num_wrap = if reference.frame_num > current_frame_num {
                        i64::from(reference.frame_num) - i64::from(maximum_frame_num)
                    } else {
                        i64::from(reference.frame_num)
                    };
                    frame_num_wrap == picture_number
                });
                let target = target.ok_or_else(|| {
                    invalid("decoded-reference marking short-term target is unavailable")
                })?;
                if command.operation == 1 {
                    buffer.remove(target.identifier);
                } else {
                    let long_term_frame_idx = operand(1)?;
                    if !maximum_long_term_frame_idx
                        .is_some_and(|maximum| long_term_frame_idx <= maximum)
                    {
                        return Err(invalid("long-term frame index exceeds MaxLongTermFrameIdx"));
                    }
                    for reference in buffer.references() {
                        if reference.long_term_frame_idx == Some(long_term_frame_idx) {
                            buffer.remove(reference.identifier);
                        }
                    }
                    if !buffer.set_long_term_frame_idx(target.identifier, Some(long_term_frame_idx))
                    {
                        return Err(invalid("decoded-reference marking target disappeared"));
                    }
                }
            }
            2 => {
                let long_term_pic_num = operand(0)?;
                let target = buffer
                    .references()
                    .into_iter()
                    .find(|reference| reference.long_term_frame_idx == Some(long_term_pic_num))
                    .ok_or_else(|| {
                        invalid("decoded-reference marking long-term target is unavailable")
                    })?;
                buffer.remove(target.identifier);
            }
            4 => {
                let maximum_plus_one = operand(0)?;
                *maximum_long_term_frame_idx = maximum_plus_one.checked_sub(1);
                for reference in buffer.references() {
                    if reference.long_term_frame_idx.is_some_and(|frame_idx| {
                        maximum_long_term_frame_idx.map_or(true, |maximum| frame_idx > maximum)
                    }) {
                        buffer.remove(reference.identifier);
                    }
                }
            }
            5 => {
                buffer.clear();
                *maximum_long_term_frame_idx = None;
                current_long_term_frame_idx = None;
                reset_frame_num = true;
            }
            6 => {
                let long_term_frame_idx = operand(0)?;
                if !maximum_long_term_frame_idx
                    .is_some_and(|maximum| long_term_frame_idx <= maximum)
                {
                    return Err(invalid("long-term frame index exceeds MaxLongTermFrameIdx"));
                }
                for reference in buffer.references() {
                    if reference.long_term_frame_idx == Some(long_term_frame_idx) {
                        buffer.remove(reference.identifier);
                    }
                }
                current_long_term_frame_idx = Some(long_term_frame_idx);
            }
            _ => return Err(invalid("decoded-reference marking operation is invalid")),
        }
    }
    Ok((current_long_term_frame_idx, reset_frame_num))
}

fn deblocking_luma_nonzero_flags(
    transform_size_8x8: bool,
    coded_flags: [bool; 16],
    luma_8x8_nonzero: [bool; 4],
) -> [bool; 16] {
    if transform_size_8x8 {
        return std::array::from_fn(|raster| {
            let group = (raster / 8) * 2 + (raster % 4) / 2;
            luma_8x8_nonzero[group]
        });
    }
    let mut nonzero = [false; 16];
    for (index, raster) in [0, 1, 4, 5, 2, 3, 6, 7, 8, 9, 12, 13, 10, 11, 14, 15]
        .into_iter()
        .enumerate()
    {
        nonzero[raster] = coded_flags[index];
    }
    nonzero
}

#[cfg(test)]
mod deblocking_metadata_tests {
    use super::deblocking_luma_nonzero_flags;

    #[test]
    fn luma_8x8_residual_marks_every_constituent_4x4_cell() {
        for pattern in 0..16 {
            let nonzero_8x8 = std::array::from_fn(|group| pattern & (1 << group) != 0);
            let nonzero = deblocking_luma_nonzero_flags(true, [false; 16], nonzero_8x8);
            for row in 0..4 {
                for column in 0..4 {
                    let group = (row / 2) * 2 + column / 2;
                    assert_eq!(nonzero[row * 4 + column], nonzero_8x8[group]);
                }
            }
        }
    }

    #[test]
    fn luma_8x8_coded_block_pattern_does_not_imply_nonzero_coefficients() {
        let nonzero = deblocking_luma_nonzero_flags(true, [false; 16], [false; 4]);
        assert_eq!(nonzero, [false; 16]);
    }

    #[test]
    fn luma_4x4_residual_flags_remain_in_raster_order() {
        for (scan, raster) in [0, 1, 4, 5, 2, 3, 6, 7, 8, 9, 12, 13, 10, 11, 14, 15]
            .into_iter()
            .enumerate()
        {
            let mut flags = [false; 16];
            flags[scan] = true;
            let nonzero = deblocking_luma_nonzero_flags(false, flags, [false; 4]);
            assert!(nonzero[raster]);
            assert_eq!(nonzero.into_iter().filter(|flag| *flag).count(), 1);
        }
    }
}

fn picture_deblocking_macroblock(
    entry: &crate::CabacDecodedSliceMacroblock,
    header: &crate::SliceHeader,
    pps: &crate::PpsInfo,
    reference_lists: [&[crate::ReferencePicture]; 2],
    slice_id: usize,
    current_qpy: i32,
) -> io::Result<(
    crate::reconstruction::PictureDeblockingMacroblock,
    Option<i32>,
)> {
    use crate::{CabacPBMacroblockPrediction, CabacSliceMacroblockResult};

    let mut nonzero = [false; 16];
    let mut predictions: [Vec<crate::LumaPredictionVector>; 16] = Default::default();
    let (qpy, updated_qpy, intra, transform_size_8x8) = match &entry.result {
        CabacSliceMacroblockResult::Intra(macroblock) => {
            nonzero = deblocking_luma_nonzero_flags(
                macroblock.transform_size_8x8,
                std::array::from_fn(|index| {
                    macroblock.luma_4x4_coded_block_flags[index]
                        || macroblock.luma_ac_coded_block_flags[index]
                }),
                macroblock.luma_8x8_nonzero,
            );
            let qpy = if macroblock.macroblock_type == 25 {
                0
            } else {
                macroblock.qpy
            };
            (qpy, Some(qpy), true, macroblock.transform_size_8x8)
        }
        CabacSliceMacroblockResult::Inter(layer) => {
            let (qpy, transform_size_8x8, partitions, intra, updated_qpy): (
                i32,
                bool,
                Vec<(
                    usize,
                    usize,
                    usize,
                    usize,
                    [bool; 2],
                    [i32; 2],
                    [crate::MotionVector; 2],
                )>,
                bool,
                Option<i32>,
            ) = match &layer.macroblock {
                CabacPBMacroblockPrediction::PSkip(macroblock) => (
                    current_qpy,
                    false,
                    vec![(
                        0,
                        0,
                        16,
                        16,
                        [true, false],
                        [macroblock.reference_index, -1],
                        [macroblock.motion_vector, crate::MotionVector::default()],
                    )],
                    false,
                    None,
                ),
                CabacPBMacroblockPrediction::BSkip(macroblock) => (
                    current_qpy,
                    false,
                    vec![(
                        0,
                        0,
                        16,
                        16,
                        macroblock.prediction_flags,
                        macroblock.reference_indices,
                        macroblock.motion_vectors,
                    )],
                    false,
                    None,
                ),
                CabacPBMacroblockPrediction::Intra(macroblock) => {
                    nonzero = deblocking_luma_nonzero_flags(
                        macroblock.transform_size_8x8,
                        std::array::from_fn(|index| {
                            macroblock.luma_4x4_coded_block_flags[index]
                                || macroblock.luma_ac_coded_block_flags[index]
                        }),
                        macroblock.luma_8x8_nonzero,
                    );
                    let qpy = if macroblock.macroblock_type == 25 {
                        0
                    } else {
                        macroblock.qpy
                    };
                    (
                        qpy,
                        macroblock.transform_size_8x8,
                        Vec::new(),
                        true,
                        Some(qpy),
                    )
                }
                CabacPBMacroblockPrediction::Inter(macroblock) => {
                    nonzero = deblocking_luma_nonzero_flags(
                        macroblock.transform_size_8x8,
                        macroblock.luma_4x4_coded_block_flags,
                        macroblock.luma_8x8_nonzero,
                    );
                    (
                        macroblock.qpy,
                        macroblock.transform_size_8x8,
                        macroblock
                            .partitions
                            .iter()
                            .map(|partition| {
                                (
                                    partition.x,
                                    partition.y,
                                    partition.width,
                                    partition.height,
                                    partition.prediction_flags,
                                    partition.reference_indices,
                                    partition.motion_vectors,
                                )
                            })
                            .collect(),
                        false,
                        Some(macroblock.qpy),
                    )
                }
            };
            if intra {
                (qpy, updated_qpy, true, transform_size_8x8)
            } else {
                for cell in 0..16 {
                    let x = (cell % 4) * 4;
                    let y = (cell / 4) * 4;
                    let partition = partitions
                        .iter()
                        .find(|(px, py, width, height, _, _, _)| {
                            *px <= x && x < px + width && *py <= y && y < py + height
                        })
                        .ok_or_else(|| {
                            invalid("decoded inter macroblock has incomplete partitions")
                        })?;
                    for list_index in 0..2 {
                        if !partition.4[list_index] {
                            continue;
                        }
                        let reference_index = usize::try_from(partition.5[list_index])
                            .map_err(|_| invalid("decoded inter reference index is negative"))?;
                        let reference = reference_lists[list_index]
                            .get(reference_index)
                            .ok_or_else(|| {
                                invalid("decoded inter reference index is unavailable")
                            })?;
                        predictions[cell].push(crate::LumaPredictionVector {
                            reference_picture_id: reference.identifier,
                            motion_vector: partition.6[list_index],
                        });
                    }
                }
                (qpy, updated_qpy, false, transform_size_8x8)
            }
        }
    };

    Ok((
        crate::reconstruction::PictureDeblockingMacroblock {
            qpy,
            intra,
            transform_size_8x8,
            nonzero,
            predictions,
            slice_id,
            disable_idc: header.disable_deblocking_filter_idc.unwrap_or(0),
            alpha_offset_div2: header.slice_alpha_c0_offset_div2,
            beta_offset_div2: header.slice_beta_offset_div2,
            chroma_qp_offsets: [
                pps.chroma_qp_index_offset as i32,
                pps.second_chroma_qp_index_offset as i32,
            ],
        },
        updated_qpy,
    ))
}

fn slice_reference_lists(
    references: &[crate::ReferencePicture],
    header: &crate::SliceHeader,
    pps: &crate::PpsInfo,
    sps: &crate::SpsInfo,
    picture_order_count: i64,
) -> io::Result<(Vec<crate::ReferencePicture>, Vec<crate::ReferencePicture>)> {
    let max_frame_num = 1_u32
        .checked_shl(sps.log2_max_frame_num_minus4 + 4)
        .ok_or_else(|| invalid("MaxFrameNum is outside the supported range"))?;
    let (mut list0, mut list1) = if header.slice_type % 5 == 0 {
        (
            crate::build_p_reference_list(references, header.frame_num, max_frame_num)?,
            Vec::new(),
        )
    } else {
        crate::build_b_reference_lists(references, picture_order_count)
    };
    list0 = crate::apply_reference_list_modifications(
        &list0,
        references,
        header.frame_num,
        max_frame_num,
        &header.ref_pic_list_reordering_l0,
    )?;
    if header.slice_type % 5 == 1 {
        list1 = crate::apply_reference_list_modifications(
            &list1,
            references,
            header.frame_num,
            max_frame_num,
            &header.ref_pic_list_reordering_l1,
        )?;
    }
    let active_l0 = if header.num_ref_idx_active_override {
        header.num_ref_idx_l0_active_minus1
    } else {
        pps.num_ref_idx_l0_default_active_minus1
    };
    let active_l1 = if header.num_ref_idx_active_override {
        header.num_ref_idx_l1_active_minus1
    } else {
        pps.num_ref_idx_l1_default_active_minus1
    };
    list0.truncate(active_l0 as usize + 1);
    if header.slice_type % 5 == 1 {
        list1.truncate(active_l1 as usize + 1);
    } else {
        list1.clear();
    }
    Ok((list0, list1))
}

fn slice_history_entry<'a>(
    history: &'a [crate::CabacDecodedSliceMacroblock],
    index: Option<usize>,
    expected_address: Option<usize>,
) -> io::Result<Option<&'a crate::CabacDecodedSliceMacroblock>> {
    let (Some(index), Some(expected_address)) = (index, expected_address) else {
        return Ok(None);
    };
    let entry = history
        .get(index)
        .ok_or_else(|| invalid("decoded slice history index is invalid"))?;
    if entry.address != expected_address {
        return Err(invalid("decoded slice history is not contiguous"));
    }
    Ok(Some(entry))
}

fn colocated_motion_facts(
    result: &crate::CabacSliceMacroblockResult,
) -> io::Result<[crate::ColocatedMotionFact; 16]> {
    let inactive = crate::ColocatedMotionFact {
        reference_index: -1,
        motion_vector: crate::MotionVector::default(),
    };
    let mut facts = [inactive; 16];
    let mut set_fact = |cell: usize,
                        prediction_flags: [bool; 2],
                        references: [i32; 2],
                        vectors: [crate::MotionVector; 2]| {
        let list_index = if prediction_flags[0] {
            Some(0)
        } else if prediction_flags[1] {
            Some(1)
        } else {
            None
        };
        if let Some(list_index) = list_index {
            facts[cell] = crate::ColocatedMotionFact {
                reference_index: references[list_index],
                motion_vector: vectors[list_index],
            };
        }
    };

    match result {
        crate::CabacSliceMacroblockResult::Intra(_) => {}
        crate::CabacSliceMacroblockResult::Inter(layer) => match &layer.macroblock {
            crate::CabacPBMacroblockPrediction::Intra(_) => {}
            crate::CabacPBMacroblockPrediction::PSkip(decoded) => {
                let fact = crate::ColocatedMotionFact {
                    reference_index: decoded.reference_index,
                    motion_vector: decoded.motion_vector,
                };
                facts.fill(fact);
            }
            crate::CabacPBMacroblockPrediction::BSkip(decoded) => {
                facts.fill(inactive);
                for fact in &mut facts {
                    let list_index = if decoded.prediction_flags[0] {
                        Some(0)
                    } else if decoded.prediction_flags[1] {
                        Some(1)
                    } else {
                        None
                    };
                    if let Some(list_index) = list_index {
                        *fact = crate::ColocatedMotionFact {
                            reference_index: decoded.reference_indices[list_index],
                            motion_vector: decoded.motion_vectors[list_index],
                        };
                    }
                }
            }
            crate::CabacPBMacroblockPrediction::Inter(decoded) => {
                for cell in 0..16 {
                    let x = (cell % 4) * 4;
                    let y = (cell / 4) * 4;
                    let partition = decoded.partitions.iter().find(|partition| {
                        partition.x <= x
                            && x < partition.x + partition.width
                            && partition.y <= y
                            && y < partition.y + partition.height
                    });
                    let partition = partition.ok_or_else(|| {
                        invalid("decoded reference macroblock has incomplete motion partitions")
                    })?;
                    set_fact(
                        cell,
                        partition.prediction_flags,
                        partition.reference_indices,
                        partition.motion_vectors,
                    );
                }
            }
        },
    }
    Ok(facts)
}

pub struct H264Decoder {
    pub sample_count: Option<usize>,
    pub sequence_parameter_sets: Vec<crate::sps::SpsInfo>,
    pub picture_parameter_sets: Vec<crate::pps::PpsInfo>,
    pub reference_picture_buffer: crate::reconstruction::ReferencePictureBuffer,
    pub presentation_order_buffer: crate::reconstruction::PresentationOrderBuffer,
    sample_reader: Option<crate::videosamplereader::VideoSampleReader>,
    first_sync_sample_index: Option<u64>,
    unsupported_feature: bool,
    checkpoint_parameter_sets: (Vec<Vec<u8>>, Vec<Vec<u8>>),
    checkpoint_path: Option<PathBuf>,
    gop_sync_sample_index: Option<u64>,
    last_decoded_sample_index: Option<u64>,
    maximum_long_term_frame_idx: Option<u32>,
    poc_type0_state: crate::PocType0State,
    poc_type12_state: crate::PocType12State,
    poc_cache: Option<(crate::PictureIdentity, i64)>,
    decoded_frames: HashMap<usize, Yuv420Frame>,
    decoded_frame_order: VecDeque<usize>,
    frame_cache_size: usize,
    // Zero selects default_decoder_threads().
    threads: usize,
    verbose: bool,
}

impl Default for H264Decoder {
    fn default() -> Self {
        Self {
            sample_count: None,
            sequence_parameter_sets: Vec::new(),
            picture_parameter_sets: Vec::new(),
            reference_picture_buffer: crate::reconstruction::ReferencePictureBuffer::default(),
            presentation_order_buffer: crate::reconstruction::PresentationOrderBuffer::new(0),
            sample_reader: None,
            first_sync_sample_index: None,
            unsupported_feature: false,
            checkpoint_parameter_sets: (Vec::new(), Vec::new()),
            checkpoint_path: None,
            gop_sync_sample_index: None,
            last_decoded_sample_index: None,
            maximum_long_term_frame_idx: None,
            poc_type0_state: crate::PocType0State::default(),
            poc_type12_state: crate::PocType12State::default(),
            poc_cache: None,
            decoded_frames: HashMap::new(),
            decoded_frame_order: VecDeque::new(),
            frame_cache_size: 8,
            threads: 0,
            verbose: false,
        }
    }
}

pub use crate::codec::default_decoder_threads;

impl H264Decoder {
    /// Sets the number of decoder threads; zero selects `default_decoder_threads()`.
    pub fn set_threads(&mut self, threads: usize) {
        self.threads = threads;
    }

    pub fn set_verbose(&mut self, verbose: bool) {
        self.verbose = verbose;
    }

    /// Returns the maximum number of recently decoded output frames retained.
    pub fn frame_cache_size(&self) -> usize {
        self.frame_cache_size
    }

    /// Sets the decoded-output cache capacity; zero disables the cache.
    pub fn set_frame_cache_size(&mut self, size: usize) {
        self.frame_cache_size = size;
        self.trim_decoded_frame_cache();
    }

    pub fn frame_cache_len(&self) -> usize {
        self.decoded_frames.len()
    }

    fn cache_decoded_frame(&mut self, index: usize, frame: Yuv420Frame) {
        if self.frame_cache_size == 0 {
            return;
        }
        if let Some(position) = self
            .decoded_frame_order
            .iter()
            .position(|cached| *cached == index)
        {
            self.decoded_frame_order.remove(position);
        }
        self.decoded_frames.insert(index, frame);
        self.decoded_frame_order.push_back(index);
        self.trim_decoded_frame_cache();
    }

    fn trim_decoded_frame_cache(&mut self) {
        while self.decoded_frame_order.len() > self.frame_cache_size {
            if let Some(oldest) = self.decoded_frame_order.pop_front() {
                self.decoded_frames.remove(&oldest);
            }
        }
    }

    pub fn threads(&self) -> usize {
        if self.threads == 0 {
            default_decoder_threads()
        } else {
            self.threads
        }
    }
}

impl H264Decoder {
    pub fn new(sample_reader: crate::videosamplereader::VideoSampleReader) -> Self {
        let first_sync_sample_index = sample_reader.first_sync_sample_index();
        let sample_count = sample_reader.sample_count();
        let configuration = sample_reader.avc_configuration().cloned().unwrap_or_default();
        let mut unsupported_feature = sample_reader.codec() != crate::VideoCodec::H264;
        let mut sequence_parameter_sets = Vec::new();
        for nal in &configuration.sequence_parameter_sets {
            match crate::sps::parse_sps(nal) {
                Ok(info) => sequence_parameter_sets.push(info),
                Err(_) => unsupported_feature = true,
            }
        }
        let picture_parameter_sets = configuration
            .picture_parameter_sets
            .iter()
            .filter_map(|nal| crate::pps::parse_pps(nal).ok())
            .collect();
        let checkpoint_parameter_sets = (
            configuration.sequence_parameter_sets.clone(),
            configuration.picture_parameter_sets.clone(),
        );
        Self {
            sample_count: Some(sample_count),
            sequence_parameter_sets,
            picture_parameter_sets,
            reference_picture_buffer: crate::reconstruction::ReferencePictureBuffer::default(),
            presentation_order_buffer: crate::reconstruction::PresentationOrderBuffer::new(0),
            sample_reader: Some(sample_reader),
            first_sync_sample_index,
            unsupported_feature,
            checkpoint_parameter_sets,
            checkpoint_path: None,
            gop_sync_sample_index: None,
            last_decoded_sample_index: None,
            maximum_long_term_frame_idx: None,
            poc_type0_state: crate::PocType0State::default(),
            poc_type12_state: crate::PocType12State::default(),
            poc_cache: None,
            decoded_frames: HashMap::new(),
            decoded_frame_order: VecDeque::new(),
            frame_cache_size: 8,
            threads: 0,
            verbose: false,
        }
    }

    pub fn new_with_checkpoint_path<P: AsRef<Path>>(
        sample_reader: crate::videosamplereader::VideoSampleReader,
        path: P,
    ) -> io::Result<Self> {
        let path = path.as_ref().to_path_buf();
        let mut decoder = Self::new(sample_reader);
        decoder.checkpoint_path = Some(path.clone());
        if path.is_file() {
            decoder.load_checkpoint(path)?;
        }
        Ok(decoder)
    }

    pub fn new_with_checkpoint_dir<P: AsRef<Path>>(
        sample_reader: crate::videosamplereader::VideoSampleReader,
        directory: P,
    ) -> io::Result<Self> {
        let stem = sample_reader
            .path()
            .and_then(Path::file_stem)
            .map(|value| value.to_string_lossy().into_owned())
            .unwrap_or_else(|| "decoder".to_owned());
        Self::new_with_checkpoint_path(
            sample_reader,
            directory.as_ref().join(format!("{stem}.ckpt")),
        )
    }

    pub fn new_with_environment_checkpoint(
        sample_reader: crate::videosamplereader::VideoSampleReader,
    ) -> io::Result<Self> {
        if let Some(path) = env::var_os("PYGORVID_CHECKPOINT_PATH") {
            return Self::new_with_checkpoint_path(sample_reader, path);
        }
        if let Some(directory) = env::var_os("PYGORVID_CHECKPOINT_DIR") {
            return Self::new_with_checkpoint_dir(sample_reader, directory);
        }
        Ok(Self::new(sample_reader))
    }

    pub fn with_sample_count(sample_count: usize) -> Self {
        Self {
            sample_count: Some(sample_count),
            sequence_parameter_sets: Vec::new(),
            picture_parameter_sets: Vec::new(),
            reference_picture_buffer: crate::reconstruction::ReferencePictureBuffer::default(),
            presentation_order_buffer: crate::reconstruction::PresentationOrderBuffer::new(0),
            sample_reader: None,
            first_sync_sample_index: None,
            unsupported_feature: false,
            checkpoint_parameter_sets: (Vec::new(), Vec::new()),
            checkpoint_path: None,
            gop_sync_sample_index: None,
            last_decoded_sample_index: None,
            maximum_long_term_frame_idx: None,
            poc_type0_state: crate::PocType0State::default(),
            poc_type12_state: crate::PocType12State::default(),
            poc_cache: None,
            decoded_frames: HashMap::new(),
            decoded_frame_order: VecDeque::new(),
            frame_cache_size: 8,
            threads: 0,
            verbose: false,
        }
    }

    pub fn sample_count(&self) -> usize {
        self.sample_count.unwrap_or(0)
    }

    pub fn save_checkpoint<P: AsRef<Path>>(&self, path: P) -> io::Result<()> {
        let target = path.as_ref();
        let parent = target
            .parent()
            .filter(|parent| !parent.as_os_str().is_empty())
            .unwrap_or_else(|| Path::new("."));
        fs::create_dir_all(parent)?;
        let name = target
            .file_name()
            .ok_or_else(|| invalid_checkpoint("checkpoint path has no file name"))?
            .to_string_lossy();
        let nonce = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .unwrap_or_default()
            .as_nanos();
        let temporary = parent.join(format!(".{name}.tmp.{}.{}", std::process::id(), nonce));
        let result = (|| {
            let mut file = File::create(&temporary)?;
            let frame_indices: Vec<_> = self
                .decoded_frame_order
                .iter()
                .copied()
                .filter(|index| self.decoded_frames.contains_key(index))
                .collect();
            let state = DecoderCheckpoint {
                magic: *CHECKPOINT_MAGIC,
                version: CHECKPOINT_VERSION,
                sample_count: self.sample_count,
                sequence_parameter_sets: self.checkpoint_parameter_sets.0.clone(),
                picture_parameter_sets: self.checkpoint_parameter_sets.1.clone(),
                gop_sync_sample_index: self.gop_sync_sample_index,
                last_decoded_sample_index: self.last_decoded_sample_index,
                maximum_long_term_frame_idx: self.maximum_long_term_frame_idx,
                poc_type0_state: self.poc_type0_state,
                poc_type12_state: self.poc_type12_state,
                decoded_frames: frame_indices
                    .into_iter()
                    .map(|index| (index, self.decoded_frames[&index].clone()))
                    .collect(),
                reference_pictures: self
                    .reference_picture_buffer
                    .references()
                    .into_iter()
                    .filter_map(|reference| self.reference_picture_buffer.get(reference.identifier))
                    .collect(),
            };
            bincode::DefaultOptions::new()
                .with_fixint_encoding()
                .with_limit(MAX_CHECKPOINT_BYTES)
                .serialize_into(&mut file, &state)
                .map_err(|error| {
                    invalid_checkpoint(&format!("checkpoint encode failed: {error}"))
                })?;
            file.sync_all()?;
            fs::rename(&temporary, target)?;
            Ok(())
        })();
        if result.is_err() {
            let _ = fs::remove_file(&temporary);
        }
        result
    }

    pub fn load_checkpoint<P: AsRef<Path>>(&mut self, path: P) -> io::Result<()> {
        let file = File::open(path)?;
        if file.metadata()?.len() > MAX_CHECKPOINT_BYTES {
            return Err(invalid_checkpoint("checkpoint file exceeds the size limit"));
        }
        let state: DecoderCheckpoint = bincode::DefaultOptions::new()
            .with_fixint_encoding()
            .with_limit(MAX_CHECKPOINT_BYTES)
            .reject_trailing_bytes()
            .deserialize_from(file)
            .map_err(|error| invalid_checkpoint(&format!("checkpoint decode failed: {error}")))?;
        if state.magic != *CHECKPOINT_MAGIC {
            return Err(invalid_checkpoint("invalid checkpoint file format"));
        }
        if state.version != CHECKPOINT_VERSION {
            return Err(invalid_checkpoint("unsupported checkpoint version"));
        }
        if state.sample_count != self.sample_count {
            return Err(invalid_checkpoint(
                "checkpoint sample count does not match the loaded video stream",
            ));
        }
        if self.sample_reader.is_some()
            && (state.sequence_parameter_sets != self.checkpoint_parameter_sets.0
                || state.picture_parameter_sets != self.checkpoint_parameter_sets.1)
        {
            return Err(invalid_checkpoint(
                "checkpoint parameter sets do not match the loaded video stream",
            ));
        }
        if let (Some(gop_sync), Some(last_decoded)) =
            (state.gop_sync_sample_index, state.last_decoded_sample_index)
        {
            if last_decoded < gop_sync
                || self
                    .sample_count
                    .is_some_and(|count| last_decoded >= count as u64)
            {
                return Err(invalid_checkpoint("checkpoint decode position is invalid"));
            }
        } else if state.gop_sync_sample_index.is_some() || state.last_decoded_sample_index.is_some()
        {
            return Err(invalid_checkpoint(
                "checkpoint decode position is incomplete",
            ));
        }

        let mut decoded_frames = HashMap::new();
        let mut decoded_frame_order = VecDeque::new();
        for (index, frame) in state.decoded_frames {
            if self.sample_count.is_some_and(|count| index >= count)
                || decoded_frames.contains_key(&index)
            {
                return Err(invalid_checkpoint("checkpoint frame index is invalid"));
            }
            validate_checkpoint_frame(&frame)?;
            decoded_frames.insert(index, frame);
            decoded_frame_order.push_back(index);
        }

        let mut reference_picture_buffer = crate::ReferencePictureBuffer::default();
        for picture in state.reference_pictures {
            if self
                .sample_count
                .is_some_and(|count| picture.reference.identifier as usize >= count)
                || reference_picture_buffer
                    .references()
                    .iter()
                    .any(|reference| reference.identifier == picture.reference.identifier)
            {
                return Err(invalid_checkpoint(
                    "checkpoint reference identifier is invalid",
                ));
            }
            validate_checkpoint_frame(&picture.frame)?;
            if picture
                .motion_field
                .windows(2)
                .any(|pair| pair[0].0 >= pair[1].0)
            {
                return Err(invalid_checkpoint("checkpoint motion field is invalid"));
            }
            reference_picture_buffer
                .store_with_motion_field(
                    picture.reference,
                    &picture.frame,
                    std::sync::Arc::unwrap_or_clone(picture.motion_field),
                )
                .map_err(|_| invalid_checkpoint("checkpoint reference picture is invalid"))?;
        }

        self.decoded_frames = decoded_frames;
        self.decoded_frame_order = decoded_frame_order;
        self.trim_decoded_frame_cache();
        self.reference_picture_buffer = reference_picture_buffer;
        self.gop_sync_sample_index = state.gop_sync_sample_index;
        self.last_decoded_sample_index = state.last_decoded_sample_index;
        self.maximum_long_term_frame_idx = state.maximum_long_term_frame_idx;
        self.poc_type0_state = state.poc_type0_state;
        self.poc_type12_state = state.poc_type12_state;
        self.poc_cache = None;
        Ok(())
    }

    pub fn reference_pictures(&self) -> Vec<crate::reconstruction::ReferencePicture> {
        self.reference_picture_buffer.references()
    }

    pub fn store_reference_picture(
        &mut self,
        reference: crate::reconstruction::ReferencePicture,
        frame: &Yuv420Frame,
    ) -> io::Result<()> {
        self.reference_picture_buffer.store(reference, frame)
    }

    pub fn store_reference_picture_with_macroblocks(
        &mut self,
        reference: crate::reconstruction::ReferencePicture,
        frame: &Yuv420Frame,
        macroblocks: &[crate::CabacDecodedSliceMacroblock],
    ) -> io::Result<()> {
        let motion_field = macroblocks
            .iter()
            .map(|entry| Ok((entry.address, colocated_motion_facts(&entry.result)?)))
            .collect::<io::Result<Vec<_>>>()?;
        self.reference_picture_buffer
            .store_with_motion_field(reference, frame, motion_field)
    }

    pub fn queue_presentation(
        &mut self,
        picture: &crate::reconstruction::PresentationPicture,
    ) -> io::Result<Option<crate::reconstruction::PresentationPicture>> {
        self.presentation_order_buffer.push(picture)
    }

    pub fn first_sync_sample_index(&self) -> Option<u64> {
        self.first_sync_sample_index
    }

    pub fn decode_order_dependency_samples(
        &mut self,
        presentation_index: usize,
    ) -> io::Result<Vec<crate::videosamplereader::CompressedSample>> {
        self.sample_reader
            .as_mut()
            .ok_or_else(|| {
                io::Error::new(io::ErrorKind::NotConnected, "sample reader is unavailable")
            })?
            .decode_order_dependency_samples(presentation_index)
    }

    /// Splits and groups coded slices from the dependency window without advancing sample order.
    pub fn dependency_slice_pictures(
        &mut self,
        presentation_index: usize,
    ) -> io::Result<Vec<Vec<DependencySlice>>> {
        let (nal_length_size, picture_parameter_sets) = {
            let reader = self.sample_reader.as_ref().ok_or_else(|| {
                io::Error::new(io::ErrorKind::NotConnected, "sample reader is unavailable")
            })?;
            let configuration = reader.configuration();
            (
                usize::from(configuration.nal_length_size),
                configuration.picture_parameter_sets.clone(),
            )
        };
        let sequence_parameter_sets = self.sequence_parameter_sets.clone();
        let samples = self.decode_order_dependency_samples(presentation_index)?;
        let mut pictures: Vec<Vec<DependencySlice>> = Vec::new();
        for sample in samples {
            let nal_units = crate::split_length_prefixed_nals(&sample.data, nal_length_size)
                .map_err(|error| invalid(format!("sample {}: {error}", sample.index)))?;
            for nal_unit in nal_units {
                let nal_header = crate::parse_nal_header(nal_unit)
                    .map_err(|error| invalid(format!("sample {}: {error}", sample.index)))?;
                if !matches!(nal_header.unit_type, 1 | 5) {
                    if (1..=5).contains(&nal_header.unit_type) {
                        return Err(invalid(format!(
                            "unsupported VCL NAL unit type {}",
                            nal_header.unit_type
                        )));
                    }
                    continue;
                }
                let mut parsed_header = None;
                let mut last_error = None;
                for pps_nal in &picture_parameter_sets {
                    let pps = crate::parse_pps(pps_nal)?;
                    let Some(sps) = sequence_parameter_sets
                        .iter()
                        .find(|sps| sps.sps_id == pps.sequence_parameter_set_id)
                    else {
                        continue;
                    };
                    match crate::parse_slice_header(nal_unit, sps, &pps) {
                        Ok(header) => {
                            parsed_header = Some(header);
                            break;
                        }
                        Err(error) => last_error = Some(error),
                    }
                }
                let header = parsed_header.ok_or_else(|| {
                    last_error.unwrap_or_else(|| invalid("slice references an unavailable SPS"))
                })?;
                let slice = DependencySlice {
                    sample_index: sample.index,
                    dts_ticks: sample.dts_ticks,
                    pts_ticks: sample.pts_ticks,
                    is_sync: sample.is_sync,
                    nal_unit: nal_unit.to_vec(),
                    header,
                };
                if pictures.last().is_none_or(|picture| {
                    !crate::same_primary_picture(&picture[0].header, &slice.header)
                }) {
                    pictures.push(vec![slice]);
                } else if let Some(picture) = pictures.last_mut() {
                    picture.push(slice);
                }
            }
        }
        Ok(pictures)
    }

    pub fn decode_dependency_slice_data<F>(
        &self,
        dependency_slice: &DependencySlice,
        builder: &mut crate::Yuv420FrameBuilder,
        input_provider: F,
    ) -> io::Result<Vec<crate::CabacDecodedSliceMacroblock>>
    where
        F: FnMut(
            usize,
            &[crate::CabacDecodedSliceMacroblock],
        ) -> io::Result<crate::CabacSliceMacroblockInput>,
    {
        let pps = self
            .picture_parameter_sets
            .iter()
            .find(|pps| {
                pps.picture_parameter_set_id == dependency_slice.header.picture_parameter_set_id
            })
            .ok_or_else(|| invalid("dependency slice references an unavailable PPS"))?;
        let sps = self
            .sequence_parameter_sets
            .iter()
            .find(|sps| sps.sps_id == pps.sequence_parameter_set_id)
            .ok_or_else(|| invalid("dependency slice references an unavailable SPS"))?;
        if !pps.entropy_coding_mode {
            return Err(invalid("dependency slice does not use CABAC"));
        }
        if dependency_slice.header.slice_data_bit_offset % 8 != 0 {
            return Err(invalid("CABAC slice-data offset is not byte-aligned"));
        }
        let rbsp = crate::ebsp_to_rbsp(
            dependency_slice
                .nal_unit
                .get(1..)
                .ok_or_else(|| invalid("dependency slice NAL unit is empty"))?,
        )?;
        let byte_offset = dependency_slice.header.slice_data_bit_offset / 8;
        let cabac_data = rbsp
            .get(byte_offset..)
            .ok_or_else(|| invalid("CABAC slice-data offset exceeds the NAL unit"))?;
        let mut cabac = crate::CabacArithmeticDecoder::new(cabac_data)?;
        let slice_qpy = 26_i64 + pps.pic_init_qp_minus26 + dependency_slice.header.slice_qp_delta;
        let slice_qpy = i32::try_from(slice_qpy)
            .map_err(|_| invalid("dependency slice QP is outside the supported range"))?;
        let contexts = crate::new_cabac_slice_contexts(
            dependency_slice.header.slice_type,
            dependency_slice.header.cabac_init_idc.unwrap_or(0),
            slice_qpy,
        )?;
        let mut contexts: [crate::CabacContextModel; crate::CABAC_CONTEXT_COUNT] = contexts
            .try_into()
            .map_err(|_| invalid("CABAC context table has an invalid length"))?;
        let width_in_mbs = usize::try_from(sps.coded_width / 16)
            .map_err(|_| invalid("picture width exceeds the host address space"))?;
        let height_in_mbs = usize::try_from(sps.coded_height / 16)
            .map_err(|_| invalid("picture height exceeds the host address space"))?;
        let macroblock_count = width_in_mbs
            .checked_mul(height_in_mbs)
            .ok_or_else(|| invalid("picture macroblock count overflows"))?;
        cabac.decode_slice_data(
            dependency_slice.header.slice_type,
            dependency_slice.header.first_macroblock_in_slice as usize,
            macroblock_count,
            &mut contexts,
            builder,
            input_provider,
        )
    }

    fn decode_dependency_slice_data_from_history(
        &mut self,
        dependency_slice: &DependencySlice,
        builder: &mut crate::Yuv420FrameBuilder,
    ) -> io::Result<Vec<crate::CabacDecodedSliceMacroblock>> {
        let pps = self
            .picture_parameter_sets
            .iter()
            .find(|pps| {
                pps.picture_parameter_set_id == dependency_slice.header.picture_parameter_set_id
            })
            .cloned()
            .ok_or_else(|| invalid("dependency slice references an unavailable PPS"))?;
        let sps = self
            .sequence_parameter_sets
            .iter()
            .find(|sps| sps.sps_id == pps.sequence_parameter_set_id)
            .cloned()
            .ok_or_else(|| invalid("dependency slice references an unavailable SPS"))?;
        if pps.entropy_coding_mode && dependency_slice.header.slice_data_bit_offset % 8 != 0 {
            return Err(invalid("CABAC slice-data offset is not byte-aligned"));
        }
        let rbsp = crate::ebsp_to_rbsp(
            dependency_slice
                .nal_unit
                .get(1..)
                .ok_or_else(|| invalid("dependency slice NAL unit is empty"))?,
        )?;
        let byte_offset = dependency_slice.header.slice_data_bit_offset / 8;
        let cabac_data = rbsp
            .get(byte_offset..)
            .ok_or_else(|| invalid("CABAC slice-data offset exceeds the NAL unit"))?;
        let mut cabac = if pps.entropy_coding_mode {
            crate::CabacArithmeticDecoder::new(cabac_data)?
        } else {
            crate::CabacArithmeticDecoder::new_cavlc(&rbsp, dependency_slice.header.slice_data_bit_offset)?
        };
        let slice_qpy = i32::try_from(
            26_i64 + pps.pic_init_qp_minus26 + dependency_slice.header.slice_qp_delta,
        )
        .map_err(|_| invalid("dependency slice QP is outside the supported range"))?;
        let contexts = crate::new_cabac_slice_contexts(
            dependency_slice.header.slice_type,
            dependency_slice.header.cabac_init_idc.unwrap_or(0),
            slice_qpy,
        )?;
        let mut contexts: [crate::CabacContextModel; crate::CABAC_CONTEXT_COUNT] = contexts
            .try_into()
            .map_err(|_| invalid("CABAC context table has an invalid length"))?;
        let width_in_mbs = usize::try_from(sps.coded_width / 16)
            .map_err(|_| invalid("picture width exceeds the host address space"))?;
        let height_in_mbs = usize::try_from(sps.coded_height / 16)
            .map_err(|_| invalid("picture height exceeds the host address space"))?;
        let macroblock_count = width_in_mbs
            .checked_mul(height_in_mbs)
            .ok_or_else(|| invalid("picture macroblock count overflows"))?;
        let threads = self.threads();
        cabac.decode_slice_data_threaded(
            dependency_slice.header.slice_type,
            dependency_slice.header.first_macroblock_in_slice as usize,
            macroblock_count,
            &mut contexts,
            builder,
            threads,
            |address, history| {
                let input =
                    self.macroblock_input_from_history(dependency_slice, address, history)?;
                if self.verbose && (345..=349).contains(&address) {
                    if let crate::CabacSliceMacroblockInput::Inter(layer) = &input {
                        eprintln!(
                            "input mb={address} hist={} inter_qpy={}/{} intra_qpy={:?}/{:?} cbp={}/{}",
                            history.len(),
                            layer.residual_input.previous_qpy,
                            layer.residual_input.previous_qp_delta,
                            layer.intra_input.map(|intra| intra.previous_qpy),
                            layer.intra_input.map(|intra| intra.previous_qp_delta),
                            layer.residual_input.left_luma_cbp,
                            layer.residual_input.top_luma_cbp
                        );
                        if address == 349 {
                            if let Some(intra) = layer.intra_input {
                                eprintln!(
                                    "mb349 intra neighbors left={:?} top={:?} dc_edges left={:?} top={:?} chroma_left={:?} chroma_top={:?}",
                                    history.last().map(|entry| match &entry.result {
                                        crate::CabacSliceMacroblockResult::Intra(mb) => format!("intra:{}", mb.macroblock_type),
                                        crate::CabacSliceMacroblockResult::Inter(layer) => match &layer.macroblock {
                                            crate::CabacPBMacroblockPrediction::Inter(mb) => format!("inter:{}", mb.macroblock_type),
                                            crate::CabacPBMacroblockPrediction::Intra(mb) => format!("wrapped-intra:{}", mb.macroblock_type),
                                            crate::CabacPBMacroblockPrediction::PSkip(_) => "p-skip".to_owned(),
                                            crate::CabacPBMacroblockPrediction::BSkip(_) => "b-skip".to_owned(),
                                        },
                                    }),
                                    history.iter().find(|entry| entry.address + (sps.coded_width as usize / 16) == address).map(|entry| match &entry.result {
                                        crate::CabacSliceMacroblockResult::Intra(mb) => format!("intra:{}", mb.macroblock_type),
                                        crate::CabacSliceMacroblockResult::Inter(layer) => match &layer.macroblock {
                                            crate::CabacPBMacroblockPrediction::Inter(mb) => format!("inter:{}", mb.macroblock_type),
                                            crate::CabacPBMacroblockPrediction::Intra(mb) => format!("wrapped-intra:{}", mb.macroblock_type),
                                            crate::CabacPBMacroblockPrediction::PSkip(_) => "p-skip".to_owned(),
                                            crate::CabacPBMacroblockPrediction::BSkip(_) => "b-skip".to_owned(),
                                        },
                                    }),
                                    intra.luma_16x16_left_edge,
                                    intra.luma_16x16_top_edge,
                                    intra.chroma_left_edges,
                                    intra.chroma_top_edges
                                );
                            }
                        }
                    }
                }
                Ok(input)
            },
        )
    }

    /// Builds the I or P/B CABAC input selected by the dependency slice type.
    pub fn macroblock_input_from_history(
        &mut self,
        dependency_slice: &DependencySlice,
        macroblock_address: usize,
        history: &[crate::CabacDecodedSliceMacroblock],
    ) -> io::Result<crate::CabacSliceMacroblockInput> {
        match dependency_slice.header.slice_type % 5 {
            2 => self
                .intra_macroblock_input_from_history(dependency_slice, macroblock_address, history)
                .map(crate::CabacSliceMacroblockInput::Intra),
            0 | 1 => self
                .inter_macroblock_input_from_history(dependency_slice, macroblock_address, history)
                .map(crate::CabacSliceMacroblockInput::Inter),
            _ => Err(invalid(
                "history input builder supports only I, P, and B slices",
            )),
        }
    }

    fn picture_order_count(
        &mut self,
        header: &crate::SliceHeader,
        sps: &crate::sps::SpsInfo,
    ) -> io::Result<i64> {
        let identity = header.picture_identity();
        if let Some((cached_identity, cached_poc)) = self.poc_cache {
            if cached_identity == identity {
                return Ok(cached_poc);
            }
        }
        let max_frame_num = 1_u32
            .checked_shl(sps.log2_max_frame_num_minus4 + 4)
            .ok_or_else(|| invalid("MaxFrameNum is outside the supported range"))?;
        let poc = match sps.pic_order_cnt_type {
            0 => {
                let max_poc_lsb = 1_u32
                    .checked_shl(sps.log2_max_pic_order_cnt_lsb_minus4.unwrap_or(0) + 4)
                    .ok_or_else(|| invalid("MaxPicOrderCntLsb is invalid"))?;
                self.poc_type0_state.calculate(
                    max_poc_lsb,
                    header.pic_order_cnt_lsb,
                    header.delta_pic_order_bottom,
                    header.nal_ref_idc,
                    header.idr,
                )?
            }
            1 => self.poc_type12_state.calculate_type1(
                max_frame_num,
                header.frame_num,
                header.delta_pic_order_cnt0,
                header.delta_pic_order_cnt1,
                sps.offset_for_non_ref_pic.unwrap_or(0),
                sps.offset_for_top_to_bottom_field.unwrap_or(0),
                &sps.offset_for_ref_frame,
                header.nal_ref_idc,
                header.idr,
            )?,
            2 => self.poc_type12_state.calculate_type2(
                max_frame_num,
                header.frame_num,
                header.nal_ref_idc,
                header.idr,
            )?,
            _ => return Err(invalid("dependency slice POC type is unsupported")),
        };
        self.poc_cache = Some((identity, poc));
        Ok(poc)
    }

    /// Gathers raw I-slice references; reconstruction filters Intra_8x8 edges after internal references are gathered.
    pub fn intra_macroblock_input_from_history(
        &self,
        dependency_slice: &DependencySlice,
        macroblock_address: usize,
        history: &[crate::CabacDecodedSliceMacroblock],
    ) -> io::Result<crate::CabacIIntraMacroblockInput> {
        if !matches!(dependency_slice.header.slice_type % 5, 0 | 1 | 2) {
            return Err(invalid(
                "history input builder requires an I-, P-, or B-slice",
            ));
        }
        let pps = self
            .picture_parameter_sets
            .iter()
            .find(|pps| {
                pps.picture_parameter_set_id == dependency_slice.header.picture_parameter_set_id
            })
            .ok_or_else(|| invalid("dependency slice references an unavailable PPS"))?;
        let sps = self
            .sequence_parameter_sets
            .iter()
            .find(|sps| sps.sps_id == pps.sequence_parameter_set_id)
            .ok_or_else(|| invalid("dependency slice references an unavailable SPS"))?;
        let width_in_mbs = usize::try_from(sps.coded_width / 16)
            .map_err(|_| invalid("picture width exceeds the host address space"))?;
        let height_in_mbs = usize::try_from(sps.coded_height / 16)
            .map_err(|_| invalid("picture height exceeds the host address space"))?;
        let picture_macroblock_count = width_in_mbs
            .checked_mul(height_in_mbs)
            .ok_or_else(|| invalid("picture macroblock count overflows"))?;
        if macroblock_address >= picture_macroblock_count {
            return Err(invalid("macroblock address is outside the picture"));
        }
        let neighbor_indices = crate::derive_cabac_slice_neighbor_indices(
            macroblock_address,
            width_in_mbs,
            dependency_slice.header.first_macroblock_in_slice as usize,
            history.len(),
        )?;
        let column = macroblock_address % width_in_mbs;
        let left_entry = slice_history_entry(
            history,
            neighbor_indices.left,
            (column > 0).then(|| macroblock_address - 1),
        )?;
        let top_entry = slice_history_entry(
            history,
            neighbor_indices.top,
            (macroblock_address >= width_in_mbs).then(|| macroblock_address - width_in_mbs),
        )?;
        let top_right_entry = slice_history_entry(
            history,
            neighbor_indices.top_right,
            (macroblock_address >= width_in_mbs && column + 1 < width_in_mbs)
                .then(|| macroblock_address - width_in_mbs + 1),
        )?;
        let top_left_entry = slice_history_entry(
            history,
            neighbor_indices.top_left,
            (macroblock_address >= width_in_mbs && column > 0)
                .then(|| macroblock_address - width_in_mbs - 1),
        )?;
        let state = |entry: Option<&crate::CabacDecodedSliceMacroblock>, direction| {
            entry.map(|entry| crate::derive_cabac_macroblock_neighbor_coded_state(entry, direction))
        };
        let left_state = state(left_entry, crate::CabacMacroblockNeighborDirection::Left);
        let top_state = state(top_entry, crate::CabacMacroblockNeighborDirection::Top);
        let top_right_state = state(
            top_right_entry,
            crate::CabacMacroblockNeighborDirection::Top,
        );
        let top_left_state = state(
            top_left_entry,
            crate::CabacMacroblockNeighborDirection::Left,
        );
        let prediction_available = |state: Option<crate::CabacMacroblockNeighborCodedState>| {
            state.is_some_and(|state| {
                !pps.constrained_intra_pred
                    || state.is_intra_nxn
                    || state.is_intra16x16
                    || state.is_ipcm
            })
        };
        let left_prediction_available = prediction_available(left_state);
        let top_prediction_available = prediction_available(top_state);
        let top_right_prediction_available = prediction_available(top_right_state);
        let top_left_prediction_available = prediction_available(top_left_state);
        let edge_samples = |entry: Option<&crate::CabacDecodedSliceMacroblock>,
                            direction: crate::CabacMacroblockNeighborDirection,
                            available: bool|
         -> Option<crate::CabacMacroblockEdgeSamples> {
            if available {
                entry.map(|entry| crate::derive_cabac_macroblock_edge_samples(entry, direction))
            } else {
                None
            }
        };
        let left_samples = edge_samples(
            left_entry,
            crate::CabacMacroblockNeighborDirection::Left,
            left_prediction_available,
        );
        let top_samples = edge_samples(
            top_entry,
            crate::CabacMacroblockNeighborDirection::Top,
            top_prediction_available,
        );
        let top_right_samples = edge_samples(
            top_right_entry,
            crate::CabacMacroblockNeighborDirection::Top,
            top_right_prediction_available,
        );
        let top_left_samples = edge_samples(
            top_left_entry,
            crate::CabacMacroblockNeighborDirection::Left,
            top_left_prediction_available,
        );
        let gather_top = |edge_start: usize, output: &mut [u8]| -> bool {
            let Some(top) = top_samples else {
                return false;
            };
            for (offset, sample) in output.iter_mut().enumerate() {
                let column = edge_start + offset;
                *sample = if column < 16 {
                    top.luma[column]
                } else if let Some(top_right) = top_right_samples {
                    top_right.luma[column - 16]
                } else {
                    top.luma[15]
                };
            }
            true
        };
        let gather_left = |edge_start: usize, output: &mut [u8]| -> bool {
            let Some(left) = left_samples else {
                return false;
            };
            for (offset, sample) in output.iter_mut().enumerate() {
                *sample = left
                    .luma
                    .get(edge_start + offset)
                    .copied()
                    .unwrap_or(left.luma[15]);
            }
            true
        };
        let top_left_sample = |block_x: usize, block_y: usize| -> Option<u8> {
            if block_y == 0 {
                if block_x == 0 {
                    top_left_samples.map(|samples| samples.luma[15])
                } else {
                    top_samples.map(|samples| samples.luma[block_x - 1])
                }
            } else if block_x == 0 {
                left_samples.map(|samples| samples.luma[block_y - 1])
            } else {
                None
            }
        };
        const LUMA4X4_SCAN_TO_RASTER: [usize; 16] =
            [0, 1, 4, 5, 2, 3, 6, 7, 8, 9, 12, 13, 10, 11, 14, 15];
        let mut luma4_blocks = [crate::LumaIntra4x4Block {
            mode: 2,
            ..crate::LumaIntra4x4Block::default()
        }; 16];
        for (scan_index, raster_index) in LUMA4X4_SCAN_TO_RASTER.iter().enumerate() {
            let block_x = (raster_index % 4) * 4;
            let block_y = (raster_index / 4) * 4;
            if block_y == 0 {
                if gather_top(block_x, &mut luma4_blocks[scan_index].top[..8]) {
                    luma4_blocks[scan_index].top_available = true;
                }
            }
            if block_x == 0 {
                if gather_left(block_y, &mut luma4_blocks[scan_index].left[..8]) {
                    luma4_blocks[scan_index].left_available = true;
                }
            }
            if let Some(sample) = top_left_sample(block_x, block_y) {
                luma4_blocks[scan_index].top_left = sample;
                luma4_blocks[scan_index].top_left_available = true;
            }
        }
        let mut luma8_blocks = [crate::LumaIntra8x8Block {
            mode: 2,
            references_filtered: false,
            ..crate::LumaIntra8x8Block::default()
        }; 4];
        for block_index in 0..4 {
            let block_x = (block_index % 2) * 8;
            let block_y = (block_index / 2) * 8;
            if block_y == 0 {
                if gather_top(block_x, &mut luma8_blocks[block_index].top) {
                    luma8_blocks[block_index].top_available = true;
                }
            }
            if block_x == 0 {
                if gather_left(block_y, &mut luma8_blocks[block_index].left[..8]) {
                    let last_sample = luma8_blocks[block_index].left[7];
                    luma8_blocks[block_index].left[8..].fill(last_sample);
                    luma8_blocks[block_index].left_available = true;
                }
            }
            if let Some(sample) = top_left_sample(block_x, block_y) {
                luma8_blocks[block_index].top_left = sample;
                luma8_blocks[block_index].top_left_available = true;
            }
        }
        let slice_qpy = 26_i64 + pps.pic_init_qp_minus26 + dependency_slice.header.slice_qp_delta;
        let slice_qpy = i32::try_from(slice_qpy)
            .map_err(|_| invalid("dependency slice QP is outside the supported range"))?;
        let (mut previous_qpy, mut previous_qp_delta) = (slice_qpy, 0);
        // Equivalent to a forward scan: QP comes from the last coded entry, the delta from the last entry.
        for (position, previous) in history.iter().enumerate().rev() {
            let last = position + 1 == history.len();
            let coded = match &previous.result {
                crate::CabacSliceMacroblockResult::Intra(previous) => {
                    Some((previous.qpy, previous.qp_delta))
                }
                crate::CabacSliceMacroblockResult::Inter(layer) if layer.skipped => None,
                crate::CabacSliceMacroblockResult::Inter(layer) => match &layer.macroblock {
                    crate::CabacPBMacroblockPrediction::Intra(previous) => {
                        Some((previous.qpy, previous.qp_delta))
                    }
                    crate::CabacPBMacroblockPrediction::Inter(previous) => {
                        Some((previous.qpy, previous.qp_delta))
                    }
                    crate::CabacPBMacroblockPrediction::PSkip(_)
                    | crate::CabacPBMacroblockPrediction::BSkip(_) => None,
                },
            };
            if let Some((qpy, qp_delta)) = coded {
                previous_qpy = qpy;
                if last {
                    previous_qp_delta = qp_delta;
                }
                break;
            }
        }
        let mut input = crate::CabacIIntraMacroblockInput {
            slice_type: dependency_slice.header.slice_type,
            left_available: left_entry.is_some(),
            left_intra16_or_pcm: left_state
                .is_some_and(|state| state.is_intra16x16 || state.is_ipcm),
            top_available: top_entry.is_some(),
            top_intra16_or_pcm: top_state.is_some_and(|state| state.is_intra16x16 || state.is_ipcm),
            previous_qpy,
            previous_qp_delta,
            left_luma_cbp: left_state
                .filter(|state| !state.is_ipcm)
                .map_or(15, |state| state.coded_block_pattern_luma),
            top_luma_cbp: top_state
                .filter(|state| !state.is_ipcm)
                .map_or(15, |state| state.coded_block_pattern_luma),
            left_chroma_cbp: left_state.map_or(0, |state| {
                if state.is_ipcm {
                    2
                } else {
                    state.coded_block_pattern_chroma
                }
            }),
            top_chroma_cbp: top_state.map_or(0, |state| {
                if state.is_ipcm {
                    2
                } else {
                    state.coded_block_pattern_chroma
                }
            }),
            transform_8x8_mode_enabled: pps.transform_8x8_mode,
            left_has_8x8_transform: left_state.is_some_and(|state| state.transform_size_8x8),
            top_has_8x8_transform: top_state.is_some_and(|state| state.transform_size_8x8),
            top_modes_4x4: top_state
                .filter(|_| top_prediction_available)
                .map_or([2; 4], |state| state.luma_4x4_modes),
            left_modes_4x4: left_state
                .filter(|_| left_prediction_available)
                .map_or([2; 4], |state| state.luma_4x4_modes),
            top_modes_8x8: top_state
                .filter(|_| top_prediction_available)
                .map_or([2; 2], |state| state.luma_8x8_modes),
            left_modes_8x8: left_state
                .filter(|_| left_prediction_available)
                .map_or([2; 2], |state| state.luma_8x8_modes),
            top_mode_available: top_prediction_available,
            left_mode_available: left_prediction_available,
            luma_4x4_top_edge: top_state.map_or_default(|state| state.luma_4x4_edge),
            luma_4x4_left_edge: left_state.map_or_default(|state| state.luma_4x4_edge),
            luma_16x16_top_edge: top_state.map_or_default(|state| state.luma_16x16_edge),
            luma_16x16_left_edge: left_state.map_or_default(|state| state.luma_16x16_edge),
            luma_4x4_blocks: luma4_blocks,
            luma_8x8_blocks: luma8_blocks,
            intra16x16_top: top_samples.map_or([0; 16], |samples| samples.luma),
            intra16x16_left: left_samples.map_or([0; 16], |samples| samples.luma),
            intra16x16_top_available: top_prediction_available,
            intra16x16_left_available: left_prediction_available,
            intra16x16_top_left: top_left_samples.map_or(0, |samples| samples.luma[15]),
            intra16x16_top_left_available: top_left_prediction_available,
            chroma_left_mode_nonzero: left_state
                .filter(|_| left_prediction_available)
                .is_some_and(|state| state.chroma_mode_nonzero),
            chroma_top_mode_nonzero: top_state
                .filter(|_| top_prediction_available)
                .is_some_and(|state| state.chroma_mode_nonzero),
            chroma_qp_index_offsets: [
                i32::try_from(pps.chroma_qp_index_offset)
                    .map_err(|_| invalid("PPS chroma QP offset is outside the supported range"))?,
                i32::try_from(pps.second_chroma_qp_index_offset)
                    .map_err(|_| invalid("PPS chroma QP offset is outside the supported range"))?,
            ],
            ..crate::CabacIIntraMacroblockInput::default()
        };
        for component in 0..2 {
            let top_available = top_prediction_available;
            let left_available = left_prediction_available;
            let top_left_available = top_left_prediction_available;
            let top_values = top_samples.map(|samples| {
                if component == 0 {
                    samples.cb
                } else {
                    samples.cr
                }
            });
            let left_values = left_samples.map(|samples| {
                if component == 0 {
                    samples.cb
                } else {
                    samples.cr
                }
            });
            input.chroma_references[component] = crate::CabacChroma420References {
                top_available,
                left_available,
                top_left_available,
                top: top_values.unwrap_or([0; 8]),
                left: left_values.unwrap_or([0; 8]),
                top_left: if component == 0 {
                    top_left_samples.map_or(0, |samples| samples.cb[7])
                } else {
                    top_left_samples.map_or(0, |samples| samples.cr[7])
                },
            };
            input.chroma_left_edges[component] =
                left_state.map_or_default(|state| state.chroma_edges[component]);
            input.chroma_top_edges[component] =
                top_state.map_or_default(|state| state.chroma_edges[component]);
        }
        Ok(input)
    }

    pub fn inter_macroblock_input_from_history(
        &mut self,
        dependency_slice: &DependencySlice,
        macroblock_address: usize,
        history: &[crate::CabacDecodedSliceMacroblock],
    ) -> io::Result<crate::CabacPBMacroblockLayerInput> {
        let header = &dependency_slice.header;
        if !matches!(header.slice_type % 5, 0 | 1) {
            return Err(invalid(
                "inter history input builder requires a P- or B-slice",
            ));
        }
        let pps = self
            .picture_parameter_sets
            .iter()
            .find(|pps| pps.picture_parameter_set_id == header.picture_parameter_set_id)
            .copied()
            .ok_or_else(|| invalid("dependency slice references an unavailable PPS"))?;
        let sps = self
            .sequence_parameter_sets
            .iter()
            .find(|sps| sps.sps_id == pps.sequence_parameter_set_id)
            .cloned()
            .ok_or_else(|| invalid("dependency slice references an unavailable SPS"))?;
        if pps.weighted_pred || pps.weighted_bipred_idc != 0 {
            return Err(invalid("weighted inter prediction is unsupported"));
        }

        let max_frame_num = 1_u32
            .checked_shl(sps.log2_max_frame_num_minus4 + 4)
            .ok_or_else(|| invalid("MaxFrameNum is outside the supported range"))?;
        let current_poc = self.picture_order_count(header, &sps)?;

        let references = self.reference_picture_buffer.references();
        let (mut list0, mut list1) = if header.slice_type % 5 == 0 {
            (
                crate::build_p_reference_list(&references, header.frame_num, max_frame_num)?,
                Vec::new(),
            )
        } else {
            crate::build_b_reference_lists(&references, current_poc)
        };
        list0 = crate::apply_reference_list_modifications(
            &list0,
            &references,
            header.frame_num,
            max_frame_num,
            &header.ref_pic_list_reordering_l0,
        )?;
        if header.slice_type % 5 == 1 {
            list1 = crate::apply_reference_list_modifications(
                &list1,
                &references,
                header.frame_num,
                max_frame_num,
                &header.ref_pic_list_reordering_l1,
            )?;
        }
        let active_l0 = if header.num_ref_idx_active_override {
            header.num_ref_idx_l0_active_minus1
        } else {
            pps.num_ref_idx_l0_default_active_minus1
        };
        let active_l1 = if header.num_ref_idx_active_override {
            header.num_ref_idx_l1_active_minus1
        } else {
            pps.num_ref_idx_l1_default_active_minus1
        };
        list0.truncate(active_l0 as usize + 1);
        if header.slice_type % 5 == 1 {
            list1.truncate(active_l1 as usize + 1);
        } else {
            list1.clear();
        }
        let decode_references = |pictures: &[crate::ReferencePicture]| {
            pictures
                .iter()
                .map(|reference| {
                    let decoded = self
                        .reference_picture_buffer
                        .get(reference.identifier)
                        .ok_or_else(|| {
                            invalid(format!(
                                "reference picture {} is not decoded",
                                reference.identifier
                            ))
                        })?;
                    Ok(crate::CabacBReferencePicture {
                        frame: decoded.frame,
                        picture_order_count: reference.picture_order_cnt,
                        long_term: reference.long_term_frame_idx.is_some(),
                    })
                })
                .collect::<io::Result<Vec<_>>>()
        };
        let references0 = decode_references(&list0)?;
        let references1 = decode_references(&list1)?;
        if references0.is_empty() || (header.slice_type % 5 == 1 && references1.is_empty()) {
            return Err(invalid("P/B slice reference list is empty"));
        }

        let width_in_mbs = usize::try_from(sps.coded_width / 16)
            .map_err(|_| invalid("picture width exceeds the host address space"))?;
        let height_in_mbs = usize::try_from(sps.coded_height / 16)
            .map_err(|_| invalid("picture height exceeds the host address space"))?;
        let picture_macroblock_count = width_in_mbs
            .checked_mul(height_in_mbs)
            .ok_or_else(|| invalid("picture macroblock count overflows"))?;
        if macroblock_address >= picture_macroblock_count {
            return Err(invalid("macroblock address is outside the picture"));
        }

        let macroblock_neighbors =
            crate::derive_cabac_inter_macroblock_neighbor_state_from_history(
                macroblock_address,
                width_in_mbs,
                header.first_macroblock_in_slice as usize,
                history,
                header.slice_type,
                0,
                0,
                4,
                4,
            )?;
        let mut partition_neighbors = Vec::with_capacity(16);
        for cell in 0..16 {
            partition_neighbors.push(
                crate::derive_cabac_inter_macroblock_neighbor_state_from_history(
                    macroblock_address,
                    width_in_mbs,
                    header.first_macroblock_in_slice as usize,
                    history,
                    header.slice_type,
                    (cell % 4) * 4,
                    (cell / 4) * 4,
                    4,
                    4,
                )?
                .partition_neighbors,
            );
        }
        let neighbor_indices = crate::derive_cabac_slice_neighbor_indices(
            macroblock_address,
            width_in_mbs,
            header.first_macroblock_in_slice as usize,
            history.len(),
        )?;
        let neighbor_state = |index: Option<usize>, direction| {
            index.map(|index| {
                crate::derive_cabac_macroblock_neighbor_coded_state(&history[index], direction)
            })
        };
        let left_state = neighbor_state(
            neighbor_indices.left,
            crate::CabacMacroblockNeighborDirection::Left,
        );
        let top_state = neighbor_state(
            neighbor_indices.top,
            crate::CabacMacroblockNeighborDirection::Top,
        );
        let slice_qpy = i32::try_from(26_i64 + pps.pic_init_qp_minus26 + header.slice_qp_delta)
            .map_err(|_| invalid("dependency slice QP is outside the supported range"))?;
        let mut previous_qpy = slice_qpy;
        let mut previous_qp_delta = 0;
        for entry in history.iter().rev() {
            let qpy = match &entry.result {
                crate::CabacSliceMacroblockResult::Intra(decoded) => Some(decoded.qpy),
                crate::CabacSliceMacroblockResult::Inter(layer) => match &layer.macroblock {
                    crate::CabacPBMacroblockPrediction::Inter(decoded) => Some(decoded.qpy),
                    crate::CabacPBMacroblockPrediction::Intra(decoded) => Some(decoded.qpy),
                    crate::CabacPBMacroblockPrediction::PSkip(_)
                    | crate::CabacPBMacroblockPrediction::BSkip(_) => None,
                },
            };
            if let Some(qpy) = qpy {
                previous_qpy = qpy;
                break;
            }
        }
        if let Some(entry) = history.last() {
            previous_qp_delta = match &entry.result {
                crate::CabacSliceMacroblockResult::Intra(decoded) => decoded.qp_delta,
                crate::CabacSliceMacroblockResult::Inter(layer) if !layer.skipped => {
                    match &layer.macroblock {
                        crate::CabacPBMacroblockPrediction::Inter(decoded) => decoded.qp_delta,
                        crate::CabacPBMacroblockPrediction::Intra(decoded) => decoded.qp_delta,
                        crate::CabacPBMacroblockPrediction::PSkip(_)
                        | crate::CabacPBMacroblockPrediction::BSkip(_) => 0,
                    }
                }
                crate::CabacSliceMacroblockResult::Inter(_) => 0,
            }
        }

        let macroblock_x = macroblock_address % width_in_mbs;
        let macroblock_y = macroblock_address / width_in_mbs;
        let left_skipped = macroblock_neighbors.left_skipped;
        let top_skipped = macroblock_neighbors.top_skipped;
        let inter_input = crate::CabacInterMacroblockInput {
            slice_type: header.slice_type,
            macroblock_x,
            macroblock_y,
            left_available: macroblock_neighbors.left_available,
            top_available: macroblock_neighbors.top_available,
            left_b_skip_or_direct: macroblock_neighbors.left_b_skip_or_direct,
            top_b_skip_or_direct: macroblock_neighbors.top_b_skip_or_direct,
            max_reference_index_minus1: [active_l0, active_l1],
            reference_lists: [references0.clone(), references1.clone()],
            partition_neighbors,
        };
        let residual_input = crate::CabacInterMacroblockResidualInput {
            previous_qpy,
            previous_qp_delta,
            left_luma_cbp: left_state
                .filter(|state| !state.is_ipcm)
                .map_or(15, |state| state.coded_block_pattern_luma),
            top_luma_cbp: top_state
                .filter(|state| !state.is_ipcm)
                .map_or(15, |state| state.coded_block_pattern_luma),
            left_chroma_cbp: left_state.map_or(0, |state| {
                if state.is_ipcm {
                    2
                } else {
                    state.coded_block_pattern_chroma
                }
            }),
            top_chroma_cbp: top_state.map_or(0, |state| {
                if state.is_ipcm {
                    2
                } else {
                    state.coded_block_pattern_chroma
                }
            }),
            transform_8x8_mode_enabled: pps.transform_8x8_mode,
            direct_8x8_inference_flag: sps.direct_8x8_inference,
            left_has_8x8_transform: left_state.is_some_and(|state| state.transform_size_8x8),
            top_has_8x8_transform: top_state.is_some_and(|state| state.transform_size_8x8),
            luma_4x4_left_edge: left_state.map_or_default(|state| state.luma_4x4_edge),
            luma_4x4_top_edge: top_state.map_or_default(|state| state.luma_4x4_edge),
            chroma_left_edges: left_state
                .map_or([Default::default(); 2], |state| state.chroma_edges),
            chroma_top_edges: top_state.map_or([Default::default(); 2], |state| state.chroma_edges),
            chroma_qp_index_offsets: [
                i32::try_from(pps.chroma_qp_index_offset)
                    .map_err(|_| invalid("PPS chroma QP offset is outside the supported range"))?,
                i32::try_from(pps.second_chroma_qp_index_offset)
                    .map_err(|_| invalid("PPS chroma QP offset is outside the supported range"))?,
            ],
            ..crate::CabacInterMacroblockResidualInput::default()
        };
        let b_skip_input = if header.slice_type % 5 == 1 {
            if !header.direct_spatial_mv_pred {
                return Err(invalid(
                    "temporal B direct requires co-located motion metadata in the reference buffer",
                ));
            }
            let direct_neighbors =
                crate::derive_cabac_inter_macroblock_neighbor_state_from_history(
                    macroblock_address,
                    width_in_mbs,
                    header.first_macroblock_in_slice as usize,
                    history,
                    header.slice_type,
                    0,
                    0,
                    16,
                    16,
                )?;
            let list0 = direct_neighbors.partition_neighbors.list0;
            let list1_neighbors = direct_neighbors.partition_neighbors.list1;
            let colocated_picture = list1
                .first()
                .ok_or_else(|| invalid("B direct co-located picture is unavailable"))?;
            let colocated_facts = self
                .reference_picture_buffer
                .colocated_motion_facts(colocated_picture.identifier, macroblock_address)
                .ok_or_else(|| {
                    invalid(format!(
                        "co-located motion facts are unavailable for reference picture {} macroblock {}",
                        colocated_picture.identifier, macroblock_address
                    ))
                })?;
            let colocated_top_left = colocated_facts[0];
            Some(crate::CabacBDirectMacroblockInput {
                slice_type: header.slice_type,
                macroblock_x,
                macroblock_y,
                direct_spatial: true,
                left_available: direct_neighbors.left_available,
                left_skipped: direct_neighbors.left_skipped,
                top_available: direct_neighbors.top_available,
                top_skipped: direct_neighbors.top_skipped,
                list0_references: references0.clone(),
                list1_references: references1.clone(),
                left_l0_candidate: list0.left_candidate,
                top_l0_candidate: list0.top_candidate,
                top_right_l0_candidate: list0.top_right_candidate,
                top_left_l0_candidate: list0.top_left_candidate,
                left_l1_candidate: list1_neighbors.left_candidate,
                top_l1_candidate: list1_neighbors.top_candidate,
                top_right_l1_candidate: list1_neighbors.top_right_candidate,
                top_left_l1_candidate: list1_neighbors.top_left_candidate,
                colocated_reference_index: colocated_top_left.reference_index,
                colocated_reference_picture_order_count: None,
                colocated_motion_vector: colocated_top_left.motion_vector,
                colocated_motion_field: Some(colocated_facts),
                current_picture_order_count: current_poc,
            })
        } else {
            None
        };
        Ok(crate::CabacPBMacroblockLayerInput {
            inter_input,
            residual_input,
            left_skipped,
            top_skipped,
            b_skip_input,
            intra_input: Some(self.intra_macroblock_input_from_history(
                dependency_slice,
                macroblock_address,
                history,
            )?),
        })
    }

    pub fn decode_first_sync_frame(&mut self) -> Result<Yuv420Frame, DecodeError> {
        let index = self
            .first_sync_sample_index
            .ok_or(DecodeError::FrameIndexOutOfRange)?;
        self.decode_frame(index)
    }

    pub fn first_sync_luma_plane_matches(&mut self, reference: &[u8]) -> Result<bool, DecodeError> {
        let frame = self.decode_first_sync_frame()?;
        let plane = frame
            .luma_plane_bytes()
            .map_err(|_| DecodeError::ReferenceMismatch)?;
        Ok(plane == reference)
    }

    pub fn validate_first_sync_frame_reference(
        &mut self,
        y_plane: &[u8],
        u_plane: &[u8],
        v_plane: &[u8],
    ) -> Result<(), DecodeError> {
        let index = self
            .first_sync_sample_index
            .ok_or(DecodeError::FrameIndexOutOfRange)?;
        self.validate_reference_frame(index, y_plane, u_plane, v_plane)
    }

    pub fn validate_reference_frame(
        &mut self,
        index: u64,
        y_plane: &[u8],
        u_plane: &[u8],
        v_plane: &[u8],
    ) -> Result<(), DecodeError> {
        let frame = self.decode_frame(index)?;
        let actual_y = frame
            .luma_plane_bytes()
            .map_err(|_| DecodeError::ReferenceMismatch)?;
        let actual_u = frame
            .u_plane_bytes()
            .map_err(|_| DecodeError::ReferenceMismatch)?;
        let actual_v = frame
            .v_plane_bytes()
            .map_err(|_| DecodeError::ReferenceMismatch)?;
        if actual_y != y_plane || actual_u != u_plane || actual_v != v_plane {
            return Err(DecodeError::ReferenceMismatch);
        }
        Ok(())
    }

    pub fn decode_frame(&mut self, index: u64) -> Result<Yuv420Frame, DecodeError> {
        if self.unsupported_feature {
            return Err(DecodeError::UnsupportedFeature);
        }
        let sample_count = match self.sample_count {
            Some(count) => count,
            None => return Err(DecodeError::MissingSampleReader),
        };
        let requested_index =
            usize::try_from(index).map_err(|_| DecodeError::FrameIndexOutOfRange)?;
        if requested_index >= sample_count {
            return Err(DecodeError::FrameIndexOutOfRange);
        }
        if let Some(frame) = self.decoded_frames.get(&requested_index) {
            return Ok(frame.clone());
        }
        if let Ok(identifier) = u32::try_from(requested_index) {
            if let Some(stored) = self.reference_picture_buffer.get(identifier) {
                let y = stored
                    .frame
                    .luma_plane_bytes()
                    .map_err(|_| DecodeError::ReferenceMismatch)?;
                let u = stored
                    .frame
                    .u_plane_bytes()
                    .map_err(|_| DecodeError::ReferenceMismatch)?;
                let v = stored
                    .frame
                    .v_plane_bytes()
                    .map_err(|_| DecodeError::ReferenceMismatch)?;
                let chroma_width = stored.frame.width / 2 + stored.frame.width % 2;
                let frame = Yuv420Frame {
                    width: stored.frame.width,
                    height: stored.frame.height,
                    y_stride: stored.frame.width,
                    u_stride: chroma_width,
                    v_stride: chroma_width,
                    y,
                    u,
                    v,
                };
                self.cache_decoded_frame(requested_index, frame.clone());
                return Ok(frame);
            }
        }

        let target_sample_index = self
            .sample_reader
            .as_ref()
            .ok_or(DecodeError::MissingSampleReader)?
            .presentation_sample_index(requested_index)
            .map_err(|error| DecodeError::DecodeFailure(error.to_string()))?;
        let picture_groups = self
            .dependency_slice_pictures(requested_index)
            .map_err(|error| DecodeError::DecodeFailure(error.to_string()))?;
        let gop_sync_sample_index = picture_groups
            .first()
            .and_then(|picture| picture.first())
            .map(|slice| slice.sample_index)
            .ok_or_else(|| DecodeError::DecodeFailure("dependency window is empty".into()))?;
        let target_already_decoded = self
            .last_decoded_sample_index
            .is_some_and(|last| target_sample_index as u64 <= last);
        if self.gop_sync_sample_index != Some(gop_sync_sample_index)
            || self.last_decoded_sample_index.is_none()
            || target_already_decoded
            || self
                .last_decoded_sample_index
                .is_some_and(|last| target_sample_index as u64 <= last)
        {
            self.reference_picture_buffer.clear();
            self.poc_type0_state = crate::PocType0State::default();
            self.poc_type12_state = crate::PocType12State::default();
            self.poc_cache = None;
            self.maximum_long_term_frame_idx = None;
            self.gop_sync_sample_index = Some(gop_sync_sample_index);
            self.last_decoded_sample_index = None;
        }

        let mut target_frame = None;
        for picture in picture_groups {
            let first_slice = picture
                .first()
                .ok_or_else(|| DecodeError::DecodeFailure("dependency picture is empty".into()))?;
            let sample_index = first_slice.sample_index;
            let sample_index_usize = usize::try_from(sample_index)
                .map_err(|error| DecodeError::DecodeFailure(error.to_string()))?;
            let display_index = self
                .sample_reader
                .as_ref()
                .ok_or(DecodeError::MissingSampleReader)?
                .presentation_index_for_sample_index(sample_index_usize)
                .map_err(|error| DecodeError::DecodeFailure(error.to_string()))?;
            if self
                .last_decoded_sample_index
                .is_some_and(|last| sample_index <= last)
            {
                if sample_index == target_sample_index as u64 && display_index == requested_index {
                    target_frame = self.decoded_frames.get(&display_index).cloned();
                    if target_frame.is_some() {
                        break;
                    }
                }
                continue;
            }
            let header = first_slice.header.clone();
            if self.verbose {
                eprintln!("decoding frame {requested_index} (sample {sample_index})");
            }
            let pps = self
                .picture_parameter_sets
                .iter()
                .find(|pps| pps.picture_parameter_set_id == header.picture_parameter_set_id)
                .copied()
                .ok_or_else(|| {
                    DecodeError::DecodeFailure(
                        "dependency picture references an unavailable PPS".into(),
                    )
                })?;
            let sps = self
                .sequence_parameter_sets
                .iter()
                .find(|sps| sps.sps_id == pps.sequence_parameter_set_id)
                .cloned()
                .ok_or_else(|| {
                    DecodeError::DecodeFailure(
                        "dependency picture references an unavailable SPS".into(),
                    )
                })?;
            if header.idr {
                self.reference_picture_buffer.clear();
                self.poc_type0_state = crate::PocType0State::default();
                self.poc_type12_state = crate::PocType12State::default();
                self.poc_cache = None;
                self.maximum_long_term_frame_idx = header.long_term_reference.then_some(0);
            }
            let picture_order_count = self
                .picture_order_count(&header, &sps)
                .map_err(|error| DecodeError::DecodeFailure(error.to_string()))?;
            let width_in_mbs = usize::try_from(sps.coded_width / 16)
                .map_err(|error| DecodeError::DecodeFailure(error.to_string()))?;
            let height_in_mbs = usize::try_from(sps.coded_height / 16)
                .map_err(|error| DecodeError::DecodeFailure(error.to_string()))?;
            let mut builder =
                crate::Yuv420FrameBuilder::new(width_in_mbs, height_in_mbs, 0, 0, 0, 0)
                    .map_err(|error| DecodeError::DecodeFailure(error.to_string()))?;
            let mut picture_history = Vec::new();
            let mut deblocking = vec![None; width_in_mbs * height_in_mbs];
            for (slice_id, dependency_slice) in picture.iter().enumerate() {
                let references = self.reference_picture_buffer.references();
                let (list0, list1) = slice_reference_lists(
                    &references,
                    &dependency_slice.header,
                    &pps,
                    &sps,
                    picture_order_count,
                )
                .map_err(|error| DecodeError::DecodeFailure(error.to_string()))?;
                let mut current_qpy = 26
                    + pps.pic_init_qp_minus26 as i32
                    + dependency_slice.header.slice_qp_delta as i32;
                let mut slice_history = self
                    .decode_dependency_slice_data_from_history(dependency_slice, &mut builder)
                    .map_err(|error| DecodeError::DecodeFailure(error.to_string()))?;
                for entry in &slice_history {
                    let (metadata, updated_qpy) = picture_deblocking_macroblock(
                        entry,
                        &dependency_slice.header,
                        &pps,
                        [&list0, &list1],
                        slice_id,
                        current_qpy,
                    )
                    .map_err(|error| DecodeError::DecodeFailure(error.to_string()))?;
                    deblocking[entry.address] = Some(metadata);
                    if let Some(qpy) = updated_qpy {
                        current_qpy = qpy;
                    }
                }
                picture_history.append(&mut slice_history);
            }
            let missing_addresses: Vec<_> = deblocking
                .iter()
                .enumerate()
                .filter_map(|(address, metadata)| metadata.is_none().then_some(address))
                .take(8)
                .collect();
            let last_decoded_macroblock = picture_history.last().map(|entry| {
                let kind = match &entry.result {
                    crate::CabacSliceMacroblockResult::Intra(macroblock) => {
                        format!("intra:{}", macroblock.macroblock_type)
                    }
                    crate::CabacSliceMacroblockResult::Inter(layer) => match &layer.macroblock {
                        crate::CabacPBMacroblockPrediction::Intra(macroblock) => {
                            format!("wrapped-intra:{}", macroblock.macroblock_type)
                        }
                        crate::CabacPBMacroblockPrediction::Inter(macroblock) => {
                            format!("inter:{}", macroblock.macroblock_type)
                        }
                        crate::CabacPBMacroblockPrediction::PSkip(_) => "p-skip".to_owned(),
                        crate::CabacPBMacroblockPrediction::BSkip(_) => "b-skip".to_owned(),
                    },
                };
                (entry.address, kind)
            });
            let deblocking = deblocking
                .into_iter()
                .collect::<Option<Vec<_>>>()
                .ok_or_else(|| {
                    DecodeError::DecodeFailure(format!(
                        "picture deblocking metadata is incomplete at {missing_addresses:?}; last decoded macroblock {last_decoded_macroblock:?}"
                    ))
                })?;
            let coded_frame = crate::reconstruction::deblock_yuv420_picture_threads(
                &builder
                    .finish()
                    .map_err(|error| DecodeError::DecodeFailure(error.to_string()))?,
                &deblocking,
                self.threads(),
            )
            .map_err(|error| DecodeError::DecodeFailure(error.to_string()))?;
            let width = sps.width as usize;
            let height = sps.height as usize;
            let mut planes = Vec::new();
            for (plane, divisor) in [
                (&coded_frame.y, 1),
                (&coded_frame.u, 2),
                (&coded_frame.v, 2),
            ] {
                let stride = coded_frame.width / divisor;
                let left = sps.frame_crop_left as usize / divisor;
                let top = sps.frame_crop_top as usize / divisor;
                let mut packed = Vec::with_capacity(width * height / (divisor * divisor));
                for row in 0..height / divisor {
                    let start = (top + row) * stride + left;
                    packed.extend_from_slice(&plane[start..start + width / divisor]);
                }
                planes.push(packed);
            }
            let mut planes = planes.into_iter();
            let frame = Yuv420Frame {
                width,
                height,
                y_stride: width,
                u_stride: width / 2,
                v_stride: width / 2,
                y: planes.next().unwrap(),
                u: planes.next().unwrap(),
                v: planes.next().unwrap(),
            };
            self.cache_decoded_frame(display_index, frame.clone());
            if header.nal_ref_idc != 0 {
                let (stored_frame_num, long_term_frame_idx, reset_poc_state) = if header.idr {
                    (
                        header.frame_num,
                        header.long_term_reference.then_some(0),
                        false,
                    )
                } else if header.adaptive_ref_pic_marking {
                    let max_frame_num = 1_u32
                        .checked_shl(sps.log2_max_frame_num_minus4 + 4)
                        .ok_or_else(|| {
                            DecodeError::DecodeFailure(
                                "MaxFrameNum is outside the supported range".into(),
                            )
                        })?;
                    let (long_term_frame_idx, reset_frame_num) = apply_adaptive_reference_marking(
                        &mut self.reference_picture_buffer,
                        header.frame_num,
                        max_frame_num,
                        &header.memory_management_operations,
                        &mut self.maximum_long_term_frame_idx,
                    )
                    .map_err(|error| DecodeError::DecodeFailure(error.to_string()))?;
                    (
                        if reset_frame_num { 0 } else { header.frame_num },
                        long_term_frame_idx,
                        reset_frame_num,
                    )
                } else {
                    (header.frame_num, None, false)
                };
                let stored_picture_order_count = if reset_poc_state {
                    0
                } else {
                    picture_order_count
                };
                let maximum_references = usize::try_from(sps.max_num_ref_frames.max(1))
                    .map_err(|error| DecodeError::DecodeFailure(error.to_string()))?;
                let references = self.reference_picture_buffer.references();
                if !header.idr
                    && !header.adaptive_ref_pic_marking
                    && references.len() >= maximum_references
                {
                    let max_frame_num = 1_u32
                        .checked_shl(sps.log2_max_frame_num_minus4 + 4)
                        .ok_or_else(|| {
                            DecodeError::DecodeFailure(
                                "MaxFrameNum is outside the supported range".into(),
                            )
                        })?;
                    let oldest = references
                        .iter()
                        .filter(|reference| reference.long_term_frame_idx.is_none())
                        .min_by_key(|reference| {
                            if reference.frame_num > header.frame_num {
                                i64::from(reference.frame_num) - i64::from(max_frame_num)
                            } else {
                                i64::from(reference.frame_num)
                            }
                        })
                        .ok_or_else(|| {
                            DecodeError::DecodeFailure(
                                "sliding-window marking found no short-term reference to evict"
                                    .into(),
                            )
                        })?;
                    self.reference_picture_buffer.remove(oldest.identifier);
                }
                let identifier = u32::try_from(display_index)
                    .map_err(|error| DecodeError::DecodeFailure(error.to_string()))?;
                let reference = crate::ReferencePicture {
                    identifier,
                    frame_num: stored_frame_num,
                    picture_order_cnt: stored_picture_order_count,
                    long_term_frame_idx,
                };
                self.store_reference_picture_with_macroblocks(
                    reference,
                    &coded_frame,
                    &picture_history,
                )
                .map_err(|error| DecodeError::DecodeFailure(error.to_string()))?;
                if reset_poc_state {
                    if sps.pic_order_cnt_type == 0 {
                        let top_order_count = self
                            .poc_type0_state
                            .previous_pic_order_cnt_msb
                            .checked_add(i64::from(header.pic_order_cnt_lsb))
                            .ok_or_else(|| {
                                DecodeError::DecodeFailure(
                                    "MMCO 5 picture order count overflows".into(),
                                )
                            })?;
                        let bottom_order_count = top_order_count
                            .checked_add(header.delta_pic_order_bottom)
                            .ok_or_else(|| {
                                DecodeError::DecodeFailure(
                                    "MMCO 5 picture order count overflows".into(),
                                )
                            })?;
                        let normalized_top_order_count = top_order_count
                            .checked_sub(top_order_count.min(bottom_order_count))
                            .and_then(|value| u32::try_from(value).ok())
                            .ok_or_else(|| {
                                DecodeError::DecodeFailure(
                                    "MMCO 5 normalized picture order count is invalid".into(),
                                )
                            })?;
                        self.poc_type0_state = crate::PocType0State {
                            previous_pic_order_cnt_msb: 0,
                            previous_pic_order_cnt_lsb: normalized_top_order_count,
                        };
                    } else {
                        self.poc_type0_state = crate::PocType0State::default();
                    }
                    self.poc_type12_state = crate::PocType12State::default();
                    self.poc_cache = None;
                }
            }
            self.last_decoded_sample_index = Some(sample_index);
            if let Some(path) = self.checkpoint_path.clone() {
                let _ = self.save_checkpoint(path);
            }
            if sample_index == target_sample_index as u64 && display_index == requested_index {
                target_frame = Some(frame);
                break;
            }
        }
        target_frame.ok_or_else(|| {
            DecodeError::DecodeFailure(format!(
                "requested presentation frame {requested_index} is absent from its dependency window"
            ))
        })
    }
}

#[cfg(test)]
mod adaptive_reference_marking_tests {
    use super::apply_adaptive_reference_marking;
    use crate::slice::MemoryManagementOperation;
    use crate::{H264Decoder, ReferencePicture, ReferencePictureBuffer, Yuv420Frame};

    fn frame() -> Yuv420Frame {
        Yuv420Frame {
            width: 1,
            height: 1,
            y_stride: 1,
            u_stride: 1,
            v_stride: 1,
            y: vec![0],
            u: vec![0],
            v: vec![0],
        }
    }

    fn operation(operation: u32, operands: &[u32]) -> MemoryManagementOperation {
        MemoryManagementOperation {
            operation,
            operands: operands.to_vec(),
        }
    }

    #[test]
    fn decoded_frame_cache_defaults_to_eight_and_evicts_oldest() {
        let mut decoder = H264Decoder::default();
        assert_eq!(decoder.frame_cache_size(), 8);
        decoder.set_frame_cache_size(2);
        for index in 0..4 {
            let mut decoded = frame();
            decoded.y[0] = index as u8;
            decoder.cache_decoded_frame(index, decoded);
        }
        assert_eq!(decoder.frame_cache_len(), 2);
        assert!(!decoder.decoded_frames.contains_key(&1));
        assert!(decoder.decoded_frames.contains_key(&2));
        assert!(decoder.decoded_frames.contains_key(&3));
        decoder.set_frame_cache_size(0);
        assert_eq!(decoder.frame_cache_len(), 0);
    }

    #[test]
    fn adaptive_marking_applies_all_frame_operations_in_order() {
        let mut buffer = ReferencePictureBuffer::default();
        for (identifier, frame_num, long_term_frame_idx) in [
            (14, 14, None),
            (15, 15, None),
            (50, 2, Some(1)),
            (51, 3, Some(3)),
            (52, 4, Some(2)),
        ] {
            buffer
                .store(
                    ReferencePicture {
                        identifier,
                        frame_num,
                        picture_order_cnt: i64::from(frame_num),
                        long_term_frame_idx,
                    },
                    &frame(),
                )
                .unwrap();
        }
        let operations = [
            operation(1, &[2]),
            operation(4, &[3]),
            operation(3, &[1, 1]),
            operation(2, &[1]),
            operation(6, &[2]),
        ];
        let mut maximum_long_term_frame_idx = None;
        let result = apply_adaptive_reference_marking(
            &mut buffer,
            1,
            16,
            &operations,
            &mut maximum_long_term_frame_idx,
        )
        .unwrap();

        assert_eq!(result, (Some(2), false));
        assert_eq!(maximum_long_term_frame_idx, Some(2));
        assert!(buffer.references().is_empty());
    }

    #[test]
    fn adaptive_marking_mmco5_clears_references_and_resets_current_frame_number() {
        let mut buffer = ReferencePictureBuffer::default();
        buffer
            .store(
                ReferencePicture {
                    identifier: 1,
                    frame_num: 1,
                    picture_order_cnt: 2,
                    long_term_frame_idx: None,
                },
                &frame(),
            )
            .unwrap();
        let mut maximum_long_term_frame_idx = Some(3);
        let result = apply_adaptive_reference_marking(
            &mut buffer,
            2,
            16,
            &[operation(5, &[])],
            &mut maximum_long_term_frame_idx,
        )
        .unwrap();

        assert_eq!(result, (None, true));
        assert_eq!(maximum_long_term_frame_idx, None);
        assert!(buffer.references().is_empty());
    }
}
