package vid

var inverseScale4x4Factors = [6][3]int64{
	{10, 13, 16},
	{11, 14, 18},
	{13, 16, 20},
	{14, 18, 23},
	{16, 20, 25},
	{18, 23, 29},
}

var inverseScale8x8Factors = [6][6]int64{
	{20, 18, 32, 19, 25, 24},
	{22, 19, 35, 21, 28, 26},
	{26, 23, 42, 24, 33, 31},
	{28, 25, 45, 26, 35, 33},
	{32, 28, 51, 30, 40, 38},
	{36, 32, 58, 34, 46, 43},
}

var inverseScale8x8Classes = [64]uint8{
	0, 3, 4, 3, 0, 3, 4, 3, 3, 1, 5, 1, 3, 1, 5, 1,
	4, 5, 2, 5, 4, 5, 2, 5, 3, 1, 5, 1, 3, 1, 5, 1,
	0, 3, 4, 3, 0, 3, 4, 3, 3, 1, 5, 1, 3, 1, 5, 1,
	4, 5, 2, 5, 4, 5, 2, 5, 3, 1, 5, 1, 3, 1, 5, 1,
}

func inverseScaleLuma4x4(levels [16]int32, scalingList [16]uint8, qpy int) ([16]int64, error) {
	var scaled [16]int64
	if qpy < 0 || qpy > 51 {
		return scaled, ErrInverseScaleQPYOutOfRange
	}
	for index, weight := range scalingList {
		if weight == 0 {
			return scaled, ErrInverseScaleListZero
		}
		row, column := index/4, index%4
		scaled[index] = inverseScale4x4Coefficient(
			int64(levels[index]), int64(weight), qpy, row%2+column%2)
	}
	return scaled, nil
}

func inverseScaleChroma4x4(levels [16]int32, scalingList [16]uint8, qpc int) ([16]int64, error) {
	var scaled [16]int64
	if qpc < 0 || qpc > 39 {
		return scaled, ErrInverseScaleQPCOutOfRange
	}
	for _, level := range levels {
		if level < -(1<<15) || level > (1<<15)-1 {
			return scaled, ErrInverseScaleChromaBlockRange
		}
	}
	for _, weight := range scalingList {
		if weight == 0 {
			return scaled, ErrInverseScaleListZero
		}
	}

	scaled[0] = int64(levels[0])
	for index := 1; index < len(levels); index++ {
		row, column := index/4, index%4
		scaled[index] = inverseScale4x4Coefficient(
			int64(levels[index]), int64(scalingList[index]), qpc, row%2+column%2)
		if scaled[index] < -(1<<15) || scaled[index] > (1<<15)-1 {
			return [16]int64{}, ErrInverseScaleChromaBlockRange
		}
	}
	return scaled, nil
}

func reconstructChroma4x4Residual(dcC int64, acScanLevels [15]int32, scalingList [16]uint8, qpc int) ([16]int64, error) {
	if qpc < 0 || qpc > 39 {
		return [16]int64{}, ErrInverseScaleQPCOutOfRange
	}
	if dcC < -(1<<15) || dcC > (1<<15)-1 {
		return [16]int64{}, ErrInverseScaleChromaBlockRange
	}
	levels := PlaceChroma4x4ScanLevels(int32(dcC), acScanLevels)
	scaled, err := inverseScaleChroma4x4(levels, scalingList, qpc)
	if err != nil {
		return [16]int64{}, err
	}
	return inverseTransformLuma4x4(scaled), nil
}

func inverseTransformLuma4x4(coefficients [16]int64) (residual [16]int64) {
	var horizontal [16]int64
	for row := 0; row < 4; row++ {
		offset := row * 4
		transformed := inverseTransform4x4Line([4]int64{
			coefficients[offset],
			coefficients[offset+1],
			coefficients[offset+2],
			coefficients[offset+3],
		})
		copy(horizontal[offset:offset+4], transformed[:])
	}
	for column := 0; column < 4; column++ {
		transformed := inverseTransform4x4Line([4]int64{
			horizontal[column],
			horizontal[4+column],
			horizontal[8+column],
			horizontal[12+column],
		})
		for row, value := range transformed {
			residual[row*4+column] = (value + 32) >> 6
		}
	}
	return residual
}

func reconstructLuma4x4Residual(levels [16]int32, scalingList [16]uint8, qpy int) ([16]int64, error) {
	coefficients, err := inverseScaleLuma4x4(levels, scalingList, qpy)
	if err != nil {
		return [16]int64{}, err
	}
	return inverseTransformLuma4x4(coefficients), nil
}

func reconstructLuma4x4ACResidual(acScanLevels [15]int32, dcCoefficient int64, scalingList [16]uint8, qpy int) ([16]int64, error) {
	var scanLevels [16]int32
	copy(scanLevels[1:], acScanLevels[:])
	coefficients, err := inverseScaleLuma4x4(PlaceLuma4x4ScanLevels(scanLevels), scalingList, qpy)
	if err != nil {
		return [16]int64{}, err
	}
	coefficients[0] = dcCoefficient
	return inverseTransformLuma4x4(coefficients), nil
}

func hadamard4x4(levels [16]int32) (transformed [16]int64) {
	var horizontal [16]int64
	for row := 0; row < 4; row++ {
		line := hadamard4([4]int64{
			int64(levels[row*4]), int64(levels[row*4+1]),
			int64(levels[row*4+2]), int64(levels[row*4+3]),
		})
		copy(horizontal[row*4:row*4+4], line[:])
	}
	for column := 0; column < 4; column++ {
		line := hadamard4([4]int64{
			horizontal[column], horizontal[4+column],
			horizontal[8+column], horizontal[12+column],
		})
		for row := 0; row < 4; row++ {
			transformed[row*4+column] = line[row]
		}
	}
	return transformed
}

func inverseScaleLuma8x8(levels [64]int32, scalingList [64]uint8, qpy int) ([64]int64, error) {
	var scaled [64]int64
	if qpy < 0 || qpy > 51 {
		return scaled, ErrInverseScaleQPYOutOfRange
	}
	for index, weight := range scalingList {
		if weight == 0 {
			return scaled, ErrInverseScaleListZero
		}
		value := int64(levels[index]) *
			inverseScale8x8Factors[qpy%6][inverseScale8x8Classes[index]] *
			int64(weight)
		if qpy >= 36 {
			scaled[index] = value << (qpy/6 - 6)
		} else {
			shift := 6 - qpy/6
			scaled[index] = (value + 1<<(shift-1)) >> shift
		}
	}
	return scaled, nil
}

func inverseTransformLuma8x8(coefficients [64]int64) (residual [64]int64) {
	var horizontal [64]int64
	for row := 0; row < 8; row++ {
		offset := row * 8
		transformed := inverseTransform8x8Line([8]int64{
			coefficients[offset], coefficients[offset+1],
			coefficients[offset+2], coefficients[offset+3],
			coefficients[offset+4], coefficients[offset+5],
			coefficients[offset+6], coefficients[offset+7],
		})
		copy(horizontal[offset:offset+8], transformed[:])
	}
	for column := 0; column < 8; column++ {
		transformed := inverseTransform8x8Line([8]int64{
			horizontal[column], horizontal[8+column],
			horizontal[16+column], horizontal[24+column],
			horizontal[32+column], horizontal[40+column],
			horizontal[48+column], horizontal[56+column],
		})
		for row, value := range transformed {
			residual[row*8+column] = (value + 32) >> 6
		}
	}
	return residual
}

func reconstructLuma8x8Residual(levels [64]int32, scalingList [64]uint8, qpy int) ([64]int64, error) {
	coefficients, err := inverseScaleLuma8x8(levels, scalingList, qpy)
	if err != nil {
		return [64]int64{}, err
	}
	return inverseTransformLuma8x8(coefficients), nil
}

func blockPosition2x2(blockIndex int) (x, y int) {
	return blockIndex % 2, blockIndex / 2
}

func placeResidualBlock(destination, residual []int64, stride, originX, originY, blockSize int) {
	for row := 0; row < blockSize; row++ {
		copy(destination[(originY+row)*stride+originX:(originY+row)*stride+originX+blockSize],
			residual[row*blockSize:(row+1)*blockSize])
	}
}

func reconstructChromaComponentResidual(dcSamples [4]int64, acScanBlocks [4][15]int32, scalingList [16]uint8, qpc int) ([64]int64, error) {
	var blocks [4][16]int64
	for blockIndex := 0; blockIndex < 4; blockIndex++ {
		block, err := reconstructChroma4x4Residual(
			dcSamples[blockIndex], acScanBlocks[blockIndex], scalingList, qpc)
		if err != nil {
			return [64]int64{}, err
		}
		blocks[blockIndex] = block
	}
	return assembleChroma420ResidualMacroblock(blocks), nil
}

func inverseScale4x4Coefficient(level, weight int64, qp, factorClass int) int64 {
	value := level * inverseScale4x4Factors[qp%6][factorClass] * weight
	if qp >= 24 {
		return value << (qp/6 - 4)
	}
	shift := 4 - qp/6
	rounding := int64(1) << (shift - 1)
	return (value + rounding) >> shift
}

func inverseTransform4x4Line(coefficients [4]int64) (samples [4]int64) {
	evenSum := coefficients[0] + coefficients[2]
	evenDifference := coefficients[0] - coefficients[2]
	oddDifference := coefficients[1]>>1 - coefficients[3]
	oddSum := coefficients[1] + coefficients[3]>>1
	return [4]int64{
		evenSum + oddSum,
		evenDifference + oddDifference,
		evenDifference - oddDifference,
		evenSum - oddSum,
	}
}

func hadamard4(values [4]int64) [4]int64 {
	return [4]int64{
		values[0] + values[1] + values[2] + values[3],
		values[0] + values[1] - values[2] - values[3],
		values[0] - values[1] - values[2] + values[3],
		values[0] - values[1] + values[2] - values[3],
	}
}

func inverseTransform8x8Line(coefficients [8]int64) (samples [8]int64) {
	a0 := coefficients[0] + coefficients[4]
	a2 := coefficients[0] - coefficients[4]
	a4 := (coefficients[2] >> 1) - coefficients[6]
	a6 := coefficients[2] + (coefficients[6] >> 1)
	b0, b2, b4, b6 := a0+a6, a2+a4, a2-a4, a0-a6
	a1 := -coefficients[3] + coefficients[5] - coefficients[7] - (coefficients[7] >> 1)
	a3 := coefficients[1] + coefficients[7] - coefficients[3] - (coefficients[3] >> 1)
	a5 := -coefficients[1] + coefficients[7] + coefficients[5] + (coefficients[5] >> 1)
	a7 := coefficients[3] + coefficients[5] + coefficients[1] + (coefficients[1] >> 1)
	b1, b3 := a1+(a7>>2), a3+(a5>>2)
	b5, b7 := (a3>>2)-a5, a7-(a1>>2)
	return [8]int64{b0 + b7, b2 + b5, b4 + b3, b6 + b1, b6 - b1, b4 - b3, b2 - b5, b0 - b7}
}
