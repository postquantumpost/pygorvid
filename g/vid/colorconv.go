package vid

import (
	"errors"
	"fmt"
	"math"
)

var ErrPixelConverter = errors.New("pixel converter input is invalid")

type PixelConverter struct {
	ColorRange PixelColorRange
}

func NewPixelConverter(colorRange PixelColorRange) (PixelConverter, error) {
	if colorRange != ColorRangeLimited && colorRange != ColorRangeFull {
		return PixelConverter{}, fmt.Errorf("%w: %d", ErrPixelBufferRange, colorRange)
	}
	return PixelConverter{ColorRange: colorRange}, nil
}

func upsampleChroma420(chroma []byte, width, height, stride int) []byte {
	if width <= 0 || height <= 0 || stride <= 0 {
		return nil
	}
	upsampled := make([]byte, width*height)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			upsampled[y*width+x] = chroma[(y/2)*stride+(x/2)]
		}
	}
	return upsampled
}

func clampByte(value int) uint8 {
	if value < 0 {
		return 0
	}
	if value > 255 {
		return 255
	}
	return uint8(value)
}

func (c PixelConverter) convertYUVSample(y, u, v uint8) (uint8, uint8, uint8) {
	luma := int(y)
	blue := int(u) - 128
	red := int(v) - 128
	if c.ColorRange == ColorRangeLimited {
		luma -= 16
		R := int(math.Round(1.164*float64(luma) + 1.596*float64(red)))
		G := int(math.Round(1.164*float64(luma) - 0.392*float64(blue) - 0.813*float64(red)))
		B := int(math.Round(1.164*float64(luma) + 2.017*float64(blue)))
		return clampByte(R), clampByte(G), clampByte(B)
	}
	R := int(math.Round(float64(luma) + 1.402*float64(red)))
	G := int(math.Round(float64(luma) - 0.344*float64(blue) - 0.714*float64(red)))
	B := int(math.Round(float64(luma) + 1.772*float64(blue)))
	return clampByte(R), clampByte(G), clampByte(B)
}

func (c PixelConverter) ConvertYUV420ToRGB(width, height int, y, u, v []byte, yStride, uStride, vStride int) (PixelBuffer, error) {
	if width <= 0 || height <= 0 {
		return PixelBuffer{}, fmt.Errorf("%w: width and height must be positive", ErrPixelConverter)
	}
	if yStride < width || uStride < (width+1)/2 || vStride < (width+1)/2 {
		return PixelBuffer{}, fmt.Errorf("%w: chroma and luma strides are too small", ErrPixelConverter)
	}
	if len(y) < yStride*height || len(u) < uStride*((height+1)/2) || len(v) < vStride*((height+1)/2) {
		return PixelBuffer{}, fmt.Errorf("%w: input planes are too short for the requested dimensions", ErrPixelConverter)
	}
	outStride := width * 3
	outData := make([]byte, outStride*height)
	uUpsampled := upsampleChroma420(u, width, height, uStride)
	vUpsampled := upsampleChroma420(v, width, height, vStride)
	for row := 0; row < height; row++ {
		for col := 0; col < width; col++ {
			offset := row*outStride + col*3
			yValue := y[row*yStride+col]
			uValue := uUpsampled[row*width+col]
			vValue := vUpsampled[row*width+col]
			r, g, b := c.convertYUVSample(yValue, uValue, vValue)
			outData[offset] = r
			outData[offset+1] = g
			outData[offset+2] = b
		}
	}
	return NewPixelBuffer(width, height, outStride, 3, c.ColorRange, outData)
}

func (frame Yuv420Frame) ConvertToRGB(colorRange PixelColorRange) (PixelBuffer, error) {
	if frame.Width <= 0 || frame.Height <= 0 {
		return PixelBuffer{}, fmt.Errorf("%w: YUV frame dimensions must be positive", ErrPixelConverter)
	}
	if frame.YStride < frame.Width || frame.UStride < (frame.Width+1)/2 || frame.VStride < (frame.Width+1)/2 {
		return PixelBuffer{}, fmt.Errorf("%w: YUV frame strides are too small for the decoded geometry", ErrPixelConverter)
	}
	if len(frame.Y) < frame.YStride*frame.Height || len(frame.U) < frame.UStride*((frame.Height+1)/2) || len(frame.V) < frame.VStride*((frame.Height+1)/2) {
		return PixelBuffer{}, fmt.Errorf("%w: YUV frame planes are too short for the decoded geometry", ErrPixelConverter)
	}
	converter, err := NewPixelConverter(colorRange)
	if err != nil {
		return PixelBuffer{}, err
	}
	return converter.ConvertYUV420ToRGB(frame.Width, frame.Height, frame.Y, frame.U, frame.V, frame.YStride, frame.UStride, frame.VStride)
}
