package vid

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
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

func TestH264DecoderReturnsTightlyPackedCachedFrame(t *testing.T) {
	decoder := NewH264Decoder(&VideoSampleReader{samples: []sampleLocation{{}}})
	frame := Yuv420Frame{
		Width: 2, Height: 2, YStride: 3, UStride: 2, VStride: 2,
		Y: []uint8{1, 2, 99, 3, 4, 99}, U: []uint8{5, 99}, V: []uint8{6, 99},
	}
	if err := decoder.StoreReferencePicture(ReferencePicture{ID: 0}, frame); err != nil {
		t.Fatal(err)
	}
	decoded, err := decoder.DecodeFrame(0)
	if err != nil {
		t.Fatalf("DecodeFrame(0) err = %v; want nil", err)
	}
	if decoded.YStride != 2 || decoded.UStride != 1 || decoded.VStride != 1 {
		t.Fatalf("decoded strides = %d/%d/%d; want 2/1/1", decoded.YStride, decoded.UStride, decoded.VStride)
	}
	if !equalPlane(decoded.Y, []byte{1, 2, 3, 4}) || !equalPlane(decoded.U, []byte{5}) || !equalPlane(decoded.V, []byte{6}) {
		t.Fatalf("decoded planes = %v/%v/%v; want tightly packed samples", decoded.Y, decoded.U, decoded.V)
	}
	decoded.Y[0] = 88
	decodedAgain, err := decoder.DecodeFrame(0)
	if err != nil || decodedAgain.Y[0] != 1 {
		t.Fatalf("second DecodeFrame() = %#v, %v; want an unchanged cached frame", decodedAgain, err)
	}
	stored, ok := decoder.referencePictureBuffer.Get(0)
	if !ok || stored.Frame.YStride != 3 || !equalPlane(stored.Frame.Y, frame.Y) {
		t.Fatalf("cached reference was mutated: %#v, found=%v", stored, ok)
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

func TestH264DecoderPFrameMatchesReferenceVector(t *testing.T) {
	manifestPath := filepath.Join("..", "..", "silkroad1-reference-vectors.json")
	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		ReferenceFrames []struct {
			Fixture         string `json:"fixture"`
			FrameReferences map[string]struct {
				Planes map[string]struct {
					SHA256 string `json:"sha256"`
				} `json:"planes"`
			} `json:"frame_references"`
		} `json:"reference_frames"`
	}
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatal(err)
	}

	var wantY, wantU, wantV string
	found := false
	for _, record := range manifest.ReferenceFrames {
		if record.Fixture == "high42-1080p.mp4" {
			wantY = record.FrameReferences["P"].Planes["y"].SHA256
			wantU = record.FrameReferences["P"].Planes["u"].SHA256
			wantV = record.FrameReferences["P"].Planes["v"].SHA256
			found = true
			break
		}
	}
	if !found {
		t.Fatal("reference vector for high42-1080p.mp4 P-frame not found")
	}

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
	luma, err := frame.LumaPlaneBytes()
	if err != nil {
		t.Fatalf("LumaPlaneBytes() err = %v; want nil", err)
	}
	uPlane, err := frame.UPlaneBytes()
	if err != nil {
		t.Fatalf("UPlaneBytes() err = %v; want nil", err)
	}
	vPlane, err := frame.VPlaneBytes()
	if err != nil {
		t.Fatalf("VPlaneBytes() err = %v; want nil", err)
	}
	yHash := sha256.Sum256(luma)
	if got := hex.EncodeToString(yHash[:]); got != wantY {
		t.Fatalf("P-frame Y sha256 = %s; want %s", got, wantY)
	}
	uHash := sha256.Sum256(uPlane)
	if got := hex.EncodeToString(uHash[:]); got != wantU {
		t.Fatalf("P-frame U sha256 = %s; want %s", got, wantU)
	}
	vHash := sha256.Sum256(vPlane)
	if got := hex.EncodeToString(vHash[:]); got != wantV {
		t.Fatalf("P-frame V sha256 = %s; want %s", got, wantV)
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

func TestH264DecoderDecodesFirstSyncSampleBeforeOtherFrames(t *testing.T) {
	path := filepath.Join("..", "..", "testdata", "h264", "high42-1080p.mp4")
	reader, err := OpenVideoSampleReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	index, ok := reader.FirstSyncSampleIndex()
	if !ok {
		t.Fatal("FirstSyncSampleIndex() reported no sync sample for fixture")
	}
	decoder := NewH264Decoder(reader)
	frame, err := decoder.DecodeFirstSyncFrame()
	if err != nil {
		t.Fatalf("DecodeFirstSyncFrame() err = %v; want nil", err)
	}
	if frame.Width != 1920 || frame.Height != 1080 {
		t.Fatalf("first sync frame size = %dx%d; want 1920x1080", frame.Width, frame.Height)
	}
	if frame.Y[0] != 29 || frame.U[0] != 129 || frame.V[0] != 127 {
		t.Fatalf("first sync frame leading pixels = %d,%d,%d; want 29,129,127", frame.Y[0], frame.U[0], frame.V[0])
	}
	expected, err := decoder.DecodeFrame(uint64(index))
	if err != nil {
		t.Fatalf("DecodeFrame(%d) err = %v; want nil", index, err)
	}
	if expected.Width != frame.Width || expected.Height != frame.Height || expected.Y[0] != frame.Y[0] || expected.U[0] != frame.U[0] || expected.V[0] != frame.V[0] {
		t.Fatalf("first sync decode mismatch: %#v; want %#v", expected, frame)
	}
}

func TestH264DecoderFirstIDRLumaPlaneMatchesReferenceVector(t *testing.T) {
	manifestPath := filepath.Join("..", "..", "silkroad1-reference-vectors.json")
	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		ReferenceFrames []struct {
			Fixture         string `json:"fixture"`
			FrameReferences map[string]struct {
				Planes map[string]struct {
					SHA256 string `json:"sha256"`
				} `json:"planes"`
			} `json:"frame_references"`
		} `json:"reference_frames"`
	}
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatal(err)
	}

	var want string
	found := false
	for _, record := range manifest.ReferenceFrames {
		if record.Fixture == "high42-1080p.mp4" {
			want = record.FrameReferences["I"].Planes["y"].SHA256
			found = true
			break
		}
	}
	if !found {
		t.Fatal("reference vector for high42-1080p.mp4 I-frame not found")
	}

	path := filepath.Join("..", "..", "testdata", "h264", "high42-1080p.mp4")
	reader, err := OpenVideoSampleReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	decoder := NewH264Decoder(reader)
	frame, err := decoder.DecodeFirstSyncFrame()
	if err != nil {
		t.Fatalf("DecodeFirstSyncFrame() err = %v; want nil", err)
	}
	luma, err := frame.LumaPlaneBytes()
	if err != nil {
		t.Fatalf("LumaPlaneBytes() err = %v; want nil", err)
	}
	sum := sha256.Sum256(luma)
	if got := hex.EncodeToString(sum[:]); got != want {
		t.Fatalf("first IDR luma sha256 = %s; want %s", got, want)
	}
}

func TestH264DecoderFirstIDRChromaPlanesMatchReferenceVector(t *testing.T) {
	manifestPath := filepath.Join("..", "..", "silkroad1-reference-vectors.json")
	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		ReferenceFrames []struct {
			Fixture         string `json:"fixture"`
			FrameReferences map[string]struct {
				Planes map[string]struct {
					SHA256 string `json:"sha256"`
				} `json:"planes"`
			} `json:"frame_references"`
		} `json:"reference_frames"`
	}
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatal(err)
	}

	var wantU, wantV string
	found := false
	for _, record := range manifest.ReferenceFrames {
		if record.Fixture == "high42-1080p.mp4" {
			wantU = record.FrameReferences["I"].Planes["u"].SHA256
			wantV = record.FrameReferences["I"].Planes["v"].SHA256
			found = true
			break
		}
	}
	if !found {
		t.Fatal("reference vector for high42-1080p.mp4 I-frame not found")
	}

	path := filepath.Join("..", "..", "testdata", "h264", "high42-1080p.mp4")
	reader, err := OpenVideoSampleReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	decoder := NewH264Decoder(reader)
	frame, err := decoder.DecodeFirstSyncFrame()
	if err != nil {
		t.Fatalf("DecodeFirstSyncFrame() err = %v; want nil", err)
	}
	uPlane, err := frame.UPlaneBytes()
	if err != nil {
		t.Fatalf("UPlaneBytes() err = %v; want nil", err)
	}
	vPlane, err := frame.VPlaneBytes()
	if err != nil {
		t.Fatalf("VPlaneBytes() err = %v; want nil", err)
	}
	uHash := sha256.Sum256(uPlane)
	if got := hex.EncodeToString(uHash[:]); got != wantU {
		t.Fatalf("first IDR U-plane sha256 = %s; want %s", got, wantU)
	}
	vHash := sha256.Sum256(vPlane)
	if got := hex.EncodeToString(vHash[:]); got != wantV {
		t.Fatalf("first IDR V-plane sha256 = %s; want %s", got, wantV)
	}
}

func TestH264DecoderRejectsFirstIDRReferenceMismatch(t *testing.T) {
	path := filepath.Join("..", "..", "testdata", "h264", "high42-1080p.mp4")
	reader, err := OpenVideoSampleReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	decoder := NewH264Decoder(reader)
	frame, err := decoder.DecodeFirstSyncFrame()
	if err != nil {
		t.Fatalf("DecodeFirstSyncFrame() err = %v; want nil", err)
	}

	luma, err := frame.LumaPlaneBytes()
	if err != nil {
		t.Fatal(err)
	}
	uPlane, err := frame.UPlaneBytes()
	if err != nil {
		t.Fatal(err)
	}
	vPlane, err := frame.VPlaneBytes()
	if err != nil {
		t.Fatal(err)
	}

	corruptY := append([]byte(nil), luma...)
	corruptY[0] ^= 0xFF
	if err := decoder.ValidateFirstSyncFrameReference(corruptY, uPlane, vPlane); !errors.Is(err, ErrDecoderReferenceMismatch) {
		t.Fatalf("ValidateFirstSyncFrameReference() err = %v; want %v", err, ErrDecoderReferenceMismatch)
	}
}

func TestH264DecoderValidatesAnyIDRReferenceFrameIndex(t *testing.T) {
	path := filepath.Join("..", "..", "testdata", "h264", "high42-1080p.mp4")
	reader, err := OpenVideoSampleReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	decoder := NewH264Decoder(reader)
	frame, err := decoder.DecodeFrame(0)
	if err != nil {
		t.Fatalf("DecodeFrame(0) err = %v; want nil", err)
	}
	luma, err := frame.LumaPlaneBytes()
	if err != nil {
		t.Fatal(err)
	}
	uPlane, err := frame.UPlaneBytes()
	if err != nil {
		t.Fatal(err)
	}
	vPlane, err := frame.VPlaneBytes()
	if err != nil {
		t.Fatal(err)
	}
	if err := decoder.ValidateReferenceFrame(0, luma, uPlane, vPlane); err != nil {
		t.Fatalf("ValidateReferenceFrame(0, ...) err = %v; want nil", err)
	}
}
