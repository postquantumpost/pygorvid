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

func TestReadAVCSampleDescriptionAndTables(t *testing.T) {
	avcc := []byte{
		1, 100, 0, 42, 0xff, 0xe1,
		0, 4, 0x67, 100, 0, 42,
		1, 0, 2, 0x68, 0xee,
	}
	avc1Payload := make([]byte, 78)
	binary.BigEndian.PutUint16(avc1Payload[24:], 1920)
	binary.BigEndian.PutUint16(avc1Payload[26:], 1080)
	avc1 := mkbox("avc1", avc1Payload, mkbox("avcC", avcc))
	stsdPayload := make([]byte, 8)
	binary.BigEndian.PutUint32(stsdPayload[4:], 1)
	stsdPayload = append(stsdPayload, avc1...)

	stszPayload := make([]byte, 24)
	binary.BigEndian.PutUint32(stszPayload[8:], 3)
	binary.BigEndian.PutUint32(stszPayload[12:], 10)
	binary.BigEndian.PutUint32(stszPayload[16:], 20)
	binary.BigEndian.PutUint32(stszPayload[20:], 30)

	stscPayload := make([]byte, 32)
	binary.BigEndian.PutUint32(stscPayload[4:], 2)
	for i, values := range [][3]uint32{{1, 2, 1}, {2, 1, 1}} {
		start := 8 + i*12
		for j, value := range values {
			binary.BigEndian.PutUint32(stscPayload[start+j*4:], value)
		}
	}
	stcoPayload := make([]byte, 16)
	binary.BigEndian.PutUint32(stcoPayload[4:], 2)
	binary.BigEndian.PutUint32(stcoPayload[8:], 100)
	binary.BigEndian.PutUint32(stcoPayload[12:], 200)
	co64Payload := make([]byte, 24)
	binary.BigEndian.PutUint32(co64Payload[4:], 2)
	binary.BigEndian.PutUint64(co64Payload[8:], 1<<32+100)
	binary.BigEndian.PutUint64(co64Payload[16:], 1<<32+200)

	data := bytes.Join([][]byte{
		mkbox("stsd", stsdPayload),
		mkbox("stsz", stszPayload),
		mkbox("stsc", stscPayload),
		mkbox("stco", stcoPayload),
		mkbox("co64", co64Payload),
	}, nil)
	boxes, err := readBoxes(bytes.NewReader(data), 0, int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}

	avc1Box, ok := find(boxes, "stsd", "avc1")[0].(*avc1Box)
	if !ok || avc1Box.width != 1920 || avc1Box.height != 1080 {
		t.Fatalf("unexpected avc1 sample entry: %#v", avc1Box)
	}
	avcC, ok := find(boxes, "stsd", "avc1", "avcC")[0].(*avcCBox)
	if !ok || avcC.profile != 100 || avcC.level != 42 || avcC.nalLengthSize != 4 || len(avcC.sequenceSets) != 1 || len(avcC.pictureSets) != 1 {
		t.Fatalf("unexpected AVC configuration: %#v", avcC)
	}
	stsz, ok := find(boxes, "stsz")[0].(*stszBox)
	if !ok || stsz.sampleCount != 3 || stsz.constantSize != 0 || !reflect.DeepEqual(stsz.sizes, []uint32{10, 20, 30}) {
		t.Fatalf("unexpected stsz: %#v", stsz)
	}
	constantStszPayload := make([]byte, 12)
	binary.BigEndian.PutUint32(constantStszPayload[4:], 7)
	binary.BigEndian.PutUint32(constantStszPayload[8:], 3)
	constantBoxes, err := readBoxes(
		bytes.NewReader(mkbox("stsz", constantStszPayload)),
		0,
		int64(8+len(constantStszPayload)),
	)
	if err != nil {
		t.Fatal(err)
	}
	constantStsz, ok := find(constantBoxes, "stsz")[0].(*stszBox)
	if !ok || constantStsz.sampleCount != 3 || constantStsz.constantSize != 7 || len(constantStsz.sizes) != 0 {
		t.Fatalf("unexpected constant-size stsz: %#v", constantStsz)
	}
	stsc, ok := find(boxes, "stsc")[0].(*stscBox)
	if !ok || !reflect.DeepEqual(stsc.entries, []stscEntry{{1, 2, 1}, {2, 1, 1}}) {
		t.Fatalf("unexpected stsc: %#v", stsc)
	}
	stco, ok := find(boxes, "stco")[0].(*stcoBox)
	if !ok || !reflect.DeepEqual(stco.offsets, []uint64{100, 200}) {
		t.Fatalf("unexpected stco: %#v", stco)
	}
	co64, ok := find(boxes, "co64")[0].(*co64Box)
	if !ok || !reflect.DeepEqual(co64.offsets, []uint64{1<<32 + 100, 1<<32 + 200}) {
		t.Fatalf("unexpected co64: %#v", co64)
	}
}

func TestReadCompositionSyncAndMediaDataBoxes(t *testing.T) {
	cttsV0 := make([]byte, 24)
	binary.BigEndian.PutUint32(cttsV0[4:], 2)
	binary.BigEndian.PutUint32(cttsV0[8:], 2)
	binary.BigEndian.PutUint32(cttsV0[16:], 1)
	binary.BigEndian.PutUint32(cttsV0[20:], 500)
	cttsV1 := make([]byte, 24)
	cttsV1[0] = 1
	binary.BigEndian.PutUint32(cttsV1[4:], 2)
	binary.BigEndian.PutUint32(cttsV1[8:], 1)
	binary.BigEndian.PutUint32(cttsV1[12:], ^uint32(1))
	binary.BigEndian.PutUint32(cttsV1[16:], 2)
	binary.BigEndian.PutUint32(cttsV1[20:], 3)
	stssPayload := make([]byte, 16)
	binary.BigEndian.PutUint32(stssPayload[4:], 2)
	binary.BigEndian.PutUint32(stssPayload[8:], 1)
	binary.BigEndian.PutUint32(stssPayload[12:], 4)
	data := bytes.Join([][]byte{
		mkbox("ctts", cttsV0),
		mkbox("ctts", cttsV1),
		mkbox("stss", stssPayload),
		mkbox("mdat", []byte("media")),
	}, nil)
	boxes, err := readBoxes(bytes.NewReader(data), 0, int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	compositionBoxes := find(boxes, "ctts")
	if len(compositionBoxes) != 2 {
		t.Fatalf("got %d ctts boxes, want 2", len(compositionBoxes))
	}
	v0, ok := compositionBoxes[0].(*cttsBox)
	if !ok || v0.version != 0 || !reflect.DeepEqual(v0.entries, []cttsEntry{{2, 0}, {1, 500}}) {
		t.Fatalf("unexpected ctts version 0: %#v", v0)
	}
	v1, ok := compositionBoxes[1].(*cttsBox)
	if !ok || v1.version != 1 || !reflect.DeepEqual(v1.entries, []cttsEntry{{1, -2}, {2, 3}}) {
		t.Fatalf("unexpected ctts version 1: %#v", v1)
	}
	stss, ok := find(boxes, "stss")[0].(*stssBox)
	if !ok || !reflect.DeepEqual(stss.sampleNumbers, []uint32{1, 4}) {
		t.Fatalf("unexpected stss: %#v", stss)
	}
	mdat, ok := find(boxes, "mdat")[0].(*mdatBox)
	if !ok || mdat.dataStart != mdat.h.payloadStart() || mdat.dataEnd-mdat.dataStart != int64(len("media")) {
		t.Fatalf("unexpected mdat bounds: %#v", mdat)
	}
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
		mktrackWithVideoInfo(640, 480, 3, 15000, 30000),
	))
	m := NewMp4File()
	if !m.Open(p) {
		t.Fatal(m.ErrorInfo())
	}
	got := m.GetBasicInfo()
	want := []BasicVideoStreamInfo{
		{Width: 1920, Height: 1080, FrameRate: 30, FrameCount: 300, DurationSeconds: 10},
		{Width: 640, Height: 480, FrameRate: 2, FrameCount: 3, DurationSeconds: 1.5},
	}
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
