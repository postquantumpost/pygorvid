package vid

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"reflect"
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
	tkhd := make([]byte, 84)
	mdhd := make([]byte, 20)
	binary.BigEndian.PutUint32(mdhd[12:], 1000)
	return mkbox("trak", mkbox("tkhd", tkhd), mkbox("mdia", mkbox("mdhd", mdhd), hdlr, mkbox("minf", mkbox("stbl", mkbox("stts", make([]byte, 8))))))
}

func mktrackWithVideoInfo(width, height, sampleCount, sampleDelta, timescale uint32) []byte {
	tkhd := make([]byte, 84)
	binary.BigEndian.PutUint32(tkhd[76:], width<<16)
	binary.BigEndian.PutUint32(tkhd[80:], height<<16)
	mdhd := make([]byte, 20)
	binary.BigEndian.PutUint32(mdhd[12:], timescale)
	stts := make([]byte, 16)
	binary.BigEndian.PutUint32(stts[4:], 1)
	binary.BigEndian.PutUint32(stts[8:], sampleCount)
	binary.BigEndian.PutUint32(stts[12:], sampleDelta)
	hdlr := mkbox("hdlr", make([]byte, 8), []byte("vide"), make([]byte, 13))
	return mkbox("trak", mkbox("tkhd", tkhd), mkbox("mdia", mkbox("mdhd", mdhd), hdlr, mkbox("minf", mkbox("stbl", mkbox("stts", stts)))))
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
		{"video", [][]byte{mktrack("vide")}, BasicInfo{HasVideo: true, VideoStreams: []BasicVideoStreamInfo{{}}}},
		{"both", [][]byte{mktrack("vide"), mktrack("soun"), mktrack("soun")}, BasicInfo{HasVideo: true, HasAudio: true, VideoStreams: []BasicVideoStreamInfo{{}}, AudioStreams: []BasicAudioStreamInfo{{}, {}}}},
		{"audio", [][]byte{mktrack("soun")}, BasicInfo{HasAudio: true, AudioStreams: []BasicAudioStreamInfo{{}}}},
		{"none", nil, BasicInfo{}},
	}
	for _, c := range cases {
		p := writeMp4(t, mkbox("moov", c.tracks...))
		m := NewMp4File()
		if !m.Open(p) {
			t.Fatalf("%s: %s", c.name, m.ErrorInfo())
		}
		if got := m.GetBasicInfo(); !reflect.DeepEqual(got, c.want) || m.ErrorInfo() != "" {
			t.Errorf("%s: got %+v err=%q, want %+v", c.name, got, m.ErrorInfo(), c.want)
		}
		v := OpenFile(p)
		if got := v.GetBasicInfo(); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s via VidFile: got %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestGetBasicInfoAudioStreams(t *testing.T) {
	p := writeMp4(t, mkbox("moov",
		mktrackWithAudioInfo(44100, 2),
		mktrackWithAudioInfo(48000, 6),
	))
	m := NewMp4File()
	if !m.Open(p) {
		t.Fatal(m.ErrorInfo())
	}
	got := m.GetBasicInfo()
	want := []BasicAudioStreamInfo{{SampleRate: 44100, Channels: 2}, {SampleRate: 48000, Channels: 6}}
	if !reflect.DeepEqual(got.AudioStreams, want) {
		t.Errorf("got %+v, want %+v", got.AudioStreams, want)
	}
}

func mktrackWithAudioInfo(sampleRate uint32, channels uint16) []byte {
	hdlr := mkbox("hdlr", make([]byte, 8), []byte("soun"), make([]byte, 13))
	mdhd := make([]byte, 20)
	binary.BigEndian.PutUint32(mdhd[12:], sampleRate)
	audioEntry := make([]byte, 28)
	binary.BigEndian.PutUint16(audioEntry[16:], channels)
	binary.BigEndian.PutUint32(audioEntry[24:], sampleRate<<16)
	stsd := make([]byte, 8)
	binary.BigEndian.PutUint32(stsd[4:], 1)
	stsd = append(stsd, mkbox("mp4a", audioEntry)...)
	return mkbox("trak", mkbox("tkhd", make([]byte, 84)), mkbox("mdia", mkbox("mdhd", mdhd), hdlr, mkbox("minf", mkbox("stbl", mkbox("stsd", stsd)))))
}

func TestGetBasicInfoVideoStreams(t *testing.T) {
	p := writeMp4(t, mkbox("moov",
		mktrackWithVideoInfo(1920, 1080, 300, 1000, 30000),
		mktrackWithVideoInfo(640, 480, 240, 1000, 24000),
	))
	m := NewMp4File()
	if !m.Open(p) {
		t.Fatal(m.ErrorInfo())
	}
	got := m.GetBasicInfo()
	want := []BasicVideoStreamInfo{{Width: 1920, Height: 1080, FrameRate: 30}, {Width: 640, Height: 480, FrameRate: 24}}
	if !reflect.DeepEqual(got.VideoStreams, want) {
		t.Errorf("got %+v, want %+v", got.VideoStreams, want)
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
	if got := m.GetBasicInfo(); !reflect.DeepEqual(got, BasicInfo{}) || m.ErrorInfo() == "" {
		t.Errorf("truncated: got %+v err=%q", got, m.ErrorInfo())
	}
	if got := NewMp4File().GetBasicInfo(); !reflect.DeepEqual(got, BasicInfo{}) {
		t.Errorf("idle: got %+v", got)
	}

	mkv := filepath.Join(t.TempDir(), "a.mkv")
	os.WriteFile(mkv, []byte("\x1a\x45\xdf\xa3rest"), 0o644)
	v := OpenFile(mkv)
	if !v.IsOpen() || !reflect.DeepEqual(v.GetBasicInfo(), BasicInfo{}) || v.ErrorInfo() == "" {
		t.Errorf("mkv: open=%v err=%q", v.IsOpen(), v.ErrorInfo())
	}
}
