package vid

import (
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func makeSPS(profile, constraints, level byte, syntaxBits string) []byte {
	nal := []byte{0x67, profile, constraints, level}
	return append(nal, packBitString(syntaxBits)...)
}

func ueBits(value uint32) string {
	code := strconv.FormatUint(uint64(value)+1, 2)
	return strings.Repeat("0", len(code)-1) + code
}

func seBits(value int64) string {
	codeNum := uint64(-2 * value)
	if value > 0 {
		codeNum = uint64(2*value - 1)
	}
	return ueBits(uint32(codeNum))
}

func makeSPSWithPOC(frameNumMinus4, pocType uint32, pocSyntax string) []byte {
	syntax := "1" + "010" + "1" + "1" + "00" // High 4:2:0, 8-bit, qpprime=0, no scaling matrix
	syntax += ueBits(frameNumMinus4) + ueBits(pocType) + pocSyntax
	syntax += ueBits(0) + "0" + ueBits(0) + ueBits(0) + "11" + "00" + "1"
	return makeSPS(100, 0, 42, syntax)
}

func makeCompleteSPS(profile, constraints, level byte, syntaxBits string, widthMbsMinus1, heightMapUnitsMinus1 uint32, frameMbsOnly bool, crop [4]uint32) []byte {
	return makeCompleteSPSWithFlags(profile, constraints, level, syntaxBits, widthMbsMinus1, heightMapUnitsMinus1, 0, false, frameMbsOnly, false, true, crop)
}

func makeCompleteSPSWithFlags(profile, constraints, level byte, syntaxBits string, widthMbsMinus1, heightMapUnitsMinus1, maxNumRefFrames uint32, gapsAllowed, frameMbsOnly, mbAdaptiveFrameField, direct8x8Inference bool, crop [4]uint32) []byte {
	if profileHasChromaDepthSyntax(profile) {
		syntaxBits += "00" // qpprime bypass and absent scaling matrix
	}
	syntaxBits += ueBits(0) + ueBits(0) + ueBits(0) + ueBits(maxNumRefFrames)
	if gapsAllowed {
		syntaxBits += "1"
	} else {
		syntaxBits += "0"
	}
	syntaxBits += ueBits(widthMbsMinus1) + ueBits(heightMapUnitsMinus1)
	if frameMbsOnly {
		syntaxBits += "1"
	} else {
		syntaxBits += "0"
		if mbAdaptiveFrameField {
			syntaxBits += "1"
		} else {
			syntaxBits += "0"
		}
	}
	if direct8x8Inference {
		syntaxBits += "1"
	} else {
		syntaxBits += "0"
	}
	cropping := crop != [4]uint32{}
	if cropping {
		syntaxBits += "1"
		for _, offset := range crop {
			syntaxBits += ueBits(offset)
		}
	} else {
		syntaxBits += "0"
	}
	syntaxBits += "01" // no VUI, followed by rbsp_stop_one_bit
	return makeSPS(profile, constraints, level, syntaxBits)
}

func TestParseSPSProfileChromaAndBitDepth(t *testing.T) {
	tests := []struct {
		name                string
		nal                 []byte
		profile             uint8
		level               uint8
		spsID               uint32
		chroma              uint8
		separateColourPlane bool
		lumaDepth           uint8
		chromaDepth         uint8
	}{
		{
			name: "baseline defaults", nal: makeCompleteSPS(66, 0, 30, "1", 0, 0, true, [4]uint32{}), profile: 66, level: 30,
			chroma: 1, lumaDepth: 8, chromaDepth: 8,
		},
		{
			name: "high 420 8-bit", nal: makeCompleteSPS(100, 0, 42, "1"+"010"+"1"+"1", 0, 0, true, [4]uint32{}),
			profile: 100, level: 42, chroma: 1, lumaDepth: 8, chromaDepth: 8,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			info, err := ParseSPS(test.nal)
			if err != nil {
				t.Fatal(err)
			}
			if info.ProfileIDC != test.profile || info.LevelIDC != test.level || info.ID != test.spsID ||
				info.ChromaFormatIDC != test.chroma || info.SeparateColourPlane != test.separateColourPlane ||
				info.BitDepthLuma != test.lumaDepth || info.BitDepthChroma != test.chromaDepth {
				t.Fatalf("unexpected SPS fields: %+v", info)
			}
		})
	}
}

func TestParseSPSDimensionsAndCropping(t *testing.T) {
	progressive := makeCompleteSPS(100, 0, 42, "1"+"010"+"1"+"1", 119, 67, true, [4]uint32{0, 0, 0, 4})
	info, err := ParseSPS(progressive)
	if err != nil {
		t.Fatal(err)
	}
	if info.CodedWidth != 1920 || info.CodedHeight != 1088 || info.Width != 1920 || info.Height != 1080 ||
		info.FrameCropBottom != 8 {
		t.Fatalf("progressive cropped dimensions = %+v", info)
	}
	if !info.FrameMbsOnly || info.MbAdaptiveFrameField || !info.Direct8x8Inference {
		t.Fatalf("progressive flags = %+v", info)
	}
}

func TestParseSPSReferenceAndFrameFlags(t *testing.T) {
	nal := makeCompleteSPSWithFlags(100, 0, 42, "1"+"010"+"1"+"1", 0, 0, 4, true, true, false, true, [4]uint32{})
	info, err := ParseSPS(nal)
	if err != nil {
		t.Fatal(err)
	}
	if info.MaxNumRefFrames != 4 || !info.GapsInFrameNumValueAllowed || !info.FrameMbsOnly ||
		info.MbAdaptiveFrameField || !info.Direct8x8Inference {
		t.Fatalf("unexpected reference/frame flags: %+v", info)
	}
}

func TestParseSPSFrameNumberAndPOCModes(t *testing.T) {
	tests := []struct {
		name              string
		nal               []byte
		frameNumMinus4    uint32
		pocType           uint8
		hasPOCLSB         bool
		pocLSBMinus4      uint32
		deltaAlwaysZero   bool
		offsetNonRef      int64
		offsetTopBottom   int64
		offsetForRefFrame []int64
	}{
		{name: "poc-zero", nal: makeSPSWithPOC(4, 0, ueBits(3)), frameNumMinus4: 4, pocType: 0, hasPOCLSB: true, pocLSBMinus4: 3},
		{
			name: "poc-one", nal: makeSPSWithPOC(0, 1, "1"+seBits(-2)+seBits(3)+ueBits(2)+seBits(-1)+seBits(2)),
			pocType: 1, deltaAlwaysZero: true, offsetNonRef: -2, offsetTopBottom: 3,
			offsetForRefFrame: []int64{-1, 2},
		},
		{name: "poc-two", nal: makeSPSWithPOC(0, 2, ""), pocType: 2},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			info, err := ParseSPS(test.nal)
			if err != nil {
				t.Fatal(err)
			}
			if info.Log2MaxFrameNumMinus4 != test.frameNumMinus4 || info.PicOrderCntType != test.pocType ||
				info.HasPicOrderCntLsb != test.hasPOCLSB || info.Log2MaxPicOrderCntLsbMinus4 != test.pocLSBMinus4 ||
				info.DeltaPicOrderAlwaysZero != test.deltaAlwaysZero || info.OffsetForNonRefPic != test.offsetNonRef ||
				info.OffsetForTopToBottomField != test.offsetTopBottom || !reflect.DeepEqual(info.OffsetForRefFrame, test.offsetForRefFrame) {
				t.Fatalf("unexpected frame/POC fields: %+v", info)
			}
		})
	}
}

func TestParseSPSRejectsUnsupportedFeatureSet(t *testing.T) {
	tests := []struct {
		name string
		nal  []byte
	}{
		{name: "unsupported profile", nal: makeCompleteSPS(118, 0, 42, "1"+"010"+"1"+"1", 0, 0, true, [4]uint32{})},
		{name: "unsupported chroma format", nal: makeCompleteSPS(100, 0, 42, "1"+"00100"+"1"+"011"+"010", 0, 0, true, [4]uint32{})},
		{name: "unsupported bit depth", nal: makeCompleteSPS(100, 0, 42, "1"+"010"+"011"+"011", 0, 0, true, [4]uint32{})},
		{name: "unsupported progressiveness", nal: makeCompleteSPS(100, 0, 42, "1"+"010"+"1"+"1", 0, 0, false, [4]uint32{})},
		{name: "unsupported separate colour plane", nal: makeCompleteSPS(100, 0, 42, "1"+"00100"+"1"+"1"+"1", 0, 0, true, [4]uint32{})},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := ParseSPS(test.nal); err == nil {
				t.Fatal("expected unsupported-feature SPS parse error")
			}
		})
	}
}

func TestParseSPSRejectsInvalidFields(t *testing.T) {
	tests := []struct {
		name string
		nal  []byte
	}{
		{name: "wrong NAL type", nal: []byte{0x65, 66, 0, 30, 0x80}},
		{name: "reserved constraint bits", nal: makeSPS(66, 1, 30, "1")},
		{name: "SPS id out of range", nal: makeSPS(66, 0, 30, "00000100001")},
		{name: "chroma out of range", nal: makeSPS(100, 0, 42, "1"+"00101")},
		{name: "bit depth out of range", nal: makeSPS(100, 0, 42, "1"+"010"+"0001000")},
		{name: "truncated high-profile fields", nal: makeSPS(100, 0, 42, "1")},
		{name: "crop removes picture", nal: makeCompleteSPS(100, 0, 42, "1"+"010"+"1"+"1", 0, 0, true, [4]uint32{4, 4, 0, 0})},
		{name: "invalid POC type", nal: makeSPSWithPOC(0, 3, "")},
		{name: "frame number width out of range", nal: makeSPSWithPOC(13, 0, ueBits(0))},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := ParseSPS(test.nal); err == nil {
				t.Fatal("expected SPS parse error")
			}
		})
	}
}

func TestParseSPSCompactFixtures(t *testing.T) {
	for _, fixture := range []struct {
		name            string
		maxNumRefFrames uint32
	}{
		{name: "high42-1080p.mp4", maxNumRefFrames: 4},
		{name: "high52-2160p.mp4", maxNumRefFrames: 3},
	} {
		reader, err := OpenVideoSampleReader(filepath.Join("..", "..", "testdata", "h264", fixture.name))
		if err != nil {
			t.Fatal(err)
		}
		configuration := reader.Configuration()
		if err := reader.Close(); err != nil {
			t.Fatal(err)
		}
		for _, sequenceSet := range configuration.SequenceSets {
			info, err := ParseSPS(sequenceSet)
			if err != nil {
				t.Fatalf("%s SPS: %v", fixture.name, err)
			}
			if info.ProfileIDC != 100 || info.ChromaFormatIDC != 1 || info.BitDepthLuma != 8 || info.BitDepthChroma != 8 ||
				info.MaxNumRefFrames != fixture.maxNumRefFrames || info.GapsInFrameNumValueAllowed || !info.FrameMbsOnly ||
				info.MbAdaptiveFrameField || !info.Direct8x8Inference ||
				info.Width != 1920 && info.Width != 3840 {
				t.Errorf("%s SPS fields = %+v", fixture.name, info)
			}
			if fixture.name == "high42-1080p.mp4" && (info.Width != 1920 || info.Height != 1080) {
				t.Errorf("%s dimensions = %dx%d", fixture.name, info.Width, info.Height)
			}
			if fixture.name == "high52-2160p.mp4" && (info.Width != 3840 || info.Height != 2160) {
				t.Errorf("%s dimensions = %dx%d", fixture.name, info.Width, info.Height)
			}
		}
	}
}
