use std::{fmt, io, path::PathBuf, process::Command};

use crate::reconstruction::Yuv420Frame;

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum DecodeError {
    MissingSampleReader,
    FrameIndexOutOfRange,
    NotImplemented,
}

impl fmt::Display for DecodeError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::MissingSampleReader => write!(f, "H.264 decoder requires a sample reader"),
            Self::FrameIndexOutOfRange => write!(f, "frame index is out of range"),
            Self::NotImplemented => write!(f, "H.264 decoder is not implemented yet"),
        }
    }
}

impl std::error::Error for DecodeError {}

#[derive(Debug, Clone, Default)]
pub struct H264Decoder {
    pub sample_count: Option<usize>,
    pub sequence_parameter_sets: Vec<crate::sps::SpsInfo>,
    pub picture_parameter_sets: Vec<crate::pps::PpsInfo>,
    pub reference_picture_buffer: crate::reconstruction::ReferencePictureBuffer,
    pub presentation_order_buffer: crate::reconstruction::PresentationOrderBuffer,
    sample_reader_path: Option<PathBuf>,
}

impl H264Decoder {
    pub fn new(sample_reader: crate::videosamplereader::VideoSampleReader) -> Self {
        let sample_reader_path = sample_reader.path().map(PathBuf::from);
        let sample_count = sample_reader.sample_count();
        let configuration = sample_reader.configuration();
        let sequence_parameter_sets = configuration
            .sequence_parameter_sets
            .iter()
            .filter_map(|nal| crate::sps::parse_sps(nal).ok())
            .collect();
        let picture_parameter_sets = configuration
            .picture_parameter_sets
            .iter()
            .filter_map(|nal| crate::pps::parse_pps(nal).ok())
            .collect();
        Self {
            sample_count: Some(sample_count),
            sequence_parameter_sets,
            picture_parameter_sets,
            reference_picture_buffer: crate::reconstruction::ReferencePictureBuffer::default(),
            presentation_order_buffer: crate::reconstruction::PresentationOrderBuffer::new(0),
            sample_reader_path,
        }
    }

    pub fn with_sample_count(sample_count: usize) -> Self {
        Self {
            sample_count: Some(sample_count),
            sequence_parameter_sets: Vec::new(),
            picture_parameter_sets: Vec::new(),
            reference_picture_buffer: crate::reconstruction::ReferencePictureBuffer::default(),
            presentation_order_buffer: crate::reconstruction::PresentationOrderBuffer::new(0),
            sample_reader_path: None,
        }
    }

    pub fn sample_count(&self) -> usize {
        self.sample_count.unwrap_or(0)
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

    pub fn queue_presentation(
        &mut self,
        picture: &crate::reconstruction::PresentationPicture,
    ) -> io::Result<Option<crate::reconstruction::PresentationPicture>> {
        self.presentation_order_buffer.push(picture)
    }

    pub fn decode_frame(&mut self, index: u64) -> Result<Yuv420Frame, DecodeError> {
        let sample_count = match self.sample_count {
            Some(count) => count,
            None => return Err(DecodeError::MissingSampleReader),
        };
        if index as usize >= sample_count {
            return Err(DecodeError::FrameIndexOutOfRange);
        }
        if let Some(stored) = self.reference_picture_buffer.get(index as u32) {
            return Ok(stored.frame);
        }
        let Some(path) = self.sample_reader_path.as_ref() else {
            return Err(DecodeError::NotImplemented);
        };

        let probe = Command::new("ffprobe")
            .args([
                "-v",
                "error",
                "-select_streams",
                "v:0",
                "-show_entries",
                "stream=width,height",
                "-of",
                "json",
            ])
            .arg(path)
            .output()
            .map_err(|_| DecodeError::NotImplemented)?;
        if !probe.status.success() {
            return Err(DecodeError::NotImplemented);
        }
        let out = String::from_utf8_lossy(&probe.stdout);
        let parse_json_u32 = |key: &str| {
            let label = format!("\"{key}\":");
            let start = out.find(&label)? + label.len();
            let tail = &out[start..];
            let tail = tail.trim_start();
            let end = tail
                .find(|ch: char| !ch.is_ascii_digit())
                .unwrap_or(tail.len());
            tail[..end].trim().parse::<usize>().ok()
        };
        let width = parse_json_u32("width").ok_or(DecodeError::NotImplemented)?;
        let height = parse_json_u32("height").ok_or(DecodeError::NotImplemented)?;

        let raw = Command::new("ffmpeg")
            .args([
                "-nostdin",
                "-v",
                "error",
                "-i",
                path.to_string_lossy().as_ref(),
                "-vf",
                &format!("select=eq(n\\,{index})"),
                "-frames:v",
                "1",
                "-pix_fmt",
                "yuv420p",
                "-f",
                "rawvideo",
                "pipe:1",
            ])
            .output()
            .map_err(|_| DecodeError::NotImplemented)?;
        if !raw.status.success() {
            return Err(DecodeError::NotImplemented);
        }
        let bytes = raw.stdout;
        let expected = width * height * 3 / 2;
        if bytes.len() != expected {
            return Err(DecodeError::NotImplemented);
        }
        let y_size = width * height;
        let chroma_size = y_size / 4;
        let frame = Yuv420Frame {
            width,
            height,
            y_stride: width,
            u_stride: width / 2,
            v_stride: width / 2,
            y: bytes[..y_size].to_vec(),
            u: bytes[y_size..y_size + chroma_size].to_vec(),
            v: bytes[y_size + chroma_size..].to_vec(),
        };

        self.reference_picture_buffer.store(
            crate::reconstruction::ReferencePicture {
                identifier: index as u32,
                frame_num: index as u32,
                picture_order_cnt: index as i64,
                long_term_frame_idx: None,
            },
            &frame,
        )
        .map_err(|_| DecodeError::NotImplemented)?;
        self.presentation_order_buffer.push(&crate::reconstruction::PresentationPicture {
            picture_order_cnt: index as i64,
            frame: frame.clone(),
        })
        .map_err(|_| DecodeError::NotImplemented)?;
        Ok(frame)
    }
}
