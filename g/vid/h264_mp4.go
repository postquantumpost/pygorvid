package vid

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

type avc1Box = visualSampleEntryBox

func newAVCBox(header boxHeader) box {
	if header.typ == "avc1" {
		return &avc1Box{boxBase: boxBase{header}}
	}
	return &avcCBox{boxBase: boxBase{header}}
}

func readAVCSampleConfiguration(entry box, timescale uint32) (*AVCConfiguration, error) {
	container, ok := entry.(parent)
	if !ok {
		return nil, errors.New("invalid AVC sample entry")
	}
	var config *avcCBox
	for _, child := range container.kids() {
		if typed, ok := child.(*avcCBox); ok {
			if config != nil {
				return nil, errors.New("multiple avcC boxes are unsupported")
			}
			config = typed
		}
	}
	if config == nil {
		return nil, errors.New("avc1 sample description has no avcC box")
	}
	return &AVCConfiguration{
		Profile: config.profile, ProfileCompat: config.profileCompat, Level: config.level,
		NALLengthSize: config.nalLengthSize, SequenceSets: cloneNALUnits(config.sequenceSets),
		PictureSets: cloneNALUnits(config.pictureSets), Timescale: timescale,
	}, nil
}

// AVCConfiguration describes the selected video track's AVC sample entry.
type AVCConfiguration struct {
	Profile       uint8
	ProfileCompat uint8
	Level         uint8
	NALLengthSize uint8
	SequenceSets  [][]byte
	PictureSets   [][]byte
	Timescale     uint32
}

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
