//! Video container detection without ffmpeg.

mod basicinfo;
mod bitreader;
mod cabac;
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
pub use cabac::{place_luma4x4_scan_levels, CabacArithmeticDecoder, CabacContextModel};
pub use mp4boxes::AvcConfiguration;
pub use mp4file::Mp4File;
pub use nal::{ebsp_to_rbsp, parse_nal_header, NalHeader};
pub use pps::{parse_pps, PpsInfo};
pub use reconstruction::{
    inverse_scale_luma4x4, inverse_scale_luma8x8, inverse_transform_luma4x4,
    inverse_transform_luma8x8, predict_luma_intra4x4_dc, predict_luma_intra4x4_diagonal_down_left,
    predict_luma_intra4x4_diagonal_down_right, predict_luma_intra4x4_horizontal,
    predict_luma_intra4x4_horizontal_down, predict_luma_intra4x4_horizontal_up,
    predict_luma_intra4x4_vertical, predict_luma_intra4x4_vertical_left,
    predict_luma_intra4x4_vertical_right, predict_luma_intra8x8_dc,
    predict_luma_intra8x8_diagonal_down_left, predict_luma_intra8x8_diagonal_down_right,
    predict_luma_intra8x8_horizontal, predict_luma_intra8x8_horizontal_down,
    predict_luma_intra8x8_horizontal_up, predict_luma_intra8x8_vertical,
    predict_luma_intra8x8_vertical_left, predict_luma_intra8x8_vertical_right,
};
pub use slice::{
    group_slices_into_pictures, parse_slice_header, same_primary_picture, PictureIdentity,
    SliceHeader,
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
