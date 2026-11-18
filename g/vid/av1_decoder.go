package vid

var ErrAV1NotImplemented = &CodecError{Code: CodecNotImplemented, Message: "AV1 decoding is not implemented"}

type AV1Decoder struct{}

func NewAV1Decoder(_ *VideoSampleReader) *AV1Decoder { return &AV1Decoder{} }

func (decoder *AV1Decoder) CheckAvailable() error { return ErrAV1NotImplemented }

func (decoder *AV1Decoder) DecodeFrame(_ uint64) (Yuv420Frame, error) {
	return Yuv420Frame{}, ErrAV1NotImplemented
}

func (decoder *AV1Decoder) SetThreads(_ int)  {}
func (decoder *AV1Decoder) SetVerbose(_ bool) {}
