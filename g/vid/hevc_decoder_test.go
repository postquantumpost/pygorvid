package vid

import (
	"errors"
	"os"
	"testing"
)

func TestHEVCDecoderLicensing(t *testing.T) {
	for _, value := range []string{"unset", "", "false", "TRUE", "1", " true", "true ", "true"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("HEVC_IS_LICENSED", value)
			if value == "unset" {
				if err := os.Unsetenv("HEVC_IS_LICENSED"); err != nil {
					t.Fatal(err)
				}
			}
			decoder := NewHEVCDecoder(nil)
			_, err := decoder.DecodeFrame(0)
			var coded *CodecError
			if !errors.As(err, &coded) {
				t.Fatalf("missing typed codec error: %v", err)
			}
			want := ErrHEVCNotImplementedAndNotLicensed
			if value == "true" {
				want = ErrHEVCNotImplemented
			}
			if !errors.Is(err, want) || coded.Code != want.Code || err.Error() != want.Message {
				t.Fatalf("got %v (%s), want %v (%s)", err, coded.Code, want, want.Code)
			}
		})
	}
}
