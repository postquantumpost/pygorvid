package vid

import (
	"errors"
	"fmt"
	"io"
	"math"
)

const DefaultJPEGQuality = 75

var ErrJPEGEncoderInvalidPixelBuffer = errors.New("JPEG encoder requires a valid RGB pixel buffer")
var ErrJPEGEncoderInvalidWriter = errors.New("JPEG encoder requires a valid writer")
var ErrJPEGEncoderInvalidDimensions = errors.New("JPEG dimensions must be between 1 and 65535")
var ErrJPEGEncoderImageTooLarge = errors.New("JPEG image exceeds the addressable size limit")
var ErrJPEGEncoderInvalidQuality = errors.New("JPEG quality must be between 1 and 100")
var ErrJPEGEncoderInvalidMarker = errors.New("JPEG marker is invalid for the requested segment")
var ErrJPEGEncoderSegmentTooLarge = errors.New("JPEG marker segment exceeds the format limit")

type JPEGEncoder struct {
	Quality int
}

func NewJPEGEncoder() JPEGEncoder {
	return JPEGEncoder{Quality: DefaultJPEGQuality}
}

func NewJPEGEncoderWithQuality(quality int) (JPEGEncoder, error) {
	if quality < 1 || quality > 100 {
		return JPEGEncoder{}, ErrJPEGEncoderInvalidQuality
	}
	return JPEGEncoder{Quality: quality}, nil
}

func (e JPEGEncoder) Encode(buffer PixelBuffer) ([]byte, error) {
	if e.Quality < 1 || e.Quality > 100 {
		return nil, ErrJPEGEncoderInvalidQuality
	}
	if err := validateJPEGPixelBuffer(buffer); err != nil {
		return nil, err
	}
	image, err := convertRGBToYCbCr444(buffer)
	if err != nil {
		return nil, err
	}
	lumaQuantization, chromaQuantization, err := jpegQuantizationTablesForQuality(e.Quality)
	if err != nil {
		return nil, err
	}
	huffmanSpecs := jpegDefaultHuffmanSpecs()
	scan, err := jpegEncodeScan(image, lumaQuantization, chromaQuantization, huffmanSpecs)
	if err != nil {
		return nil, err
	}
	output, err := appendJPEGFrameHeaders(buffer.Width, buffer.Height, lumaQuantization, chromaQuantization, huffmanSpecs)
	if err != nil {
		return nil, err
	}
	output = append(output, scan...)
	return appendJPEGMarker(output, 0xd9, nil)
}

func validateJPEGPixelBuffer(buffer PixelBuffer) error {
	if buffer.Width <= 0 || buffer.Height <= 0 || buffer.Stride <= 0 || buffer.Channels != 3 {
		return ErrJPEGEncoderInvalidPixelBuffer
	}
	if buffer.Width > math.MaxUint16 || buffer.Height > math.MaxUint16 {
		return ErrJPEGEncoderInvalidDimensions
	}
	if buffer.ColorRange != ColorRangeLimited && buffer.ColorRange != ColorRangeFull {
		return ErrJPEGEncoderInvalidPixelBuffer
	}
	if buffer.Width > int(^uint(0)>>1)/3 {
		return ErrJPEGEncoderInvalidDimensions
	}
	rowBytes := buffer.Width * 3
	if buffer.Stride < rowBytes || buffer.Height > len(buffer.Data)/buffer.Stride {
		return ErrJPEGEncoderInvalidPixelBuffer
	}
	maxInt := int(^uint(0) >> 1)
	if buffer.Width > maxInt/buffer.Height {
		return ErrJPEGEncoderImageTooLarge
	}
	return nil
}

type jpegYCbCr444 struct {
	width  int
	height int
	y      []byte
	cb     []byte
	cr     []byte
}

func convertRGBToYCbCr444(buffer PixelBuffer) (jpegYCbCr444, error) {
	if err := validateJPEGPixelBuffer(buffer); err != nil {
		return jpegYCbCr444{}, err
	}
	pixelCount := buffer.Width * buffer.Height
	converted := jpegYCbCr444{
		width:  buffer.Width,
		height: buffer.Height,
		y:      make([]byte, pixelCount),
		cb:     make([]byte, pixelCount),
		cr:     make([]byte, pixelCount),
	}
	for row := 0; row < buffer.Height; row++ {
		source := row * buffer.Stride
		for column := 0; column < buffer.Width; column++ {
			sourceOffset := source + column*3
			destinationOffset := row*buffer.Width + column
			y, cb, cr := jpegRGBToYCbCr(buffer.Data[sourceOffset], buffer.Data[sourceOffset+1], buffer.Data[sourceOffset+2])
			converted.y[destinationOffset] = y
			converted.cb[destinationOffset] = cb
			converted.cr[destinationOffset] = cr
		}
	}
	return converted, nil
}

func jpegRGBToYCbCr(red, green, blue uint8) (uint8, uint8, uint8) {
	r, g, b := int(red), int(green), int(blue)
	y := (19595*r + 38470*g + 7471*b + 32768) >> 16
	cb := (-11059*r - 21709*g + 32768*b + (128 << 16) + 32768) >> 16
	cr := (32768*r - 27439*g - 5329*b + (128 << 16) + 32768) >> 16
	return clampJPEGByte(y), clampJPEGByte(cb), clampJPEGByte(cr)
}

func clampJPEGByte(value int) uint8 {
	if value < 0 {
		return 0
	}
	if value > 255 {
		return 255
	}
	return uint8(value)
}

func (e JPEGEncoder) EncodeBuffer(buffer PixelBuffer) ([]byte, error) {
	return e.Encode(buffer)
}

func (e JPEGEncoder) Write(writer io.Writer, buffer PixelBuffer) error {
	if writer == nil {
		return ErrJPEGEncoderInvalidWriter
	}
	payload, err := e.Encode(buffer)
	if err != nil {
		return err
	}
	if _, err := writer.Write(payload); err != nil {
		return fmt.Errorf("JPEG encoder write failed: %w", err)
	}
	return nil
}

func appendJPEGMarker(output []byte, marker byte, payload []byte) ([]byte, error) {
	if marker == 0x00 || marker == 0xff {
		return nil, ErrJPEGEncoderInvalidMarker
	}
	if isJPEGStandaloneMarker(marker) {
		if len(payload) != 0 {
			return nil, ErrJPEGEncoderInvalidMarker
		}
		return append(output, 0xff, marker), nil
	}
	if len(payload) > math.MaxUint16-2 {
		return nil, ErrJPEGEncoderSegmentTooLarge
	}
	length := uint16(len(payload) + 2)
	output = append(output, 0xff, marker, byte(length>>8), byte(length))
	output = append(output, payload...)
	return output, nil
}

func isJPEGStandaloneMarker(marker byte) bool {
	return marker == 0x01 || marker == 0xd8 || marker == 0xd9 || marker >= 0xd0 && marker <= 0xd7
}
