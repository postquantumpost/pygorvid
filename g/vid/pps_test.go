package vid

import (
	"path/filepath"
	"strings"
	"testing"
)

func packPPSBits(bits string) []byte {
	data := make([]byte, (len(bits)+7)/8)
	for index, bit := range bits {
		if bit == '1' {
			data[index/8] |= 1 << (7 - index%8)
		}
	}
	return data
}

func makePPS(ppsID, spsID uint32, entropyCoding, bottomFieldPOC bool) []byte {
	bits := ueBits(ppsID) + ueBits(spsID)
	if entropyCoding {
		bits += "1"
	} else {
		bits += "0"
	}
	if bottomFieldPOC {
		bits += "1"
	} else {
		bits += "0"
	}
	bits += ueBits(0) + ueBits(0) + ueBits(0) + "0" + "00"
	bits += seBits(0) + seBits(0) + seBits(0) + "1001"
	return append([]byte{0x68}, packPPSBits(bits)...)
}

func makePPSWithCorpusTools(transform8x8, scalingMatrix bool, secondChromaQP int64) []byte {
	bits := ueBits(0) + ueBits(0) + "10"
	bits += ueBits(0) + ueBits(0) + ueBits(0) + "1" + "01"
	bits += seBits(0) + seBits(0) + seBits(0) + "111"
	if transform8x8 {
		bits += "1"
	} else {
		bits += "0"
	}
	if scalingMatrix {
		bits += "1"
	} else {
		bits += "0"
	}
	if scalingMatrix {
		bits += "1" // one list-present flag; parser must reject scaling matrices
	}
	bits += seBits(secondChromaQP) + "1"
	return append([]byte{0x68}, packPPSBits(bits)...)
}

func TestParsePPSIdentifiersAndCoreFlags(t *testing.T) {
	tests := []struct {
		name           string
		nal            []byte
		ppsID          uint32
		spsID          uint32
		entropyCoding  bool
		bottomFieldPOC bool
	}{
		{name: "cabac-default", nal: makePPS(0, 0, true, false), ppsID: 0, spsID: 0, entropyCoding: true},
		{name: "cavlc-bottom-field", nal: makePPS(255, 31, false, true), ppsID: 255, spsID: 31, bottomFieldPOC: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			info, err := ParsePPS(test.nal)
			if err != nil {
				t.Fatal(err)
			}
			if info.PictureParameterSetID != test.ppsID || info.SequenceParameterSetID != test.spsID ||
				info.EntropyCodingMode != test.entropyCoding ||
				info.BottomFieldPicOrderInFramePresent != test.bottomFieldPOC {
				t.Fatalf("unexpected PPS fields: %+v", info)
			}
		})
	}
}

func TestParsePPSRejectsInvalidPrefix(t *testing.T) {
	for _, nal := range [][]byte{
		{0x67, 0x80},
		{0xe8, 0x80},
		makePPS(256, 0, false, false),
		makePPS(0, 32, false, false),
		makePPSWithCorpusTools(false, true, 0),
		makePPSWithCorpusTools(false, false, 13),
		{0x68, 0x80},
	} {
		if _, err := ParsePPS(nal); err == nil {
			t.Errorf("ParsePPS(%x) unexpectedly succeeded", nal)
		}
	}
}

func TestParsePPSCorpusTools(t *testing.T) {
	info, err := ParsePPS(makePPSWithCorpusTools(true, false, 0))
	if err != nil {
		t.Fatal(err)
	}
	if info.NumSliceGroupsMinus1 != 0 || info.NumRefIdxL0DefaultActiveMinus1 != 0 ||
		info.NumRefIdxL1DefaultActiveMinus1 != 0 || !info.WeightedPred || info.WeightedBipredIDC != 1 ||
		info.PicInitQpMinus26 != 0 || info.PicInitQsMinus26 != 0 || info.ChromaQPIndexOffset != 0 ||
		!info.DeblockingFilterControlPresent || !info.ConstrainedIntraPred || !info.RedundantPicCntPresent ||
		!info.HasExtension || !info.Transform8x8Mode || info.PicScalingMatrixPresent || info.SecondChromaQPIndexOffset != 0 {
		t.Fatalf("unexpected parsed PPS tools: %+v", info)
	}
	if _, err := ParsePPS(makePPS(0, 0, true, false)); err != nil {
		t.Fatalf("PPS with only rbsp_trailing_bits: %v", err)
	}
}

func TestParsePPSRejectsFMO(t *testing.T) {
	bits := ueBits(0) + ueBits(0) + "10" + ueBits(1)
	nal := append([]byte{0x68}, packPPSBits(bits)...)
	if _, err := ParsePPS(nal); err == nil || !strings.Contains(err.Error(), "slice groups") {
		t.Fatalf("ParsePPS() error = %v; want unsupported slice groups", err)
	}
}

func TestParsePPSCompactFixtures(t *testing.T) {
	for _, fixture := range []struct {
		name         string
		hasExtension bool
		transform8x8 bool
	}{
		{name: "high42-1080p.mp4"},
		{name: "high52-2160p.mp4", hasExtension: true, transform8x8: true},
	} {
		reader, err := OpenVideoSampleReader(filepath.Join("..", "..", "testdata", "h264", fixture.name))
		if err != nil {
			t.Fatal(err)
		}
		configuration := reader.Configuration()
		if err := reader.Close(); err != nil {
			t.Fatal(err)
		}
		for _, pictureSet := range configuration.PictureSets {
			info, err := ParsePPS(pictureSet)
			if err != nil {
				t.Fatalf("%s PPS: %v", fixture.name, err)
			}
			if info.PictureParameterSetID != 0 || info.SequenceParameterSetID != 0 ||
				!info.EntropyCodingMode || info.BottomFieldPicOrderInFramePresent || info.NumSliceGroupsMinus1 != 0 ||
				info.NumRefIdxL0DefaultActiveMinus1 != 0 || info.NumRefIdxL1DefaultActiveMinus1 != 0 ||
				info.WeightedPred || info.WeightedBipredIDC != 0 || info.PicInitQpMinus26 != 0 ||
				info.PicInitQsMinus26 != 0 || info.ChromaQPIndexOffset != 0 ||
				!info.DeblockingFilterControlPresent || info.ConstrainedIntraPred || info.RedundantPicCntPresent ||
				info.HasExtension != fixture.hasExtension || info.Transform8x8Mode != fixture.transform8x8 ||
				info.PicScalingMatrixPresent || info.SecondChromaQPIndexOffset != 0 {
				t.Errorf("%s PPS fields = %+v", fixture.name, info)
			}
		}
	}
}
