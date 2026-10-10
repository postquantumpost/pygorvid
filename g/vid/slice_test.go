package vid

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixedSliceBits(value uint32, width int) string {
	bits := make([]byte, width)
	for index := range width {
		if value&(1<<uint(width-index-1)) != 0 {
			bits[index] = '1'
		} else {
			bits[index] = '0'
		}
	}
	return string(bits)
}

func makeSliceNAL(header byte, firstMB, sliceType, ppsID uint32, suffix string) []byte {
	typePrefix := ""
	switch sliceType % 5 {
	case 0:
		typePrefix = "0" // reference-count override flag
	case 1:
		typePrefix = "00" // direct prediction and reference-count override flags
	}
	return makeSliceNALWithTypePrefix(header, firstMB, sliceType, ppsID, suffix, typePrefix)
}

func makeSliceNALWithTypePrefix(header byte, firstMB, sliceType, ppsID uint32, suffix, typePrefix string) []byte {
	bits := ueBits(firstMB) + ueBits(sliceType) + ueBits(ppsID) + suffix + typePrefix
	normalizedType := sliceType % 5
	if normalizedType == 0 || normalizedType == 1 {
		bits += "0" // ref_pic_list_reordering_flag_l0
		if normalizedType == 1 {
			bits += "0" // ref_pic_list_reordering_flag_l1
		}
	}
	if header&0x1f == 5 {
		bits += "00" // IDR decoded-reference marking flags
	} else if header&0x60 != 0 {
		bits += "0" // adaptive_ref_pic_marking_mode_flag
	}
	if normalizedType != 2 {
		bits += ueBits(0) // cabac_init_idc
	}
	bits += seBits(0) + ueBits(0) + seBits(0) + seBits(0) // QP and deblocking syntax
	for len(bits)%8 != 0 {
		bits += "1"
	}
	return append([]byte{header}, packBitString(bits)...)
}

func makeSlicePPS(redundant bool) PPSInfo {
	bits := ueBits(0) + ueBits(0) + "10"
	bits += ueBits(0) + ueBits(0) + ueBits(0) + "0" + "00"
	bits += seBits(0) + seBits(0) + seBits(0) + "10"
	if redundant {
		bits += "1"
	} else {
		bits += "0"
	}
	bits += "1"
	pps, err := ParsePPS(append([]byte{0x68}, packBitString(bits)...))
	if err != nil {
		panic(err)
	}
	return pps
}

func makePOC1SPS(deltaAlwaysZero bool) SPSInfo {
	syntax := "1" + "010" + "1" + "1" + "00" // High 4:2:0, 8-bit, no scaling matrix
	syntax += ueBits(0) + ueBits(1)
	if deltaAlwaysZero {
		syntax += "1"
	} else {
		syntax += "0"
	}
	syntax += seBits(-2) + seBits(3) + ueBits(2) + seBits(-1) + seBits(2)
	syntax += ueBits(0) + "0" + ueBits(0) + ueBits(0) + "11" + "00" + "1"
	info, err := ParseSPS(makeSPS(100, 0, 42, syntax))
	if err != nil {
		panic(err)
	}
	return info
}

func TestParseSliceHeaderPictureIdentity(t *testing.T) {
	sps := parseTestSPS(t, makeCompleteSPS(100, 0, 42, "1"+"010"+"1"+"1", 119, 67, true, [4]uint32{}))
	pps, err := ParsePPS(makePPS(0, 0, true, false))
	if err != nil {
		t.Fatal(err)
	}
	nal := makeSliceNAL(0x65, 0, 2, 0, fixedSliceBits(5, 4)+ueBits(7)+fixedSliceBits(3, 4))
	header, err := ParseSliceHeader(nal, sps, pps)
	if err != nil {
		t.Fatal(err)
	}
	if header.FirstMacroblockInSlice != 0 || header.SliceType != 2 || header.FrameNum != 5 ||
		!header.IDR || header.IDRPicID != 7 || header.PicOrderCntLSB != 3 || header.NALRefIDC != 3 {
		t.Fatalf("unexpected IDR slice header: %+v", header)
	}
	identity := header.PictureIdentity()
	if !identity.IDR || identity.IDRPicID != 7 || identity.FrameNum != 5 || identity.NALRefIDCZero {
		t.Fatalf("unexpected picture identity: %+v", identity)
	}
}

func TestParseSliceHeaderPOCModesAndFieldFlags(t *testing.T) {
	ppsWithBottomFieldPOC, err := ParsePPS(makePPS(0, 0, true, true))
	if err != nil {
		t.Fatal(err)
	}
	spsPOC1 := makePOC1SPS(true)
	for _, test := range []struct {
		name  string
		sps   SPSInfo
		pps   PPSInfo
		nal   []byte
		check func(SliceHeader) bool
	}{
		{
			name: "poc-zero-bottom-delta",
			sps:  parseTestSPS(t, makeCompleteSPS(100, 0, 42, "1"+"010"+"1"+"1", 0, 0, true, [4]uint32{})),
			pps:  ppsWithBottomFieldPOC,
			nal:  makeSliceNAL(0x41, 2, 0, 0, fixedSliceBits(9, 4)+fixedSliceBits(6, 4)+seBits(-2)),
			check: func(header SliceHeader) bool {
				return header.PicOrderCntLSB == 6 && header.HasDeltaPicOrderBottom && header.DeltaPicOrderBottom == -2
			},
		},
		{
			name:  "poc-one-zero-deltas",
			sps:   spsPOC1,
			pps:   ppsWithBottomFieldPOC,
			nal:   makeSliceNAL(0x01, 1, 1, 0, fixedSliceBits(2, 4)),
			check: func(header SliceHeader) bool { return !header.HasDeltaPicOrderCnt0 && !header.HasDeltaPicOrderCnt1 },
		},
		{
			name: "field-coded-bottom-field",
			sps:  parseTestSPS(t, makeCompleteSPS(66, 0, 30, "1", 0, 0, false, [4]uint32{})),
			pps:  ppsWithBottomFieldPOC,
			nal:  makeSliceNAL(0x01, 0, 0, 0, fixedSliceBits(3, 4)+"11"+fixedSliceBits(5, 4)),
			check: func(header SliceHeader) bool {
				return header.FieldPicFlag && header.BottomFieldFlag && header.PicOrderCntLSB == 5 && !header.HasDeltaPicOrderBottom
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			header, err := ParseSliceHeader(test.nal, test.sps, test.pps)
			if err != nil {
				t.Fatal(err)
			}
			if !test.check(header) {
				t.Fatalf("unexpected slice identity fields: %+v", header)
			}
		})
	}
}

func TestParseSliceHeaderIPBBranchesAndRedundantCount(t *testing.T) {
	sps := parseTestSPS(t, makeCompleteSPS(100, 0, 42, "1"+"010"+"1"+"1", 0, 0, true, [4]uint32{}))
	pps := makeSlicePPS(false)
	frameAndPOC := fixedSliceBits(3, 4) + fixedSliceBits(5, 4)

	iSlice, err := ParseSliceHeader(makeSliceNAL(0x41, 0, 2, 0, frameAndPOC), sps, pps)
	if err != nil || iSlice.NumRefIdxActiveOverride || iSlice.HasDirectSpatialMVPred {
		t.Fatalf("I-slice prefix = %+v, %v", iSlice, err)
	}
	pSlice, err := ParseSliceHeader(makeSliceNALWithTypePrefix(0x41, 0, 0, 0, frameAndPOC, "1"+ueBits(3)), sps, pps)
	if err != nil || !pSlice.NumRefIdxActiveOverride || pSlice.NumRefIdxL0ActiveMinus1 != 3 || pSlice.HasDirectSpatialMVPred {
		t.Fatalf("P-slice prefix = %+v, %v", pSlice, err)
	}
	bSlice, err := ParseSliceHeader(makeSliceNALWithTypePrefix(0x41, 0, 1, 0, frameAndPOC, "1"+"1"+ueBits(2)+ueBits(1)), sps, pps)
	if err != nil || !bSlice.DirectSpatialMVPred || !bSlice.NumRefIdxActiveOverride ||
		bSlice.NumRefIdxL0ActiveMinus1 != 2 || bSlice.NumRefIdxL1ActiveMinus1 != 1 {
		t.Fatalf("B-slice prefix = %+v, %v", bSlice, err)
	}
	redundant, err := ParseSliceHeader(makeSliceNALWithTypePrefix(0x41, 0, 2, 0, frameAndPOC, ueBits(5)), sps, makeSlicePPS(true))
	if err != nil || !redundant.HasRedundantPicCnt || redundant.RedundantPicCnt != 5 {
		t.Fatalf("redundant-picture prefix = %+v, %v", redundant, err)
	}
}

func TestParseSliceHeaderRejectsUnsupportedSliceClassAndOverrides(t *testing.T) {
	sps := parseTestSPS(t, makeCompleteSPS(100, 0, 42, "1"+"010"+"1"+"1", 0, 0, true, [4]uint32{}))
	pps := makeSlicePPS(false)
	prefix := fixedSliceBits(0, 4) + fixedSliceBits(0, 4)
	for _, test := range []struct {
		name   string
		typeID uint32
		suffix string
		kind   string
	}{
		{name: "SP-slice", typeID: 3, suffix: prefix, kind: "unsupported slice_type"},
		{name: "L0-override-out-of-range", typeID: 0, suffix: prefix, kind: "l0_active_minus1 exceeds 31"},
		{name: "B-L1-override-out-of-range", typeID: 1, suffix: prefix, kind: "l1_active_minus1 exceeds 31"},
	} {
		typePrefix := "1" + ueBits(32)
		if test.typeID == 1 {
			typePrefix = "1" + "1" + ueBits(0) + ueBits(32)
		}
		if test.typeID == 3 {
			typePrefix = ""
		}
		_, err := ParseSliceHeader(makeSliceNALWithTypePrefix(0x41, 0, test.typeID, 0, test.suffix, typePrefix), sps, pps)
		if err == nil || !strings.Contains(err.Error(), test.kind) {
			t.Errorf("%s error = %v; want %q", test.name, err, test.kind)
		}
	}
}

func TestParseSliceHeaderRejectsInvalidIdentitySyntax(t *testing.T) {
	sps := parseTestSPS(t, makeCompleteSPS(100, 0, 42, "1"+"010"+"1"+"1", 0, 0, true, [4]uint32{}))
	pps, err := ParsePPS(makePPS(0, 0, true, false))
	if err != nil {
		t.Fatal(err)
	}
	wrongPPS := makeSliceNAL(0x41, 0, 0, 1, fixedSliceBits(0, 4)+fixedSliceBits(0, 4))
	invalidType := makeSliceNAL(0x41, 0, 10, 0, fixedSliceBits(0, 4)+fixedSliceBits(0, 4))
	truncated := append([]byte{0x41}, packBitString(ueBits(0)+ueBits(0)+ueBits(0)+"00")...)
	for _, test := range []struct {
		name string
		nal  []byte
		want string
	}{
		{name: "wrong-nal-type", nal: []byte{0x67, 0x80}, want: "not a coded slice"},
		{name: "wrong-pps", nal: wrongPPS, want: "supplied PPS"},
		{name: "invalid-slice-type", nal: invalidType, want: "slice_type"},
		{name: "truncated-header", nal: truncated, want: "slice"},
	} {
		_, err := ParseSliceHeader(test.nal, sps, pps)
		if err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("%s error = %v; want %q", test.name, err, test.want)
		}
	}
}

func TestParseSliceHeaderRejectsPPSWithDifferentSPSReference(t *testing.T) {
	sps := parseTestSPS(t, makeCompleteSPS(100, 0, 42, "1"+"010"+"1"+"1", 0, 0, true, [4]uint32{}))
	pps, err := ParsePPS(makePPS(0, 1, true, false))
	if err != nil {
		t.Fatal(err)
	}
	nal := makeSliceNAL(0x41, 0, 2, 0, fixedSliceBits(0, 4)+fixedSliceBits(0, 4))
	if _, err := ParseSliceHeader(nal, sps, pps); err == nil || !strings.Contains(err.Error(), "PPS references SPS") {
		t.Fatalf("ParseSliceHeader() error = %v; want mismatched SPS reference", err)
	}
}

func TestSamePrimaryPictureBoundaries(t *testing.T) {
	base := SliceHeader{
		FrameNum: 3, PictureParameterSetID: 1, PicOrderCntType: 0,
		HasPicOrderCntLSB: true, PicOrderCntLSB: 4, NALRefIDC: 2,
	}
	tests := []struct {
		name   string
		mutate func(*SliceHeader)
		same   bool
	}{
		{name: "same-picture-different-first-macroblock", mutate: func(header *SliceHeader) { header.FirstMacroblockInSlice = 12 }, same: true},
		{name: "different-frame-number", mutate: func(header *SliceHeader) { header.FrameNum++ }},
		{name: "different-pps", mutate: func(header *SliceHeader) { header.PictureParameterSetID++ }},
		{name: "frame-versus-field", mutate: func(header *SliceHeader) { header.FieldPicFlag = true }},
		{name: "different-reference-zero-state", mutate: func(header *SliceHeader) { header.NALRefIDC = 0 }},
		{name: "idr-versus-non-idr", mutate: func(header *SliceHeader) { header.IDR = true }},
		{name: "different-poc-lsb", mutate: func(header *SliceHeader) { header.PicOrderCntLSB++ }},
		{name: "different-delta-poc-bottom", mutate: func(header *SliceHeader) { header.HasDeltaPicOrderBottom = true; header.DeltaPicOrderBottom = 1 }},
		{name: "different-separate-colour-plane", mutate: func(header *SliceHeader) { header.SeparateColourPlane = true }},
		{name: "different-colour-plane-id", mutate: func(header *SliceHeader) { header.SeparateColourPlane = true; header.ColourPlaneID = 1 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			other := base
			test.mutate(&other)
			if got := SamePrimaryPicture(base, other); got != test.same {
				t.Fatalf("SamePrimaryPicture() = %v, want %v", got, test.same)
			}
		})
	}

	pocOne := base
	pocOne.PicOrderCntType = 1
	pocOne.HasPicOrderCntLSB = false
	pocOne.HasDeltaPicOrderCnt0 = true
	pocOne.DeltaPicOrderCnt0 = -1
	pocOneOther := pocOne
	pocOneOther.DeltaPicOrderCnt0 = 1
	if SamePrimaryPicture(pocOne, pocOneOther) {
		t.Fatal("different POC type 1 deltas grouped into one picture")
	}
	pocTwo := base
	pocTwo.PicOrderCntType = 2
	pocTwo.HasPicOrderCntLSB = false
	pocTwoOther := pocTwo
	pocTwoOther.PicOrderCntLSB = 9
	if !SamePrimaryPicture(pocTwo, pocTwoOther) {
		t.Fatal("POC type 2 compared fields that are not present")
	}
}

func TestGroupSlicesIntoPictures(t *testing.T) {
	first := SliceHeader{FrameNum: 0, PicOrderCntType: 2}
	secondSlice := first
	secondSlice.FirstMacroblockInSlice = 8
	secondPicture := first
	secondPicture.FrameNum = 1
	groups := GroupSlicesIntoPictures([]SliceHeader{first, secondSlice, secondPicture})
	if len(groups) != 2 || len(groups[0].Slices) != 2 || len(groups[1].Slices) != 1 {
		t.Fatalf("unexpected picture groups: %+v", groups)
	}
	if GroupSlicesIntoPictures(nil) != nil {
		t.Fatal("empty input should produce no picture groups")
	}
}

func parseTestSPS(t *testing.T, nal []byte) SPSInfo {
	t.Helper()
	info, err := ParseSPS(nal)
	if err != nil {
		t.Fatal(err)
	}
	return info
}

func TestParseSliceHeadersCompactFixtures(t *testing.T) {
	for _, name := range []string{"high42-1080p.mp4", "high52-2160p.mp4"} {
		reader, err := OpenVideoSampleReader(filepath.Join("..", "..", "testdata", "h264", name))
		if err != nil {
			t.Fatal(err)
		}
		configuration := reader.Configuration()
		if err := reader.Close(); err != nil {
			t.Fatal(err)
		}
		sps, err := ParseSPS(configuration.SequenceSets[0])
		if err != nil {
			t.Fatal(err)
		}
		pps, err := ParsePPS(configuration.PictureSets[0])
		if err != nil {
			t.Fatal(err)
		}
		reader, err = OpenVideoSampleReader(filepath.Join("..", "..", "testdata", "h264", name))
		if err != nil {
			t.Fatal(err)
		}
		defer reader.Close()
		pictureTypes := make(map[uint32]int)
		for {
			sample, ok, err := reader.NextSample()
			if err != nil {
				t.Fatal(err)
			}
			if !ok {
				break
			}
			for offset := 0; offset < len(sample.Data); {
				if len(sample.Data)-offset < 4 {
					t.Fatalf("%s sample %d has a truncated NAL prefix", name, sample.Index)
				}
				size := int(binary.BigEndian.Uint32(sample.Data[offset : offset+4]))
				offset += 4
				if size == 0 || size > len(sample.Data)-offset {
					t.Fatalf("%s sample %d has an invalid NAL size", name, sample.Index)
				}
				nal := sample.Data[offset : offset+size]
				header, err := ParseNALHeader(nal)
				if err != nil {
					t.Fatal(err)
				}
				if header.UnitType == 1 || header.UnitType == 5 {
					slice, err := ParseSliceHeader(nal, sps, pps)
					if err != nil {
						t.Fatalf("%s sample %d: %v", name, sample.Index, err)
					}
					if slice.IDR && pps.EntropyCodingMode {
						if slice.SliceDataBitOffset%8 != 0 {
							t.Fatalf("%s IDR slice data bit offset %d is not byte-aligned", name, slice.SliceDataBitOffset)
						}
						rbsp, err := EBSPToRBSP(nal[1:])
						if err != nil {
							t.Fatalf("%s IDR slice RBSP: %v", name, err)
						}
						byteOffset := slice.SliceDataBitOffset / 8
						if byteOffset >= uint64(len(rbsp)) {
							t.Fatalf("%s IDR slice data offset %d exceeds %d-byte RBSP", name, byteOffset, len(rbsp))
						}
						if _, err := NewCABACArithmeticDecoder(rbsp[byteOffset:]); err != nil {
							t.Fatalf("%s IDR CABAC initialization: %v", name, err)
						}
						if slice.FirstMacroblockInSlice == 0 && slice.SliceType%5 == 2 {
							cabac, err := NewCABACArithmeticDecoder(rbsp[byteOffset:])
							if err != nil {
								t.Fatalf("%s IDR CABAC initialization: %v", name, err)
							}
							contexts, err := NewCABACIIntraMBTypeContexts(int(26 + pps.PicInitQpMinus26 + slice.SliceQPDelta))
							if err != nil {
								t.Fatalf("%s IDR mb_type contexts: %v", name, err)
							}
							mbType, err := cabac.DecodeIIntraMBType(slice.SliceType, &contexts, false, false, false, false)
							if err != nil {
								t.Fatalf("%s first IDR mb_type: %v", name, err)
							}
							if mbType != 0 {
								t.Fatalf("%s first IDR mb_type = %d; want Intra_NxN (0)", name, mbType)
							}
							if pps.Transform8x8Mode {
								transformContexts, err := NewCABACITransformSize8x8Contexts(int(26 + pps.PicInitQpMinus26 + slice.SliceQPDelta))
								if err != nil {
									t.Fatalf("%s transform_size_8x8 contexts: %v", name, err)
								}
								transform8x8, err := cabac.DecodeTransformSize8x8Flag(&transformContexts, false, false)
								if err != nil {
									t.Fatalf("%s transform_size_8x8_flag: %v", name, err)
								}
								if transform8x8 {
									t.Fatalf("%s first IDR transform_size_8x8_flag = true; want false", name)
								}
							}
							modeContexts, err := NewCABACIIntra4x4PredModeContexts(int(26 + pps.PicInitQpMinus26 + slice.SliceQPDelta))
							if err != nil {
								t.Fatalf("%s IDR Intra4x4 contexts: %v", name, err)
							}
							blockPositions := [16][2]int{
								{0, 0}, {1, 0}, {0, 1}, {1, 1},
								{2, 0}, {3, 0}, {2, 1}, {3, 1},
								{0, 2}, {1, 2}, {0, 3}, {1, 3},
								{2, 2}, {3, 2}, {2, 3}, {3, 3},
							}
							var modesByPosition [4][4]uint8
							var decodedModes [16]uint8
							for blockIndex, position := range blockPositions {
								x, y := position[0], position[1]
								leftMode, topMode := uint8(2), uint8(2)
								if x > 0 {
									leftMode = modesByPosition[y][x-1]
								}
								if y > 0 {
									topMode = modesByPosition[y-1][x]
								}
								predictedMode := leftMode
								if topMode < predictedMode {
									predictedMode = topMode
								}
								mode, err := cabac.DecodeIntra4x4PredMode(predictedMode, &modeContexts)
								if err != nil {
									t.Fatalf("%s IDR Intra4x4 block %d mode: %v", name, blockIndex, err)
								}
								modesByPosition[y][x] = mode
								decodedModes[blockIndex] = mode
							}
							wantModes := [16]uint8{2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2}
							if decodedModes != wantModes {
								t.Fatalf("%s first IDR luma prediction modes = %v; want %v", name, decodedModes, wantModes)
							}
							chromaContexts, err := NewCABACIIntraChromaPredModeContexts(int(26 + pps.PicInitQpMinus26 + slice.SliceQPDelta))
							if err != nil {
								t.Fatalf("%s IDR chroma prediction contexts: %v", name, err)
							}
							chromaMode, err := cabac.DecodeIntraChromaPredMode(&chromaContexts, false, false)
							if err != nil {
								t.Fatalf("%s first IDR intra_chroma_pred_mode: %v", name, err)
							}
							if chromaMode != 0 {
								t.Fatalf("%s first IDR intra_chroma_pred_mode = %d; want DC (0)", name, chromaMode)
							}
							lumaCBPContexts, err := NewCABACILumaCodedBlockPatternContexts(int(26 + pps.PicInitQpMinus26 + slice.SliceQPDelta))
							if err != nil {
								t.Fatalf("%s IDR luma coded-block-pattern contexts: %v", name, err)
							}
							lumaCBP, err := cabac.DecodeLumaCodedBlockPattern(0, 0, &lumaCBPContexts)
							if err != nil {
								t.Fatalf("%s first IDR luma coded_block_pattern: %v", name, err)
							}
							if lumaCBP != 13 {
								t.Fatalf("%s first IDR luma coded_block_pattern = %d; want 13", name, lumaCBP)
							}
							chromaCBPContexts, err := NewCABACIChromaCodedBlockPatternContexts(int(26 + pps.PicInitQpMinus26 + slice.SliceQPDelta))
							if err != nil {
								t.Fatalf("%s IDR chroma coded-block-pattern contexts: %v", name, err)
							}
							chromaCBP, err := cabac.DecodeChromaCodedBlockPattern(0, 0, &chromaCBPContexts)
							if err != nil {
								t.Fatalf("%s first IDR chroma coded_block_pattern: %v", name, err)
							}
							if chromaCBP != 2 {
								t.Fatalf("%s first IDR chroma coded_block_pattern = %d; want 2", name, chromaCBP)
							}
							qpDeltaContexts, err := NewCABACIMBQPDeltaContexts(int(26 + pps.PicInitQpMinus26 + slice.SliceQPDelta))
							if err != nil {
								t.Fatalf("%s IDR mb_qp_delta contexts: %v", name, err)
							}
							qpDelta, err := cabac.DecodeMBQPDelta(&qpDeltaContexts, 0)
							if err != nil {
								t.Fatalf("%s first IDR mb_qp_delta: %v", name, err)
							}
							wantQPDelta := map[string]int{"high42-1080p.mp4": -1, "high52-2160p.mp4": 0}[name]
							if qpDelta != wantQPDelta {
								t.Fatalf("%s first IDR mb_qp_delta = %d; want %d", name, qpDelta, wantQPDelta)
							}
							codedBlockFlagContexts, err := NewCABACILuma4x4CodedBlockFlagContexts(int(26+pps.PicInitQpMinus26+slice.SliceQPDelta) + qpDelta)
							if err != nil {
								t.Fatalf("%s IDR luma coded-block-flag contexts: %v", name, err)
							}
							codedBlockFlag, err := cabac.DecodeLuma4x4CodedBlockFlag(0, 0, &codedBlockFlagContexts)
							if err != nil {
								t.Fatalf("%s first IDR luma coded_block_flag: %v", name, err)
							}
							wantCodedBlockFlag := map[string]bool{"high42-1080p.mp4": true, "high52-2160p.mp4": false}[name]
							if codedBlockFlag != wantCodedBlockFlag {
								t.Fatalf("%s first IDR luma coded_block_flag = %t; want %t", name, codedBlockFlag, wantCodedBlockFlag)
							}
						}
					}
					pictureTypes[uint32(slice.SliceType%5)]++
				}
				offset += size
			}
		}
		for _, sliceType := range []uint32{0, 1, 2} {
			if pictureTypes[sliceType] == 0 {
				t.Errorf("%s has no slice type %d: %v", name, sliceType, pictureTypes)
			}
		}
	}
}

func TestParsedSPSPPSAndSliceMetadataForEveryEligibleInput(t *testing.T) {
	repositoryRoot := filepath.Join("..", "..")
	manifest, err := os.ReadFile(filepath.Join(repositoryRoot, "silkroad1.sha256"))
	if err != nil {
		t.Fatal(err)
	}
	activePaths := make([]string, 0, 10)
	for _, line := range strings.Split(string(manifest), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 {
			activePaths = append(activePaths, fields[1])
		}
	}
	if len(activePaths) != 10 {
		t.Fatalf("eligible input manifest has %d paths; want 10", len(activePaths))
	}
	var allSliceTypes [3]bool

	for _, relativePath := range activePaths {
		reader, err := OpenVideoSampleReader(filepath.Join(repositoryRoot, relativePath))
		if err != nil {
			t.Fatalf("%s: %v", relativePath, err)
		}
		configuration := reader.Configuration()
		if configuration.NALLengthSize != 4 || len(configuration.SequenceSets) != 1 || len(configuration.PictureSets) != 1 {
			t.Fatalf("%s has unexpected AVC configuration: %+v", relativePath, configuration)
		}
		sps, err := ParseSPS(configuration.SequenceSets[0])
		if err != nil {
			t.Fatalf("%s SPS: %v", relativePath, err)
		}
		pps, err := ParsePPS(configuration.PictureSets[0])
		if err != nil {
			t.Fatalf("%s PPS: %v", relativePath, err)
		}
		if sps.ProfileIDC != 100 || (sps.LevelIDC != 42 && sps.LevelIDC != 52) || sps.ChromaFormatIDC != 1 ||
			sps.BitDepthLuma != 8 || sps.BitDepthChroma != 8 || (sps.Width != 1920 && sps.Width != 3840) ||
			(sps.Height != 1080 && sps.Height != 2160) || !sps.FrameMbsOnly || sps.SeparateColourPlane {
			t.Fatalf("%s parsed unsupported SPS metadata: %+v", relativePath, sps)
		}
		if pps.SequenceParameterSetID != sps.ID || !pps.EntropyCodingMode || pps.NumSliceGroupsMinus1 != 0 {
			t.Fatalf("%s parsed unsupported/mismatched PPS metadata: %+v", relativePath, pps)
		}
		if pps.WeightedPred || pps.WeightedBipredIDC != 0 {
			t.Fatalf("%s uses unsupported weighted prediction: weighted_pred_flag=%t weighted_bipred_idc=%d", relativePath, pps.WeightedPred, pps.WeightedBipredIDC)
		}

		sliceCount := 0
		for {
			sample, ok, err := reader.NextSample()
			if err != nil {
				t.Fatalf("%s sample: %v", relativePath, err)
			}
			if !ok {
				break
			}
			for offset := 0; offset < len(sample.Data); {
				if len(sample.Data)-offset < 4 {
					t.Fatalf("%s sample %d has truncated NAL prefix", relativePath, sample.Index)
				}
				size := int(binary.BigEndian.Uint32(sample.Data[offset : offset+4]))
				offset += 4
				if size == 0 || size > len(sample.Data)-offset {
					t.Fatalf("%s sample %d has invalid NAL size", relativePath, sample.Index)
				}
				nal := sample.Data[offset : offset+size]
				header, err := ParseNALHeader(nal)
				if err != nil {
					t.Fatalf("%s sample %d NAL: %v", relativePath, sample.Index, err)
				}
				if header.UnitType == 1 || header.UnitType == 5 {
					slice, err := ParseSliceHeader(nal, sps, pps)
					if err != nil {
						t.Fatalf("%s sample %d slice: %v", relativePath, sample.Index, err)
					}
					if slice.PictureParameterSetID != pps.PictureParameterSetID || slice.PicOrderCntType != sps.PicOrderCntType || slice.IDR != (header.UnitType == 5) {
						t.Fatalf("%s sample %d inconsistent slice metadata: %+v", relativePath, sample.Index, slice)
					}
					sliceType := slice.SliceType % 5
					if sliceType > 2 {
						t.Fatalf("%s sample %d has unsupported slice type %d", relativePath, sample.Index, slice.SliceType)
					}
					allSliceTypes[sliceType] = true
					sliceCount++
				}
				offset += size
			}
		}
		if sliceCount == 0 {
			t.Errorf("%s has no parsed VCL slices", relativePath)
		}
		if err := reader.Close(); err != nil {
			t.Errorf("%s close: %v", relativePath, err)
		}
	}
	if allSliceTypes != [3]bool{true, true, true} {
		t.Fatalf("active corpus slice types = %v; want I, P, and B", allSliceTypes)
	}
}
