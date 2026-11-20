package vid

import "errors"

var (
	ErrEmptyNALUnit     = errors.New("NAL unit is empty")
	ErrForbiddenZeroBit = errors.New("NAL forbidden_zero_bit is set")
	ErrMalformedEBSP    = errors.New("malformed emulation-prevention sequence")
)

// SplitAnnexBNALUnits splits a deterministic Annex-B byte stream into NAL
// units. Both three- and four-byte start codes are accepted.
func SplitAnnexBNALUnits(data []byte) ([][]byte, error) {
	var units [][]byte
	start := -1
	for index := 0; index+3 <= len(data); index++ {
		length := 0
		switch {
		case index+4 <= len(data) && data[index] == 0 && data[index+1] == 0 &&
			data[index+2] == 0 && data[index+3] == 1:
			length = 4
		case data[index] == 0 && data[index+1] == 0 && data[index+2] == 1:
			length = 3
		}
		if length == 0 {
			continue
		}
		if start >= 0 {
			units = append(units, append([]byte(nil), data[start:index]...))
		}
		start = index + length
		index += length - 1
	}
	if start < 0 {
		return nil, errors.New("Annex-B stream has no start code")
	}
	units = append(units, append([]byte(nil), data[start:]...))
	for _, unit := range units {
		if len(unit) == 0 {
			return nil, ErrEmptyNALUnit
		}
	}
	return units, nil
}

// NALHeader contains the fields in the one-byte H.264 NAL unit header.
type NALHeader struct {
	ReferenceIDC uint8
	UnitType     uint8
}

// ParseNALHeader extracts the NAL reference IDC and five-bit unit type.
func ParseNALHeader(nal []byte) (NALHeader, error) {
	if len(nal) == 0 {
		return NALHeader{}, ErrEmptyNALUnit
	}
	if nal[0]&0x80 != 0 {
		return NALHeader{}, ErrForbiddenZeroBit
	}
	return NALHeader{
		ReferenceIDC: (nal[0] >> 5) & 0x03,
		UnitType:     nal[0] & 0x1f,
	}, nil
}

// SplitH264SampleNALs recovers an Annex B end marker appended to the final VCL NAL.
func SplitH264SampleNALs(data []byte, lengthSize uint8) ([][]byte, error) {
	if err := validateLengthPrefixedNALs(data, lengthSize); err != nil {
		return nil, err
	}
	var nals [][]byte
	for offset := 0; offset < len(data); {
		size := 0
		for _, value := range data[offset : offset+int(lengthSize)] {
			size = size*256 + int(value)
		}
		offset += int(lengthSize)
		nals = append(nals, data[offset:offset+size])
		offset += size
	}
	if len(nals) != 0 {
		nal := nals[len(nals)-1]
		unitType := nal[0] & 0x1f
		if (unitType == 1 || unitType == 5) && len(nal) >= 4 &&
			(nal[len(nal)-1] == 0x0a || nal[len(nal)-1] == 0x0b) &&
			nal[len(nal)-4] == 0 && nal[len(nal)-3] == 0 && nal[len(nal)-2] == 1 {
			end := len(nal) - 4
			if end > 0 && nal[end-1] == 0 {
				end--
			}
			if end > 1 {
				// The start code is framing, not EBSP; preserve the empty end-marker NAL.
				nals[len(nals)-1] = nal[:end]
				nals = append(nals, nal[len(nal)-1:])
			}
		}
	}
	return nals, nil
}

// EBSPToRBSP removes valid emulation-prevention bytes from a NAL payload.
func EBSPToRBSP(ebsp []byte) ([]byte, error) {
	rbsp := make([]byte, 0, len(ebsp))
	zeroCount := 0
	for index, value := range ebsp {
		if zeroCount == 2 {
			if value == 0x03 {
				if index+1 < len(ebsp) && ebsp[index+1] > 0x03 {
					return nil, ErrMalformedEBSP
				}
				zeroCount = 0
				continue
			}
			if value <= 0x02 {
				return nil, ErrMalformedEBSP
			}
		}
		rbsp = append(rbsp, value)
		if value == 0 {
			zeroCount++
		} else {
			zeroCount = 0
		}
	}
	return rbsp, nil
}

// RBSPToEBSP inserts emulation-prevention bytes into RBSP data.
func RBSPToEBSP(rbsp []byte) []byte {
	ebsp := make([]byte, 0, len(rbsp)+len(rbsp)/32)
	zeroCount := 0
	for _, value := range rbsp {
		if zeroCount >= 2 && value <= 0x03 {
			ebsp = append(ebsp, 0x03)
			zeroCount = 0
		}
		ebsp = append(ebsp, value)
		if value == 0 {
			zeroCount++
		} else {
			zeroCount = 0
		}
	}
	return ebsp
}
