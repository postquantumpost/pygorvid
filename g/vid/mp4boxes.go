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

type stsdBox struct {
	boxBase
	sampleRate uint32
	channels   uint16
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
	if binary.BigEndian.Uint32(countBytes[:]) == 0 {
		return nil
	}
	entry, ok, err := readHeader(r, b.h.end)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("audio sample entry too short")
	}
	if entry.end-entry.payloadStart() < 28 {
		return nil
	}
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
	return nil
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
	"stsd": func(h boxHeader) box { return &stsdBox{boxBase: boxBase{h}} },
	"moov": func(h boxHeader) box { return &moovBox{containerBox{boxBase: boxBase{h}}} },
	"trak": func(h boxHeader) box { return &trakBox{containerBox{boxBase: boxBase{h}}} },
	"mdia": func(h boxHeader) box { return &mdiaBox{containerBox{boxBase: boxBase{h}}} },
	"hdlr": func(h boxHeader) box { return &hdlrBox{boxBase: boxBase{h}} },
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
		mk := boxRegistry[h.typ]
		var b box = &unknownBox{boxBase{h}}
		if mk != nil {
			b = mk(h)
		}
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
