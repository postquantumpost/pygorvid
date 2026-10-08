package vid

// Reader for the mp4 box structure. Each box type is a named type.
// To read more of the format, add a box type and register it in boxRegistry.
// Unregistered box types are skipped without being parsed.

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

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
