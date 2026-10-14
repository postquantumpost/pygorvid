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

func TestADecoderSmoke16x16(t *testing.T) {
	assertDecoderSmokeFixture(t, "decoder-smoke-16x16.mp4", 16, [2]uint64{0xcabf755e2a6b7d56, 0x29631735ff370504})
}

func TestNativeDecoderFirstUnfilteredIDRMacroblock(t *testing.T) {
	t.Setenv("PATH", "")
	reader, err := OpenVideoSampleReader(filepath.Join("..", "..", "samples", "decoder-smoke-16x16.mp4"))
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	samples, err := reader.DecodeOrderDependencySamples(0)
	if err != nil {
		t.Fatal(err)
	}
	frame, err := NewH264Decoder(reader).decodeNativeIntraSample(samples[0])
	if err != nil {
		t.Fatal(err)
	}
	if got := h264SmokeFrameHash(frame); got != 0x653abbad7d081027 {
		t.Fatalf("native unfiltered IDR hash = %016x", got)
	}
}

func TestNativeDecoderUnfiltered1080pIDR(t *testing.T) {
	t.Setenv("PATH", "")
	reader, err := OpenVideoSampleReader(filepath.Join("..", "..", "testdata", "h264", "high42-1080p.mp4"))
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	samples, err := reader.DecodeOrderDependencySamples(0)
	if err != nil {
		t.Fatal(err)
	}
	frame, err := NewH264Decoder(reader).decodeNativeIntraSample(samples[0])
	if err != nil {
		t.Fatal(err)
	}
	want := [3]string{"c0c5e21d3b83efdd7dddabbf2d512f25db7c7d21153cf4d6260ba43853a40643", "e7c2b2fca087820ecf5798e9744b61284c395acb164f3bb03af9c8d05ab445c0", "9a7422ce1ec42db3856d7e08f7d86c4271975790a18efaccaca89876e5c39f9d"}
	for index, plane := range [][]byte{frame.Y, frame.U, frame.V} {
		hash := sha256.Sum256(plane)
		if hex.EncodeToString(hash[:]) != want[index] {
			t.Fatalf("plane %d hash = %x", index, hash)
		}
	}
}

func TestNativeDecoderFilteredIDR(t *testing.T) {
	t.Setenv("PATH", "")
	for _, test := range []struct {
		path string
		hash uint64
	}{{filepath.Join("..", "..", "samples", "decoder-smoke-16x16.mp4"), 0xcabf755e2a6b7d56}, {filepath.Join("..", "..", "testdata", "h264", "high42-1080p.mp4"), 0}} {
		reader, err := OpenVideoSampleReader(test.path)
		if err != nil {
			t.Fatal(err)
		}
		samples, err := reader.DecodeOrderDependencySamples(0)
		if err != nil {
			reader.Close()
			t.Fatal(err)
		}
		decoder := NewH264Decoder(reader)
		frame, err := decoder.decodeNativeCodedIntraSample(samples[0])
		if err == nil {
			err = deblockNativePicture(&frame, decoder.nativeBlocks)
		}
		reader.Close()
		if err != nil {
			t.Fatal(err)
		}
		frame = cropNativeFrame(frame, decoder.nativeBlocks[0].slice.sps)
		if test.hash != 0 {
			if got := h264SmokeFrameHash(frame); got != test.hash {
				t.Fatalf("native filtered IDR hash = %016x", got)
			}
		} else {
			want := [3]uint64{0x5336845088ae484d, 0x2340c7e52129a24b, 0xe6ca68150dff6267}
			for component, plane := range [][]byte{frame.Y, frame.U, frame.V} {
				hash := uint64(14695981039346656037)
				for _, value := range plane {
					hash = (hash ^ uint64(value)) * 1099511628211
				}
				if hash != want[component] {
					t.Fatalf("filtered plane %d hash = %016x", component, hash)
				}
			}
		}
	}
}

func TestNativeDecoderFiltered2160pIDR(t *testing.T) {
	t.Setenv("PATH", "")
	reader, err := OpenVideoSampleReader(filepath.Join("..", "..", "testdata", "h264", "high52-2160p.mp4"))
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	samples, err := reader.DecodeOrderDependencySamples(0)
	if err != nil {
		t.Fatal(err)
	}
	decoder := NewH264Decoder(reader)
	frame, err := decoder.decodeNativeCodedIntraSample(samples[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := deblockNativePicture(&frame, decoder.nativeBlocks); err != nil {
		t.Fatal(err)
	}
	frame = cropNativeFrame(frame, decoder.nativeBlocks[0].slice.sps)
	want := [3]uint64{0x012e6eca5b23190d, 0xeddc31b90090f325, 0xeddc31b90090f325}
	for component, plane := range [][]byte{frame.Y, frame.U, frame.V} {
		hash := uint64(14695981039346656037)
		for _, value := range plane {
			hash = (hash ^ uint64(value)) * 1099511628211
		}
		if hash != want[component] {
			t.Fatalf("filtered 2160p plane %d hash = %016x", component, hash)
		}
	}
}

func TestNativeDecoderSmokeP(t *testing.T) {
	t.Setenv("PATH", "")
	for _, fixture := range []struct {
		name string
		hash uint64
	}{{"decoder-smoke-16x16.mp4", 0x29631735ff370504}, {"decoder-smoke-64x64.mp4", 0x93ecf9bed15545a5}} {
		reader, err := OpenVideoSampleReader(filepath.Join("..", "..", "samples", fixture.name))
		if err != nil {
			t.Fatal(err)
		}
		samples, err := reader.DecodeOrderDependencySamples(1)
		if err != nil {
			reader.Close()
			t.Fatal(err)
		}
		decoder := NewH264Decoder(reader)
		frame, err := decoder.decodeNativeCodedIntraSample(samples[0])
		if err == nil {
			err = deblockNativePicture(&frame, decoder.nativeBlocks)
		}
		if err != nil {
			reader.Close()
			t.Fatal(err)
		}
		if err := decoder.StoreReferencePicture(ReferencePicture{ID: 0, FrameNum: 0, PictureOrderCnt: 0}, frame); err != nil {
			reader.Close()
			t.Fatal(err)
		}
		frame, err = decoder.decodeNativeInterSample(samples[1], 2)
		if err == nil {
			err = deblockNativePicture(&frame, decoder.nativeBlocks)
		}
		reader.Close()
		if err != nil {
			t.Fatal(err)
		}
		frame = cropNativeFrame(frame, decoder.nativeBlocks[0].slice.sps)
		if got := h264SmokeFrameHash(frame); got != fixture.hash {
			t.Fatalf("%s native P hash = %016x", fixture.name, got)
		}
	}
}

func TestNativeBPartitionLayouts(t *testing.T) {
	for _, direct8 := range []bool{false, true} {
		parts, err := nativeBPartitions(0, [4]uint8{}, direct8)
		if err != nil {
			t.Fatal(err)
		}
		want := 16
		if direct8 {
			want = 4
		}
		if len(parts) != want {
			t.Fatalf("direct partition count = %d; want %d", len(parts), want)
		}
	}
	for mbType := uint8(1); mbType <= 21; mbType++ {
		parts, err := nativeBPartitions(mbType, [4]uint8{}, true)
		if err != nil {
			t.Fatal(err)
		}
		area := 0
		for _, part := range parts {
			area += part.width * part.height
			if !part.lists[0] && !part.lists[1] {
				t.Fatal("partition has no prediction list")
			}
		}
		if area != 256 {
			t.Fatal("B partitions do not cover the macroblock")
		}
	}
}

func TestNativeDirectVectorDerivation(t *testing.T) {
	var colocated nativeMacroblock
	setNativePrediction(&colocated, nativePartition{width: 16, height: 16}, 0, nativePrediction{used: true, reference: 0, refIndex: 0, motion: MotionVector{X: 8, Y: 4}, referenceDecoded: true, motionDecoded: true})
	decoder := &H264Decoder{nativeReferenceBlocks: map[uint32][]nativeMacroblock{1: {colocated}}}
	var lists [2][]DecodedReferencePicture
	lists[0] = []DecodedReferencePicture{{Reference: ReferencePicture{ID: 0, PictureOrderCnt: 0}}}
	lists[1] = []DecodedReferencePicture{{Reference: ReferencePicture{ID: 1, PictureOrderCnt: 4}}}
	slice := nativeSlice{sps: SPSInfo{CodedWidth: 16, CodedHeight: 16, Direct8x8Inference: true}}
	predictions, err := decoder.nativeBDirect(slice, 0, 2, nativePartition{width: 8, height: 8}, lists, true)
	if err != nil {
		t.Fatal(err)
	}
	if predictions[0].motion != (MotionVector{X: 4, Y: 2}) || predictions[1].motion != (MotionVector{X: -4, Y: -2}) {
		t.Fatalf("temporal direct vectors = %v", predictions)
	}
	lists[0][0].Reference.IsLongTerm = true
	predictions, err = decoder.nativeBDirect(slice, 0, 2, nativePartition{width: 8, height: 8}, lists, true)
	if err != nil || predictions[0].motion != (MotionVector{X: 8, Y: 4}) || predictions[1].motion != (MotionVector{}) {
		t.Fatalf("long-term temporal direct = %v, %v", predictions, err)
	}
	slice.sps.CodedWidth, slice.sps.CodedHeight = 32, 32
	slice.header.DirectSpatialMVPred = true
	decoder.nativeBlocks = make([]nativeMacroblock, 4)
	setNativePrediction(&decoder.nativeBlocks[0], nativePartition{width: 16, height: 16}, 0, nativePrediction{used: true, reference: 0, refIndex: 0, motion: MotionVector{X: 4}, referenceDecoded: true, motionDecoded: true})
	setNativePrediction(&decoder.nativeBlocks[1], nativePartition{width: 16, height: 16}, 0, nativePrediction{used: true, reference: 7, refIndex: 1, motion: MotionVector{X: 24}, referenceDecoded: true, motionDecoded: true})
	lists[0] = append(lists[0], DecodedReferencePicture{Reference: ReferencePicture{ID: 7}})
	predictions, err = decoder.nativeBDirect(slice, 3, 2, nativePartition{width: 8, height: 8}, lists, true)
	if err != nil || predictions[0].refIndex != 0 || predictions[0].motion != (MotionVector{X: 4}) || predictions[1].used {
		t.Fatalf("spatial direct top-left fallback = %v, %v", predictions, err)
	}
}

func TestNativeDecoderFilteredBReference(t *testing.T) {
	t.Setenv("PATH", "")
	for _, fixture := range []struct {
		name   string
		hashes [3]uint64
	}{{"high42-1080p.mp4", [3]uint64{0xa99464d2dff09478, 0x8451b5e0f15cb528, 0x0be48dcfbba71067c}}, {"high52-2160p.mp4", [3]uint64{0xfb6aab2d0b56d8e2, 0xeddc31b90090f325, 0xeddc31b90090f325}}} {
		reader, err := OpenVideoSampleReader(filepath.Join("..", "..", "testdata", "h264", fixture.name))
		if err != nil {
			t.Fatal(err)
		}
		decoder := NewH264Decoder(reader)
		frame, err := decoder.DecodeFrame(1)
		if err != nil {
			reader.Close()
			t.Fatalf("%s: %v", fixture.name, err)
		}
		for component, plane := range [][]byte{frame.Y, frame.U, frame.V} {
			hash := uint64(14695981039346656037)
			for _, value := range plane {
				hash = (hash ^ uint64(value)) * 1099511628211
			}
			if hash != fixture.hashes[component] {
				reader.Close()
				t.Fatalf("%s filtered B plane %d hash = %016x", fixture.name, component, hash)
			}
		}
		frame.Y[0] ^= 1
		cached, err := decoder.DecodeFrame(1)
		reader.Close()
		if err != nil {
			t.Fatal(err)
		}
		if cached.Y[0] == frame.Y[0] {
			t.Fatal("returned B frame aliases the cache")
		}
	}
}

func TestNativeDecoderFilteredPReference(t *testing.T) {
	t.Setenv("PATH", "")
	for _, fixture := range []struct {
		name   string
		index  uint64
		hashes [3]uint64
	}{{"high42-1080p.mp4", 3, [3]uint64{0xa0513f65f6b02a72, 0xb0a2e9bbb5949342, 0x784053d21fb83a58}}, {"high52-2160p.mp4", 4, [3]uint64{0xadf923704a54e720, 0xeddc31b90090f325, 0xeddc31b90090f325}}} {
		reader, err := OpenVideoSampleReader(filepath.Join("..", "..", "testdata", "h264", fixture.name))
		if err != nil {
			t.Fatal(err)
		}
		samples, err := reader.DecodeOrderDependencySamples(fixture.index)
		if err != nil {
			reader.Close()
			t.Fatal(err)
		}
		decoder := NewH264Decoder(reader)
		frame, err := decoder.decodeNativeCodedIntraSample(samples[0])
		if err == nil {
			err = deblockNativePicture(&frame, decoder.nativeBlocks)
		}
		if err != nil {
			reader.Close()
			t.Fatal(err)
		}
		if err := decoder.StoreReferencePicture(ReferencePicture{ID: 0, FrameNum: 0, PictureOrderCnt: 0}, frame); err != nil {
			reader.Close()
			t.Fatal(err)
		}
		frame, err = decoder.decodeNativeInterSample(samples[1], int64(fixture.index*2))
		if err == nil {
			err = deblockNativePicture(&frame, decoder.nativeBlocks)
		}
		reader.Close()
		if err != nil {
			t.Fatalf("%s: %v", fixture.name, err)
		}
		frame = cropNativeFrame(frame, decoder.nativeBlocks[0].slice.sps)
		for component, plane := range [][]byte{frame.Y, frame.U, frame.V} {
			hash := uint64(14695981039346656037)
			for _, value := range plane {
				hash = (hash ^ uint64(value)) * 1099511628211
			}
			if hash != fixture.hashes[component] {
				t.Fatalf("%s filtered P plane %d hash = %016x", fixture.name, component, hash)
			}
		}
	}
}

func TestBDecoderSmoke64x64(t *testing.T) {
	assertDecoderSmokeFixture(t, "decoder-smoke-64x64.mp4", 64, [2]uint64{0xdd69fba2006d59e0, 0x93ecf9bed15545a5})
}

func assertDecoderSmokeFixture(t *testing.T, name string, dimension int, hashes [2]uint64) {
	t.Helper()
	path := filepath.Join("..", "..", "samples", name)
	reader, err := OpenVideoSampleReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	decoder := NewH264Decoder(reader)
	for frameIndex, wantHash := range hashes {
		frame, err := decoder.DecodeFrame(uint64(frameIndex))
		if err != nil {
			t.Fatalf("DecodeFrame(%d): %v", frameIndex, err)
		}
		if frame.Width != dimension || frame.Height != dimension {
			t.Fatalf("frame %d size = %dx%d; want %dx%d", frameIndex, frame.Width, frame.Height, dimension, dimension)
		}
		if got := h264SmokeFrameHash(frame); got != wantHash {
			t.Fatalf("frame %d YUV FNV64 = %016x; want %016x", frameIndex, got, wantHash)
		}
	}
}

func h264SmokeFrameHash(frame Yuv420Frame) uint64 {
	hash := uint64(14695981039346656037)
	for _, plane := range [][]uint8{frame.Y, frame.U, frame.V} {
		for _, sample := range plane {
			hash = (hash ^ uint64(sample)) * 1099511628211
		}
	}
	return hash
}

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
	references := decoder.referencePictureBuffer.References()
	if len(references) != 2 || references[0].ID != 0 || references[1].ID != 3 {
		t.Fatalf("decoded native reference state = %v; want IDs 0 and 3", references)
	}
	cached, err := decoder.DecodeFrame(1)
	if err != nil {
		t.Fatalf("DecodeFrame(1) on cached frame err = %v; want nil", err)
	}
	if len(decoder.referencePictureBuffer.References()) != 2 {
		t.Fatalf("cached decode unexpectedly changed references: got %d; want 2", len(decoder.referencePictureBuffer.References()))
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
