package vid

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"image/png"
	"io"
	"testing"
)

func TestPNGEncoderRejectsInvalidInput(t *testing.T) {
	encoder := NewPNGEncoder()
	_, err := encoder.Encode(PixelBuffer{Width: 2, Height: 2, Stride: 2, Channels: 3, Data: []byte{1, 2, 3}})
	if !errors.Is(err, ErrPNGEncoderInvalidPixelBuffer) {
		t.Fatalf("Encode() err = %v; want %v", err, ErrPNGEncoderInvalidPixelBuffer)
	}
	_, err = encoder.Encode(PixelBuffer{Width: 2, Height: 2, Stride: 6, Channels: 1, Data: []byte{0, 0, 0, 0, 0, 0, 0, 0}})
	if !errors.Is(err, ErrPNGEncoderInvalidPixelBuffer) {
		t.Fatalf("Encode() err = %v; want %v", err, ErrPNGEncoderInvalidPixelBuffer)
	}
}

func TestPNGEncoderWritesFilterZeroScanlinesAndIgnoresRowPadding(t *testing.T) {
	encoder := NewPNGEncoder()
	buf, err := NewPixelBuffer(2, 2, 8, 3, ColorRangeFull, []byte{
		10, 20, 30, 40, 50, 60, 99, 99,
		70, 80, 90, 100, 110, 120, 88, 88,
	})
	if err != nil {
		t.Fatalf("NewPixelBuffer() error: %v", err)
	}
	encoded, err := encoder.Encode(buf)
	if err != nil {
		t.Fatalf("Encode() error: %v", err)
	}
	compressed := readPNGIDAT(t, encoded)
	reader, err := zlib.NewReader(bytes.NewReader(compressed))
	if err != nil {
		t.Fatalf("zlib.NewReader() error: %v", err)
	}
	decoded, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read IDAT zlib stream: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("close zlib reader: %v", err)
	}
	want := []byte{
		0, 10, 20, 30, 40, 50, 60,
		0, 70, 80, 90, 100, 110, 120,
	}
	if !bytes.Equal(decoded, want) {
		t.Fatalf("decoded scanlines = %v; want %v", decoded, want)
	}
}

func TestPNGEncoderRoundTripsOddDimensionsAndStride(t *testing.T) {
	const width, height, stride = 3, 3, 12
	pixels := []byte{
		1, 2, 3, 4, 5, 6, 7, 8, 9, 201, 202, 203,
		10, 11, 12, 13, 14, 15, 16, 17, 18, 204, 205, 206,
		19, 20, 21, 22, 23, 24, 25, 26, 27, 207, 208, 209,
	}
	buffer, err := NewPixelBuffer(width, height, stride, 3, ColorRangeFull, pixels)
	if err != nil {
		t.Fatalf("NewPixelBuffer() error: %v", err)
	}
	encoded, err := NewPNGEncoder().Encode(buffer)
	if err != nil {
		t.Fatalf("Encode() error: %v", err)
	}
	config, err := png.DecodeConfig(bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("png.DecodeConfig() error: %v", err)
	}
	if config.Width != width || config.Height != height {
		t.Fatalf("decoded dimensions = %dx%d; want %dx%d", config.Width, config.Height, width, height)
	}
	decoded, err := png.Decode(bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("png.Decode() error: %v", err)
	}
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			offset := y*stride + x*3
			r, g, b, a := decoded.At(x, y).RGBA()
			if got := []uint8{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), uint8(a >> 8)}; !bytes.Equal(got, []byte{pixels[offset], pixels[offset+1], pixels[offset+2], 255}) {
				t.Fatalf("pixel (%d,%d) = %v; want RGB(%d,%d,%d) opaque", x, y, got, pixels[offset], pixels[offset+1], pixels[offset+2])
			}
		}
	}
}

func TestPNGEncoderDecodesMultiMegabyteImage(t *testing.T) {
	const width, height = 1024, 1024
	stride := width * 3
	pixels := make([]byte, stride*height)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			offset := y*stride + x*3
			pixels[offset] = byte(x)
			pixels[offset+1] = byte(y)
			pixels[offset+2] = byte(x ^ y)
		}
	}
	buffer, err := NewPixelBuffer(width, height, stride, 3, ColorRangeFull, pixels)
	if err != nil {
		t.Fatalf("NewPixelBuffer() error: %v", err)
	}
	encoded, err := NewPNGEncoder().Encode(buffer)
	if err != nil {
		t.Fatalf("Encode() error: %v", err)
	}
	if len(encoded) <= 3*1024*1024 {
		t.Fatalf("PNG output length = %d; want more than 3 MiB", len(encoded))
	}
	config, err := png.DecodeConfig(bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("png.DecodeConfig() error: %v", err)
	}
	if config.Width != width || config.Height != height {
		t.Fatalf("decoded dimensions = %dx%d; want %dx%d", config.Width, config.Height, width, height)
	}
	decoded, err := png.Decode(bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("png.Decode() error: %v", err)
	}
	for _, point := range [][2]int{{0, 0}, {width / 2, height / 2}, {width - 1, height - 1}} {
		x, y := point[0], point[1]
		r, g, b, a := decoded.At(x, y).RGBA()
		if got, want := []uint8{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), uint8(a >> 8)}, []uint8{byte(x), byte(y), byte(x ^ y), 255}; !bytes.Equal(got, want) {
			t.Fatalf("pixel (%d,%d) = %v; want %v", x, y, got, want)
		}
	}
}

func TestFramePNGChunksWritesSignatureAndOrderedChunks(t *testing.T) {
	idat := []byte{0x78, 0x01, 0x02, 0x03}
	encoded, err := framePNGChunks(2, 3, idat)
	if err != nil {
		t.Fatalf("framePNGChunks() error: %v", err)
	}
	if !bytes.Equal(encoded[:len(pngSignature)], pngSignature[:]) {
		t.Fatalf("PNG signature = %v; want %v", encoded[:len(pngSignature)], pngSignature)
	}

	reader := bytes.NewReader(encoded[len(pngSignature):])
	wantTypes := []string{"IHDR", "IDAT", "IEND"}
	for index, wantType := range wantTypes {
		var length uint32
		if err := binary.Read(reader, binary.BigEndian, &length); err != nil {
			t.Fatalf("read %s length: %v", wantType, err)
		}
		chunk := make([]byte, 4+int(length))
		if _, err := reader.Read(chunk); err != nil {
			t.Fatalf("read %s chunk: %v", wantType, err)
		}
		if gotType := string(chunk[:4]); gotType != wantType {
			t.Fatalf("chunk %d type = %q; want %q", index, gotType, wantType)
		}
		var gotCRC uint32
		if err := binary.Read(reader, binary.BigEndian, &gotCRC); err != nil {
			t.Fatalf("read %s CRC: %v", wantType, err)
		}
		if wantCRC := pngCRC32(chunk); gotCRC != wantCRC {
			t.Fatalf("%s CRC = %#08x; want %#08x", wantType, gotCRC, wantCRC)
		}
		switch wantType {
		case "IHDR":
			if length != 13 || binary.BigEndian.Uint32(chunk[4:8]) != 2 || binary.BigEndian.Uint32(chunk[8:12]) != 3 {
				t.Fatalf("IHDR = %v; want 2x3 dimensions and 13-byte payload", chunk)
			}
			if !bytes.Equal(chunk[12:], []byte{8, 2, 0, 0, 0}) {
				t.Fatalf("IHDR format fields = %v; want 8-bit RGB, standard compression/filter, non-interlaced", chunk[12:])
			}
		case "IDAT":
			if !bytes.Equal(chunk[4:], idat) {
				t.Fatalf("IDAT = %v; want %v", chunk[4:], idat)
			}
		case "IEND":
			if length != 0 {
				t.Fatalf("IEND length = %d; want 0", length)
			}
		}
	}
	if reader.Len() != 0 {
		t.Fatalf("%d trailing bytes after IEND", reader.Len())
	}
}

func TestPNGCRC32KnownVector(t *testing.T) {
	if got := pngCRC32([]byte("123456789")); got != 0xcbf43926 {
		t.Fatalf("pngCRC32() = %#08x; want %#08x", got, uint32(0xcbf43926))
	}
}

func TestPNGAdler32KnownVectors(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want uint32
	}{
		{name: "empty", data: nil, want: 0x00000001},
		{name: "Wikipedia", data: []byte("Wikipedia"), want: 0x11e60398},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := pngAdler32(test.data); got != test.want {
				t.Fatalf("pngAdler32() = %#08x; want %#08x", got, test.want)
			}
		})
	}
}

func TestPNGZlibStoredBlockRoundTrips(t *testing.T) {
	for _, input := range [][]byte{nil, []byte{0, 1, 127, 255}, bytes.Repeat([]byte{0xa5}, 65535)} {
		encoded, err := pngZlibStoredBlock(input)
		if err != nil {
			t.Fatalf("pngZlibStoredBlock(%v) error: %v", input, err)
		}
		if !bytes.Equal(encoded[:2], []byte{0x78, 0x01}) {
			t.Fatalf("zlib header = %v; want [120 1]", encoded[:2])
		}
		if encoded[2] != 0x01 {
			t.Fatalf("stored block header = %#02x; want final, uncompressed block 0x01", encoded[2])
		}
		if len(encoded) < 11 || binary.LittleEndian.Uint16(encoded[3:5]) != uint16(len(input)) ||
			binary.LittleEndian.Uint16(encoded[5:7]) != ^uint16(len(input)) {
			t.Fatalf("stored block LEN/NLEN do not match input length %d: %v", len(input), encoded)
		}
		if !bytes.Equal(encoded[7:len(encoded)-4], input) {
			t.Fatalf("stored block payload = %v; want %v", encoded[7:len(encoded)-4], input)
		}
		if got := binary.BigEndian.Uint32(encoded[len(encoded)-4:]); got != pngAdler32(input) {
			t.Fatalf("zlib Adler-32 = %#08x; want %#08x", got, pngAdler32(input))
		}
		reader, err := zlib.NewReader(bytes.NewReader(encoded))
		if err != nil {
			t.Fatalf("zlib.NewReader() error: %v", err)
		}
		decoded, err := io.ReadAll(reader)
		if err != nil {
			t.Fatalf("read zlib stream: %v", err)
		}
		if err := reader.Close(); err != nil {
			t.Fatalf("close zlib reader: %v", err)
		}
		if !bytes.Equal(decoded, input) {
			t.Fatalf("decoded = %v; want %v", decoded, input)
		}
	}
}

func TestPNGZlibStoredBlockRejectsMoreThan65535Bytes(t *testing.T) {
	if _, err := pngZlibStoredBlock(make([]byte, 65536)); !errors.Is(err, ErrPNGEncoderStoredBlockTooLarge) {
		t.Fatalf("pngZlibStoredBlock() err = %v; want %v", err, ErrPNGEncoderStoredBlockTooLarge)
	}
}

func TestPNGZlibStoredBlocksRoundTripAcrossBlockBoundaries(t *testing.T) {
	input := make([]byte, 2*65535+17)
	for index := range input {
		input[index] = byte(index * 31)
	}
	encoded, err := pngZlibStoredBlocks(input)
	if err != nil {
		t.Fatalf("pngZlibStoredBlocks() error: %v", err)
	}
	reader, err := zlib.NewReader(bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("zlib.NewReader() error: %v", err)
	}
	decoded, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read zlib stream: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("close zlib reader: %v", err)
	}
	if !bytes.Equal(decoded, input) {
		t.Fatal("decoded zlib stream differs from input")
	}

	offset := 2
	for blockIndex, wantLength := range []int{65535, 65535, 17} {
		wantHeader := byte(0)
		if blockIndex == 2 {
			wantHeader = 1
		}
		if encoded[offset] != wantHeader {
			t.Fatalf("block %d header = %#02x; want %#02x", blockIndex, encoded[offset], wantHeader)
		}
		offset++
		length := binary.LittleEndian.Uint16(encoded[offset : offset+2])
		complement := binary.LittleEndian.Uint16(encoded[offset+2 : offset+4])
		if int(length) != wantLength || complement != ^length {
			t.Fatalf("block %d LEN/NLEN = %d/%d; want %d/%d", blockIndex, length, complement, wantLength, ^uint16(wantLength))
		}
		offset += 4 + wantLength
	}
	if offset+4 != len(encoded) {
		t.Fatalf("zlib trailer starts at %d for stream length %d", offset, len(encoded))
	}
}

func TestPNGEncoderHandlesImageAcrossStoredBlockBoundary(t *testing.T) {
	const width, height = 256, 86
	stride := width * 3
	pixels := make([]byte, stride*height)
	for index := range pixels {
		pixels[index] = byte(index * 13)
	}
	buffer, err := NewPixelBuffer(width, height, stride, 3, ColorRangeFull, pixels)
	if err != nil {
		t.Fatalf("NewPixelBuffer() error: %v", err)
	}
	encoded, err := NewPNGEncoder().Encode(buffer)
	if err != nil {
		t.Fatalf("Encode() error: %v", err)
	}
	reader, err := zlib.NewReader(bytes.NewReader(readPNGIDAT(t, encoded)))
	if err != nil {
		t.Fatalf("zlib.NewReader() error: %v", err)
	}
	scanlines, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read PNG scanlines: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("close zlib reader: %v", err)
	}
	if len(scanlines) != height*(stride+1) {
		t.Fatalf("decoded scanline length = %d; want %d", len(scanlines), height*(stride+1))
	}
	for row := 0; row < height; row++ {
		scanlineOffset := row * (stride + 1)
		if scanlines[scanlineOffset] != 0 {
			t.Fatalf("row %d filter = %d; want filter 0", row, scanlines[scanlineOffset])
		}
		if !bytes.Equal(scanlines[scanlineOffset+1:scanlineOffset+1+stride], pixels[row*stride:(row+1)*stride]) {
			t.Fatalf("decoded row %d differs from input RGB", row)
		}
	}
}

func TestFramePNGChunksRejectsZeroDimensions(t *testing.T) {
	if _, err := framePNGChunks(0, 1, nil); !errors.Is(err, ErrPNGEncoderInvalidDimensions) {
		t.Fatalf("framePNGChunks() err = %v; want %v", err, ErrPNGEncoderInvalidDimensions)
	}
}

func readPNGIDAT(t *testing.T, encoded []byte) []byte {
	t.Helper()
	if len(encoded) < len(pngSignature) || !bytes.Equal(encoded[:len(pngSignature)], pngSignature[:]) {
		t.Fatal("invalid PNG signature")
	}
	var idat []byte
	for offset := len(pngSignature); offset < len(encoded); {
		if len(encoded)-offset < 12 {
			t.Fatal("truncated PNG chunk header")
		}
		length := int(binary.BigEndian.Uint32(encoded[offset : offset+4]))
		chunkEnd := offset + 12 + length
		if chunkEnd > len(encoded) {
			t.Fatal("truncated PNG chunk data")
		}
		chunkType := string(encoded[offset+4 : offset+8])
		chunkData := encoded[offset+8 : offset+8+length]
		if got, want := binary.BigEndian.Uint32(encoded[offset+8+length:chunkEnd]), pngCRC32(encoded[offset+4:offset+8+length]); got != want {
			t.Fatalf("%s CRC = %#08x; want %#08x", chunkType, got, want)
		}
		if chunkType == "IDAT" {
			idat = append(idat, chunkData...)
		}
		offset = chunkEnd
		if chunkType == "IEND" {
			break
		}
	}
	return idat
}
