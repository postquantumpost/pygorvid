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
	if err != nil || !bytes.Contains(payload, []byte(value)) ||
		len(payload)-len(plain) != 69+32+len("title")+len(value) {
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
