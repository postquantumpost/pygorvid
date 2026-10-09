package vid

import "errors"

var (
	ErrInverseScaleQPYOutOfRange = errors.New("inverse scaling QPY is outside [0,51]")
	ErrInverseScaleListZero      = errors.New("inverse scaling list contains zero")
)

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
	0, 3, 4, 3, 0, 3, 4, 3,
	3, 1, 5, 1, 3, 1, 5, 1,
	4, 5, 2, 5, 4, 5, 2, 5,
	3, 1, 5, 1, 3, 1, 5, 1,
	0, 3, 4, 3, 0, 3, 4, 3,
	3, 1, 5, 1, 3, 1, 5, 1,
	4, 5, 2, 5, 4, 5, 2, 5,
	3, 1, 5, 1, 3, 1, 5, 1,
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
		factorClass := row%2 + column%2
		value := int64(levels[index]) * inverseScale4x4Factors[qpy%6][factorClass] * int64(weight)
		if qpy >= 24 {
			scaled[index] = value << (qpy/6 - 4)
		} else {
			shift := 4 - qpy/6
			rounding := int64(1) << (shift - 1)
			scaled[index] = (value + rounding) >> shift
		}
	}
	return scaled, nil
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
		factorClass := inverseScale8x8Classes[index]
		value := int64(levels[index]) * inverseScale8x8Factors[qpy%6][factorClass] * int64(weight)
		if qpy >= 24 {
			scaled[index] = value << (qpy/6 - 4)
		} else {
			shift := 4 - qpy/6
			rounding := int64(1) << (shift - 1)
			scaled[index] = (value + rounding) >> shift
		}
	}
	return scaled, nil
}

func inverseTransformLuma4x4(coefficients [16]int64) (residual [16]int64) {
	var horizontal [16]int64
	for row := 0; row < 4; row++ {
		rowOffset := row * 4
		transformed := inverseTransform4x4Line([4]int64{
			coefficients[rowOffset],
			coefficients[rowOffset+1],
			coefficients[rowOffset+2],
			coefficients[rowOffset+3],
		})
		copy(horizontal[rowOffset:rowOffset+4], transformed[:])
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

func inverseTransform4x4Line(coefficients [4]int64) (samples [4]int64) {
	evenSum := coefficients[0] + coefficients[2]
	evenDifference := coefficients[0] - coefficients[2]
	oddDifference := (coefficients[1] >> 1) - coefficients[3]
	oddSum := coefficients[1] + (coefficients[3] >> 1)
	samples[0] = evenSum + oddSum
	samples[1] = evenDifference + oddDifference
	samples[2] = evenDifference - oddDifference
	samples[3] = evenSum - oddSum
	return samples
}

func inverseTransformLuma8x8(coefficients [64]int64) (residual [64]int64) {
	var horizontal [64]int64
	for row := 0; row < 8; row++ {
		rowOffset := row * 8
		transformed := inverseTransform8x8Line([8]int64{
			coefficients[rowOffset],
			coefficients[rowOffset+1],
			coefficients[rowOffset+2],
			coefficients[rowOffset+3],
			coefficients[rowOffset+4],
			coefficients[rowOffset+5],
			coefficients[rowOffset+6],
			coefficients[rowOffset+7],
		})
		copy(horizontal[rowOffset:rowOffset+8], transformed[:])
	}
	for column := 0; column < 8; column++ {
		transformed := inverseTransform8x8Line([8]int64{
			horizontal[column],
			horizontal[8+column],
			horizontal[16+column],
			horizontal[24+column],
			horizontal[32+column],
			horizontal[40+column],
			horizontal[48+column],
			horizontal[56+column],
		})
		for row, value := range transformed {
			residual[row*8+column] = (value + 32) >> 6
		}
	}
	return residual
}

func inverseTransform8x8Line(coefficients [8]int64) (samples [8]int64) {
	a0 := coefficients[0] + coefficients[4]
	a2 := coefficients[0] - coefficients[4]
	a4 := (coefficients[2] >> 1) - coefficients[6]
	a6 := coefficients[2] + (coefficients[6] >> 1)
	b0 := a0 + a6
	b2 := a2 + a4
	b4 := a2 - a4
	b6 := a0 - a6

	a1 := -coefficients[3] + coefficients[5] - coefficients[7] - (coefficients[7] >> 1)
	a3 := coefficients[1] + coefficients[7] - coefficients[3] - (coefficients[3] >> 1)
	a5 := -coefficients[1] + coefficients[7] + coefficients[5] + (coefficients[5] >> 1)
	a7 := coefficients[3] + coefficients[5] + coefficients[1] + (coefficients[1] >> 1)
	b1 := a1 + (a7 >> 2)
	b3 := a3 + (a5 >> 2)
	b5 := a5 - (a3 >> 2)
	b7 := a7 - (a1 >> 2)

	samples = [8]int64{
		b0 + b7,
		b2 + b5,
		b4 + b3,
		b6 + b1,
		b6 - b1,
		b4 - b3,
		b2 - b5,
		b0 - b7,
	}
	return samples
}

func predictLumaIntra8x8Vertical(top [8]uint8) (prediction [64]uint8) {
	for row := 0; row < 8; row++ {
		copy(prediction[row*8:row*8+8], top[:])
	}
	return prediction
}

func predictLumaIntra8x8Horizontal(left [8]uint8) (prediction [64]uint8) {
	for row, sample := range left {
		for column := 0; column < 8; column++ {
			prediction[row*8+column] = sample
		}
	}
	return prediction
}

func predictLumaIntra8x8DC(top, left *[8]uint8) (prediction [64]uint8) {
	dcValue := 128
	switch {
	case top != nil && left != nil:
		sum := 0
		for index := 0; index < 8; index++ {
			sum += int(top[index]) + int(left[index])
		}
		dcValue = (sum + 8) >> 4
	case top != nil:
		sum := 0
		for _, sample := range top {
			sum += int(sample)
		}
		dcValue = (sum + 4) >> 3
	case left != nil:
		sum := 0
		for _, sample := range left {
			sum += int(sample)
		}
		dcValue = (sum + 4) >> 3
	}
	for index := range prediction {
		prediction[index] = uint8(dcValue)
	}
	return prediction
}

func predictLumaIntra8x8DiagonalDownLeft(top [16]uint8) (prediction [64]uint8) {
	for row := 0; row < 8; row++ {
		for column := 0; column < 8; column++ {
			index := row + column
			lastIndex := index + 2
			if lastIndex >= len(top) {
				lastIndex = len(top) - 1
			}
			value := (int(top[index]) + 2*int(top[index+1]) + int(top[lastIndex]) + 2) >> 2
			prediction[row*8+column] = uint8(value)
		}
	}
	return prediction
}

func predictLumaIntra4x4Vertical(top [4]uint8) (prediction [16]uint8) {
	for row := 0; row < 4; row++ {
		copy(prediction[row*4:row*4+4], top[:])
	}
	return prediction
}

func predictLumaIntra4x4Horizontal(left [4]uint8) (prediction [16]uint8) {
	for row, sample := range left {
		for column := 0; column < 4; column++ {
			prediction[row*4+column] = sample
		}
	}
	return prediction
}

func predictLumaIntra4x4DC(top, left *[4]uint8) (prediction [16]uint8) {
	dcValue := 128
	switch {
	case top != nil && left != nil:
		sum := 0
		for index := 0; index < 4; index++ {
			sum += int(top[index]) + int(left[index])
		}
		dcValue = (sum + 4) >> 3
	case top != nil:
		sum := 0
		for _, sample := range top {
			sum += int(sample)
		}
		dcValue = (sum + 2) >> 2
	case left != nil:
		sum := 0
		for _, sample := range left {
			sum += int(sample)
		}
		dcValue = (sum + 2) >> 2
	}
	for index := range prediction {
		prediction[index] = uint8(dcValue)
	}
	return prediction
}

func predictLumaIntra4x4DiagonalDownLeft(top [8]uint8) (prediction [16]uint8) {
	for row := 0; row < 4; row++ {
		for column := 0; column < 4; column++ {
			index := row + column
			nextIndex := index + 1
			lastIndex := index + 2
			if lastIndex >= len(top) {
				lastIndex = len(top) - 1
			}
			value := (int(top[index]) + 2*int(top[nextIndex]) + int(top[lastIndex]) + 2) >> 2
			prediction[row*4+column] = uint8(value)
		}
	}
	return prediction
}

func predictLumaIntra4x4DiagonalDownRight(top [8]uint8, left [4]uint8, topLeft uint8) (prediction [16]uint8) {
	referenceAt := func(position int) int {
		switch {
		case position == -1:
			return int(topLeft)
		case position < -1:
			return int(left[-position-2])
		default:
			return int(top[position])
		}
	}
	for row := 0; row < 4; row++ {
		for column := 0; column < 4; column++ {
			position := column - row
			value := (referenceAt(position-1) + 2*referenceAt(position) + referenceAt(position+1) + 2) >> 2
			prediction[row*4+column] = uint8(value)
		}
	}
	return prediction
}

func predictLumaIntra8x8DiagonalDownRight(top [16]uint8, left [8]uint8, topLeft uint8) (prediction [64]uint8) {
	referenceAt := func(position int) int {
		switch {
		case position == -1:
			return int(topLeft)
		case position < -1:
			return int(left[-position-2])
		default:
			return int(top[position])
		}
	}
	for row := 0; row < 8; row++ {
		for column := 0; column < 8; column++ {
			position := column - row
			value := (referenceAt(position-1) + 2*referenceAt(position) + referenceAt(position+1) + 2) >> 2
			prediction[row*8+column] = uint8(value)
		}
	}
	return prediction
}

func predictLumaIntra8x8VerticalRight(top [16]uint8, left [8]uint8, topLeft uint8) (prediction [64]uint8) {
	topReferenceAt := func(position int) int {
		if position == -1 {
			return int(topLeft)
		}
		return int(top[position])
	}
	for row := 0; row < 8; row++ {
		for column := 0; column < 8; column++ {
			phase := 2*column - row
			var value int
			switch {
			case phase >= 0 && phase%2 == 0:
				center := phase / 2
				value = (topReferenceAt(center-1) + 2*topReferenceAt(center) + topReferenceAt(center+1) + 2) >> 2
			case phase >= 0:
				center := (phase - 1) / 2
				value = (topReferenceAt(center-1) + topReferenceAt(center) + 1) >> 1
			case phase == -1:
				value = (int(left[0]) + int(topLeft) + 1) >> 1
			case phase == -2:
				value = (int(left[0]) + 2*int(topLeft) + int(top[0]) + 2) >> 2
			case -phase%2 == 1:
				center := (-phase - 3) / 2
				value = (int(left[center]) + int(left[center+1]) + 1) >> 1
			default:
				center := (-phase - 4) / 2
				value = (int(left[center]) + 2*int(left[center+1]) + int(left[center+2]) + 2) >> 2
			}
			prediction[row*8+column] = uint8(value)
		}
	}
	return prediction
}

func predictLumaIntra8x8HorizontalDown(top [8]uint8, left [16]uint8, topLeft uint8) (prediction [64]uint8) {
	verticalPrediction := predictLumaIntra8x8VerticalRight(left, top, topLeft)
	for row := 0; row < 8; row++ {
		for column := 0; column < 8; column++ {
			prediction[row*8+column] = verticalPrediction[column*8+row]
		}
	}
	return prediction
}

func predictLumaIntra8x8VerticalLeft(top [16]uint8) (prediction [64]uint8) {
	for row := 0; row < 8; row++ {
		for column := 0; column < 8; column++ {
			phase := 2*column + row
			var value int
			if phase%2 == 0 {
				center := phase / 2
				value = (int(top[center]) + int(top[center+1]) + 1) >> 1
			} else {
				center := (phase - 1) / 2
				value = (int(top[center]) + 2*int(top[center+1]) + int(top[center+2]) + 2) >> 2
			}
			prediction[row*8+column] = uint8(value)
		}
	}
	return prediction
}

func predictLumaIntra8x8HorizontalUp(left [16]uint8) (prediction [64]uint8) {
	verticalPrediction := predictLumaIntra8x8VerticalLeft(left)
	for row := 0; row < 8; row++ {
		for column := 0; column < 8; column++ {
			prediction[row*8+column] = verticalPrediction[column*8+row]
		}
	}
	return prediction
}

func predictLumaIntra4x4VerticalRight(top [8]uint8, left [4]uint8, topLeft uint8) (prediction [16]uint8) {
	topReferenceAt := func(position int) int {
		if position == -1 {
			return int(topLeft)
		}
		return int(top[position])
	}
	for row := 0; row < 4; row++ {
		for column := 0; column < 4; column++ {
			phase := 2*column - row
			var value int
			switch {
			case phase >= 0 && phase%2 == 0:
				center := phase / 2
				value = (topReferenceAt(center-1) + 2*topReferenceAt(center) + topReferenceAt(center+1) + 2) >> 2
			case phase >= 0:
				center := (phase - 1) / 2
				value = (topReferenceAt(center-1) + topReferenceAt(center) + 1) >> 1
			case phase == -1:
				value = (int(left[0]) + int(topLeft) + 1) >> 1
			case phase == -2:
				value = (int(left[0]) + 2*int(topLeft) + int(top[0]) + 2) >> 2
			default:
				value = (int(left[0]) + int(left[1]) + 1) >> 1
			}
			prediction[row*4+column] = uint8(value)
		}
	}
	return prediction
}

func predictLumaIntra4x4HorizontalDown(top [4]uint8, left [4]uint8, topLeft uint8) (prediction [16]uint8) {
	verticalTop := [8]uint8{left[0], left[1], left[2], left[3], left[3], left[3], left[3], left[3]}
	verticalPrediction := predictLumaIntra4x4VerticalRight(verticalTop, top, topLeft)
	for row := 0; row < 4; row++ {
		for column := 0; column < 4; column++ {
			prediction[row*4+column] = verticalPrediction[column*4+row]
		}
	}
	return prediction
}

func predictLumaIntra4x4VerticalLeft(top [8]uint8) (prediction [16]uint8) {
	for row := 0; row < 4; row++ {
		for column := 0; column < 4; column++ {
			phase := 2*column + row
			var value int
			if phase%2 == 0 {
				center := phase / 2
				value = (int(top[center]) + int(top[center+1]) + 1) >> 1
			} else {
				center := (phase - 1) / 2
				value = (int(top[center]) + 2*int(top[center+1]) + int(top[center+2]) + 2) >> 2
			}
			prediction[row*4+column] = uint8(value)
		}
	}
	return prediction
}

func predictLumaIntra4x4HorizontalUp(left [8]uint8) (prediction [16]uint8) {
	for row := 0; row < 4; row++ {
		for column := 0; column < 4; column++ {
			phase := 2*row + column
			var value int
			if phase%2 == 0 {
				center := phase / 2
				value = (int(left[center]) + int(left[center+1]) + 1) >> 1
			} else {
				center := (phase - 1) / 2
				value = (int(left[center]) + 2*int(left[center+1]) + int(left[center+2]) + 2) >> 2
			}
			prediction[row*4+column] = uint8(value)
		}
	}
	return prediction
}
