package main

import (
	"bytes"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pygorvid/vid"
)

func TestRunRejectsNonPositiveThreads(t *testing.T) {
	for _, value := range []string{"0", "-2"} {
		if status := run([]string{"--threads", value, "input.mp4"}); status != 2 {
			t.Fatalf("run(--threads %s) = %d; want 2", value, status)
		}
	}
}

func TestReadAllFrames(t *testing.T) {
	path := filepath.Join("..", "..", "..", "samples", "decoder-smoke-16x16.mp4")
	for _, threads := range []int{1, 2} {
		count, err := readAllFrames(path, threads, false)
		if err != nil || count != 3 {
			t.Fatalf("readAllFrames() = %d, %v; want 3, nil", count, err)
		}
	}
	if status := run([]string{path, "--readall", "--input", path, "--threads", "2"}); status != 0 {
		t.Fatalf("read-all multiple files status = %d; want 0", status)
	}
	if status := run([]string{"--readall", "missing.mp4", path}); status != 1 {
		t.Fatalf("read-all missing input status = %d; want 1", status)
	}
}

func TestReadAllRejectsExtractionOptions(t *testing.T) {
	for _, arguments := range [][]string{
		{"--extractframe", "0"},
		{"--output", "out.png"},
		{"--extractframe", "0", "--output", "out.png"},
	} {
		arguments = append([]string{"--readall", "input.mp4"}, arguments...)
		if status := run(arguments); status != 2 {
			t.Fatalf("run(%v) = %d; want 2", arguments, status)
		}
	}
}

func TestReadAllReportsFailingFrame(t *testing.T) {
	path := filepath.Join("..", "..", "..", "samples", "decoder-smoke-16x16.mp4")
	reader, err := vid.OpenVideoSampleReader(path)
	if err != nil {
		t.Fatal(err)
	}
	sample, err := reader.SampleAt(1)
	reader.Close()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	offset := bytes.Index(data, sample.Data)
	if offset < 0 {
		t.Fatal("sample bytes are not in fixture")
	}
	clear(data[offset : offset+len(sample.Data)])
	invalid := filepath.Join(t.TempDir(), "invalid.mp4")
	if err := os.WriteFile(invalid, data, 0o600); err != nil {
		t.Fatal(err)
	}
	count, err := readAllFrames(invalid, 1, false)
	if count != 1 || err == nil || !strings.Contains(err.Error(), "decode frame 1:") {
		t.Fatalf("readAllFrames() = %d, %v; want failure at frame 1", count, err)
	}
}

func TestVerboseExtractionReportsProgressOnlyWhenEnabled(t *testing.T) {
	path := filepath.Join("..", "..", "..", "samples", "decoder-smoke-16x16.mp4")
	for _, verbose := range []bool{false, true} {
		readEnd, writeEnd, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		previousStderr := os.Stderr
		os.Stderr = writeEnd
		output := filepath.Join(t.TempDir(), "frame.png")
		extractErr := extractFrame(path, 0, output, 1, verbose)
		writeEnd.Close()
		os.Stderr = previousStderr
		debugOutput, readErr := io.ReadAll(readEnd)
		readEnd.Close()
		if extractErr != nil || readErr != nil {
			t.Fatalf("extractFrame(verbose=%t) = %v, read stderr: %v", verbose, extractErr, readErr)
		}
		if strings.Contains(string(debugOutput), "decoding frame") != verbose {
			t.Fatalf("debug output for verbose=%t = %q", verbose, debugOutput)
		}
	}
}

func TestFlagsFirstKeepsThreadsValue(t *testing.T) {
	got := flagsFirst([]string{"input.mp4", "--threads", "3"})
	want := []string{"--threads", "3", "input.mp4"}
	for i := range want {
		if len(got) != len(want) || got[i] != want[i] {
			t.Fatalf("flagsFirst() = %v; want %v", got, want)
		}
	}
}

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
	if err := extractFrame(path, 10_000, filepath.Join(t.TempDir(), "out.png"), 1, false); err == nil {
		t.Fatal("extractFrame() unexpectedly succeeded for an out-of-range frame index")
	}
}

func TestExtractFrameRejectsUnsupportedImageExtension(t *testing.T) {
	if err := extractFrame("sample.mp4", 0, "out.bmp", 1, false); err == nil {
		t.Fatal("extractFrame() accepted an unsupported output extension")
	}
}

func TestExtractFrameWritesJPEGForFixture(t *testing.T) {
	t.Setenv("PATH", "")
	path := filepath.Join("..", "..", "..", "samples", "decoder-smoke-16x16.mp4")
	for _, extension := range []string{".jpg", ".JPG"} {
		for _, frame := range []int{0, 1} {
			t.Run(extension+string(rune('0'+frame)), func(t *testing.T) {
				output := prefixedOutput(filepath.Join(t.TempDir(), "image with spaces"+extension), "native_")
				if err := extractFrame(path, frame, output, 2, false); err != nil {
					t.Fatalf("extractFrame() err = %v; want native JPEG output", err)
				}
				file, err := os.Open(output)
				if err != nil {
					t.Fatal(err)
				}
				defer file.Close()
				image, err := jpeg.Decode(file)
				if err != nil {
					t.Fatalf("jpeg.Decode() err = %v; want a valid JPEG image", err)
				}
				if image.Bounds().Dx() != 16 || image.Bounds().Dy() != 16 {
					t.Fatalf("image size = %dx%d; want 16x16", image.Bounds().Dx(), image.Bounds().Dy())
				}
				original, err := os.ReadFile(output)
				if err != nil {
					t.Fatal(err)
				}
				if err := extractFrame(path, 999, output, 1, false); err == nil {
					t.Fatal("extractFrame() accepted an out-of-range frame")
				}
				preserved, err := os.ReadFile(output)
				if err != nil || string(preserved) != string(original) {
					t.Fatal("failed extraction changed existing output")
				}
				entries, err := os.ReadDir(filepath.Dir(output))
				if err != nil || len(entries) != 1 {
					t.Fatal("extraction left temporary output behind")
				}
			})
		}
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
	entries, err := os.ReadDir(filepath.Dir(dir))
	if err != nil || len(entries) != 1 {
		t.Fatal("failed output rename left temporary files behind")
	}
}

func TestExtractFrameWritesPNGForFixture(t *testing.T) {
	path := filepath.Join("..", "..", "..", "testdata", "h264", "high42-1080p.mp4")
	output := filepath.Join(t.TempDir(), "out.png")
	if err := extractFrame(path, 0, output, vid.DefaultDecoderThreads(), false); err != nil {
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
