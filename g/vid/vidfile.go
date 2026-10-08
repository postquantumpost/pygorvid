package vid

import (
	"fmt"
	"os"
)

// VidFile is a handle for a video file. The zero value is idle (not open).
// An mp4 file is handled by a held Mp4File.
type VidFile struct {
	f      *os.File
	mp4    *Mp4File
	errMsg string
	Format string
}

// Construct returns an idle VidFile, ready for its Open method.
func Construct() *VidFile { return &VidFile{} }

// OpenFile returns a VidFile that is open on success, otherwise idle with ErrorInfo set.
func OpenFile(name string) *VidFile {
	v := Construct()
	v.Open(name)
	return v
}

// Open closes any open file, then tries to open name. It reports success.
func (v *VidFile) Open(name string) bool {
	v.Close()
	v.errMsg = ""
	f, err := os.Open(name)
	if err != nil {
		v.errMsg = err.Error()
		return false
	}
	format, err := detectFromFile(f)
	if err != nil {
		f.Close()
		v.errMsg = err.Error()
		return false
	}
	if format == "unknown" {
		f.Close()
		v.errMsg = fmt.Sprintf("%s: unrecognized video format", name)
		return false
	}
	if format == "mp4" {
		f.Close()
		m := NewMp4File()
		if !m.Open(name) {
			v.errMsg = m.ErrorInfo()
			return false
		}
		v.mp4, v.Format = m, format
		return true
	}
	v.f, v.Format = f, format
	return true
}

// Close returns the object to the idle state.
func (v *VidFile) Close() {
	if v.f != nil {
		v.f.Close()
	}
	if v.mp4 != nil {
		v.mp4.Close()
	}
	v.f, v.mp4, v.Format = nil, nil, ""
}

// IsOpen reports whether the object is in the open state.
func (v *VidFile) IsOpen() bool {
	return v.f != nil || (v.mp4 != nil && v.mp4.IsOpen())
}

// GetBasicInfo returns basic file info; on failure it returns a zero BasicInfo and sets ErrorInfo.
func (v *VidFile) GetBasicInfo() BasicInfo {
	if v.mp4 != nil {
		info := v.mp4.GetBasicInfo()
		v.errMsg = v.mp4.ErrorInfo()
		return info
	}
	if v.f == nil {
		v.errMsg = "file not open"
	} else {
		v.errMsg = fmt.Sprintf("basic info not supported for %s", v.Format)
	}
	return BasicInfo{}
}

// ErrorInfo returns error text; an empty string means no error.
func (v *VidFile) ErrorInfo() (msg string) { return v.errMsg }
