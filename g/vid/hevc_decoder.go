package vid

import "os"

var ErrHEVCNotImplemented = &CodecError{Code: CodecNotImplemented, Message: "HEVC decoding is not implemented"}
var ErrHEVCNotImplementedAndNotLicensed = &CodecError{Code: CodecNotImplementedAndNotLicensed, Message: "HEVC decoding is not implemented and not licensed"}

type HEVCDecoder struct {
	licensed bool
}

func NewHEVCDecoder(_ *VideoSampleReader) *HEVCDecoder {
	return &HEVCDecoder{licensed: os.Getenv("HEVC_IS_LICENSED") == "true"}
}

func (decoder *HEVCDecoder) CheckAvailable() error {
	if decoder != nil && decoder.licensed {
		return ErrHEVCNotImplemented
	}
	return ErrHEVCNotImplementedAndNotLicensed
}

func (decoder *HEVCDecoder) DecodeFrame(_ uint64) (Yuv420Frame, error) {
	return Yuv420Frame{}, decoder.CheckAvailable()
}

func (decoder *HEVCDecoder) SetThreads(_ int)  {}
func (decoder *HEVCDecoder) SetVerbose(_ bool) {}
