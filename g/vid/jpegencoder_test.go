package vid

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image/jpeg"
	"math"
	"testing"
)

func TestJPEGEncoderContractAndQualityValidation(t *testing.T) {
	encoder := NewJPEGEncoder()
	if encoder.Quality != DefaultJPEGQuality {
		t.Fatalf("default quality = %d; want %d", encoder.Quality, DefaultJPEGQuality)
	}
	if _, err := NewJPEGEncoderWithQuality(0); !errors.Is(err, ErrJPEGEncoderInvalidQuality) {
		t.Fatalf("quality 0 error = %v; want %v", err, ErrJPEGEncoderInvalidQuality)
	}
	if _, err := NewJPEGEncoderWithQuality(101); !errors.Is(err, ErrJPEGEncoderInvalidQuality) {
		t.Fatalf("quality 101 error = %v; want %v", err, ErrJPEGEncoderInvalidQuality)
	}
	configured, err := NewJPEGEncoderWithQuality(90)
	if err != nil {
		t.Fatalf("quality 90 constructor error: %v", err)
	}
	if configured.Quality != 90 {
		t.Fatalf("configured quality = %d; want 90", configured.Quality)
	}
	for _, quality := range []int{1, 100} {
		if _, err := NewJPEGEncoderWithQuality(quality); err != nil {
			t.Fatalf("quality %d constructor error: %v", quality, err)
		}
	}

	buffer, err := NewPixelBuffer(2, 1, 8, 3, ColorRangeFull, []byte{1, 2, 3, 4, 5, 6, 99, 99})
	if err != nil {
		t.Fatalf("NewPixelBuffer() error: %v", err)
	}
	encoded, err := encoder.Encode(buffer)
	if err != nil {
		t.Fatalf("Encode() error: %v", err)
	}
	if !bytes.HasPrefix(encoded, []byte{0xff, 0xd8}) || !bytes.HasSuffix(encoded, []byte{0xff, 0xd9}) {
		t.Fatalf("JPEG framing does not have SOI/EOI: %v ... %v", encoded[:2], encoded[len(encoded)-2:])
	}
	config, err := jpeg.DecodeConfig(bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("jpeg.DecodeConfig() error: %v", err)
	}
	if config.Width != 2 || config.Height != 1 {
		t.Fatalf("JPEG dimensions = %dx%d; want 2x1", config.Width, config.Height)
	}
	decoded, err := jpeg.Decode(bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("jpeg.Decode() error: %v", err)
	}
	if decoded.Bounds().Dx() != 2 || decoded.Bounds().Dy() != 1 {
		t.Fatalf("decoded image bounds = %v; want 2x1", decoded.Bounds())
	}
	if _, err := encoder.Encode(PixelBuffer{Width: 2, Height: 1, Stride: 6, Channels: 1, Data: make([]byte, 6)}); !errors.Is(err, ErrJPEGEncoderInvalidPixelBuffer) {
		t.Fatalf("grayscale input error = %v; want %v", err, ErrJPEGEncoderInvalidPixelBuffer)
	}
	if _, err := encoder.Encode(PixelBuffer{Width: 2, Height: 1, Stride: 5, Channels: 3, Data: make([]byte, 5)}); !errors.Is(err, ErrJPEGEncoderInvalidPixelBuffer) {
		t.Fatalf("short-row input error = %v; want %v", err, ErrJPEGEncoderInvalidPixelBuffer)
	}
	if _, err := encoder.Encode(PixelBuffer{Width: 65536, Height: 1, Stride: 196608, Channels: 3}); !errors.Is(err, ErrJPEGEncoderInvalidDimensions) {
		t.Fatalf("oversized dimensions error = %v; want %v", err, ErrJPEGEncoderInvalidDimensions)
	}
	if err := encoder.Write(nil, buffer); !errors.Is(err, ErrJPEGEncoderInvalidWriter) {
		t.Fatalf("nil writer error = %v; want %v", err, ErrJPEGEncoderInvalidWriter)
	}
}

func TestJPEGMarkerWriterEncodesStandaloneAndLengthBearingMarkers(t *testing.T) {
	encoded, err := appendJPEGMarker(nil, 0xd8, nil)
	if err != nil {
		t.Fatalf("write SOI: %v", err)
	}
	segment := []byte{'J', 'F', 'I', 'F', 0, 1, 2}
	encoded, err = appendJPEGMarker(encoded, 0xe0, segment)
	if err != nil {
		t.Fatalf("write APP0: %v", err)
	}
	encoded, err = appendJPEGMarker(encoded, 0xd9, nil)
	if err != nil {
		t.Fatalf("write EOI: %v", err)
	}
	want := []byte{0xff, 0xd8, 0xff, 0xe0, 0, 9, 'J', 'F', 'I', 'F', 0, 1, 2, 0xff, 0xd9}
	if !bytes.Equal(encoded, want) {
		t.Fatalf("marker bytes = %v; want %v", encoded, want)
	}
}

func TestJPEGMarkerWriterRejectsInvalidSegments(t *testing.T) {
	if _, err := appendJPEGMarker(nil, 0xe0, make([]byte, 65534)); !errors.Is(err, ErrJPEGEncoderSegmentTooLarge) {
		t.Fatalf("oversized segment error = %v; want %v", err, ErrJPEGEncoderSegmentTooLarge)
	}
	if _, err := appendJPEGMarker(nil, 0xd8, []byte{1}); !errors.Is(err, ErrJPEGEncoderInvalidMarker) {
		t.Fatalf("SOI payload error = %v; want %v", err, ErrJPEGEncoderInvalidMarker)
	}
	if _, err := appendJPEGMarker(nil, 0x00, nil); !errors.Is(err, ErrJPEGEncoderInvalidMarker) {
		t.Fatalf("zero marker error = %v; want %v", err, ErrJPEGEncoderInvalidMarker)
	}
}

func TestJPEGMarkerWriterAcceptsMaximumSegmentPayload(t *testing.T) {
	payload := make([]byte, 65533)
	encoded, err := appendJPEGMarker(nil, 0xe1, payload)
	if err != nil {
		t.Fatalf("maximum legal segment error: %v", err)
	}
	if len(encoded) != 65537 || encoded[2] != 0xff || encoded[3] != 0xff {
		t.Fatalf("maximum segment header/length = %v, total length %d; want length 65535 and total 65537", encoded[:4], len(encoded))
	}
}

func TestJPEGYCbCr444UsesJFIFPrimariesAndNeutralVectors(t *testing.T) {
	tests := []struct {
		name    string
		r, g, b uint8
		wantY   uint8
		wantCb  uint8
		wantCr  uint8
	}{
		{name: "black", r: 0, g: 0, b: 0, wantY: 0, wantCb: 128, wantCr: 128},
		{name: "white", r: 255, g: 255, b: 255, wantY: 255, wantCb: 128, wantCr: 128},
		{name: "red", r: 255, g: 0, b: 0, wantY: 76, wantCb: 85, wantCr: 255},
		{name: "green", r: 0, g: 255, b: 0, wantY: 150, wantCb: 44, wantCr: 21},
		{name: "blue", r: 0, g: 0, b: 255, wantY: 29, wantCb: 255, wantCr: 107},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotY, gotCb, gotCr := jpegRGBToYCbCr(test.r, test.g, test.b)
			if gotY != test.wantY || gotCb != test.wantCb || gotCr != test.wantCr {
				t.Fatalf("jpegRGBToYCbCr(%d,%d,%d) = (%d,%d,%d); want (%d,%d,%d)",
					test.r, test.g, test.b, gotY, gotCb, gotCr, test.wantY, test.wantCb, test.wantCr)
			}
		})
	}
}

func TestJPEGConvertRGBToYCbCr444HonorsStride(t *testing.T) {
	buffer, err := NewPixelBuffer(3, 2, 11, 3, ColorRangeFull, []byte{
		255, 0, 0, 0, 255, 0, 0, 0, 255, 99, 99,
		0, 0, 0, 255, 255, 255, 128, 128, 128, 88, 88,
	})
	if err != nil {
		t.Fatalf("NewPixelBuffer() error: %v", err)
	}
	got, err := convertRGBToYCbCr444(buffer)
	if err != nil {
		t.Fatalf("convertRGBToYCbCr444() error: %v", err)
	}
	if got.width != 3 || got.height != 2 {
		t.Fatalf("YCbCr dimensions = %dx%d; want 3x2", got.width, got.height)
	}
	if !bytes.Equal(got.y, []byte{76, 150, 29, 0, 255, 128}) {
		t.Fatalf("Y = %v; want [76 150 29 0 255 128]", got.y)
	}
	if !bytes.Equal(got.cb, []byte{85, 44, 255, 128, 128, 128}) {
		t.Fatalf("Cb = %v; want [85 44 255 128 128 128]", got.cb)
	}
	if !bytes.Equal(got.cr, []byte{255, 21, 107, 128, 128, 128}) {
		t.Fatalf("Cr = %v; want [255 21 107 128 128 128]", got.cr)
	}
}

func TestJPEGForwardDCTConstantAndImpulseVectors(t *testing.T) {
	var constant [64]uint8
	for index := range constant {
		constant[index] = 128
	}
	for index, coefficient := range jpegForwardDCT(constant) {
		if coefficient != 0 {
			t.Fatalf("level-shifted constant coefficient %d = %g; want 0", index, coefficient)
		}
	}

	for index := range constant {
		constant[index] = 129
	}
	transformed := jpegForwardDCT(constant)
	if math.Abs(transformed[0]-8) > 1e-12 {
		t.Fatalf("constant-block DC = %.15g; want 8", transformed[0])
	}
	for index, coefficient := range transformed[1:] {
		if math.Abs(coefficient) > 1e-12 {
			t.Fatalf("constant-block AC coefficient %d = %.15g; want 0", index+1, coefficient)
		}
	}

	for index := range constant {
		constant[index] = 128
	}
	constant[0] = 129
	transformed = jpegForwardDCT(constant)
	want := [4]float64{0.125, 0.1733799806652684, 0.1733799806652684, 0.24048494156391084}
	indices := [4]int{0, 1, 8, 9}
	for index, coefficientIndex := range indices {
		if math.Abs(transformed[coefficientIndex]-want[index]) > 1e-12 {
			t.Fatalf("impulse coefficient %d = %.15g; want %.15g", coefficientIndex, transformed[coefficientIndex], want[index])
		}
	}
}

func TestJPEGQuantizeBlockRoundsSignedCoefficients(t *testing.T) {
	var coefficients [64]float64
	var quantization [64]uint8
	for index := range quantization {
		quantization[index] = 2
	}
	coefficients[0] = 7
	coefficients[1] = -7
	coefficients[2] = 8.49
	quantized, err := jpegQuantizeBlock(coefficients, quantization)
	if err != nil {
		t.Fatalf("jpegQuantizeBlock() error: %v", err)
	}
	if quantized[0] != 4 || quantized[1] != -4 || quantized[2] != 4 {
		t.Fatalf("quantized coefficients = %d, %d, %d; want 4, -4, 4", quantized[0], quantized[1], quantized[2])
	}

	quantization[3] = 0
	if _, err := jpegQuantizeBlock(coefficients, quantization); !errors.Is(err, ErrJPEGEncoderInvalidQuantizationTable) {
		t.Fatalf("zero quantizer error = %v; want %v", err, ErrJPEGEncoderInvalidQuantizationTable)
	}
	quantization[3] = 2
	coefficients[4] = math.Inf(1)
	if _, err := jpegQuantizeBlock(coefficients, quantization); !errors.Is(err, ErrJPEGEncoderCoefficientOutOfRange) {
		t.Fatalf("non-finite coefficient error = %v; want %v", err, ErrJPEGEncoderCoefficientOutOfRange)
	}
}

func TestJPEGZigZagOrdering(t *testing.T) {
	var raster [64]int16
	for index := range raster {
		raster[index] = int16(index)
	}
	got := jpegZigZag(raster)
	wantRasterIndices := [64]int{
		0, 1, 8, 16, 9, 2, 3, 10,
		17, 24, 32, 25, 18, 11, 4, 5,
		12, 19, 26, 33, 40, 48, 41, 34,
		27, 20, 13, 6, 7, 14, 21, 28,
		35, 42, 49, 56, 57, 50, 43, 36,
		29, 22, 15, 23, 30, 37, 44, 51,
		58, 59, 52, 45, 38, 31, 39, 46,
		53, 60, 61, 54, 47, 55, 62, 63,
	}
	for index, rasterIndex := range wantRasterIndices {
		if got[index] != int16(rasterIndex) {
			t.Fatalf("zigzag[%d] = %d; want raster coefficient %d", index, got[index], rasterIndex)
		}
	}
}

func TestJPEGHuffmanTableBuildsCanonicalCodes(t *testing.T) {
	var counts [16]uint8
	counts[1], counts[2], counts[3], counts[4], counts[5], counts[6] = 1, 3, 3, 2, 7, 1
	symbols := []uint8{4, 3, 20, 1, 19, 5, 36, 2, 6, 52, 51, 0, 21, 134, 7, 180, 35}
	table, err := newJPEGHuffmanTable(counts, symbols)
	if err != nil {
		t.Fatalf("newJPEGHuffmanTable() error: %v", err)
	}
	if got := table.codes[4]; got.value != 0 || got.bitCount != 2 {
		t.Fatalf("code for local DHT symbol 4 = (%b,%d); want (00,2)", got.value, got.bitCount)
	}
	if got := table.codes[3]; got.value != 2 || got.bitCount != 3 {
		t.Fatalf("code for local DHT symbol 3 = (%b,%d); want (010,3)", got.value, got.bitCount)
	}
}

func TestJPEGHuffmanTableRejectsInvalidCodeTrees(t *testing.T) {
	tests := []struct {
		name    string
		counts  [16]uint8
		symbols []uint8
	}{
		{name: "oversubscribed", counts: [16]uint8{3}, symbols: []uint8{0, 1, 2}},
		{name: "all-ones code", counts: [16]uint8{2}, symbols: []uint8{0, 1}},
		{name: "symbol count mismatch", counts: [16]uint8{1}, symbols: nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := newJPEGHuffmanTable(test.counts, test.symbols); !errors.Is(err, ErrJPEGEncoderInvalidHuffmanTable) {
				t.Fatalf("newJPEGHuffmanTable() error = %v; want %v", err, ErrJPEGEncoderInvalidHuffmanTable)
			}
		})
	}
}

func TestJPEGEncodeDCCoefficientDifference(t *testing.T) {
	var counts [16]uint8
	counts[2] = 4
	table, err := newJPEGHuffmanTable(counts, []uint8{0, 1, 2, 3})
	if err != nil {
		t.Fatalf("newJPEGHuffmanTable() error: %v", err)
	}
	codewords, err := jpegEncodeDC(-3, 0, table)
	if err != nil {
		t.Fatalf("jpegEncodeDC() error: %v", err)
	}
	want := []jpegCodeword{{value: 2, bitCount: 3}, {value: 0, bitCount: 2}}
	if !equalJPEGCodewords(codewords, want) {
		t.Fatalf("DC codewords = %v; want %v", codewords, want)
	}
	zero, err := jpegEncodeDC(7, 7, table)
	if err != nil {
		t.Fatalf("zero DC difference error: %v", err)
	}
	if wantZero := []jpegCodeword{{value: 0, bitCount: 3}}; !equalJPEGCodewords(zero, wantZero) {
		t.Fatalf("zero-difference codewords = %v; want %v", zero, wantZero)
	}
}

func TestJPEGEncodeACZeroRunsAndAmplitudes(t *testing.T) {
	var counts [16]uint8
	counts[2] = 4
	table, err := newJPEGHuffmanTable(counts, []uint8{0x03, 0xf0, 0x02, 0x00})
	if err != nil {
		t.Fatalf("newJPEGHuffmanTable() error: %v", err)
	}
	var coefficients [64]int16
	coefficients[1] = 5
	coefficients[18] = -2
	codewords, err := jpegEncodeAC(coefficients, table)
	if err != nil {
		t.Fatalf("jpegEncodeAC() error: %v", err)
	}
	want := []jpegCodeword{
		{value: 0, bitCount: 3}, {value: 5, bitCount: 3},
		{value: 1, bitCount: 3},
		{value: 2, bitCount: 3}, {value: 1, bitCount: 2},
		{value: 3, bitCount: 3},
	}
	if !equalJPEGCodewords(codewords, want) {
		t.Fatalf("AC codewords = %v; want %v", codewords, want)
	}
}

func TestJPEGBitWriterPadsWithOnesAndStuffsFFBytes(t *testing.T) {
	var writer jpegBitWriter
	if err := writer.WriteBits(5, 3); err != nil {
		t.Fatalf("WriteBits() error: %v", err)
	}
	if err := writer.WriteBits(255, 8); err != nil {
		t.Fatalf("WriteBits() error: %v", err)
	}
	if got, want := writer.Finish(), []byte{0xbf, 0xff, 0x00}; !bytes.Equal(got, want) {
		t.Fatalf("stuffed bits = %v; want %v", got, want)
	}
	if err := writer.WriteBits(0, 1); !errors.Is(err, ErrJPEGEncoderBitWriterFinished) {
		t.Fatalf("write after Finish() error = %v; want %v", err, ErrJPEGEncoderBitWriterFinished)
	}

	var partial jpegBitWriter
	if err := partial.WriteBits(5, 3); err != nil {
		t.Fatalf("partial WriteBits() error: %v", err)
	}
	if got, want := partial.Finish(), []byte{0xbf}; !bytes.Equal(got, want) {
		t.Fatalf("one-padded bits = %v; want %v", got, want)
	}
}

func TestJPEGMarkerPayloadsEncodeJFIF444Layout(t *testing.T) {
	if got, want := jpegJFIFAPP0Payload(), []byte{'J', 'F', 'I', 'F', 0, 1, 1, 0, 0, 1, 0, 1, 0, 0}; !bytes.Equal(got, want) {
		t.Fatalf("APP0 payload = %v; want %v", got, want)
	}
	sof, err := jpegSOF0Payload(0x1234, 0x5678)
	if err != nil {
		t.Fatalf("jpegSOF0Payload() error: %v", err)
	}
	wantSOF := []byte{8, 0x56, 0x78, 0x12, 0x34, 3, 1, 0x11, 0, 2, 0x11, 1, 3, 0x11, 1}
	if !bytes.Equal(sof, wantSOF) {
		t.Fatalf("SOF0 payload = %v; want %v", sof, wantSOF)
	}
	if got, want := jpegSOSPayload(), []byte{3, 1, 0, 2, 0x11, 3, 0x11, 0, 63, 0}; !bytes.Equal(got, want) {
		t.Fatalf("SOS payload = %v; want %v", got, want)
	}
}

func TestJPEGEncoderDecodesOddDimensionsWithPaddedStride(t *testing.T) {
	buffer, err := NewPixelBuffer(3, 5, 12, 3, ColorRangeFull, []byte{
		255, 0, 0, 0, 255, 0, 0, 0, 255, 99, 99, 99,
		0, 0, 0, 255, 255, 255, 128, 128, 128, 98, 98, 98,
		20, 40, 60, 80, 100, 120, 140, 160, 180, 97, 97, 97,
		200, 180, 160, 140, 120, 100, 80, 60, 40, 96, 96, 96,
		10, 30, 50, 70, 90, 110, 130, 150, 170, 95, 95, 95,
	})
	if err != nil {
		t.Fatalf("NewPixelBuffer() error: %v", err)
	}
	encoded, err := NewJPEGEncoder().Encode(buffer)
	if err != nil {
		t.Fatalf("Encode() error: %v", err)
	}
	decoded, err := jpeg.Decode(bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("jpeg.Decode() error: %v", err)
	}
	if decoded.Bounds().Dx() != 3 || decoded.Bounds().Dy() != 5 {
		t.Fatalf("decoded dimensions = %dx%d; want 3x5", decoded.Bounds().Dx(), decoded.Bounds().Dy())
	}
}

func TestJPEGEncoderStructureAndPixelErrorBounds(t *testing.T) {
	cases := []struct {
		name          string
		width         int
		height        int
		highFrequency bool
		maxError      int
		meanError     float64
	}{
		{name: "single-pixel", width: 1, height: 1, maxError: 24, meanError: 8},
		{name: "tiny-gradient", width: 2, height: 2, maxError: 24, meanError: 8},
		{name: "odd-gradient", width: 3, height: 5, maxError: 24, meanError: 8},
		{name: "block-gradient", width: 8, height: 8, maxError: 24, meanError: 8},
		{name: "high-frequency", width: 8, height: 8, highFrequency: true, maxError: 64, meanError: 20},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			width, height := test.width, test.height
			stride := width*3 + 2
			pixels := make([]byte, stride*height)
			for y := 0; y < height; y++ {
				for x := 0; x < width; x++ {
					offset := y*stride + x*3
					if test.highFrequency {
						pixels[offset] = byte((x*71 + y*31) & 0xff)
						pixels[offset+1] = byte((x*13 + y*83) & 0xff)
						pixels[offset+2] = byte((x*149 + y*7) & 0xff)
					} else {
						pixels[offset] = uint8(16 + x*20 + y*5)
						pixels[offset+1] = uint8(32 + x*8 + y*15)
						pixels[offset+2] = uint8(64 + x*10 + y*9)
					}
				}
			}
			buffer, err := NewPixelBuffer(width, height, stride, 3, ColorRangeFull, pixels)
			if err != nil {
				t.Fatalf("NewPixelBuffer() error: %v", err)
			}
			encoded, err := NewJPEGEncoder().Encode(buffer)
			if err != nil {
				t.Fatalf("Encode() error: %v", err)
			}
			validateBaselineJPEGStructure(t, encoded, width, height)
			decoded, err := jpeg.Decode(bytes.NewReader(encoded))
			if err != nil {
				t.Fatalf("jpeg.Decode() error: %v", err)
			}
			if decoded.Bounds().Dx() != width || decoded.Bounds().Dy() != height {
				t.Fatalf("decoded dimensions = %dx%d; want %dx%d", decoded.Bounds().Dx(), decoded.Bounds().Dy(), width, height)
			}
			maximumError := 0
			totalError := 0
			for y := 0; y < height; y++ {
				for x := 0; x < width; x++ {
					offset := y*stride + x*3
					r, g, b, _ := decoded.At(x, y).RGBA()
					for channel, got := range []int{int(r >> 8), int(g >> 8), int(b >> 8)} {
						difference := got - int(pixels[offset+channel])
						if difference < 0 {
							difference = -difference
						}
						if difference > maximumError {
							maximumError = difference
						}
						totalError += difference
					}
				}
			}
			meanError := float64(totalError) / float64(width*height*3)
			if maximumError > test.maxError || meanError > test.meanError {
				t.Fatalf("RGB channel errors exceed tolerance: maximum=%d (limit %d), mean=%.2f (limit %.2f)", maximumError, test.maxError, meanError, test.meanError)
			}
		})
	}
}

func validateBaselineJPEGStructure(t *testing.T, encoded []byte, width, height int) {
	t.Helper()
	if len(encoded) < 4 || !bytes.Equal(encoded[:2], []byte{0xff, 0xd8}) || !bytes.Equal(encoded[len(encoded)-2:], []byte{0xff, 0xd9}) {
		t.Fatal("JPEG is missing SOI or EOI")
	}
	offset := 2
	wantMarkers := []byte{0xe0, 0xdb, 0xc0, 0xc4, 0xda}
	var sawJFIF, sawDQT, sawSOF, sawDHT, sawSOS bool
	for _, wantMarker := range wantMarkers {
		if offset+4 > len(encoded) || encoded[offset] != 0xff || encoded[offset+1] != wantMarker {
			t.Fatalf("marker at offset %d = %v; want FF %02X", offset, encoded[offset:min(offset+2, len(encoded))], wantMarker)
		}
		length := int(binary.BigEndian.Uint16(encoded[offset+2 : offset+4]))
		segmentEnd := offset + 2 + length
		if length < 2 || segmentEnd > len(encoded) {
			t.Fatalf("marker FF%02X has invalid length %d", wantMarker, length)
		}
		payload := encoded[offset+4 : segmentEnd]
		switch wantMarker {
		case 0xe0:
			sawJFIF = len(payload) == 14 && bytes.Equal(payload[:5], []byte{'J', 'F', 'I', 'F', 0})
		case 0xdb:
			sawDQT = len(payload) == 130 && payload[0] == 0 && payload[65] == 1
			for index, value := range payload {
				if index%65 != 0 && value == 0 {
					t.Fatalf("DQT contains zero quantizer at payload index %d", index)
				}
			}
		case 0xc0:
			sawSOF = len(payload) == 15 && payload[0] == 8 && int(binary.BigEndian.Uint16(payload[1:3])) == height &&
				int(binary.BigEndian.Uint16(payload[3:5])) == width && payload[5] == 3 && payload[7] == 0x11 && payload[10] == 0x11 && payload[13] == 0x11
		case 0xc4:
			sawDHT = validateJPEGDHTPayload(t, payload)
		case 0xda:
			sawSOS = len(payload) == 10 && payload[0] == 3 && payload[7] == 0 && payload[8] == 63 && payload[9] == 0
		}
		offset = segmentEnd
	}
	if !sawJFIF || !sawDQT || !sawSOF || !sawDHT || !sawSOS {
		t.Fatalf("incomplete baseline JFIF markers: APP0=%t DQT=%t SOF0=%t DHT=%t SOS=%t", sawJFIF, sawDQT, sawSOF, sawDHT, sawSOS)
	}
	for offset < len(encoded)-2 {
		if encoded[offset] == 0xff {
			if offset+1 >= len(encoded)-2 || encoded[offset+1] != 0 {
				t.Fatalf("unstuffed FF byte in entropy scan at offset %d", offset)
			}
			offset += 2
			continue
		}
		offset++
	}
	if offset != len(encoded)-2 {
		t.Fatalf("entropy scan does not terminate immediately before EOI: offset=%d length=%d", offset, len(encoded))
	}
}

func validateJPEGDHTPayload(t *testing.T, payload []byte) bool {
	t.Helper()
	for offset := 0; offset < len(payload); {
		if offset+17 > len(payload) {
			return false
		}
		selector := payload[offset]
		if selector>>4 > 1 || selector&0x0f > 3 {
			return false
		}
		count := 0
		for _, value := range payload[offset+1 : offset+17] {
			count += int(value)
		}
		offset += 17
		if offset+count > len(payload) {
			return false
		}
		offset += count
	}
	return len(payload) > 0
}

func equalJPEGCodewords(left, right []jpegCodeword) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
