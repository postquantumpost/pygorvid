package vid

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
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

func nalSample(nals [][]byte, width int) []byte {
	var sample []byte
	for _, nal := range nals {
		var prefix [4]byte
		binary.BigEndian.PutUint32(prefix[:], uint32(len(nal)))
		sample = append(sample, prefix[4-width:]...)
		sample = append(sample, nal...)
	}
	return sample
}

func TestSplitH264SampleNALsEndMarkers(t *testing.T) {
	for _, width := range []int{1, 2, 4} {
		for _, header := range []byte{0x41, 0x65} {
			for _, marker := range []byte{0x0a, 0x0b} {
				for _, prefix := range [][]byte{{0, 0, 1}, {0, 0, 0, 1}} {
					slice := []byte{header, 0x80}
					nal := append(append(append([]byte{}, slice...), prefix...), marker)
					if _, err := EBSPToRBSP(nal[1:]); !errors.Is(err, ErrMalformedEBSP) {
						t.Fatalf("raw payload error = %v; want malformed EBSP", err)
					}
					nals, err := SplitH264SampleNALs(nalSample([][]byte{nal}, width), uint8(width))
					if err != nil || len(nals) != 2 || !bytes.Equal(nals[0], slice) || !bytes.Equal(nals[1], []byte{marker}) {
						t.Fatalf("recovered NALs = %v, %v", nals, err)
					}
					if rbsp, err := EBSPToRBSP(nals[0][1:]); err != nil || !bytes.Equal(rbsp, []byte{0x80}) {
						t.Fatalf("recovered slice RBSP = %v, %v", rbsp, err)
					}
				}
			}
		}
	}
}

func TestSplitH264SampleNALsPreservesOtherPayloads(t *testing.T) {
	for _, nal := range [][]byte{
		{0x41, 0x80},
		{0x06, 0x80, 0, 0, 0, 1, 0x0a},
		{0x41, 0x80, 0, 0, 3, 1, 0x0a},
		{0x41, 0x80, 0, 0, 0, 1, 0x0c},
		{0x41, 0x80, 0, 0, 0, 1, 0x0a, 0x80},
		{0x41, 0, 0, 0, 1, 0x0a},
	} {
		nals, err := SplitH264SampleNALs(nalSample([][]byte{nal}, 4), 4)
		if err != nil || len(nals) != 1 || !bytes.Equal(nals[0], nal) {
			t.Fatalf("unexpected recovery of %v: %v, %v", nal, nals, err)
		}
	}
	bad := []byte{0x41, 0x80, 0, 0, 0, 1, 0x0a}
	nals, err := SplitH264SampleNALs(nalSample([][]byte{bad, {0x41, 0x80}}, 4), 4)
	if err != nil || len(nals) != 2 || !bytes.Equal(nals[0], bad) {
		t.Fatalf("nonfinal NAL changed: %v, %v", nals, err)
	}
	if _, err := EBSPToRBSP(nals[0][1:]); !errors.Is(err, ErrMalformedEBSP) {
		t.Fatalf("nonfinal payload error = %v", err)
	}
	bad = []byte{0x41, 0, 0, 2, 0x80, 0, 0, 0, 1, 0x0a}
	nals, err = SplitH264SampleNALs(nalSample([][]byte{bad}, 4), 4)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := EBSPToRBSP(nals[0][1:]); !errors.Is(err, ErrMalformedEBSP) {
		t.Fatalf("earlier malformed escape was accepted: %v", err)
	}
	for _, test := range []struct {
		data  []byte
		width uint8
	}{{[]byte{0}, 2}, {[]byte{0}, 1}, {[]byte{2, 0x41}, 1}, {nil, 3}} {
		if _, err := SplitH264SampleNALs(test.data, test.width); err == nil {
			t.Fatalf("accepted malformed framing: %+v", test)
		}
	}
}

func TestRanniruneFinalSliceFramingRecovery(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "h264", "rannirune-eos.hex"))
	if err != nil {
		t.Fatal(err)
	}
	var records [][]byte
	for _, text := range strings.Fields(string(data)) {
		record, err := hex.DecodeString(text)
		if err != nil {
			t.Fatal(err)
		}
		records = append(records, record)
	}
	if len(records) != 3 || len(records[2]) != 1092 {
		t.Fatal("invalid rannirune fixture")
	}
	sps, err := ParseSPS(records[0])
	if err != nil {
		t.Fatal(err)
	}
	pps, err := ParsePPS(records[1])
	if err != nil {
		t.Fatal(err)
	}
	nal := records[2]
	if _, err := ParseSliceHeader(nal, sps, pps); !errors.Is(err, ErrMalformedEBSP) {
		t.Fatalf("raw slice error = %v", err)
	}
	nals, err := SplitH264SampleNALs(nalSample([][]byte{nal}, 4), 4)
	if err != nil || len(nals) != 2 || !bytes.Equal(nals[0], nal[:1087]) || !bytes.Equal(nals[1], []byte{0x0a}) {
		t.Fatalf("recovered final slice = %v, %v", nals, err)
	}
	if rbsp, err := EBSPToRBSP(nals[0][1:]); err != nil || len(rbsp) != 1086 {
		t.Fatalf("recovered RBSP length = %d, %v", len(rbsp), err)
	}
	if _, err := ParseSliceHeader(nals[0], sps, pps); err != nil {
		t.Fatal(err)
	}
}
