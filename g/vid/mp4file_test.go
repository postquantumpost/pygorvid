package vid

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMp4File(t *testing.T) {
	dir := t.TempDir()
	mp4 := filepath.Join(dir, "a.mp4")
	os.WriteFile(mp4, []byte("\x00\x00\x00\x18ftypisom"), 0o644)
	mkv := filepath.Join(dir, "a.mkv")
	os.WriteFile(mkv, []byte("\x1a\x45\xdf\xa3rest"), 0o644)

	m := NewMp4File()
	if m.IsOpen() || m.ErrorInfo() != "" {
		t.Fatal("new Mp4File should be idle")
	}
	if !m.Open(mp4) || !m.IsOpen() {
		t.Fatalf("open mp4: %q", m.ErrorInfo())
	}
	m.Close()
	if m.IsOpen() {
		t.Error("still open after Close")
	}
	if m.Open(mkv) || m.ErrorInfo() == "" {
		t.Error("mkv should be rejected with an error")
	}

	v := OpenFile(mp4)
	if !v.IsOpen() || v.Format != "mp4" || v.mp4 == nil || !v.mp4.IsOpen() {
		t.Errorf("VidFile should hold an open Mp4File: %q", v.ErrorInfo())
	}
	v.Close()
	if v.IsOpen() {
		t.Error("VidFile still open after Close")
	}
}
