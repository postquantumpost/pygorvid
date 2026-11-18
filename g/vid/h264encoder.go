package vid

import "encoding/binary"

type h264BitWriter struct {
	data     []byte
	bitCount int
}

func (w *h264BitWriter) bits(value uint32, count int) {
	for shift := count - 1; shift >= 0; shift-- {
		if w.bitCount%8 == 0 {
			w.data = append(w.data, 0)
		}
		w.data[len(w.data)-1] |= byte((value>>shift)&1) << (7 - w.bitCount%8)
		w.bitCount++
	}
}

func (w *h264BitWriter) ue(value uint32) {
	count := 0
	for v := value + 1; v > 0; v >>= 1 {
		count++
	}
	w.bits(0, count-1)
	w.bits(value+1, count)
}

func (w *h264BitWriter) align() {
	for w.bitCount%8 != 0 {
		w.bits(0, 1)
	}
}

func (w *h264BitWriter) nal(header byte) []byte {
	w.bits(1, 1)
	w.align()
	result := []byte{header}
	zeros := 0
	for _, value := range w.data {
		if zeros == 2 && value <= 3 {
			result = append(result, 3)
			zeros = 0
		}
		result = append(result, value)
		if value == 0 {
			zeros++
		} else {
			zeros = 0
		}
	}
	return result
}

// H264Encoder initially encodes only 64x64 grey Baseline I_PCM IDR pictures.
type H264Encoder struct{}

func NewH264Encoder() H264Encoder { return H264Encoder{} }

func (e H264Encoder) SequenceParameterSet() []byte {
	w := h264BitWriter{}
	w.bits(66, 8)
	w.bits(0xc0, 8)
	w.bits(20, 8)
	for _, value := range []uint32{0, 0, 2, 1} {
		w.ue(value)
	}
	w.bits(0, 1)
	w.ue(3)
	w.ue(3)
	w.bits(0b1100, 4)
	return w.nal(0x67)
}

func (e H264Encoder) PictureParameterSet() []byte {
	w := h264BitWriter{}
	w.ue(0)
	w.ue(0)
	w.bits(0, 2)
	for i := 0; i < 3; i++ {
		w.ue(0)
	}
	w.bits(0, 3)
	for i := 0; i < 3; i++ {
		w.ue(0)
	}
	w.bits(0b100, 3)
	return w.nal(0x68)
}

func (e H264Encoder) EncodeGreyFrame(idrPicID uint16) []byte {
	return e.encodePCMFrame(idrPicID, false)
}

func (e H264Encoder) EncodeGeneratedFrame(frameIndex uint16) []byte {
	if frameIndex == 3 {
		return e.encodeIntra16x16DCFrame()
	}
	if frameIndex == 4 {
		return e.encodeIntra16x16PredictionModesFrame()
	}
	if frameIndex == 5 {
		return e.encodeIntra4x4DCFrame()
	}
	if frameIndex == 6 {
		return e.encodeIntra4x4DirectionalFrame()
	}
	return e.encodePCMFrame(frameIndex, frameIndex == 1)
}

func (e H264Encoder) encodeIntra16x16DCFrame() []byte {
	w := h264BitWriter{}
	for _, value := range []uint32{0, 2, 0} {
		w.ue(value)
	}

	w.bits(2, 4)
	w.bits(0, 1)
	w.ue(0)
	w.ue(1)
	for mb := 0; mb < 16; mb++ {
		w.ue(3)
		w.ue(0)
		w.ue(0)
		w.bits(1, 1)
	}
	return prependH264NALSize(w.nal(0x21))
}

func (e H264Encoder) encodeIntra4x4DCFrame() []byte {
	w := h264BitWriter{}
	for _, value := range []uint32{0, 2, 0} {
		w.ue(value)
	}
	w.bits(4, 4)
	w.bits(0, 1)
	w.ue(0)
	w.ue(1)
	for mb := 0; mb < 16; mb++ {
		w.ue(0)
		for block := 0; block < 16; block++ {
			w.bits(1, 1)
		}
		w.ue(0)
		w.ue(3)
	}
	return prependH264NALSize(w.nal(0x21))
}

func (e H264Encoder) encodeIntra4x4DirectionalFrame() []byte {
	w := h264BitWriter{}
	for _, value := range []uint32{0, 2, 0} {
		w.ue(value)
	}
	w.bits(5, 4)
	w.bits(0, 1)
	w.ue(0)
	w.ue(1)

	w.ue(25)
	w.align()
	for row := 0; row < 16; row++ {
		for column := 0; column < 16; column++ {
			w.data = append(w.data, byte(32+3*column+5*row))
		}
	}
	for _, base := range []int{80, 176} {
		for row := 0; row < 8; row++ {
			for column := 0; column < 8; column++ {
				w.data = append(w.data, byte(base+column+2*row))
			}
		}
	}
	w.bitCount += 384 * 8

	modes := [...]int{2, 2, 2, 4, 8, 1, 5, 6, 7, 0, 3, 4, 5, 7, 6, 8}
	scan := [...]int{0, 1, 4, 5, 2, 3, 6, 7, 8, 9, 12, 13, 10, 11, 14, 15}
	macroblockModes := [][16]int{{2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2}}
	for mb := 1; mb < 16; mb++ {
		w.ue(0)
		mbX, mbY := mb%4, mb/4
		for block, desired := range modes {
			raster := scan[block]
			column, row := raster%4, raster/4
			leftMode := -1
			topMode := -1
			if column != 0 {
				leftMode = modes[indexIntra4x4Scan(scan, raster-1)]
			} else if mbX != 0 {
				leftMode = macroblockModes[mb-1][indexIntra4x4Scan(scan, row*4+3)]
			}
			if row != 0 {
				topMode = modes[indexIntra4x4Scan(scan, raster-4)]
			} else if mbY != 0 {
				topMode = macroblockModes[mb-4][indexIntra4x4Scan(scan, 12+column)]
			}
			predicted := 2
			if leftMode >= 0 && topMode >= 0 {
				predicted = min(leftMode, topMode)
			}
			if desired == predicted {
				w.bits(1, 1)
			} else {
				w.bits(0, 1)
				remaining := desired
				if desired >= predicted {
					remaining--
				}
				w.bits(uint32(remaining), 3)
			}
		}
		w.ue(0)
		w.ue(3)
		macroblockModes = append(macroblockModes, modes)
	}
	return prependH264NALSize(w.nal(0x21))
}

func indexIntra4x4Scan(scan [16]int, raster int) int {
	for index, value := range scan {
		if value == raster {
			return index
		}
	}
	panic("invalid Intra4x4 raster index")
}

func (e H264Encoder) encodeIntra16x16PredictionModesFrame() []byte {
	w := h264BitWriter{}
	for _, value := range []uint32{0, 2, 0} {
		w.ue(value)
	}
	w.bits(3, 4)
	w.bits(0, 1)
	w.ue(0)
	w.ue(1)
	lumaModes := [...]uint32{2, 2, 2, 2, 0, 3, 2, 1, 0, 0, 1, 2, 0, 3, 2, 1}
	chromaModes := [...]uint32{0, 1, 1, 1, 2, 3, 0, 1, 2, 2, 3, 0, 2, 3, 2, 1}
	for index := range lumaModes {
		extra := uint32(0)
		if index == 0 {
			extra = 8
		}
		w.ue(1 + lumaModes[index] + extra)
		w.ue(chromaModes[index])
		w.ue(0)
		if index == 0 {
			w.bits(0b01000011, 8)
			w.bits(0b10000, 5)
			w.bits(0b10000, 5)
			w.bits(0xff, 8)
		} else {
			w.bits(1, 1)
		}
	}
	return prependH264NALSize(w.nal(0x21))
}

func (e H264Encoder) encodePCMFrame(idrPicID uint16, emulationPreventionTest bool) []byte {
	w := h264BitWriter{}
	for _, value := range []uint32{0, 2, 0} {
		w.ue(value)
	}
	nonIDRReference := idrPicID == 2
	if nonIDRReference {
		w.bits(1, 4)
		w.bits(0, 1)
	} else {
		w.bits(0, 4)
		w.ue(uint32(idrPicID))
		w.bits(0, 2)
	}
	w.ue(0)
	w.ue(1)
	for mb := 0; mb < 16; mb++ {
		w.ue(25)
		w.align()
		if emulationPreventionTest && mb == 0 {
			w.data = append(w.data, 0, 0, 1)
			for i := 0; i < 253; i++ {
				w.data = append(w.data, 128)
			}
		} else {
			for i := 0; i < 256; i++ {
				w.data = append(w.data, 128)
			}
		}
		for i := 0; i < 128; i++ {
			w.data = append(w.data, 128)
		}
		w.bitCount += 384 * 8
	}
	header := byte(0x65)
	if nonIDRReference {
		header = 0x21
	}
	return prependH264NALSize(w.nal(header))
}

func prependH264NALSize(nal []byte) []byte {
	result := make([]byte, 4, 4+len(nal))
	binary.BigEndian.PutUint32(result, uint32(len(nal)))
	return append(result, nal...)
}
