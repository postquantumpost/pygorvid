package vid

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func companionNALs(t *testing.T, stream []byte) [][]byte {
	t.Helper()
	nals, err := SplitAnnexBNALUnits(stream)
	if err != nil {
		t.Fatal(err)
	}
	return nals
}

func TestH264CompanionFilesKeysAndDeterminism(t *testing.T) {
	first := GenerateH264CompanionFiles()
	second := GenerateH264CompanionFiles()
	keys := make([]string, 0, len(first))
	for key := range first {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	expected := append([]string(nil), H264CompanionSuffixes...)
	sort.Strings(expected)
	if !reflect.DeepEqual(keys, expected) {
		t.Fatalf("companion keys = %v; want %v", keys, expected)
	}
	for _, key := range expected {
		if !bytes.Equal(first[key], second[key]) {
			t.Fatalf("%s is not deterministic", key)
		}
	}
}

func TestH264CompanionFilesMatchCanonicalBytes(t *testing.T) {
	expected := map[string]struct {
		size int
		hash string
	}{
		"frame-40-422.mp4":        {8895, "e5a66d62ab0836c8a18a152602bce67e122882207f42e29ff820709c6816b77c"},
		"frame-41-444.mp4":        {12992, "2beeee98781fb81f63716cd7ec7b162c66abdc2ab45326dabe41ea21413c2d01"},
		"frame-41-separate.mp4":   {13072, "353816dc8cb9f79a11e6b00261dd367a35a68c3c51a033e350081ba3f84c4ea6"},
		"frame-42-10bit.mp4":      {8384, "c4a165a1be6a507e563b5b298399804d9b71cffbceff2a54f5320221e37870df"},
		"frame-42-12bit.mp4":      {19137, "fe92c204bdac8fd497687599d4e9975af13a4afaf0f6678914c93edeaa1df7ed"},
		"frame-43-fields.mp4":     {6863, "74b217b854742b458826a96dd8a43764f391d27112cf836921c55a09f4140393"},
		"frame-44-mbaff.mp4":      {6847, "2633954c3df2234752f72fa45bb7aec441d05eecae7e9f5b81aaea4eb0744bef"},
		"frame-45-fmo.mp4":        {6857, "23071982ca210edd3798819005651a4958261d6dfb0db2ee08c377b6f62f3d23"},
		"frame-46-redundant.mp4":  {19251, "dc570e9b1ed7030436e847057f2b0ab547df7ef17032f2d15324e42e01db9072"},
		"frame-47-partitions.mp4": {13381, "55d0581a7a62290402bd93c43a72ef35776c3edb7390e879db7c6e3d33cd058c"},
		"frame-48-sp-si.mp4":      {13076, "cc2eca1521b71bf0b0e66ff3932731b4bcc5085c91c89082a8208a46cd47a543"},
		"frame-49-svc.264":        {6219, "e094925b0ac873f569f84b367254759fbc5ae2536e66fd81605a7c2f363e9d6f"},
		"frame-49-mvc.264":        {6213, "dc8fa9319c70d3111a7fd2c0cab6a8342323fdffa91ff00d302b5993ee719d60"},
		"frame-49-mvcd.264":       {6213, "5d46a64c28462ebeaec725130e5a61d8993febfcf17a769f33478cdeff824983"},
		"frame-49-3davc.264":      {6213, "f95f4f18aadeb145071643d89b32b2733530a4b44e00199292e348a5dfea1511"},
	}

	streams := GenerateH264CompanionFiles()
	for key, want := range expected {
		data := streams[key]
		if len(data) != want.size || fmt.Sprintf("%x", sha256.Sum256(data)) != want.hash {
			t.Errorf("%s differs from canonical corpus: size=%d hash=%x", key, len(data), sha256.Sum256(data))
		}
	}
}

func TestH264CompanionMP4SampleCounts(t *testing.T) {
	files := GenerateH264CompanionFiles()
	expected := map[string]int{
		"frame-40-422.mp4":        1,
		"frame-41-444.mp4":        1,
		"frame-41-separate.mp4":   1,
		"frame-42-10bit.mp4":      1,
		"frame-42-12bit.mp4":      1,
		"frame-43-fields.mp4":     2,
		"frame-44-mbaff.mp4":      1,
		"frame-45-fmo.mp4":        1,
		"frame-46-redundant.mp4":  3,
		"frame-47-partitions.mp4": 2,
		"frame-48-sp-si.mp4":      3,
	}
	for name, count := range expected {
		path := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(path, files[name], 0o600); err != nil {
			t.Fatal(err)
		}
		reader, err := OpenVideoSampleReader(path)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		got := reader.SampleCount()
		reader.Close()
		if got != count {
			t.Errorf("%s sample count = %d; want %d", name, got, count)
		}
	}
}

func TestH264CompanionStreamsRetainCanonicalAnnexBBytes(t *testing.T) {
	expected := map[string]string{
		"frame-40-422.264":        "68290a7a880534b2ad3c746e0691f51eff57fae1646fec992b41c04301be4d25",
		"frame-41-444.264":        "d90e363bb06d3221c11034ee0b457a5c269635c3fdf2fe33407691127ef5c372",
		"frame-41-separate.264":   "a8dc48d97d82dace1b0b045c4a0fb90d09b4a456abd89615eafd354639813364",
		"frame-42-10bit.264":      "ec6016e0aa9e73de3a44eda11e09802ddc7486daccfbd41342192d531d3d7074",
		"frame-42-12bit.264":      "3edd8bf070c4fdabd0a17e68905fcd65f4c010699f648fe87cf8d523e2b5efd4",
		"frame-43-fields.264":     "9b385c9fad6f55d106787833e44adc06a1a264647d73ef5dd7b0aed6895de94e",
		"frame-44-mbaff.264":      "469e6bc89a3149c4572bb6d904aae8a9afd05d1898b56dbfdf8188eb34a98033",
		"frame-45-fmo.264":        "e30e025ffee4d2a0c250b7f6e27ff14ffd68a731fd6fdea7bff331401f16ba64",
		"frame-46-redundant.264":  "270f3104f1fcbb9192a199c10e8149221743f0ee7eaa785d8274785db14ee820",
		"frame-47-partitions.264": "bfddcec6b63a5845c7aff978b195b2c2f25eef7c9d1728c56c24ef34d5d82ae5",
		"frame-48-sp-si.264":      "5dad4cb1dfeb4ac6c9fa36dc8e1378c06298ce2359522b54a968455a042a8289",
	}
	streams := GenerateH264CompanionStreams()
	for name, hash := range expected {
		if got := fmt.Sprintf("%x", sha256.Sum256(streams[name])); got != hash {
			t.Errorf("%s Annex-B SHA256 = %s; want %s", name, got, hash)
		}
	}
}

func TestGeneratedCoreEndAndVUIParity(t *testing.T) {
	encoder := NewH264Encoder()
	wantSPS := "d26ee6f94ca38d480713bb82c5d6e34a237d2f6ac251416f362a968dc0117d64"
	sps := encoder.sequenceParameterSet(4, 2, true, false, false, true)
	if got := fmt.Sprintf("%x", sha256.Sum256(sps)); got != wantSPS {
		t.Fatalf("SPS4 SHA256 = %s; want %s", got, wantSPS)
	}
	for frame, want := range map[uint16]string{
		39: "50c43ccf48216bd32e155a31043ff2840f58d2e4c2bdbd3c7c2ef45894dc4d98",
		59: "8dd9c2b790df2ac778bfd86500114ad03565b3d38e5fbfce3b166a1099117f44",
	} {
		data := encoder.EncodeGeneratedFrame(frame)
		if got := fmt.Sprintf("%x", sha256.Sum256(data)); got != want {
			t.Errorf("frame %d SHA256 = %s; want %s", frame, got, want)
		}
	}
}

func TestH264CompanionSampleFormatsAndScanModes(t *testing.T) {
	streams := GenerateH264CompanionStreams()
	formats := []struct {
		key       string
		profile   uint8
		chroma    uint8
		lumaDepth uint8
		width     uint32
		height    uint32
		separate  bool
		frameOnly bool
		mbaff     bool
	}{
		{"frame-40-422.264", 122, 2, 8, 64, 64, false, true, false},
		{"frame-41-444.264", 244, 3, 8, 64, 64, false, true, false},
		{"frame-41-separate.264", 244, 3, 8, 64, 64, true, true, false},
		{"frame-42-10bit.264", 110, 1, 10, 64, 64, false, true, false},
		{"frame-42-12bit.264", 244, 3, 12, 64, 64, false, true, false},
		{"frame-43-fields.264", 100, 1, 8, 64, 64, false, false, false},
		{"frame-44-mbaff.264", 100, 1, 8, 64, 64, false, false, true},
	}
	for _, format := range formats {
		nals := companionNALs(t, streams[format.key])
		info, err := ParseSPSStructure(nals[0])
		if err != nil {
			t.Fatalf("%s SPS: %v", format.key, err)
		}
		if info.ProfileIDC != format.profile || info.ChromaFormatIDC != format.chroma ||
			info.BitDepthLuma != format.lumaDepth || info.Width != format.width ||
			info.Height != format.height || info.SeparateColourPlane != format.separate ||
			info.FrameMbsOnly != format.frameOnly || info.MbAdaptiveFrameField != format.mbaff {
			t.Errorf("%s SPS = %+v", format.key, info)
		}
		if nals[1][0]&0x1f != 8 {
			t.Errorf("%s NALs do not contain PPS", format.key)
		}
	}
}

func TestH264CompanionSliceFamilySyntax(t *testing.T) {
	streams := GenerateH264CompanionStreams()
	fields := companionNALs(t, streams["frame-43-fields.264"])
	fieldSPS, err := ParseSPSStructure(fields[0])
	if err != nil {
		t.Fatal(err)
	}
	fieldPPS, err := ParsePPSStructure(fields[1])
	if err != nil {
		t.Fatal(err)
	}
	top, err := ParseSliceHeader(fields[2], fieldSPS, fieldPPS)
	if err != nil {
		t.Fatal(err)
	}
	bottom, err := ParseSliceHeader(fields[3], fieldSPS, fieldPPS)
	if err != nil {
		t.Fatal(err)
	}
	if !top.FieldPicFlag || top.BottomFieldFlag || !bottom.FieldPicFlag || !bottom.BottomFieldFlag {
		t.Fatalf("field flags = top:%+v bottom:%+v", top, bottom)
	}

	fmo := companionNALs(t, streams["frame-45-fmo.264"])
	fmoPPS, err := ParsePPSStructure(fmo[1])
	if err != nil {
		t.Fatal(err)
	}
	if fmoPPS.NumSliceGroupsMinus1 != 1 || fmoPPS.SliceGroupMapType != 0 ||
		!reflect.DeepEqual(fmoPPS.RunLengthMinus1, []uint32{7, 7}) {
		t.Fatalf("FMO PPS = %+v", fmoPPS)
	}

	redundant := companionNALs(t, streams["frame-46-redundant.264"])
	redundantPPS, err := ParsePPSStructure(redundant[1])
	if err != nil || !redundantPPS.RedundantPicCntPresent {
		t.Fatalf("redundant PPS = %+v, %v", redundantPPS, err)
	}
	types := companionNALTypes(t, streams["frame-47-partitions.264"])
	want := []byte{7, 8, 9, 5, 9}
	for mb := 0; mb < 16; mb++ {
		want = append(want, 2, 3, 4)
	}
	if !reflect.DeepEqual(types, want) {
		t.Fatalf("partition NAL types = %v", types)
	}
	types = companionNALTypes(t, streams["frame-48-sp-si.264"])
	if !reflect.DeepEqual(types, []byte{7, 8, 9, 5, 9, 1, 9, 1}) {
		t.Fatalf("SP/SI NAL types = %v", types)
	}
}

func TestH264CompanionExtensionStreamsHaveDistinctStructure(t *testing.T) {
	streams := GenerateH264CompanionStreams()
	wantTypes := map[string][]byte{
		"frame-49-svc.264":   {7, 8, 5, 14, 20},
		"frame-49-mvc.264":   {7, 8, 5, 20},
		"frame-49-mvcd.264":  {7, 8, 5, 21},
		"frame-49-3davc.264": {7, 8, 5, 21},
	}
	extensions := make(map[string]bool)
	for key, types := range wantTypes {
		nals := companionNALs(t, streams[key])
		got := companionNALTypes(t, streams[key])
		if !reflect.DeepEqual(got, types) {
			t.Fatalf("%s NAL types = %v; want %v", key, got, types)
		}
		extensionIndex := len(nals) - 1
		if len(nals[extensionIndex]) < 5 {
			t.Fatalf("%s VCL has no Annex extension header", key)
		}
		extensions[fmt.Sprintf("%d:%d:%x", nals[extensionIndex][0]&0x1f, len(nals), nals[extensionIndex][1:])] = true
	}
	if len(extensions) != len(wantTypes) {
		t.Fatalf("extension headers are not distinct: %v", extensions)
	}
}

func companionNALTypes(t *testing.T, stream []byte) []byte {
	t.Helper()
	nals := companionNALs(t, stream)
	types := make([]byte, len(nals))
	for index, nal := range nals {
		types[index] = nal[0] & 0x1f
	}
	return types
}
