//! Video container detection without ffmpeg.

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
    let mut buf = Vec::with_capacity(HEADER_SIZE);
    File::open(path)?.take(HEADER_SIZE as u64).read_to_end(&mut buf)?;
    Ok(detect_format(&buf))
}
