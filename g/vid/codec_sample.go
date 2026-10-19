package vid

import (
	"errors"
)

func cloneNALUnits(nals [][]byte) [][]byte {
	clones := make([][]byte, len(nals))
	for index, nal := range nals {
		clones[index] = append([]byte(nil), nal...)
	}
	return clones
}

func validateLengthPrefixedNALs(data []byte, lengthSizes ...uint8) error {
	lengthSize := uint8(4)
	if len(lengthSizes) != 0 {
		lengthSize = lengthSizes[0]
	}
	if lengthSize != 1 && lengthSize != 2 && lengthSize != 4 {
		return errors.New("NAL length size must be 1, 2, or 4")
	}
	for offset := 0; offset < len(data); {
		if len(data)-offset < int(lengthSize) {
			return errors.New("truncated NAL length prefix")
		}
		var nalSize uint64
		for _, value := range data[offset : offset+int(lengthSize)] {
			nalSize = nalSize*256 + uint64(value)
		}
		offset += int(lengthSize)
		if nalSize == 0 {
			return errors.New("NAL unit has zero length")
		}
		if nalSize > uint64(len(data)-offset) {
			return errors.New("NAL unit is truncated")
		}
		offset += int(nalSize)
	}
	return nil
}
