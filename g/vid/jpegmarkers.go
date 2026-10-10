package vid

import "encoding/binary"

type jpegHuffmanSpec struct {
	class   uint8
	id      uint8
	counts  [16]uint8
	symbols []uint8
}

func jpegJFIFAPP0Payload() []byte {
	return []byte{'J', 'F', 'I', 'F', 0, 1, 1, 0, 0, 1, 0, 1, 0, 0}
}

func jpegSOF0Payload(width, height int) ([]byte, error) {
	if width <= 0 || height <= 0 || width > 65535 || height > 65535 {
		return nil, ErrJPEGEncoderInvalidDimensions
	}
	payload := make([]byte, 15)
	payload[0] = 8
	binary.BigEndian.PutUint16(payload[1:3], uint16(height))
	binary.BigEndian.PutUint16(payload[3:5], uint16(width))
	payload[5] = 3
	copy(payload[6:], []byte{
		1, 0x11, 0,
		2, 0x11, 1,
		3, 0x11, 1,
	})
	return payload, nil
}

func jpegSOSPayload() []byte {
	return []byte{3, 1, 0, 2, 0x11, 3, 0x11, 0, 63, 0}
}

func jpegDefaultHuffmanSpecs() []jpegHuffmanSpec {
	var dcCounts, acCounts [16]uint8
	dcCounts[3] = 12
	acCounts[7] = 162
	dcSymbols := make([]uint8, 12)
	for category := range dcSymbols {
		dcSymbols[category] = uint8(category)
	}
	acSymbols := make([]uint8, 0, 162)
	acSymbols = append(acSymbols, 0x00, 0xf0)
	for run := 0; run < 16; run++ {
		for size := 1; size <= 10; size++ {
			acSymbols = append(acSymbols, uint8(run<<4|size))
		}
	}
	specs := make([]jpegHuffmanSpec, 0, 4)
	for tableID := uint8(0); tableID < 2; tableID++ {
		specs = append(specs,
			jpegHuffmanSpec{class: 0, id: tableID, counts: dcCounts, symbols: append([]uint8(nil), dcSymbols...)},
			jpegHuffmanSpec{class: 1, id: tableID, counts: acCounts, symbols: append([]uint8(nil), acSymbols...)},
		)
	}
	return specs
}

func jpegDQTMarkerPayload(luma, chroma [64]uint8) ([]byte, error) {
	payload := make([]byte, 0, 130)
	for tableID, table := range [2][64]uint8{luma, chroma} {
		payload = append(payload, byte(tableID))
		for _, index := range jpegZigZagIndices {
			value := table[index]
			if value == 0 {
				return nil, ErrJPEGEncoderInvalidQuantizationTable
			}
			payload = append(payload, value)
		}
	}
	return payload, nil
}

func jpegDHTMarkerPayload(specs []jpegHuffmanSpec) ([]byte, error) {
	payload := make([]byte, 0)
	for _, spec := range specs {
		if spec.class > 1 || spec.id > 3 {
			return nil, ErrJPEGEncoderInvalidHuffmanTable
		}
		if _, err := newJPEGHuffmanTable(spec.counts, spec.symbols); err != nil {
			return nil, err
		}
		if len(spec.symbols) > 255 || len(payload)+17+len(spec.symbols) > 65533 {
			return nil, ErrJPEGEncoderSegmentTooLarge
		}
		payload = append(payload, spec.class<<4|spec.id)
		payload = append(payload, spec.counts[:]...)
		payload = append(payload, spec.symbols...)
	}
	return payload, nil
}

func appendJPEGFrameHeaders(width, height int, luma, chroma [64]uint8, huffman []jpegHuffmanSpec) ([]byte, error) {
	output, err := appendJPEGMarker(nil, 0xd8, nil)
	if err != nil {
		return nil, err
	}
	output, err = appendJPEGMarker(output, 0xe0, jpegJFIFAPP0Payload())
	if err != nil {
		return nil, err
	}
	dqt, err := jpegDQTMarkerPayload(luma, chroma)
	if err != nil {
		return nil, err
	}
	output, err = appendJPEGMarker(output, 0xdb, dqt)
	if err != nil {
		return nil, err
	}
	sof, err := jpegSOF0Payload(width, height)
	if err != nil {
		return nil, err
	}
	output, err = appendJPEGMarker(output, 0xc0, sof)
	if err != nil {
		return nil, err
	}
	dht, err := jpegDHTMarkerPayload(huffman)
	if err != nil {
		return nil, err
	}
	output, err = appendJPEGMarker(output, 0xc4, dht)
	if err != nil {
		return nil, err
	}
	return appendJPEGMarker(output, 0xda, jpegSOSPayload())
}
