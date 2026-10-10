package vid

import "testing"

func TestPixelBufferOwnsDataAndValidatesLayout(t *testing.T) {
	data := []uint8{10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21}
	buf, err := NewPixelBuffer(2, 2, 4, 1, ColorRangeLimited, data)
	if err != nil {
		t.Fatalf("NewPixelBuffer() returned error: %v", err)
	}
	if buf.Width != 2 || buf.Height != 2 || buf.Stride != 4 || buf.Channels != 1 {
		t.Fatalf("buffer layout = %#v; want width=2 height=2 stride=4 channels=1", buf)
	}
	if buf.ColorRange != ColorRangeLimited {
		t.Fatalf("buffer color range = %v; want %v", buf.ColorRange, ColorRangeLimited)
	}
	if len(buf.Data) != 8 {
		t.Fatalf("len(buf.Data) = %d; want 8", len(buf.Data))
	}
	if buf.Data[0] != 10 || buf.Data[7] != 17 {
		t.Fatalf("buffer copy mismatch: %#v", buf.Data)
	}
	data[0] = 99
	if buf.Data[0] != 10 {
		t.Fatalf("buffer must own its storage; got %d after original mutation", buf.Data[0])
	}

	if _, err := NewPixelBuffer(0, 2, 4, 1, ColorRangeLimited, data); err == nil {
		t.Fatal("expected width validation error")
	}
	if _, err := NewPixelBuffer(2, 0, 4, 1, ColorRangeLimited, data); err == nil {
		t.Fatal("expected height validation error")
	}
	if _, err := NewPixelBuffer(2, 2, 1, 1, ColorRangeLimited, data); err == nil {
		t.Fatal("expected stride validation error")
	}
	if _, err := NewPixelBuffer(2, 2, 4, 1, PixelColorRange(9), data); err == nil {
		t.Fatal("expected color-range validation error")
	}
	if _, err := NewPixelBuffer(2, 2, 4, 1, ColorRangeLimited, make([]uint8, 7)); err == nil {
		t.Fatal("expected data-length validation error")
	}
}
