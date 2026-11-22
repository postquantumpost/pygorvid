use std::fmt;

use crate::{
    Av1Configuration, Av1Decoder, AvcConfiguration, H264Decoder, HevcConfiguration, HevcDecoder,
    VideoSampleReader, Yuv420Frame,
};
use std::path::Path;

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum VideoCodec {
    H264,
    Hevc,
    Av1,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub enum CodecConfiguration {
    H264(AvcConfiguration),
    Hevc(HevcConfiguration),
    Av1(Av1Configuration),
}

impl CodecConfiguration {
    pub fn codec(&self) -> VideoCodec {
        match self {
            Self::H264(_) => VideoCodec::H264,
            Self::Hevc(_) => VideoCodec::Hevc,
            Self::Av1(_) => VideoCodec::Av1,
        }
    }

    pub fn nal_length_size(&self) -> u8 {
        match self {
            Self::H264(config) => config.nal_length_size,
            Self::Hevc(config) => config.nal_length_size,
            Self::Av1(_) => 0,
        }
    }
}

pub(crate) fn sample_configuration(
    entry: &dyn crate::mp4boxes::Mp4Box,
) -> std::io::Result<CodecConfiguration> {
    match &entry.header().kind {
        b"avc1" => crate::h264_mp4::sample_configuration(entry).map(CodecConfiguration::H264),
        b"hvc1" | b"hev1" => {
            crate::hevc_mp4::sample_configuration(entry).map(CodecConfiguration::Hevc)
        }
        b"av01" => crate::av1_mp4::sample_configuration(entry).map(CodecConfiguration::Av1),
        _ => Err(std::io::Error::new(
            std::io::ErrorKind::Unsupported,
            "unsupported video sample entry",
        )),
    }
}

pub fn default_decoder_threads() -> usize {
    std::thread::available_parallelism().map_or(1, std::num::NonZero::get)
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum CodecErrorCode {
    NotImplemented,
    NotImplementedAndNotLicensed,
}

impl CodecErrorCode {
    pub fn as_str(self) -> &'static str {
        match self {
            Self::NotImplemented => "NOT_IMPLEMENTED",
            Self::NotImplementedAndNotLicensed => "NOT_IMPLEMENTED_AND_NOT_LICENSED",
        }
    }
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct CodecError {
    pub code: CodecErrorCode,
    pub message: &'static str,
}

impl fmt::Display for CodecError {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str(self.message)
    }
}

impl std::error::Error for CodecError {}

pub enum VideoDecoder {
    H264(H264Decoder),
    Hevc(HevcDecoder),
    Av1(Av1Decoder),
}

impl VideoDecoder {
    pub fn open<P: AsRef<Path>>(path: P) -> Result<Self, crate::DecodeError> {
        let reader = VideoSampleReader::open(path)
            .map_err(|error| crate::DecodeError::DecodeFailure(error.to_string()))?;
        Self::new(reader)
    }

    pub fn new(reader: VideoSampleReader) -> Result<Self, crate::DecodeError> {
        match reader.codec() {
            VideoCodec::H264 => Ok(Self::H264(H264Decoder::new(reader))),
            VideoCodec::Hevc => {
                let decoder = HevcDecoder::new();
                decoder
                    .check_available()
                    .map_err(crate::DecodeError::Codec)?;
                Ok(Self::Hevc(decoder))
            }
            VideoCodec::Av1 => {
                let decoder = Av1Decoder::new();
                decoder
                    .check_available()
                    .map_err(crate::DecodeError::Codec)?;
                Ok(Self::Av1(decoder))
            }
        }
    }

    pub fn new_with_environment_checkpoint(
        reader: VideoSampleReader,
    ) -> Result<Self, crate::DecodeError> {
        if reader.codec() == VideoCodec::H264 {
            H264Decoder::new_with_environment_checkpoint(reader)
                .map(Self::H264)
                .map_err(|error| crate::DecodeError::DecodeFailure(error.to_string()))
        } else {
            Self::new(reader)
        }
    }

    pub fn set_threads(&mut self, threads: usize) {
        if let Self::H264(decoder) = self {
            decoder.set_threads(threads);
        }
    }

    pub fn set_verbose(&mut self, verbose: bool) {
        if let Self::H264(decoder) = self {
            decoder.set_verbose(verbose);
        }
    }

    pub fn decode_frame(&mut self, index: u64) -> Result<Yuv420Frame, crate::DecodeError> {
        match self {
            Self::H264(decoder) => decoder.decode_frame(index),
            Self::Hevc(decoder) => decoder
                .decode_frame(index)
                .map_err(crate::DecodeError::Codec),
            Self::Av1(decoder) => decoder
                .decode_frame(index)
                .map_err(crate::DecodeError::Codec),
        }
    }

    pub fn stream_frames(
        &mut self,
        readahead: bool,
        consume: &mut dyn FnMut(crate::DecodedVideoFrame) -> Result<(), String>,
    ) -> Result<usize, String> {
        match self {
            Self::H264(decoder) => decoder.stream_frames(readahead, consume),
            Self::Hevc(_) | Self::Av1(_) => {
                Err("streaming decode is not implemented for this codec".into())
            }
        }
    }
}
