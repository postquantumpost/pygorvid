package vid

import (
	"bytes"
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
