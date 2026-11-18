use crate::codec::{CodecError, CodecErrorCode};

#[derive(Default)]
pub struct Av1Decoder;

impl Av1Decoder {
    pub fn new() -> Self {
        Self
    }

    pub fn check_available(&self) -> Result<(), CodecError> {
        Err(CodecError {
            code: CodecErrorCode::NotImplemented,
            message: "AV1 decoding is not implemented",
        })
    }

    pub fn decode_frame(
        &mut self,
        _index: u64,
    ) -> Result<crate::codec_frame::Yuv420Frame, CodecError> {
        self.check_available()?;
        unreachable!()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn av1_decoder_is_explicitly_unimplemented() {
        let error = Av1Decoder::new().check_available().unwrap_err();
        assert_eq!(error.code, CodecErrorCode::NotImplemented);
        assert_eq!(error.to_string(), "AV1 decoding is not implemented");
    }
}
