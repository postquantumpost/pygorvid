//go:build !slideshow

package main

import "errors"

func showSlideshow(_ string, _ int, _ bool) (int, error) {
	return 0, errors.New("optional slideshow build feature was not included; rebuild with -tags slideshow")
}
