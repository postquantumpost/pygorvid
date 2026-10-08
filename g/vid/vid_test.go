package vid

import "testing"

func TestDetectFormat(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"\x00\x00\x00\x18ftypisom", "mp4"},
		{"\x00\x00\x00\x14ftypqt  ", "mov"},
		{"\x1a\x45\xdf\xa3rest", "matroska"},
		{"RIFF\x00\x00\x00\x00AVI LIST", "avi"},
		{"", "unknown"},
	}
	for _, c := range cases {
		if got := DetectFormat([]byte(c.in)); got != c.want {
			t.Errorf("DetectFormat(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
