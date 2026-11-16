//go:build !slideshow

package main

import (
	"strings"
	"testing"
)

func TestSlideshowDisabled(t *testing.T) {
	_, err := showSlideshow("unused.mp4", 1, false)
	if err == nil || !strings.Contains(err.Error(), "optional slideshow build feature was not included") {
		t.Fatalf("unexpected error: %v", err)
	}
}
