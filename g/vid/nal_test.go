package vid

import (
	"bytes"
	"errors"
	"testing"
)

func TestParseNALHeader(t *testing.T) {
	tests := []struct {
		value        byte
		referenceIDC uint8
		unitType     uint8
	}{
		{value: 0x65, referenceIDC: 3, unitType: 5},
		{value: 0x41, referenceIDC: 2, unitType: 1},
		{value: 0x06, referenceIDC: 0, unitType: 6},
		{value: 0x1f, referenceIDC: 0, unitType: 31},
	}
	for _, test := range tests {
		header, err := ParseNALHeader([]byte{test.value})
		if err != nil || header.ReferenceIDC != test.referenceIDC || header.UnitType != test.unitType {
			t.Errorf("ParseNALHeader(%#x) = %+v, %v", test.value, header, err)
		}
	}
	if _, err := ParseNALHeader(nil); !errors.Is(err, ErrEmptyNALUnit) {
		t.Errorf("empty NAL error = %v; want empty NAL", err)
	}
	if _, err := ParseNALHeader([]byte{0xe5}); !errors.Is(err, ErrForbiddenZeroBit) {
		t.Errorf("forbidden bit error = %v; want forbidden_zero_bit error", err)
	}
}

func TestEBSPToRBSPValidEscapes(t *testing.T) {
	for _, value := range []byte{0, 1, 2, 3} {
		got, err := EBSPToRBSP([]byte{0, 0, 3, value})
		want := []byte{0, 0, value}
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("escape for %#x = %v, %v; want %v, nil", value, got, err, want)
		}
	}
	got, err := EBSPToRBSP([]byte{0x67, 0, 0, 3, 0, 0, 3, 0, 1, 0x80})
	want := []byte{0x67, 0, 0, 0, 0, 0, 1, 0x80}
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("multiple escapes = %v, %v; want %v, nil", got, err, want)
	}
}

func TestEBSPToRBSPRejectsMalformedEscapes(t *testing.T) {
	for _, ebsp := range [][]byte{
		{0, 0, 0},
		{0, 0, 1},
		{0, 0, 2},
		{0, 0, 3},
		{0, 0, 3, 4},
	} {
		if _, err := EBSPToRBSP(ebsp); !errors.Is(err, ErrMalformedEBSP) {
			t.Errorf("EBSPToRBSP(%v) error = %v; want malformed EBSP", ebsp, err)
		}
	}
}
