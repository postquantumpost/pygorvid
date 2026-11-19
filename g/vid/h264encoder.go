package vid

import "encoding/binary"

type h264BitWriter struct {
	data     []byte
	bitCount int
}

type h264CABACWriter struct {
	contexts     [CABACContextCount]CABACContextModel
	low          uint32
	codeRange    uint32
	bitsToFollow int
	bits         []byte
	firstBit     bool
}

func newH264CABACWriter(sliceType, cabacInitIDC uint8, sliceQPY int) *h264CABACWriter {
	contexts, err := NewCABACSliceContexts(sliceType, cabacInitIDC, sliceQPY)
	if err != nil {
		panic(err)
	}
	return &h264CABACWriter{contexts: contexts, codeRange: cabacInitialRange, firstBit: true}
}

func (w *h264CABACWriter) put(bit byte) {
	if w.firstBit {
		w.firstBit = false
	} else {
		w.bits = append(w.bits, bit)
	}
	for w.bitsToFollow != 0 {
		w.bits = append(w.bits, bit^1)
		w.bitsToFollow--
	}
}

func (w *h264CABACWriter) renormalize() {
	for w.codeRange < 256 {
		switch {
		case w.low < 256:
			w.put(0)
		case w.low >= 512:
			w.put(1)
			w.low -= 512
		default:
			w.bitsToFollow++
			w.low -= 256
		}
		w.low <<= 1
		w.codeRange <<= 1
	}
}

func (w *h264CABACWriter) decision(value int, contextIndex int) {
	model := &w.contexts[contextIndex]
	rangeLPS := uint32(cabacRangeLPS[(w.codeRange>>6)&3][model.stateIndex])
	if (value != 0) == model.valueMPS {
		w.codeRange -= rangeLPS
		model.Update(value != 0)
		w.renormalize()
		return
	}
	w.low += w.codeRange - rangeLPS
	w.codeRange = rangeLPS
	model.Update(value != 0)
	w.renormalize()
}

func (w *h264CABACWriter) lumaCBP(left, top, desired int) {
	contextIndex := boolInt(left&2 == 0) + 2*boolInt(top&4 == 0)
	w.decision(desired&1, 73+contextIndex)
	current := desired & 1
	contextIndex = boolInt(current&1 == 0) + 2*boolInt(top&8 == 0)
	w.decision((desired>>1)&1, 73+contextIndex)
	current |= desired & 2
	contextIndex = boolInt(left&8 == 0) + 2*boolInt(current&1 == 0)
	w.decision((desired>>2)&1, 73+contextIndex)
	current |= desired & 4
	contextIndex = boolInt(current&4 == 0) + 2*boolInt(current&2 == 0)
	w.decision((desired>>3)&1, 73+contextIndex)
}

func (w *h264CABACWriter) zeroLumaCBP(left, top int) {
	w.lumaCBP(left, top, 0)
}

func (w *h264CABACWriter) terminate(value int) {
	w.codeRange -= 2
	if value == 0 {
		w.renormalize()
		return
	}
	w.low += w.codeRange
	w.codeRange = 2
	w.renormalize()
	w.put(byte((w.low >> 9) & 1))
	w.bits = append(w.bits, byte((w.low>>8)&1), 1)
}

func (w *h264CABACWriter) bypass(value int) {
	w.low <<= 1
	if value != 0 {
		w.low += w.codeRange
	}
	switch {
	case w.low >= 1024:
		w.low -= 1024
		w.put(1)
	case w.low < 512:
		w.put(0)
	default:
		w.low -= 512
		w.bitsToFollow++
	}
}

func (w *h264CABACWriter) level(magnitude, firstContext, greaterContext int) {
	if magnitude == 1 {
		w.decision(0, firstContext)
	} else {
		w.decision(1, firstContext)
		count := magnitude - 2
		if count > 13 {
			count = 13
		}
		for index := 0; index < count; index++ {
			w.decision(1, greaterContext+5)
		}
		if magnitude <= 14 {
			w.decision(0, greaterContext+5)
		}
	}
	w.bypass(0)
}

func (w *h264CABACWriter) end() {
	w.codeRange -= 2
	w.low += w.codeRange
	w.codeRange = 2
	w.renormalize()
	w.put(byte((w.low >> 9) & 1))
	w.bits = append(w.bits, byte((w.low>>8)&1), 1)
}

func (w *h264CABACWriter) data(padding int) []byte {
	result := make([]byte, (len(w.bits)+7)/8+padding)
	for index, bit := range w.bits {
		if bit != 0 {
			result[index/8] |= 0x80 >> (index % 8)
		}
	}
	return result
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
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

func (w *h264BitWriter) se(value int32) {
	if value > 0 {
		w.ue(uint32(2*value - 1))
	} else {
		w.ue(uint32(-2 * value))
	}
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
	w.bits(100, 8)
	w.bits(0, 8)
	w.bits(20, 8)
	w.ue(0)
	for _, value := range []uint32{1, 0, 0} {
		w.ue(value)
	}
	w.bits(0, 1)
	w.bits(0, 1)
	for _, value := range []uint32{0, 2, 2} {
		w.ue(value)
	}
	w.bits(0, 1)
	w.ue(3)
	w.ue(3)
	w.bits(0b1100, 4)
	return w.nal(0x67)
}

func (e H264Encoder) PictureParameterSet() []byte {
	return e.pictureParameterSet(0, false, 0)
}

func (e H264Encoder) pictureParameterSet(ppsID uint32, cabac bool, weightedBipredIDC uint32) []byte {
	w := h264BitWriter{}
	w.ue(ppsID)
	w.ue(0)
	w.bits(boolUint32(cabac), 1)
	w.bits(0, 1)
	for i := 0; i < 3; i++ {
		w.ue(0)
	}
	w.bits(1, 1)
	w.bits(weightedBipredIDC, 2)
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
		return e.encodeIntra4x4DirectionalFrame(5, false)
	}
	if frameIndex == 7 {
		return e.encodeIntra4x4DirectionalFrame(6, true)
	}
	if frameIndex == 8 {
		return e.encodeIntra4x4DirectionalFrameWithChromaResidual(7)
	}
	if frameIndex == 9 {
		return e.encodeIntra4x4DirectionalFrameInternal(8, true, true, true)
	}
	if frameIndex >= 10 && frameIndex <= 18 {
		return e.encodePFrame(frameIndex)
	}
	if frameIndex == 19 {
		return e.encodePFrame(19)
	}
	if frameIndex == 20 {
		return e.encodeBFrame()
	}
	if frameIndex >= 21 && frameIndex <= 23 {
		return e.encodeBFeatureFrame(frameIndex)
	}
	if frameIndex == 24 {
		return e.encodeBWeightedFrame(true)
	}
	if frameIndex == 25 {
		return e.encodeBWeightedFrame(false)
	}
	if frameIndex == 26 {
		return e.encodeInterResidualFrame()
	}
	if frameIndex == 27 {
		return e.encodeCABACIFrame()
	}
	if frameIndex == 28 {
		return e.encodeCABACPFrame()
	}
	if frameIndex == 29 {
		return e.encodeCABACBFrame()
	}
	if frameIndex == 30 {
		return e.encodeCABACResidualFrame()
	}
	return e.encodePCMFrame(frameIndex, frameIndex == 1)
}

func bMacroblock(w *h264BitWriter, kind int, subTypes []int) {
	w.ue(uint32(kind))
	switch {
	case kind == 22:
		for _, subType := range subTypes {
			w.ue(uint32(subType))
		}
		w.ue(0)
	case kind == 0:
		w.ue(0)
	case kind >= 1 && kind <= 3:
		for listIndex := 0; listIndex < 2; listIndex++ {
			if kind == 1 && listIndex == 1 || kind == 2 && listIndex == 0 {
				continue
			}
			w.se(0)
			w.se(0)
		}
		w.ue(0)
	case kind >= 4 && kind <= 21:
		flags := [][2]int{
			{1, 0}, {1, 0}, {0, 1}, {0, 1}, {1, 0}, {1, 0},
			{0, 1}, {0, 1}, {1, 1}, {1, 1}, {0, 1}, {0, 1},
			{1, 1}, {1, 1}, {0, 1}, {0, 1}, {1, 1}, {1, 1},
		}[kind-4]
		for partition := 0; partition < 2; partition++ {
			for _, active := range flags {
				if active != 0 {
					w.se(0)
					w.se(0)
				}
			}
		}
		w.ue(0)
	}
}

func beginBSlice(w *h264BitWriter, frameNum, ppsID uint32, spatialDirect, explicitWeights bool) {
	w.ue(0)
	w.ue(1)
	w.ue(ppsID)
	w.bits(frameNum, 4)
	w.bits(boolUint32(spatialDirect), 1)
	w.bits(0, 1)
	w.bits(0, 1)
	w.bits(0, 1)
	if explicitWeights {
		w.ue(0)
		w.ue(0)
		for reference := 0; reference < 2; reference++ {
			w.bits(1, 1)
			w.se(2)
			w.se(-8)
			w.bits(1, 1)
			w.se(2)
			w.se(4)
			w.se(2)
			w.se(-4)
		}
	}
	w.bits(0, 1)
	w.ue(0)
	w.ue(1)
}

func (e H264Encoder) encodeBFeatureFrame(frameIndex uint16) []byte {
	w := h264BitWriter{}
	spatialDirect := frameIndex != 22
	beginBSlice(&w, uint32(frameIndex-16)&15, 0, spatialDirect, false)
	for macroblock := 0; macroblock < 16; macroblock++ {
		w.ue(0)
		if frameIndex == 23 {
			bMacroblock(&w, 1+(macroblock%11)%3, nil)
		} else if frameIndex == 21 && macroblock%2 != 0 {
			bMacroblock(&w, 22, []int{0, 0, 0, 0})
		} else {
			bMacroblock(&w, 0, nil)
		}
	}
	return prependH264NALSize(w.nal(0x21))
}

func (e H264Encoder) encodeBWeightedFrame(explicit bool) []byte {
	ppsID := uint32(2)
	frameNum := uint32(10)
	if explicit {
		ppsID, frameNum = 1, 9
	}
	w := h264BitWriter{}
	beginBSlice(&w, frameNum, ppsID, true, explicit)
	for macroblock := 0; macroblock < 16; macroblock++ {
		w.ue(0)
		bMacroblock(&w, 3, nil)
	}
	return prependH264NALSize(w.nal(0x21))
}

func (e H264Encoder) encodeInterResidualFrame() []byte {
	w := h264BitWriter{}
	w.ue(0)
	w.ue(0)
	w.ue(0)
	w.bits(11, 4)
	w.bits(0, 1)
	w.bits(0, 1)
	w.predictionWeightTable(1, false)
	w.bits(0, 1)
	w.ue(0)
	w.ue(1)
	for macroblock := 0; macroblock < 16; macroblock++ {
		w.ue(0)
		w.ue(0)
		w.se(0)
		w.se(0)
		if macroblock == 0 || macroblock == 15 {
			w.ue(2)
			w.se(1)
			for block := 0; block < 4; block++ {
				if block == 0 || block == 3 {
					w.bits(0b01, 2)
					w.bits(0, 1)
					w.bits(1, 1)
				} else {
					w.bits(1, 1)
				}
			}
		} else {
			w.ue(0)
		}
	}
	return prependH264NALSize(w.nal(0x21))
}

func cabacNAL(header byte, rbsp []byte) []byte {
	result := []byte{header}
	zeros := 0
	for _, value := range rbsp {
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

func alignCABACHeader(header *h264BitWriter) {
	for header.bitCount%8 != 0 {
		header.bits(1, 1)
	}
}

func (e H264Encoder) encodeCABACIFrame() []byte {
	header := h264BitWriter{}
	header.ue(0)
	header.ue(2)
	header.ue(3)
	header.bits(12, 4)
	header.bits(0, 1)
	header.ue(0)
	header.ue(1)
	alignCABACHeader(&header)
	cabac := newH264CABACWriter(2, 0, 26)
	for macroblock := 0; macroblock < 16; macroblock++ {
		cabac.decision(0, 3)
		for block := 0; block < 16; block++ {
			cabac.decision(1, 68)
		}
		cabac.decision(0, 64)
		left, top := 0, 0
		if macroblock%4 == 0 {
			left = 15
		}
		if macroblock < 4 {
			top = 15
		}
		cabac.zeroLumaCBP(left, top)
		cabac.decision(0, 77)
		if macroblock != 15 {
			cabac.terminate(0)
		}
	}
	cabac.end()
	return prependH264NALSize(cabacNAL(0x21, append(header.data, cabac.data(4)...)))
}

func (e H264Encoder) encodeCABACPFrame() []byte {
	header := h264BitWriter{}
	header.ue(0)
	header.ue(0)
	header.ue(3)
	header.bits(13, 4)
	header.bits(0, 1)
	header.bits(0, 1)
	header.ue(0)
	header.ue(0)
	header.bits(0, 1)
	header.bits(0, 1)
	header.bits(0, 1)
	header.ue(0)
	header.ue(0)
	header.ue(1)
	alignCABACHeader(&header)
	cabac := newH264CABACWriter(0, 0, 26)
	for macroblock := 0; macroblock < 16; macroblock++ {
		skipped := macroblock%3 == 1
		leftSkip := macroblock%4 != 0 && (macroblock-1)%3 == 1
		topSkip := macroblock >= 4 && (macroblock-4)%3 == 1
		increment := 0
		if macroblock%4 != 0 && !leftSkip {
			increment++
		}
		if macroblock >= 4 && !topSkip {
			increment++
		}
		cabac.decision(boolInt(skipped), 11+increment)
		if skipped {
			if macroblock != 15 {
				cabac.terminate(0)
			}
			continue
		}
		cabac.decision(0, 14)
		cabac.decision(0, 15)
		cabac.decision(0, 16)
		cabac.decision(0, 40)
		cabac.decision(0, 47)
		left, top := 0, 0
		if macroblock%4 == 0 {
			left = 15
		}
		if macroblock < 4 {
			top = 15
		}
		cabac.zeroLumaCBP(left, top)
		cabac.decision(0, 77)
		if macroblock != 15 {
			cabac.terminate(0)
		}
	}
	cabac.end()
	return prependH264NALSize(cabacNAL(0x21, append(header.data, cabac.data(4)...)))
}

func (e H264Encoder) encodeCABACBFrame() []byte {
	header := h264BitWriter{}
	header.ue(0)
	header.ue(1)
	header.ue(3)
	header.bits(14, 4)
	header.bits(1, 1)
	header.bits(0, 1)
	header.bits(0, 1)
	header.bits(0, 1)
	header.bits(0, 1)
	header.ue(0)
	header.ue(0)
	header.ue(1)
	alignCABACHeader(&header)
	cabac := newH264CABACWriter(1, 0, 26)
	for macroblock := 0; macroblock < 16; macroblock++ {
		cabac.decision(0, 24+boolInt(macroblock%4 != 0)+boolInt(macroblock >= 4))
		cabac.decision(0, 27)
		left, top := 0, 0
		if macroblock%4 == 0 {
			left = 15
		}
		if macroblock < 4 {
			top = 15
		}
		cabac.zeroLumaCBP(left, top)
		cabac.decision(0, 77)
		if macroblock != 15 {
			cabac.terminate(0)
		}
	}
	cabac.end()
	return prependH264NALSize(cabacNAL(0x21, append(header.data, cabac.data(4)...)))
}

func (e H264Encoder) encodeCABACResidualFrame() []byte {
	header := h264BitWriter{}
	header.ue(0)
	header.ue(2)
	header.ue(3)
	header.bits(15, 4)
	header.bits(0, 1)
	header.ue(0)
	header.ue(1)
	alignCABACHeader(&header)
	cabac := newH264CABACWriter(2, 0, 26)
	for macroblock := 0; macroblock < 16; macroblock++ {
		cabac.decision(0, 3)
		for block := 0; block < 16; block++ {
			cabac.decision(1, 68)
		}
		cabac.decision(0, 64)
		left, top := 0, 0
		if macroblock%4 == 0 {
			left = 15
		}
		if macroblock < 4 {
			top = 15
		}
		desired := 0
		if macroblock == 15 {
			desired = 1
		}
		cabac.lumaCBP(left, top, desired)
		cabac.decision(0, 77)
		if macroblock == 15 {
			cabac.decision(0, 60)
			cabac.decision(1, 93)
			cabac.decision(1, 134)
			cabac.decision(1, 195)
			cabac.decision(0, 248)
			cabac.bypass(0)
			cabac.decision(0, 94)
			cabac.decision(0, 95)
			cabac.decision(0, 93)
		}
		if macroblock != 15 {
			cabac.terminate(0)
		}
	}
	cabac.end()
	return prependH264NALSize(cabacNAL(0x21, append(header.data, cabac.data(4)...)))
}

func (w *h264BitWriter) predictionWeightTable(referenceCount int, explicit bool) {
	w.ue(0)
	w.ue(0)
	for index := 0; index < referenceCount; index++ {
		if explicit {
			w.bits(1, 1)
			w.se(2)
			w.se(-16)
			w.bits(1, 1)
			w.se(2)
			w.se(8)
			w.se(2)
			w.se(-8)
		} else {
			w.bits(0, 1)
			w.bits(0, 1)
		}
	}
}

func (w *h264BitWriter) pMacroblock(kind int, vectors [][2]int32, references []int, multipleReferences bool, subTypes []int) {
	w.ue(uint32(kind))
	if kind == 3 || kind == 4 {
		for _, subType := range subTypes {
			w.ue(uint32(subType))
		}
	}
	for partition, vector := range vectors {
		if multipleReferences && kind != 4 {
			if references[partition] == 0 {
				w.bits(1, 1)
			} else {
				w.bits(0, 1)
			}
		}
		w.se(vector[0])
		w.se(vector[1])
	}
	w.ue(0)
}

func (e H264Encoder) encodePFrame(frameIndex uint16) []byte {
	w := h264BitWriter{}
	w.ue(0)
	w.ue(0)
	w.ue(0)
	w.bits(uint32(frameIndex-1)&15, 4)
	multipleReferences := frameIndex == 16 || frameIndex == 17
	referenceCount := 1
	w.bits(boolUint32(multipleReferences), 1)
	if multipleReferences {
		w.ue(1)
		referenceCount = 2
	}
	reordered := frameIndex == 17
	w.bits(boolUint32(reordered), 1)
	if reordered {
		w.ue(0)
		w.ue(1)
		w.ue(3)
	}
	w.predictionWeightTable(referenceCount, frameIndex == 18)
	w.bits(boolUint32(reordered), 1)
	if reordered {
		w.ue(1)
		w.ue(0)
		w.ue(0)
	}
	w.ue(0)
	w.ue(1)

	switch frameIndex {
	case 10:
		for mb := 0; mb < 16; mb++ {
			w.ue(0)
			w.pMacroblock(0, [][2]int32{{0, 0}}, nil, false, nil)
		}
	case 11:
		w.ue(0)
		w.pMacroblock(0, [][2]int32{{0, 0}}, nil, false, nil)
		w.ue(14)
		w.pMacroblock(0, [][2]int32{{0, 0}}, nil, false, nil)
	case 12:
		motion := [][2]int32{{0, 0}, {4, 0}, {-4, 0}, {0, 4}, {0, -4}}
		for mb := 0; mb < 16; mb++ {
			w.ue(0)
			w.pMacroblock(0, [][2]int32{motion[mb%len(motion)]}, nil, false, nil)
		}
	case 13:
		for mb := 0; mb < 16; mb++ {
			w.ue(0)
			w.pMacroblock(1+mb%2, [][2]int32{{4, 0}, {-4, 4}}, nil, false, nil)
		}
	case 14:
		for mb := 0; mb < 16; mb++ {
			w.ue(0)
			subTypes := []int{0, 1, 2, 3}
			counts := []int{1, 2, 2, 4}
			vectors := make([][2]int32, 0, 9)
			for sub, subType := range subTypes {
				for part := 0; part < counts[subType]; part++ {
					vectors = append(vectors, [2]int32{int32((sub - 1) * 4), int32((part - 1) * 4)})
				}
			}
			w.pMacroblock(3+mb%2, vectors, nil, false, subTypes)
		}
	case 15:
		motion := [][2]int32{{1, 0}, {-1, 2}, {2, -1}, {-2, 1}}
		for mb := 0; mb < 16; mb++ {
			w.ue(0)
			w.pMacroblock(0, [][2]int32{motion[mb%4]}, nil, false, nil)
		}
	case 16, 17:
		for mb := 0; mb < 16; mb++ {
			w.ue(0)
			w.pMacroblock(0, [][2]int32{{0, 0}}, []int{mb % 2}, true, nil)
		}
	case 19:
		for mb := 0; mb < 16; mb++ {
			w.ue(0)
			w.pMacroblock(0, [][2]int32{{4, 0}}, nil, false, nil)
		}
	default:
		for mb := 0; mb < 16; mb++ {
			w.ue(0)
			w.pMacroblock(0, [][2]int32{{0, 0}}, nil, false, nil)
		}
	}
	return prependH264NALSize(w.nal(0x21))
}

func (e H264Encoder) encodeBFrame() []byte {
	w := h264BitWriter{}
	w.ue(0)
	w.ue(1)
	w.ue(0)
	w.bits(2, 4)
	w.bits(1, 1)
	w.bits(0, 1)
	w.bits(0, 1)
	w.bits(0, 1)
	w.ue(0)
	w.ue(1)
	for mb := 0; mb < 16; mb++ {
		w.ue(0)
		w.ue(uint32(1 + mb%3))
		lists := 1
		if mb%3 == 2 {
			lists = 2
		}
		for list := 0; list < lists; list++ {
			w.se(0)
			w.se(0)
		}
		w.ue(0)
	}
	return prependH264NALSize(w.nal(0x01))
}

func boolUint32(value bool) uint32 {
	if value {
		return 1
	}
	return 0
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

func (e H264Encoder) encodeIntra4x4DirectionalFrame(frameNum uint32, residual bool) []byte {
	return e.encodeIntra4x4DirectionalFrameInternal(frameNum, residual, false, false)
}

func (e H264Encoder) encodeIntra4x4DirectionalFrameWithChromaResidual(frameNum uint32) []byte {
	return e.encodeIntra4x4DirectionalFrameInternal(frameNum, true, true, false)
}

func (e H264Encoder) encodeIntra4x4DirectionalFrameInternal(
	frameNum uint32, residual, chromaResidual, expandedCAVLC bool,
) []byte {
	w := h264BitWriter{}
	for _, value := range []uint32{0, 2, 0} {
		w.ue(value)
	}
	w.bits(frameNum, 4)
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
		if residual && mb == 15 {
			cbpCode := uint32(29)
			if chromaResidual {
				cbpCode = 42
			}
			w.ue(cbpCode)
			w.se(1)
			if expandedCAVLC {
				// nC = 0: sixteen alternating +2/-2 coefficients.
				w.bits(0b0000000000000100, 16)
				for index := 0; index < 16; index++ {
					value := uint32(2)
					if index%2 != 0 {
						value = 3
					}
					w.bits(value, 2)
				}
				// nC >= 8: three trailing ones and two total zeros.
				w.bits(0b001011, 6)
				w.bits(0b010, 3)
				w.bits(0b110, 3)
				w.bits(0b010, 3)
				// nC >= 8 followed by nC = 2 select different empty-block tables.
				w.bits(0b000011, 6)
				w.bits(0b11, 2)
			} else {
				// Positive trailing-one token, zero runs, then three empty blocks.
				w.bits(0b01, 2)
				w.bits(0, 1)
				w.bits(1, 1)
				w.bits(1, 1)
				w.bits(1, 1)
				w.bits(1, 1)
			}
			if chromaResidual {
				// Cb DC +1 and Cr DC -1, each at the sole 2x2 DC coefficient.
				w.bits(0b101, 3)
				w.bits(0b111, 3)
				// One Cb and one Cr AC coefficient; remaining AC blocks are empty.
				for _, sign := range []uint32{0, 1} {
					w.bits(0b01, 2)
					w.bits(sign, 1)
					w.bits(1, 1)
					w.bits(0b111, 3)
				}
			}
		} else {
			w.ue(3)
		}
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
