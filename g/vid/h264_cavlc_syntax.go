package vid

import "errors"

var cavlcIntraCBP = [48]uint8{47, 31, 15, 0, 23, 27, 29, 30, 7, 11, 13, 14, 39, 43, 45, 46, 16, 3, 5, 10, 12, 19, 21, 26, 28, 35, 37, 42, 44, 1, 2, 4, 8, 17, 18, 20, 24, 6, 9, 22, 25, 32, 33, 34, 36, 40, 38, 41}
var cavlcInterCBP = [48]uint8{0, 16, 1, 2, 4, 8, 32, 3, 5, 10, 12, 15, 47, 7, 11, 13, 14, 6, 9, 31, 35, 37, 42, 44, 33, 34, 36, 40, 39, 43, 45, 46, 17, 18, 20, 24, 19, 21, 26, 28, 23, 27, 29, 30, 22, 25, 38, 41}
var cavlcBlockScan = [16]int{0, 1, 4, 5, 2, 3, 6, 7, 8, 9, 12, 13, 10, 11, 14, 15}

type cavlcSyntax struct {
	counts                                [24]uint8
	left, top                             *[24]uint8
	lumaIndex, chromaIndex, chromaDCIndex int
	lumaCBP, chromaCBP                    uint8
	intra                                 bool
	skipRemaining                         uint32
	codedAfterSkip                        bool
}

func (syntax *cavlcSyntax) clone() *cavlcSyntax {
	if syntax == nil {
		return nil
	}
	copy := *syntax
	return &copy
}

func (syntax *cavlcSyntax) begin(address, first, width int, history [][24]uint8) {
	syntax.counts = [24]uint8{}
	syntax.left, syntax.top = nil, nil
	if address%width != 0 && len(history) != 0 {
		syntax.left = &history[len(history)-1]
	}
	if index := address - first - width; index >= 0 && index < len(history) {
		syntax.top = &history[index]
	}
	syntax.lumaIndex, syntax.chromaIndex, syntax.chromaDCIndex = 0, 0, 0
	syntax.lumaCBP, syntax.chromaCBP, syntax.intra = 0, 0, false
}

func (syntax *cavlcSyntax) skipped(reader *BitReader) (bool, error) {
	if syntax.skipRemaining > 0 {
		syntax.skipRemaining--
		return true, nil
	}
	if syntax.codedAfterSkip {
		syntax.codedAfterSkip = false
		return false, nil
	}
	run, err := reader.ReadUE()
	if err != nil || run == 0 {
		return false, err
	}
	syntax.skipRemaining, syntax.codedAfterSkip = run-1, true
	return true, nil
}

func (syntax *cavlcSyntax) finished(reader *BitReader) bool {
	return syntax.skipRemaining == 0 && !reader.MoreRBSPData()
}

func (syntax *cavlcSyntax) mbType(reader *BitReader, sliceType uint8) (uint8, error) {
	value, err := reader.ReadUE()
	if err != nil {
		return 0, err
	}
	base := uint32(0)
	switch sliceType % 5 {
	case 0:
		base = 5
	case 1:
		base = 23
	case 2:
	default:
		return 0, errors.New("unsupported CAVLC slice type")
	}
	if value > base+25 {
		return 0, errors.New("CAVLC mb_type exceeds slice range")
	}
	syntax.intra = value >= base
	if syntax.intra {
		kind := value - base
		if kind == 25 {
			for index := range syntax.counts {
				syntax.counts[index] = 16
			}
		} else if kind > 0 {
			if kind >= 13 {
				syntax.lumaCBP = 15
			}
			syntax.chromaCBP = uint8((kind - 1) / 4 % 3)
		}
	}
	return uint8(value), nil
}

func (syntax *cavlcSyntax) lumaPattern(reader *BitReader) (uint8, error) {
	code, err := reader.ReadUE()
	if err != nil {
		return 0, err
	}
	if code >= 48 {
		return 0, errors.New("CAVLC coded_block_pattern exceeds [0,47]")
	}
	pattern := cavlcInterCBP[code]
	if syntax.intra {
		pattern = cavlcIntraCBP[code]
	}
	syntax.lumaCBP, syntax.chromaCBP = pattern&15, pattern>>4
	return syntax.lumaCBP, nil
}

func cavlcUnsigned(reader *BitReader, maximum uint32) (uint8, error) {
	value, err := reader.ReadUE()
	if err != nil {
		return 0, err
	}
	if value > maximum {
		return 0, errors.New("CAVLC syntax value exceeds supported range")
	}
	return uint8(value), nil
}

func cavlcSigned(reader *BitReader) (int32, error) {
	value, err := reader.ReadSE()
	if err != nil {
		return 0, err
	}
	if value < -1<<31 || value > 1<<31-1 {
		return 0, errors.New("CAVLC signed value exceeds int32")
	}
	return int32(value), nil
}

func cavlcReference(reader *BitReader, maximum uint32) (uint8, error) {
	if maximum == 0 {
		return 0, nil
	}
	if maximum == 1 {
		bit, err := reader.ReadBit()
		return boolToUint8(!bit), err
	}
	return cavlcUnsigned(reader, maximum)
}

func cavlcPredictionMode(reader *BitReader, predicted uint8) (uint8, error) {
	previous, err := reader.ReadBit()
	if err != nil || previous {
		return predicted, err
	}
	remaining, err := reader.ReadBits(3)
	mode := uint8(remaining)
	if mode >= predicted {
		mode++
	}
	return mode, err
}

func (syntax *cavlcSyntax) nc(plane, block int) int {
	offset, width := 0, 4
	if plane != 0 {
		offset, width = 16+(plane-1)*4, 2
	}
	x, y := block%width, block/width
	left, top := -1, -1
	if x > 0 {
		left = int(syntax.counts[offset+block-1])
	} else if syntax.left != nil {
		left = int(syntax.left[offset+y*width+width-1])
	}
	if y > 0 {
		top = int(syntax.counts[offset+block-width])
	} else if syntax.top != nil {
		top = int(syntax.top[offset+(width-1)*width+x])
	}
	if left >= 0 && top >= 0 {
		return (left + top + 1) / 2
	}
	return max(0, max(left, top))
}

func (syntax *cavlcSyntax) residual(reader *BitReader, category uint8) ([16]int32, bool, error) {
	var levels [16]int32
	plane, block, capacity, nc := 0, 0, 16, syntax.nc(0, 0)
	switch category {
	case 0:
	case 1, 2:
		for syntax.lumaIndex < 16 && syntax.lumaCBP&(1<<(syntax.lumaIndex/4)) == 0 {
			syntax.lumaIndex++
		}
		if syntax.lumaIndex >= 16 {
			return levels, false, errors.New("CAVLC luma residual exceeds coded block pattern")
		}
		block = cavlcBlockScan[syntax.lumaIndex]
		syntax.lumaIndex++
		if category == 1 {
			capacity = 15
		}
		nc = syntax.nc(0, block)
	case 3:
		plane, capacity, nc = syntax.chromaDCIndex+1, 4, -1
		syntax.chromaDCIndex++
		if plane > 2 {
			return levels, false, errors.New("CAVLC chroma DC plane exceeds 4:2:0")
		}
	case 4:
		plane, block, capacity = syntax.chromaIndex/4+1, syntax.chromaIndex%4, 15
		syntax.chromaIndex++
		if plane > 2 {
			return levels, false, errors.New("CAVLC chroma AC plane exceeds 4:2:0")
		}
		nc = syntax.nc(plane, block)
	default:
		return levels, false, errors.New("unsupported CAVLC residual category")
	}
	decoded, err := DecodeCAVLCResidualBlock(reader, nc, capacity)
	if err != nil {
		return levels, false, err
	}
	if category != 0 && category != 3 {
		offset := 0
		if plane != 0 {
			offset = 16 + (plane-1)*4
		}
		syntax.counts[offset+block] = decoded.TotalCoeff
	}
	copy(levels[:], decoded.Coefficients)
	return levels, decoded.TotalCoeff != 0, nil
}

func (syntax *cavlcSyntax) residual8x8(reader *BitReader) ([64]int32, error) {
	var levels [64]int32
	for group := 0; group < 4; group++ {
		block, _, err := syntax.residual(reader, 2)
		if err != nil {
			return levels, err
		}
		for index, value := range block {
			levels[4*index+group] = value
		}
	}
	return levels, nil
}

func newCAVLCSyntaxDecoder(data []byte, offset uint64) (*CABACArithmeticDecoder, error) {
	reader := NewBitReader(data)
	for index := uint64(0); index < offset; index++ {
		if _, err := reader.ReadBit(); err != nil {
			return nil, err
		}
	}
	return &CABACArithmeticDecoder{bits: reader, codeRange: cabacInitialRange, cavlc: &cavlcSyntax{}}, nil
}

func readPCMRestartOffset(decoder *CABACArithmeticDecoder) (uint32, error) {
	if decoder.cavlc != nil {
		return 0, nil
	}
	return decoder.bits.ReadBits(9)
}
