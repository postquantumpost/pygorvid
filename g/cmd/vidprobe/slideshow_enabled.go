//go:build cgo && !noslideshow

package main

import (
	"errors"
	"fmt"
	"runtime"
	"time"
	"unsafe"

	"github.com/veandco/go-sdl2/sdl"
	"pygorvid/vid"
)

const maxSDLWaitMilliseconds = 2_147_483_647

var errSlideshowClosed = fmt.Errorf("slideshow closed")

func sdlWaitTimeout(remaining time.Duration) int {
	maximum := time.Duration(maxSDLWaitMilliseconds) * time.Millisecond
	if remaining >= maximum {
		return maxSDLWaitMilliseconds
	}
	timeout := int(remaining / time.Millisecond)
	if remaining%time.Millisecond != 0 {
		timeout++
	}
	return max(timeout, 1)
}

func showSlideshow(input string, threads int, verbose bool, readaheadOption ...bool) (int, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := sdl.InitSubSystem(sdl.INIT_VIDEO); err != nil {
		return 0, fmt.Errorf("initialize SDL2 video: %w", err)
	}
	defer sdl.QuitSubSystem(sdl.INIT_VIDEO)
	var window *sdl.Window
	var renderer *sdl.Renderer
	var texture *sdl.Texture
	closed := false
	defer func() {
		if texture != nil {
			texture.Destroy()
		}
		if renderer != nil {
			renderer.Destroy()
		}
		if window != nil {
			window.Destroy()
		}
	}()
	isClosed := func(event sdl.Event) bool {
		switch e := event.(type) {
		case *sdl.QuitEvent:
			return true
		case *sdl.WindowEvent:
			if e.Event == sdl.WINDOWEVENT_CLOSE {
				return true
			}
		}
		return false
	}
	wait := func(deadline time.Time) bool {
		for !closed {
			remaining := time.Until(deadline)
			if remaining <= 0 {
				return true
			}
			closed = isClosed(sdl.WaitEventTimeout(sdlWaitTimeout(remaining)))
		}
		return !closed
	}
	deadline := time.Now()
	count, width, height := 0, 0, 0
	consume := func(result vid.StreamFrame) error {
		index := result.Index
		if result.Timescale == 0 {
			return fmt.Errorf("video timescale is zero")
		}
		if closed {
			return errSlideshowClosed
		}
		if !wait(deadline) {
			return errSlideshowClosed
		}
		frame := result.Frame
		rgb, err := frame.ConvertToRGB(vid.ColorRangeLimited)
		if err != nil {
			return fmt.Errorf("convert frame %d: %w", index, err)
		}
		if window == nil {
			scale := min(1, 1024/float64(rgb.Width), 768/float64(rgb.Height))
			windowWidth := max(1, int32(float64(rgb.Width)*scale))
			windowHeight := max(1, int32(float64(rgb.Height)*scale))
			window, err = sdl.CreateWindow(input, sdl.WINDOWPOS_CENTERED, sdl.WINDOWPOS_CENTERED,
				windowWidth, windowHeight, sdl.WINDOW_SHOWN|sdl.WINDOW_RESIZABLE)
			if err != nil {
				return fmt.Errorf("create slideshow window: %w", err)
			}
			renderer, err = sdl.CreateRenderer(window, -1, 0)
			if err != nil {
				return fmt.Errorf("create slideshow renderer: %w", err)
			}
		}
		if width != rgb.Width || height != rgb.Height {
			if texture != nil {
				texture.Destroy()
				texture = nil
			}
			texture, err = renderer.CreateTexture(sdl.PIXELFORMAT_RGB24, sdl.TEXTUREACCESS_STREAMING, int32(rgb.Width), int32(rgb.Height))
			if err != nil {
				return fmt.Errorf("create slideshow texture: %w", err)
			}
			width, height = rgb.Width, rgb.Height
		}
		if err = texture.Update(nil, unsafe.Pointer(&rgb.Data[0]), rgb.Stride); err != nil {
			return fmt.Errorf("update slideshow texture: %w", err)
		}
		if err = renderer.Clear(); err != nil {
			return fmt.Errorf("clear slideshow window: %w", err)
		}
		if err = renderer.Copy(texture, nil, nil); err != nil {
			return fmt.Errorf("render slideshow frame: %w", err)
		}
		renderer.Present()
		count++
		duration := time.Duration(
			float64(result.DurationTicks) / float64(result.Timescale) * float64(time.Second),
		)
		deadline = time.Now().Add(duration)
		return nil
	}
	decoder := vid.NewVideoStreamDecoder(input)
	decoder.SetThreads(threads)
	decoder.SetVerbose(verbose)
	decoder.SetReadahead(len(readaheadOption) != 0 && readaheadOption[0])
	count, err := decoder.Decode(consume)
	if err != nil {
		if errors.Is(err, errSlideshowClosed) {
			return count, nil
		}
		return count, err
	}
	if count > 0 {
		wait(deadline)
	}
	return count, nil
}
