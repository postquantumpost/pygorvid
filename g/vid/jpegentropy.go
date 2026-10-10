package vid

import (
	"errors"
	"math/bits"
)

var ErrJPEGEncoderInvalidHuffmanTable = errors.New("JPEG Huffman table is invalid")
var ErrJPEGEncoderMissingHuffmanSymbol = errors.New("JPEG Huffman table does not contain the required symbol")

type jpegCodeword struct {
	value    uint16
	bitCount uint8
}

type jpegHuffmanTable struct {
	codes   [256]jpegCodeword
	present [256]bool
}

func newJPEGHuffmanTable(codeCounts [16]uint8, symbols []uint8) (jpegHuffmanTable, error) {
	total := 0
	for _, count := range codeCounts {
		total += int(count)
	}
	if total == 0 || total != len(symbols) || total > 256 {
		return jpegHuffmanTable{}, ErrJPEGEncoderInvalidHuffmanTable
	}

	var table jpegHuffmanTable
	code, symbolIndex := uint32(0), 0
	for length, count := range codeCounts {
		bitCount := uint8(length + 1)
		limit := uint32(1) << bitCount
		if code+uint32(count) > limit {
			return jpegHuffmanTable{}, ErrJPEGEncoderInvalidHuffmanTable
		}
		for range count {
			if code == limit-1 {
				return jpegHuffmanTable{}, ErrJPEGEncoderInvalidHuffmanTable
			}
			symbol := symbols[symbolIndex]
			if table.present[symbol] {
				return jpegHuffmanTable{}, ErrJPEGEncoderInvalidHuffmanTable
			}
			table.codes[symbol] = jpegCodeword{value: uint16(code), bitCount: bitCount}
			table.present[symbol] = true
			code++
			symbolIndex++
		}
		code <<= 1
	}
	return table, nil
}

func jpegEncodeDC(current, previous int16, table jpegHuffmanTable) ([]jpegCodeword, error) {
	difference := int32(current) - int32(previous)
	category := jpegMagnitudeCategory(difference)
	if category > 11 {
		return nil, ErrJPEGEncoderCoefficientOutOfRange
	}
	huffman, ok := jpegHuffmanCodeword(table, uint8(category))
	if !ok {
		return nil, ErrJPEGEncoderMissingHuffmanSymbol
	}
	codewords := []jpegCodeword{huffman}
	if category != 0 {
		amplitude, err := jpegAmplitudeCodeword(difference, uint8(category))
		if err != nil {
			return nil, err
		}
		codewords = append(codewords, amplitude)
	}
	return codewords, nil
}

func jpegEncodeAC(zigzag [64]int16, table jpegHuffmanTable) ([]jpegCodeword, error) {
	codewords := make([]jpegCodeword, 0, 64)
	zeroRun := 0
	for index := 1; index < len(zigzag); index++ {
		coefficient := int32(zigzag[index])
		if coefficient == 0 {
			zeroRun++
			continue
		}

		category := jpegMagnitudeCategory(coefficient)
		if category > 10 {
			return nil, ErrJPEGEncoderCoefficientOutOfRange
		}
		for zeroRun >= 16 {
			if err := appendJPEGHuffmanCodeword(&codewords, table, 0xf0); err != nil {
				return nil, err
			}
			zeroRun -= 16
		}
		symbol := uint8(zeroRun<<4) | uint8(category)
		if err := appendJPEGHuffmanCodeword(&codewords, table, symbol); err != nil {
			return nil, err
		}
		amplitude, err := jpegAmplitudeCodeword(coefficient, uint8(category))
		if err != nil {
			return nil, err
		}
		codewords = append(codewords, amplitude)
		zeroRun = 0
	}
	if zeroRun > 0 {
		if err := appendJPEGHuffmanCodeword(&codewords, table, 0x00); err != nil {
			return nil, err
		}
	}
	return codewords, nil
}

func appendJPEGHuffmanCodeword(codewords *[]jpegCodeword, table jpegHuffmanTable, symbol uint8) error {
	codeword, ok := jpegHuffmanCodeword(table, symbol)
	if !ok {
		return ErrJPEGEncoderMissingHuffmanSymbol
	}
	*codewords = append(*codewords, codeword)
	return nil
}

func jpegHuffmanCodeword(table jpegHuffmanTable, symbol uint8) (jpegCodeword, bool) {
	return table.codes[symbol], table.present[symbol]
}

func jpegMagnitudeCategory(value int32) int {
	if value < 0 {
		value = int32(-int64(value))
	}
	return bits.Len32(uint32(value))
}

func jpegAmplitudeCodeword(value int32, category uint8) (jpegCodeword, error) {
	if category > 16 || jpegMagnitudeCategory(value) != int(category) {
		return jpegCodeword{}, ErrJPEGEncoderCoefficientOutOfRange
	}
	if category == 0 {
		return jpegCodeword{}, nil
	}
	encoded := int64(value)
	if value < 0 {
		encoded += (int64(1) << category) - 1
	}
	return jpegCodeword{value: uint16(encoded), bitCount: category}, nil
}
