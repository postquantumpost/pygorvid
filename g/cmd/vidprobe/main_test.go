package main

import (
	"strings"
	"testing"
)

func TestExtractFrameRejectsUnimplementedNativePath(t *testing.T) {
	err := extractFrame("sample.mp4", 0, "out.png")
	if err == nil {
		t.Fatal("extractFrame() succeeded unexpectedly")
	}
	if !strings.Contains(err.Error(), "native frame extraction") {
		t.Fatalf("extractFrame() error = %q; want native extraction gate", err)
	}
}
