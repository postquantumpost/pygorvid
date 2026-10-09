package vid

import "errors"

var (
	ErrBitstreamExhausted = errors.New("truncated bitstream")
	ErrInvalidBitWidth    = errors.New("bit read width must be between 0 and 32")
	ErrExpGolombOverflow  = errors.New("Exp-Golomb value exceeds uint32")
)

// BitReader reads bits in network order from a byte slice.
type BitReader struct {
	data      []byte
	bitOffset uint64
}

// NewBitReader creates a reader over data without copying it.
func NewBitReader(data []byte) *BitReader {
	return &BitReader{data: data}
}

// ReadBits reads count bits, where count is between zero and 32.
func (r *BitReader) ReadBits(count int) (uint32, error) {
	if count < 0 || count > 32 {
		return 0, ErrInvalidBitWidth
	}
	available := uint64(len(r.data))*8 - r.bitOffset
	if uint64(count) > available {
		return 0, ErrBitstreamExhausted
	}
	var value uint32
	for range count {
		byteIndex := r.bitOffset / 8
		shift := 7 - r.bitOffset%8
		value = value<<1 | uint32((r.data[byteIndex]>>shift)&1)
		r.bitOffset++
	}
	return value, nil
}

// ReadBit reads one bit.
func (r *BitReader) ReadBit() (bool, error) {
	value, err := r.ReadBits(1)
	return value != 0, err
}

// AlignToByte skips the unread bits in the current byte.
func (r *BitReader) AlignToByte() {
	if remainder := r.bitOffset % 8; remainder != 0 {
		r.bitOffset += 8 - remainder
	}
}

// MoreRBSPData reports whether unread bits contain syntax beyond rbsp_trailing_bits.
func (r *BitReader) MoreRBSPData() bool {
	bitCount := uint64(len(r.data)) * 8
	if r.bitOffset >= bitCount {
		return false
	}
	readAt := func(offset uint64) bool {
		return r.data[offset/8]&(1<<(7-offset%8)) != 0
	}
	if !readAt(r.bitOffset) {
		return true
	}
	for offset := r.bitOffset + 1; offset < bitCount; offset++ {
		if readAt(offset) {
			return true
		}
	}
	return false
}

// ReadUE reads an unsigned Exp-Golomb value representable as uint32.
func (r *BitReader) ReadUE() (uint32, error) {
	start := r.bitOffset
	leadingZeros := 0
	for {
		bit, err := r.ReadBit()
		if err != nil {
			r.bitOffset = start
			return 0, err
		}
		if bit {
			break
		}
		leadingZeros++
		if leadingZeros > 32 {
			r.bitOffset = start
			return 0, ErrExpGolombOverflow
		}
	}
	suffix, err := r.ReadBits(leadingZeros)
	if err != nil {
		r.bitOffset = start
		return 0, err
	}
	value := (uint64(1) << leadingZeros) - 1 + uint64(suffix)
	if value > uint64(^uint32(0)) {
		r.bitOffset = start
		return 0, ErrExpGolombOverflow
	}
	return uint32(value), nil
}

// ReadSE reads a signed Exp-Golomb value.
func (r *BitReader) ReadSE() (int64, error) {
	codeNumber, err := r.ReadUE()
	if err != nil {
		return 0, err
	}
	magnitude := (uint64(codeNumber) + 1) / 2
	if codeNumber&1 == 0 {
		return -int64(magnitude), nil
	}
	return int64(magnitude), nil
}
