package vid

import "testing"

func TestUpsampleChroma420DuplicatesSamplesAcross2x2Blocks(t *testing.T) {
	up := upsampleChroma420([]byte{10, 20, 30, 40}, 4, 4, 2)
	want := []byte{
		10, 10, 20, 20,
		10, 10, 20, 20,
		30, 30, 40, 40,
		30, 30, 40, 40,
	}
	if len(up) != len(want) {
		t.Fatalf("len(upsampled) = %d; want %d", len(up), len(want))
	}
	for i := range want {
		if up[i] != want[i] {
			t.Fatalf("upsampled[%d] = %d; want %d", i, up[i], want[i])
		}
	}
}

func TestConvertLimitedRangeYUVToRGBMatchesBT601ReferencePixels(t *testing.T) {
	converter, err := NewPixelConverter(ColorRangeLimited)
	if err != nil {
		t.Fatalf("NewPixelConverter() error = %v", err)
	}
	var y, u, v []byte
	y = []byte{16, 235}
	u = []byte{128, 128}
	v = []byte{128, 128}
	buf, err := converter.ConvertYUV420ToRGB(2, 1, y, u, v, 2, 2, 2)
	if err != nil {
		t.Fatalf("ConvertYUV420ToRGB() error = %v", err)
	}
	if got := []byte{buf.Data[0], buf.Data[1], buf.Data[2], buf.Data[3], buf.Data[4], buf.Data[5]}; len(got) != 6 {
		t.Fatalf("converted row length = %d; want 6", len(got))
	}
	if buf.Data[0] != 0 || buf.Data[1] != 0 || buf.Data[2] != 0 {
		t.Fatalf("black pixel = (%d,%d,%d); want (0,0,0)", buf.Data[0], buf.Data[1], buf.Data[2])
	}
	if buf.Data[3] != 255 || buf.Data[4] != 255 || buf.Data[5] != 255 {
		t.Fatalf("white pixel = (%d,%d,%d); want (255,255,255)", buf.Data[3], buf.Data[4], buf.Data[5])
	}
}

func TestConvertFullRangeYUVToRGBPreservesGray(t *testing.T) {
	converter, err := NewPixelConverter(ColorRangeFull)
	if err != nil {
		t.Fatalf("NewPixelConverter() error = %v", err)
	}
	buf, err := converter.ConvertYUV420ToRGB(2, 1, []byte{128, 128}, []byte{128, 128}, []byte{128, 128}, 2, 2, 2)
	if err != nil {
		t.Fatalf("ConvertYUV420ToRGB() error = %v", err)
	}
	if buf.Data[0] != 128 || buf.Data[1] != 128 || buf.Data[2] != 128 {
		t.Fatalf("gray pixel = (%d,%d,%d); want (128,128,128)", buf.Data[0], buf.Data[1], buf.Data[2])
	}
	if buf.Data[3] != 128 || buf.Data[4] != 128 || buf.Data[5] != 128 {
		t.Fatalf("gray pixel = (%d,%d,%d); want (128,128,128)", buf.Data[3], buf.Data[4], buf.Data[5])
	}
}
