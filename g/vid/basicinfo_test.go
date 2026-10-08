package vid

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func mkbox(kind string, payload ...[]byte) []byte {
	body := bytes.Join(payload, nil)
	b := make([]byte, 8, 8+len(body))
	binary.BigEndian.PutUint32(b, uint32(8+len(body)))
	copy(b[4:], kind)
	return append(b, body...)
}

func mktrack(handler string) []byte {
	hdlr := mkbox("hdlr", make([]byte, 8), []byte(handler), make([]byte, 13))
	return mkbox("trak", mkbox("tkhd", make([]byte, 8)), mkbox("mdia", mkbox("mdhd", make([]byte, 4)), hdlr))
}

func writeMp4(t *testing.T, tail ...[]byte) string {
	t.Helper()
	data := bytes.Join([][]byte{
		mkbox("ftyp", []byte("isom\x00\x00\x00\x00isom")),
		mkbox("free", []byte("xx")),
	}, nil)
	data = append(data, bytes.Join(tail, nil)...)
	p := filepath.Join(t.TempDir(), "a.mp4")
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestGetBasicInfo(t *testing.T) {
	cases := []struct {
		name   string
		tracks [][]byte
		want   BasicInfo
	}{
		{"video", [][]byte{mktrack("vide")}, BasicInfo{HasVideo: true}},
		{"both", [][]byte{mktrack("vide"), mktrack("soun"), mktrack("soun")}, BasicInfo{true, true}},
		{"audio", [][]byte{mktrack("soun")}, BasicInfo{HasAudio: true}},
		{"none", nil, BasicInfo{}},
	}
	for _, c := range cases {
		p := writeMp4(t, mkbox("moov", c.tracks...))
		m := NewMp4File()
		if !m.Open(p) {
			t.Fatalf("%s: %s", c.name, m.ErrorInfo())
		}
		if got := m.GetBasicInfo(); got != c.want || m.ErrorInfo() != "" {
			t.Errorf("%s: got %+v err=%q, want %+v", c.name, got, m.ErrorInfo(), c.want)
		}
		v := OpenFile(p)
		if got := v.GetBasicInfo(); got != c.want {
			t.Errorf("%s via VidFile: got %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestGetBasicInfoErrors(t *testing.T) {
	bad := make([]byte, 8)
	binary.BigEndian.PutUint32(bad, 1000)
	copy(bad[4:], "moov")
	m := NewMp4File()
	if !m.Open(writeMp4(t, bad, make([]byte, 8))) {
		t.Fatal(m.ErrorInfo())
	}
	if got := m.GetBasicInfo(); got != (BasicInfo{}) || m.ErrorInfo() == "" {
		t.Errorf("truncated: got %+v err=%q", got, m.ErrorInfo())
	}
	if got := NewMp4File().GetBasicInfo(); got != (BasicInfo{}) {
		t.Errorf("idle: got %+v", got)
	}

	mkv := filepath.Join(t.TempDir(), "a.mkv")
	os.WriteFile(mkv, []byte("\x1a\x45\xdf\xa3rest"), 0o644)
	v := OpenFile(mkv)
	if !v.IsOpen() || v.GetBasicInfo() != (BasicInfo{}) || v.ErrorInfo() == "" {
		t.Errorf("mkv: open=%v err=%q", v.IsOpen(), v.ErrorInfo())
	}
}
