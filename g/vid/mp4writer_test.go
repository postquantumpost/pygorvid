package vid

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
)

func TestMp4WriterOptionalMetadata(t *testing.T) {
	writer, encoder := NewMp4Writer(), NewH264Encoder()
	plain := writer.Encode(encoder)
	for _, metadata := range []*Mp4Metadata{nil, {}} {
		payload, err := writer.EncodeWithMetadata(encoder, metadata)
		if err != nil || !bytes.Equal(plain, payload) {
			t.Fatalf("empty metadata changed output: %v", err)
		}
	}
	value := "Caf\u00e9 \u65e5\u672c \U0001f4f7\x00tail"
	payload, err := writer.EncodeWithMetadata(encoder, &Mp4Metadata{Title: value})
	mdta, mdtaErr := mp4MetadataBox(&Mp4Metadata{Title: value})
	if err != nil || !bytes.Contains(payload, []byte(value)) ||
		mdtaErr != nil || len(mdta) != 69+32+len("title")+len(value) ||
		!bytes.Contains(payload, mdta) || !bytes.Contains(payload, []byte("mdirappl")) {
		t.Fatalf("UTF-8 metadata was not preserved: %v", err)
	}

	if bytes.Contains(plain, []byte("mdta")) || !bytes.Contains(payload, []byte("ilst")) {
		t.Fatal("metadata presence does not match argument")
	}

	for _, value := range []string{"\xff", "\xc0\x80", "\xed\xa0\x80", "\xf4\x90\x80\x80", "\xe2\x82"} {
		payload, err := writer.EncodeWithMetadata(encoder, &Mp4Metadata{Title: value})
		if payload != nil || err == nil || !strings.Contains(err.Error(), "title is not valid UTF-8") {
			t.Fatalf("invalid UTF-8 accepted: %x, %v", value, err)
		}
	}
}

func TestMp4CompatibilityNumbers(t *testing.T) {
	for _, value := range []string{"0", "1", "001", "65535", "65536", "-1", "1/2", "\u0661"} {
		payload, err := mp4CompatibilityMetadataBox(&Mp4Metadata{Track: value, Disc: value})
		if err != nil || !bytes.Contains(payload, []byte(value)) {
			t.Fatalf("raw number lost: %q", value)
		}

		binary := value == "0" || value == "1" || value == "001" || value == "65535"
		if bytes.Contains(payload, []byte("trkn")) != binary || bytes.Contains(payload, []byte("disk")) != binary {
			t.Fatalf("unexpected binary number tag for %q", value)
		}
	}
}

func TestMp4WindowsXtra(t *testing.T) {
	value := "A\u00e9\u65e5\U0001f4f7\x00tail"
	data := []byte{'A', 0, 0xe9, 0, 0xe5, 0x65, 0x3d, 0xd8, 0xf7, 0xdc, 0, 0,
		't', 0, 'a', 0, 'i', 0, 'l', 0, 0, 0}
	var records []byte
	for _, name := range []string{"Title", "pygorvid_title"} {
		header := make([]byte, 8)
		binary.BigEndian.PutUint32(header, uint32(18+len(name)+len(data)))
		binary.BigEndian.PutUint32(header[4:], uint32(len(name)))
		records = append(records, header...)
		records = append(records, []byte(name)...)
		header = make([]byte, 10)
		binary.BigEndian.PutUint32(header, 1)
		binary.BigEndian.PutUint32(header[4:], uint32(6+len(data)))
		binary.BigEndian.PutUint16(header[8:], 8)
		records = append(records, header...)
		records = append(records, data...)
	}
	expected := make([]byte, 8)
	binary.BigEndian.PutUint32(expected, uint32(8+len(records)))
	copy(expected[4:], "Xtra")
	expected = append(expected, records...)
	metadata := &Mp4Metadata{Title: value}
	actual, err := mp4WindowsMetadataBox(metadata)
	if err != nil || !bytes.Equal(actual, expected) {
		t.Fatalf("incorrect Xtra layout: %x, %v", actual, err)
	}
	payload, err := NewMp4Writer().EncodeWithMetadata(NewH264Encoder(), metadata)
	if err != nil || !bytes.Contains(payload, expected) {
		t.Fatalf("missing Xtra: %v", err)
	}
	for _, empty := range []*Mp4Metadata{nil, {}} {
		payload, err := mp4WindowsMetadataBox(empty)
		if err != nil || len(payload) != 0 {
			t.Fatalf("empty Xtra emitted: %v", err)
		}
	}
	for _, date := range []string{"2016", "2016-12-31 23:59:60 UTC", "2016junk", "-2016", "\u0662\u0660\u0661\u0666", ""} {
		payload, err := mp4WindowsMetadataBox(&Mp4Metadata{Date: date})
		year := date == "2016" || date == "2016-12-31 23:59:60 UTC"
		if err != nil || bytes.Contains(payload, []byte("WM/Year")) != year {
			t.Fatalf("incorrect year for %q: %v", date, err)
		}
	}
}
