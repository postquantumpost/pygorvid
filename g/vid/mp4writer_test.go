package vid

import (
	"bytes"
	"encoding/binary"
	"os"
	"strings"
	"testing"
)

func TestMp4WriterOptionalMetadata(t *testing.T) {
	writer, encoder := NewMp4Writer(), NewH264Encoder()
	plain := writer.Encode(encoder)
	for _, metadata := range []*Mp4Metadata{nil, {}} {
		payload, err := writer.EncodeWithMetadata(encoder, metadata)
		if err != nil || !bytes.Equal(plain, payload) {
			t.Fatalf("empty metadata changed output: %v", err)
		}
	}
	value := "Caf\u00e9 \u65e5\u672c \U0001f4f7\x00tail"
	payload, err := writer.EncodeWithMetadata(encoder, &Mp4Metadata{Title: value})
	mdta, mdtaErr := mp4MetadataBox(&Mp4Metadata{Title: value})
	if err != nil || !bytes.Contains(payload, []byte(value)) ||
		mdtaErr != nil || len(mdta) != 69+32+len("title")+len(value) ||
		!bytes.Contains(payload, mdta) || !bytes.Contains(payload, []byte("mdirappl")) {
		t.Fatalf("UTF-8 metadata was not preserved: %v", err)
	}

	if bytes.Contains(plain, []byte("mdta")) || !bytes.Contains(payload, []byte("ilst")) {
		t.Fatal("metadata presence does not match argument")
	}

	for _, value := range []string{"\xff", "\xc0\x80", "\xed\xa0\x80", "\xf4\x90\x80\x80", "\xe2\x82"} {
		payload, err := writer.EncodeWithMetadata(encoder, &Mp4Metadata{Title: value})
		if payload != nil || err == nil || !strings.Contains(err.Error(), "title is not valid UTF-8") {
			t.Fatalf("invalid UTF-8 accepted: %x, %v", value, err)
		}
	}
}

func TestMp4CompatibilityNumbers(t *testing.T) {
	for _, value := range []string{"0", "1", "001", "65535", "65536", "-1", "1/2", "\u0661"} {
		payload, err := mp4CompatibilityMetadataBox(&Mp4Metadata{Track: value, Disc: value})
		if err != nil || !bytes.Contains(payload, []byte(value)) {
			t.Fatalf("raw number lost: %q", value)
		}

		binary := value == "0" || value == "1" || value == "001" || value == "65535"
		if bytes.Contains(payload, []byte("trkn")) != binary || bytes.Contains(payload, []byte("disk")) != binary {
			t.Fatalf("unexpected binary number tag for %q", value)
		}
	}
}

func TestMp4WindowsXtra(t *testing.T) {
	value := "A\u00e9\u65e5\U0001f4f7\x00tail"
	data := []byte{'A', 0, 0xe9, 0, 0xe5, 0x65, 0x3d, 0xd8, 0xf7, 0xdc, 0, 0,
		't', 0, 'a', 0, 'i', 0, 'l', 0, 0, 0}
	var records []byte
	for _, name := range []string{"Title", "pygorvid_title"} {
		header := make([]byte, 8)
		binary.BigEndian.PutUint32(header, uint32(18+len(name)+len(data)))
		binary.BigEndian.PutUint32(header[4:], uint32(len(name)))
		records = append(records, header...)
		records = append(records, []byte(name)...)
		header = make([]byte, 10)
		binary.BigEndian.PutUint32(header, 1)
		binary.BigEndian.PutUint32(header[4:], uint32(6+len(data)))
		binary.BigEndian.PutUint16(header[8:], 8)
		records = append(records, header...)
		records = append(records, data...)
	}
	expected := make([]byte, 8)
	binary.BigEndian.PutUint32(expected, uint32(8+len(records)))
	copy(expected[4:], "Xtra")
	expected = append(expected, records...)
	metadata := &Mp4Metadata{Title: value}
	actual, err := mp4WindowsMetadataBox(metadata)
	if err != nil || !bytes.Equal(actual, expected) {
		t.Fatalf("incorrect Xtra layout: %x, %v", actual, err)
	}
	payload, err := NewMp4Writer().EncodeWithMetadata(NewH264Encoder(), metadata)
	if err != nil || !bytes.Contains(payload, expected) {
		t.Fatalf("missing Xtra: %v", err)
	}
	for _, empty := range []*Mp4Metadata{nil, {}} {
		payload, err := mp4WindowsMetadataBox(empty)
		if err != nil || len(payload) != 0 {
			t.Fatalf("empty Xtra emitted: %v", err)
		}
	}
	for _, date := range []string{"2016", "2016-12-31 23:59:60 UTC", "2016junk", "-2016", "\u0662\u0660\u0661\u0666", ""} {
		payload, err := mp4WindowsMetadataBox(&Mp4Metadata{Date: date})
		year := date == "2016" || date == "2016-12-31 23:59:60 UTC"
		if err != nil || bytes.Contains(payload, []byte("WM/Year")) != year {
			t.Fatalf("incorrect year for %q: %v", date, err)
		}
	}
}

func TestGeneratedH264FeatureFrames31Through39(t *testing.T) {
	t.Setenv("PATH", "")
	const path = "generated-h264-features.mp4"
	payload := NewMp4Writer().Encode(NewH264Encoder())
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(path) })

	reader, err := OpenVideoSampleReader(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reader.Close() })
	if got := reader.SampleCount(); got != 60 {
		t.Fatalf("sample count = %d; want 60", got)
	}
	configuration := reader.Configuration()
	if len(configuration.SequenceSets) != 4 || len(configuration.PictureSets) != 10 {
		t.Fatalf("avcC parameter set counts = %d/%d; want 4/10",
			len(configuration.SequenceSets), len(configuration.PictureSets))
	}
	sps := make(map[uint32]SPSInfo)
	for _, nal := range configuration.SequenceSets {
		info, err := ParseSPS(nal)
		if err != nil {
			t.Fatal(err)
		}
		sps[info.ID] = info
	}
	pps := make(map[uint32]PPSInfo)
	for _, nal := range configuration.PictureSets {
		info, err := ParsePPS(nal)
		if err != nil {
			t.Fatal(err)
		}
		pps[info.PictureParameterSetID] = info
	}
	if sps[1].PicOrderCntType != 1 || !sps[1].GapsInFrameNumValueAllowed ||
		!sps[3].SeqScalingMatrixPresent || !sps[3].SeqScalingListPresent[0] ||
		!sps[3].SeqScalingListPresent[6] || sps[3].SeqScalingLists[0][0] != 12 ||
		sps[3].SeqScalingLists[6][0] != 20 || !sps[4].QPPrimeYZeroTransformBypass ||
		!sps[4].VUIParametersPresent || !pps[5].Transform8x8Mode {
		t.Fatalf("generated parameter-set features missing: SPS=%+v PPS5=%+v", sps, pps[5])
	}
	nalCounts := []int{1, 2, 2, 1, 1, 1, 1, 1, 3}
	for offset, wantCount := range nalCounts {
		sample, err := reader.SampleAt(uint64(31 + offset))
		if err != nil {
			t.Fatal(err)
		}
		nals, err := SplitH264SampleNALs(sample.Data, 4)
		if err != nil {
			t.Fatal(err)
		}
		if len(nals) != wantCount {
			t.Fatalf("frame %d NAL count = %d; want %d", 31+offset, len(nals), wantCount)
		}
		var headers []SliceHeader
		for _, nal := range nals {
			header, err := ParseNALHeader(nal)
			if err != nil {
				t.Fatal(err)
			}
			if header.UnitType != 1 && header.UnitType != 5 {
				continue
			}
			_, ppsID, err := readGeneratedPPSID(nal)
			if err != nil {
				t.Fatal(err)
			}
			slice, err := ParseSliceHeader(nal, sps[pps[ppsID].SequenceParameterSetID], pps[ppsID])
			if err != nil {
				t.Fatal(err)
			}
			headers = append(headers, slice)
		}
		switch offset {
		case 0:
			if len(headers) != 1 || headers[0].DisableDeblockingFilterIDC != 0 ||
				headers[0].SliceAlphaC0OffsetDiv2 != 2 || headers[0].SliceBetaOffsetDiv2 != -3 {
				t.Fatalf("frame 31 deblocking header = %+v", headers)
			}
		case 1:
			if len(headers) != 2 || headers[0].DisableDeblockingFilterIDC != 0 ||
				headers[0].SliceAlphaC0OffsetDiv2 != 2 || headers[0].SliceBetaOffsetDiv2 != -2 ||
				headers[1].DisableDeblockingFilterIDC != 1 {
				t.Fatalf("frame 32 deblocking headers = %+v", headers)
			}
		case 2:
			deltas := []int64{1, -1}
			for index, header := range headers {
				if header.FirstMacroblockInSlice != uint32(index*8) || header.SliceQPDelta != deltas[index] {
					t.Fatalf("frame 33 slice %d = %+v", index, header)
				}
			}
		case 3:
			if len(headers) != 1 || !headers[0].IDR || headers[0].PictureParameterSetID != 6 ||
				headers[0].PicOrderCntType != 1 || !headers[0].HasDeltaPicOrderCnt0 {
				t.Fatalf("frame 34 POC type 1 header = %+v", headers)
			}
		case 4, 5:
			if len(headers) != 1 || headers[0].PictureParameterSetID != 5 {
				t.Fatalf("frame %d 8x8 PPS header = %+v", 35+offset-4, headers)
			}
		}
	}

	decoder := NewH264Decoder(reader)
	decoder.SetThreads(1)
	for index := 0; index < 60; index++ {
		frame, err := decoder.DecodeFrame(uint64(index))
		if err != nil {
			t.Fatalf("native DecodeFrame(%d): %v", index, err)
		}
		if frame.Width != 64 || frame.Height != 64 {
			t.Fatalf("native frame %d dimensions = %dx%d", index, frame.Width, frame.Height)
		}
	}

	for _, test := range []struct {
		presentationIndex uint64
		all               bool
	}{{35, true}, {36, true}} {
		inspector := NewH264Decoder(reader)
		inspector.SetThreads(1)
		if _, err := inspector.DecodeFrame(test.presentationIndex); err != nil {
			t.Fatalf("inspect frame %d: %v", test.presentationIndex, err)
		}
		trueCount := 0
		var modes [9]bool
		for _, block := range inspector.nativeBlocks {
			if block.result.TransformSize8x8 {
				trueCount++
				for _, mode := range block.result.Luma8x8Modes {
					modes[mode] = true
				}
			}
		}
		if trueCount == 0 || test.all != (trueCount == len(inspector.nativeBlocks)) {
			t.Fatalf("frame %d 8x8 transform count = %d/%d", test.presentationIndex,
				trueCount, len(inspector.nativeBlocks))
		}
		if test.presentationIndex == 36 && (!modes[0] || !modes[1] || !modes[2]) {
			t.Fatalf("frame 36 8x8 prediction modes = %v", modes)
		}
	}

	frame38, err := reader.SampleAt(38)
	if err != nil {
		t.Fatal(err)
	}
	frame38NALs, err := SplitH264SampleNALs(frame38.Data, 4)
	if err != nil {
		t.Fatal(err)
	}
	frame39, err := reader.SampleAt(39)
	if err != nil {
		t.Fatal(err)
	}
	nals, err := SplitH264SampleNALs(frame39.Data, 4)
	if err != nil {
		t.Fatal(err)
	}
	types := make([]uint8, 0, len(nals))
	for _, nal := range nals {
		header, err := ParseNALHeader(nal)
		if err != nil {
			t.Fatal(err)
		}
		types = append(types, header.UnitType)
	}
	if !bytes.Equal(types, []byte{9, 6, 5}) {
		t.Fatalf("frame 39 NAL types = %v", types)
	}
	if len(frame38NALs) != 1 || !bytes.Equal(frame38NALs[0], nals[2]) {
		t.Fatal("frame 39 did not preserve frame 38's coded picture")
	}
	frame59, err := reader.SampleAt(59)
	if err != nil {
		t.Fatal(err)
	}
	frame59NALs, err := SplitH264SampleNALs(frame59.Data, 4)
	if err != nil {
		t.Fatal(err)
	}
	frame59Types := make([]uint8, len(frame59NALs))
	for index, nal := range frame59NALs {
		header, err := ParseNALHeader(nal)
		if err != nil {
			t.Fatal(err)
		}
		frame59Types[index] = header.UnitType
	}
	if !bytes.Equal(frame59Types, []byte{5, 10, 11}) {
		t.Fatalf("frame 59 NAL types = %v", frame59Types)
	}
}

func readGeneratedPPSID(nal []byte) (bool, uint32, error) {
	rbsp, err := EBSPToRBSP(nal[1:])
	if err != nil {
		return false, 0, err
	}
	reader := NewBitReader(rbsp)
	if _, err := reader.ReadUE(); err != nil {
		return false, 0, err
	}
	if _, err := reader.ReadUE(); err != nil {
		return false, 0, err
	}
	ppsID, err := reader.ReadUE()
	return err == nil, ppsID, err
}
