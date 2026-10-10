package vid

import (
	"errors"
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
