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
	w := h264BitWriter{}
	for _, value := range []uint32{0, 2, 0} {
		w.ue(value)
	}
	w.bits(0, 4)
	w.ue(uint32(idrPicID))
	w.bits(0, 2)
	w.ue(0)
	w.ue(1)
	for mb := 0; mb < 16; mb++ {
		w.ue(25)
		w.align()
		for i := 0; i < 384; i++ {
			w.data = append(w.data, 128)
		}
		w.bitCount += 384 * 8
	}
	nal := w.nal(0x65)
	result := make([]byte, 4, 4+len(nal))
	binary.BigEndian.PutUint32(result, uint32(len(nal)))
	return append(result, nal...)
}
