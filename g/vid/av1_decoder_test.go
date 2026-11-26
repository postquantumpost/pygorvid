package vid

import (
	"errors"
	"testing"
)

func TestAV1DecoderIsExplicitlyUnimplemented(t *testing.T) {
	decoder := NewAV1Decoder(nil)
	err := decoder.CheckAvailable()
	if !errors.Is(err, ErrAV1NotImplemented) {
		t.Fatalf("unexpected availability error: %v", err)
	}
}
