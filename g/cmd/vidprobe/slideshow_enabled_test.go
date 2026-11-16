//go:build slideshow

package main

import (
	"path/filepath"
	"testing"
)

func TestSlideshowEnabled(t *testing.T) {
	t.Setenv("SDL_VIDEODRIVER", "dummy")
	count, err := showSlideshow(filepath.Join("..", "..", "..", "samples", "decoder-smoke-16x16.mp4"), 1, false)
	if err != nil || count != 3 {
		t.Fatalf("displayed %d frames: %v", count, err)
	}
}
