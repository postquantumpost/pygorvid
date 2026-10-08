package vid

import (
	"fmt"
	"os"
)

// Mp4File is a handle for an mp4 file. The zero value is idle (not open).
type Mp4File struct {
	f      *os.File
	errMsg string
}

// NewMp4File returns an idle Mp4File, ready for its Open method.
func NewMp4File() *Mp4File { return &Mp4File{} }

// Open closes any open file, then tries to open name as mp4. It reports success.
func (m *Mp4File) Open(name string) bool {
	m.Close()
	m.errMsg = ""
	f, err := os.Open(name)
	if err != nil {
		m.errMsg = err.Error()
		return false
	}
	format, err := detectFromFile(f)
	if err != nil {
		f.Close()
		m.errMsg = err.Error()
		return false
	}
	if format != "mp4" {
		f.Close()
		m.errMsg = fmt.Sprintf("%s: not an mp4 file", name)
		return false
	}
	m.f = f
	return true
}

// Close returns the object to the idle state.
func (m *Mp4File) Close() {
	if m.f != nil {
		m.f.Close()
	}
	m.f = nil
}

// IsOpen reports whether the object is in the open state.
func (m *Mp4File) IsOpen() bool { return m.f != nil }

// ErrorInfo returns error text; an empty string means no error.
func (m *Mp4File) ErrorInfo() (msg string) { return m.errMsg }

// GetBasicInfo returns basic file info; on failure it returns a zero BasicInfo and sets ErrorInfo.
func (m *Mp4File) GetBasicInfo() BasicInfo {
	m.errMsg = ""
	if m.f == nil {
		m.errMsg = "file not open"
		return BasicInfo{}
	}
	st, err := m.f.Stat()
	if err != nil {
		m.errMsg = err.Error()
		return BasicInfo{}
	}
	boxes, err := readBoxes(m.f, 0, st.Size())
	if err != nil {
		m.errMsg = err.Error()
		return BasicInfo{}
	}
	var info BasicInfo
	for _, b := range find(boxes, "moov", "trak", "mdia", "hdlr") {
		if h, ok := b.(*hdlrBox); ok {
			switch h.handlerType {
			case "vide":
				info.HasVideo = true
			case "soun":
				info.HasAudio = true
			}
		}
	}
	return info
}
