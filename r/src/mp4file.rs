//! Handle object for an mp4 file.

use std::fs::File;
use std::io;

use crate::basicinfo::BasicInfo;
use crate::mp4boxes::{find, read_boxes};
use crate::probe_file_handle;

/// Starts idle (not open); `open` moves it to the open state on success.
#[derive(Default)]
pub struct Mp4File {
    file: Option<File>,
    error: String,
}

impl Mp4File {
    pub fn new() -> Self {
        Self::default()
    }

    /// Closes any open file, then tries to open `name` as mp4. Returns success.
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
            Ok("mp4") => {
                self.file = Some(f);
                true
            }
            Ok(_) => {
                self.error = format!("{name}: not an mp4 file");
                false
            }
            Err(e) => {
                self.error = e.to_string();
                false
            }
        }
    }

    pub fn close(&mut self) {
        self.file = None;
    }

    pub fn isopen(&self) -> bool {
        self.file.is_some()
    }

    /// First element is error text; empty means no error.
    pub fn errorinfo(&self) -> (String,) {
        (self.error.clone(),)
    }

    /// Returns basic file info; on failure returns a default `BasicInfo` and sets `errorinfo`.
    pub fn getbasicinfo(&mut self) -> BasicInfo {
        self.error.clear();
        let result = match self.file.as_ref() {
            Some(f) => read_info(f),
            None => Err(io::Error::new(io::ErrorKind::NotConnected, "file not open")),
        };
        result.unwrap_or_else(|e| {
            self.error = e.to_string();
            BasicInfo::default()
        })
    }
}

fn read_info(mut f: &File) -> io::Result<BasicInfo> {
    let size = f.metadata()?.len();
    let boxes = read_boxes(&mut f, 0, size)?;
    let mut info = BasicInfo::default();
    for b in find(&boxes, &[b"moov", b"trak", b"mdia", b"hdlr"]) {
        match b.handler_type().as_ref().map(|h| &h[..]) {
            Some(b"vide") => info.hasvideo = true,
            Some(b"soun") => info.hasaudio = true,
            _ => {}
        }
    }
    Ok(info)
}
