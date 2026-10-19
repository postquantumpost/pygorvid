use crate::codec::{CodecError, CodecErrorCode};

pub struct HevcDecoder {
    licensed: bool,
}

impl Default for HevcDecoder {
    fn default() -> Self {
        Self::new()
    }
}

impl HevcDecoder {
    pub fn new() -> Self {
        Self::from_license_value(std::env::var("HEVC_IS_LICENSED").ok().as_deref())
    }

    fn from_license_value(value: Option<&str>) -> Self {
        Self {
            licensed: value == Some("true"),
        }
    }

    pub fn check_available(&self) -> Result<(), CodecError> {
        Err(self.availability_error())
    }

    fn availability_error(&self) -> CodecError {
        if self.licensed {
            CodecError {
                code: CodecErrorCode::NotImplemented,
                message: "HEVC decoding is not implemented",
            }
        } else {
            CodecError {
                code: CodecErrorCode::NotImplementedAndNotLicensed,
                message: "HEVC decoding is not implemented and not licensed",
            }
        }
    }

    pub fn decode_frame(
        &mut self,
        _index: u64,
    ) -> Result<crate::codec_frame::Yuv420Frame, CodecError> {
        Err(self.availability_error())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn hevc_license_errors() {
        for value in [
            None,
            Some(""),
            Some("false"),
            Some("TRUE"),
            Some("1"),
            Some(" true"),
            Some("true "),
            Some("true"),
        ] {
            let mut decoder = HevcDecoder::from_license_value(value);
            let error = decoder.decode_frame(0).unwrap_err();
            if value == Some("true") {
                assert_eq!(error.code, CodecErrorCode::NotImplemented);
                assert_eq!(error.to_string(), "HEVC decoding is not implemented");
            } else {
                assert_eq!(error.code, CodecErrorCode::NotImplementedAndNotLicensed);
                assert_eq!(
                    error.to_string(),
                    "HEVC decoding is not implemented and not licensed"
                );
            }
        }
    }
}
