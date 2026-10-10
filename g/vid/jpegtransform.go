package vid

import (
	"errors"
	"math"
)

var ErrJPEGEncoderInvalidQuantizationTable = errors.New("JPEG quantization table contains a zero entry")
var ErrJPEGEncoderCoefficientOutOfRange = errors.New("JPEG quantized coefficient is out of range")

var jpegDCTBasis = buildJPEGDCTBasis()

var jpegZigZagIndices = [64]uint8{
	0, 1, 8, 16, 9, 2, 3, 10,
	17, 24, 32, 25, 18, 11, 4, 5,
	12, 19, 26, 33, 40, 48, 41, 34,
	27, 20, 13, 6, 7, 14, 21, 28,
	35, 42, 49, 56, 57, 50, 43, 36,
	29, 22, 15, 23, 30, 37, 44, 51,
	58, 59, 52, 45, 38, 31, 39, 46,
	53, 60, 61, 54, 47, 55, 62, 63,
}

var jpegBaseLumaQuantization = [64]uint8{
	16, 11, 10, 16, 24, 40, 51, 61,
	12, 12, 14, 19, 26, 58, 60, 55,
	14, 13, 16, 24, 40, 57, 69, 56,
	14, 17, 22, 29, 51, 87, 80, 62,
	18, 22, 37, 56, 68, 109, 103, 77,
	24, 35, 55, 64, 81, 104, 113, 92,
	49, 64, 78, 87, 103, 121, 120, 101,
	72, 92, 95, 98, 112, 100, 103, 99,
}

var jpegBaseChromaQuantization = [64]uint8{
	17, 18, 24, 47, 99, 99, 99, 99,
	18, 21, 26, 66, 99, 99, 99, 99,
	24, 26, 56, 99, 99, 99, 99, 99,
	47, 66, 99, 99, 99, 99, 99, 99,
	99, 99, 99, 99, 99, 99, 99, 99,
	99, 99, 99, 99, 99, 99, 99, 99,
	99, 99, 99, 99, 99, 99, 99, 99,
	99, 99, 99, 99, 99, 99, 99, 99,
}

func buildJPEGDCTBasis() [8][8]float64 {
	var basis [8][8]float64
	for frequency := range basis {
		normalization := 0.5
		if frequency == 0 {
			normalization /= math.Sqrt2
		}
		for position := range basis[frequency] {
			basis[frequency][position] = normalization * math.Cos(float64((2*position+1)*frequency)*math.Pi/16)
		}
	}
	return basis
}

func jpegForwardDCT(samples [64]uint8) [64]float64 {
	var horizontal [64]float64
	for row := 0; row < 8; row++ {
		for horizontalFrequency := 0; horizontalFrequency < 8; horizontalFrequency++ {
			var sum float64
			for column := 0; column < 8; column++ {
				sample := float64(int(samples[row*8+column]) - 128)
				sum += sample * jpegDCTBasis[horizontalFrequency][column]
			}
			horizontal[row*8+horizontalFrequency] = sum
		}
	}

	var coefficients [64]float64
	for verticalFrequency := 0; verticalFrequency < 8; verticalFrequency++ {
		for horizontalFrequency := 0; horizontalFrequency < 8; horizontalFrequency++ {
			var sum float64
			for row := 0; row < 8; row++ {
				sum += horizontal[row*8+horizontalFrequency] * jpegDCTBasis[verticalFrequency][row]
			}
			coefficients[verticalFrequency*8+horizontalFrequency] = sum
		}
	}
	return coefficients
}

func jpegQuantizeBlock(coefficients [64]float64, quantization [64]uint8) ([64]int16, error) {
	var quantized [64]int16
	for index, coefficient := range coefficients {
		if quantization[index] == 0 {
			return [64]int16{}, ErrJPEGEncoderInvalidQuantizationTable
		}
		value := math.Round(coefficient / float64(quantization[index]))
		if math.IsNaN(value) || math.IsInf(value, 0) || value < math.MinInt16 || value > math.MaxInt16 {
			return [64]int16{}, ErrJPEGEncoderCoefficientOutOfRange
		}
		quantized[index] = int16(value)
	}
	return quantized, nil
}

func jpegZigZag(raster [64]int16) [64]int16 {
	var scan [64]int16
	for index, rasterIndex := range jpegZigZagIndices {
		scan[index] = raster[rasterIndex]
	}
	return scan
}

func jpegQuantizationTablesForQuality(quality int) ([64]uint8, [64]uint8, error) {
	if quality < 1 || quality > 100 {
		return [64]uint8{}, [64]uint8{}, ErrJPEGEncoderInvalidQuality
	}
	scale := 200 - 2*quality
	if quality < 50 {
		scale = 5000 / quality
	}
	return scaleJPEGQuantizationTable(jpegBaseLumaQuantization, scale),
		scaleJPEGQuantizationTable(jpegBaseChromaQuantization, scale), nil
}

func scaleJPEGQuantizationTable(base [64]uint8, scale int) [64]uint8 {
	var scaled [64]uint8
	for index, value := range base {
		quantizer := (int(value)*scale + 50) / 100
		if quantizer < 1 {
			quantizer = 1
		}
		if quantizer > 255 {
			quantizer = 255
		}
		scaled[index] = uint8(quantizer)
	}
	return scaled
}
