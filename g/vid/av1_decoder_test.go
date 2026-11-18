package vid

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestAV1DecoderIsExplicitlyUnimplemented(t *testing.T) {
	decoder := NewAV1Decoder(nil)
	err := decoder.CheckAvailable()
	if !errors.Is(err, ErrAV1NotImplemented) {
		t.Fatalf("unexpected availability error: %v", err)
	}
}

func TestDroneSampleSelectsAV1Decoder(t *testing.T) {
	reader, err := OpenVideoSampleReader(filepath.Join("..", "..", "samples", "drone64.mp4"))
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if reader.Codec() != CodecAV1 {
		t.Fatalf("unexpected codec: %q", reader.Codec())
	}
	configuration := reader.AV1Configuration()
	if configuration == nil || configuration.Profile != 0 || configuration.Level != 19 {
		t.Fatalf("unexpected AV1 configuration: %#v", configuration)
	}
	if _, err := NewVideoDecoder(reader); !errors.Is(err, ErrAV1NotImplemented) {
		t.Fatalf("unexpected decoder error: %v", err)
	}
}
