package vid

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestH264DecoderTracksReaderState(t *testing.T) {
	reader := &VideoSampleReader{samples: []sampleLocation{{}, {}}}
	decoder := NewH264Decoder(reader)
	if decoder == nil {
		t.Fatal("NewH264Decoder returned nil")
	}
	if decoder.sampleReader != reader {
		t.Fatal("decoder.sampleReader is not the supplied reader")
	}
	if decoder.sampleCount != 2 {
		t.Fatalf("decoder.sampleCount = %d; want 2", decoder.sampleCount)
	}
	frame := Yuv420Frame{Width: 2, Height: 2, YStride: 2, UStride: 1, VStride: 1, Y: []uint8{1, 2, 3, 4}, U: []uint8{5}, V: []uint8{6}}
	reference := ReferencePicture{ID: 7, FrameNum: 3, PictureOrderCnt: 8}
	if err := decoder.StoreReferencePicture(reference, frame); err != nil {
		t.Fatal(err)
	}
	if stored, ok := decoder.referencePictureBuffer.Get(reference.ID); !ok || stored.Reference.ID != reference.ID || stored.Frame.Y[0] != 1 {
		t.Fatalf("stored picture = %#v, found=%v; want original reference frame", stored, ok)
	}
	decoder.presentationOrderBuffer = NewPresentationOrderBuffer(1)
	ready, err := decoder.QueuePresentation(PresentationPicture{PictureOrderCnt: 8, Frame: frame})
	if err != nil || ready != nil {
		t.Fatalf("presentation queue = %#v, %v; want next picture held until reorder limit", ready, err)
	}
}

func TestH264DecoderRequiresSampleReader(t *testing.T) {
	decoder := NewH264Decoder(nil)
	if decoder == nil {
		t.Fatal("NewH264Decoder returned nil")
	}
	if _, err := decoder.DecodeFrame(0); !errors.Is(err, ErrDecoderMissingSampleReader) {
		t.Fatalf("DecodeFrame() err = %v; want %v", err, ErrDecoderMissingSampleReader)
	}
}

func TestH264DecoderRejectsOutOfRangeFrameRequests(t *testing.T) {
	decoder := NewH264Decoder(&VideoSampleReader{})
	if _, err := decoder.DecodeFrame(0); !errors.Is(err, ErrDecoderFrameIndexOutOfRange) {
		t.Fatalf("DecodeFrame() err = %v; want %v", err, ErrDecoderFrameIndexOutOfRange)
	}
}

func TestH264DecoderContractMarksFrameDecodeAsUnimplemented(t *testing.T) {
	decoder := NewH264Decoder(&VideoSampleReader{samples: []sampleLocation{{}}})
	if _, err := decoder.DecodeFrame(0); !errors.Is(err, ErrDecoderNotImplemented) {
		t.Fatalf("DecodeFrame() err = %v; want %v", err, ErrDecoderNotImplemented)
	}
}

func TestH264DecoderUsesRealFixtureReader(t *testing.T) {
	path := filepath.Join("..", "..", "testdata", "h264", "high42-1080p.mp4")
	reader, err := OpenVideoSampleReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	decoder := NewH264Decoder(reader)
	if decoder == nil {
		t.Fatal("NewH264Decoder returned nil")
	}
	if decoder.sampleCount != reader.SampleCount() {
		t.Fatalf("decoder.sampleCount = %d; want %d", decoder.sampleCount, reader.SampleCount())
	}
	frame, err := decoder.DecodeFrame(0)
	if err != nil {
		t.Fatalf("DecodeFrame() err = %v; want nil", err)
	}
	if frame.Width != 1920 || frame.Height != 1080 {
		t.Fatalf("frame size = %dx%d; want 1920x1080", frame.Width, frame.Height)
	}
	if frame.Y[0] != 29 || frame.U[0] != 129 || frame.V[0] != 127 {
		t.Fatalf("decoded frame leading pixels = %d,%d,%d; want 29,129,127", frame.Y[0], frame.U[0], frame.V[0])
	}
}

func TestH264DecoderUsesRealFixtureReaderForPFrame(t *testing.T) {
	path := filepath.Join("..", "..", "testdata", "h264", "high42-1080p.mp4")
	reader, err := OpenVideoSampleReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	decoder := NewH264Decoder(reader)
	frame, err := decoder.DecodeFrame(3)
	if err != nil {
		t.Fatalf("DecodeFrame(3) err = %v; want nil", err)
	}
	if frame.Width != 1920 || frame.Height != 1080 {
		t.Fatalf("frame size = %dx%d; want 1920x1080", frame.Width, frame.Height)
	}
	if frame.Y[100000] != 41 || frame.U[100] != 129 || frame.V[100] != 127 {
		t.Fatalf("decoded P-frame pixels = %d,%d,%d; want 41,129,127", frame.Y[100000], frame.U[100], frame.V[100])
	}
}

func TestH264DecoderUsesRealFixtureReaderForBFrame(t *testing.T) {
	path := filepath.Join("..", "..", "testdata", "h264", "high42-1080p.mp4")
	reader, err := OpenVideoSampleReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	decoder := NewH264Decoder(reader)
	frame, err := decoder.DecodeFrame(1)
	if err != nil {
		t.Fatalf("DecodeFrame(1) err = %v; want nil", err)
	}
	if frame.Width != 1920 || frame.Height != 1080 {
		t.Fatalf("frame size = %dx%d; want 1920x1080", frame.Width, frame.Height)
	}
	if frame.Y[0] != 29 || frame.U[0] != 129 || frame.V[0] != 127 {
		t.Fatalf("decoded B-frame leading pixels = %d,%d,%d; want 29,129,127", frame.Y[0], frame.U[0], frame.V[0])
	}
	if len(decoder.referencePictureBuffer.References()) != 1 {
		t.Fatalf("decoded frame state = %d reference pictures; want 1", len(decoder.referencePictureBuffer.References()))
	}
	cached, err := decoder.DecodeFrame(1)
	if err != nil {
		t.Fatalf("DecodeFrame(1) on cached frame err = %v; want nil", err)
	}
	if len(decoder.referencePictureBuffer.References()) != 1 {
		t.Fatalf("cached decode unexpectedly duplicated references: got %d; want 1", len(decoder.referencePictureBuffer.References()))
	}
	if cached.Width != frame.Width || cached.Height != frame.Height || cached.Y[0] != frame.Y[0] {
		t.Fatalf("cached frame mismatch: %#v; want %#v", cached, frame)
	}
}
