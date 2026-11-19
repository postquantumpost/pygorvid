package vid

// H264CompanionSuffixes is the deterministic set of Annex-B companion streams
// emitted by vidprobe alongside its generated MP4.
var H264CompanionSuffixes = []string{
	"frame-40-422.264", "frame-41-444.264", "frame-41-separate.264",
	"frame-42-10bit.264", "frame-42-12bit.264", "frame-43-fields.264",
	"frame-44-mbaff.264", "frame-45-fmo.264", "frame-46-redundant.264",
	"frame-47-partitions.264", "frame-48-sp-si.264", "frame-49-svc.264",
	"frame-49-mvc.264", "frame-49-mvcd.264", "frame-49-3davc.264",
}

type companionSequence struct {
	profile             uint32
	chroma              uint32
	separate            bool
	lumaDepth           int
	chromaDepth         int
	frameOnly           bool
	mbaff               bool
	gaps                bool
	pocType             uint32
	widthMBs, heightMBs uint32
	spsID               uint32
}

func companionSequenceDefaults(profile uint32) companionSequence {
	return companionSequence{
		profile: profile, chroma: 1, lumaDepth: 8, chromaDepth: 8,
		frameOnly: true, pocType: 2, widthMBs: 4, heightMBs: 4,
	}
}

// GenerateH264CompanionStreams is a byte-for-byte port of the canonical
// Python Annex-B companion corpus.
func GenerateH264CompanionStreams() map[string][]byte {
	chroma422 := companionSequenceDefaults(122)
	chroma422.chroma = 2
	chroma444 := companionSequenceDefaults(244)
	chroma444.chroma = 3
	separate444 := chroma444
	separate444.separate = true
	bit10 := companionSequenceDefaults(110)
	bit10.lumaDepth, bit10.chromaDepth = 10, 10
	bit12 := companionSequenceDefaults(244)
	bit12.chroma, bit12.lumaDepth, bit12.chromaDepth = 3, 12, 12
	fields := companionSequenceDefaults(100)
	fields.frameOnly = false
	mbaff := fields
	mbaff.mbaff = true
	base := companionSequenceDefaults(100)

	fmo := companionAnnexB(
		companionSPS(base), companionPPS(0, false, false, 1, 0),
		companionIDRPicture(base, true, nil, nil, 0, intPtr(8), 0, 0, nil),
		companionIDRPicture(base, true, nil, nil, 1, intPtr(8), 0, 0, nil))
	redundant := companionAnnexB(
		companionSPS(base), companionPPS(0, true, false, 0, 0), companionAUD(0),
		companionIDRPicture(base, true, nil, nil, 0, nil, 0, 0, intPtr(0)),
		companionAUD(0),
		companionIDRPicture(base, false, nil, nil, 0, nil, 1, 0, intPtr(0)),
		companionAUD(0),
		companionIDRPicture(base, false, nil, nil, 0, nil, 1, 0, intPtr(1)))
	partitionNALs := [][]byte{}
	for mb := 0; mb < 16; mb++ {
		partitionNALs = append(partitionNALs, companionPartitionNals(base, mb)...)
	}
	partitionStreamNals := append([][]byte{
		companionSPS(base), companionPPS(0, false, false, 0, 0), companionAUD(0),
		companionIDRPicture(base, true, nil, nil, 0, nil, 0, 0, nil),
		companionAUD(0),
	}, partitionNALs...)
	partitions := companionAnnexB(partitionStreamNals...)
	extended := companionSequenceDefaults(88)
	spSI := companionAnnexB(
		companionSPS(extended), companionPPS(0, false, false, 0, 0), companionAUD(0),
		companionIDRPicture(extended, true, nil, nil, 0, nil, 0, 0, nil),
		companionAUD(0), companionSwitchingSlice(true), companionAUD(0),
		companionSwitchingSlice(false))
	extensionBase := companionAnnexB(
		companionSPS(base), companionPPS(0, false, false, 0, 0),
		companionIDRPicture(base, true, nil, nil, 0, nil, 0, 0, nil))
	return map[string][]byte{
		"frame-40-422.264": companionAnnexB(companionSPS(chroma422),
			companionPPS(0, false, false, 0, 0),
			companionIDRPicture(chroma422, true, nil, nil, 0, nil, 0, 0, nil)),
		"frame-41-444.264": companionAnnexB(companionSPS(chroma444),
			companionPPS(0, false, true, 0, 0),
			companionIDRPicture(chroma444, true, nil, nil, 0, nil, 0, 0, nil)),
		"frame-41-separate.264": companionAnnexB(companionSPS(separate444),
			companionPPS(0, false, true, 0, 0),
			companionIDRPicture(separate444, true, nil, intPtr(0), 0, nil, 0, 0, nil),
			companionIDRPicture(separate444, true, nil, intPtr(1), 0, nil, 0, 0, nil),
			companionIDRPicture(separate444, true, nil, intPtr(2), 0, nil, 0, 0, nil)),
		"frame-42-10bit.264": companionAnnexB(companionSPS(bit10),
			companionPPS(0, false, false, 0, 0),
			companionIDRPicture(bit10, true, nil, nil, 0, nil, 0, 0, nil)),
		"frame-42-12bit.264": companionAnnexB(companionSPS(bit12),
			companionPPS(0, false, false, 0, 0),
			companionIDRPicture(bit12, true, nil, nil, 0, nil, 0, 0, nil)),
		"frame-43-fields.264": companionAnnexB(companionSPS(fields),
			companionPPS(0, false, false, 0, 0),
			companionIDRPicture(fields, true, intPtr(0), nil, 0, nil, 0, 0, nil),
			companionIDRPicture(fields, false, intPtr(1), nil, 0, nil, 1, 0, nil)),
		"frame-44-mbaff.264": companionAnnexB(companionSPS(mbaff),
			companionPPS(0, false, false, 0, 0),
			companionIDRPicture(mbaff, true, nil, nil, 0, nil, 0, 0, nil)),
		"frame-45-fmo.264":        fmo,
		"frame-46-redundant.264":  redundant,
		"frame-47-partitions.264": partitions,
		"frame-48-sp-si.264":      spSI,
		"frame-49-svc.264":        append(append([]byte(nil), extensionBase...), companionExtension("svc")...),
		"frame-49-mvc.264":        append(append([]byte(nil), extensionBase...), companionExtension("mvc")...),
		"frame-49-mvcd.264":       append(append([]byte(nil), extensionBase...), companionExtension("mvcd")...),
		"frame-49-3davc.264":      append(append([]byte(nil), extensionBase...), companionExtension("3davc")...),
	}
}

func intPtr(value int) *int { return &value }

func companionSPS(sequence companionSequence) []byte {
	w := h264BitWriter{}
	w.bits(sequence.profile, 8)
	w.bits(0, 8)
	w.bits(20, 8)
	w.ue(sequence.spsID)
	switch sequence.profile {
	case 44, 83, 86, 100, 110, 118, 122, 128, 134, 135, 138, 139, 144, 244:
		w.ue(sequence.chroma)
		if sequence.chroma == 3 {
			w.bits(boolUint32(sequence.separate), 1)
		}
		w.ue(uint32(sequence.lumaDepth - 8))
		w.ue(uint32(sequence.chromaDepth - 8))
		w.bits(0, 2)
	}
	w.ue(0)
	w.ue(sequence.pocType)
	switch sequence.pocType {
	case 0:
		w.ue(2)
	case 1:
		w.bits(0, 1)
		w.se(0)
		w.se(0)
		w.ue(0)
	}
	w.ue(2)
	w.bits(boolUint32(sequence.gaps), 1)
	w.ue(sequence.widthMBs - 1)
	heightUnits := sequence.heightMBs
	if !sequence.frameOnly {
		heightUnits /= 2
	}
	w.ue(heightUnits - 1)
	w.bits(boolUint32(sequence.frameOnly), 1)
	if !sequence.frameOnly {
		w.bits(boolUint32(sequence.mbaff), 1)
	}
	w.bits(1, 1)
	w.bits(0, 2)
	return w.nal(0x67)
}

func companionPPS(spsID uint32, redundant, transform8x8 bool, groups, mapType uint32) []byte {
	w := h264BitWriter{}
	w.ue(0)
	w.ue(spsID)
	w.bits(0, 2)
	w.ue(groups)
	if groups != 0 {
		w.ue(mapType)
		if mapType == 0 {
			w.ue(7)
			w.ue(7)
		}
	}
	w.ue(0)
	w.ue(0)
	w.bits(0, 1)
	w.bits(0, 2)
	w.se(0)
	w.se(0)
	w.se(0)
	w.bits(1, 1)
	w.bits(0, 1)
	w.bits(boolUint32(redundant), 1)
	if transform8x8 {
		w.bits(1, 1)
		w.bits(0, 1)
		w.se(0)
	}
	return w.nal(0x68)
}

func companionSliceHeader(w *h264BitWriter, firstMB int, sequence companionSequence, idr bool,
	field, colourPlane *int, frameNum, ppsID int, redundant *int) {
	w.ue(uint32(firstMB))
	w.ue(2)
	w.ue(uint32(ppsID))
	if sequence.separate {
		value := 0
		if colourPlane != nil {
			value = *colourPlane
		}
		w.bits(uint32(value), 2)
	}
	w.bits(uint32(frameNum), 4)
	if !sequence.frameOnly {
		w.bits(boolUint32(field != nil), 1)
		if field != nil {
			w.bits(uint32(*field), 1)
		}
	}
	if idr {
		w.ue(0)
	}
	if sequence.pocType == 0 {
		w.bits(0, 6)
	}
	if redundant != nil {
		w.ue(uint32(*redundant))
	}
	w.bits(0, boolIntToBitCount(idr, 2, 1))
	w.se(0)
	w.ue(1)
}

func boolIntToBitCount(value bool, yes, no int) int {
	if value {
		return yes
	}
	return no
}

func companionPCM(w *h264BitWriter, sequence companionSequence, count int, fieldMBAFF bool) {
	for macroblock := 0; macroblock < count; macroblock++ {
		if fieldMBAFF && macroblock%2 == 0 {
			w.bits(uint32((macroblock/2)%2), 1)
		}
		w.ue(25)
		w.align()
		planes := 3
		if sequence.separate {
			planes = 1
		}
		for plane := 0; plane < planes; plane++ {
			samples, depth := 256, sequence.lumaDepth
			if !sequence.separate && plane != 0 {
				samples = map[uint32]int{1: 64, 2: 128, 3: 256}[sequence.chroma]
				depth = sequence.chromaDepth
			}
			value := uint32(macroblock*11+32+plane*47) & ((1 << depth) - 1)
			if depth == 8 {
				for index := 0; index < samples; index++ {
					w.data = append(w.data, byte(value))
				}
				w.bitCount += samples * 8
			} else {
				for index := 0; index < samples; index++ {
					w.bits(value, depth)
				}
			}
		}
	}
}

func companionIDRPicture(sequence companionSequence, idr bool, field, colourPlane *int,
	firstMB int, count *int, frameNum, ppsID int, redundant *int) []byte {
	w := h264BitWriter{}
	companionSliceHeader(&w, firstMB, sequence, idr, field, colourPlane, frameNum, ppsID, redundant)
	macroblocks := int(sequence.widthMBs * sequence.heightMBs)
	if field != nil {
		macroblocks /= 2
	}
	if count != nil {
		macroblocks = *count
	}
	companionPCM(&w, sequence, macroblocks, !sequence.frameOnly && sequence.mbaff)
	header := byte(0x65)
	if !idr {
		header = 0x21
	}
	return w.nal(header)
}

func companionAnnexB(nals ...[]byte) []byte {
	var stream []byte
	for _, nal := range nals {
		stream = append(stream, 0, 0, 0, 1)
		stream = append(stream, nal...)
	}
	return stream
}

func companionAUD(primaryPictureType int) []byte {
	w := h264BitWriter{}
	w.bits(uint32(primaryPictureType<<5), 8)
	return w.nal(0x09)
}

func companionPartitionNals(sequence companionSequence, mb int) [][]byte {
	_ = sequence
	w := h264BitWriter{}
	w.ue(uint32(mb))
	w.ue(2)
	w.ue(0)
	w.bits(1, 4)
	w.bits(0, 1)
	w.se(0)
	w.ue(1)
	w.ue(uint32(mb))
	w.ue(25)
	partitionA := w.nal(0x42)

	w = h264BitWriter{}
	w.ue(uint32(mb))
	w.align()
	for _, value := range []byte{byte(112 + mb), byte(144 + mb), byte(160 + mb)} {
		count := 64
		if value == byte(112+mb) {
			count = 256
		}
		for index := 0; index < count; index++ {
			w.data = append(w.data, value)
		}
		w.bitCount += count * 8
	}
	partitionB := w.nal(0x43)

	w = h264BitWriter{}
	w.ue(uint32(mb))
	partitionC := w.nal(0x44)
	return [][]byte{partitionA, partitionB, partitionC}
}

func companionSwitchingSlice(sp bool) []byte {
	w := h264BitWriter{}
	w.ue(0)
	if sp {
		w.ue(3)
	} else {
		w.ue(4)
	}
	w.ue(0)
	if sp {
		w.bits(1, 4)
		w.bits(0, 2)
	} else {
		w.bits(2, 4)
	}
	w.bits(0, 1)
	w.se(0)
	if sp {
		w.bits(0, 1)
	}
	w.se(0)
	w.ue(1)
	if sp {
		w.ue(16)
	} else {
		for mb := 0; mb < 16; mb++ {
			_ = mb
			w.ue(26)
			w.align()
			for index := 0; index < 384; index++ {
				w.data = append(w.data, 128)
			}
			w.bitCount += 384 * 8
		}
	}
	return w.nal(0x21)
}

func companionExtension(family string) []byte {
	var data []byte
	switch family {
	case "svc":
		data = append(data, 0, 0, 0, 1, 0x6e, 0x80)
		return append(data, 0, 0, 0, 1, 0x74, 0x80, 0, 0, 0x80)
	case "mvc":
		data = []byte{0, 0, 0, 1, 0x74, 0, 0, 0, 0x80}
	case "mvcd":
		data = []byte{0, 0, 0, 1, 0x75, 0x80, 0, 0, 0x80}
	default:
		data = []byte{0, 0, 0, 1, 0x75, 0xc0, 0, 0, 0x80}
	}
	return data
}
