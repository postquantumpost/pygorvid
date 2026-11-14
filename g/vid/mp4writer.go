package vid

import "encoding/binary"

func mp4U32(values ...uint32) []byte {
	result := make([]byte, len(values)*4)
	for i, value := range values {
		binary.BigEndian.PutUint32(result[i*4:], value)
	}
	return result
}

func mp4U16(values ...uint16) []byte {
	result := make([]byte, len(values)*2)
	for i, value := range values {
		binary.BigEndian.PutUint16(result[i*2:], value)
	}
	return result
}

func mp4Box(kind string, parts ...[]byte) []byte {
	length := 8
	for _, part := range parts {
		length += len(part)
	}
	result := make([]byte, 8, length)
	binary.BigEndian.PutUint32(result, uint32(length))
	copy(result[4:], kind)
	for _, part := range parts {
		result = append(result, part...)
	}
	return result
}

func mp4FullBox(kind string, flags uint32, parts ...[]byte) []byte {
	return mp4Box(kind, append([][]byte{mp4U32(flags)}, parts...)...)
}

func mp4Matrix() []byte {
	return mp4U32(0x10000, 0, 0, 0, 0x10000, 0, 0, 0, 0x40000000)
}

func mp4Track(id uint32, entry, sizes []byte, count, timescale, offset uint32, video bool) []byte {
	var dimension uint32
	var volume uint16 = 0x100
	handler, name := "soun", "Audio\x00"
	header := mp4FullBox("smhd", 0, make([]byte, 4))
	if video {
		dimension, volume = 64<<16, 0
		handler, name = "vide", "Video\x00"
		header = mp4FullBox("vmhd", 1, make([]byte, 8))
	}
	tkhd := mp4FullBox("tkhd", 7, mp4U32(0, 0, id, 0, 1000), make([]byte, 8),
		mp4U16(0, 0, volume, 0), mp4Matrix(), mp4U32(dimension, dimension))
	mdhd := mp4FullBox("mdhd", 0, mp4U32(0, 0, timescale, count), mp4U16(0x55c4, 0))
	hdlr := mp4FullBox("hdlr", 0, mp4U32(0), []byte(handler), make([]byte, 12), []byte(name))
	dinf := mp4Box("dinf", mp4FullBox("dref", 0, mp4U32(1), mp4FullBox("url ", 1)))
	stbl := mp4Box("stbl", mp4FullBox("stsd", 0, mp4U32(1), entry),
		mp4FullBox("stts", 0, mp4U32(1, count, 1)),
		mp4FullBox("stsc", 0, mp4U32(1, 1, count, 1)), sizes,
		mp4FullBox("stco", 0, mp4U32(1, offset)))
	return mp4Box("trak", tkhd, mp4Box("mdia", mdhd, hdlr, mp4Box("minf", header, dinf, stbl)))
}

// Mp4Writer initially muxes one second of 64x64 H.264 and stereo PCM silence.
type Mp4Writer struct{}

func NewMp4Writer() Mp4Writer { return Mp4Writer{} }

func (w Mp4Writer) Encode(encoder H264Encoder) []byte {
	var video []byte
	sampleSizes := []uint32{0, 25}
	for index := uint16(0); index < 25; index++ {
		sample := encoder.EncodeGreyFrame(index)
		video = append(video, sample...)
		sampleSizes = append(sampleSizes, uint32(len(sample)))
	}
	audio := make([]byte, 48000*2*2)
	sps, pps := encoder.SequenceParameterSet(), encoder.PictureParameterSet()
	avcc := mp4Box("avcC", []byte{1, sps[1], sps[2], sps[3], 0xff, 0xe1},
		mp4U16(uint16(len(sps))), sps, []byte{1}, mp4U16(uint16(len(pps))), pps)
	visual := mp4Box("avc1", make([]byte, 6), mp4U16(1), make([]byte, 16), mp4U16(64, 64),
		mp4U32(0x480000, 0x480000, 0), mp4U16(1), make([]byte, 32), mp4U16(24, 0xffff), avcc)
	sound := mp4Box("sowt", make([]byte, 6), mp4U16(1), make([]byte, 8),
		mp4U16(2, 16, 0, 0), mp4U32(48000<<16))
	ftyp := mp4Box("ftyp", []byte("isom"), mp4U32(0x200), []byte("isomiso2avc1qt  "))
	offset := uint32(len(ftyp) + 8)
	videoTrack := mp4Track(1, visual, mp4FullBox("stsz", 0, mp4U32(sampleSizes...)), 25, 25, offset, true)
	audioTrack := mp4Track(2, sound, mp4FullBox("stsz", 0, mp4U32(4, 48000)),
		48000, 48000, offset+uint32(len(video)), false)
	mvhd := mp4FullBox("mvhd", 0, mp4U32(0, 0, 1000, 1000, 0x10000), mp4U16(0x100, 0),
		make([]byte, 8), mp4Matrix(), make([]byte, 24), mp4U32(3))
	result := append(ftyp, mp4Box("mdat", video, audio)...)
	return append(result, mp4Box("moov", mvhd, videoTrack, audioTrack)...)
}
