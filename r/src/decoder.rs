use std::{fmt, io};

use crate::reconstruction::Yuv420Frame;

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum DecodeError {
    MissingSampleReader,
    FrameIndexOutOfRange,
    NotImplemented,
    UnsupportedFeature,
    ReferenceMismatch,
}

impl fmt::Display for DecodeError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
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
        }
    }
}

impl std::error::Error for DecodeError {}

#[derive(Default)]
pub struct H264Decoder {
    pub sample_count: Option<usize>,
    pub sequence_parameter_sets: Vec<crate::sps::SpsInfo>,
    pub picture_parameter_sets: Vec<crate::pps::PpsInfo>,
    pub reference_picture_buffer: crate::reconstruction::ReferencePictureBuffer,
    pub presentation_order_buffer: crate::reconstruction::PresentationOrderBuffer,
    sample_reader: Option<crate::videosamplereader::VideoSampleReader>,
    first_sync_sample_index: Option<u64>,
    unsupported_feature: bool,
}

impl H264Decoder {
    pub fn new(sample_reader: crate::videosamplereader::VideoSampleReader) -> Self {
        let first_sync_sample_index = sample_reader.first_sync_sample_index();
        let sample_count = sample_reader.sample_count();
        let configuration = sample_reader.configuration();
        let mut unsupported_feature = false;
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
        Self {
            sample_count: Some(sample_count),
            sequence_parameter_sets,
            picture_parameter_sets,
            reference_picture_buffer: crate::reconstruction::ReferencePictureBuffer::default(),
            presentation_order_buffer: crate::reconstruction::PresentationOrderBuffer::new(0),
            sample_reader: Some(sample_reader),
            first_sync_sample_index,
            unsupported_feature,
        }
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
        if index as usize >= sample_count {
            return Err(DecodeError::FrameIndexOutOfRange);
        }
        if let Some(stored) = self.reference_picture_buffer.get(index as u32) {
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
            return Ok(Yuv420Frame {
                width: stored.frame.width,
                height: stored.frame.height,
                y_stride: stored.frame.width,
                u_stride: chroma_width,
                v_stride: chroma_width,
                y,
                u,
                v,
            });
        }
        Err(DecodeError::NotImplemented)
    }
}
