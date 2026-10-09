package vid

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// Reader for the mp4 box structure. Each box type is a named type.
// To read more of the format, add a box type and register it in boxRegistry.
// Unregistered box types are skipped without being parsed.

type minfBox struct{ containerBox }
type stblBox struct{ containerBox }

type tkhdBox struct {
	boxBase
	width  uint32
	height uint32
}

func (b *tkhdBox) read(r io.ReadSeeker) error {
	payloadSize := b.h.end - b.h.payloadStart()
	if payloadSize < 84 {
		return errors.New("tkhd box too short")
	}
	if _, err := r.Seek(b.h.payloadStart(), io.SeekStart); err != nil {
		return err
	}
	var version [1]byte
	if _, err := io.ReadFull(r, version[:]); err != nil {
		return err
	}
	widthOffset := int64(76)
	if version[0] == 1 {
		widthOffset = 88
	}
	if payloadSize < widthOffset+8 {
		return errors.New("tkhd box too short")
	}
	if _, err := r.Seek(b.h.payloadStart()+widthOffset, io.SeekStart); err != nil {
		return err
	}
	var dimensions [8]byte
	if _, err := io.ReadFull(r, dimensions[:]); err != nil {
		return err
	}
	b.width = binary.BigEndian.Uint32(dimensions[:4]) >> 16
	b.height = binary.BigEndian.Uint32(dimensions[4:]) >> 16
	return nil
}

type mdhdBox struct {
	boxBase
	timescale uint32
}

func (b *mdhdBox) read(r io.ReadSeeker) error {
	payloadSize := b.h.end - b.h.payloadStart()
	if payloadSize < 20 {
		return errors.New("mdhd box too short")
	}
	if _, err := r.Seek(b.h.payloadStart(), io.SeekStart); err != nil {
		return err
	}
	var version [1]byte
	if _, err := io.ReadFull(r, version[:]); err != nil {
		return err
	}
	timescaleOffset := int64(12)
	if version[0] == 1 {
		timescaleOffset = 20
	}
	if payloadSize < timescaleOffset+4 {
		return errors.New("mdhd box too short")
	}
	if _, err := r.Seek(b.h.payloadStart()+timescaleOffset, io.SeekStart); err != nil {
		return err
	}
	var timescale [4]byte
	if _, err := io.ReadFull(r, timescale[:]); err != nil {
		return err
	}
	b.timescale = binary.BigEndian.Uint32(timescale[:])
	return nil
}

type sttsEntry struct {
	sampleCount uint32
	sampleDelta uint32
}

type sttsBox struct {
	boxBase
	entries []sttsEntry
}

func (b *sttsBox) read(r io.ReadSeeker) error {
	if b.h.end-b.h.payloadStart() < 8 {
		return errors.New("stts box too short")
	}
	if _, err := r.Seek(b.h.payloadStart()+4, io.SeekStart); err != nil {
		return err
	}
	var countBytes [4]byte
	if _, err := io.ReadFull(r, countBytes[:]); err != nil {
		return err
	}
	count := binary.BigEndian.Uint32(countBytes[:])
	if int64(count)*8 > b.h.end-(b.h.payloadStart()+8) {
		return errors.New("stts entries exceed box size")
	}
	b.entries = make([]sttsEntry, count)
	var entryBytes [8]byte
	for i := range b.entries {
		if _, err := io.ReadFull(r, entryBytes[:]); err != nil {
			return err
		}
		b.entries[i] = sttsEntry{
			sampleCount: binary.BigEndian.Uint32(entryBytes[:4]),
			sampleDelta: binary.BigEndian.Uint32(entryBytes[4:]),
		}
	}
	return nil
}

type cttsEntry struct {
	sampleCount  uint32
	sampleOffset int64
}

type cttsBox struct {
	boxBase
	version byte
	entries []cttsEntry
}

func (b *cttsBox) read(r io.ReadSeeker) error {
	if b.h.end-b.h.payloadStart() < 8 {
		return errors.New("ctts box too short")
	}
	if _, err := r.Seek(b.h.payloadStart(), io.SeekStart); err != nil {
		return err
	}
	var versionFlags [4]byte
	if _, err := io.ReadFull(r, versionFlags[:]); err != nil {
		return err
	}
	b.version = versionFlags[0]
	if b.version > 1 {
		return fmt.Errorf("unsupported ctts version %d", b.version)
	}
	var countBytes [4]byte
	if _, err := io.ReadFull(r, countBytes[:]); err != nil {
		return err
	}
	count := binary.BigEndian.Uint32(countBytes[:])
	if int64(count)*8 > b.h.end-(b.h.payloadStart()+8) {
		return errors.New("ctts entries exceed box size")
	}
	b.entries = make([]cttsEntry, count)
	var entryBytes [8]byte
	for i := range b.entries {
		if _, err := io.ReadFull(r, entryBytes[:]); err != nil {
			return err
		}
		offset := int64(binary.BigEndian.Uint32(entryBytes[4:]))
		if b.version == 1 {
			offset = int64(int32(binary.BigEndian.Uint32(entryBytes[4:])))
		}
		b.entries[i] = cttsEntry{
			sampleCount:  binary.BigEndian.Uint32(entryBytes[:4]),
			sampleOffset: offset,
		}
	}
	return nil
}

type stssBox struct {
	boxBase
	sampleNumbers []uint32
}

func (b *stssBox) read(r io.ReadSeeker) error {
	if b.h.end-b.h.payloadStart() < 8 {
		return errors.New("stss box too short")
	}
	if _, err := r.Seek(b.h.payloadStart()+4, io.SeekStart); err != nil {
		return err
	}
	var countBytes [4]byte
	if _, err := io.ReadFull(r, countBytes[:]); err != nil {
		return err
	}
	count := binary.BigEndian.Uint32(countBytes[:])
	if int64(count)*4 > b.h.end-(b.h.payloadStart()+8) {
		return errors.New("stss entries exceed box size")
	}
	b.sampleNumbers = make([]uint32, count)
	var sampleBytes [4]byte
	for i := range b.sampleNumbers {
		if _, err := io.ReadFull(r, sampleBytes[:]); err != nil {
			return err
		}
		sampleNumber := binary.BigEndian.Uint32(sampleBytes[:])
		if sampleNumber == 0 || i > 0 && sampleNumber <= b.sampleNumbers[i-1] {
			return errors.New("stss sample numbers must be positive and increasing")
		}
		b.sampleNumbers[i] = sampleNumber
	}
	return nil
}

type mdatBox struct {
	boxBase
	dataStart int64
	dataEnd   int64
}

func (b *mdatBox) read(io.ReadSeeker) error {
	b.dataStart = b.h.payloadStart()
	b.dataEnd = b.h.end
	return nil
}

type stsdBox struct {
	boxBase
	sampleRate uint32
	channels   uint16
	entries    []box
}

func (b *stsdBox) read(r io.ReadSeeker) error {
	if b.h.end-b.h.payloadStart() < 8 {
		return errors.New("stsd box too short")
	}
	if _, err := r.Seek(b.h.payloadStart()+4, io.SeekStart); err != nil {
		return err
	}
	var countBytes [4]byte
	if _, err := io.ReadFull(r, countBytes[:]); err != nil {
		return err
	}
	count := binary.BigEndian.Uint32(countBytes[:])
	for range count {
		entry, ok, err := readHeader(r, b.h.end)
		if err != nil {
			return err
		}
		if !ok {
			return errors.New("stsd entry missing")
		}
		entryBox := newBox(entry)
		if entry.typ == "mp4a" {
			if entry.end-entry.payloadStart() >= 28 {
				if _, err := r.Seek(entry.payloadStart()+16, io.SeekStart); err != nil {
					return err
				}
				var channels [2]byte
				if _, err := io.ReadFull(r, channels[:]); err != nil {
					return err
				}
				b.channels = binary.BigEndian.Uint16(channels[:])
				if _, err := r.Seek(entry.payloadStart()+24, io.SeekStart); err != nil {
					return err
				}
				var sampleRate [4]byte
				if _, err := io.ReadFull(r, sampleRate[:]); err != nil {
					return err
				}
				b.sampleRate = binary.BigEndian.Uint32(sampleRate[:]) >> 16
			}
		}
		if err := entryBox.read(r); err != nil {
			return err
		}
		b.entries = append(b.entries, entryBox)
		if _, err := r.Seek(entry.end, io.SeekStart); err != nil {
			return err
		}
	}
	return nil
}

func (b *stsdBox) kids() []box { return b.entries }

type avc1Box struct {
	boxBase
	width    uint16
	height   uint16
	children []box
}

func (b *avc1Box) read(r io.ReadSeeker) error {
	if b.h.end-b.h.payloadStart() < 78 {
		return errors.New("avc1 sample entry too short")
	}
	if _, err := r.Seek(b.h.payloadStart()+24, io.SeekStart); err != nil {
		return err
	}
	var dimensions [4]byte
	if _, err := io.ReadFull(r, dimensions[:]); err != nil {
		return err
	}
	b.width = binary.BigEndian.Uint16(dimensions[:2])
	b.height = binary.BigEndian.Uint16(dimensions[2:])
	var err error
	b.children, err = readBoxes(r, b.h.payloadStart()+78, b.h.end)
	return err
}

func (b *avc1Box) kids() []box { return b.children }

type avcCBox struct {
	boxBase
	profile       byte
	profileCompat byte
	level         byte
	nalLengthSize uint8
	sequenceSets  [][]byte
	pictureSets   [][]byte
}

func (b *avcCBox) read(r io.ReadSeeker) error {
	payloadSize := b.h.end - b.h.payloadStart()
	if payloadSize < 7 {
		return errors.New("avcC box too short")
	}
	data := make([]byte, payloadSize)
	if _, err := r.Seek(b.h.payloadStart(), io.SeekStart); err != nil {
		return err
	}
	if _, err := io.ReadFull(r, data); err != nil {
		return err
	}
	if data[0] != 1 {
		return fmt.Errorf("unsupported avcC configuration version %d", data[0])
	}
	if data[4]&0xfc != 0xfc || data[5]&0xe0 != 0xe0 {
		return errors.New("avcC reserved bits are invalid")
	}
	if data[4]&0x03 != 0x03 {
		return errors.New("only four-byte AVC NAL length prefixes are supported")
	}
	b.profile = data[1]
	b.profileCompat = data[2]
	b.level = data[3]
	b.nalLengthSize = 4
	offset := 6
	spsCount := int(data[5] & 0x1f)
	if spsCount == 0 {
		return errors.New("avcC contains no sequence parameter sets")
	}
	var err error
	b.sequenceSets, offset, err = readAVCConfigNALs(data, offset, spsCount, 7)
	if err != nil {
		return err
	}
	if offset >= len(data) {
		return errors.New("avcC picture-set count missing")
	}
	ppsCount := int(data[offset])
	offset++
	if ppsCount == 0 {
		return errors.New("avcC contains no picture parameter sets")
	}
	b.pictureSets, offset, err = readAVCConfigNALs(data, offset, ppsCount, 8)
	if err != nil {
		return err
	}
	if offset < len(data) {
		if !avcProfileHasConfigExtension(b.profile) || len(data)-offset < 4 {
			return errors.New("avcC has invalid trailing data")
		}
		if data[offset]&0xfc != 0xfc || data[offset+1]&0xf8 != 0xf8 || data[offset+2]&0xf8 != 0xf8 {
			return errors.New("avcC extension reserved bits are invalid")
		}
		extensionCount := int(data[offset+3])
		_, offset, err = readAVCConfigNALs(data, offset+4, extensionCount, 13)
		if err != nil {
			return err
		}
	}
	if offset != len(data) {
		return errors.New("avcC has invalid trailing data")
	}
	return nil
}

func avcProfileHasConfigExtension(profile byte) bool {
	switch profile {
	case 44, 83, 86, 100, 110, 118, 122, 128, 134, 135, 138, 139, 144, 244:
		return true
	default:
		return false
	}
}

func readAVCConfigNALs(data []byte, offset, count, nalType int) ([][]byte, int, error) {
	nals := make([][]byte, 0, count)
	for range count {
		if offset+2 > len(data) {
			return nil, offset, errors.New("avcC NAL length truncated")
		}
		size := int(binary.BigEndian.Uint16(data[offset : offset+2]))
		offset += 2
		if size == 0 || size > len(data)-offset {
			return nil, offset, errors.New("avcC NAL exceeds box size")
		}
		if data[offset]&0x80 != 0 || int(data[offset]&0x1f) != nalType {
			return nil, offset, fmt.Errorf("avcC parameter-set NAL has unexpected type; want %d", nalType)
		}
		nals = append(nals, append([]byte(nil), data[offset:offset+size]...))
		offset += size
	}
	return nals, offset, nil
}

type stszBox struct {
	boxBase
	constantSize uint32
	sampleCount  uint32
	sizes        []uint32
}

func (b *stszBox) read(r io.ReadSeeker) error {
	if b.h.end-b.h.payloadStart() < 12 {
		return errors.New("stsz box too short")
	}
	if _, err := r.Seek(b.h.payloadStart()+4, io.SeekStart); err != nil {
		return err
	}
	var fields [8]byte
	if _, err := io.ReadFull(r, fields[:]); err != nil {
		return err
	}
	b.constantSize = binary.BigEndian.Uint32(fields[:4])
	b.sampleCount = binary.BigEndian.Uint32(fields[4:])
	if b.constantSize != 0 {
		return nil
	}
	if int64(b.sampleCount)*4 > b.h.end-(b.h.payloadStart()+12) {
		return errors.New("stsz entries exceed box size")
	}
	b.sizes = make([]uint32, b.sampleCount)
	var sizeBytes [4]byte
	for i := range b.sizes {
		if _, err := io.ReadFull(r, sizeBytes[:]); err != nil {
			return err
		}
		b.sizes[i] = binary.BigEndian.Uint32(sizeBytes[:])
	}
	return nil
}

type stscEntry struct {
	firstChunk             uint32
	samplesPerChunk        uint32
	sampleDescriptionIndex uint32
}

type stscBox struct {
	boxBase
	entries []stscEntry
}

func (b *stscBox) read(r io.ReadSeeker) error {
	if b.h.end-b.h.payloadStart() < 8 {
		return errors.New("stsc box too short")
	}
	if _, err := r.Seek(b.h.payloadStart()+4, io.SeekStart); err != nil {
		return err
	}
	var countBytes [4]byte
	if _, err := io.ReadFull(r, countBytes[:]); err != nil {
		return err
	}
	count := binary.BigEndian.Uint32(countBytes[:])
	if int64(count)*12 > b.h.end-(b.h.payloadStart()+8) {
		return errors.New("stsc entries exceed box size")
	}
	b.entries = make([]stscEntry, count)
	var entryBytes [12]byte
	for i := range b.entries {
		if _, err := io.ReadFull(r, entryBytes[:]); err != nil {
			return err
		}
		entry := stscEntry{
			firstChunk:             binary.BigEndian.Uint32(entryBytes[:4]),
			samplesPerChunk:        binary.BigEndian.Uint32(entryBytes[4:8]),
			sampleDescriptionIndex: binary.BigEndian.Uint32(entryBytes[8:]),
		}
		if entry.firstChunk == 0 || entry.samplesPerChunk == 0 || entry.sampleDescriptionIndex == 0 {
			return errors.New("invalid stsc entry values")
		}
		if i == 0 && entry.firstChunk != 1 || i > 0 && entry.firstChunk <= b.entries[i-1].firstChunk {
			return errors.New("stsc first_chunk values must start at 1 and increase")
		}
		b.entries[i] = entry
	}
	return nil
}

type stcoBox struct {
	boxBase
	offsets []uint64
}

type co64Box struct {
	boxBase
	offsets []uint64
}

func readChunkOffsets(r io.ReadSeeker, h boxHeader, entrySize int64) ([]uint64, error) {
	if h.end-h.payloadStart() < 8 {
		return nil, errors.New("chunk offset box too short")
	}
	if _, err := r.Seek(h.payloadStart()+4, io.SeekStart); err != nil {
		return nil, err
	}
	var countBytes [4]byte
	if _, err := io.ReadFull(r, countBytes[:]); err != nil {
		return nil, err
	}
	count := binary.BigEndian.Uint32(countBytes[:])
	if int64(count)*entrySize > h.end-(h.payloadStart()+8) {
		return nil, errors.New("chunk offsets exceed box size")
	}
	offsets := make([]uint64, count)
	for i := range offsets {
		if entrySize == 4 {
			var bytes [4]byte
			if _, err := io.ReadFull(r, bytes[:]); err != nil {
				return nil, err
			}
			offsets[i] = uint64(binary.BigEndian.Uint32(bytes[:]))
		} else {
			var bytes [8]byte
			if _, err := io.ReadFull(r, bytes[:]); err != nil {
				return nil, err
			}
			offsets[i] = binary.BigEndian.Uint64(bytes[:])
		}
	}
	return offsets, nil
}

func (b *stcoBox) read(r io.ReadSeeker) error {
	var err error
	b.offsets, err = readChunkOffsets(r, b.h, 4)
	return err
}

func (b *co64Box) read(r io.ReadSeeker) error {
	var err error
	b.offsets, err = readChunkOffsets(r, b.h, 8)
	return err
}

type boxHeader struct {
	typ        string
	start      int64
	headerSize int64
	end        int64
}

func (h boxHeader) payloadStart() int64 { return h.start + h.headerSize }

// readHeader reads a box header at the current position; ok is false if no room is left before limit.
func readHeader(r io.ReadSeeker, limit int64) (h boxHeader, ok bool, err error) {
	start, err := r.Seek(0, io.SeekCurrent)
	if err != nil {
		return
	}
	if start+8 > limit {
		return h, false, nil
	}
	var b [8]byte
	if _, err = io.ReadFull(r, b[:]); err != nil {
		return
	}
	size := int64(binary.BigEndian.Uint32(b[:4]))
	hs := int64(8)
	switch size {
	case 0:
		size = limit - start
	case 1:
		var l [8]byte
		if _, err = io.ReadFull(r, l[:]); err != nil {
			return
		}
		size = int64(binary.BigEndian.Uint64(l[:]))
		hs = 16
	}
	if size < hs || start+size > limit {
		err = fmt.Errorf("invalid box size %d at offset %d", size, start)
		return
	}
	return boxHeader{string(b[4:]), start, hs, start + size}, true, nil
}

type box interface {
	header() boxHeader
	// read parses the payload. No position is guaranteed; seek as needed.
	read(r io.ReadSeeker) error
}

type boxBase struct{ h boxHeader }

func (b *boxBase) header() boxHeader        { return b.h }
func (b *boxBase) read(io.ReadSeeker) error { return nil }

type unknownBox struct{ boxBase }

type parent interface{ kids() []box }

type containerBox struct {
	boxBase
	children []box
}

func (c *containerBox) read(r io.ReadSeeker) (err error) {
	c.children, err = readBoxes(r, c.h.payloadStart(), c.h.end)
	return
}

func (c *containerBox) kids() []box { return c.children }

type moovBox struct{ containerBox }
type trakBox struct{ containerBox }
type mdiaBox struct{ containerBox }

type hdlrBox struct {
	boxBase
	handlerType string
}

func (b *hdlrBox) read(r io.ReadSeeker) error {
	// payload: version/flags (4), pre_defined (4), handler_type (4), ...
	if b.h.end-b.h.payloadStart() < 12 {
		return errors.New("hdlr box too short")
	}
	if _, err := r.Seek(b.h.payloadStart()+8, io.SeekStart); err != nil {
		return err
	}
	var t [4]byte
	if _, err := io.ReadFull(r, t[:]); err != nil {
		return err
	}
	b.handlerType = string(t[:])
	return nil
}

var boxRegistry = map[string]func(boxHeader) box{
	"minf": func(h boxHeader) box { return &minfBox{containerBox{boxBase: boxBase{h}}} },
	"stbl": func(h boxHeader) box { return &stblBox{containerBox{boxBase: boxBase{h}}} },
	"tkhd": func(h boxHeader) box { return &tkhdBox{boxBase: boxBase{h}} },
	"mdhd": func(h boxHeader) box { return &mdhdBox{boxBase: boxBase{h}} },
	"stts": func(h boxHeader) box { return &sttsBox{boxBase: boxBase{h}} },
	"ctts": func(h boxHeader) box { return &cttsBox{boxBase: boxBase{h}} },
	"stss": func(h boxHeader) box { return &stssBox{boxBase: boxBase{h}} },
	"mdat": func(h boxHeader) box { return &mdatBox{boxBase: boxBase{h}} },
	"stsd": func(h boxHeader) box { return &stsdBox{boxBase: boxBase{h}} },
	"avc1": func(h boxHeader) box { return &avc1Box{boxBase: boxBase{h}} },
	"avcC": func(h boxHeader) box { return &avcCBox{boxBase: boxBase{h}} },
	"stsz": func(h boxHeader) box { return &stszBox{boxBase: boxBase{h}} },
	"stsc": func(h boxHeader) box { return &stscBox{boxBase: boxBase{h}} },
	"stco": func(h boxHeader) box { return &stcoBox{boxBase: boxBase{h}} },
	"co64": func(h boxHeader) box { return &co64Box{boxBase: boxBase{h}} },
	"moov": func(h boxHeader) box { return &moovBox{containerBox{boxBase: boxBase{h}}} },
	"trak": func(h boxHeader) box { return &trakBox{containerBox{boxBase: boxBase{h}}} },
	"mdia": func(h boxHeader) box { return &mdiaBox{containerBox{boxBase: boxBase{h}}} },
	"hdlr": func(h boxHeader) box { return &hdlrBox{boxBase: boxBase{h}} },
}

func newBox(h boxHeader) box {
	if mk := boxRegistry[h.typ]; mk != nil {
		return mk(h)
	}
	return &unknownBox{boxBase{h}}
}

// readBoxes reads the sibling boxes occupying [start, end).
func readBoxes(r io.ReadSeeker, start, end int64) ([]box, error) {
	if _, err := r.Seek(start, io.SeekStart); err != nil {
		return nil, err
	}
	var boxes []box
	for {
		h, ok, err := readHeader(r, end)
		if err != nil {
			return nil, err
		}
		if !ok {
			return boxes, nil
		}
		b := newBox(h)
		if err := b.read(r); err != nil {
			return nil, err
		}
		if _, err := r.Seek(h.end, io.SeekStart); err != nil {
			return nil, err
		}
		boxes = append(boxes, b)
	}
}

// find returns the boxes reached by matching path[0] among boxes, then path[1] among their children, ...
func find(boxes []box, path ...string) []box {
	level := boxes
	for i, typ := range path {
		var matched []box
		for _, b := range level {
			if b.header().typ == typ {
				matched = append(matched, b)
			}
		}
		if i == len(path)-1 {
			return matched
		}
		level = nil
		for _, b := range matched {
			if p, ok := b.(parent); ok {
				level = append(level, p.kids()...)
			}
		}
	}
	return nil
}
