package vid

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestCAVLCIPBFramesMatchReference(t *testing.T) {
	t.Setenv("PATH", "")
	root := filepath.Join("..", "..", "testdata", "h264")
	expected, err := os.ReadFile(filepath.Join(root, "cavlc-32x32-reference.yuv"))
	if err != nil {
		t.Fatal(err)
	}
	for _, threads := range []int{1, 4} {
		reader, err := OpenVideoSampleReader(filepath.Join(root, "cavlc-32x32.mp4"))
		if err != nil {
			t.Fatal(err)
		}
		decoder := NewH264Decoder(reader)
		decoder.SetThreads(threads)
		for index := 0; index < reader.SampleCount(); index++ {
			frame, err := decoder.DecodeFrame(uint64(index))
			if err != nil {
				reader.Close()
				t.Fatalf("frame %d: %v", index, err)
			}
			actual := append(append(append([]byte(nil), frame.Y...), frame.U...), frame.V...)
			start := index * 32 * 32 * 3 / 2
			if !bytes.Equal(actual, expected[start:start+len(actual)]) {
				t.Fatalf("CAVLC frame %d differs, threads %d", index, threads)
			}
		}
		reader.Close()
	}
}

func cavlcBits(value string) []byte {
	data := make([]byte, (len(value)+7)/8)
	for index, bit := range value {
		data[index/8] |= byte(bit-'0') << (7 - index%8)
	}
	return data
}

func TestCAVLCResidualVectors(t *testing.T) {
	for _, test := range []struct {
		code      string
		nc, count int
		nonzero   map[int]int32
	}{
		{"1", 0, 16, nil}, {"0101", 0, 16, map[int]int32{0: 1}},
		{"011011", 0, 16, map[int]int32{1: -1}}, {"00101111", 0, 16, map[int]int32{0: -1, 1: 1}},
		{"101", -1, 4, map[int]int32{0: 1}}, {"000011", 8, 16, nil},
	} {
		reader := NewBitReader(cavlcBits(test.code))
		block, err := DecodeCAVLCResidualBlock(reader, test.nc, test.count)
		if err != nil {
			t.Fatalf("%s: %v", test.code, err)
		}
		expected := make([]int32, test.count)
		for index, value := range test.nonzero {
			expected[index] = value
		}
		if !reflect.DeepEqual(block.Coefficients, expected) || reader.bitOffset != uint64(len(test.code)) {
			t.Fatalf("%s: got %v at %d, want %v at %d", test.code, block, reader.bitOffset, expected, len(test.code))
		}
	}
	reader := NewBitReader([]byte{0})
	if _, err := DecodeCAVLCResidualBlock(reader, 0, 16); err == nil || reader.bitOffset != 0 {
		t.Fatal("truncated residual did not roll back")
	}
}
