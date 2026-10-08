// Package vid provides video container detection without ffmpeg.
package vid

import (
	"bytes"
	"io"
	"os"
)

const headerSize = 380

// DetectFormat returns a container name, or "unknown".
func DetectFormat(h []byte) string {
	at := func(off int, s string) bool {
		return len(h) >= off+len(s) && bytes.Equal(h[off:off+len(s)], []byte(s))
	}
	switch {
	case at(4, "ftyp"):
		if at(8, "qt  ") {
			return "mov"
		}
		return "mp4"
	case at(0, "\x1a\x45\xdf\xa3"):
		return "matroska"
	case at(0, "RIFF") && at(8, "AVI "):
		return "avi"
	case at(0, "OggS"):
		return "ogg"
	case at(0, "FLV"):
		return "flv"
	case len(h) > 188 && h[0] == 0x47 && h[188] == 0x47:
		return "mpegts"
	}
	return "unknown"
}

// ProbeFile reads the header of the named file and detects its container.
func ProbeFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	buf := make([]byte, headerSize)
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return "", err
	}
	return DetectFormat(buf[:n]), nil
}
