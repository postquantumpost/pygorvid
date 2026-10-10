package vid

import (
	"errors"
	"fmt"
)

var (
	ErrPixelBufferLayout = errors.New("pixel buffer layout is invalid")
	ErrPixelBufferRange  = errors.New("pixel buffer color range is invalid")
)

type PixelColorRange uint8

const (
	ColorRangeLimited PixelColorRange = iota
	ColorRangeFull
)

type PixelBuffer struct {
	Width      int
	Height     int
	Stride     int
	Channels   int
	ColorRange PixelColorRange
	Data       []uint8
}

func NewPixelBuffer(width, height, stride, channels int, colorRange PixelColorRange, data []uint8) (PixelBuffer, error) {
	if width <= 0 || height <= 0 || stride <= 0 || channels <= 0 {
		return PixelBuffer{}, fmt.Errorf("%w: dimensions, stride, and channels must be positive", ErrPixelBufferLayout)
	}
	if stride < width*channels {
		return PixelBuffer{}, fmt.Errorf("%w: stride must cover the row width", ErrPixelBufferLayout)
	}
	if colorRange != ColorRangeLimited && colorRange != ColorRangeFull {
		return PixelBuffer{}, fmt.Errorf("%w: %d", ErrPixelBufferRange, colorRange)
	}
	if len(data) < stride*height {
		return PixelBuffer{}, fmt.Errorf("%w: data must hold at least %d bytes for %dx%d stride=%d", ErrPixelBufferLayout, stride*height, width, height, stride)
	}
	owned := make([]uint8, stride*height)
	copy(owned, data[:stride*height])
	return PixelBuffer{
		Width:      width,
		Height:     height,
		Stride:     stride,
		Channels:   channels,
		ColorRange: colorRange,
		Data:       owned,
	}, nil
}

func (b PixelBuffer) Clone() (PixelBuffer, error) {
	return NewPixelBuffer(b.Width, b.Height, b.Stride, b.Channels, b.ColorRange, b.Data)
}

func (b PixelBuffer) Row(y int) ([]uint8, error) {
	if y < 0 || y >= b.Height {
		return nil, fmt.Errorf("%w: row index %d out of range [0,%d)", ErrPixelBufferLayout, y, b.Height)
	}
	start := y * b.Stride
	return b.Data[start : start+b.Stride], nil
}

func (b PixelBuffer) PixelOffset(x, y int) (int, error) {
	if x < 0 || y < 0 || x >= b.Width || y >= b.Height {
		return 0, fmt.Errorf("%w: pixel (%d,%d) is outside %dx%d buffer", ErrPixelBufferLayout, x, y, b.Width, b.Height)
	}
	return (y*b.Stride + x*b.Channels), nil
}
