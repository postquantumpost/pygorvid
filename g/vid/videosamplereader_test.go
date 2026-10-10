package vid

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func sampleReaderFixture(useCo64, constantSize bool, avcc []byte) ([]byte, [][]byte) {
	samples := [][]byte{
		{0, 0, 0, 2, 0x65, 0x80},
		{0, 0, 0, 3, 0x41, 0x80, 0x80},
		{0, 0, 0, 4, 0x41, 0x80, 0x80, 0x80},
	}
	if constantSize {
		samples = [][]byte{{0, 0, 0, 2, 0x65, 0x80}, {0, 0, 0, 2, 0x65, 0x80}, {0, 0, 0, 2, 0x65, 0x80}}
	}

	tkhd := make([]byte, 84)
	mdhd := make([]byte, 20)
	binary.BigEndian.PutUint32(mdhd[12:], 1000)
	hdlr := mkbox("hdlr", make([]byte, 8), []byte("vide"), make([]byte, 13))
	avc1Payload := make([]byte, 78)
	binary.BigEndian.PutUint16(avc1Payload[24:], 16)
	binary.BigEndian.PutUint16(avc1Payload[26:], 16)
	avc1 := mkbox("avc1", avc1Payload, mkbox("avcC", avcc))
	stsdPayload := make([]byte, 8)
	binary.BigEndian.PutUint32(stsdPayload[4:], 1)
	stsdPayload = append(stsdPayload, avc1...)
	stsd := mkbox("stsd", stsdPayload)

	stszPayload := make([]byte, 12)
	if constantSize {
		binary.BigEndian.PutUint32(stszPayload[4:], uint32(len(samples[0])))
	} else {
		stszPayload = make([]byte, 24)
	}
	binary.BigEndian.PutUint32(stszPayload[8:], uint32(len(samples)))
	if !constantSize {
		for i, sample := range samples {
			binary.BigEndian.PutUint32(stszPayload[12+i*4:], uint32(len(sample)))
		}
	}
	stsz := mkbox("stsz", stszPayload)

	stscPayload := make([]byte, 32)
	binary.BigEndian.PutUint32(stscPayload[4:], 2)
	for i, entry := range [][3]uint32{{1, 2, 1}, {2, 1, 1}} {
		for j, value := range entry {
			binary.BigEndian.PutUint32(stscPayload[8+i*12+j*4:], value)
		}
	}
	stsc := mkbox("stsc", stscPayload)
	sttsPayload := make([]byte, 16)
	binary.BigEndian.PutUint32(sttsPayload[4:], 1)
	binary.BigEndian.PutUint32(sttsPayload[8:], 3)
	binary.BigEndian.PutUint32(sttsPayload[12:], 1000)
	stts := mkbox("stts", sttsPayload)
	cttsPayload := make([]byte, 32)
	cttsPayload[0] = 1
	binary.BigEndian.PutUint32(cttsPayload[4:], 3)
	for i, entry := range [][2]uint32{{1, 0}, {1, ^uint32(499)}, {1, 1000}} {
		binary.BigEndian.PutUint32(cttsPayload[8+i*8:], entry[0])
		binary.BigEndian.PutUint32(cttsPayload[12+i*8:], entry[1])
	}
	ctts := mkbox("ctts", cttsPayload)
	stssPayload := make([]byte, 16)
	binary.BigEndian.PutUint32(stssPayload[4:], 2)
	binary.BigEndian.PutUint32(stssPayload[8:], 1)
	binary.BigEndian.PutUint32(stssPayload[12:], 3)
	stss := mkbox("stss", stssPayload)

	ftyp := mkbox("ftyp", []byte("isom\x00\x00\x00\x00isom"))
	free := mkbox("free", []byte("xx"))
	makeMoov := func(offsets [2]uint64) []byte {
		offsetPayload := make([]byte, 16)
		binary.BigEndian.PutUint32(offsetPayload[4:], 2)
		if useCo64 {
			offsetPayload = make([]byte, 24)
			binary.BigEndian.PutUint32(offsetPayload[4:], 2)
			binary.BigEndian.PutUint64(offsetPayload[8:], offsets[0])
			binary.BigEndian.PutUint64(offsetPayload[16:], offsets[1])
		} else {
			binary.BigEndian.PutUint32(offsetPayload[8:], uint32(offsets[0]))
			binary.BigEndian.PutUint32(offsetPayload[12:], uint32(offsets[1]))
		}
		offsetType := "stco"
		if useCo64 {
			offsetType = "co64"
		}
		stbl := mkbox("stbl", stsd, stsz, stsc, stts, ctts, stss, mkbox(offsetType, offsetPayload))
		minf := mkbox("minf", stbl)
		mdia := mkbox("mdia", mkbox("mdhd", mdhd), hdlr, minf)
		return mkbox("moov", mkbox("trak", mkbox("tkhd", tkhd), mdia))
	}
	placeholder := makeMoov([2]uint64{})
	mdatDataStart := uint64(len(ftyp) + len(free) + len(placeholder) + 8)
	offsets := [2]uint64{mdatDataStart, mdatDataStart + uint64(len(samples[0])+len(samples[1]))}
	moov := makeMoov(offsets)
	data := append(append(append(ftyp, free...), moov...), mkbox("mdat", samples...)...)
	return data, samples
}

func TestVideoSampleReaderCompactFixtures(t *testing.T) {
	fixtures := []struct {
		name   string
		level  uint8
		width  uint32
		height uint32
	}{
		{name: "high42-1080p.mp4", level: 42},
		{name: "high52-2160p.mp4", level: 52},
	}
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			path := filepath.Join("..", "..", "testdata", "h264", fixture.name)
			reader, err := OpenVideoSampleReader(path)
			if err != nil {
				t.Fatal(err)
			}
			defer reader.Close()
			configuration := reader.Configuration()
			if configuration.Profile != 100 || configuration.Level != fixture.level || configuration.NALLengthSize != 4 {
				t.Fatalf("unexpected configuration: %+v", configuration)
			}
			if len(configuration.SequenceSets) == 0 || len(configuration.PictureSets) == 0 {
				t.Fatalf("missing parameter sets: %+v", configuration)
			}
			if reader.SampleCount() != 48 {
				t.Fatalf("got %d samples, want 48", reader.SampleCount())
			}
			var count uint64
			var lastDTS int64
			for {
				sample, ok, err := reader.NextSample()
				if err != nil {
					t.Fatal(err)
				}
				if !ok {
					break
				}
				if len(sample.Data) == 0 || sample.DurationTicks == 0 {
					t.Fatalf("invalid sample %d: %+v", sample.Index, sample)
				}
				if count > 0 && sample.DTSTicks < lastDTS {
					t.Fatalf("DTS decreased at sample %d", sample.Index)
				}
				lastDTS = sample.DTSTicks
				count++
			}
			if count != 48 {
				t.Fatalf("read %d samples, want 48", count)
			}
		})
	}
}

func TestVideoSampleReaderDecodeOrderDependenciesForPresentationIndex(t *testing.T) {
	path := filepath.Join("..", "..", "testdata", "h264", "high42-1080p.mp4")
	reader, err := OpenVideoSampleReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	dependencies, err := reader.DecodeOrderDependencySamples(1)
	if err != nil {
		t.Fatal(err)
	}
	got := make([][3]int64, len(dependencies))
	for index, sample := range dependencies {
		got[index] = [3]int64{int64(sample.Index), sample.DTSTicks, sample.PTSTicks}
	}
	want := [][3]int64{{0, 0, 256}, {1, 256, 1024}, {2, 512, 512}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("dependency window = %v; want %v", got, want)
	}
	first, ok, err := reader.NextSample()
	if err != nil || !ok || first.Index != 0 {
		t.Fatalf("NextSample() after indexed reads = %#v, %v, %v; want sample 0", first, ok, err)
	}
	dependencies, err = reader.DecodeOrderDependencySamples(3)
	if err != nil || len(dependencies) != 4 {
		t.Fatalf("dependency window for presentation frame 3 = %d samples, %v; want 4", len(dependencies), err)
	}
	if _, err := reader.DecodeOrderDependencySamples(uint64(reader.SampleCount())); err == nil {
		t.Fatal("DecodeOrderDependencySamples accepted an out-of-range presentation index")
	}
}

func TestVideoSampleReaderTablesAndSamples(t *testing.T) {
	for _, test := range []struct {
		name         string
		useCo64      bool
		constantSize bool
	}{
		{name: "stco-variable", useCo64: false, constantSize: false},
		{name: "co64-constant", useCo64: true, constantSize: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			avcc := []byte{1, 100, 0, 42, 0xff, 0xe1, 0, 4, 0x67, 100, 0, 42, 1, 0, 2, 0x68, 0xee}
			data, wantSamples := sampleReaderFixture(test.useCo64, test.constantSize, avcc)
			path := filepath.Join(t.TempDir(), "sample.mp4")
			if err := os.WriteFile(path, data, 0o644); err != nil {
				t.Fatal(err)
			}
			reader, err := OpenVideoSampleReader(path)
			if err != nil {
				t.Fatal(err)
			}
			defer reader.Close()
			config := reader.Configuration()
			if config.Profile != 100 || config.Level != 42 || config.NALLengthSize != 4 || config.Timescale != 1000 {
				t.Fatalf("unexpected AVC configuration: %+v", config)
			}
			if reader.SampleCount() != 3 {
				t.Fatalf("got %d samples, want 3", reader.SampleCount())
			}
			var got []CompressedSample
			for {
				sample, ok, err := reader.NextSample()
				if err != nil {
					t.Fatal(err)
				}
				if !ok {
					break
				}
				got = append(got, sample)
			}
			if len(got) != len(wantSamples) {
				t.Fatalf("got %d samples, want %d", len(got), len(wantSamples))
			}
			for i := range got {
				if !reflect.DeepEqual(got[i].Data, wantSamples[i]) {
					t.Errorf("sample %d bytes differ", i)
				}
			}
			if got[0].DTSTicks != 0 || got[1].DTSTicks != 1000 || got[2].DTSTicks != 2000 {
				t.Errorf("unexpected DTS values: %d %d %d", got[0].DTSTicks, got[1].DTSTicks, got[2].DTSTicks)
			}
			if got[0].PTSTicks != 0 || got[1].PTSTicks != 500 || got[2].PTSTicks != 3000 {
				t.Errorf("unexpected PTS values: %d %d %d", got[0].PTSTicks, got[1].PTSTicks, got[2].PTSTicks)
			}
			if got[0].DurationTicks != 1000 || got[1].DurationTicks != 1000 || got[2].DurationTicks != 1000 {
				t.Errorf("unexpected durations: %d %d %d", got[0].DurationTicks, got[1].DurationTicks, got[2].DurationTicks)
			}
			if !got[0].IsSync || got[1].IsSync || !got[2].IsSync {
				t.Errorf("unexpected sync flags: %v %v %v", got[0].IsSync, got[1].IsSync, got[2].IsSync)
			}
			if _, ok, err := reader.NextSample(); err != nil || ok {
				t.Errorf("EOF result = ok %v, err %v", ok, err)
			}
			if err := reader.Close(); err != nil {
				t.Fatal(err)
			}
			if _, ok, err := reader.NextSample(); err == nil || ok {
				t.Errorf("read after close = ok %v, err %v", ok, err)
			}
		})
	}
}

func TestVideoSampleReaderRejectsInvalidAVCConfiguration(t *testing.T) {
	base := []byte{1, 100, 0, 42, 0xff, 0xe1, 0, 4, 0x67, 100, 0, 42, 1, 0, 2, 0x68, 0xee}
	tests := []struct {
		name   string
		mutate func([]byte) []byte
	}{
		{name: "unsupported-length-size", mutate: func(data []byte) []byte { data[4] = 0xfe; return data }},
		{name: "invalid-reserved-bits", mutate: func(data []byte) []byte { data[4] = 0x7f; return data }},
		{name: "wrong-sps-type", mutate: func(data []byte) []byte { data[8] = 0x68; return data }},
		{name: "wrong-pps-type", mutate: func(data []byte) []byte { data[15] = 0x67; return data }},
		{name: "truncated-pps", mutate: func(data []byte) []byte { return data[:len(data)-1] }},
		{name: "trailing-data", mutate: func(data []byte) []byte { return append(data, 0) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			avcc := test.mutate(append([]byte(nil), base...))
			data, _ := sampleReaderFixture(false, false, avcc)
			path := filepath.Join(t.TempDir(), "invalid-avcc.mp4")
			if err := os.WriteFile(path, data, 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := OpenVideoSampleReader(path); err == nil {
				t.Fatal("expected invalid avcC record to be rejected")
			}
		})
	}
}

func TestVideoSampleReaderRejectsTruncatedNALUnit(t *testing.T) {
	avcc := []byte{1, 100, 0, 42, 0xff, 0xe1, 0, 4, 0x67, 100, 0, 42, 1, 0, 2, 0x68, 0xee}
	data, _ := sampleReaderFixture(false, false, avcc)
	firstSample := []byte{0, 0, 0, 2, 0x65, 0x80}
	offset := bytes.Index(data, firstSample)
	if offset < 0 {
		t.Fatal("could not find first sample in fixture")
	}
	data[offset+3] = 3
	path := filepath.Join(t.TempDir(), "truncated-nal.mp4")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	reader, err := OpenVideoSampleReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if _, ok, err := reader.NextSample(); ok || err == nil || err.Error() != "sample 0: NAL unit is truncated" {
		t.Fatalf("NextSample() = ok %v, err %v; want truncated NAL error", ok, err)
	}
	if reader.nextIndex != 0 {
		t.Fatalf("reader advanced to sample %d after failure", reader.nextIndex)
	}
}

func TestVideoSampleReaderRejectsInvalidSampleTables(t *testing.T) {
	tests := []struct {
		name      string
		boxType   string
		relative  int
		value     uint32
		wantError string
	}{
		{name: "malformed-timing-table", boxType: "stts", relative: 12, value: 2, wantError: "timing entries do not cover all samples"},
		{name: "invalid-media-offset", boxType: "stco", relative: 12, value: 0, wantError: "outside every mdat payload"},
		{name: "unsupported-sample-description", boxType: "stsc", relative: 20, value: 2, wantError: "unsupported sample description"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			avcc := []byte{1, 100, 0, 42, 0xff, 0xe1, 0, 4, 0x67, 100, 0, 42, 1, 0, 2, 0x68, 0xee}
			data, _ := sampleReaderFixture(false, false, avcc)
			marker := bytes.Index(data, []byte(test.boxType))
			if marker < 0 {
				t.Fatalf("could not find %s box", test.boxType)
			}
			binary.BigEndian.PutUint32(data[marker+test.relative:], test.value)
			path := filepath.Join(t.TempDir(), "invalid-table.mp4")
			if err := os.WriteFile(path, data, 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := OpenVideoSampleReader(path); err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("OpenVideoSampleReader() error = %v; want %q", err, test.wantError)
			}
		})
	}
}

type demuxPacketReference struct {
	index    uint64
	offset   uint64
	size     uint32
	dts      int64
	pts      int64
	duration uint32
	isSync   bool
}

type demuxFileReference struct {
	parameterSetsHex string
	packets          []demuxPacketReference
}

func TestVideoSampleReaderActiveCorpus(t *testing.T) {
	repositoryRoot := filepath.Join("..", "..")
	referenceFile, err := os.Open(filepath.Join(repositoryRoot, "silkroad1-demux.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	defer referenceFile.Close()
	references := make(map[string]*demuxFileReference)
	scanner := bufio.NewScanner(referenceFile)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		switch fields[0] {
		case "S":
			if len(fields) != 4 {
				t.Fatalf("invalid sample reference row: %q", line)
			}
			packetCount, err := strconv.Atoi(fields[2])
			if err != nil {
				t.Fatal(err)
			}
			references[fields[1]] = &demuxFileReference{
				parameterSetsHex: fields[3],
				packets:          make([]demuxPacketReference, 0, packetCount),
			}
		case "P":
			if len(fields) != 9 || references[fields[1]] == nil {
				t.Fatalf("invalid packet reference row: %q", line)
			}
			index, err := strconv.ParseUint(fields[2], 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			offset, err := strconv.ParseUint(fields[3], 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			size, err := strconv.ParseUint(fields[4], 10, 32)
			if err != nil {
				t.Fatal(err)
			}
			dts, err := strconv.ParseInt(fields[5], 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			pts, err := strconv.ParseInt(fields[6], 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			duration, err := strconv.ParseUint(fields[7], 10, 32)
			if err != nil {
				t.Fatal(err)
			}
			references[fields[1]].packets = append(references[fields[1]].packets, demuxPacketReference{
				index: index, offset: offset, size: uint32(size), dts: dts, pts: pts,
				duration: uint32(duration), isSync: fields[8] == "1",
			})
		default:
			t.Fatalf("unknown demux reference row: %q", line)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	manifest, err := os.ReadFile(filepath.Join(repositoryRoot, "silkroad1.sha256"))
	if err != nil {
		t.Fatal(err)
	}
	activePaths := make(map[string]struct{})
	for _, line := range strings.Split(string(manifest), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 {
			activePaths[fields[1]] = struct{}{}
		}
	}
	if len(references) != len(activePaths) {
		t.Fatalf("got references for %d files, want %d active files", len(references), len(activePaths))
	}
	for relativePath := range activePaths {
		if references[relativePath] == nil {
			t.Fatalf("active file %s has no demux reference", relativePath)
		}
	}

	for relativePath, expected := range references {
		t.Run(filepath.Base(relativePath), func(t *testing.T) {
			path := filepath.Join(repositoryRoot, relativePath)
			source, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			reader, err := OpenVideoSampleReader(path)
			if err != nil {
				t.Fatal(err)
			}
			defer reader.Close()
			if reader.SampleCount() != len(expected.packets) {
				t.Fatalf("sample count = %d, want %d", reader.SampleCount(), len(expected.packets))
			}
			configuration := reader.Configuration()
			var parameterSets []byte
			for _, sets := range [][][]byte{configuration.SequenceSets, configuration.PictureSets} {
				for _, parameterSet := range sets {
					var length [4]byte
					binary.BigEndian.PutUint32(length[:], uint32(len(parameterSet)))
					parameterSets = append(parameterSets, length[:]...)
					parameterSets = append(parameterSets, parameterSet...)
				}
			}
			if actual := hex.EncodeToString(parameterSets); actual != expected.parameterSetsHex {
				t.Fatalf("parameter sets = %s, want %s", actual, expected.parameterSetsHex)
			}
			for packetIndex, packet := range expected.packets {
				sample, ok, err := reader.NextSample()
				if err != nil || !ok {
					t.Fatalf("sample %d: ok=%v err=%v", packetIndex, ok, err)
				}
				if sample.Index != packet.index || sample.DTSTicks != packet.dts || sample.PTSTicks != packet.pts ||
					sample.DurationTicks != packet.duration || sample.IsSync != packet.isSync || len(sample.Data) != int(packet.size) {
					t.Fatalf("sample %d metadata differs from ffprobe reference", packetIndex)
				}
				end := packet.offset + uint64(packet.size)
				if end > uint64(len(source)) || !bytes.Equal(sample.Data, source[int(packet.offset):int(end)]) {
					t.Fatalf("sample %d bytes do not match reference range [%d,%d)", packetIndex, packet.offset, end)
				}
			}
			if _, ok, err := reader.NextSample(); err != nil || ok {
				t.Fatalf("unexpected sample after %d packets: ok=%v err=%v", len(expected.packets), ok, err)
			}
		})
	}
}
