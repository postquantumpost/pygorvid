//! Handle object for an mp4 file.

use std::fs::File;
use std::io;

use crate::basicinfo::{BasicAudioStreamInfo, BasicInfo, BasicVideoStreamInfo};
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
    for track in find(&boxes, &[b"moov", b"trak"]) {
        let handler = find(track.children(), &[b"mdia", b"hdlr"])
            .iter()
            .find_map(|b| b.handler_type());
        match handler {
            Some(h) if h == *b"vide" => {
                info.hasvideo = true;
                info.videostreams.push(video_stream_info(track));
            }
            Some(h) if h == *b"soun" => {
                info.hasaudio = true;
                info.audiostreams.push(audio_stream_info(track));
            }
            _ => {}
        }
    }
    Ok(info)
}

fn video_stream_info(track: &dyn crate::mp4boxes::Mp4Box) -> BasicVideoStreamInfo {
    let children = track.children();
    let mut stream = BasicVideoStreamInfo::default();
    for b in find(children, &[b"tkhd"]) {
        if let Some((width, height)) = b.dimensions() {
            stream.width = width;
            stream.height = height;
        }
    }
    let timescale = find(children, &[b"mdia", b"mdhd"])
        .iter()
        .find_map(|b| b.timescale())
        .unwrap_or(0);
    let mut samples = 0.0;
    let mut duration = 0.0;
    let mut frame_count = 0u64;
    for b in find(children, &[b"mdia", b"minf", b"stbl", b"stts"]) {
        if let Some(entries) = b.sample_timing() {
            for (sample_count, sample_delta) in entries {
                samples += f64::from(*sample_count);
                duration += f64::from(*sample_count) * f64::from(*sample_delta);
                frame_count = frame_count.saturating_add(u64::from(*sample_count));
            }
        }
    }
    stream.frame_count = frame_count;
    if timescale > 0 && duration > 0.0 {
        stream.duration_seconds = duration / f64::from(timescale);
        stream.framerate = samples / stream.duration_seconds;
    }
    stream
}

fn audio_stream_info(track: &dyn crate::mp4boxes::Mp4Box) -> BasicAudioStreamInfo {
    let mut stream = BasicAudioStreamInfo::default();
    for b in find(track.children(), &[b"mdia", b"minf", b"stbl", b"stsd"]) {
        if let Some((sample_rate, channels)) = b.audio_info() {
            stream.sample_rate = sample_rate;
            stream.channels = channels;
        }
    }
    stream
}
