package vid

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConstruct(t *testing.T) {
	v := Construct()
	if v == nil || v.IsOpen() || v.ErrorInfo() != "" {
		t.Fatal("Construct should return an idle object with no error")
	}
}

func TestOpenFile(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "a.mkv")
	os.WriteFile(good, []byte("\x1a\x45\xdf\xa3rest"), 0o644)
	bad := filepath.Join(dir, "a.bin")
	os.WriteFile(bad, []byte("hello"), 0o644)

	v := OpenFile(good)
	if !v.IsOpen() || v.ErrorInfo() != "" || v.Format != "matroska" {
		t.Fatalf("good: open=%v err=%q fmt=%q", v.IsOpen(), v.ErrorInfo(), v.Format)
	}
	v.Close()
	if v.IsOpen() {
		t.Error("still open after Close")
	}
	for _, name := range []string{bad, filepath.Join(dir, "missing")} {
		v := OpenFile(name)
		if v.IsOpen() || v.ErrorInfo() == "" {
			t.Errorf("%s: open=%v err=%q", name, v.IsOpen(), v.ErrorInfo())
		}
	}
}
