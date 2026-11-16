//go:build cgo && !noslideshow

package main

import (
	"fmt"
	"runtime"
	"sort"
	"time"
	"unsafe"

	"github.com/veandco/go-sdl2/sdl"
	"pygorvid/vid"
)

func showSlideshow(input string, threads int, verbose bool) (int, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	reader, err := vid.OpenVideoSampleReader(input)
	if err != nil {
		return 0, err
	}
	defer reader.Close()
	decoder, err := vid.NewVideoDecoder(reader)
	if err != nil {
		return 0, err
	}
	decoder.SetThreads(threads)
	decoder.SetVerbose(verbose)
	timescale := reader.Configuration().Timescale
	if config := reader.HEVCConfiguration(); config != nil {
		timescale = config.Timescale
	}
	if timescale == 0 {
		return 0, fmt.Errorf("video timescale is zero")
	}
	samples := make([]vid.CompressedSample, reader.SampleCount())
	for index := range samples {
		samples[index], err = reader.SampleAt(uint64(index))
		if err != nil {
			return 0, fmt.Errorf("read sample %d: %w", index, err)
		}
		samples[index].Data = nil
	}
	sort.SliceStable(samples, func(i, j int) bool { return samples[i].PTSTicks < samples[j].PTSTicks })
	if err := sdl.InitSubSystem(sdl.INIT_VIDEO); err != nil {
		return 0, fmt.Errorf("initialize SDL2 video: %w", err)
	}
	defer sdl.QuitSubSystem(sdl.INIT_VIDEO)
	var window *sdl.Window
	var renderer *sdl.Renderer
	var texture *sdl.Texture
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
	isOpen := func() bool {
		for event := sdl.PollEvent(); event != nil; event = sdl.PollEvent() {
			switch e := event.(type) {
			case *sdl.QuitEvent:
				return false
			case *sdl.WindowEvent:
				if e.Event == sdl.WINDOWEVENT_CLOSE {
					return false
				}
			}
		}
		return true
	}
	wait := func(deadline time.Time) bool {
		for isOpen() {
			remaining := time.Until(deadline)
			if remaining <= 0 {
				return true
			}
			time.Sleep(min(remaining, 20*time.Millisecond))
		}
		return false
	}
	deadline := time.Now()
	count, width, height := 0, 0, 0
	for index, sample := range samples {
		if !isOpen() {
			return count, nil
		}
		frame, err := decoder.DecodeFrame(uint64(index))
		if err != nil {
			return count, fmt.Errorf("decode frame %d: %w", index, err)
		}
		if !wait(deadline) {
			return count, nil
		}
		rgb, err := frame.ConvertToRGB(vid.ColorRangeLimited)
		if err != nil {
			return count, fmt.Errorf("convert frame %d: %w", index, err)
		}
		if window == nil {
			window, err = sdl.CreateWindow(input, sdl.WINDOWPOS_CENTERED, sdl.WINDOWPOS_CENTERED,
				int32(rgb.Width), int32(rgb.Height), sdl.WINDOW_SHOWN|sdl.WINDOW_RESIZABLE)
			if err != nil {
				return count, fmt.Errorf("create slideshow window: %w", err)
			}
			renderer, err = sdl.CreateRenderer(window, -1, 0)
			if err != nil {
				return count, fmt.Errorf("create slideshow renderer: %w", err)
			}
		}
		if width != rgb.Width || height != rgb.Height {
			if texture != nil {
				texture.Destroy()
				texture = nil
			}
			texture, err = renderer.CreateTexture(sdl.PIXELFORMAT_RGB24, sdl.TEXTUREACCESS_STREAMING, int32(rgb.Width), int32(rgb.Height))
			if err != nil {
				return count, fmt.Errorf("create slideshow texture: %w", err)
			}
			width, height = rgb.Width, rgb.Height
		}
		if err = texture.Update(nil, unsafe.Pointer(&rgb.Data[0]), rgb.Stride); err != nil {
			return count, fmt.Errorf("update slideshow texture: %w", err)
		}
		if err = renderer.Clear(); err != nil {
			return count, fmt.Errorf("clear slideshow window: %w", err)
		}
		if err = renderer.Copy(texture, nil, nil); err != nil {
			return count, fmt.Errorf("render slideshow frame: %w", err)
		}
		renderer.Present()
		count++
		duration := time.Duration(float64(sample.DurationTicks) / float64(timescale) * float64(time.Second))
		deadline = time.Now().Add(duration)
	}
	if count > 0 {
		wait(deadline)
	}
	return count, nil
}
