//! Video container detection without ffmpeg.

mod basicinfo;
mod bitreader;
mod cabac;
mod cabac_init;
mod decoder;
mod mp4boxes;
mod mp4file;
mod nal;
mod pps;
mod reconstruction;
mod slice;
mod sps;
mod videosamplereader;
mod vidfile;

pub use basicinfo::{BasicAudioStreamInfo, BasicInfo, BasicVideoStreamInfo};
pub use bitreader::BitReader;
pub use cabac::{
    cabac_residual_context_bases, derive_cabac_mvd_context_increment,
    derive_cabac_reference_index_context_increment, derive_coded_block_flag_cond_term,
    new_cabac_i_chroma_coded_block_pattern_contexts, new_cabac_i_intra4x4_pred_mode_contexts,
    new_cabac_i_intra_chroma_pred_mode_contexts, new_cabac_i_intra_mb_type_contexts,
    new_cabac_i_luma4x4_coded_block_flag_contexts, new_cabac_i_luma_coded_block_pattern_contexts,
    new_cabac_i_mb_qp_delta_contexts, new_cabac_i_transform_size_8x8_contexts,
    new_cabac_inter_prediction_contexts, new_cabac_p_inter_mb_type_contexts,
    new_cabac_slice_contexts, place_chroma4x4_scan_levels, place_luma4x4_scan_levels,
    place_luma8x8_scan_levels, CabacArithmeticDecoder, CabacChroma420EdgeState,
    CabacChroma420References, CabacContextModel, CabacIIntraMacroblockInput,
    CabacIIntraMacroblockResult, CabacInterNeighbor, CabacIntra16x16EdgeState,
    CabacIntra4x4EdgeState, Intra16x16LumaMacroblockResult, Intra4x4LumaMacroblockResult,
    IntraChroma420MacroblockResult, CABAC_CONTEXT_COUNT,
};
pub use decoder::{DecodeError, H264Decoder};
pub use mp4boxes::AvcConfiguration;
pub use mp4file::Mp4File;
pub use nal::{ebsp_to_rbsp, parse_nal_header, NalHeader};
pub use pps::{parse_pps, PpsInfo};
pub use reconstruction::{
    apply_luma_deblocking_edge, apply_luma_deblocking_edge_segment,
    apply_luma_deblocking_macroblock, apply_luma_deblocking_plane_edge_segment,
    apply_luma_deblocking_plane_macroblock_edge, derive_luma_boundary_strength,
    derive_luma_boundary_strength_from_predictions, derive_luma_deblocking_edge_flags,
    derive_luma_deblocking_neighbors, luma_inter_prediction_differs,
    resolve_luma_deblocking_macroblock, LumaDeblockingEdgeFlags, LumaDeblockingMacroblock,
    LumaDeblockingNeighbors, LumaIntra8x8Block, LumaPredictionVector,
};
pub use reconstruction::{
    apply_motion_vector_difference, apply_reference_list_modifications,
    assemble_chroma420_residual_macroblock, build_b_reference_lists, build_p_reference_list,
    derive_chroma_qpc, derive_luma_deblocking_parameters, derive_motion_vector,
    filter_chroma_weak_edge, filter_luma_strong_edge, filter_luma_weak_edge,
    gather_luma_quarter_sample_neighborhood, interpolate_luma_fractional_sample,
    interpolate_luma_half_sample_diagonal, interpolate_luma_half_sample_horizontal,
    interpolate_luma_half_sample_vertical, interpolate_luma_quarter_sample_around_j,
    interpolate_luma_quarter_sample_average, interpolate_luma_quarter_sample_axial,
    interpolate_luma_quarter_sample_diagonal, interpolate_luma_quarter_sample_grid,
    interpolate_luma_quarter_sample_pair, inverse_scale_chroma4x4, inverse_scale_chroma_dc2x2,
    inverse_scale_luma4x4, inverse_scale_luma8x8, inverse_transform_chroma_dc2x2,
    inverse_transform_luma4x4, inverse_transform_luma8x8, lookup_luma_deblocking_thresholds,
    lookup_luma_tc0, motion_vector_difference_neighbor_magnitudes, predict_chroma_intra8x8,
    predict_chroma_intra8x8_dc, predict_chroma_intra8x8_horizontal, predict_chroma_intra8x8_plane,
    predict_chroma_intra8x8_vertical, predict_luma_intra16x16_dc,
    predict_luma_intra16x16_horizontal, predict_luma_intra16x16_plane,
    predict_luma_intra16x16_vertical, predict_luma_intra4x4_dc,
    predict_luma_intra4x4_diagonal_down_left, predict_luma_intra4x4_diagonal_down_right,
    predict_luma_intra4x4_horizontal, predict_luma_intra4x4_horizontal_down,
    predict_luma_intra4x4_horizontal_up, predict_luma_intra4x4_vertical,
    predict_luma_intra4x4_vertical_left, predict_luma_intra4x4_vertical_right,
    predict_luma_intra8x8_dc, predict_luma_intra8x8_diagonal_down_left,
    predict_luma_intra8x8_diagonal_down_right, predict_luma_intra8x8_horizontal,
    predict_luma_intra8x8_horizontal_down, predict_luma_intra8x8_horizontal_up,
    predict_luma_intra8x8_plane, predict_luma_intra8x8_vertical,
    predict_luma_intra8x8_vertical_left, predict_luma_intra8x8_vertical_right,
    predict_motion_vector, predict_motion_vector_for_partition, reconstruct_chroma420_macroblock,
    reconstruct_chroma4x4_residual, reconstruct_intra16x16_luma_dc,
    reconstruct_luma_intra16x16_macroblock, reconstruct_luma_intra4x4_macroblock,
    reconstruct_luma_intra8x8_macroblock, select_luma_fractional_sample, should_filter_luma_edge,
    ChromaEdgeSamples, DecodedReferencePicture, LumaDeblockingMode, LumaDeblockingParameters,
    LumaDeblockingThresholds, LumaEdgeSamples, LumaIntra4x4Block, LumaStrongEdgeSamples,
    MotionVector, MotionVectorCandidate, MotionVectorPartitionShape, PocType0State, PocType12State,
    PresentationOrderBuffer, PresentationPicture, ReferencePicture, ReferencePictureBuffer,
    Yuv420Frame, Yuv420FrameBuilder,
};
pub use slice::{
    group_slices_into_pictures, parse_slice_header, same_primary_picture, PictureIdentity,
    RefPicListModification, SliceHeader,
};
pub use sps::{parse_sps, SpsInfo};
pub use videosamplereader::{CompressedSample, VideoSampleReader};
pub use vidfile::{construct, open_file, VidFile};

use std::fs::File;
use std::io::{self, Read};
use std::path::Path;

const HEADER_SIZE: usize = 380;

/// Returns a container name, or "unknown".
pub fn detect_format(h: &[u8]) -> &'static str {
    let at = |off: usize, s: &[u8]| h.len() >= off + s.len() && &h[off..off + s.len()] == s;
    if at(4, b"ftyp") {
        if at(8, b"qt  ") {
            "mov"
        } else {
            "mp4"
        }
    } else if at(0, b"\x1a\x45\xdf\xa3") {
        "matroska"
    } else if at(0, b"RIFF") && at(8, b"AVI ") {
        "avi"
    } else if at(0, b"OggS") {
        "ogg"
    } else if at(0, b"FLV") {
        "flv"
    } else if h.len() > 188 && h[0] == 0x47 && h[188] == 0x47 {
        "mpegts"
    } else {
        "unknown"
    }
}

/// Reads the header of the file at `path` and detects its container.
pub fn probe_file<P: AsRef<Path>>(path: P) -> io::Result<&'static str> {
    probe_file_handle(&mut File::open(path)?)
}

pub(crate) fn probe_file_handle(f: &mut File) -> io::Result<&'static str> {
    let mut buf = Vec::with_capacity(HEADER_SIZE);
    f.take(HEADER_SIZE as u64).read_to_end(&mut buf)?;
    Ok(detect_format(&buf))
}
