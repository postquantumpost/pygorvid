//go:build !cgo || noslideshow

package main

import "errors"

func showSlideshow(_ string, _ int, _ bool, readaheadOption ...bool) (int, error) {
	return 0, errors.New("optional slideshow build feature was not included; rebuild with cgo enabled and without -tags noslideshow")
}

func showSlideshowConfigured(
	_ string, _, _ int, _ bool, _ bool,
) (int, error) {
	return 0, errors.New("optional slideshow build feature was not included; rebuild with cgo enabled and without -tags noslideshow")
}
