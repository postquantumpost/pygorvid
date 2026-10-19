package vid

import (
	"errors"
	"fmt"
	"os"
)

var ErrDecoderMissingSampleReader = errors.New("H.264 decoder requires a sample reader")
var ErrDecoderFrameIndexOutOfRange = errors.New("frame index is out of range")
var ErrDecoderNotImplemented = errors.New("H.264 decoder is not implemented yet")
var ErrDecoderUnsupportedFeature = errors.New("unsupported H.264 profile, chroma format, bit depth, or interlace mode")
var ErrDecoderReferenceMismatch = errors.New("decoded frame does not match the reference frame data")

const defaultFrameCacheSize = 8

// H264Decoder owns native H.264 reconstruction, reference pictures, and presentation frames.
type H264Decoder struct {
	sampleReader            *VideoSampleReader
	sampleCount             int
	sequenceParameterSets   []SPSInfo
	pictureParameterSets    []PPSInfo
	referencePictureBuffer  *ReferencePictureBuffer
	presentationOrderBuffer *PresentationOrderBuffer
	unsupportedFeature      bool
	nativeBlocks            []nativeMacroblock
	nativeReferenceBlocks   map[uint32][]nativeMacroblock
	decodedFrames           map[uint64]Yuv420Frame
	decodedFrameOrder       []uint64
	frameCacheSize          int
	nativeSyncSample        int64
	nativeLastSample        int64
	nativeMaximumLongTerm   int64
	nativePOC0              POCType0State
	nativePOC12             POCType12State
	threads                 int
	verbose                 bool
}

// SetThreads sets the number of worker threads; values below one select DefaultDecoderThreads.
func (decoder *H264Decoder) SetThreads(threads int) {
	if threads < 1 {
		threads = DefaultDecoderThreads()
	}
	decoder.threads = threads
}

func (decoder *H264Decoder) Threads() int {
	return decoder.threads
}

func (decoder *H264Decoder) SetVerbose(verbose bool) {
	decoder.verbose = verbose
}

// SetFrameCacheSize sets the number of most recently decoded output frames to retain.
// A size of zero disables the decoded-frame cache.
func (decoder *H264Decoder) SetFrameCacheSize(size int) error {
	if size < 0 {
		return errors.New("frame cache size must be non-negative")
	}
	decoder.frameCacheSize = size
	decoder.trimDecodedFrameCache()
	return nil
}

// FrameCacheSize returns the decoded-frame cache capacity.
func (decoder *H264Decoder) FrameCacheSize() int {
	return decoder.frameCacheSize
}

func (decoder *H264Decoder) cacheDecodedFrame(index uint64, frame Yuv420Frame) {
	if decoder.frameCacheSize == 0 {
		return
	}
	if _, exists := decoder.decodedFrames[index]; exists {
		for position, cachedIndex := range decoder.decodedFrameOrder {
			if cachedIndex == index {
				decoder.decodedFrameOrder = append(decoder.decodedFrameOrder[:position], decoder.decodedFrameOrder[position+1:]...)
				break
			}
		}
	}
	decoder.decodedFrames[index] = frame
	decoder.decodedFrameOrder = append(decoder.decodedFrameOrder, index)
	decoder.trimDecodedFrameCache()
}

func (decoder *H264Decoder) trimDecodedFrameCache() {
	for len(decoder.decodedFrameOrder) > decoder.frameCacheSize {
		oldest := decoder.decodedFrameOrder[0]
		decoder.decodedFrameOrder = decoder.decodedFrameOrder[1:]
		delete(decoder.decodedFrames, oldest)
	}
}

func NewH264Decoder(sampleReader *VideoSampleReader) *H264Decoder {
	decoder := &H264Decoder{
		threads:                 DefaultDecoderThreads(),
		sampleReader:            sampleReader,
		sampleCount:             0,
		sequenceParameterSets:   nil,
		pictureParameterSets:    nil,
		referencePictureBuffer:  &ReferencePictureBuffer{},
		presentationOrderBuffer: NewPresentationOrderBuffer(0),
		unsupportedFeature:      false,
		decodedFrames:           make(map[uint64]Yuv420Frame),
		frameCacheSize:          defaultFrameCacheSize,
		nativeReferenceBlocks:   make(map[uint32][]nativeMacroblock),
		nativeSyncSample:        -1,
		nativeLastSample:        -1,
		nativeMaximumLongTerm:   -1,
	}
	if sampleReader != nil {
		if codec := sampleReader.Codec(); codec != "" && codec != CodecH264 {
			decoder.unsupportedFeature = true
			return decoder
		}
		decoder.sampleCount = sampleReader.SampleCount()
		configuration := sampleReader.Configuration()
		decoder.sequenceParameterSets = make([]SPSInfo, 0, len(configuration.SequenceSets))
		decoder.pictureParameterSets = make([]PPSInfo, 0, len(configuration.PictureSets))
		for _, nal := range configuration.SequenceSets {
			info, err := ParseSPS(nal)
			if err != nil {
				decoder.unsupportedFeature = true
				continue
			}
			decoder.sequenceParameterSets = append(decoder.sequenceParameterSets, info)
		}
		for _, nal := range configuration.PictureSets {
			if info, err := ParsePPS(nal); err == nil {
				decoder.pictureParameterSets = append(decoder.pictureParameterSets, info)
			}
		}
	}
	return decoder
}

func (decoder *H264Decoder) StoreReferencePicture(reference ReferencePicture, frame Yuv420Frame) error {
	if decoder == nil || decoder.referencePictureBuffer == nil {
		return ErrDecoderMissingSampleReader
	}
	return decoder.referencePictureBuffer.Store(reference, frame)
}

func (decoder *H264Decoder) QueuePresentation(picture PresentationPicture) (*PresentationPicture, error) {
	if decoder == nil {
		return nil, ErrDecoderMissingSampleReader
	}
	if decoder.presentationOrderBuffer == nil {
		decoder.presentationOrderBuffer = NewPresentationOrderBuffer(0)
	}
	return decoder.presentationOrderBuffer.Push(picture)
}

func (decoder *H264Decoder) DecodeFrame(index uint64) (Yuv420Frame, error) {
	if decoder == nil || decoder.sampleReader == nil {
		return Yuv420Frame{}, ErrDecoderMissingSampleReader
	}
	if decoder.unsupportedFeature {
		return Yuv420Frame{}, ErrDecoderUnsupportedFeature
	}
	if index >= uint64(decoder.sampleCount) {
		return Yuv420Frame{}, ErrDecoderFrameIndexOutOfRange
	}
	if decoder.verbose {
		fmt.Fprintf(os.Stderr, "decoding frame %d\n", index)
	}
	if cached, ok := decoder.decodedFrames[index]; ok {
		return cloneNativeFrame(cached), nil
	}
	if decoder.referencePictureBuffer != nil {
		if stored, ok := decoder.referencePictureBuffer.Get(uint32(index)); ok {
			y, err := stored.Frame.LumaPlaneBytes()
			if err != nil {
				return Yuv420Frame{}, err
			}
			u, err := stored.Frame.UPlaneBytes()
			if err != nil {
				return Yuv420Frame{}, err
			}
			v, err := stored.Frame.VPlaneBytes()
			if err != nil {
				return Yuv420Frame{}, err
			}
			chromaWidth := stored.Frame.Width/2 + stored.Frame.Width%2
			frame := Yuv420Frame{
				Width: stored.Frame.Width, Height: stored.Frame.Height,
				YStride: stored.Frame.Width, UStride: chromaWidth, VStride: chromaWidth,
				Y: y, U: u, V: v,
			}
			decoder.cacheDecodedFrame(index, cloneNativeFrame(frame))
			return frame, nil
		}
	}
	if decoder.sampleReader.path == "" {
		return Yuv420Frame{}, ErrDecoderNotImplemented
	}
	return decoder.decodeNativeDependencies(index)
}

// DecodeFirstSyncFrame decodes the first sync sample before any non-reference or re-ordered frames.
func (decoder *H264Decoder) DecodeFirstSyncFrame() (Yuv420Frame, error) {
	if decoder == nil || decoder.sampleReader == nil {
		return Yuv420Frame{}, ErrDecoderMissingSampleReader
	}
	index, ok := decoder.sampleReader.FirstSyncSampleIndex()
	if !ok {
		return Yuv420Frame{}, ErrDecoderFrameIndexOutOfRange
	}
	return decoder.DecodeFrame(uint64(index))
}

func (decoder *H264Decoder) FirstSyncLumaPlaneMatches(reference []byte) (bool, error) {
	frame, err := decoder.DecodeFirstSyncFrame()
	if err != nil {
		return false, err
	}
	return frame.LumaPlaneMatches(reference)
}

func (decoder *H264Decoder) ValidateFirstSyncFrameReference(yPlane, uPlane, vPlane []byte) error {
	index, ok := decoder.sampleReader.FirstSyncSampleIndex()
	if !ok {
		return ErrDecoderFrameIndexOutOfRange
	}
	return decoder.ValidateReferenceFrame(uint64(index), yPlane, uPlane, vPlane)
}

func (decoder *H264Decoder) ValidateReferenceFrame(index uint64, yPlane, uPlane, vPlane []byte) error {
	if decoder == nil || decoder.sampleReader == nil {
		return ErrDecoderMissingSampleReader
	}
	if index >= uint64(decoder.sampleCount) {
		return ErrDecoderFrameIndexOutOfRange
	}
	frame, err := decoder.DecodeFrame(index)
	if err != nil {
		return err
	}
	if ok, err := frame.LumaPlaneMatches(yPlane); err != nil {
		return err
	} else if !ok {
		return ErrDecoderReferenceMismatch
	}
	actualU, err := frame.UPlaneBytes()
	if err != nil {
		return err
	}
	if !equalPlane(actualU, uPlane) {
		return ErrDecoderReferenceMismatch
	}
	actualV, err := frame.VPlaneBytes()
	if err != nil {
		return err
	}
	if !equalPlane(actualV, vPlane) {
		return ErrDecoderReferenceMismatch
	}
	return nil
}
