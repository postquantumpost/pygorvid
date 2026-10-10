package vid

import (
	"errors"
	"fmt"
	"io"
	"math"
)

var ErrPNGEncoderInvalidPixelBuffer = errors.New("PNG encoder requires a valid RGB pixel buffer")
var ErrPNGEncoderInvalidWriter = errors.New("PNG encoder requires a valid writer")
var ErrPNGEncoderInvalidDimensions = errors.New("PNG dimensions exceed the format limit")
var ErrPNGEncoderChunkTooLarge = errors.New("PNG chunk exceeds the format limit")
var ErrPNGEncoderImageTooLarge = errors.New("PNG image data exceeds the addressable size limit")
var ErrPNGEncoderStoredBlockTooLarge = errors.New("PNG zlib stored block exceeds 65535 bytes")

var pngSignature = [8]byte{137, 80, 78, 71, 13, 10, 26, 10}

// PNGEncoder writes non-interlaced, 8-bit RGB PNG images using filter type 0.
type PNGEncoder struct{}

func NewPNGEncoder() PNGEncoder {
	return PNGEncoder{}
}

func (e PNGEncoder) Encode(buffer PixelBuffer) ([]byte, error) {
	if buffer.Width <= 0 || buffer.Height <= 0 || buffer.Stride <= 0 || buffer.Channels <= 0 {
		return nil, ErrPNGEncoderInvalidPixelBuffer
	}
	if buffer.Channels != 3 {
		return nil, ErrPNGEncoderInvalidPixelBuffer
	}
	if buffer.ColorRange != ColorRangeLimited && buffer.ColorRange != ColorRangeFull {
		return nil, ErrPNGEncoderInvalidPixelBuffer
	}
	if uint64(buffer.Width) > math.MaxUint32 || uint64(buffer.Height) > math.MaxUint32 {
		return nil, ErrPNGEncoderInvalidDimensions
	}
	maxInt := int(^uint(0) >> 1)
	if buffer.Width > maxInt/3 {
		return nil, ErrPNGEncoderInvalidDimensions
	}
	rowBytes := buffer.Width * 3
	if buffer.Stride < rowBytes || buffer.Height > len(buffer.Data)/buffer.Stride {
		return nil, ErrPNGEncoderInvalidPixelBuffer
	}
	if rowBytes == maxInt || buffer.Height > maxInt/(rowBytes+1) {
		return nil, ErrPNGEncoderImageTooLarge
	}

	scanlines := make([]byte, buffer.Height*(rowBytes+1))
	for row := 0; row < buffer.Height; row++ {
		destination := row * (rowBytes + 1)
		scanlines[destination] = 0
		source := row * buffer.Stride
		copy(scanlines[destination+1:destination+1+rowBytes], buffer.Data[source:source+rowBytes])
	}
	zlibData, err := pngZlibStoredBlocks(scanlines)
	if err != nil {
		return nil, err
	}
	return framePNGChunks(uint32(buffer.Width), uint32(buffer.Height), zlibData)
}

func framePNGChunks(width, height uint32, idat []byte) ([]byte, error) {
	if width == 0 || height == 0 {
		return nil, ErrPNGEncoderInvalidDimensions
	}
	if uint64(len(idat)) > math.MaxUint32 {
		return nil, ErrPNGEncoderChunkTooLarge
	}

	ihdr := []byte{
		byte(width >> 24), byte(width >> 16), byte(width >> 8), byte(width),
		byte(height >> 24), byte(height >> 16), byte(height >> 8), byte(height),
		8, 2, 0, 0, 0,
	}
	output := make([]byte, 0, len(pngSignature)+25+len(idat)+12+12)
	output = append(output, pngSignature[:]...)
	var err error
	output, err = appendPNGChunk(output, "IHDR", ihdr)
	if err != nil {
		return nil, err
	}
	output, err = appendPNGChunk(output, "IDAT", idat)
	if err != nil {
		return nil, err
	}
	output, err = appendPNGChunk(output, "IEND", nil)
	if err != nil {
		return nil, err
	}
	return output, nil
}

func appendPNGChunk(output []byte, chunkType string, data []byte) ([]byte, error) {
	if len(chunkType) != 4 || uint64(len(data)) > math.MaxUint32 {
		return nil, ErrPNGEncoderChunkTooLarge
	}
	length := uint32(len(data))
	output = append(output, byte(length>>24), byte(length>>16), byte(length>>8), byte(length))
	chunkStart := len(output)
	output = append(output, chunkType...)
	output = append(output, data...)
	checksum := pngCRC32(output[chunkStart:])
	output = append(output, byte(checksum>>24), byte(checksum>>16), byte(checksum>>8), byte(checksum))
	return output, nil
}

func pngCRC32(data []byte) uint32 {
	crc := uint32(math.MaxUint32)
	for _, value := range data {
		crc ^= uint32(value)
		for bit := 0; bit < 8; bit++ {
			if crc&1 != 0 {
				crc = (crc >> 1) ^ 0xedb88320
			} else {
				crc >>= 1
			}
		}
	}
	return ^crc
}

func pngAdler32(data []byte) uint32 {
	const modulus = uint32(65521)
	const maxBlockSize = 5552
	a, b := uint32(1), uint32(0)
	for len(data) > 0 {
		blockSize := len(data)
		if blockSize > maxBlockSize {
			blockSize = maxBlockSize
		}
		for _, value := range data[:blockSize] {
			a += uint32(value)
			b += a
		}
		a %= modulus
		b %= modulus
		data = data[blockSize:]
	}
	return b<<16 | a
}

func pngZlibStoredBlock(data []byte) ([]byte, error) {
	if len(data) > math.MaxUint16 {
		return nil, ErrPNGEncoderStoredBlockTooLarge
	}

	length := uint16(len(data))
	complement := ^length
	output := make([]byte, 2+5+len(data)+4)
	output[0], output[1] = 0x78, 0x01
	output[2] = 0x01
	output[3], output[4] = byte(length), byte(length>>8)
	output[5], output[6] = byte(complement), byte(complement>>8)
	copy(output[7:], data)
	checksum := pngAdler32(data)
	trailer := len(output) - 4
	output[trailer] = byte(checksum >> 24)
	output[trailer+1] = byte(checksum >> 16)
	output[trailer+2] = byte(checksum >> 8)
	output[trailer+3] = byte(checksum)
	return output, nil
}

func pngZlibStoredBlocks(data []byte) ([]byte, error) {
	const maxBlockSize = math.MaxUint16
	blockCount := 1
	if len(data) > 0 {
		blockCount = (len(data)-1)/maxBlockSize + 1
	}
	maxInt := int(^uint(0) >> 1)
	if len(data) > maxInt-6 || blockCount > (maxInt-len(data)-6)/5 {
		return nil, ErrPNGEncoderImageTooLarge
	}

	output := make([]byte, 2, 2+len(data)+5*blockCount+4)
	output[0], output[1] = 0x78, 0x01
	for offset, blockIndex := 0, 0; blockIndex < blockCount; blockIndex++ {
		blockLength := len(data) - offset
		if blockLength > maxBlockSize {
			blockLength = maxBlockSize
		}
		blockHeader := byte(0)
		if blockIndex == blockCount-1 {
			blockHeader = 1
		}
		length := uint16(blockLength)
		complement := ^length
		output = append(output, blockHeader, byte(length), byte(length>>8), byte(complement), byte(complement>>8))
		output = append(output, data[offset:offset+blockLength]...)
		offset += blockLength
	}
	checksum := pngAdler32(data)
	output = append(output, byte(checksum>>24), byte(checksum>>16), byte(checksum>>8), byte(checksum))
	return output, nil
}

func (e PNGEncoder) EncodeBuffer(buffer PixelBuffer) ([]byte, error) {
	return e.Encode(buffer)
}

func (e PNGEncoder) Write(writer io.Writer, buffer PixelBuffer) error {
	if writer == nil {
		return ErrPNGEncoderInvalidWriter
	}
	payload, err := e.Encode(buffer)
	if err != nil {
		return err
	}
	if _, err := writer.Write(payload); err != nil {
		return fmt.Errorf("PNG encoder write failed: %w", err)
	}
	return nil
}
