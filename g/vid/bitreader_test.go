package vid

import (
	"errors"
	"strings"
	"testing"
)

func packBitString(bits string) []byte {
	data := make([]byte, (len(bits)+7)/8)
	for index, bit := range bits {
		if bit == '1' {
			data[index/8] |= 1 << (7 - index%8)
		}
	}
	return data
}

func TestBitReaderBoundedReadsAndAlignment(t *testing.T) {
	reader := NewBitReader([]byte{0b10110011, 0b01101010})
	if value, err := reader.ReadBits(3); err != nil || value != 5 {
		t.Fatalf("ReadBits(3) = %d, %v; want 5, nil", value, err)
	}
	reader.AlignToByte()
	if value, err := reader.ReadBits(8); err != nil || value != 0x6a {
		t.Fatalf("ReadBits(8) after alignment = %#x, %v; want 0x6a, nil", value, err)
	}
	if value, err := reader.ReadBits(0); err != nil || value != 0 {
		t.Fatalf("ReadBits(0) = %d, %v; want 0, nil", value, err)
	}
	if _, err := reader.ReadBits(1); !errors.Is(err, ErrBitstreamExhausted) {
		t.Fatalf("ReadBits(1) at EOF error = %v; want truncated bitstream", err)
	}
}

func TestBitReaderRejectsInvalidWidthsWithoutConsuming(t *testing.T) {
	reader := NewBitReader([]byte{0xa5})
	if _, err := reader.ReadBits(33); !errors.Is(err, ErrInvalidBitWidth) {
		t.Fatalf("ReadBits(33) error = %v; want invalid width", err)
	}
	if _, err := reader.ReadBits(9); !errors.Is(err, ErrBitstreamExhausted) {
		t.Fatalf("ReadBits(9) error = %v; want truncated bitstream", err)
	}
	if value, err := reader.ReadBits(8); err != nil || value != 0xa5 {
		t.Fatalf("ReadBits(8) after failed reads = %#x, %v; want 0xa5, nil", value, err)
	}
}

func TestBitReaderMoreRBSPData(t *testing.T) {
	reader := NewBitReader([]byte{0xb0})
	if !reader.MoreRBSPData() {
		t.Fatal("syntax bits were mistaken for rbsp_trailing_bits")
	}
	if _, err := reader.ReadBits(3); err != nil {
		t.Fatal(err)
	}
	if reader.MoreRBSPData() {
		t.Fatal("rbsp_trailing_bits were mistaken for syntax")
	}
	if NewBitReader(nil).MoreRBSPData() {
		t.Fatal("empty reader has RBSP data")
	}
}

func TestBitReaderUnsignedExpGolomb(t *testing.T) {
	reader := NewBitReader(packBitString("1010011001000010100110"))
	for index, expected := range []uint32{0, 1, 2, 3, 4, 5} {
		value, err := reader.ReadUE()
		if err != nil || value != expected {
			t.Fatalf("ReadUE() #%d = %d, %v; want %d, nil", index, value, err, expected)
		}
	}
	maximum := NewBitReader(packBitString(strings.Repeat("0", 32) + "1" + strings.Repeat("0", 32)))
	if value, err := maximum.ReadUE(); err != nil || value != ^uint32(0) {
		t.Fatalf("ReadUE() at uint32 max = %d, %v", value, err)
	}
}

func TestBitReaderSignedExpGolomb(t *testing.T) {
	reader := NewBitReader(packBitString("1010011001000010100110"))
	for index, expected := range []int64{0, 1, -1, 2, -2, 3} {
		value, err := reader.ReadSE()
		if err != nil || value != expected {
			t.Fatalf("ReadSE() #%d = %d, %v; want %d, nil", index, value, err, expected)
		}
	}
}

func TestBitReaderExpGolombErrorsDoNotConsume(t *testing.T) {
	truncated := NewBitReader([]byte{0})
	if _, err := truncated.ReadUE(); !errors.Is(err, ErrBitstreamExhausted) {
		t.Fatalf("ReadUE() error = %v; want truncated bitstream", err)
	}
	if value, err := truncated.ReadBits(1); err != nil || value != 0 {
		t.Fatalf("first bit after failed ReadUE() = %d, %v; want 0, nil", value, err)
	}

	overflow := NewBitReader(packBitString("000000000000000000000000000000000"))
	if _, err := overflow.ReadUE(); !errors.Is(err, ErrExpGolombOverflow) {
		t.Fatalf("overflowing ReadUE() error = %v; want Exp-Golomb overflow", err)
	}
	if value, err := overflow.ReadBits(1); err != nil || value != 0 {
		t.Fatalf("first bit after overflowing ReadUE() = %d, %v; want 0, nil", value, err)
	}
	tooLarge := NewBitReader(packBitString(strings.Repeat("0", 32) + "1" + strings.Repeat("0", 31) + "1"))
	if _, err := tooLarge.ReadUE(); !errors.Is(err, ErrExpGolombOverflow) {
		t.Fatalf("out-of-range ReadUE() error = %v; want Exp-Golomb overflow", err)
	}
}
