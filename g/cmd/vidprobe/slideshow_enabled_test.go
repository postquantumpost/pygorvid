//go:build cgo && !noslideshow

package main

import (
	"path/filepath"
	"testing"
	"time"
)

func TestSDLWaitTimeoutUsesMilliseconds(t *testing.T) {
	for input, want := range map[time.Duration]int{
		time.Nanosecond:        1,
		time.Millisecond:       1,
		time.Millisecond + 1:   2,
		150 * time.Millisecond: 150,
	} {
		if got := sdlWaitTimeout(input); got != want {
			t.Fatalf("sdlWaitTimeout(%s) = %d; want %d", input, got, want)
		}
	}
}

func TestSlideshowEnabled(t *testing.T) {
	t.Setenv("SDL_VIDEODRIVER", "dummy")
	count, err := showSlideshow(filepath.Join("..", "..", "..", "samples", "decoder-smoke-16x16.mp4"), 1, false)
	if err != nil || count != 3 {
		t.Fatalf("displayed %d frames: %v", count, err)
	}
}
