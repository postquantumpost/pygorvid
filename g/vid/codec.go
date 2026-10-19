package vid

import (
	"errors"
	"runtime"
)

type VideoCodec string

const (
	CodecH264 VideoCodec = "h264"
	CodecHEVC VideoCodec = "hevc"
)

type CodecErrorCode string

const (
	CodecNotImplemented               CodecErrorCode = "NOT_IMPLEMENTED"
	CodecNotImplementedAndNotLicensed CodecErrorCode = "NOT_IMPLEMENTED_AND_NOT_LICENSED"
)

type CodecError struct {
	Code    CodecErrorCode
	Message string
}

func (err *CodecError) Error() string { return err.Message }

func DefaultDecoderThreads() int { return max(1, runtime.NumCPU()/2) }

type CodecConfiguration struct {
	Codec VideoCodec
	AVC   *AVCConfiguration
	HEVC  *HEVCConfiguration
}

func (config CodecConfiguration) nalLengthSize() uint8 {
	if config.HEVC != nil {
		return config.HEVC.NALLengthSize
	}
	return config.AVC.NALLengthSize
}

func readCodecConfiguration(entry box, timescale uint32) (CodecConfiguration, error) {
	switch entry.header().typ {
	case "avc1":
		config, err := readAVCSampleConfiguration(entry, timescale)
		return CodecConfiguration{Codec: CodecH264, AVC: config}, err
	case "hvc1", "hev1":
		config, err := readHEVCSampleConfiguration(entry, timescale)
		return CodecConfiguration{Codec: CodecHEVC, HEVC: config}, err
	default:
		return CodecConfiguration{}, errors.New("unsupported video sample entry")
	}
}

type VideoDecoder interface {
	DecodeFrame(uint64) (Yuv420Frame, error)
	SetThreads(int)
	SetVerbose(bool)
}

func NewVideoDecoder(reader *VideoSampleReader) (VideoDecoder, error) {
	if reader == nil {
		return nil, ErrDecoderMissingSampleReader
	}
	if reader.Codec() == CodecHEVC {
		decoder := NewHEVCDecoder(reader)
		if err := decoder.CheckAvailable(); err != nil {
			return nil, err
		}
		return decoder, nil
	}
	return NewH264Decoder(reader), nil
}
