package vid

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestHEVCSampleEntriesAndConfiguration(t *testing.T) {
	data := make([]byte, 23)
	data[0], data[1], data[12] = 1, 2, 120
	data[13], data[15], data[16] = 0xf0, 0xfc, 0xfd
	data[17], data[18], data[21], data[22] = 0xfa, 0xfa, 3, 3
	for _, nalType := range []byte{32, 33, 34} {
		data = append(data, 0x80|nalType, 0, 1, 0, 3, nalType<<1, 1, 0x80)
	}
	parse := func(kind string, payload []byte) ([]box, error) {
		encoded := mkbox(kind, make([]byte, 78), mkbox("hvcC", payload))
		return readBoxes(bytes.NewReader(encoded), 0, int64(len(encoded)))
	}
	for _, kind := range []string{"hvc1", "hev1"} {
		boxes, err := parse(kind, data)
		if err != nil {
			t.Fatal(err)
		}
		entry, ok := boxes[0].(*hevcSampleEntryBox)
		if !ok {
			t.Fatalf("unexpected sample entry %T", boxes[0])
		}
		config := entry.children[0].(*hvcCBox)
		if config.profile != 2 || config.level != 120 || config.bitDepthLuma != 10 || config.bitDepthChroma != 10 || config.nalLengthSize != 4 || len(config.arrays) != 3 {
			t.Fatalf("unexpected HEVC configuration: %#v", config)
		}
		for index, array := range config.arrays {
			nalType := byte(32 + index)
			if !array.complete || array.nalType != nalType || len(array.units) != 1 || !bytes.Equal(array.units[0], []byte{nalType << 1, 1, 0x80}) {
				t.Fatalf("unexpected HEVC NAL array: %#v", array)
			}
		}
	}
	for length := range len(data) {
		if _, err := parse("hvc1", data[:length]); err == nil {
			t.Fatalf("accepted truncated hvcC at %d", length)
		}
	}
	for _, mutation := range [][2]int{{0, 2}, {13, 0}, {15, 0}, {16, 0}, {17, 0}, {18, 0}, {21, 2}, {23, 0x60}, {26, 0xff}, {28, 0}, {29, 0}} {
		invalid := bytes.Clone(data)
		invalid[mutation[0]] = byte(mutation[1])
		if _, err := parse("hvc1", invalid); err == nil {
			t.Fatalf("accepted invalid hvcC at %d", mutation[0])
		}
	}
	if _, err := parse("hvc1", append(data, 0)); err == nil {
		t.Fatal("accepted trailing hvcC data")
	}
}

func TestMp4File(t *testing.T) {
	dir := t.TempDir()
	mp4 := filepath.Join(dir, "a.mp4")
	os.WriteFile(mp4, []byte("\x00\x00\x00\x18ftypisom"), 0o644)
	mkv := filepath.Join(dir, "a.mkv")
	os.WriteFile(mkv, []byte("\x1a\x45\xdf\xa3rest"), 0o644)

	m := NewMp4File()
	if m.IsOpen() || m.ErrorInfo() != "" {
		t.Fatal("new Mp4File should be idle")
	}
	if !m.Open(mp4) || !m.IsOpen() {
		t.Fatalf("open mp4: %q", m.ErrorInfo())
	}
	m.Close()
	if m.IsOpen() {
		t.Error("still open after Close")
	}
	if m.Open(mkv) || m.ErrorInfo() == "" {
		t.Error("mkv should be rejected with an error")
	}

	v := OpenFile(mp4)
	if !v.IsOpen() || v.Format != "mp4" || v.mp4 == nil || !v.mp4.IsOpen() {
		t.Errorf("VidFile should hold an open Mp4File: %q", v.ErrorInfo())
	}
	v.Close()
	if v.IsOpen() {
		t.Error("VidFile still open after Close")
	}
}
