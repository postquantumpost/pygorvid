package vid

import (
	"encoding/binary"
	"errors"
	"io"
)

type hevcSampleEntryBox struct {
	visualSampleEntryBox
}

func newHEVCBox(header boxHeader) box {
	if header.typ == "hvc1" || header.typ == "hev1" {
		return &hevcSampleEntryBox{visualSampleEntryBox{boxBase: boxBase{header}}}
	}
	return &hvcCBox{boxBase: boxBase{header}}
}

type HEVCConfiguration struct {
	Profile        byte
	Level          byte
	BitDepthLuma   byte
	BitDepthChroma byte
	NALLengthSize  byte
	Timescale      uint32
	Arrays         []HEVCNALArray
}

type HEVCNALArray struct {
	Complete bool
	NALType  byte
	Units    [][]byte
}

func readHEVCSampleConfiguration(entry box, timescale uint32) (*HEVCConfiguration, error) {
	container, ok := entry.(parent)
	if !ok {
		return nil, errors.New("invalid HEVC sample entry")
	}
	var config *hvcCBox
	for _, child := range container.kids() {
		if typed, ok := child.(*hvcCBox); ok {
			if config != nil {
				return nil, errors.New("HEVC sample entry must contain one hvcC box")
			}
			config = typed
		}
	}
	if config == nil {
		return nil, errors.New("HEVC sample entry must contain one hvcC box")
	}
	result := &HEVCConfiguration{
		Profile: config.profile, Level: config.level, BitDepthLuma: config.bitDepthLuma,
		BitDepthChroma: config.bitDepthChroma, NALLengthSize: config.nalLengthSize, Timescale: timescale,
	}
	for _, array := range config.arrays {
		result.Arrays = append(result.Arrays, HEVCNALArray{Complete: array.complete, NALType: array.nalType, Units: cloneNALUnits(array.units)})
	}
	return result, nil
}

type hevcNALArray struct {
	complete bool
	nalType  byte
	units    [][]byte
}

type hvcCBox struct {
	boxBase
	profile        byte
	level          byte
	bitDepthLuma   byte
	bitDepthChroma byte
	nalLengthSize  byte
	arrays         []hevcNALArray
}

func (b *hvcCBox) read(r io.ReadSeeker) error {
	payloadSize := b.h.end - b.h.payloadStart()
	if payloadSize < 23 {
		return errors.New("hvcC box too short")
	}
	data := make([]byte, payloadSize)
	if _, err := r.Seek(b.h.payloadStart(), io.SeekStart); err != nil {
		return err
	}
	if _, err := io.ReadFull(r, data); err != nil {
		return err
	}
	if data[0] != 1 {
		return errors.New("unsupported HEVC configuration version")
	}
	if data[13]&0xf0 != 0xf0 || data[15]&0xfc != 0xfc || data[16]&0xfc != 0xfc || data[17]&0xf8 != 0xf8 || data[18]&0xf8 != 0xf8 {
		return errors.New("hvcC reserved bits are invalid")
	}
	b.nalLengthSize = (data[21] & 3) + 1
	if b.nalLengthSize == 3 {
		return errors.New("hvcC uses a reserved NAL length size")
	}
	b.profile = data[1] & 0x1f
	b.level = data[12]
	b.bitDepthLuma = (data[17] & 7) + 8
	b.bitDepthChroma = (data[18] & 7) + 8
	b.arrays = nil
	offset := 23
	for range int(data[22]) {
		if len(data)-offset < 3 {
			return errors.New("hvcC array header truncated")
		}
		flags := data[offset]
		if flags&0x40 != 0 {
			return errors.New("hvcC array reserved bit is invalid")
		}
		count := int(binary.BigEndian.Uint16(data[offset+1 : offset+3]))
		offset += 3
		array := hevcNALArray{complete: flags&0x80 != 0, nalType: flags & 0x3f}
		for range count {
			if len(data)-offset < 2 {
				return errors.New("hvcC NAL length truncated")
			}
			length := int(binary.BigEndian.Uint16(data[offset : offset+2]))
			offset += 2
			if length < 2 || length > len(data)-offset {
				return errors.New("hvcC NAL exceeds box size or is too short")
			}
			if data[offset]&0x80 != 0 || data[offset+1]&7 == 0 || (data[offset]>>1)&0x3f != array.nalType {
				return errors.New("hvcC NAL header does not match array type")
			}
			array.units = append(array.units, append([]byte(nil), data[offset:offset+length]...))
			offset += length
		}
		b.arrays = append(b.arrays, array)
	}
	if offset != len(data) {
		return errors.New("hvcC has invalid trailing data")
	}
	return nil
}
