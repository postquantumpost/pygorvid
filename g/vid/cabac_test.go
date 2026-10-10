package vid

import (
	"bytes"
	"errors"
	"fmt"
	"testing"
)

func TestCABACArithmeticInitialization(t *testing.T) {
	decoder, err := NewCABACArithmeticDecoder([]byte{0b01010101, 0b10000000})
	if err != nil {
		t.Fatal(err)
	}
	if decoder.CodeRange() != 510 || decoder.CodeOffset() != 171 {
		t.Fatalf("initialized state = range %d offset %d; want 510, 171", decoder.CodeRange(), decoder.CodeOffset())
	}
	for _, test := range []struct {
		data       [2]byte
		wantOffset uint32
	}{
		{data: [2]byte{0x00, 0x00}, wantOffset: 0},
		{data: [2]byte{0xfe, 0x80}, wantOffset: 509},
	} {
		endpoint, err := NewCABACArithmeticDecoder(test.data[:])
		if err != nil {
			t.Fatalf("valid initial offset %d rejected: %v", test.wantOffset, err)
		}
		if endpoint.CodeRange() != 510 || endpoint.CodeOffset() != test.wantOffset {
			t.Errorf("initial endpoint state = range %d offset %d; want 510, %d", endpoint.CodeRange(), endpoint.CodeOffset(), test.wantOffset)
		}
		if remaining, err := endpoint.bits.ReadBits(7); err != nil || remaining != 0 {
			t.Errorf("initial offset %d did not leave the expected seven bits: value=%d err=%v", test.wantOffset, remaining, err)
		}
	}
	if _, err := NewCABACArithmeticDecoder([]byte{0xff, 0x80}); !errors.Is(err, ErrCABACOffsetOutOfRange) {
		t.Fatalf("offset 511 error = %v; want out-of-range", err)
	}
	if _, err := NewCABACArithmeticDecoder([]byte{0xff, 0x00}); !errors.Is(err, ErrCABACOffsetOutOfRange) {
		t.Fatalf("offset 510 error = %v; want out-of-range", err)
	}
	if _, err := NewCABACArithmeticDecoder([]byte{0xff}); !errors.Is(err, ErrBitstreamExhausted) {
		t.Fatalf("truncated initialization error = %v; want truncated bitstream", err)
	}
}

func TestCABACRenormalization(t *testing.T) {
	decoder := &CABACArithmeticDecoder{
		bits:       NewBitReader([]byte{0b10100000}),
		codeRange:  100,
		codeOffset: 40,
	}
	if err := decoder.Renormalize(); err != nil {
		t.Fatal(err)
	}
	if decoder.CodeRange() != 400 || decoder.CodeOffset() != 162 {
		t.Fatalf("renormalized state = range %d offset %d; want 400, 162", decoder.CodeRange(), decoder.CodeOffset())
	}
	if next, err := decoder.bits.ReadBit(); err != nil || !next {
		t.Fatalf("renormalization consumed wrong number of bits: next=%v err=%v", next, err)
	}
}

func TestCABACRenormalizationTruncationIsTransactional(t *testing.T) {
	decoder := &CABACArithmeticDecoder{
		bits:       NewBitReader([]byte{0x80}),
		codeRange:  64,
		codeOffset: 32,
	}
	decoder.bits.bitOffset = 7
	if err := decoder.Renormalize(); !errors.Is(err, ErrBitstreamExhausted) {
		t.Fatalf("Renormalize() error = %v; want truncated bitstream", err)
	}
	if decoder.CodeRange() != 64 || decoder.CodeOffset() != 32 {
		t.Fatalf("failed renormalization changed state to range %d offset %d", decoder.CodeRange(), decoder.CodeOffset())
	}
	if decoder.bits.bitOffset != 7 {
		t.Fatalf("failed renormalization consumed input through bit %d", decoder.bits.bitOffset)
	}
}

func TestCABACRejectsInvalidArithmeticState(t *testing.T) {
	decoder := &CABACArithmeticDecoder{bits: NewBitReader(nil), codeRange: 0, codeOffset: 0}
	if err := decoder.Renormalize(); !errors.Is(err, ErrCABACRangeOutOfRange) {
		t.Fatalf("Renormalize() error = %v; want invalid range", err)
	}
}

func TestCABACContextInitialization(t *testing.T) {
	for _, test := range []struct {
		n          int
		stateIndex uint8
		mps        bool
	}{

		{n: 63, stateIndex: 0, mps: false},
		{n: 64, stateIndex: 0, mps: true},
		{n: 0, stateIndex: 62, mps: false},
		{n: 127, stateIndex: 62, mps: true},
	} {
		model, err := NewCABACContextModel(0, test.n, 26)
		if err != nil {
			t.Fatal(err)
		}
		if model.StateIndex() != test.stateIndex || model.MPS() != test.mps {
			t.Errorf("context (m=0,n=%d) = (%d,%t); want (%d,%t)", test.n, model.StateIndex(), model.MPS(), test.stateIndex, test.mps)
		}
	}
	for _, test := range []struct {
		m, n, sliceQPY int
		stateIndex     uint8
		mps            bool
	}{
		{m: 1, n: 63, sliceQPY: 15, stateIndex: 0, mps: false},
		{m: 1, n: 63, sliceQPY: 16, stateIndex: 0, mps: true},
		{m: -1, n: 64, sliceQPY: 15, stateIndex: 0, mps: false},
		{m: 2, n: 0, sliceQPY: 51, stateIndex: 57, mps: false},
		{m: -2, n: 64, sliceQPY: 51, stateIndex: 6, mps: false},
		{m: -128, n: -128, sliceQPY: 0, stateIndex: 62, mps: false},
		{m: 127, n: 127, sliceQPY: 51, stateIndex: 62, mps: true},
	} {
		model, err := NewCABACContextModel(test.m, test.n, test.sliceQPY)
		if err != nil {
			t.Fatal(err)
		}
		if model.StateIndex() != test.stateIndex || model.MPS() != test.mps {
			t.Errorf("context (m=%d,n=%d,SliceQPY=%d) = (%d,%t); want (%d,%t)", test.m, test.n, test.sliceQPY, model.StateIndex(), model.MPS(), test.stateIndex, test.mps)
		}
	}
	for _, test := range []struct{ m, n, sliceQPY int }{
		{m: -129, n: 0, sliceQPY: 26}, {m: 128, n: 0, sliceQPY: 26},
		{m: 0, n: -129, sliceQPY: 26}, {m: 0, n: 128, sliceQPY: 26},
		{m: 0, n: 0, sliceQPY: -1}, {m: 0, n: 0, sliceQPY: 52},
	} {
		if _, err := NewCABACContextModel(test.m, test.n, test.sliceQPY); !errors.Is(err, ErrCABACContextInitRange) {
			t.Errorf("context parameters (%d,%d,%d) error = %v; want context initialization range", test.m, test.n, test.sliceQPY, err)
		}
	}
}

func TestCABACIIntraMBTypeContextInitialization(t *testing.T) {
	contexts, err := NewCABACIIntraMBTypeContexts(26)
	if err != nil {
		t.Fatal(err)
	}
	want := [8]struct {
		state uint8
		mps   bool
	}{
		{46, false}, {6, false}, {14, true}, {17, true},
		{2, true}, {20, false}, {11, false}, {1, false},
	}
	for index, context := range contexts {
		if context.StateIndex() != want[index].state || context.MPS() != want[index].mps {
			t.Errorf("ctxIdx %d initialized to (%d,%t); want (%d,%t)", index+3, context.StateIndex(), context.MPS(), want[index].state, want[index].mps)
		}
	}
	for _, qpy := range []int{-1, 52} {
		if _, err := NewCABACIIntraMBTypeContexts(qpy); !errors.Is(err, ErrCABACContextInitRange) {
			t.Errorf("SliceQPY %d error = %v; want out-of-range", qpy, err)
		}
	}
}

func TestCABACSliceContextsMatchSyntaxInitializers(t *testing.T) {
	contexts, err := NewCABACSliceContexts(7, 3, 26)
	if err != nil {
		t.Fatal(err)
	}
	same := func(name string, first int, want []CABACContextModel) {
		t.Helper()
		for index, model := range want {
			if contexts[first+index] != model {
				t.Errorf("%s ctxIdx %d = %+v; want %+v", name, first+index, contexts[first+index], model)
			}
		}
	}
	mbType, _ := NewCABACIIntraMBTypeContexts(26)
	qpDelta, _ := NewCABACIMBQPDeltaContexts(26)
	chroma, _ := NewCABACIIntraChromaPredModeContexts(26)
	intra4x4, _ := NewCABACIIntra4x4PredModeContexts(26)
	lumaCBP, _ := NewCABACILumaCodedBlockPatternContexts(26)
	chromaCBP, _ := NewCABACIChromaCodedBlockPatternContexts(26)
	codedFlag, _ := NewCABACILuma4x4CodedBlockFlagContexts(26)
	transform, _ := NewCABACITransformSize8x8Contexts(26)
	same("I mb_type", 3, mbType[:])
	same("mb_qp_delta", 60, qpDelta[:])
	same("intra_chroma_pred_mode", 64, chroma[:])
	same("intra4x4 mode", 68, intra4x4[:])
	same("luma CBP", 73, lumaCBP[:])
	same("chroma CBP", 77, chromaCBP[:])
	same("luma4x4 coded_block_flag", 93, codedFlag[:])
	same("transform_size_8x8_flag", 399, transform[:])
	for initIDC := uint8(0); initIDC <= 2; initIDC++ {
		contexts, err = NewCABACSliceContexts(5, initIDC, 30)
		if err != nil {
			t.Fatal(err)
		}
		pMBType, _ := NewCABACPInterMBTypeContexts(initIDC, 30)
		mvdX, mvdY, refIdx, _ := NewCABACInterPredictionContexts(initIDC, 30)
		same("P mb_type", 14, pMBType[:])
		same("mvd x", 40, mvdX[:])
		same("mvd y", 47, mvdY[:])
		same("ref_idx", 54, refIdx[:])
	}
}

func TestCABACSliceContextsUseStandardColumns(t *testing.T) {
	for _, test := range []struct {
		sliceType, initIDC uint8
		ctxIdx, m, n       int
		state              uint8
		mps                bool
	}{
		{sliceType: 2, ctxIdx: 85, m: -17, n: 123, state: 31, mps: true},
		{sliceType: 2, ctxIdx: 227, m: -3, n: 71, state: 2, mps: true},
		{sliceType: 2, ctxIdx: 402, m: -17, n: 120, state: 28, mps: true},
		{sliceType: 0, initIDC: 0, ctxIdx: 105, m: -2, n: 85, state: 17, mps: true},
		{sliceType: 1, initIDC: 1, ctxIdx: 30, m: -45, n: 127, state: 10, mps: false},
		{sliceType: 6, initIDC: 2, ctxIdx: 459, m: 20, n: 64, state: 32, mps: true},
	} {
		contexts, err := NewCABACSliceContexts(test.sliceType, test.initIDC, 26)
		if err != nil {
			t.Fatal(err)
		}
		model, _ := NewCABACContextModel(test.m, test.n, 26)
		got := contexts[test.ctxIdx]
		if got != *model || got.StateIndex() != test.state || got.MPS() != test.mps {
			t.Errorf("slice %d idc %d ctxIdx %d = (%d,%t); want (%d,%t)", test.sliceType, test.initIDC, test.ctxIdx, got.StateIndex(), got.MPS(), test.state, test.mps)
		}
	}
	for _, test := range []struct {
		sliceType, initIDC uint8
		sliceQPY           int
		want               error
	}{
		{10, 0, 26, ErrCABACUnsupportedSyntax},
		{0, 3, 26, ErrCABACContextInitRange},
		{1, 3, 26, ErrCABACContextInitRange},
		{2, 0, 52, ErrCABACContextInitRange},
	} {
		if _, err := NewCABACSliceContexts(test.sliceType, test.initIDC, test.sliceQPY); !errors.Is(err, test.want) {
			t.Errorf("NewCABACSliceContexts(%d,%d,%d) error = %v; want %v", test.sliceType, test.initIDC, test.sliceQPY, err, test.want)
		}
	}
}

func TestCABACResidualContextBases(t *testing.T) {
	want := [6]CABACResidualContextBases{
		{85, 105, 166, 227}, {89, 120, 181, 237}, {93, 134, 195, 247},
		{97, 149, 210, 257}, {101, 152, 213, 266}, {-1, 402, 417, 426},
	}
	for category, expected := range want {
		got, err := CABACResidualContextBasesForCategory(uint8(category))
		if err != nil || got != expected {
			t.Errorf("ctxBlockCat %d bases = %+v, %v; want %+v", category, got, err, expected)
		}
	}
	if _, err := CABACResidualContextBasesForCategory(6); !errors.Is(err, ErrCABACUnsupportedSyntax) {
		t.Fatalf("ctxBlockCat 6 error = %v; want unsupported syntax", err)
	}
}

func TestCABACPInterMBTypeContextInitialization(t *testing.T) {
	want := [3][4]struct {
		state uint8
		mps   bool
	}{
		{{54, false}, {14, false}, {54, true}, {6, false}},
		{{54, false}, {22, false}, {54, true}, {1, true}},
		{{12, false}, {1, false}, {35, true}, {47, false}},
	}
	for initIDC := uint8(0); initIDC <= 2; initIDC++ {
		contexts, err := NewCABACPInterMBTypeContexts(initIDC, 0)
		if err != nil {
			t.Fatal(err)
		}
		for index, context := range contexts {
			if context.StateIndex() != want[initIDC][index].state || context.MPS() != want[initIDC][index].mps {
				t.Errorf("cabac_init_idc %d ctxIdx %d = (%d,%t); want (%d,%t)", initIDC, index+14, context.StateIndex(), context.MPS(), want[initIDC][index].state, want[initIDC][index].mps)
			}
		}
	}
	if _, err := NewCABACPInterMBTypeContexts(3, 26); !errors.Is(err, ErrCABACContextInitRange) {
		t.Fatalf("invalid cabac_init_idc error = %v; want context-init range error", err)
	}
	if _, err := NewCABACPInterMBTypeContexts(0, 52); !errors.Is(err, ErrCABACContextInitRange) {
		t.Fatalf("invalid SliceQPY error = %v; want context-init range error", err)
	}
}

func TestCABACInterPredictionContextInitialization(t *testing.T) {
	mvdX, mvdY, refIdx, err := NewCABACInterPredictionContexts(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	wantMPSX := [7]bool{true, true, true, false, true, true, true}
	for index, context := range mvdX {
		if context.StateIndex() != [7]uint8{5, 17, 32, 8, 3, 22, 24}[index] || context.MPS() != wantMPSX[index] {
			t.Errorf("MVD X context %d = (%d,%t); want expected Table 9-15 state", index+40, context.StateIndex(), context.MPS())
		}
	}
	for index, context := range mvdY {
		wantStates := [7]uint8{5, 12, 30, 9, 5, 17, 24}
		wantMPS := [7]bool{false, true, true, false, true, true, true}
		if context.StateIndex() != wantStates[index] || context.MPS() != wantMPS[index] {
			t.Errorf("MVD Y context %d = (%d,%t); want (%d,%t)", index+47, context.StateIndex(), context.MPS(), wantStates[index], wantMPS[index])
		}
	}
	wantRefStates := [6]uint8{3, 10, 10, 16, 8, 5}
	wantRefMPS := [6]bool{true, true, true, true, true, false}
	for index, context := range refIdx {
		if context.StateIndex() != wantRefStates[index] || context.MPS() != wantRefMPS[index] {
			t.Errorf("ref_idx context %d = (%d,%t); want (%d,%t)", index+54, context.StateIndex(), context.MPS(), wantRefStates[index], wantRefMPS[index])
		}
	}
	for _, input := range [][2]int{{3, 26}, {0, -1}, {0, 52}} {
		if _, _, _, err := NewCABACInterPredictionContexts(uint8(input[0]), input[1]); !errors.Is(err, ErrCABACContextInitRange) {
			t.Errorf("inter contexts (%d,%d) error = %v; want context-init range", input[0], input[1], err)
		}
	}
}

func TestCABACIIntraChromaPredModeContextInitialization(t *testing.T) {
	contexts, err := NewCABACIIntraChromaPredModeContexts(26)
	if err != nil {
		t.Fatal(err)
	}
	want := [4]struct {
		state uint8
		mps   bool
	}{{4, true}, {28, true}, {33, true}, {3, false}}
	for index, context := range contexts {
		if context.StateIndex() != want[index].state || context.MPS() != want[index].mps {
			t.Errorf("ctxIdx %d initialized to (%d,%t); want (%d,%t)", index+64, context.StateIndex(), context.MPS(), want[index].state, want[index].mps)
		}
	}
	for _, qpy := range []int{-1, 52} {
		if _, err := NewCABACIIntraChromaPredModeContexts(qpy); !errors.Is(err, ErrCABACContextInitRange) {
			t.Errorf("SliceQPY %d error = %v; want out-of-range", qpy, err)
		}
	}
}

func TestCABACIIntra4x4PredModeContextInitialization(t *testing.T) {
	contexts, err := NewCABACIIntra4x4PredModeContexts(26)
	if err != nil {
		t.Fatal(err)
	}
	want := [2]struct {
		state uint8
		mps   bool
	}{{1, false}, {2, true}}
	for index, context := range contexts {
		if context.StateIndex() != want[index].state || context.MPS() != want[index].mps {
			t.Errorf("ctxIdx %d initialized to (%d,%t); want (%d,%t)", index+68, context.StateIndex(), context.MPS(), want[index].state, want[index].mps)
		}
	}
	for _, qpy := range []int{-1, 52} {
		if _, err := NewCABACIIntra4x4PredModeContexts(qpy); !errors.Is(err, ErrCABACContextInitRange) {
			t.Errorf("SliceQPY %d error = %v; want out-of-range", qpy, err)
		}
	}
}

func TestCABACITransformSize8x8ContextInitialization(t *testing.T) {
	contexts, err := NewCABACITransformSize8x8Contexts(26)
	if err != nil {
		t.Fatal(err)
	}
	want := [3]struct {
		state uint8
		mps   bool
	}{{7, true}, {17, true}, {26, true}}
	for index, context := range contexts {
		if context.StateIndex() != want[index].state || context.MPS() != want[index].mps {
			t.Errorf("ctxIdx %d initialized to (%d,%t); want (%d,%t)", index+399, context.StateIndex(), context.MPS(), want[index].state, want[index].mps)
		}
	}
	for _, qpy := range []int{-1, 52} {
		if _, err := NewCABACITransformSize8x8Contexts(qpy); !errors.Is(err, ErrCABACContextInitRange) {
			t.Errorf("SliceQPY %d error = %v; want out-of-range", qpy, err)
		}
	}
}

func TestCABACContextUpdate(t *testing.T) {
	model, err := NewCABACContextModel(0, 64, 26)
	if err != nil {
		t.Fatal(err)
	}
	model.Update(false)
	if model.StateIndex() != 0 || model.MPS() {
		t.Fatalf("state-zero LPS update = (%d,%t); want (0,false)", model.StateIndex(), model.MPS())
	}
	model.Update(false)
	if model.StateIndex() != 1 || model.MPS() {
		t.Fatalf("state-zero MPS update = (%d,%t); want (1,false)", model.StateIndex(), model.MPS())
	}

	lps, err := NewCABACContextModel(0, 53, 26)
	if err != nil {
		t.Fatal(err)
	}
	lps.Update(true)
	if lps.StateIndex() != 8 || lps.MPS() {
		t.Fatalf("state-ten LPS update = (%d,%t); want (8,false)", lps.StateIndex(), lps.MPS())
	}

	saturated, err := NewCABACContextModel(0, 0, 26)
	if err != nil {
		t.Fatal(err)
	}
	saturated.Update(false)
	if saturated.StateIndex() != 62 || saturated.MPS() {
		t.Fatalf("saturated MPS update = (%d,%t); want (62,false)", saturated.StateIndex(), saturated.MPS())
	}
}

func TestCABACContextTransitionsMatchStandardVectors(t *testing.T) {
	lpsTransitions := [...]uint8{
		0, 0, 1, 2, 2, 4, 4, 5, 6, 7, 8, 9, 9, 11, 11, 12,
		13, 13, 15, 15, 16, 16, 18, 18, 19, 19, 21, 21, 22, 22, 23, 24,
		24, 25, 26, 26, 27, 27, 28, 29, 29, 30, 30, 30, 31, 32, 32, 33,
		33, 33, 34, 34, 35, 35, 35, 36, 36, 37, 37, 37, 38, 38, 63, 63,
	}
	for stateIndex, expected := range lpsTransitions {
		model := &CABACContextModel{stateIndex: uint8(stateIndex), valueMPS: false}
		model.Update(true)
		wantMPS := stateIndex == 0
		if model.StateIndex() != expected || model.MPS() != wantMPS {
			t.Errorf("LPS transition from state %d = (%d,%t); want (%d,%t)", stateIndex, model.StateIndex(), model.MPS(), expected, wantMPS)
		}
	}
	for stateIndex := 0; stateIndex < 64; stateIndex++ {
		model := &CABACContextModel{stateIndex: uint8(stateIndex), valueMPS: false}
		model.Update(false)
		want := uint8(stateIndex)
		if stateIndex < 62 {
			want++
		}
		if model.StateIndex() != want || model.MPS() {
			t.Errorf("MPS transition from state %d = (%d,%t); want (%d,false)", stateIndex, model.StateIndex(), model.MPS(), want)
		}
	}
}

func TestCABACBypassBin(t *testing.T) {
	for _, test := range []struct {
		data       []byte
		codeOffset uint32
		wantBin    bool
		wantOffset uint32
	}{
		{data: []byte{0x00}, codeOffset: 200, wantBin: false, wantOffset: 400},
		{data: []byte{0x80}, codeOffset: 260, wantBin: true, wantOffset: 11},
	} {
		decoder := &CABACArithmeticDecoder{bits: NewBitReader(test.data), codeRange: 510, codeOffset: test.codeOffset}
		got, err := decoder.DecodeBypassBin()
		if err != nil {
			t.Fatal(err)
		}
		if got != test.wantBin || decoder.CodeRange() != 510 || decoder.CodeOffset() != test.wantOffset {
			t.Errorf("bypass = (%t, range %d, offset %d); want (%t, 510, %d)", got, decoder.CodeRange(), decoder.CodeOffset(), test.wantBin, test.wantOffset)
		}
	}
}

func TestCABACBypassBinMatchesThresholdVectors(t *testing.T) {
	for _, test := range []struct {
		codeOffset uint32
		wantBin    bool
		wantOffset uint32
	}{
		{codeOffset: 254, wantBin: false, wantOffset: 508},
		{codeOffset: 255, wantBin: true, wantOffset: 0},
	} {
		decoder := &CABACArithmeticDecoder{bits: NewBitReader([]byte{0x00}), codeRange: 510, codeOffset: test.codeOffset}
		got, err := decoder.DecodeBypassBin()
		if err != nil || got != test.wantBin {
			t.Errorf("bypass offset %d = %t, %v; want %t, nil", test.codeOffset, got, err, test.wantBin)
			continue
		}
		if decoder.CodeRange() != 510 || decoder.CodeOffset() != test.wantOffset || decoder.bits.bitOffset != 1 {
			t.Errorf("bypass offset %d left state at range %d offset %d bit %d", test.codeOffset, decoder.CodeRange(), decoder.CodeOffset(), decoder.bits.bitOffset)
		}
	}
}

func TestCABACBypassTruncationIsTransactional(t *testing.T) {
	decoder := &CABACArithmeticDecoder{bits: NewBitReader(nil), codeRange: 510, codeOffset: 200}
	if _, err := decoder.DecodeBypassBin(); !errors.Is(err, ErrBitstreamExhausted) {
		t.Fatalf("DecodeBypassBin() error = %v; want truncated bitstream", err)
	}
	if decoder.CodeRange() != 510 || decoder.CodeOffset() != 200 || decoder.bits.bitOffset != 0 {
		t.Fatalf("failed bypass changed state: range=%d offset=%d bitOffset=%d", decoder.CodeRange(), decoder.CodeOffset(), decoder.bits.bitOffset)
	}
}

func TestCABACTerminateBin(t *testing.T) {
	terminated := &CABACArithmeticDecoder{bits: NewBitReader(nil), codeRange: 300, codeOffset: 299}
	got, err := terminated.DecodeTerminateBin()
	if err != nil || !got {
		t.Fatalf("terminating bin = %t, %v; want true, nil", got, err)
	}
	if terminated.CodeRange() != 298 {
		t.Fatalf("terminal range = %d; want 298", terminated.CodeRange())
	}
	if _, err := terminated.DecodeBypassBin(); !errors.Is(err, ErrCABACTerminated) {
		t.Fatalf("decode after termination error = %v; want terminated", err)
	}
	model := &CABACContextModel{stateIndex: 10, valueMPS: true}
	if _, err := terminated.DecodeBin(model); !errors.Is(err, ErrCABACTerminated) {
		t.Fatalf("regular bin after termination error = %v; want terminated", err)
	}
	if _, err := terminated.DecodeTerminateBin(); !errors.Is(err, ErrCABACTerminated) {
		t.Fatalf("repeated terminate bin error = %v; want terminated", err)
	}
	if terminated.CodeRange() != 298 || terminated.CodeOffset() != 299 || terminated.bits.bitOffset != 0 || model.StateIndex() != 10 || !model.MPS() {
		t.Fatalf("failed post-termination bin changed decoder or context state")
	}
	if err := terminated.Renormalize(); !errors.Is(err, ErrCABACTerminated) {
		t.Fatalf("renormalize after termination error = %v; want terminated", err)
	}

	continuing := &CABACArithmeticDecoder{bits: NewBitReader([]byte{0x80}), codeRange: 257, codeOffset: 0}
	got, err = continuing.DecodeTerminateBin()
	if err != nil || got {
		t.Fatalf("continuing bin = %t, %v; want false, nil", got, err)
	}
	if continuing.CodeRange() != 510 || continuing.CodeOffset() != 1 || continuing.bits.bitOffset != 1 {
		t.Fatalf("renormalized state = range %d offset %d bitOffset %d; want 510, 1, 1", continuing.CodeRange(), continuing.CodeOffset(), continuing.bits.bitOffset)
	}
}

func TestCABACTerminateBinMatchesThresholdVectors(t *testing.T) {
	below := &CABACArithmeticDecoder{bits: NewBitReader(nil), codeRange: 300, codeOffset: 297}
	if terminated, err := below.DecodeTerminateBin(); err != nil || terminated {
		t.Fatalf("offset below terminate threshold = %t, %v; want continue", terminated, err)
	}
	if below.CodeRange() != 298 || below.CodeOffset() != 297 {
		t.Fatalf("continuing threshold state = range %d offset %d; want 298, 297", below.CodeRange(), below.CodeOffset())
	}

	at := &CABACArithmeticDecoder{bits: NewBitReader(nil), codeRange: 300, codeOffset: 298}
	if terminated, err := at.DecodeTerminateBin(); err != nil || !terminated {
		t.Fatalf("offset at terminate threshold = %t, %v; want terminate", terminated, err)
	}
	if at.CodeRange() != 298 || at.CodeOffset() != 298 {
		t.Fatalf("terminated threshold state = range %d offset %d; want 298, 298", at.CodeRange(), at.CodeOffset())
	}
}

func TestCABACTerminateTruncationIsTransactional(t *testing.T) {
	decoder := &CABACArithmeticDecoder{bits: NewBitReader(nil), codeRange: 257, codeOffset: 0}
	if _, err := decoder.DecodeTerminateBin(); !errors.Is(err, ErrBitstreamExhausted) {
		t.Fatalf("DecodeTerminateBin() error = %v; want truncated bitstream", err)
	}
	if decoder.CodeRange() != 257 || decoder.CodeOffset() != 0 || decoder.bits.bitOffset != 0 {
		t.Fatalf("failed terminate changed state: range=%d offset=%d bitOffset=%d", decoder.CodeRange(), decoder.CodeOffset(), decoder.bits.bitOffset)
	}
}

func TestCABACRegularBinMPSAndLPS(t *testing.T) {
	mpsDecoder := &CABACArithmeticDecoder{bits: NewBitReader(nil), codeRange: 510, codeOffset: 0}
	mpsModel := &CABACContextModel{stateIndex: 0, valueMPS: false}
	got, err := mpsDecoder.DecodeBin(mpsModel)
	if err != nil || got {
		t.Fatalf("MPS bin = %t, %v; want false, nil", got, err)
	}
	if mpsDecoder.CodeRange() != 270 || mpsDecoder.CodeOffset() != 0 || mpsModel.StateIndex() != 1 || mpsModel.MPS() {
		t.Fatalf("MPS state = range %d offset %d context (%d,%t); want 270, 0, (1,false)", mpsDecoder.CodeRange(), mpsDecoder.CodeOffset(), mpsModel.StateIndex(), mpsModel.MPS())
	}

	lpsDecoder := &CABACArithmeticDecoder{bits: NewBitReader([]byte{0x80}), codeRange: 510, codeOffset: 270}
	lpsModel := &CABACContextModel{stateIndex: 0, valueMPS: false}
	got, err = lpsDecoder.DecodeBin(lpsModel)
	if err != nil || !got {
		t.Fatalf("LPS bin = %t, %v; want true, nil", got, err)
	}
	if lpsDecoder.CodeRange() != 480 || lpsDecoder.CodeOffset() != 1 || lpsModel.StateIndex() != 0 || !lpsModel.MPS() {
		t.Fatalf("LPS state = range %d offset %d context (%d,%t); want 480, 1, (0,true)", lpsDecoder.CodeRange(), lpsDecoder.CodeOffset(), lpsModel.StateIndex(), lpsModel.MPS())
	}
}

func TestCABACRegularBinUsesRangeClassAndRollsBackOnTruncation(t *testing.T) {
	decoder := &CABACArithmeticDecoder{bits: NewBitReader([]byte{0x80}), codeRange: 300, codeOffset: 0}
	model := &CABACContextModel{stateIndex: 0, valueMPS: false}
	decoded, err := decoder.DecodeBin(model)
	if err != nil || decoded {
		t.Fatalf("qIdx 0 MPS bin = %t, %v; want false, nil", decoded, err)
	}
	if decoder.CodeRange() != 344 || decoder.CodeOffset() != 1 || decoder.bits.bitOffset != 1 {
		t.Fatalf("qIdx 0 state = range %d offset %d bitOffset %d; want 344, 1, 1", decoder.CodeRange(), decoder.CodeOffset(), decoder.bits.bitOffset)
	}
	for _, test := range []struct {
		codeRange  uint32
		data       []byte
		wantRange  uint32
		wantOffset uint32
	}{
		{codeRange: 300, data: []byte{0x80}, wantRange: 344, wantOffset: 1},
		{codeRange: 350, data: []byte{0x80}, wantRange: 348, wantOffset: 1},
		{codeRange: 410, data: []byte{0x00}, wantRange: 404, wantOffset: 0},
		{codeRange: 510, data: nil, wantRange: 270, wantOffset: 0},
	} {
		classDecoder := &CABACArithmeticDecoder{bits: NewBitReader(test.data), codeRange: test.codeRange}
		classModel := &CABACContextModel{stateIndex: 0, valueMPS: false}
		if decoded, err := classDecoder.DecodeBin(classModel); err != nil || decoded {
			t.Errorf("range class %d MPS bin = %t, %v; want false, nil", (test.codeRange>>6)&3, decoded, err)
		}
		if classDecoder.CodeRange() != test.wantRange || classDecoder.CodeOffset() != test.wantOffset {
			t.Errorf("range class %d state = (%d,%d); want (%d,%d)", (test.codeRange>>6)&3, classDecoder.CodeRange(), classDecoder.CodeOffset(), test.wantRange, test.wantOffset)
		}
	}
	stateTen := &CABACArithmeticDecoder{bits: NewBitReader([]byte{0x00}), codeRange: 510, codeOffset: 368}
	stateTenModel := &CABACContextModel{stateIndex: 10, valueMPS: false}
	if decoded, err := stateTen.DecodeBin(stateTenModel); err != nil || !decoded {
		t.Fatalf("state-ten LPS bin = %t, %v; want true, nil", decoded, err)
	}
	if stateTen.CodeRange() != 284 || stateTen.CodeOffset() != 0 || stateTenModel.StateIndex() != 8 || stateTenModel.MPS() {
		t.Fatalf("state-ten LPS state = range %d offset %d context (%d,%t); want 284, 0, (8,false)", stateTen.CodeRange(), stateTen.CodeOffset(), stateTenModel.StateIndex(), stateTenModel.MPS())
	}

	truncated := &CABACArithmeticDecoder{bits: NewBitReader(nil), codeRange: 510, codeOffset: 270}
	unchanged := &CABACContextModel{stateIndex: 0, valueMPS: false}
	if _, err := truncated.DecodeBin(unchanged); !errors.Is(err, ErrBitstreamExhausted) {
		t.Fatalf("truncated regular bin error = %v; want truncated bitstream", err)
	}
	if truncated.CodeRange() != 510 || truncated.CodeOffset() != 270 || truncated.bits.bitOffset != 0 || unchanged.StateIndex() != 0 || unchanged.MPS() {
		t.Fatalf("failed regular bin changed decoder/context state")
	}
}

func TestCABACRegularBinMatchesRangeMPSBoundaryVectors(t *testing.T) {
	below := &CABACArithmeticDecoder{bits: NewBitReader(nil), codeRange: 510, codeOffset: 269}
	belowModel := &CABACContextModel{stateIndex: 0, valueMPS: false}
	if decoded, err := below.DecodeBin(belowModel); err != nil || decoded {
		t.Fatalf("offset below rangeMPS decoded = %t, %v; want MPS false", decoded, err)
	}
	if below.CodeRange() != 270 || below.CodeOffset() != 269 || belowModel.StateIndex() != 1 || belowModel.MPS() {
		t.Fatalf("below-boundary state = range %d offset %d context (%d,%t)", below.CodeRange(), below.CodeOffset(), belowModel.StateIndex(), belowModel.MPS())
	}

	above := &CABACArithmeticDecoder{bits: NewBitReader([]byte{0x00}), codeRange: 510, codeOffset: 271}
	aboveModel := &CABACContextModel{stateIndex: 0, valueMPS: false}
	if decoded, err := above.DecodeBin(aboveModel); err != nil || !decoded {
		t.Fatalf("offset above rangeMPS decoded = %t, %v; want LPS true", decoded, err)
	}
	if above.CodeRange() != 480 || above.CodeOffset() != 2 || above.bits.bitOffset != 1 || aboveModel.StateIndex() != 0 || !aboveModel.MPS() {
		t.Fatalf("above-boundary state = range %d offset %d bit %d context (%d,%t)", above.CodeRange(), above.CodeOffset(), above.bits.bitOffset, aboveModel.StateIndex(), aboveModel.MPS())
	}
}

func TestCABACRangeLPSMatchesStandardVectors(t *testing.T) {
	rangeClasses := [...]uint32{256, 320, 384, 448}
	rangeLPS := [4][64]uint32{
		{128, 128, 128, 123, 116, 111, 105, 100, 95, 90, 85, 81, 77, 73, 69, 66, 62, 59, 56, 53, 51, 48, 46, 43, 41, 39, 37, 35, 33, 32, 30, 29, 27, 26, 24, 23, 22, 21, 20, 19, 18, 17, 16, 15, 14, 14, 13, 12, 12, 11, 11, 10, 10, 9, 9, 8, 8, 7, 7, 7, 6, 6, 6, 2},
		{176, 167, 158, 150, 142, 135, 128, 122, 116, 110, 104, 99, 94, 89, 85, 80, 76, 72, 69, 65, 62, 59, 56, 53, 50, 48, 45, 43, 41, 39, 37, 35, 33, 31, 30, 28, 27, 26, 24, 23, 22, 21, 20, 19, 18, 17, 16, 15, 14, 14, 13, 12, 12, 11, 11, 10, 9, 9, 9, 8, 8, 7, 7, 2},
		{208, 197, 187, 178, 169, 160, 152, 144, 137, 130, 123, 117, 111, 105, 100, 95, 90, 86, 81, 77, 73, 69, 66, 63, 59, 56, 54, 51, 48, 46, 43, 41, 39, 37, 35, 33, 32, 30, 29, 27, 26, 25, 23, 22, 21, 20, 19, 18, 17, 16, 15, 15, 14, 13, 12, 12, 11, 11, 10, 10, 9, 9, 8, 2},
		{240, 227, 216, 205, 195, 185, 175, 166, 158, 150, 142, 135, 128, 122, 116, 110, 104, 99, 94, 89, 85, 80, 76, 72, 69, 65, 62, 59, 56, 53, 50, 48, 45, 43, 41, 39, 37, 35, 33, 31, 30, 28, 27, 25, 24, 23, 22, 21, 20, 19, 18, 17, 16, 15, 14, 14, 13, 12, 12, 11, 11, 10, 9, 2},
	}
	for classIndex, codeRange := range rangeClasses {
		for contextState := uint8(0); contextState < 64; contextState++ {
			lpsRange := rangeLPS[classIndex][contextState]
			decoder := &CABACArithmeticDecoder{
				bits: NewBitReader([]byte{0x00, 0x00}), codeRange: codeRange, codeOffset: codeRange - lpsRange,
			}
			model := &CABACContextModel{stateIndex: contextState, valueMPS: false}
			decoded, err := decoder.DecodeBin(model)
			if err != nil || !decoded {
				t.Errorf("range class %d state %d decoded = %t, %v; want LPS true", classIndex, contextState, decoded, err)
				continue
			}
			normalizedRange, renormalizationBits := lpsRange, uint32(0)
			for normalizedRange < 256 {
				normalizedRange <<= 1
				renormalizationBits++
			}
			if decoder.CodeRange() != normalizedRange || decoder.CodeOffset() != 0 || decoder.bits.bitOffset != uint64(renormalizationBits) {
				t.Errorf("range class %d state %d result = range %d offset %d bits %d; want range %d offset 0 bits %d", classIndex, contextState, decoder.CodeRange(), decoder.CodeOffset(), decoder.bits.bitOffset, normalizedRange, renormalizationBits)
			}
			if model.MPS() != (contextState == 0) {
				t.Errorf("range class %d state %d LPS MPS flag = %t; want %t", classIndex, contextState, model.MPS(), contextState == 0)
			}
		}
	}
}

func TestCABACMBQPDeltaSyntax(t *testing.T) {
	if _, err := (&CABACArithmeticDecoder{}).DecodeMBQPDelta(nil, 0); !errors.Is(err, ErrCABACContextState) {
		t.Fatalf("nil context bank error = %v; want invalid context state", err)
	}
	newContexts := func() [4]CABACContextModel {
		return [4]CABACContextModel{
			{stateIndex: 0, valueMPS: false},
			{stateIndex: 0, valueMPS: false},
			{stateIndex: 0, valueMPS: true},
			{stateIndex: 0, valueMPS: true},
		}
	}
	for _, test := range []struct {
		name          string
		data          []byte
		codeOffset    uint32
		previousDelta int
		wantDelta     int
	}{
		{name: "zero", codeOffset: 0, wantDelta: 0},
		{name: "positive one", data: []byte{0x00}, codeOffset: 390, wantDelta: 1},
		{name: "negative one", data: []byte{0x00}, codeOffset: 330, wantDelta: -1},
		{name: "selects previous-delta context", codeOffset: 0, previousDelta: 1, wantDelta: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			decoder := &CABACArithmeticDecoder{bits: NewBitReader(test.data), codeRange: 510, codeOffset: test.codeOffset}
			contexts := newContexts()
			got, err := decoder.DecodeMBQPDelta(&contexts, test.previousDelta)
			if err != nil || got != test.wantDelta {
				t.Fatalf("DecodeMBQPDelta() = %d, %v; want %d, nil", got, err, test.wantDelta)
			}
			if test.name == "selects previous-delta context" && (contexts[0].StateIndex() != 0 || contexts[0].MPS() || contexts[1].StateIndex() != 1) {
				t.Fatalf("wrong first-bin context updated: context60=(%d,%t), context61=(%d,%t)", contexts[0].StateIndex(), contexts[0].MPS(), contexts[1].StateIndex(), contexts[1].MPS())
			}
		})
	}
}

func TestCABACMBQPDeltaTruncationIsTransactional(t *testing.T) {
	decoder := &CABACArithmeticDecoder{bits: NewBitReader(nil), codeRange: 510, codeOffset: 390}
	contexts := [4]CABACContextModel{
		{stateIndex: 0, valueMPS: false},
		{stateIndex: 0, valueMPS: false},
		{stateIndex: 0, valueMPS: true},
		{stateIndex: 0, valueMPS: true},
	}
	if _, err := decoder.DecodeMBQPDelta(&contexts, 0); !errors.Is(err, ErrBitstreamExhausted) {
		t.Fatalf("DecodeMBQPDelta() error = %v; want truncated bitstream", err)
	}
	if decoder.CodeRange() != 510 || decoder.CodeOffset() != 390 || decoder.bits.bitOffset != 0 {
		t.Fatalf("truncated mb_qp_delta changed decoder state")
	}
	if contexts[0].StateIndex() != 0 || contexts[0].MPS() || contexts[2].StateIndex() != 0 || !contexts[2].MPS() {
		t.Fatalf("truncated mb_qp_delta changed context state")
	}
}

func TestCABACMBSkipFlagContextDerivation(t *testing.T) {
	for _, test := range []struct {
		name                       string
		sliceType                  uint8
		leftAvailable, leftSkipped bool
		topAvailable, topSkipped   bool
		wantContext                int
	}{
		{name: "no available neighbors", sliceType: 0, wantContext: 0},
		{name: "only left contributes", sliceType: 5, leftAvailable: true, topAvailable: true, topSkipped: true, wantContext: 1},
		{name: "both contribute in B slice", sliceType: 6, leftAvailable: true, topAvailable: true, wantContext: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			decoder := &CABACArithmeticDecoder{bits: NewBitReader(nil), codeRange: 510, codeOffset: 0}
			contexts := [3]CABACContextModel{
				{stateIndex: 0, valueMPS: true},
				{stateIndex: 0, valueMPS: true},
				{stateIndex: 0, valueMPS: true},
			}
			contexts[test.wantContext].valueMPS = false
			decoded, err := decoder.DecodeMBSkipFlag(test.sliceType, &contexts, test.leftAvailable, test.leftSkipped, test.topAvailable, test.topSkipped)
			if err != nil || decoded {
				t.Fatalf("DecodeMBSkipFlag() = %t, %v; want false, nil", decoded, err)
			}
			for index, model := range contexts {
				wantState := uint8(0)
				if index == test.wantContext {
					wantState = 1
				}
				if model.StateIndex() != wantState {
					t.Errorf("context %d state = %d; want %d", index, model.StateIndex(), wantState)
				}
			}
		})
	}
}

func TestCABACReferenceIndexTruncatedUnary(t *testing.T) {
	for _, test := range []struct {
		name    string
		maximum uint32
		inc     uint8
		mps     [6]bool
		want    uint8
	}{
		{name: "inferred zero", maximum: 0, want: 0},
		{name: "zero", maximum: 3, inc: 2, want: 0},
		{name: "one", maximum: 3, inc: 1, mps: [6]bool{false, true, false, false, false, false}, want: 1},
		{name: "two", maximum: 3, inc: 0, mps: [6]bool{true, false, false, false, true, false}, want: 2},
		{name: "maximum", maximum: 3, inc: 3, mps: [6]bool{false, false, false, true, true, true}, want: 3},
	} {
		t.Run(test.name, func(t *testing.T) {
			decoder := &CABACArithmeticDecoder{bits: NewBitReader(nil), codeRange: 510}
			var contexts [6]CABACContextModel
			for index := range contexts {
				contexts[index] = CABACContextModel{stateIndex: 63, valueMPS: test.mps[index]}
			}
			got, err := decoder.DecodeReferenceIndex(test.maximum, test.inc, &contexts)
			if err != nil || got != test.want {
				t.Fatalf("ref_idx = %d, %v; want %d", got, err, test.want)
			}
		})
	}
	decoder := &CABACArithmeticDecoder{bits: NewBitReader(nil), codeRange: 510}
	contexts := [6]CABACContextModel{}
	if _, err := decoder.DecodeReferenceIndex(3, 4, &contexts); !errors.Is(err, ErrCABACContextState) {
		t.Fatalf("invalid neighbor context increment error = %v; want context-state error", err)
	}
	if _, err := decoder.DecodeReferenceIndex(32, 0, &contexts); !errors.Is(err, ErrCABACUnsupportedSyntax) {
		t.Fatalf("invalid maximum ref_idx error = %v; want unsupported syntax", err)
	}
}

func TestCABACInterReferenceIndexContextIncrement(t *testing.T) {
	neighbor := CABACInterNeighbor{Available: true, PredictionModeMatches: true, ReferenceIndex: 1}
	if got := DeriveCABACReferenceIndexContextIncrement(neighbor, CABACInterNeighbor{}, false, false); got != 1 {
		t.Fatalf("left-only ref_idx increment = %d; want 1", got)
	}
	if got := DeriveCABACReferenceIndexContextIncrement(CABACInterNeighbor{}, neighbor, false, false); got != 2 {
		t.Fatalf("top-only ref_idx increment = %d; want 2", got)
	}
	if got := DeriveCABACReferenceIndexContextIncrement(neighbor, neighbor, false, false); got != 3 {
		t.Fatalf("left/top ref_idx increment = %d; want 3", got)
	}
	for _, blocked := range []CABACInterNeighbor{
		{},
		{Available: true, Skip: true, PredictionModeMatches: true, ReferenceIndex: 1},
		{Available: true, Intra: true, PredictionModeMatches: true, ReferenceIndex: 1},
		{Available: true, PredictionModeMatches: false, ReferenceIndex: 1},
		{Available: true, PredictionModeMatches: true, ReferenceIndex: 0},
	} {
		if got := DeriveCABACReferenceIndexContextIncrement(blocked, CABACInterNeighbor{}, false, false); got != 0 {
			t.Errorf("blocked ref_idx neighbor %+v increment = %d; want 0", blocked, got)
		}
	}
	fieldNeighbor := neighbor
	fieldNeighbor.IsField = true
	if got := DeriveCABACReferenceIndexContextIncrement(fieldNeighbor, CABACInterNeighbor{}, true, false); got != 0 {
		t.Errorf("frame-to-field ref_idx 1 increment = %d; want zero because the field threshold is >1", got)
	}
	fieldNeighbor.ReferenceIndex = 2
	if got := DeriveCABACReferenceIndexContextIncrement(fieldNeighbor, CABACInterNeighbor{}, true, false); got != 1 {
		t.Errorf("frame-to-field ref_idx 2 increment = %d; want 1", got)
	}
	decoder, _ := NewCABACArithmeticDecoder(make([]byte, 8))
	var contexts [6]CABACContextModel
	for index := range contexts {
		contexts[index].stateIndex = uint8(index)
	}
	if _, err := decoder.DecodeReferenceIndexForPartition(1, neighbor, neighbor, false, false, &contexts); err != nil {
		t.Fatal(err)
	}
	for index, model := range contexts {
		want := uint8(index)
		if index == 3 {
			want++
		}
		if model.stateIndex != want {
			t.Errorf("partition ref_idx context %d state = %d; want %d", index, model.stateIndex, want)
		}
	}
}

func TestCABACInterMVDContextIncrement(t *testing.T) {
	neighbor := func(x, y int32) CABACInterNeighbor {
		return CABACInterNeighbor{Available: true, PredictionModeMatches: true, MotionVectorDifference: [2]int32{x, y}}
	}
	for _, test := range []struct {
		name         string
		left, top    CABACInterNeighbor
		component    uint8
		mbaff, field bool
		want         uint8
	}{
		{name: "sum at two", left: neighbor(1, 1), top: neighbor(1, 1), component: 0, want: 0},
		{name: "sum over two", left: neighbor(2, 2), top: neighbor(1, 1), component: 0, want: 1},
		{name: "sum over 32", left: neighbor(20, 20), top: neighbor(13, 13), component: 0, want: 2},
		{name: "one exceeds 32", left: neighbor(33, 33), component: 0, want: 2},
		{name: "unavailable ignored", left: CABACInterNeighbor{MotionVectorDifference: [2]int32{90, 90}}, component: 0, want: 0},
		{name: "skip ignored", left: CABACInterNeighbor{Available: true, Skip: true, MotionVectorDifference: [2]int32{90, 90}}, component: 0, want: 0},
		{name: "intra ignored", left: CABACInterNeighbor{Available: true, Intra: true, MotionVectorDifference: [2]int32{90, 90}}, component: 0, want: 0},
		{name: "other prediction ignored", left: CABACInterNeighbor{Available: true, MotionVectorDifference: [2]int32{90, 90}}, component: 0, want: 0},
		{name: "frame current field neighbor doubles vertical", left: CABACInterNeighbor{Available: true, PredictionModeMatches: true, IsField: true, MotionVectorDifference: [2]int32{0, 2}}, component: 1, mbaff: true, want: 1},
		{name: "field current frame neighbor halves vertical", left: neighbor(0, 7), component: 1, mbaff: true, field: true, want: 1},
		{name: "horizontal is not field scaled", left: CABACInterNeighbor{Available: true, PredictionModeMatches: true, IsField: true, MotionVectorDifference: [2]int32{2, 0}}, component: 0, mbaff: true, want: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := DeriveCABACMotionVectorDifferenceContextIncrement(test.left, test.top, test.component, test.mbaff, test.field)
			if err != nil || got != test.want {
				t.Fatalf("MVD context increment = %d, %v; want %d", got, err, test.want)
			}
		})
	}
	if _, err := DeriveCABACMotionVectorDifferenceContextIncrement(CABACInterNeighbor{}, CABACInterNeighbor{}, 2, false, false); !errors.Is(err, ErrCABACContextState) {
		t.Fatalf("invalid MVD component error = %v; want context-state error", err)
	}
	minimum := CABACInterNeighbor{Available: true, PredictionModeMatches: true, MotionVectorDifference: [2]int32{-1 << 31, 0}}
	if got, err := DeriveCABACMotionVectorDifferenceContextIncrement(minimum, CABACInterNeighbor{}, 0, false, false); err != nil || got != 2 {
		t.Fatalf("minimum int32 MVD increment = %d, %v; want 2", got, err)
	}
	decoder, _ := NewCABACArithmeticDecoder(make([]byte, 8))
	var contexts [7]CABACContextModel
	for index := range contexts {
		contexts[index].stateIndex = uint8(index)
	}
	if _, err := decoder.DecodeMotionVectorDifferenceForPartition(0, neighbor(2, 0), neighbor(1, 0), false, false, &contexts); err != nil {
		t.Fatal(err)
	}
	for index, model := range contexts {
		want := uint8(index)
		if index == 1 {
			want++
		}
		if model.stateIndex != want {
			t.Errorf("partition MVD context %d state = %d; want %d", index, model.stateIndex, want)
		}
	}
}

func TestCABACReferenceIndexValuesAndMaximumTruncation(t *testing.T) {
	for _, test := range []struct {
		name    string
		maximum uint32
		inc     uint8
		mps     [6]bool
		want    uint8
	}{
		{name: "inferred zero", maximum: 0, want: 0},
		{name: "zero", maximum: 3, inc: 2, want: 0},
		{name: "one", maximum: 3, inc: 1, mps: [6]bool{false, true, false, false, false, false}, want: 1},
		{name: "two", maximum: 3, inc: 0, mps: [6]bool{true, false, false, false, true, false}, want: 2},
		{name: "maximum", maximum: 3, inc: 3, mps: [6]bool{false, false, false, true, true, true}, want: 3},
	} {
		t.Run(test.name, func(t *testing.T) {
			decoder := &CABACArithmeticDecoder{bits: NewBitReader(nil), codeRange: 510}
			var contexts [6]CABACContextModel
			for index := range contexts {
				contexts[index] = CABACContextModel{stateIndex: 63, valueMPS: test.mps[index]}
			}
			got, err := decoder.DecodeReferenceIndex(test.maximum, test.inc, &contexts)
			if err != nil || got != test.want {
				t.Fatalf("ref_idx = %d, %v; want %d", got, err, test.want)
			}
		})
	}
}

func TestCABACMBSkipFlagRejectsOtherSliceTypes(t *testing.T) {
	decoder := &CABACArithmeticDecoder{bits: NewBitReader(nil), codeRange: 510, codeOffset: 0}
	contexts := [3]CABACContextModel{}
	for _, sliceType := range []uint8{2, 3, 4, 7, 8, 9, 10} {
		if _, err := decoder.DecodeMBSkipFlag(sliceType, &contexts, false, false, false, false); !errors.Is(err, ErrCABACUnsupportedSyntax) {
			t.Errorf("slice_type %d error = %v; want unsupported syntax", sliceType, err)
		}
	}
}

func TestCABACIIntraMBTypeBranches(t *testing.T) {
	contexts := func() [8]CABACContextModel {
		return [8]CABACContextModel{
			{stateIndex: 0, valueMPS: false},
			{stateIndex: 0, valueMPS: false},
			{stateIndex: 0, valueMPS: false},
			{stateIndex: 0, valueMPS: false},
			{stateIndex: 0, valueMPS: false},
			{stateIndex: 0, valueMPS: false},
			{stateIndex: 0, valueMPS: false},
			{stateIndex: 0, valueMPS: false},
		}
	}
	t.Run("intra NxN", func(t *testing.T) {
		decoder := &CABACArithmeticDecoder{bits: NewBitReader(nil), codeRange: 510}
		models := contexts()
		got, err := decoder.DecodeIIntraMBType(2, &models, false, false, false, false)
		if err != nil || got != 0 {
			t.Fatalf("DecodeIIntraMBType() = %d, %v; want 0, nil", got, err)
		}
	})
	t.Run("I16x16", func(t *testing.T) {
		decoder := &CABACArithmeticDecoder{bits: NewBitReader([]byte{0x00}), codeRange: 510}
		models := contexts()
		models[0].valueMPS = true
		got, err := decoder.DecodeIIntraMBType(7, &models, false, false, false, false)
		if err != nil || got != 1 {
			t.Fatalf("DecodeIIntraMBType() = %d, %v; want 1, nil", got, err)
		}
	})
	t.Run("I PCM", func(t *testing.T) {
		decoder := &CABACArithmeticDecoder{bits: NewBitReader([]byte{0x80}), codeRange: 510, codeOffset: 509}
		models := contexts()
		got, err := decoder.DecodeIIntraMBType(2, &models, false, false, false, false)
		if err != nil || got != 25 || !decoder.terminated {
			t.Fatalf("DecodeIIntraMBType() = %d, %v; terminated=%t; want 25, nil, true", got, err, decoder.terminated)
		}
	})
}

func TestCABACDecodeIPCMIntraMacroblockPlacesSamplesAndRestarts(t *testing.T) {
	data := make([]byte, 1+256+64+64+2)
	data[0] = 0x80
	for index := 1; index <= 256; index++ {
		data[index] = 0x11
	}
	for index := 257; index <= 320; index++ {
		data[index] = 0x22
	}
	for index := 321; index <= 384; index++ {
		data[index] = 0x33
	}
	decoder := &CABACArithmeticDecoder{bits: NewBitReader(data), codeRange: 510, codeOffset: 509}
	contexts := [8]CABACContextModel{}
	builder, err := NewYuv420FrameBuilder(1, 1, 0, 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := decoder.DecodeIPCMIntraMacroblock(2, &contexts, false, false, false, false, builder, 0); err != nil {
		t.Fatal(err)
	}
	if decoder.codeRange != 510 || decoder.codeOffset != 0 || decoder.terminated || decoder.bits.bitOffset != 8+384*8+9 {
		t.Fatalf("CABAC state after I_PCM = range %d offset %d terminated=%t bit=%d", decoder.codeRange, decoder.codeOffset, decoder.terminated, decoder.bits.bitOffset)
	}
	if contexts[0].stateIndex != 0 || !contexts[0].valueMPS {
		t.Fatalf("I_PCM mb_type context = %+v; expected the decoded first bin update", contexts[0])
	}
	frame, err := builder.Finish()
	if err != nil {
		t.Fatal(err)
	}
	if len(frame.Y) != 256 || len(frame.U) != 64 || len(frame.V) != 64 || frame.Y[0] != 0x11 || frame.Y[255] != 0x11 || frame.U[0] != 0x22 || frame.U[63] != 0x22 || frame.V[0] != 0x33 || frame.V[63] != 0x33 {
		t.Fatalf("I_PCM assembled planes have unexpected values or lengths")
	}
}

func TestCABACDecodeIPCMIntraMacroblockFailuresAreTransactional(t *testing.T) {
	for _, test := range []struct {
		name string
		data []byte
	}{
		{name: "truncated samples", data: []byte{0x80, 0x11}},
		{name: "nonzero alignment", data: []byte{0xc0}},
		{name: "invalid restart offset", data: append(append([]byte{0x80}, bytes.Repeat([]byte{0x11}, 384)...), 0xff, 0x00)},
	} {
		t.Run(test.name, func(t *testing.T) {
			decoder := &CABACArithmeticDecoder{bits: NewBitReader(test.data), codeRange: 510, codeOffset: 509}
			contexts := [8]CABACContextModel{}
			originalContexts := contexts
			originalRange, originalOffset, originalBitOffset := decoder.codeRange, decoder.codeOffset, decoder.bits.bitOffset
			builder, err := NewYuv420FrameBuilder(1, 1, 0, 0, 0, 0)
			if err != nil {
				t.Fatal(err)
			}
			if err := decoder.DecodeIPCMIntraMacroblock(2, &contexts, false, false, false, false, builder, 0); err == nil {
				t.Fatal("DecodeIPCMIntraMacroblock() unexpectedly succeeded")
			}
			if decoder.codeRange != originalRange || decoder.codeOffset != originalOffset || decoder.bits.bitOffset != originalBitOffset || contexts != originalContexts || builder.written[0] {
				t.Fatal("failed I_PCM decode mutated decoder, contexts, or builder")
			}
		})
	}
}

func TestCABACDecodeIntraNxN4x4LumaMacroblockModesAndAssembly(t *testing.T) {
	decoder := &CABACArithmeticDecoder{bits: NewBitReader(make([]byte, 64)), codeRange: 510}
	var contexts [CABACContextCount]CABACContextModel
	for index := range contexts {
		contexts[index] = CABACContextModel{stateIndex: 63}
	}
	blocks := [16]LumaIntra4x4Block{}
	for index := range blocks {
		blocks[index].TopAvailable = true
		blocks[index].LeftAvailable = true
		blocks[index].TopLeftAvailable = true
	}
	var modes [4]uint8
	for index := range modes {
		modes[index] = 2
	}
	result, err := decoder.DecodeIntraNxN4x4LumaMacroblock(&contexts, 0, 0, [16]uint8{16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16}, modes, modes, true, true, CABACIntra4x4EdgeState{}, CABACIntra4x4EdgeState{}, blocks)
	if err != nil {
		t.Fatal(err)
	}
	if result.Modes[0] != 0 || result.CodedBlockFlags != [16]bool{} || result.Samples != [256]uint8{} {
		t.Fatalf("decoded I_NxN luma = modes %v flags %v samples[0]=%d; want vertical/DC-derived modes, no residuals, zero samples", result.Modes, result.CodedBlockFlags, result.Samples[0])
	}
}

func TestCABACDecodeIntraNxN4x4LumaMacroblockResidualFlags(t *testing.T) {
	decoder := &CABACArithmeticDecoder{bits: NewBitReader(make([]byte, 128)), codeRange: 510}
	var contexts [CABACContextCount]CABACContextModel
	for index := range contexts {
		contexts[index] = CABACContextModel{stateIndex: 63}
	}
	contexts[88].valueMPS = true
	contexts[105].valueMPS = true
	contexts[166].valueMPS = true
	blocks := [16]LumaIntra4x4Block{}
	for index := range blocks {
		blocks[index].TopAvailable = true
		blocks[index].LeftAvailable = true
		blocks[index].TopLeftAvailable = true
		blocks[index].TopLeft = 100
		for sample := range blocks[index].Top {
			blocks[index].Top[sample] = 100
			blocks[index].Left[sample] = 100
		}
	}
	var modes [4]uint8
	for index := range modes {
		modes[index] = 2
	}
	result, err := decoder.DecodeIntraNxN4x4LumaMacroblock(&contexts, 1, 51, [16]uint8{16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16}, modes, modes, true, true, CABACIntra4x4EdgeState{}, CABACIntra4x4EdgeState{}, blocks)
	if err != nil {
		t.Fatal(err)
	}
	if !result.CodedBlockFlags[0] {
		t.Fatal("first CBP-selected luma block has coded_block_flag=false")
	}
	residualNonzero := false
	for _, sample := range result.Residuals[0] {
		residualNonzero = residualNonzero || sample != 0
	}
	if !residualNonzero {
		t.Fatal("decoded coded block produced no reconstructed residual")
	}
}

func TestReconstructIntra16x16LumaDCHadamardAndScaling(t *testing.T) {
	var levels [16]int32
	levels[0] = 1
	scalingList := [16]uint8{16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16}
	scaled, err := reconstructIntra16x16LumaDC(levels, scalingList, 51)
	if err != nil {
		t.Fatal(err)
	}
	for index, value := range scaled {
		if value != 896 {
			t.Fatalf("scaled I16x16 DC[%d] = %d; want 896", index, value)
		}
	}
	if _, err := reconstructIntra16x16LumaDC(levels, [16]uint8{}, 0); !errors.Is(err, ErrInverseScaleListZero) {
		t.Fatalf("zero scaling list error = %v; want scaling-list error", err)
	}
}

func TestCABACDecodeIntra16x16LumaDCPredictAndReconstruct(t *testing.T) {
	decoder := &CABACArithmeticDecoder{bits: NewBitReader(make([]byte, 64)), codeRange: 510}
	var contexts [CABACContextCount]CABACContextModel
	for index := range contexts {
		contexts[index] = CABACContextModel{stateIndex: 63}
	}
	contexts[96].valueMPS = true
	contexts[134].valueMPS = true
	contexts[195].valueMPS = true
	top := [16]uint8{100, 100, 100, 100, 100, 100, 100, 100, 100, 100, 100, 100, 100, 100, 100, 100}
	scalingList := [16]uint8{16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16}
	result, err := decoder.DecodeIntra16x16LumaMacroblock(1, &contexts, 51, scalingList, &top, nil, 0, false, CABACIntra16x16EdgeState{}, CABACIntra16x16EdgeState{})
	if err != nil {
		t.Fatal(err)
	}
	if result.PredictionMode != 0 || result.CodedBlockPatternLuma != 0 || !result.DCCoded {
		t.Fatalf("I16x16 mode/CBP/DC = %d/%d/%t", result.PredictionMode, result.CodedBlockPatternLuma, result.DCCoded)
	}
	if !anyNonzeroInt64(result.Residual[:]) || result.Samples[0] == 100 {
		t.Fatalf("I16x16 DC did not affect luma: residual[0]=%d sample[0]=%d", result.Residual[0], result.Samples[0])
	}
}

func TestCABACDecodeIntra16x16LumaACResiduals(t *testing.T) {
	decoder := &CABACArithmeticDecoder{bits: NewBitReader(make([]byte, 128)), codeRange: 510}
	var contexts [CABACContextCount]CABACContextModel
	for index := range contexts {
		contexts[index] = CABACContextModel{stateIndex: 63}
	}
	for _, index := range []int{92, 96, 120, 134, 166, 181} {
		contexts[index].valueMPS = true
	}
	top := [16]uint8{100, 100, 100, 100, 100, 100, 100, 100, 100, 100, 100, 100, 100, 100, 100, 100}
	scalingList := [16]uint8{16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16}
	result, err := decoder.DecodeIntra16x16LumaMacroblock(13, &contexts, 51, scalingList, &top, nil, 0, false, CABACIntra16x16EdgeState{}, CABACIntra16x16EdgeState{})
	if err != nil {
		t.Fatal(err)
	}
	for index, coded := range result.ACCodedBlockFlags {
		if !coded {
			t.Errorf("I16x16 AC block %d coded_block_flag=false", index)
		}
	}
	if !anyNonzeroInt64(result.Residual[:]) {
		t.Fatal("I16x16 AC residuals did not reconstruct into luma samples")
	}
}

func TestCABACDecodeIntraNxN4x4LumaMacroblockFailureIsTransactional(t *testing.T) {
	data := make([]byte, 64)
	decoder := &CABACArithmeticDecoder{bits: NewBitReader(data), codeRange: 510}
	var contexts [CABACContextCount]CABACContextModel
	for index := range contexts {
		contexts[index] = CABACContextModel{stateIndex: 63}
	}
	originalContexts := contexts
	blocks := [16]LumaIntra4x4Block{}
	originalRange, originalOffset, originalBit := decoder.codeRange, decoder.codeOffset, decoder.bits.bitOffset
	var modes [4]uint8
	if _, err := decoder.DecodeIntraNxN4x4LumaMacroblock(&contexts, 0, 0, [16]uint8{16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16}, modes, modes, false, false, CABACIntra4x4EdgeState{}, CABACIntra4x4EdgeState{}, blocks); !errors.Is(err, ErrLumaIntra4x4Macroblock) {
		t.Fatalf("missing reconstruction references error = %v; want intra macroblock error", err)
	}
	if contexts != originalContexts || decoder.codeRange != originalRange || decoder.codeOffset != originalOffset || decoder.bits.bitOffset != originalBit {
		t.Fatal("failed Intra_NxN macroblock changed decoder or contexts")
	}
}

func TestCABACDecodeIntraNxN8x8LumaMacroblockModesAndTransformFlag(t *testing.T) {
	decoder := &CABACArithmeticDecoder{bits: NewBitReader(make([]byte, 128)), codeRange: 510}
	var contexts [CABACContextCount]CABACContextModel
	for index := range contexts {
		contexts[index] = CABACContextModel{stateIndex: 63}
	}
	contexts[399].valueMPS = true
	blocks := [4]LumaIntra8x8Block{}
	for index := range blocks {
		blocks[index].TopAvailable = true
		blocks[index].LeftAvailable = true
		blocks[index].TopLeftAvailable = true
	}
	result, err := decoder.DecodeIntraNxN8x8LumaMacroblock(&contexts, true, false, false, 0, 0, [64]uint8{
		16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16,
		16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16,
		16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16,
		16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16,
	}, [2]uint8{2, 2}, [2]uint8{2, 2}, true, true, blocks)
	if err != nil {
		t.Fatal(err)
	}
	if !result.TransformSize8x8 || result.Modes[0] != 0 || result.Samples != [256]uint8{} || result.Residuals != [4][64]int64{} {
		t.Fatalf("I_NxN 8x8 result transform=%t modes=%v samples0=%d", result.TransformSize8x8, result.Modes, result.Samples[0])
	}
}

func TestCABACDecodeIntraNxN8x8LumaMacroblockResidualAndRollback(t *testing.T) {
	decoder := &CABACArithmeticDecoder{bits: NewBitReader(make([]byte, 128)), codeRange: 510}
	var contexts [CABACContextCount]CABACContextModel
	for index := range contexts {
		contexts[index] = CABACContextModel{stateIndex: 63}
	}
	contexts[399].valueMPS = true
	contexts[402].valueMPS = true
	contexts[417].valueMPS = true
	blocks := [4]LumaIntra8x8Block{}
	for index := range blocks {
		blocks[index].TopAvailable = true
		blocks[index].LeftAvailable = true
		blocks[index].TopLeftAvailable = true
		blocks[index].Top = [16]uint8{100, 100, 100, 100, 100, 100, 100, 100, 100, 100, 100, 100, 100, 100, 100, 100}
		blocks[index].Left = [16]uint8{100, 100, 100, 100, 100, 100, 100, 100, 100, 100, 100, 100, 100, 100, 100, 100}
	}
	result, err := decoder.DecodeIntraNxN8x8LumaMacroblock(&contexts, true, false, false, 1, 51, [64]uint8{
		16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16,
		16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16,
		16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16,
		16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16,
	}, [2]uint8{2, 2}, [2]uint8{2, 2}, true, true, blocks)
	if err != nil {
		t.Fatal(err)
	}
	if !result.TransformSize8x8 || !anyNonzeroInt64(result.Residuals[0][:]) {
		t.Fatal("category-5 residual was not reconstructed")
	}

	truncated := &CABACArithmeticDecoder{bits: NewBitReader(nil), codeRange: 510, codeOffset: 0}
	var truncatedContexts [CABACContextCount]CABACContextModel
	for index := range truncatedContexts {
		truncatedContexts[index] = CABACContextModel{stateIndex: 63}
	}
	truncatedContexts[399].valueMPS = true
	original := truncatedContexts
	if _, err := truncated.DecodeIntraNxN8x8LumaMacroblock(&truncatedContexts, true, false, false, 1, 0, [64]uint8{
		16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16,
		16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16,
		16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16,
		16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16,
	}, [2]uint8{2, 2}, [2]uint8{2, 2}, true, true, blocks); !errors.Is(err, ErrBitstreamExhausted) {
		t.Fatalf("truncated 8x8 residual error = %v; want exhausted bitstream", err)
	}
	if truncatedContexts != original || truncated.codeRange != 510 || truncated.codeOffset != 0 || truncated.bits.bitOffset != 0 {
		t.Fatal("failed 8x8 macroblock decode changed CABAC state")
	}
}

func anyNonzeroInt64(values []int64) bool {
	for _, value := range values {
		if value != 0 {
			return true
		}
	}
	return false
}

// cabacTestEncoder is the clause 9.3.4.2 arithmetic encoder, used only to build decoder vectors.
type cabacTestEncoder struct {
	low, codeRange uint32
	outstanding    int
	firstBit       bool
	bits           []bool
	flushed        bool
}

func newCABACTestEncoder() *cabacTestEncoder {
	return &cabacTestEncoder{codeRange: 510, firstBit: true}
}

func (encoder *cabacTestEncoder) putBit(bit bool) {
	if encoder.firstBit {
		encoder.firstBit = false
	} else {
		encoder.bits = append(encoder.bits, bit)
	}
	for ; encoder.outstanding > 0; encoder.outstanding-- {
		encoder.bits = append(encoder.bits, !bit)
	}
}

func (encoder *cabacTestEncoder) renormalize() {
	for encoder.codeRange < 256 {
		switch {
		case encoder.low < 256:
			encoder.putBit(false)
		case encoder.low >= 512:
			encoder.low -= 512
			encoder.putBit(true)
		default:
			encoder.low -= 256
			encoder.outstanding++
		}
		encoder.codeRange <<= 1
		encoder.low <<= 1
	}
}

func (encoder *cabacTestEncoder) decision(model *CABACContextModel, bin bool) {
	rangeLPS := uint32(cabacRangeLPS[(encoder.codeRange>>6)&3][model.stateIndex])
	encoder.codeRange -= rangeLPS
	if bin != model.valueMPS {
		encoder.low += encoder.codeRange
		encoder.codeRange = rangeLPS
	}
	model.Update(bin)
	encoder.renormalize()
}

func (encoder *cabacTestEncoder) terminate(bin bool) {
	encoder.codeRange -= 2
	if !bin {
		encoder.renormalize()
		return
	}
	encoder.low += encoder.codeRange
	encoder.codeRange = 2
	encoder.renormalize()
	encoder.putBit((encoder.low>>9)&1 == 1)
	encoder.bits = append(encoder.bits, (encoder.low>>8)&1 == 1, true)
	encoder.flushed = true
}

func (encoder *cabacTestEncoder) bytes() []byte {
	if !encoder.flushed {
		encoder.terminate(true)
	}
	data := make([]byte, (len(encoder.bits)+7)/8+4)
	for index, bit := range encoder.bits {
		if bit {
			data[index/8] |= 0x80 >> (index % 8)
		}
	}
	return data
}

// encodeBinString encodes a Table 9-36/9-37/9-38 bin string; ctxIdx maps (binIdx, prior bins) to ctxIdx, 276 terminating.
func (encoder *cabacTestEncoder) encodeBinString(contexts *[CABACContextCount]CABACContextModel, bins string, ctxIdx func(binIdx int, prior string) int) {
	for index := range bins {
		bin := bins[index] == '1'
		if context := ctxIdx(index, bins[:index]); context == 276 {
			encoder.terminate(bin)
		} else {
			encoder.decision(&contexts[context], bin)
		}
	}
}

// Table 9-36 bin strings for I mb_type 0-25.
var cabacTestIMBTypeBins = [26]string{
	"0", "100000", "100001", "100010", "100011", "1001000", "1001001", "1001010", "1001011",
	"1001100", "1001101", "1001110", "1001111", "101000", "101001", "101010", "101011",
	"1011000", "1011001", "1011010", "1011011", "1011100", "1011101", "1011110", "1011111", "11",
}

// Table 9-37 bin strings for B mb_type 0-22.
var cabacTestBMBTypeBins = [23]string{
	"0", "100", "101", "110000", "110001", "110010", "110011", "110100", "110101", "110110", "110111",
	"111110", "1110000", "1110001", "1110010", "1110011", "1110100", "1110101", "1110110", "1110111",
	"1111000", "1111001", "111111",
}

// Table 9-39/9-41 suffix contexts for intra mb_type in P (offset 17) and B (offset 32) slices.
func cabacTestIntraSuffixContext(offset int) func(int, string) int {
	return func(binIdx int, prior string) int {
		switch binIdx {
		case 0:
			return offset
		case 1:
			return 276
		case 2:
			return offset + 1
		case 3:
			return offset + 2
		case 4:
			if prior[3] != '0' {
				return offset + 2
			}
		}
		return offset + 3
	}
}

type cabacTestSymbol struct {
	value  uint8
	encode func(*cabacTestEncoder, *[CABACContextCount]CABACContextModel)
}

func cabacTestInterMBTypeSymbols(sliceType uint8, increment int) []cabacTestSymbol {
	type symbol = cabacTestSymbol
	var symbols []symbol
	prefixAndSuffix := func(value uint8, prefix string, prefixContext func(int, string) int, suffix string, suffixOffset int) symbol {
		return symbol{value, func(encoder *cabacTestEncoder, contexts *[CABACContextCount]CABACContextModel) {
			encoder.encodeBinString(contexts, prefix, prefixContext)
			if suffix != "" {
				encoder.encodeBinString(contexts, suffix, cabacTestIntraSuffixContext(suffixOffset))
			}
		}}
	}
	if sliceType%5 == 0 {
		pContext := func(binIdx int, prior string) int {
			if binIdx == 2 && prior[1] == '1' {
				return 17
			}
			return 14 + binIdx
		}
		for value, bins := range []string{"000", "011", "010", "001"} {
			symbols = append(symbols, prefixAndSuffix(uint8(value), bins, pContext, "", 0))
		}
		for intra, bins := range cabacTestIMBTypeBins {
			symbols = append(symbols, prefixAndSuffix(uint8(5+intra), "1", pContext, bins, 17))
		}
		return symbols
	}
	bContext := func(binIdx int, prior string) int {
		switch {
		case binIdx == 0:
			return 27 + increment
		case binIdx == 1:
			return 30
		case binIdx == 2 && prior[1] != '0':
			return 31
		}
		return 32
	}
	for value, bins := range cabacTestBMBTypeBins {
		symbols = append(symbols, prefixAndSuffix(uint8(value), bins, bContext, "", 0))
	}
	for intra, bins := range cabacTestIMBTypeBins {
		symbols = append(symbols, prefixAndSuffix(uint8(23+intra), "111101", bContext, bins, 32))
	}
	return symbols
}

func TestCABACDecodeInterMBTypeRoundTrip(t *testing.T) {
	for _, test := range []struct {
		sliceType             uint8
		leftAvailable         bool
		topAvailable          bool
		leftDirect, topDirect bool
		increment             int
	}{
		{sliceType: 0, leftAvailable: true, topAvailable: true},
		{sliceType: 5},
		{sliceType: 1},
		{sliceType: 6, leftAvailable: true, increment: 1},
		{sliceType: 1, leftAvailable: true, topAvailable: true, increment: 2},
		{sliceType: 1, leftAvailable: true, topAvailable: true, leftDirect: true, increment: 1},
	} {
		all := cabacTestInterMBTypeSymbols(test.sliceType, test.increment)
		// Every value is coded twice to exercise adaptation; I_PCM terminates the stream, so it is last.
		body := all[:len(all)-1]
		symbols := append(append(append([]cabacTestSymbol{}, body...), body...), all[len(all)-1])
		sliceContexts, err := NewCABACSliceContexts(test.sliceType, 1, 30)
		if err != nil {
			t.Fatal(err)
		}
		encoderContexts := sliceContexts
		encoder := newCABACTestEncoder()
		for _, symbol := range symbols {
			symbol.encode(encoder, &encoderContexts)
		}
		decoder, err := NewCABACArithmeticDecoder(encoder.bytes())
		if err != nil {
			t.Fatal(err)
		}
		decoderContexts := sliceContexts
		for _, symbol := range symbols {
			got, err := decoder.DecodeInterMBType(test.sliceType, &decoderContexts, test.leftAvailable, test.topAvailable, test.leftDirect, test.topDirect)
			if err != nil || got != symbol.value {
				t.Fatalf("slice %d mb_type = %d, %v; want %d", test.sliceType, got, err, symbol.value)
			}
		}
		if decoderContexts != encoderContexts || !decoder.terminated {
			t.Fatalf("slice %d decoder contexts/termination diverged from encoder", test.sliceType)
		}
	}
}

func TestCABACDecodeSubMBTypeRoundTrip(t *testing.T) {
	pContext := func(binIdx int, _ string) int { return 21 + binIdx }
	bContext := func(binIdx int, prior string) int {
		switch {
		case binIdx < 2:
			return 36 + binIdx
		case binIdx == 2 && prior[1] != '0':
			return 38
		}
		return 39
	}
	for _, test := range []struct {
		sliceType uint8
		bins      []string
		context   func(int, string) int
	}{
		{0, []string{"1", "00", "011", "010"}, pContext},
		{1, []string{"0", "100", "101", "11000", "11001", "11010", "11011", "111000", "111001", "111010", "111011", "11110", "11111"}, bContext},
	} {
		sliceContexts, _ := NewCABACSliceContexts(test.sliceType, 2, 33)
		encoderContexts := sliceContexts
		encoder := newCABACTestEncoder()
		for round := 0; round < 2; round++ {
			for _, bins := range test.bins {
				encoder.encodeBinString(&encoderContexts, bins, test.context)
			}
		}
		decoder, _ := NewCABACArithmeticDecoder(encoder.bytes())
		decoderContexts := sliceContexts
		for round := 0; round < 2; round++ {
			for want := range test.bins {
				got, err := decoder.DecodeSubMBType(test.sliceType, &decoderContexts)
				if err != nil || int(got) != want {
					t.Fatalf("slice %d sub_mb_type = %d, %v; want %d", test.sliceType, got, err, want)
				}
			}
		}
		if decoderContexts != encoderContexts {
			t.Fatalf("slice %d sub_mb_type contexts diverged from encoder", test.sliceType)
		}
	}
}

func TestCABACInterMBTypeAndSubMBTypeRejectInvalidInputTransactionally(t *testing.T) {
	contexts, _ := NewCABACSliceContexts(1, 0, 26)
	original := contexts
	decoder, err := NewCABACArithmeticDecoder([]byte{0x5a, 0x3c})
	if err != nil {
		t.Fatal(err)
	}
	for _, sliceType := range []uint8{2, 4, 10} {
		if _, err := decoder.DecodeInterMBType(sliceType, &contexts, false, false, false, false); !errors.Is(err, ErrCABACUnsupportedSyntax) {
			t.Errorf("slice %d mb_type error = %v; want unsupported", sliceType, err)
		}
		if _, err := decoder.DecodeSubMBType(sliceType, &contexts); !errors.Is(err, ErrCABACUnsupportedSyntax) {
			t.Errorf("slice %d sub_mb_type error = %v; want unsupported", sliceType, err)
		}
	}
	if _, err := decoder.DecodeInterMBType(1, nil, false, false, false, false); !errors.Is(err, ErrCABACContextState) {
		t.Fatalf("nil contexts error = %v; want context state", err)
	}
	startRange, startOffset := decoder.CodeRange(), decoder.CodeOffset()
	for {
		if _, err := decoder.DecodeInterMBType(1, &contexts, false, false, false, false); err != nil {
			break
		}
		startRange, startOffset, original = decoder.CodeRange(), decoder.CodeOffset(), contexts
	}
	if contexts != original || decoder.CodeRange() != startRange || decoder.CodeOffset() != startOffset {
		t.Fatal("failed mb_type decode mutated decoder or contexts")
	}
}

func TestCABACIIntraMBTypeContextAndFailureBehavior(t *testing.T) {
	decoder := &CABACArithmeticDecoder{bits: NewBitReader(nil), codeRange: 510}
	contexts := [8]CABACContextModel{}
	contexts[0] = CABACContextModel{stateIndex: 0, valueMPS: true}
	contexts[2] = CABACContextModel{stateIndex: 0, valueMPS: false}
	got, err := decoder.DecodeIIntraMBType(2, &contexts, true, true, true, true)
	if err != nil || got != 0 || contexts[0].StateIndex() != 0 || contexts[2].StateIndex() != 1 {
		t.Fatalf("neighbor context selection = type %d err %v states (%d,%d); want 0, nil, (0,1)", got, err, contexts[0].StateIndex(), contexts[2].StateIndex())
	}

	truncated := &CABACArithmeticDecoder{bits: NewBitReader(nil), codeRange: 510}
	truncatedContexts := [8]CABACContextModel{}
	truncatedContexts[0].valueMPS = true
	if _, err := truncated.DecodeIIntraMBType(2, &truncatedContexts, false, false, false, false); !errors.Is(err, ErrBitstreamExhausted) {
		t.Fatalf("truncated I16x16 mb_type error = %v; want truncated bitstream", err)
	}
	if truncated.CodeRange() != 510 || truncated.CodeOffset() != 0 || truncated.bits.bitOffset != 0 || truncatedContexts[0].StateIndex() != 0 || !truncatedContexts[0].MPS() {
		t.Fatalf("failed mb_type decode changed decoder/context state")
	}
	if _, err := truncated.DecodeIIntraMBType(0, &truncatedContexts, false, false, false, false); !errors.Is(err, ErrCABACUnsupportedSyntax) {
		t.Fatalf("P-slice mb_type error = %v; want unsupported syntax", err)
	}
}

func TestCABACIntraChromaPredModeValues(t *testing.T) {
	for _, test := range []struct {
		mode       uint8
		offset     uint32
		firstMPS   bool
		contextMPS bool
	}{
		{mode: 0, firstMPS: false},
		{mode: 1, firstMPS: true, contextMPS: false},
		{mode: 2, offset: 100, firstMPS: true, contextMPS: true},
		{mode: 3, firstMPS: true, contextMPS: true},
	} {
		decoder := &CABACArithmeticDecoder{bits: NewBitReader([]byte{0x00}), codeRange: 510, codeOffset: test.offset}
		contexts := [4]CABACContextModel{
			{stateIndex: 0, valueMPS: test.firstMPS},
			{stateIndex: 0, valueMPS: false},
			{stateIndex: 0, valueMPS: false},
			{stateIndex: 0, valueMPS: test.contextMPS},
		}
		got, err := decoder.DecodeIntraChromaPredMode(&contexts, false, false)
		if err != nil || got != test.mode {
			t.Errorf("mode vector %d: DecodeIntraChromaPredMode() = %d, %v", test.mode, got, err)
		}
	}
}

func TestCABACIntraChromaPredModeContextAndRollback(t *testing.T) {
	for contextIndex := 0; contextIndex < 3; contextIndex++ {
		decoder := &CABACArithmeticDecoder{bits: NewBitReader(nil), codeRange: 510}
		contexts := [4]CABACContextModel{
			{stateIndex: 0, valueMPS: true},
			{stateIndex: 0, valueMPS: true},
			{stateIndex: 0, valueMPS: true},
			{stateIndex: 0, valueMPS: false},
		}
		contexts[contextIndex].valueMPS = false
		mode, err := decoder.DecodeIntraChromaPredMode(&contexts, contextIndex > 0, contextIndex == 2)
		if err != nil || mode != 0 || contexts[contextIndex].StateIndex() != 1 {
			t.Errorf("context increment %d decoded mode=%d err=%v selected state=%d", contextIndex, mode, err, contexts[contextIndex].StateIndex())
		}
	}

	decoder := &CABACArithmeticDecoder{bits: NewBitReader(nil), codeRange: 510}
	contexts := [4]CABACContextModel{
		{stateIndex: 0, valueMPS: true},
		{stateIndex: 0, valueMPS: false},
		{stateIndex: 0, valueMPS: false},
		{stateIndex: 0, valueMPS: false},
	}
	if _, err := decoder.DecodeIntraChromaPredMode(&contexts, false, false); !errors.Is(err, ErrBitstreamExhausted) {
		t.Fatalf("truncated chroma mode error = %v; want truncated bitstream", err)
	}
	if decoder.CodeRange() != 510 || decoder.CodeOffset() != 0 || decoder.bits.bitOffset != 0 || !contexts[0].MPS() || contexts[0].StateIndex() != 0 {
		t.Fatalf("truncated chroma mode changed decoder/context state")
	}
}

func TestCABACIntra4x4PredMode(t *testing.T) {
	previous := &CABACArithmeticDecoder{bits: NewBitReader(nil), codeRange: 510}
	previousContexts := [2]CABACContextModel{
		{stateIndex: 0, valueMPS: true},
		{stateIndex: 0, valueMPS: false},
	}
	mode, err := previous.DecodeIntra4x4PredMode(6, &previousContexts)
	if err != nil || mode != 6 {
		t.Fatalf("prev_intra4x4_pred_mode_flag result = %d, %v; want 6, nil", mode, err)
	}

	remainder := &CABACArithmeticDecoder{bits: NewBitReader([]byte{0x00}), codeRange: 510}
	remainderContexts := [2]CABACContextModel{
		{stateIndex: 0, valueMPS: false},
		{stateIndex: 62, valueMPS: true},
	}
	mode, err = remainder.DecodeIntra4x4PredMode(7, &remainderContexts)
	if err != nil || mode != 8 {
		t.Fatalf("rem_intra4x4_pred_mode result = %d, %v; want 8, nil", mode, err)
	}
}

func TestDeriveIntra4x4PredictedModeUsesSyntaxScanNeighbors(t *testing.T) {
	var decoded [16]uint8
	decoded[0], decoded[1], decoded[2], decoded[3], decoded[4] = 7, 5, 4, 6, 3
	top := [4]uint8{3, 2, 1, 0}
	left := [4]uint8{5, 6, 7, 8}
	for _, test := range []struct {
		index                       int
		topAvailable, leftAvailable bool
		want                        uint8
	}{
		{index: 0, topAvailable: true, leftAvailable: true, want: 3},
		{index: 1, topAvailable: true, leftAvailable: true, want: 2},
		{index: 2, topAvailable: true, leftAvailable: true, want: 6},
		{index: 4, topAvailable: true, leftAvailable: true, want: 1},
		{index: 0, topAvailable: false, leftAvailable: true, want: 2},
	} {
		got, err := DeriveIntra4x4PredictedMode(decoded, test.index, top, left, test.topAvailable, test.leftAvailable)
		if err != nil || got != test.want {
			t.Errorf("predicted mode at syntax block %d = %d, %v; want %d", test.index, got, err, test.want)
		}
	}
	if _, err := DeriveIntra4x4PredictedMode(decoded, 16, top, left, true, true); !errors.Is(err, ErrCABACIntraPredNeighbors) {
		t.Fatalf("invalid block index error = %v; want neighbor-state error", err)
	}
}

func TestDecodeIntra4x4PredModesAndRollback(t *testing.T) {
	decoder, err := NewCABACArithmeticDecoder(make([]byte, 128))
	if err != nil {
		t.Fatal(err)
	}
	contexts := [2]CABACContextModel{
		{stateIndex: 0, valueMPS: false},
		{stateIndex: 0, valueMPS: false},
	}
	modes, err := decoder.DecodeIntra4x4PredModes(&contexts, [4]uint8{}, [4]uint8{}, false, false)
	if err != nil {
		t.Fatal(err)
	}
	for index, mode := range modes {
		if mode > 8 {
			t.Fatalf("decoded mode[%d] = %d; want mode in [0,8]", index, mode)
		}
	}

	truncated := &CABACArithmeticDecoder{bits: NewBitReader(nil), codeRange: 510}
	truncatedContexts := [2]CABACContextModel{
		{stateIndex: 0, valueMPS: false},
		{stateIndex: 0, valueMPS: false},
	}
	if _, err := truncated.DecodeIntra4x4PredModes(&truncatedContexts, [4]uint8{}, [4]uint8{}, false, false); !errors.Is(err, ErrBitstreamExhausted) {
		t.Fatalf("truncated macroblock mode error = %v; want truncated bitstream", err)
	}
	if truncated.CodeRange() != 510 || truncated.CodeOffset() != 0 || truncated.bits.bitOffset != 0 || truncatedContexts[0].StateIndex() != 0 || truncatedContexts[1].StateIndex() != 0 {
		t.Fatal("truncated Intra_4x4 modes changed decoder or contexts")
	}
}

func TestCABACIntra4x4PredModeRejectsInvalidAndRollsBack(t *testing.T) {
	decoder := &CABACArithmeticDecoder{bits: NewBitReader(nil), codeRange: 510}
	contexts := [2]CABACContextModel{
		{stateIndex: 0, valueMPS: false},
		{stateIndex: 0, valueMPS: false},
	}
	if _, err := decoder.DecodeIntra4x4PredMode(9, &contexts); !errors.Is(err, ErrCABACIntraPredModeRange) {
		t.Fatalf("invalid predicted mode error = %v; want out-of-range", err)
	}
	if _, err := decoder.DecodeIntra4x4PredMode(0, &contexts); !errors.Is(err, ErrBitstreamExhausted) {
		t.Fatalf("truncated rem mode error = %v; want truncated bitstream", err)
	}
	if decoder.CodeRange() != 510 || decoder.CodeOffset() != 0 || decoder.bits.bitOffset != 0 || contexts[0].StateIndex() != 0 || contexts[1].StateIndex() != 0 {
		t.Fatalf("failed intra4x4 mode changed decoder/context state")
	}
}

func TestCABACTransformSize8x8FlagContextDerivation(t *testing.T) {
	for contextIndex := 0; contextIndex < 3; contextIndex++ {
		decoder := &CABACArithmeticDecoder{bits: NewBitReader(nil), codeRange: 510}
		contexts := [3]CABACContextModel{
			{stateIndex: 0, valueMPS: true},
			{stateIndex: 0, valueMPS: true},
			{stateIndex: 0, valueMPS: true},
		}
		contexts[contextIndex].valueMPS = false
		decoded, err := decoder.DecodeTransformSize8x8Flag(&contexts, contextIndex > 0, contextIndex == 2)
		if err != nil || decoded || contexts[contextIndex].StateIndex() != 1 {
			t.Errorf("context increment %d decoded=%t err=%v selected state=%d", contextIndex, decoded, err, contexts[contextIndex].StateIndex())
		}
	}
	if _, err := (&CABACArithmeticDecoder{}).DecodeTransformSize8x8Flag(nil, false, false); !errors.Is(err, ErrCABACContextState) {
		t.Fatalf("nil transform context bank error = %v; want invalid context", err)
	}
}

func TestCABACLumaCodedBlockPatternValuesAndContexts(t *testing.T) {
	zeroDecoder := &CABACArithmeticDecoder{bits: NewBitReader([]byte{0x00}), codeRange: 510}
	zeroContexts := [4]CABACContextModel{}
	if got, err := zeroDecoder.DecodeLumaCodedBlockPattern(0, 0, &zeroContexts); err != nil || got != 0 {
		t.Fatalf("zero luma CBP = %d, %v; want 0, nil", got, err)
	}
	if zeroContexts[3].StateIndex() != 4 {
		t.Fatalf("four zero bins did not reuse context index 3: state=%d", zeroContexts[3].StateIndex())
	}

	patternDecoder := &CABACArithmeticDecoder{bits: NewBitReader([]byte{0x00}), codeRange: 510}
	patternContexts := [4]CABACContextModel{
		{stateIndex: 0, valueMPS: false},
		{stateIndex: 0, valueMPS: true},
		{stateIndex: 0, valueMPS: true},
		{stateIndex: 0, valueMPS: true},
	}
	if got, err := patternDecoder.DecodeLumaCodedBlockPattern(2, 4, &patternContexts); err != nil || got != 6 {
		t.Fatalf("neighbor-derived luma CBP = %d, %v; want 6, nil", got, err)
	}
	if patternContexts[0].StateIndex() != 2 || patternContexts[3].StateIndex() != 2 {
		t.Fatalf("luma CBP context reuse states = (%d,%d); want (2,2)", patternContexts[0].StateIndex(), patternContexts[3].StateIndex())
	}

	allCoded := &CABACArithmeticDecoder{bits: NewBitReader(nil), codeRange: 510}
	allContexts := [4]CABACContextModel{{stateIndex: 62, valueMPS: true}, {stateIndex: 62, valueMPS: true}, {stateIndex: 62, valueMPS: true}, {stateIndex: 62, valueMPS: true}}
	if got, err := allCoded.DecodeLumaCodedBlockPattern(15, 15, &allContexts); err != nil || got != 15 {
		t.Fatalf("all-coded luma CBP = %d, %v; want 15, nil", got, err)
	}
}

func TestCABACLumaCodedBlockPatternValidationAndRollback(t *testing.T) {
	decoder := &CABACArithmeticDecoder{bits: NewBitReader(nil), codeRange: 510}
	contexts := [4]CABACContextModel{}
	if _, err := decoder.DecodeLumaCodedBlockPattern(16, 0, &contexts); !errors.Is(err, ErrCABACCBPOutOfRange) {
		t.Fatalf("invalid left CBP error = %v; want out-of-range", err)
	}
	if _, err := decoder.DecodeLumaCodedBlockPattern(0, 0, nil); !errors.Is(err, ErrCABACContextState) {
		t.Fatalf("nil CBP contexts error = %v; want invalid context", err)
	}
	if _, err := decoder.DecodeLumaCodedBlockPattern(0, 0, &contexts); !errors.Is(err, ErrBitstreamExhausted) {
		t.Fatalf("truncated luma CBP error = %v; want truncated bitstream", err)
	}
	if decoder.CodeRange() != 510 || decoder.CodeOffset() != 0 || decoder.bits.bitOffset != 0 || contexts[3].StateIndex() != 0 {
		t.Fatalf("truncated luma CBP changed decoder/context state")
	}
}

func TestCABACChromaCodedBlockPatternValuesAndContexts(t *testing.T) {
	for _, test := range []struct {
		name      string
		left, top uint8
		firstMPS  bool
		secondMPS bool
		want      uint8
	}{
		{name: "zero", left: 0, top: 0, want: 0},
		{name: "one", left: 0, top: 0, firstMPS: true, want: 1},
		{name: "two", left: 0, top: 0, firstMPS: true, secondMPS: true, want: 2},
	} {
		data := []byte(nil)
		if test.want != 0 {
			data = []byte{0x00}
		}
		decoder := &CABACArithmeticDecoder{bits: NewBitReader(data), codeRange: 510}
		contexts := [8]CABACContextModel{}
		contexts[0].valueMPS = test.firstMPS
		contexts[4].valueMPS = test.secondMPS
		got, err := decoder.DecodeChromaCodedBlockPattern(test.left, test.top, &contexts)
		if err != nil || got != test.want {
			t.Errorf("%s chroma CBP = %d, %v; want %d, nil", test.name, got, err, test.want)
		}
	}

	neighborDecoder := &CABACArithmeticDecoder{bits: NewBitReader(nil), codeRange: 510}
	neighborContexts := [8]CABACContextModel{}
	neighborContexts[3].valueMPS = false
	for index := range neighborContexts {
		if index != 3 {
			neighborContexts[index].valueMPS = true
		}
	}
	if got, err := neighborDecoder.DecodeChromaCodedBlockPattern(1, 1, &neighborContexts); err != nil || got != 0 || neighborContexts[3].StateIndex() != 1 {
		t.Fatalf("first-bin chroma context derivation = (%d,%v) ctx3 state %d; want (0,nil),1", got, err, neighborContexts[3].StateIndex())
	}

	secondContextDecoder := &CABACArithmeticDecoder{bits: NewBitReader([]byte{0x00}), codeRange: 510}
	secondContextModels := [8]CABACContextModel{}
	secondContextModels[3].valueMPS = true
	secondContextModels[7].valueMPS = false
	for index := range secondContextModels {
		if index != 7 {
			secondContextModels[index].valueMPS = true
		}
	}
	if got, err := secondContextDecoder.DecodeChromaCodedBlockPattern(2, 2, &secondContextModels); err != nil || got != 1 || secondContextModels[7].StateIndex() != 1 {
		t.Fatalf("second-bin chroma context derivation = (%d,%v) ctx7 state %d; want (1,nil),1", got, err, secondContextModels[7].StateIndex())
	}
}

func TestCABACChromaCodedBlockPatternValidationAndRollback(t *testing.T) {
	decoder := &CABACArithmeticDecoder{bits: NewBitReader(nil), codeRange: 510}
	contexts := [8]CABACContextModel{}
	if _, err := decoder.DecodeChromaCodedBlockPattern(3, 0, &contexts); !errors.Is(err, ErrCABACChromaCBPOutOfRange) {
		t.Fatalf("invalid chroma CBP error = %v; want out-of-range", err)
	}
	contexts[0].valueMPS = true
	if _, err := decoder.DecodeChromaCodedBlockPattern(0, 0, &contexts); !errors.Is(err, ErrBitstreamExhausted) {
		t.Fatalf("truncated chroma CBP error = %v; want truncated bitstream", err)
	}
	if decoder.CodeRange() != 510 || decoder.CodeOffset() != 0 || decoder.bits.bitOffset != 0 || !contexts[0].MPS() || contexts[0].StateIndex() != 0 {
		t.Fatalf("truncated chroma CBP changed decoder/context state")
	}
}

func TestCABACLuma4x4CodedBlockFlagContextDerivation(t *testing.T) {
	for _, test := range []struct {
		left, top uint8
		context   int
	}{
		{left: 0, top: 0, context: 0},
		{left: 1, top: 0, context: 1},
		{left: 0, top: 16, context: 2},
		{left: 3, top: 7, context: 3},
	} {
		decoder := &CABACArithmeticDecoder{bits: NewBitReader(nil), codeRange: 510}
		contexts := [4]CABACContextModel{
			{stateIndex: 0, valueMPS: true},
			{stateIndex: 0, valueMPS: true},
			{stateIndex: 0, valueMPS: true},
			{stateIndex: 0, valueMPS: true},
		}
		contexts[test.context].valueMPS = false
		decoded, err := decoder.DecodeLuma4x4CodedBlockFlag(test.left, test.top, &contexts)
		if err != nil || decoded || contexts[test.context].StateIndex() != 1 {
			t.Errorf("nonzero counts (%d,%d) selected context %d: decoded=%t err=%v", test.left, test.top, test.context, decoded, err)
		}
	}
}

func TestCABACLuma4x4CodedBlockFlagValidationAndRollback(t *testing.T) {
	decoder := &CABACArithmeticDecoder{bits: NewBitReader(nil), codeRange: 510, codeOffset: 270}
	contexts := [4]CABACContextModel{}
	if _, err := decoder.DecodeLuma4x4CodedBlockFlag(17, 0, &contexts); !errors.Is(err, ErrCABACNonzeroCountRange) {
		t.Fatalf("invalid nonzero count error = %v; want out-of-range", err)
	}
	if _, err := decoder.DecodeLuma4x4CodedBlockFlag(0, 0, &contexts); !errors.Is(err, ErrBitstreamExhausted) {
		t.Fatalf("truncated CBF error = %v; want truncated bitstream", err)
	}
	if decoder.CodeRange() != 510 || decoder.CodeOffset() != 270 || decoder.bits.bitOffset != 0 || contexts[0].StateIndex() != 0 {
		t.Fatalf("truncated CBF changed decoder/context state")
	}
}

func TestCABACLuma4x4SignificanceMapImpliedFinalCoefficient(t *testing.T) {
	decoder := &CABACArithmeticDecoder{bits: NewBitReader(nil), codeRange: 510}
	significantContexts := [15]CABACContextModel{}
	lastContexts := [15]CABACContextModel{}
	for index := range significantContexts {
		significantContexts[index] = CABACContextModel{stateIndex: 62, valueMPS: false}
		lastContexts[index] = CABACContextModel{stateIndex: 62, valueMPS: false}
	}
	mapBits, err := decoder.DecodeLuma4x4SignificanceMap(&significantContexts, &lastContexts)
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 15; index++ {
		if mapBits[index] {
			t.Errorf("scan position %d is significant; want false", index)
		}
	}
	if !mapBits[15] {
		t.Fatal("final scan position is not implicitly significant")
	}
}

func TestCABACLuma4x4SignificanceMapStopsAtLastFlag(t *testing.T) {
	decoder := &CABACArithmeticDecoder{bits: NewBitReader(nil), codeRange: 510}
	significantContexts := [15]CABACContextModel{}
	lastContexts := [15]CABACContextModel{}
	for index := range significantContexts {
		significantContexts[index] = CABACContextModel{stateIndex: 62, valueMPS: false}
		lastContexts[index] = CABACContextModel{stateIndex: 62, valueMPS: false}
	}
	significantContexts[0].valueMPS = true
	significantContexts[1].valueMPS = true
	lastContexts[1].valueMPS = true
	mapBits, err := decoder.DecodeLuma4x4SignificanceMap(&significantContexts, &lastContexts)
	if err != nil {
		t.Fatal(err)
	}
	if !mapBits[0] || !mapBits[1] || mapBits[2] || mapBits[15] {
		t.Fatalf("significance map has wrong early-stop positions")
	}
}

func TestCABACLuma4x4SignificanceMapTruncationIsTransactional(t *testing.T) {
	decoder := &CABACArithmeticDecoder{bits: NewBitReader(nil), codeRange: 510, codeOffset: 382}
	significantContexts := [15]CABACContextModel{}
	lastContexts := [15]CABACContextModel{}
	for index := range significantContexts {
		significantContexts[index] = CABACContextModel{stateIndex: 62, valueMPS: false}
		lastContexts[index] = CABACContextModel{stateIndex: 62, valueMPS: false}
	}
	significantContexts[0] = CABACContextModel{stateIndex: 0, valueMPS: false}
	if _, err := decoder.DecodeLuma4x4SignificanceMap(&significantContexts, &lastContexts); !errors.Is(err, ErrBitstreamExhausted) {
		t.Fatalf("truncated significance map error = %v; want truncated bitstream", err)
	}
	if decoder.CodeRange() != 510 || decoder.CodeOffset() != 382 || decoder.bits.bitOffset != 0 || significantContexts[0].StateIndex() != 0 {
		t.Fatalf("truncated significance map changed decoder/context state")
	}
}

func TestCABACMotionVectorDifferenceValuesAndContextBands(t *testing.T) {
	for _, test := range []struct {
		neighborMagnitude uint64
		wantContext       int
	}{
		{neighborMagnitude: 0, wantContext: 0},
		{neighborMagnitude: 2, wantContext: 0},
		{neighborMagnitude: 3, wantContext: 1},
		{neighborMagnitude: 32, wantContext: 1},
		{neighborMagnitude: 33, wantContext: 2},
		{neighborMagnitude: 1 << 32, wantContext: 2},
	} {
		decoder := &CABACArithmeticDecoder{bits: NewBitReader(nil), codeRange: 510}
		var contexts [7]CABACContextModel
		for index := range contexts {
			contexts[index] = CABACContextModel{stateIndex: 0}
		}
		if got, err := decoder.DecodeMotionVectorDifference(test.neighborMagnitude, &contexts); err != nil || got != 0 {
			t.Fatalf("zero MVD with neighbor magnitude %d = %d, %v; want 0, nil", test.neighborMagnitude, got, err)
		}
		for index, context := range contexts {
			wantState := uint8(0)
			if index == test.wantContext {
				wantState = 1
			}
			if context.StateIndex() != wantState {
				t.Errorf("neighbor magnitude %d updated context %d to %d; want context %d only", test.neighborMagnitude, index, context.StateIndex(), test.wantContext)
			}
		}
	}

	for _, test := range []struct {
		name       string
		data       []byte
		codeOffset uint32
		contexts   [7]CABACContextModel
		want       int32
	}{
		{
			name: "unit positive",
			data: []byte{0},
			contexts: [7]CABACContextModel{
				{stateIndex: 62, valueMPS: true}, {}, {},
				{stateIndex: 62}, {}, {}, {},
			},
			want: 1,
		},
		{
			name:       "unit negative",
			data:       []byte{0},
			codeOffset: 253,
			contexts: [7]CABACContextModel{
				{stateIndex: 62, valueMPS: true}, {}, {},
				{stateIndex: 62}, {}, {}, {},
			},
			want: -1,
		},
		{
			name: "bypass escape boundary",
			data: []byte{0},
			contexts: [7]CABACContextModel{
				{stateIndex: 62, valueMPS: true}, {}, {},
				{stateIndex: 62, valueMPS: true},
				{stateIndex: 62, valueMPS: true},
				{stateIndex: 62, valueMPS: true},
				{stateIndex: 62, valueMPS: true},
			},
			want: 9,
		},
		{
			name:       "bypass suffix bit",
			data:       []byte{0x08},
			codeOffset: 31,
			contexts: [7]CABACContextModel{
				{stateIndex: 62, valueMPS: true},
				{stateIndex: 62, valueMPS: true},
				{stateIndex: 62, valueMPS: true},
				{stateIndex: 62, valueMPS: true},
				{stateIndex: 62, valueMPS: true},
				{stateIndex: 62, valueMPS: true},
				{stateIndex: 62, valueMPS: true},
			},
			want: 10,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			decoder := &CABACArithmeticDecoder{bits: NewBitReader(test.data), codeRange: 510, codeOffset: test.codeOffset}
			contexts := test.contexts
			got, err := decoder.DecodeMotionVectorDifference(0, &contexts)
			if err != nil || got != test.want {
				t.Fatalf("MVD = %d, %v; want %d, nil", got, err, test.want)
			}
		})
	}
}

func TestCABACMotionVectorDifferenceOverlongEscapeIsTransactional(t *testing.T) {
	decoder := &CABACArithmeticDecoder{bits: NewBitReader([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}), codeRange: 510, codeOffset: 491}
	var contexts [7]CABACContextModel
	for index := range contexts {
		contexts[index] = CABACContextModel{stateIndex: 63, valueMPS: true}
	}
	if _, err := decoder.DecodeMotionVectorDifference(0, &contexts); !errors.Is(err, ErrCABACMotionVectorDifferenceOutOfRange) {
		t.Fatalf("overlong MVD error = %v; want signed output range", err)
	}
	if decoder.CodeRange() != 510 || decoder.CodeOffset() != 491 || decoder.bits.bitOffset != 0 {
		t.Fatalf("overlong MVD changed decoder state")
	}
	for index, context := range contexts {
		if context.StateIndex() != 63 || !context.MPS() {
			t.Fatalf("overlong MVD changed context %d", index)
		}
	}
}

func TestCABACMotionVectorDifferenceTruncationIsTransactional(t *testing.T) {
	decoder := &CABACArithmeticDecoder{bits: NewBitReader(nil), codeRange: 510, codeOffset: 509}
	var contexts [7]CABACContextModel
	contexts[0] = CABACContextModel{stateIndex: 62, valueMPS: true}
	if _, err := decoder.DecodeMotionVectorDifference(0, &contexts); !errors.Is(err, ErrBitstreamExhausted) {
		t.Fatalf("truncated MVD error = %v; want truncated bitstream", err)
	}
	if decoder.CodeRange() != 510 || decoder.CodeOffset() != 509 || decoder.bits.bitOffset != 0 || contexts[0].StateIndex() != 62 || !contexts[0].MPS() {
		t.Fatalf("truncated MVD changed decoder or context state")
	}
}

func TestCABACCoeffAbsLevelMinus1Values(t *testing.T) {
	zeroDecoder := &CABACArithmeticDecoder{bits: NewBitReader(nil), codeRange: 510}
	zeroFirst, zeroGreater := &CABACContextModel{stateIndex: 62}, &CABACContextModel{stateIndex: 62}
	if got, err := zeroDecoder.DecodeCoeffAbsLevelMinus1(zeroFirst, zeroGreater); err != nil || got != 0 {
		t.Fatalf("zero coefficient level = %d, %v; want 0, nil", got, err)
	}

	unitDecoder := &CABACArithmeticDecoder{bits: NewBitReader(nil), codeRange: 510}
	unitFirst, unitGreater := &CABACContextModel{stateIndex: 62, valueMPS: true}, &CABACContextModel{stateIndex: 62}
	if got, err := unitDecoder.DecodeCoeffAbsLevelMinus1(unitFirst, unitGreater); err != nil || got != 1 {
		t.Fatalf("unit coefficient level = %d, %v; want 1, nil", got, err)
	}

	level15Decoder := &CABACArithmeticDecoder{bits: NewBitReader([]byte{0x00}), codeRange: 510}
	level15First, level15Greater := &CABACContextModel{stateIndex: 62, valueMPS: true}, &CABACContextModel{stateIndex: 62, valueMPS: true}
	if got, err := level15Decoder.DecodeCoeffAbsLevelMinus1(level15First, level15Greater); err != nil || got != 14 {
		t.Fatalf("escaped coefficient level = %d, %v; want 14, nil", got, err)
	}

	escapedDecoder := &CABACArithmeticDecoder{bits: NewBitReader([]byte{0x40}), codeRange: 510}
	escapedFirst, escapedGreater := &CABACContextModel{stateIndex: 62, valueMPS: true}, &CABACContextModel{stateIndex: 62, valueMPS: true}
	if got, err := escapedDecoder.DecodeCoeffAbsLevelMinus1(escapedFirst, escapedGreater); err != nil || got != 14 {
		t.Fatalf("escaped coefficient level = %d, %v; want 14, nil", got, err)
	}
}

func TestCABACCoeffAbsLevelMinus1TruncationIsTransactional(t *testing.T) {
	decoder := &CABACArithmeticDecoder{bits: NewBitReader(nil), codeRange: 510}
	first := &CABACContextModel{stateIndex: 62, valueMPS: true}
	greater := &CABACContextModel{stateIndex: 62, valueMPS: true}
	if _, err := decoder.DecodeCoeffAbsLevelMinus1(first, greater); !errors.Is(err, ErrBitstreamExhausted) {
		t.Fatalf("truncated coefficient level error = %v; want truncated bitstream", err)
	}
	if decoder.CodeRange() != 510 || decoder.CodeOffset() != 0 || decoder.bits.bitOffset != 0 || first.StateIndex() != 62 || greater.StateIndex() != 62 {
		t.Fatalf("truncated coefficient level changed decoder/context state")
	}
}

func TestCABACCoeffAbsLevelMinus1RejectsOverlongEscapePrefix(t *testing.T) {
	decoder := &CABACArithmeticDecoder{bits: NewBitReader([]byte{0xff, 0xff, 0xff, 0xff}), codeRange: 510, codeOffset: 481}
	first := &CABACContextModel{stateIndex: 63, valueMPS: true}
	greater := &CABACContextModel{stateIndex: 63, valueMPS: true}
	if _, err := decoder.DecodeCoeffAbsLevelMinus1(first, greater); !errors.Is(err, ErrCABACCoeffLevelOutOfRange) {
		t.Fatalf("overlong coefficient prefix error = %v; want unsupported level range", err)
	}
	if decoder.CodeRange() != 510 || decoder.CodeOffset() != 481 || decoder.bits.bitOffset != 0 {
		t.Fatalf("overlong coefficient prefix changed decoder state")
	}
	if first.StateIndex() != 63 || !first.MPS() || greater.StateIndex() != 63 || !greater.MPS() {
		t.Fatalf("overlong coefficient prefix changed context state")
	}
}

func TestCABACCoeffAbsLevelMinus1StandardEscapeVectors(t *testing.T) {
	for _, test := range []struct {
		codeOffset uint32
		data       byte
		want       uint32
		wantOffset uint32
	}{
		{codeOffset: 300, data: 0x00, want: 15, wantOffset: 472},
		{codeOffset: 302, data: 0x20, want: 16, wantOffset: 7},
	} {
		decoder := &CABACArithmeticDecoder{bits: NewBitReader([]byte{test.data}), codeRange: 510, codeOffset: test.codeOffset}
		first := &CABACContextModel{stateIndex: 63, valueMPS: true}
		greater := &CABACContextModel{stateIndex: 63, valueMPS: true}
		got, err := decoder.DecodeCoeffAbsLevelMinus1(first, greater)
		if err != nil || got != test.want {
			t.Errorf("escape vector offset %d: level = %d, %v; want %d, nil", test.codeOffset, got, err, test.want)
			continue
		}
		if decoder.CodeRange() != 482 || decoder.CodeOffset() != test.wantOffset || decoder.bits.bitOffset != 3 {
			t.Errorf("escape vector offset %d left state at range %d offset %d bit %d", test.codeOffset, decoder.CodeRange(), decoder.CodeOffset(), decoder.bits.bitOffset)
		}
	}
}

func TestCABACCoeffSign(t *testing.T) {
	positive := &CABACArithmeticDecoder{bits: NewBitReader([]byte{0x00}), codeRange: 510}
	if got, err := positive.DecodeCoeffSign(4); err != nil || got != 5 {
		t.Fatalf("positive coefficient = %d, %v; want 5, nil", got, err)
	}
	negative := &CABACArithmeticDecoder{bits: NewBitReader([]byte{0x00}), codeRange: 510, codeOffset: 255}
	if got, err := negative.DecodeCoeffSign(4); err != nil || got != -5 {
		t.Fatalf("negative coefficient = %d, %v; want -5, nil", got, err)
	}
	maxMagnitude := int32(0x7fffffff)
	maxPositive := &CABACArithmeticDecoder{bits: NewBitReader([]byte{0x00}), codeRange: 510}
	if got, err := maxPositive.DecodeCoeffSign(0x7ffffffe); err != nil || got != maxMagnitude {
		t.Fatalf("maximum positive coefficient = %d, %v; want %d, nil", got, err, maxMagnitude)
	}
	maxNegative := &CABACArithmeticDecoder{bits: NewBitReader([]byte{0x00}), codeRange: 510, codeOffset: 255}
	if got, err := maxNegative.DecodeCoeffSign(0x7ffffffe); err != nil || got != -maxMagnitude {
		t.Fatalf("maximum negative coefficient = %d, %v; want %d, nil", got, err, -maxMagnitude)
	}
	overflow := &CABACArithmeticDecoder{bits: NewBitReader(nil), codeRange: 510}
	if _, err := overflow.DecodeCoeffSign(0x7fffffff); !errors.Is(err, ErrCABACCoeffSignRange) {
		t.Fatalf("overflowing coefficient sign error = %v; want signed output range", err)
	}
	if overflow.CodeRange() != 510 || overflow.CodeOffset() != 0 || overflow.bits.bitOffset != 0 {
		t.Fatalf("overflowing coefficient sign consumed input or changed decoder state")
	}
	truncated := &CABACArithmeticDecoder{bits: NewBitReader(nil), codeRange: 510}
	if _, err := truncated.DecodeCoeffSign(0); !errors.Is(err, ErrBitstreamExhausted) {
		t.Fatalf("truncated coefficient sign error = %v; want truncated bitstream", err)
	}
	if truncated.CodeRange() != 510 || truncated.CodeOffset() != 0 || truncated.bits.bitOffset != 0 {
		t.Fatalf("truncated coefficient sign changed decoder state")
	}
}

func TestCABACLuma4x4ResidualLevelsAndReverseScanOrder(t *testing.T) {
	decoder := &CABACArithmeticDecoder{bits: NewBitReader([]byte{0x00}), codeRange: 510}
	var significance [16]bool
	significance[2] = true
	contexts := [10]CABACContextModel{}
	levels, err := decoder.DecodeLuma4x4ResidualLevels(&significance, &contexts)
	if err != nil || levels[2] != 1 {
		t.Fatalf("luma residual levels = %v, %v; want level 1 at scan position 2", levels, err)
	}
	for index, level := range levels {
		if index != 2 && level != 0 {
			t.Fatalf("unexpected level %d at scan position %d", level, index)
		}
	}
}

func TestCABACPlaceLuma4x4ScanLevels(t *testing.T) {
	scanLevels := [16]int32{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}
	wantRaster := [16]int32{0, 1, 5, 6, 2, 4, 7, 12, 3, 8, 11, 13, 9, 10, 14, 15}
	if got := PlaceLuma4x4ScanLevels(scanLevels); got != wantRaster {
		t.Fatalf("raster levels = %v; want %v", got, wantRaster)
	}
}

func TestCABACPlaceChroma4x4ScanLevels(t *testing.T) {
	acScanLevels := [15]int32{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}
	wantRaster := [16]int32{100, 1, 5, 6, 2, 4, 7, 12, 3, 8, 11, 13, 9, 10, 14, 15}
	if got := PlaceChroma4x4ScanLevels(100, acScanLevels); got != wantRaster {
		t.Fatalf("chroma raster levels = %v; want %v", got, wantRaster)
	}
}

func TestCABACLuma4x4ResidualBlockWithFlag(t *testing.T) {
	newBlockContexts := func() ([4]CABACContextModel, [15]CABACContextModel, [15]CABACContextModel, [10]CABACContextModel) {
		var coded [4]CABACContextModel
		var significant, last [15]CABACContextModel
		var coefficients [10]CABACContextModel
		for index := range significant {
			significant[index] = CABACContextModel{stateIndex: 63}
			last[index] = CABACContextModel{stateIndex: 63}
		}
		for index := range coefficients {
			coefficients[index] = CABACContextModel{stateIndex: 63}
		}
		return coded, significant, last, coefficients
	}

	decoder, err := NewCABACArithmeticDecoder(make([]byte, 128))
	if err != nil {
		t.Fatal(err)
	}
	coded, significant, last, coefficients := newBlockContexts()
	levels, hasResidual, err := decoder.DecodeLuma4x4ResidualBlockWithFlag(0, 0, &coded, &significant, &last, &coefficients)
	if err != nil || hasResidual || levels != [16]int32{} {
		t.Fatalf("uncoded residual block = %v, %t, %v; want zero levels and false", levels, hasResidual, err)
	}

	decoder, err = NewCABACArithmeticDecoder(make([]byte, 128))
	if err != nil {
		t.Fatal(err)
	}
	coded, significant, last, coefficients = newBlockContexts()
	coded[0].valueMPS = true
	if _, hasResidual, err = decoder.DecodeLuma4x4ResidualBlockWithFlag(0, 0, &coded, &significant, &last, &coefficients); err != nil || !hasResidual {
		t.Fatalf("coded residual block flag = %t, %v; want true, nil", hasResidual, err)
	}

	truncated := &CABACArithmeticDecoder{bits: NewBitReader(nil), codeRange: 510}
	coded, significant, last, coefficients = newBlockContexts()
	coded[0].valueMPS = true
	if _, _, err := truncated.DecodeLuma4x4ResidualBlockWithFlag(0, 0, &coded, &significant, &last, &coefficients); !errors.Is(err, ErrBitstreamExhausted) {
		t.Fatalf("truncated residual block error = %v; want truncated bitstream", err)
	}
	if truncated.CodeRange() != 510 || truncated.CodeOffset() != 0 || truncated.bits.bitOffset != 0 || coded[0].StateIndex() != 0 || !coded[0].MPS() || significant[0].StateIndex() != 63 {
		t.Fatal("truncated coded residual block changed decoder or contexts")
	}
}

func TestCABACDecodeAndReconstructLuma4x4Residual(t *testing.T) {
	decoder, err := NewCABACArithmeticDecoder(make([]byte, 128))
	if err != nil {
		t.Fatal(err)
	}
	var coded [4]CABACContextModel
	var significant, last [15]CABACContextModel
	var coefficients [10]CABACContextModel
	for index := range significant {
		significant[index] = CABACContextModel{stateIndex: 63}
		last[index] = CABACContextModel{stateIndex: 63}
	}
	for index := range coefficients {
		coefficients[index] = CABACContextModel{stateIndex: 63}
	}
	var scalingList [16]uint8
	for index := range scalingList {
		scalingList[index] = 16
	}
	residual, codedBlock, err := decoder.DecodeAndReconstructLuma4x4Residual(0, 0, &coded, &significant, &last, &coefficients, scalingList, 0)
	if err != nil || codedBlock || residual != [16]int64{} {
		t.Fatalf("uncoded residual samples = %v, %t, %v; want zero residual and false", residual, codedBlock, err)
	}
	beforeOffset := decoder.bits.bitOffset
	if _, _, err := decoder.DecodeAndReconstructLuma4x4Residual(0, 0, &coded, &significant, &last, &coefficients, scalingList, 52); !errors.Is(err, ErrInverseScaleQPYOutOfRange) {
		t.Fatalf("invalid QPY error = %v; want QPY range error", err)
	}
	if decoder.bits.bitOffset != beforeOffset {
		t.Fatal("invalid QPY consumed CABAC input")
	}
}

func forcedResidualContexts(mpsTrue ...int) [CABACContextCount]CABACContextModel {
	var contexts [CABACContextCount]CABACContextModel
	for index := range contexts {
		contexts[index] = CABACContextModel{stateIndex: 61}
	}
	for _, index := range mpsTrue {
		contexts[index].valueMPS = true
	}
	return contexts
}

func TestCABACCodedBlockFlagCondTerm(t *testing.T) {
	for _, test := range []struct {
		available, intra, ipcm, transAvailable, transCoded, want bool
	}{
		{false, true, false, false, false, true},
		{false, false, false, false, false, false},
		{true, false, true, false, false, true},
		{true, true, false, false, true, false},
		{true, false, false, true, false, false},
		{true, false, false, true, true, true},
	} {
		if got := DeriveCABACCodedBlockFlagCondTerm(test.available, test.intra, test.ipcm, test.transAvailable, test.transCoded); got != test.want {
			t.Errorf("condTerm(%+v) = %t; want %t", test, got, test.want)
		}
	}
}

// With a zero offset every regular bin decodes as its context's MPS and every bypass bin as 0.
func TestCABACDecodeResidualBlockChromaDCForcedBins(t *testing.T) {
	contexts := forcedResidualContexts(98, 149, 151, 212, 259)
	decoder, err := NewCABACArithmeticDecoder(make([]byte, 16))
	if err != nil {
		t.Fatal(err)
	}
	levels, coded, err := decoder.DecodeResidualBlock(3, true, false, &contexts)
	if err != nil {
		t.Fatal(err)
	}
	if !coded || levels != [16]int32{2, 0, 1} {
		t.Fatalf("chroma DC = %v coded=%t; want [2 0 1 0] coded", levels[:4], coded)
	}
	used := map[int]bool{98: true, 149: true, 210: true, 150: true, 151: true, 212: true, 258: true, 259: true, 262: true}
	for index, model := range contexts {
		wantState := uint8(61)
		if used[index] {
			wantState = 62
		}
		if model.stateIndex != wantState {
			t.Errorf("ctxIdx %d state = %d; want %d", index, model.stateIndex, wantState)
		}
	}

	contexts = forcedResidualContexts(97, 149, 150, 151, 257, 258, 259, 260, 261, 262, 263, 264, 265, 266)
	decoder, _ = NewCABACArithmeticDecoder(make([]byte, 64))
	levels, coded, err = decoder.DecodeResidualBlock(3, false, false, &contexts)
	if err != nil {
		t.Fatal(err)
	}
	if !coded || levels != [16]int32{15, 15, 15, 15} {
		t.Fatalf("escaped chroma DC = %v coded=%t; want four 15s", levels[:4], coded)
	}
	// Category 3 caps the greater-than-one context at ctxIdxInc 8 (ctxIdx 265), leaving 266 to category 4.
	if contexts[265].stateIndex != 62 || contexts[266].stateIndex != 61 || contexts[257].stateIndex != 62 {
		t.Fatalf("abs-level states 257/265/266 = %d/%d/%d; want 62/62/61", contexts[257].stateIndex, contexts[265].stateIndex, contexts[266].stateIndex)
	}
}

func TestCABACDecodeResidualBlockUncodedAndCategoryRange(t *testing.T) {
	contexts := forcedResidualContexts()
	decoder, _ := NewCABACArithmeticDecoder(make([]byte, 4))
	levels, coded, err := decoder.DecodeResidualBlock(0, true, true, &contexts)
	if err != nil || coded || levels != [16]int32{} || contexts[88].stateIndex != 62 {
		t.Fatalf("uncoded Intra16x16 DC = %v coded=%t err=%v ctx88=%d", levels, coded, err, contexts[88].stateIndex)
	}
	if _, _, err := decoder.DecodeResidualBlock(5, false, false, &contexts); !errors.Is(err, ErrCABACUnsupportedSyntax) {
		t.Fatalf("ctxBlockCat 5 error = %v; want unsupported", err)
	}
}

func TestCABACDecodeResidualBlockTruncationIsTransactional(t *testing.T) {
	mpsTrue := []int{85}
	for index := 105; index < 120; index++ {
		mpsTrue = append(mpsTrue, index)
	}
	for index := 227; index < 237; index++ {
		mpsTrue = append(mpsTrue, index)
	}
	contexts := forcedResidualContexts(mpsTrue...)
	original := contexts
	decoder, _ := NewCABACArithmeticDecoder([]byte{0, 0})
	if _, _, err := decoder.DecodeResidualBlock(0, false, false, &contexts); !errors.Is(err, ErrBitstreamExhausted) {
		t.Fatalf("truncated residual error = %v; want exhausted bitstream", err)
	}
	if contexts != original || decoder.CodeRange() != 510 || decoder.CodeOffset() != 0 || decoder.bits.bitOffset != 9 {
		t.Fatalf("failed residual mutated state: range=%d offset=%d bit=%d", decoder.CodeRange(), decoder.CodeOffset(), decoder.bits.bitOffset)
	}
}

func TestCABACDecodeResidualBlockMatchesBankDecoders(t *testing.T) {
	sliceContexts, err := NewCABACSliceContexts(2, 0, 28)
	if err != nil {
		t.Fatal(err)
	}
	patterns := [][]byte{
		{0x5a, 0x3c, 0x91, 0x07, 0xe2, 0x48, 0xb3, 0x6f, 0x12, 0xc5, 0x7e, 0x29, 0x84, 0xd1, 0x3b, 0xf6, 0x55, 0xaa, 0x0f, 0xf0},
		{0x00, 0x7f, 0x10, 0x20, 0x40, 0x80, 0xff, 0x01, 0x33, 0xcc, 0x99, 0x66, 0x11, 0xee, 0x22, 0xdd, 0x44, 0xbb, 0x88, 0x77},
		{0x1f, 0xe0, 0x3e, 0xc1, 0x7c, 0x83, 0xf8, 0x07, 0xab, 0xcd, 0xef, 0x01, 0x23, 0x45, 0x67, 0x89, 0x9a, 0xbc, 0xde, 0xf0},
	}
	for _, category := range []uint8{0, 1, 2, 4} {
		bases, _ := CABACResidualContextBasesForCategory(category)
		codedCases := 0
		for patternIndex, data := range patterns {
			for condTerms := 0; condTerms < 4; condTerms++ {
				condA, condB := condTerms&1 != 0, condTerms&2 != 0
				contexts := sliceContexts
				generic, _ := NewCABACArithmeticDecoder(data)
				levels, coded, genericErr := generic.DecodeResidualBlock(category, condA, condB, &contexts)

				var codedFlag [4]CABACContextModel
				var significant, last [15]CABACContextModel
				var coefficients [10]CABACContextModel
				copy(codedFlag[:], sliceContexts[bases.CodedBlockFlag:])
				copy(significant[:], sliceContexts[bases.Significant:])
				copy(last[:], sliceContexts[bases.Last:])
				copy(coefficients[:], sliceContexts[bases.AbsLevel:])
				bank, _ := NewCABACArithmeticDecoder(data)
				var wantRaster, gotRaster [16]int32
				var wantCoded bool
				var bankErr error
				if category == 0 || category == 2 {
					wantRaster, wantCoded, bankErr = bank.DecodeLuma4x4ResidualBlockWithFlag(boolToUint8(condA), boolToUint8(condB), &codedFlag, &significant, &last, &coefficients)
					gotRaster = PlaceLuma4x4ScanLevels(levels)
				} else {
					wantRaster, wantCoded, bankErr = bank.DecodeChroma4x4ACResidualBlock(condA, condB, &codedFlag, &significant, &last, &coefficients)
					var ac [15]int32
					copy(ac[:], levels[:15])
					gotRaster = PlaceChroma4x4ScanLevels(0, ac)
				}
				name := fmt.Sprintf("cat %d pattern %d cond %d", category, patternIndex, condTerms)
				if (genericErr == nil) != (bankErr == nil) {
					t.Fatalf("%s errors differ: %v vs %v", name, genericErr, bankErr)
				}
				if genericErr != nil {
					continue
				}
				if coded {
					codedCases++
				}
				if gotRaster != wantRaster || coded != wantCoded || generic.CodeRange() != bank.CodeRange() || generic.CodeOffset() != bank.CodeOffset() || generic.bits.bitOffset != bank.bits.bitOffset {
					t.Fatalf("%s: generic %v/%t differs from bank decoder %v/%t", name, gotRaster, coded, wantRaster, wantCoded)
				}
				for _, check := range []struct {
					base  int
					banks []CABACContextModel
				}{{bases.CodedBlockFlag, codedFlag[:]}, {bases.Significant, significant[:]}, {bases.Last, last[:]}, {bases.AbsLevel, coefficients[:]}} {
					for offset, model := range check.banks {
						if contexts[check.base+offset] != model {
							t.Fatalf("%s: ctxIdx %d = %+v; want %+v", name, check.base+offset, contexts[check.base+offset], model)
						}
					}
				}
			}
		}
		if codedCases == 0 {
			t.Errorf("ctxBlockCat %d equivalence vectors decoded no coded blocks", category)
		}
	}
}

func TestCABACDecodeLuma8x8ResidualBlockTable943(t *testing.T) {
	// Only significance ctxIdxInc 7 (ctxIdx 409) has MPS 1, so significant positions reveal Table 9-43.
	contexts := forcedResidualContexts(409)
	decoder, _ := NewCABACArithmeticDecoder(make([]byte, 64))
	levels, err := decoder.DecodeLuma8x8ResidualBlock(&contexts)
	if err != nil {
		t.Fatal(err)
	}
	var want [64]int32
	for _, index := range []int{23, 24, 25, 31, 32, 39, 63} {
		want[index] = 1
	}
	if levels != want {
		t.Fatalf("8x8 levels = %v; want ones at 23,24,25,31,32,39,63", levels)
	}
	used := map[int]bool{419: true, 420: true, 427: true, 428: true, 429: true, 430: true}
	for index := 402; index < 417; index++ {
		used[index] = true
	}
	for index, model := range contexts {
		wantState := uint8(61)
		if used[index] {
			wantState = 62
		}
		if model.stateIndex != wantState {
			t.Errorf("ctxIdx %d state = %d; want %d", index, model.stateIndex, wantState)
		}
	}

	contexts = forcedResidualContexts(409, 426, 427, 428, 429, 430, 431, 432, 433, 434, 435)
	decoder, _ = NewCABACArithmeticDecoder(make([]byte, 256))
	levels, err = decoder.DecodeLuma8x8ResidualBlock(&contexts)
	if err != nil {
		t.Fatal(err)
	}
	for _, index := range []int{23, 24, 25, 31, 32, 39, 63} {
		want[index] = 15
	}
	if levels != want {
		t.Fatalf("escaped 8x8 levels = %v; want 15 at 23,24,25,31,32,39,63", levels)
	}
	for index := 426; index < 436; index++ {
		wantState := uint8(62)
		if index == 428 || index == 429 || index == 430 {
			wantState = 61
		}
		if contexts[index].stateIndex != wantState {
			t.Errorf("abs-level ctxIdx %d state = %d; want %d", index, contexts[index].stateIndex, wantState)
		}
	}
}

func TestCABACDecodeLuma8x8ResidualBlockLastAndTruncation(t *testing.T) {
	contexts := forcedResidualContexts(402, 417)
	decoder, _ := NewCABACArithmeticDecoder(make([]byte, 8))
	levels, err := decoder.DecodeLuma8x8ResidualBlock(&contexts)
	if err != nil || levels != [64]int32{1} {
		t.Fatalf("first-coefficient 8x8 block = %v err=%v; want [1 0...]", levels[:4], err)
	}

	mpsTrue := []int{}
	for index := 402; index < 417; index++ {
		mpsTrue = append(mpsTrue, index)
	}
	for index := 426; index < 436; index++ {
		mpsTrue = append(mpsTrue, index)
	}
	contexts = forcedResidualContexts(mpsTrue...)
	original := contexts
	decoder, _ = NewCABACArithmeticDecoder([]byte{0, 0})
	if _, err := decoder.DecodeLuma8x8ResidualBlock(&contexts); !errors.Is(err, ErrBitstreamExhausted) {
		t.Fatalf("truncated 8x8 error = %v; want exhausted bitstream", err)
	}
	if contexts != original || decoder.CodeRange() != 510 || decoder.CodeOffset() != 0 || decoder.bits.bitOffset != 9 {
		t.Fatalf("failed 8x8 residual mutated state")
	}
	if _, err := decoder.DecodeLuma8x8ResidualBlock(nil); !errors.Is(err, ErrCABACContextState) {
		t.Fatalf("nil contexts error = %v; want context state", err)
	}
}

func TestCABACDecodeChroma4x4ACResidualBlock(t *testing.T) {
	makeContexts := func() ([4]CABACContextModel, [15]CABACContextModel, [15]CABACContextModel, [10]CABACContextModel) {
		var coded [4]CABACContextModel
		var significant, last [15]CABACContextModel
		var coefficients [10]CABACContextModel
		for index := range coded {
			coded[index] = CABACContextModel{stateIndex: 63, valueMPS: false}
		}
		for index := range significant {
			significant[index] = CABACContextModel{stateIndex: 63, valueMPS: false}
			last[index] = CABACContextModel{stateIndex: 63, valueMPS: false}
		}
		for index := range coefficients {
			coefficients[index] = CABACContextModel{stateIndex: 63, valueMPS: false}
		}
		return coded, significant, last, coefficients
	}
	decoder := &CABACArithmeticDecoder{bits: NewBitReader(nil), codeRange: 510}
	coded, significant, last, coefficients := makeContexts()
	levels, hasResidual, err := decoder.DecodeChroma4x4ACResidualBlock(false, false, &coded, &significant, &last, &coefficients)
	if err != nil || hasResidual || levels != [16]int32{} {
		t.Fatalf("uncoded chroma AC block = %v, %t, %v; want zero levels and false", levels, hasResidual, err)
	}

	decoder = &CABACArithmeticDecoder{bits: NewBitReader([]byte{0}), codeRange: 510}
	coded, significant, last, coefficients = makeContexts()
	coded[0].valueMPS = true
	levels, hasResidual, err = decoder.DecodeChroma4x4ACResidualBlock(false, false, &coded, &significant, &last, &coefficients)
	want := [16]int32{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}
	if err != nil || !hasResidual || levels != want {
		t.Fatalf("coded chroma AC block = %v, %t, %v; want %v, true, nil", levels, hasResidual, err, want)
	}

	decoder = &CABACArithmeticDecoder{bits: NewBitReader(nil), codeRange: 510}
	coded, significant, last, coefficients = makeContexts()
	coded[0].valueMPS = true
	if _, _, err := decoder.DecodeChroma4x4ACResidualBlock(false, false, &coded, &significant, &last, &coefficients); !errors.Is(err, ErrBitstreamExhausted) {
		t.Fatalf("truncated chroma AC block error = %v; want truncated bitstream", err)
	}
	if decoder.CodeRange() != 510 || decoder.CodeOffset() != 0 || decoder.bits.bitOffset != 0 || !coded[0].MPS() || coded[0].StateIndex() != 63 {
		t.Fatal("truncated chroma AC block changed decoder or coded-block context")
	}
}

func TestCABACLuma4x4ResidualBlockAndRollback(t *testing.T) {
	makeSignificanceContexts := func() [15]CABACContextModel {
		contexts := [15]CABACContextModel{}
		for index := range contexts {
			contexts[index] = CABACContextModel{stateIndex: 63}
		}
		contexts[2].valueMPS = true
		return contexts
	}
	makeLastContexts := func() [15]CABACContextModel {
		contexts := [15]CABACContextModel{}
		for index := range contexts {
			contexts[index] = CABACContextModel{stateIndex: 63}
		}
		contexts[2].valueMPS = true
		return contexts
	}
	makeCoefficientContexts := func() [10]CABACContextModel {
		contexts := [10]CABACContextModel{}
		for index := range contexts {
			contexts[index] = CABACContextModel{stateIndex: 63}
		}
		return contexts
	}

	decoder := &CABACArithmeticDecoder{bits: NewBitReader([]byte{0x00}), codeRange: 510}
	significant, last, coefficients := makeSignificanceContexts(), makeLastContexts(), makeCoefficientContexts()
	levels, err := decoder.DecodeLuma4x4ResidualBlock(&significant, &last, &coefficients)
	want := [16]int32{0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	if err != nil || levels != want {
		t.Fatalf("residual raster block = %v, %v; want %v", levels, err, want)
	}

	truncated := &CABACArithmeticDecoder{bits: NewBitReader(nil), codeRange: 510}
	significant, last, coefficients = makeSignificanceContexts(), makeLastContexts(), makeCoefficientContexts()
	for index := range significant {
		significant[index].stateIndex = 61
		last[index].stateIndex = 61
	}
	for index := range coefficients {
		coefficients[index].stateIndex = 61
	}
	if _, err := truncated.DecodeLuma4x4ResidualBlock(&significant, &last, &coefficients); !errors.Is(err, ErrBitstreamExhausted) {
		t.Fatalf("truncated residual block error = %v; want truncated bitstream", err)
	}
	if truncated.CodeRange() != 510 || truncated.CodeOffset() != 0 || truncated.bits.bitOffset != 0 || significant[0].StateIndex() != 61 || significant[2].StateIndex() != 61 || last[2].StateIndex() != 61 || coefficients[1].StateIndex() != 61 {
		t.Fatalf("truncated residual block changed decoder or context state")
	}
}

func TestCABACLuma4x4ResidualLevelsNegativeAndTruncationRollback(t *testing.T) {
	negative := &CABACArithmeticDecoder{bits: NewBitReader([]byte{0x00}), codeRange: 510, codeOffset: 255}
	var significance [16]bool
	significance[15] = true
	contexts := [10]CABACContextModel{}
	levels, err := negative.DecodeLuma4x4ResidualLevels(&significance, &contexts)
	if err != nil || levels[15] != -1 {
		t.Fatalf("negative scan level = %d, %v; want -1, nil", levels[15], err)
	}

	truncated := &CABACArithmeticDecoder{bits: NewBitReader(nil), codeRange: 510, codeOffset: 390}
	var truncatedSignificance [16]bool
	truncatedSignificance[0] = true
	truncatedContexts := [10]CABACContextModel{}
	for index := range truncatedContexts {
		truncatedContexts[index] = CABACContextModel{stateIndex: 62, valueMPS: true}
	}
	if _, err := truncated.DecodeLuma4x4ResidualLevels(&truncatedSignificance, &truncatedContexts); !errors.Is(err, ErrBitstreamExhausted) {
		t.Fatalf("truncated residual block error = %v; want truncated bitstream", err)
	}
	if truncated.CodeRange() != 510 || truncated.CodeOffset() != 390 || truncated.bits.bitOffset != 0 || truncatedContexts[0].StateIndex() != 62 {
		t.Fatalf("truncated residual block changed decoder/context state")
	}
}

func TestCABACLuma4x4ResidualLevelsRejectsEmptyMap(t *testing.T) {
	decoder := &CABACArithmeticDecoder{bits: NewBitReader(nil), codeRange: 510}
	var significance [16]bool
	var contexts [10]CABACContextModel
	if _, err := decoder.DecodeLuma4x4ResidualLevels(&significance, &contexts); !errors.Is(err, ErrCABACEmptyResidualBlock) {
		t.Fatalf("empty significance map error = %v; want empty residual block", err)
	}
}

func TestCABACDecodeIntraChroma420Macroblock(t *testing.T) {
	for _, cbp := range []uint8{1, 2} {
		contexts := [CABACContextCount]CABACContextModel{}
		for index := range contexts {
			contexts[index] = CABACContextModel{stateIndex: 63}
		}
		decoder, err := NewCABACArithmeticDecoder(make([]byte, 128))
		if err != nil {
			t.Fatal(err)
		}
		var scalingLists [2][16]uint8
		for component := range scalingLists {
			for index := range scalingLists[component] {
				scalingLists[component][index] = 16
			}
		}
		result, err := decoder.DecodeIntraChroma420Macroblock(
			&contexts, 0, false, 0, false, false, false, cbp, 26, [2]int{}, scalingLists,
			[2]CABACChroma420References{}, [2]CABACChroma420EdgeState{}, [2]CABACChroma420EdgeState{},
		)
		if err != nil {
			t.Fatalf("CBPChroma %d: %v", cbp, err)
		}
		if result.PredictionMode != 0 || result.QPC != [2]int{26, 26} {
			t.Fatalf("CBPChroma %d metadata = mode %d QPC %v", cbp, result.PredictionMode, result.QPC)
		}
		for _, sample := range result.Cb {
			if sample != 128 {
				t.Fatalf("CBPChroma %d Cb sample = %d; want 128", cbp, sample)
			}
		}
		if result.Cr != result.Cb || result.CbResidual != [64]int64{} || result.CrResidual != [64]int64{} {
			t.Fatalf("CBPChroma %d reconstructed unexpected chroma samples/residual", cbp)
		}
	}

	dcacContexts := forcedResidualContexts(100, 104)
	dcacDecoder, _ := NewCABACArithmeticDecoder(make([]byte, 128))
	var dcacScalingLists [2][16]uint8
	for component := range dcacScalingLists {
		for index := range dcacScalingLists[component] {
			dcacScalingLists[component][index] = 16
		}
	}
	dcacResult, err := dcacDecoder.DecodeIntraChroma420Macroblock(
		&dcacContexts, 0, false, 0, false, false, false, 2, 26, [2]int{}, dcacScalingLists,
		[2]CABACChroma420References{}, [2]CABACChroma420EdgeState{}, [2]CABACChroma420EdgeState{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if dcacResult.CbResidual == [64]int64{} || dcacResult.CrResidual == [64]int64{} {
		t.Fatalf("coded chroma DC/AC produced zero residuals: Cb=%v Cr=%v", dcacResult.CbResidual, dcacResult.CrResidual)
	}
	if !dcacResult.DCCoded[0] || !dcacResult.DCCoded[1] || !dcacResult.ACCodedBlockFlags[0][0] || !dcacResult.ACCodedBlockFlags[1][0] {
		t.Fatalf("coded-block flags were not returned: DC=%v AC=%v", dcacResult.DCCoded, dcacResult.ACCodedBlockFlags)
	}

	contexts := [CABACContextCount]CABACContextModel{}
	decoder, _ := NewCABACArithmeticDecoder(make([]byte, 16))
	var references [2]CABACChroma420References
	references[0].LeftAvailable = true
	references[0].Left = [8]uint8{20, 30, 40, 50, 60, 70, 80, 90}
	references[1].LeftAvailable = true
	references[1].Left = [8]uint8{100, 110, 120, 130, 140, 150, 160, 170}
	var modeScalingLists [2][16]uint8
	for component := range modeScalingLists {
		for index := range modeScalingLists[component] {
			modeScalingLists[component][index] = 16
		}
	}
	result, err := decoder.DecodeIntraChroma420Macroblock(
		&contexts, 1, true, 2, true, false, false, 0, 26, [2]int{}, modeScalingLists,
		references, [2]CABACChroma420EdgeState{}, [2]CABACChroma420EdgeState{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Cb[0] != 20 || result.Cb[56] != 90 || result.Cr[0] != 100 || result.Cr[56] != 170 {
		t.Fatalf("I16x16 horizontal chroma predictions = Cb %v Cr %v", result.Cb[:8], result.Cr[:8])
	}

	mbTypeContexts := forcedResidualContexts(100, 104)
	mbTypeDecoder, _ := NewCABACArithmeticDecoder(make([]byte, 128))
	mbTypeResult, err := mbTypeDecoder.DecodeIntraChroma420Macroblock(
		&mbTypeContexts, 0, true, 9, true, false, false, 0, 26, [2]int{}, modeScalingLists,
		[2]CABACChroma420References{}, [2]CABACChroma420EdgeState{}, [2]CABACChroma420EdgeState{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if mbTypeResult.PredictionMode != 0 || mbTypeResult.CbResidual == [64]int64{} || mbTypeResult.CrResidual == [64]int64{} {
		t.Fatal("I16x16 mb_type did not supply chroma coded-block-pattern 2")
	}
}

func TestCABACDecodeIntraChroma420MacroblockFailureRollsBack(t *testing.T) {
	contexts := forcedResidualContexts(64, 67)
	originalContexts := contexts
	decoder, _ := NewCABACArithmeticDecoder(make([]byte, 128))
	originalRange, originalOffset, originalBitOffset := decoder.CodeRange(), decoder.CodeOffset(), decoder.bits.bitOffset
	var scalingLists [2][16]uint8
	for component := range scalingLists {
		for index := range scalingLists[component] {
			scalingLists[component][index] = 16
		}
	}
	if _, err := decoder.DecodeIntraChroma420Macroblock(
		&contexts, 0, false, 0, false, false, false, 0, 26, [2]int{}, scalingLists,
		[2]CABACChroma420References{}, [2]CABACChroma420EdgeState{}, [2]CABACChroma420EdgeState{},
	); err == nil {
		t.Fatal("mode-3 chroma prediction without edges unexpectedly succeeded")
	}
	if contexts != originalContexts || decoder.CodeRange() != originalRange || decoder.CodeOffset() != originalOffset || decoder.bits.bitOffset != originalBitOffset {
		t.Fatal("failed chroma macroblock changed decoder or slice-context state")
	}
}

func TestCABACDecodeIIntraMacroblockDispatchesAndPlacesPlanes(t *testing.T) {
	contexts := [CABACContextCount]CABACContextModel{}
	for index := range contexts {
		contexts[index] = CABACContextModel{stateIndex: 63}
	}
	decoder, err := NewCABACArithmeticDecoder(make([]byte, 128))
	if err != nil {
		t.Fatal(err)
	}
	builder, err := NewYuv420FrameBuilder(1, 1, 0, 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	input := CABACIIntraMacroblockInput{SliceType: 2, PreviousQPY: 26}
	for index := range input.Luma4x4ScalingList {
		input.Luma4x4ScalingList[index] = 16
	}
	for component := range input.ChromaScalingLists {
		for index := range input.ChromaScalingLists[component] {
			input.ChromaScalingLists[component][index] = 16
		}
	}
	for index := range input.Luma4x4Blocks {
		input.Luma4x4Blocks[index] = LumaIntra4x4Block{
			Top:              [8]uint8{90, 90, 90, 90, 90, 90, 90, 90},
			Left:             [8]uint8{90, 90, 90, 90, 90, 90, 90, 90},
			TopLeft:          90,
			TopAvailable:     true,
			LeftAvailable:    true,
			TopLeftAvailable: true,
		}
	}
	result, err := decoder.DecodeIIntraMacroblock(input, &contexts, builder, 0)
	if err != nil {
		t.Fatal(err)
	}
	if result.MacroblockType != 0 || result.CodedBlockPatternLuma != 0 || result.CodedBlockPatternChroma != 0 || result.QPY != 26 {
		t.Fatalf("dispatched macroblock metadata = %+v", result)
	}
	frame, err := builder.Finish()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(frame.Y, result.Luma[:]) || !bytes.Equal(frame.U, result.Cb[:]) || !bytes.Equal(frame.V, result.Cr[:]) {
		t.Fatal("frame builder planes differ from the reconstructed macroblock")
	}
}

func TestCABACDecodeIIntraMacroblockI16x16AndIPCMBranches(t *testing.T) {
	t.Run("I16x16 derives CBP but decodes chroma mode", func(t *testing.T) {
		contexts := [CABACContextCount]CABACContextModel{}
		for index := range contexts {
			contexts[index] = CABACContextModel{stateIndex: 63}
		}
		contexts[3].valueMPS = true
		contexts[7].valueMPS = true
		contexts[8].valueMPS = true
		decoder, _ := NewCABACArithmeticDecoder(make([]byte, 128))
		builder, _ := NewYuv420FrameBuilder(1, 1, 0, 0, 0, 0)
		input := CABACIIntraMacroblockInput{SliceType: 2, PreviousQPY: 26, Intra16x16TopAvailable: true}
		for index := range input.Luma4x4ScalingList {
			input.Luma4x4ScalingList[index] = 16
		}
		for component := range input.ChromaScalingLists {
			for index := range input.ChromaScalingLists[component] {
				input.ChromaScalingLists[component][index] = 16
			}
		}
		for index := range input.Intra16x16Top {
			input.Intra16x16Top[index] = 90
		}
		result, err := decoder.DecodeIIntraMacroblock(input, &contexts, builder, 0)
		if err != nil {
			t.Fatal(err)
		}
		if result.MacroblockType != 9 || result.CodedBlockPatternChroma != 2 || result.QPY != 26 {
			t.Fatalf("I16x16 dispatch metadata = %+v", result)
		}
		for _, sample := range result.Luma {
			if sample != 90 {
				t.Fatalf("I16x16 luma prediction sample = %d; want 90", sample)
			}
		}
	})

	t.Run("I_PCM places samples and restarts CABAC", func(t *testing.T) {
		data := make([]byte, 1+256+64+64+2)
		data[0] = 0x80
		for index := 1; index <= 256; index++ {
			data[index] = 0x11
		}
		for index := 257; index <= 320; index++ {
			data[index] = 0x22
		}
		for index := 321; index <= 384; index++ {
			data[index] = 0x33
		}
		decoder := &CABACArithmeticDecoder{bits: NewBitReader(data), codeRange: 510, codeOffset: 509}
		contexts := [CABACContextCount]CABACContextModel{}
		builder, _ := NewYuv420FrameBuilder(1, 1, 0, 0, 0, 0)
		result, err := decoder.DecodeIIntraMacroblock(CABACIIntraMacroblockInput{SliceType: 2}, &contexts, builder, 0)
		if err != nil {
			t.Fatal(err)
		}
		if result.MacroblockType != 25 || decoder.CodeRange() != 510 || decoder.CodeOffset() != 0 || decoder.terminated {
			t.Fatalf("I_PCM dispatch/restart state = type %d range %d offset %d terminated=%t", result.MacroblockType, decoder.CodeRange(), decoder.CodeOffset(), decoder.terminated)
		}
		frame, err := builder.Finish()
		if err != nil || frame.Y[0] != 0x11 || frame.U[0] != 0x22 || frame.V[0] != 0x33 {
			t.Fatalf("I_PCM frame sample = %x/%x/%x, %v", frame.Y[0], frame.U[0], frame.V[0], err)
		}
	})
}

func TestCABACDecodeIIntraMacroblockDispatches8x8Transform(t *testing.T) {
	contexts := [CABACContextCount]CABACContextModel{}
	for index := range contexts {
		contexts[index] = CABACContextModel{stateIndex: 63}
	}
	contexts[399].valueMPS = true
	decoder, _ := NewCABACArithmeticDecoder(make([]byte, 128))
	builder, _ := NewYuv420FrameBuilder(1, 1, 0, 0, 0, 0)
	input := CABACIIntraMacroblockInput{SliceType: 2, PreviousQPY: 26, Transform8x8ModeEnabled: true}
	for index := range input.Luma8x8ScalingList {
		input.Luma8x8ScalingList[index] = 16
	}
	for component := range input.ChromaScalingLists {
		for index := range input.ChromaScalingLists[component] {
			input.ChromaScalingLists[component][index] = 16
		}
	}
	for index := range input.Luma8x8Blocks {
		input.Luma8x8Blocks[index] = LumaIntra8x8Block{
			Top:     [16]uint8{90, 90, 90, 90, 90, 90, 90, 90, 90, 90, 90, 90, 90, 90, 90, 90},
			Left:    [16]uint8{90, 90, 90, 90, 90, 90, 90, 90, 90, 90, 90, 90, 90, 90, 90, 90},
			TopLeft: 90, TopAvailable: true, LeftAvailable: true, TopLeftAvailable: true,
		}
	}
	result, err := decoder.DecodeIIntraMacroblock(input, &contexts, builder, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !result.TransformSize8x8 || result.MacroblockType != 0 {
		t.Fatalf("I_NxN dispatcher selected transform=%t mb_type=%d", result.TransformSize8x8, result.MacroblockType)
	}
	for _, sample := range result.Luma {
		if sample != 90 {
			t.Fatalf("8x8 intra prediction sample = %d; want 90", sample)
		}
	}
}

func TestCABACDecodeIIntraMacroblockBuilderFailureRollsBack(t *testing.T) {
	contexts := [CABACContextCount]CABACContextModel{}
	for index := range contexts {
		contexts[index] = CABACContextModel{stateIndex: 63}
	}
	originalContexts := contexts
	decoder, _ := NewCABACArithmeticDecoder(make([]byte, 128))
	originalRange, originalOffset, originalBitOffset := decoder.CodeRange(), decoder.CodeOffset(), decoder.bits.bitOffset
	builder, _ := NewYuv420FrameBuilder(1, 1, 0, 0, 0, 0)
	if err := builder.PlaceMacroblock(0, [256]uint8{}, [64]uint8{}, [64]uint8{}); err != nil {
		t.Fatal(err)
	}
	input := CABACIIntraMacroblockInput{SliceType: 2, PreviousQPY: 26}
	for index := range input.Luma4x4ScalingList {
		input.Luma4x4ScalingList[index] = 16
	}
	for component := range input.ChromaScalingLists {
		for index := range input.ChromaScalingLists[component] {
			input.ChromaScalingLists[component][index] = 16
		}
	}
	for index := range input.Luma4x4Blocks {
		input.Luma4x4Blocks[index].TopAvailable = true
		input.Luma4x4Blocks[index].LeftAvailable = true
		input.Luma4x4Blocks[index].TopLeftAvailable = true
	}
	if _, err := decoder.DecodeIIntraMacroblock(input, &contexts, builder, 0); !errors.Is(err, ErrYuv420MacroblockAssembly) {
		t.Fatalf("duplicate builder placement error = %v; want assembly error", err)
	}
	if contexts != originalContexts || decoder.CodeRange() != originalRange || decoder.CodeOffset() != originalOffset || decoder.bits.bitOffset != originalBitOffset {
		t.Fatal("builder rejection committed CABAC decoder or context state")
	}
}
