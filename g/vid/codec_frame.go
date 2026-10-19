package vid

import "errors"

var ErrReferencePictureFrameLayout = errors.New("reference picture frame layout is invalid or truncated")

type Yuv420Frame struct {
	Width   int
	Height  int
	YStride int
	UStride int
	VStride int
	Y       []uint8
	U       []uint8
	V       []uint8
}

func (frame Yuv420Frame) planeBytes(plane []byte, width, height, stride int) ([]byte, error) {
	if width <= 0 || height <= 0 || len(plane) == 0 {
		return nil, ErrReferencePictureFrameLayout
	}
	if stride <= 0 {
		stride = width
	}
	if stride < width {
		return nil, ErrReferencePictureFrameLayout
	}
	out := make([]byte, 0, width*height)
	for row := 0; row < height; row++ {
		start := row * stride
		end := start + width
		if end > len(plane) {
			return nil, ErrReferencePictureFrameLayout
		}
		out = append(out, plane[start:end]...)
	}
	return out, nil
}

func (frame Yuv420Frame) LumaPlaneBytes() ([]byte, error) {
	return frame.planeBytes(frame.Y, frame.Width, frame.Height, frame.YStride)
}

func (frame Yuv420Frame) UPlaneBytes() ([]byte, error) {
	chromaWidth := frame.Width/2 + frame.Width%2
	chromaHeight := frame.Height/2 + frame.Height%2
	return frame.planeBytes(frame.U, chromaWidth, chromaHeight, frame.UStride)
}

func (frame Yuv420Frame) VPlaneBytes() ([]byte, error) {
	chromaWidth := frame.Width/2 + frame.Width%2
	chromaHeight := frame.Height/2 + frame.Height%2
	return frame.planeBytes(frame.V, chromaWidth, chromaHeight, frame.VStride)
}

func (frame Yuv420Frame) LumaPlaneMatches(reference []byte) (bool, error) {
	plane, err := frame.LumaPlaneBytes()
	if err != nil {
		return false, err
	}
	return equalPlane(plane, reference), nil
}

func equalPlane(left, right []byte) bool {
	if len(left) != len(right) {
		return false
	}
	for index, value := range left {
		if value != right[index] {
			return false
		}
	}
	return true
}
