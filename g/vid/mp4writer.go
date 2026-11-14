package vid

import (
	"encoding/binary"
	"fmt"
	"strconv"
	"unicode/utf16"
	"unicode/utf8"
)

type Mp4Metadata struct {
	Title, Artist, AlbumArtist, Genre, Track, Disc, Date        string
	Composer, Comment, Description, Copyright                   string
	Make, Model, Firmware, Location, Altitude, Heading, Encoder string
}

type mp4MetadataEntry struct{ key, value string }

func (metadata *Mp4Metadata) entries() []mp4MetadataEntry {
	return []mp4MetadataEntry{
		{"title", metadata.Title}, {"artist", metadata.Artist},
		{"album_artist", metadata.AlbumArtist}, {"genre", metadata.Genre},
		{"track", metadata.Track}, {"disc", metadata.Disc}, {"date", metadata.Date},
		{"composer", metadata.Composer}, {"comment", metadata.Comment},
		{"description", metadata.Description}, {"copyright", metadata.Copyright},
		{"make", metadata.Make}, {"model", metadata.Model}, {"firmware", metadata.Firmware},
		{"location", metadata.Location}, {"altitude", metadata.Altitude},
		{"heading", metadata.Heading}, {"encoder", metadata.Encoder},
	}
}

func mp4CompatibilityMetadataBox(metadata *Mp4Metadata) ([]byte, error) {
	if metadata == nil {
		return nil, nil
	}
	entries := metadata.entries()
	textTags := []struct{ tag, value string }{
		{"\xa9nam", metadata.Title}, {"\xa9ART", metadata.Artist},
		{"aART", metadata.AlbumArtist}, {"\xa9gen", metadata.Genre},
		{"\xa9day", metadata.Date}, {"\xa9wrt", metadata.Composer},
		{"\xa9com", metadata.Composer}, {"\xa9cmt", metadata.Comment},
		{"desc", metadata.Description}, {"cprt", metadata.Copyright},
		{"\xa9cpy", metadata.Copyright}, {"\xa9too", metadata.Encoder},
	}
	length := uint64(61)
	count := 0
	for _, entry := range entries {
		if entry.value != "" {
			length += 64 + uint64(len(entry.key)) + uint64(len(entry.value))
			count++
		}
	}
	if count == 0 {
		return nil, nil
	}
	for _, tag := range textTags {
		if tag.value != "" {
			length += 24 + uint64(len(tag.value))
		}
	}
	// Reserve both numeric items even when a string has no binary representation.
	length += 32 + 30
	if length > uint64(^uint32(0)) {
		return nil, fmt.Errorf("MP4 compatibility metadata exceeds the 32-bit box size limit")
	}
	var items []byte
	for _, tag := range textTags {
		if tag.value != "" {
			items = append(items, mp4Box(tag.tag, mp4FullBox("data", 1, mp4U32(0), []byte(tag.value)))...)
		}
	}
	for _, entry := range []struct {
		tag, value string
		trailing   bool
	}{
		{"trkn", metadata.Track, true}, {"disk", metadata.Disc, false},
	} {
		digits := len(entry.value) > 0 && len(entry.value) <= 5
		for _, ch := range entry.value {
			digits = digits && ch >= '0' && ch <= '9'
		}
		if !digits {
			continue
		}
		number, err := strconv.ParseUint(entry.value, 10, 16)
		if err != nil {
			continue
		} // Raw string retained in mdta and free-form tags.
		data := mp4U16(0, uint16(number), 0)
		if entry.trailing {
			data = append(data, 0, 0)
		}
		items = append(items, mp4Box(entry.tag, mp4FullBox("data", 0, mp4U32(0), data))...)
	}
	for _, entry := range entries {
		if entry.value != "" {
			items = append(items, mp4Box("----",
				mp4FullBox("mean", 0, []byte("com.apple.iTunes")),
				mp4FullBox("name", 0, []byte(entry.key)),
				mp4FullBox("data", 1, mp4U32(0), []byte(entry.value)))...)
		}
	}
	handler := mp4FullBox("hdlr", 0, mp4U32(0), []byte("mdirappl"), make([]byte, 8), []byte{0})
	windows, err := mp4WindowsMetadataBox(metadata)
	if err != nil {
		return nil, err
	}
	if 61+uint64(len(items))+uint64(len(windows)) > uint64(^uint32(0)) {
		return nil, fmt.Errorf("MP4 user metadata exceeds the 32-bit box size limit")
	}
	return mp4Box("udta", mp4FullBox("meta", 0, handler, mp4Box("ilst", items)), windows), nil
}

func mp4WindowsMetadataBox(metadata *Mp4Metadata) ([]byte, error) {
	if metadata == nil {
		return nil, nil
	}
	attributes := []mp4MetadataEntry{
		{"Title", metadata.Title}, {"Author", metadata.Artist},
		{"WM/AlbumArtist", metadata.AlbumArtist}, {"WM/Genre", metadata.Genre},
		{"WM/TrackNumber", metadata.Track}, {"WM/PartOfSet", metadata.Disc},
		{"WM/OriginalReleaseTime", metadata.Date}, {"WM/Composer", metadata.Composer},
		{"Description", metadata.Comment}, {"WM/SubTitleDescription", metadata.Description},
		{"Copyright", metadata.Copyright}, {"CameraManufacturer", metadata.Make},
		{"CameraModel", metadata.Model}, {"WM/EncodedBy", metadata.Encoder},
	}
	if len(metadata.Date) >= 4 {
		year := metadata.Date[:4]
		digits := true
		for _, ch := range year {
			digits = digits && ch >= '0' && ch <= '9'
		}
		if digits && (len(metadata.Date) == 4 || metadata.Date[4] == '-') {
			attributes = append(attributes, mp4MetadataEntry{"WM/Year", year})
		}
	}
	for _, entry := range metadata.entries() {
		attributes = append(attributes, mp4MetadataEntry{"pygorvid_" + entry.key, entry.value})
	}
	length := uint64(8)
	for _, entry := range attributes {
		if !utf8.ValidString(entry.value) {
			return nil, fmt.Errorf("MP4 Xtra attribute %s is not valid UTF-8", entry.key)
		}
		if entry.value == "" {
			continue
		}
		units := uint64(1) // UTF-16 terminating NUL.
		for _, ch := range entry.value {
			units++
			if ch > 0xffff {
				units++
			}
		}
		length += 18 + uint64(len(entry.key)) + units*2
	}
	if length == 8 {
		return nil, nil
	}
	if length > uint64(^uint32(0)) {
		return nil, fmt.Errorf("MP4 Xtra metadata exceeds the 32-bit box size limit")
	}
	var records []byte
	for _, entry := range attributes {
		if entry.value == "" {
			continue
		}
		units := utf16.Encode([]rune(entry.value))
		data := make([]byte, (len(units)+1)*2)
		for index, unit := range units {
			binary.LittleEndian.PutUint16(data[index*2:], unit)
		}
		valueSize := uint32(len(data) + 6)
		record := mp4U32(uint32(12+len(entry.key))+valueSize, uint32(len(entry.key)))
		record = append(record, []byte(entry.key)...)
		record = append(record, mp4U32(1, valueSize)...)
		record = append(record, mp4U16(8)...)
		records = append(records, append(record, data...)...)
	}
	return mp4Box("Xtra", records), nil
}

func mp4MetadataBox(metadata *Mp4Metadata) ([]byte, error) {
	if metadata == nil {
		return nil, nil
	}
	entries := metadata.entries()
	// Account for meta, hdlr, keys and ilst headers before allocating strings.
	length := uint64(69)
	count := uint32(0)
	for _, entry := range entries {
		if !utf8.ValidString(entry.value) {
			return nil, fmt.Errorf("MP4 metadata %s is not valid UTF-8", entry.key)
		}
		if entry.value != "" {
			length += 8 + uint64(len(entry.key)) + 24 + uint64(len(entry.value))
			count++
		}
	}
	if count == 0 {
		return nil, nil
	}
	if length > uint64(^uint32(0)) {
		return nil, fmt.Errorf("MP4 metadata exceeds the 32-bit box size limit")
	}
	var keys, items []byte
	index := uint32(0)
	for _, entry := range entries {
		if entry.value == "" {
			continue
		}
		index++
		keys = append(keys, mp4Box("mdta", []byte(entry.key))...)
		items = append(items, mp4Box(string(mp4U32(index)),
			mp4FullBox("data", 1, mp4U32(0), []byte(entry.value)))...)
	}
	handler := mp4FullBox("hdlr", 0, mp4U32(0), []byte("mdta"), make([]byte, 12), []byte{0})
	return mp4FullBox("meta", 0, handler,
		mp4FullBox("keys", 0, mp4U32(count), keys), mp4Box("ilst", items)), nil
}

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
	payload, err := w.EncodeWithMetadata(encoder, nil)
	if err != nil {
		panic(err)
	} // The fixed metadata-free file fits 32-bit boxes.
	return payload
}

func (w Mp4Writer) EncodeWithMetadata(encoder H264Encoder, metadata *Mp4Metadata) ([]byte, error) {
	metadataBox, err := mp4MetadataBox(metadata)
	if err != nil {
		return nil, err
	}
	compatibilityBox, err := mp4CompatibilityMetadataBox(metadata)
	if err != nil {
		return nil, err
	}
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
	if uint64(8+len(mvhd)+len(videoTrack)+len(audioTrack))+uint64(len(metadataBox))+uint64(len(compatibilityBox)) > uint64(^uint32(0)) {
		return nil, fmt.Errorf("MP4 movie metadata exceeds the 32-bit box size limit")
	}
	return append(result, mp4Box("moov", mvhd, videoTrack, audioTrack, metadataBox, compatibilityBox)...), nil
}
