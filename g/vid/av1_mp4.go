package vid

import (
	"errors"
	"io"
)

type av1SampleEntryBox struct {
	visualSampleEntryBox
}

func newAV1Box(header boxHeader) box {
	if header.typ == "av01" {
		return &av1SampleEntryBox{visualSampleEntryBox{boxBase: boxBase{header}}}
	}
	return &av1CBox{boxBase: boxBase{header}}
}

type AV1Configuration struct {
	Profile              byte
	Level                byte
	Tier                 byte
	BitDepth             byte
	Monochrome           bool
	ChromaSubsamplingX   bool
	ChromaSubsamplingY   bool
	ChromaSamplePosition byte
	ConfigOBUs           [][]byte
	Timescale            uint32
}

func readAV1SampleConfiguration(entry box, timescale uint32) (*AV1Configuration, error) {
	container, ok := entry.(parent)
	if !ok {
		return nil, errors.New("invalid AV1 sample entry")
	}
	var configuration *av1CBox
	for _, child := range container.kids() {
		if typed, ok := child.(*av1CBox); ok {
			if configuration != nil {
				return nil, errors.New("av01 sample entry must contain one av1C box")
			}
			configuration = typed
		}
	}
	if configuration == nil {
		return nil, errors.New("av01 sample entry must contain one av1C box")
	}
	return &AV1Configuration{
		Profile: configuration.profile, Level: configuration.level, Tier: configuration.tier,
		BitDepth: configuration.bitDepth, Monochrome: configuration.monochrome,
		ChromaSubsamplingX:   configuration.chromaSubsamplingX,
		ChromaSubsamplingY:   configuration.chromaSubsamplingY,
		ChromaSamplePosition: configuration.chromaSamplePosition,
		ConfigOBUs:           cloneNALUnits(configuration.configOBUs), Timescale: timescale,
	}, nil
}

type av1CBox struct {
	boxBase
	profile              byte
	level                byte
	tier                 byte
	bitDepth             byte
	monochrome           bool
	chromaSubsamplingX   bool
	chromaSubsamplingY   bool
	chromaSamplePosition byte
	configOBUs           [][]byte
}

func (b *av1CBox) read(r io.ReadSeeker) error {
	size := b.h.end - b.h.payloadStart()
	if size < 4 {
		return errors.New("av1C box too short")
	}
	data := make([]byte, size)
	if _, err := r.Seek(b.h.payloadStart(), io.SeekStart); err != nil {
		return err
	}
	if _, err := io.ReadFull(r, data); err != nil {
		return err
	}
	if data[0] != 0x81 {
		return errors.New("invalid AV1DecoderConfigurationRecord header")
	}
	b.profile = data[1] >> 5
	b.level = data[1] & 0x1f
	b.tier = (data[2] >> 7) & 1
	highBitDepth := data[2]&0x40 != 0
	twelveBit := data[2]&0x20 != 0
	switch {
	case twelveBit:
		b.bitDepth = 12
	case highBitDepth:
		b.bitDepth = 10
	default:
		b.bitDepth = 8
	}
	b.monochrome = data[2]&0x10 != 0
	b.chromaSubsamplingX = data[2]&0x08 != 0
	b.chromaSubsamplingY = data[2]&0x04 != 0
	b.chromaSamplePosition = data[2] & 0x03
	if data[3]&0xe0 != 0 {
		return errors.New("av1C reserved bits are invalid")
	}
	if len(data) > 4 {
		b.configOBUs = [][]byte{append([]byte(nil), data[4:]...)}
	}
	return nil
}
