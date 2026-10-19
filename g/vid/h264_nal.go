package vid

import "errors"

var (
	ErrEmptyNALUnit     = errors.New("NAL unit is empty")
	ErrForbiddenZeroBit = errors.New("NAL forbidden_zero_bit is set")
	ErrMalformedEBSP    = errors.New("malformed emulation-prevention sequence")
)

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

// EBSPToRBSP removes valid emulation-prevention bytes from a NAL payload.
func EBSPToRBSP(ebsp []byte) ([]byte, error) {
	rbsp := make([]byte, 0, len(ebsp))
	zeroCount := 0
	for index, value := range ebsp {
		if zeroCount == 2 {
			if value == 0x03 {
				if index+1 == len(ebsp) || ebsp[index+1] > 0x03 {
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
