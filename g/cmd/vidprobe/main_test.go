package main

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestFlagsFirstPrioritizesExplicitInputAndOrder(t *testing.T) {
	args := []string{"--prefix", "pre-", "positional.mp4", "--input", "input.mp4", "--extractframe", "0", "--output", "out.png"}
	got := flagsFirst(args)
	want := []string{"--prefix", "pre-", "--input", "input.mp4", "--extractframe", "0", "--output", "out.png", "positional.mp4"}
	if len(got) != len(want) {
		t.Fatalf("flagsFirst() len = %d; want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("flagsFirst()[%d] = %q; want %q", i, got[i], want[i])
		}
	}
	if gotOutput := prefixedOutput("out.png", "pre-"); gotOutput != filepath.Join(".", "pre-out.png") {
		t.Fatalf("prefixedOutput() = %q; want %q", gotOutput, filepath.Join(".", "pre-out.png"))
	}
}

func TestWriteOutputFileUsesAtomicRename(t *testing.T) {
	output := filepath.Join(t.TempDir(), "frame.png")
	if err := writeOutputFile(output, []byte("PNGDATA")); err != nil {
		t.Fatalf("writeOutputFile() err = %v; want nil", err)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("os.ReadFile(%q) err = %v; want final output written", output, err)
	}
	if string(data) != "PNGDATA" {
		t.Fatalf("output payload = %q; want %q", string(data), "PNGDATA")
	}
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(output), ".tmp-*"))
	if err != nil {
		t.Fatalf("filepath.Glob() err = %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("leftover temp files = %v; want none after successful rename", matches)
	}
}

func TestExtractFrameRejectsOutOfRangeFrameIndex(t *testing.T) {
	path := filepath.Join("..", "..", "..", "testdata", "h264", "high42-1080p.mp4")
	if err := extractFrame(path, 10_000, filepath.Join(t.TempDir(), "out.png")); err == nil {
		t.Fatal("extractFrame() unexpectedly succeeded for an out-of-range frame index")
	}
}

func TestExtractFrameRejectsUnsupportedImageExtension(t *testing.T) {
	if err := extractFrame("sample.mp4", 0, "out.bmp"); err == nil {
		t.Fatal("extractFrame() accepted an unsupported output extension")
	}
}

func TestExtractFrameRejectsJPEGUntilImplemented(t *testing.T) {
	if err := extractFrame("sample.mp4", 0, "out.jpg"); err == nil {
		t.Fatal("extractFrame() unexpectedly accepted JPEG output before implementation")
	}
}

func TestWriteOutputFileRejectsDirectoryDestinations(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "output-dir")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("os.MkdirAll(%q) err = %v", dir, err)
	}
	if err := writeOutputFile(dir, []byte("unused")); err == nil {
		t.Fatal("writeOutputFile() unexpectedly accepted a directory output path")
	}
}

func TestExtractFrameWritesPNGForFixture(t *testing.T) {
	path := filepath.Join("..", "..", "..", "testdata", "h264", "high42-1080p.mp4")
	output := filepath.Join(t.TempDir(), "out.png")
	if err := extractFrame(path, 0, output); err != nil {
		t.Fatalf("extractFrame() err = %v; want nil for a valid fixture", err)
	}
	f, err := os.Open(output)
	if err != nil {
		t.Fatalf("os.Open(%q) err = %v; want a written PNG file", output, err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatalf("png.Decode(%q) err = %v; want a valid PNG image", output, err)
	}
	if img.Bounds().Dx() != 1920 || img.Bounds().Dy() != 1080 {
		t.Fatalf("image size = %dx%d; want 1920x1080", img.Bounds().Dx(), img.Bounds().Dy())
	}
}
