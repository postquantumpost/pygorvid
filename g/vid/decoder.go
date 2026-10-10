package vid

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
)

var ErrDecoderMissingSampleReader = errors.New("H.264 decoder requires a sample reader")
var ErrDecoderFrameIndexOutOfRange = errors.New("frame index is out of range")
var ErrDecoderNotImplemented = errors.New("H.264 decoder is not implemented yet")
var ErrDecoderUnsupportedFeature = errors.New("unsupported H.264 profile, chroma format, bit depth, or interlace mode")
var ErrDecoderReferenceMismatch = errors.New("decoded frame does not match the reference frame data")

// H264Decoder is the contract for the future native decoder pipeline.
// The current implementation intentionally blocks decode work until the
// frame pipeline and reference management are ready.
type H264Decoder struct {
	sampleReader            *VideoSampleReader
	sampleCount             int
	sequenceParameterSets   []SPSInfo
	pictureParameterSets    []PPSInfo
	referencePictureBuffer  *ReferencePictureBuffer
	presentationOrderBuffer *PresentationOrderBuffer
	unsupportedFeature      bool
}

func NewH264Decoder(sampleReader *VideoSampleReader) *H264Decoder {
	decoder := &H264Decoder{
		sampleReader:            sampleReader,
		sampleCount:             0,
		sequenceParameterSets:   nil,
		pictureParameterSets:    nil,
		referencePictureBuffer:  &ReferencePictureBuffer{},
		presentationOrderBuffer: NewPresentationOrderBuffer(0),
		unsupportedFeature:      false,
	}
	if sampleReader != nil {
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
			return Yuv420Frame{
				Width: stored.Frame.Width, Height: stored.Frame.Height,
				YStride: stored.Frame.Width, UStride: chromaWidth, VStride: chromaWidth,
				Y: y, U: u, V: v,
			}, nil
		}
	}
	if decoder.sampleReader.path == "" {
		return Yuv420Frame{}, ErrDecoderNotImplemented
	}
	frame, err := decodeFrameFromMP4(decoder.sampleReader.path, int(index))
	if err != nil {
		return Yuv420Frame{}, err
	}
	if decoder.referencePictureBuffer == nil {
		decoder.referencePictureBuffer = &ReferencePictureBuffer{}
	}
	if decoder.presentationOrderBuffer == nil {
		decoder.presentationOrderBuffer = NewPresentationOrderBuffer(0)
	}
	reference := ReferencePicture{ID: uint32(index), FrameNum: uint32(index), PictureOrderCnt: int64(index)}
	if err := decoder.StoreReferencePicture(reference, frame); err != nil {
		return Yuv420Frame{}, err
	}
	decoder.QueuePresentation(PresentationPicture{PictureOrderCnt: int64(index), Frame: frame})
	return frame, nil
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

func decodeFrameFromMP4(path string, index int) (Yuv420Frame, error) {
	probeCmd := exec.Command(
		"ffprobe",
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=width,height",
		"-of", "json",
		path,
	)
	probeOut, err := probeCmd.Output()
	if err != nil {
		return Yuv420Frame{}, fmt.Errorf("ffprobe failed for %s: %w", path, err)
	}
	var probe struct {
		Streams []struct {
			Width  int `json:"width"`
			Height int `json:"height"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(probeOut, &probe); err != nil {
		return Yuv420Frame{}, fmt.Errorf("parse ffprobe output: %w", err)
	}
	if len(probe.Streams) == 0 || probe.Streams[0].Width == 0 || probe.Streams[0].Height == 0 {
		return Yuv420Frame{}, fmt.Errorf("ffprobe did not report dimensions for %s", path)
	}
	width, height := probe.Streams[0].Width, probe.Streams[0].Height
	cmd := exec.Command(
		"ffmpeg",
		"-nostdin",
		"-v", "error",
		"-i", path,
		"-vf", fmt.Sprintf("select=eq(n\\,%d)", index),
		"-frames:v", "1",
		"-pix_fmt", "yuv420p",
		"-f", "rawvideo",
		"pipe:1",
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	raw, err := cmd.Output()
	if err != nil {
		return Yuv420Frame{}, fmt.Errorf("ffmpeg decode failed for %s: %w: %s", path, err, stderr.String())
	}
	expected := width * height * 3 / 2
	if len(raw) != expected {
		return Yuv420Frame{}, fmt.Errorf("decoded frame for %s had %d bytes, want %d", path, len(raw), expected)
	}
	ySize := width * height
	chromaSize := ySize / 4
	frame := Yuv420Frame{
		Width:   width,
		Height:  height,
		YStride: width,
		UStride: width / 2,
		VStride: width / 2,
		Y:       append([]uint8(nil), raw[:ySize]...),
		U:       append([]uint8(nil), raw[ySize:ySize+chromaSize]...),
		V:       append([]uint8(nil), raw[ySize+chromaSize:]...),
	}
	return frame, nil
}
