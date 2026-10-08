//! Handle object for a video file.

use std::fs::File;

use crate::basicinfo::BasicInfo;
use crate::mp4file::Mp4File;
use crate::probe_file_handle;

/// Starts idle (not open); `open` moves it to the open state on success.
/// An mp4 file is handled by a held `Mp4File`.
#[derive(Default)]
pub struct VidFile {
    file: Option<File>,
    mp4: Option<Mp4File>,
    error: String,
    format: &'static str,
}

impl VidFile {
    pub fn new() -> Self {
        Self::default()
    }

    /// Closes any open file, then tries to open `name`. Returns success.
    pub fn open(&mut self, name: &str) -> bool {
        self.close();
        self.error.clear();
        let mut f = match File::open(name) {
            Ok(f) => f,
            Err(e) => {
                self.error = e.to_string();
                return false;
            }
        };
        match probe_file_handle(&mut f) {
            Ok("unknown") => {
                self.error = format!("{name}: unrecognized video format");
                false
            }
            Ok("mp4") => {
                drop(f);
                let mut m = Mp4File::new();
                if !m.open(name) {
                    self.error = m.errorinfo().0;
                    return false;
                }
                self.mp4 = Some(m);
                self.format = "mp4";
                true
            }
            Ok(fmt) => {
                self.file = Some(f);
                self.format = fmt;
                true
            }
            Err(e) => {
                self.error = e.to_string();
                false
            }
        }
    }

    pub fn close(&mut self) {
        self.file = None;
        self.mp4 = None;
        self.format = "";
    }

    pub fn isopen(&self) -> bool {
        self.file.is_some() || self.mp4.as_ref().is_some_and(|m| m.isopen())
    }

    /// First element is error text; empty means no error.
    pub fn errorinfo(&self) -> (String,) {
        (self.error.clone(),)
    }

    /// Returns basic file info; on failure returns a default `BasicInfo` and sets `errorinfo`.
    pub fn getbasicinfo(&mut self) -> BasicInfo {
        if let Some(m) = self.mp4.as_mut() {
            let info = m.getbasicinfo();
            self.error = m.errorinfo().0;
            return info;
        }
        self.error = if self.file.is_none() {
            "file not open".to_string()
        } else {
            format!("basic info not supported for {}", self.format)
        };
        BasicInfo::default()
    }

    /// Container name while open, otherwise "".
    pub fn format(&self) -> &'static str {
        self.format
    }
}

/// Returns an idle `VidFile`, ready for its `open` method.
pub fn construct() -> VidFile {
    VidFile::new()
}

/// Returns a `VidFile`; open on success, otherwise idle with `errorinfo` set.
pub fn open_file(name: &str) -> VidFile {
    let mut v = construct();
    v.open(name);
    v
}
