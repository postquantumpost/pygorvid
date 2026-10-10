package vid

import (
	"errors"
	"fmt"
)

var (
	ErrCABACOffsetOutOfRange     = errors.New("CABAC initial offset is outside the arithmetic range")
	ErrCABACRangeOutOfRange      = errors.New("CABAC arithmetic range is outside [1,510]")
	ErrCABACContextInitRange     = errors.New("CABAC context initialization value is outside its valid range")
	ErrCABACContextState         = errors.New("CABAC context state index is outside [0,63]")
	ErrCABACQPYDeltaOutOfRange   = errors.New("CABAC mb_qp_delta exceeds the 8-bit QP range")
	ErrCABACUnsupportedSyntax    = errors.New("CABAC syntax is unsupported for this slice type")
	ErrCABACIntraPredModeRange   = errors.New("CABAC intra4x4 prediction mode is outside [0,8]")
	ErrCABACCBPOutOfRange        = errors.New("CABAC luma coded_block_pattern is outside [0,15]")
	ErrCABACChromaCBPOutOfRange  = errors.New("CABAC chroma coded_block_pattern is outside [0,2]")
	ErrCABACNonzeroCountRange    = errors.New("CABAC luma 4x4 nonzero count is outside [0,16]")
	ErrCABACCoeffLevelOutOfRange = errors.New("CABAC coefficient level exceeds the supported bypass prefix")
	ErrCABACCoeffSignRange       = errors.New("CABAC coefficient magnitude exceeds the signed output range")
	ErrCABACEmptyResidualBlock   = errors.New("CABAC residual block has no significant coefficients")
	ErrCABACTerminated           = errors.New("CABAC decoder is already terminated")
)

var ErrCABACMotionVectorDifferenceOutOfRange = errors.New("CABAC motion-vector difference exceeds the signed output range")

const cabacInitialRange uint32 = 510
const cabacMaxQPY uint32 = 51
const cabacMaxMotionVectorDifference uint32 = uint32(^uint32(0) >> 1)

var cabacCoeffAbsLevel1Context = [8]uint8{1, 2, 3, 4, 0, 0, 0, 0}
var cabacCoeffAbsLevelGreater1Context = [8]uint8{5, 5, 5, 5, 6, 7, 8, 9}
var cabacCoeffLevel1Transition = [8]uint8{1, 2, 3, 3, 4, 5, 6, 7}
var cabacCoeffLevelGreater1Transition = [8]uint8{4, 4, 4, 4, 5, 6, 7, 7}

var cabacRangeLPS = [4][64]uint8{
	{128, 128, 128, 123, 116, 111, 105, 100, 95, 90, 85, 81, 77, 73, 69, 66, 62, 59, 56, 53, 51, 48, 46, 43, 41, 39, 37, 35, 33, 32, 30, 29, 27, 26, 24, 23, 22, 21, 20, 19, 18, 17, 16, 15, 14, 14, 13, 12, 12, 11, 11, 10, 10, 9, 9, 8, 8, 7, 7, 7, 6, 6, 6, 2},
	{176, 167, 158, 150, 142, 135, 128, 122, 116, 110, 104, 99, 94, 89, 85, 80, 76, 72, 69, 65, 62, 59, 56, 53, 50, 48, 45, 43, 41, 39, 37, 35, 33, 31, 30, 28, 27, 26, 24, 23, 22, 21, 20, 19, 18, 17, 16, 15, 14, 14, 13, 12, 12, 11, 11, 10, 9, 9, 9, 8, 8, 7, 7, 2},
	{208, 197, 187, 178, 169, 160, 152, 144, 137, 130, 123, 117, 111, 105, 100, 95, 90, 86, 81, 77, 73, 69, 66, 63, 59, 56, 54, 51, 48, 46, 43, 41, 39, 37, 35, 33, 32, 30, 29, 27, 26, 25, 23, 22, 21, 20, 19, 18, 17, 16, 15, 15, 14, 13, 12, 12, 11, 11, 10, 10, 9, 9, 8, 2},
	{240, 227, 216, 205, 195, 185, 175, 166, 158, 150, 142, 135, 128, 122, 116, 110, 104, 99, 94, 89, 85, 80, 76, 72, 69, 65, 62, 59, 56, 53, 50, 48, 45, 43, 41, 39, 37, 35, 33, 31, 30, 28, 27, 25, 24, 23, 22, 21, 20, 19, 18, 17, 16, 15, 14, 14, 13, 12, 12, 11, 11, 10, 9, 2},
}

var cabacTransitionLPS = [64]uint8{
	0, 0, 1, 2, 2, 4, 4, 5, 6, 7, 8, 9, 9, 11, 11, 12,
	13, 13, 15, 15, 16, 16, 18, 18, 19, 19, 21, 21, 22, 22, 23, 24,
	24, 25, 26, 26, 27, 27, 28, 29, 29, 30, 30, 30, 31, 32, 32, 33,
	33, 33, 34, 34, 35, 35, 35, 36, 36, 37, 37, 37, 38, 38, 63, 63,
}

// CABACContextModel stores pStateIdx and valMPS for one syntax context.
type CABACContextModel struct {
	stateIndex uint8
	valueMPS   bool
}

// NewCABACContextModel applies the H.264 context initialization equation.
func NewCABACContextModel(m, n, sliceQPY int) (*CABACContextModel, error) {
	if m < -128 || m > 127 || n < -128 || n > 127 || sliceQPY < 0 || sliceQPY > 51 {
		return nil, ErrCABACContextInitRange
	}
	preContextState := (m*sliceQPY)>>4 + n
	if preContextState < 1 {
		preContextState = 1
	} else if preContextState > 126 {
		preContextState = 126
	}
	model := &CABACContextModel{}
	if preContextState <= 63 {
		model.stateIndex = uint8(63 - preContextState)
	} else {
		model.stateIndex = uint8(preContextState - 64)
		model.valueMPS = true
	}
	return model, nil
}

func (model *CABACContextModel) StateIndex() uint8 { return model.stateIndex }

func (model *CABACContextModel) MPS() bool { return model.valueMPS }

// Update adapts the context after decoding one bin.
func (model *CABACContextModel) Update(binValue bool) {
	if binValue == model.valueMPS {
		if model.stateIndex < 62 {
			model.stateIndex++
		}
		return
	}
	if model.stateIndex == 0 {
		model.valueMPS = !model.valueMPS
	}
	model.stateIndex = cabacTransitionLPS[model.stateIndex]
}

// CABACArithmeticDecoder owns the arithmetic state for one CABAC substream.
type CABACArithmeticDecoder struct {
	bits       *BitReader
	codeRange  uint32
	codeOffset uint32
	terminated bool
}

// NewCABACArithmeticDecoder initializes the arithmetic engine from CABAC bytes.
// The input begins at the byte-aligned CABAC substream, after slice-header alignment bits.
func NewCABACArithmeticDecoder(data []byte) (*CABACArithmeticDecoder, error) {
	reader := NewBitReader(data)
	offset, err := reader.ReadBits(9)
	if err != nil {
		return nil, fmt.Errorf("CABAC initial offset: %w", err)
	}
	if offset >= cabacInitialRange {
		return nil, ErrCABACOffsetOutOfRange
	}
	return &CABACArithmeticDecoder{
		bits:       reader,
		codeRange:  cabacInitialRange,
		codeOffset: offset,
	}, nil
}

func (decoder *CABACArithmeticDecoder) CodeRange() uint32 { return decoder.codeRange }

func (decoder *CABACArithmeticDecoder) CodeOffset() uint32 { return decoder.codeOffset }

// DecodeBin decodes one regular context-coded bin and updates its model.
func (decoder *CABACArithmeticDecoder) DecodeBin(model *CABACContextModel) (bool, error) {
	if model == nil {
		return false, ErrCABACContextState
	}
	if model.stateIndex >= 64 {
		return false, ErrCABACContextState
	}
	if err := decoder.validateBinState(); err != nil {
		return false, err
	}
	originalRange, originalOffset := decoder.codeRange, decoder.codeOffset
	startBitOffset := decoder.bits.bitOffset
	rangeLPS := uint32(cabacRangeLPS[(decoder.codeRange>>6)&3][model.stateIndex])
	rangeMPS := decoder.codeRange - rangeLPS
	decoded := model.valueMPS
	if decoder.codeOffset >= rangeMPS {
		decoded = !decoded
		decoder.codeOffset -= rangeMPS
		decoder.codeRange = rangeLPS
	} else {
		decoder.codeRange = rangeMPS
	}
	if err := decoder.Renormalize(); err != nil {
		decoder.codeRange, decoder.codeOffset = originalRange, originalOffset
		decoder.bits.bitOffset = startBitOffset
		return false, err
	}
	model.Update(decoded)
	return decoded, nil
}

// DecodeMBQPDelta decodes mb_qp_delta using caller-initialized contexts 60-63.
func (decoder *CABACArithmeticDecoder) DecodeMBQPDelta(contexts *[4]CABACContextModel, previousDelta int) (int, error) {
	if contexts == nil {
		return 0, ErrCABACContextState
	}
	for index := range contexts {
		if contexts[index].stateIndex >= 64 {
			return 0, ErrCABACContextState
		}
	}
	readerCopy := *decoder.bits
	trial := *decoder
	trial.bits = &readerCopy
	trialContexts := *contexts
	contextIndex := 0
	if previousDelta != 0 {
		contextIndex = 1
	}

	first, err := trial.DecodeBin(&trialContexts[contextIndex])
	if err != nil {
		return 0, err
	}
	if !first {
		decoder.commitTrial(&trial, &readerCopy)
		*contexts = trialContexts
		return 0, nil
	}

	value := uint32(1)
	contextIndex = 2
	for {
		continued, err := trial.DecodeBin(&trialContexts[contextIndex])
		if err != nil {
			return 0, err
		}
		if !continued {
			break
		}
		value++
		if value > 2*cabacMaxQPY {
			return 0, ErrCABACQPYDeltaOutOfRange
		}
		contextIndex = 3
	}

	delta := int((value + 1) >> 1)
	if value&1 == 0 {
		delta = -delta
	}
	decoder.commitTrial(&trial, &readerCopy)
	*contexts = trialContexts
	return delta, nil
}

// DecodeMBSkipFlag decodes mb_skip_flag from three caller-initialized P- or B-slice contexts.
func (decoder *CABACArithmeticDecoder) DecodeMBSkipFlag(sliceType uint8, contexts *[3]CABACContextModel, leftAvailable, leftSkipped, topAvailable, topSkipped bool) (bool, error) {
	if contexts == nil {
		return false, ErrCABACContextState
	}
	if sliceType > 9 || (sliceType%5 != 0 && sliceType%5 != 1) {
		return false, ErrCABACUnsupportedSyntax
	}
	contextIndex := 0
	if leftAvailable && !leftSkipped {
		contextIndex++
	}
	if topAvailable && !topSkipped {
		contextIndex++
	}
	return decoder.DecodeBin(&contexts[contextIndex])
}

// DecodeIIntraMBType decodes I-slice mb_type using caller-initialized contexts 3-10.
func (decoder *CABACArithmeticDecoder) DecodeIIntraMBType(sliceType uint8, contexts *[8]CABACContextModel, leftAvailable, leftIntra16OrPCM, topAvailable, topIntra16OrPCM bool) (uint8, error) {
	if contexts == nil {
		return 0, ErrCABACContextState
	}
	if sliceType > 9 || sliceType%5 != 2 {
		return 0, ErrCABACUnsupportedSyntax
	}
	for index := range contexts {
		if contexts[index].stateIndex >= 64 {
			return 0, ErrCABACContextState
		}
	}
	readerCopy := *decoder.bits
	trial := *decoder
	trial.bits = &readerCopy
	trialContexts := *contexts
	firstContext := 0
	if leftAvailable && leftIntra16OrPCM {
		firstContext++
	}
	if topAvailable && topIntra16OrPCM {
		firstContext++
	}
	first, err := trial.DecodeBin(&trialContexts[firstContext])
	if err != nil {
		return 0, err
	}
	if !first {
		decoder.commitTrial(&trial, &readerCopy)
		*contexts = trialContexts
		return 0, nil
	}
	terminated, err := trial.DecodeTerminateBin()
	if err != nil {
		return 0, err
	}
	if terminated {
		decoder.commitTrial(&trial, &readerCopy)
		*contexts = trialContexts
		return 25, nil
	}

	mbType := uint8(1)
	cbpLuma, err := trial.DecodeBin(&trialContexts[3])
	if err != nil {
		return 0, err
	}
	if cbpLuma {
		mbType += 12
	}
	cbpChroma, err := trial.DecodeBin(&trialContexts[4])
	if err != nil {
		return 0, err
	}
	if cbpChroma {
		chromaMode, err := trial.DecodeBin(&trialContexts[5])
		if err != nil {
			return 0, err
		}
		mbType += 4 + 4*boolToUint8(chromaMode)
	}
	chromaPredictionMode, err := trial.DecodeBin(&trialContexts[6])
	if err != nil {
		return 0, err
	}
	mbType += 2 * boolToUint8(chromaPredictionMode)
	intra16x16Mode, err := trial.DecodeBin(&trialContexts[7])
	if err != nil {
		return 0, err
	}
	mbType += boolToUint8(intra16x16Mode)
	decoder.commitTrial(&trial, &readerCopy)
	*contexts = trialContexts
	return mbType, nil
}

// DecodeIntraChromaPredMode decodes intra_chroma_pred_mode using contexts 64-67.
// leftHasNonzeroMode and topHasNonzeroMode include the caller's availability and intra checks.
func (decoder *CABACArithmeticDecoder) DecodeIntraChromaPredMode(contexts *[4]CABACContextModel, leftHasNonzeroMode, topHasNonzeroMode bool) (uint8, error) {
	if contexts == nil {
		return 0, ErrCABACContextState
	}
	for index := range contexts {
		if contexts[index].stateIndex >= 64 {
			return 0, ErrCABACContextState
		}
	}
	readerCopy := *decoder.bits
	trial := *decoder
	trial.bits = &readerCopy
	trialContexts := *contexts
	contextIndex := 0
	if leftHasNonzeroMode {
		contextIndex++
	}
	if topHasNonzeroMode {
		contextIndex++
	}
	first, err := trial.DecodeBin(&trialContexts[contextIndex])
	if err != nil {
		return 0, err
	}
	mode := uint8(0)
	if first {
		second, err := trial.DecodeBin(&trialContexts[3])
		if err != nil {
			return 0, err
		}
		if !second {
			mode = 1
		} else {
			third, err := trial.DecodeBin(&trialContexts[3])
			if err != nil {
				return 0, err
			}
			if third {
				mode = 3
			} else {
				mode = 2
			}
		}
	}
	decoder.commitTrial(&trial, &readerCopy)
	*contexts = trialContexts
	return mode, nil
}

// DecodeIntra4x4PredMode decodes prev_intra4x4_pred_mode_flag and rem_intra4x4_pred_mode.
func (decoder *CABACArithmeticDecoder) DecodeIntra4x4PredMode(predictedMode uint8, contexts *[2]CABACContextModel) (uint8, error) {
	if predictedMode > 8 {
		return 0, ErrCABACIntraPredModeRange
	}
	if contexts == nil {
		return 0, ErrCABACContextState
	}
	for index := range contexts {
		if contexts[index].stateIndex >= 64 {
			return 0, ErrCABACContextState
		}
	}
	readerCopy := *decoder.bits
	trial := *decoder
	trial.bits = &readerCopy
	trialContexts := *contexts
	previousMode, err := trial.DecodeBin(&trialContexts[0])
	if err != nil {
		return 0, err
	}
	mode := predictedMode
	if !previousMode {
		remainingMode := uint8(0)
		for bitIndex := uint8(0); bitIndex < 3; bitIndex++ {
			bit, err := trial.DecodeBin(&trialContexts[1])
			if err != nil {
				return 0, err
			}
			if bit {
				remainingMode |= 1 << bitIndex
			}
		}
		mode = remainingMode
		if mode >= predictedMode {
			mode++
		}
	}
	decoder.commitTrial(&trial, &readerCopy)
	*contexts = trialContexts
	return mode, nil
}

// DecodeTransformSize8x8Flag decodes transform_size_8x8_flag using contexts 399-401.
// Neighbor flags must already account for availability and slice membership.
func (decoder *CABACArithmeticDecoder) DecodeTransformSize8x8Flag(contexts *[3]CABACContextModel, leftHas8x8Transform, topHas8x8Transform bool) (bool, error) {
	if contexts == nil {
		return false, ErrCABACContextState
	}
	contextIndex := 0
	if leftHas8x8Transform {
		contextIndex++
	}
	if topHas8x8Transform {
		contextIndex++
	}
	return decoder.DecodeBin(&contexts[contextIndex])
}

// DecodeLumaCodedBlockPattern decodes the four luma coded_block_pattern bins using contexts 73-76.
func (decoder *CABACArithmeticDecoder) DecodeLumaCodedBlockPattern(leftCBP, topCBP uint8, contexts *[4]CABACContextModel) (uint8, error) {
	if leftCBP > 15 || topCBP > 15 {
		return 0, ErrCABACCBPOutOfRange
	}
	if contexts == nil {
		return 0, ErrCABACContextState
	}
	for index := range contexts {
		if contexts[index].stateIndex >= 64 {
			return 0, ErrCABACContextState
		}
	}
	readerCopy := *decoder.bits
	trial := *decoder
	trial.bits = &readerCopy
	trialContexts := *contexts
	var pattern uint8
	contextIndex := 0
	if leftCBP&0x02 == 0 {
		contextIndex++
	}
	if topCBP&0x04 == 0 {
		contextIndex += 2
	}
	bin, err := trial.DecodeBin(&trialContexts[contextIndex])
	if err != nil {
		return 0, err
	}
	pattern |= boolToUint8(bin)

	contextIndex = 0
	if pattern&0x01 == 0 {
		contextIndex++
	}
	if topCBP&0x08 == 0 {
		contextIndex += 2
	}
	bin, err = trial.DecodeBin(&trialContexts[contextIndex])
	if err != nil {
		return 0, err
	}
	pattern |= boolToUint8(bin) << 1

	contextIndex = 0
	if leftCBP&0x08 == 0 {
		contextIndex++
	}
	if pattern&0x01 == 0 {
		contextIndex += 2
	}
	bin, err = trial.DecodeBin(&trialContexts[contextIndex])
	if err != nil {
		return 0, err
	}
	pattern |= boolToUint8(bin) << 2

	contextIndex = 0
	if pattern&0x04 == 0 {
		contextIndex++
	}
	if pattern&0x02 == 0 {
		contextIndex += 2
	}
	bin, err = trial.DecodeBin(&trialContexts[contextIndex])
	if err != nil {
		return 0, err
	}
	pattern |= boolToUint8(bin) << 3

	decoder.commitTrial(&trial, &readerCopy)
	*contexts = trialContexts
	return pattern, nil
}

// DecodeChromaCodedBlockPattern decodes the chroma coded_block_pattern bins using contexts 77-84.
func (decoder *CABACArithmeticDecoder) DecodeChromaCodedBlockPattern(leftCBP, topCBP uint8, contexts *[8]CABACContextModel) (uint8, error) {
	if leftCBP > 2 || topCBP > 2 {
		return 0, ErrCABACChromaCBPOutOfRange
	}
	if contexts == nil {
		return 0, ErrCABACContextState
	}
	for index := range contexts {
		if contexts[index].stateIndex >= 64 {
			return 0, ErrCABACContextState
		}
	}
	readerCopy := *decoder.bits
	trial := *decoder
	trial.bits = &readerCopy
	trialContexts := *contexts
	contextIndex := 0
	if leftCBP > 0 {
		contextIndex++
	}
	if topCBP > 0 {
		contextIndex += 2
	}
	first, err := trial.DecodeBin(&trialContexts[contextIndex])
	if err != nil {
		return 0, err
	}
	pattern := uint8(0)
	if first {
		contextIndex = 4
		if leftCBP == 2 {
			contextIndex++
		}
		if topCBP == 2 {
			contextIndex += 2
		}
		second, err := trial.DecodeBin(&trialContexts[contextIndex])
		if err != nil {
			return 0, err
		}
		pattern = 1 + boolToUint8(second)
	}
	decoder.commitTrial(&trial, &readerCopy)
	*contexts = trialContexts
	return pattern, nil
}

// DecodeLuma4x4CodedBlockFlag decodes a luma 4x4 coded_block_flag using contexts 93-96.
func (decoder *CABACArithmeticDecoder) DecodeLuma4x4CodedBlockFlag(leftNonzero, topNonzero uint8, contexts *[4]CABACContextModel) (bool, error) {
	if leftNonzero > 16 || topNonzero > 16 {
		return false, ErrCABACNonzeroCountRange
	}
	if contexts == nil {
		return false, ErrCABACContextState
	}
	contextIndex := 0
	if leftNonzero > 0 {
		contextIndex++
	}
	if topNonzero > 0 {
		contextIndex += 2
	}
	return decoder.DecodeBin(&contexts[contextIndex])
}

// DecodeLuma4x4SignificanceMap decodes frame-scan significant/last flags for a luma 4x4 block.
// Scan position 15 is implied significant when no earlier last flag terminates the scan.
func (decoder *CABACArithmeticDecoder) DecodeLuma4x4SignificanceMap(significantContexts, lastContexts *[15]CABACContextModel) ([16]bool, error) {
	var significance [16]bool
	if significantContexts == nil || lastContexts == nil {
		return significance, ErrCABACContextState
	}
	for index := range significantContexts {
		if significantContexts[index].stateIndex >= 64 || lastContexts[index].stateIndex >= 64 {
			return significance, ErrCABACContextState
		}
	}
	readerCopy := *decoder.bits
	trial := *decoder
	trial.bits = &readerCopy
	trialSignificantContexts := *significantContexts
	trialLastContexts := *lastContexts
	lastCoefficientFound := false
	for scanIndex := 0; scanIndex < 15; scanIndex++ {
		isSignificant, err := trial.DecodeBin(&trialSignificantContexts[scanIndex])
		if err != nil {
			return significance, err
		}
		if !isSignificant {
			continue
		}
		significance[scanIndex] = true
		isLast, err := trial.DecodeBin(&trialLastContexts[scanIndex])
		if err != nil {
			return [16]bool{}, err
		}
		if isLast {
			lastCoefficientFound = true
			break
		}
	}
	if !lastCoefficientFound {
		significance[15] = true
	}
	decoder.commitTrial(&trial, &readerCopy)
	*significantContexts = trialSignificantContexts
	*lastContexts = trialLastContexts
	return significance, nil
}

func boolToUint8(value bool) uint8 {
	if value {
		return 1
	}
	return 0
}

func (decoder *CABACArithmeticDecoder) commitTrial(trial *CABACArithmeticDecoder, reader *BitReader) {
	decoder.bits.bitOffset = reader.bitOffset
	decoder.codeRange = trial.codeRange
	decoder.codeOffset = trial.codeOffset
	decoder.terminated = trial.terminated
}

// DecodeBypassBin decodes a bypass bin without changing the arithmetic range.
func (decoder *CABACArithmeticDecoder) DecodeBypassBin() (bool, error) {
	if err := decoder.validateBinState(); err != nil {
		return false, err
	}
	startBitOffset := decoder.bits.bitOffset
	bit, err := decoder.bits.ReadBit()
	if err != nil {
		decoder.bits.bitOffset = startBitOffset
		return false, fmt.Errorf("CABAC bypass bin: %w", err)
	}
	codeOffset := decoder.codeOffset<<1 | boolToUint32(bit)
	decoded := codeOffset >= decoder.codeRange
	if decoded {
		codeOffset -= decoder.codeRange
	}
	decoder.codeOffset = codeOffset
	return decoded, nil
}

// DecodeTerminateBin decodes the termination bin; true means the caller must stop decoding.
func (decoder *CABACArithmeticDecoder) DecodeTerminateBin() (bool, error) {
	if err := decoder.validateBinState(); err != nil {
		return false, err
	}
	startBitOffset := decoder.bits.bitOffset
	originalRange, originalOffset := decoder.codeRange, decoder.codeOffset
	decoder.codeRange -= 2
	if decoder.codeOffset >= decoder.codeRange {
		decoder.terminated = true
		return true, nil
	}
	if err := decoder.Renormalize(); err != nil {
		decoder.codeRange, decoder.codeOffset = originalRange, originalOffset
		decoder.bits.bitOffset = startBitOffset
		return false, err
	}
	return false, nil
}

func (decoder *CABACArithmeticDecoder) validateBinState() error {
	if decoder.terminated {
		return ErrCABACTerminated
	}
	if decoder.codeRange < 256 || decoder.codeRange > cabacInitialRange {
		return ErrCABACRangeOutOfRange
	}
	if decoder.codeOffset >= decoder.codeRange {
		return ErrCABACOffsetOutOfRange
	}
	return nil
}

// Renormalize restores the CABAC range invariant without partially updating on truncation.
func (decoder *CABACArithmeticDecoder) Renormalize() error {
	if decoder.terminated {
		return ErrCABACTerminated
	}
	if decoder.codeRange == 0 || decoder.codeRange > cabacInitialRange {
		return ErrCABACRangeOutOfRange
	}
	codeRange, codeOffset := decoder.codeRange, decoder.codeOffset
	startBitOffset := decoder.bits.bitOffset
	for codeRange < 256 {
		bit, err := decoder.bits.ReadBit()
		if err != nil {
			decoder.bits.bitOffset = startBitOffset
			return fmt.Errorf("CABAC renormalization: %w", err)
		}
		codeRange <<= 1
		codeOffset = codeOffset<<1 | boolToUint32(bit)
		if codeOffset >= codeRange {
			decoder.bits.bitOffset = startBitOffset
			return ErrCABACOffsetOutOfRange
		}
	}
	decoder.codeRange, decoder.codeOffset = codeRange, codeOffset
	return nil
}

// DecodeCoeffAbsLevelMinus1 decodes one coefficient level using caller-selected level contexts.
func (decoder *CABACArithmeticDecoder) DecodeCoeffAbsLevelMinus1(firstContext, greaterOneContext *CABACContextModel) (uint32, error) {
	if firstContext == nil || greaterOneContext == nil || firstContext == greaterOneContext || firstContext.stateIndex >= 64 || greaterOneContext.stateIndex >= 64 {
		return 0, ErrCABACContextState
	}
	readerCopy := *decoder.bits
	trial := *decoder
	trial.bits = &readerCopy
	trialFirst, trialGreaterOne := *firstContext, *greaterOneContext
	first, err := trial.DecodeBin(&trialFirst)
	if err != nil {
		return 0, err
	}
	absoluteLevel := uint32(1)
	if first {
		absoluteLevel = 2
		for absoluteLevel < 15 {
			greater, err := trial.DecodeBin(&trialGreaterOne)
			if err != nil {
				return 0, err
			}
			if !greater {
				break
			}
			absoluteLevel++
		}
		if absoluteLevel == 15 {
			prefixLength := uint32(0)
			for {
				bit, err := trial.DecodeBypassBin()
				if err != nil {
					return 0, err
				}
				if !bit {
					break
				}
				prefixLength++
				if prefixLength >= 23 {
					return 0, ErrCABACCoeffLevelOutOfRange
				}
			}
			suffix := uint32(0)
			for range prefixLength {
				bit, err := trial.DecodeBypassBin()
				if err != nil {
					return 0, err
				}
				suffix = suffix<<1 | boolToUint32(bit)
			}
			absoluteLevel = 14 + (1 << prefixLength) + suffix
		}
	}
	decoder.commitTrial(&trial, &readerCopy)
	*firstContext, *greaterOneContext = trialFirst, trialGreaterOne
	return absoluteLevel - 1, nil
}

// DecodeCoeffSign decodes coeff_sign_flag and applies it to coeff_abs_level_minus1.
func (decoder *CABACArithmeticDecoder) DecodeCoeffSign(absLevelMinus1 uint32) (int32, error) {
	if absLevelMinus1 >= uint32(^uint32(0)>>1) {
		return 0, ErrCABACCoeffSignRange
	}
	negative, err := decoder.DecodeBypassBin()
	if err != nil {
		return 0, err
	}
	magnitude := int32(absLevelMinus1 + 1)
	if negative {
		return -magnitude, nil
	}
	return magnitude, nil
}

// DecodeMotionVectorDifference decodes one component using seven caller-selected consecutive contexts.
// neighborMagnitude sums absolute left/top MVD components; contexts start at 40 horizontally or 47 vertically.
func (decoder *CABACArithmeticDecoder) DecodeMotionVectorDifference(neighborMagnitude uint64, contexts *[7]CABACContextModel) (int32, error) {
	if contexts == nil {
		return 0, ErrCABACContextState
	}
	for index := range contexts {
		if contexts[index].stateIndex >= 64 {
			return 0, ErrCABACContextState
		}
	}

	contextIndex := 0
	if neighborMagnitude >= 3 {
		contextIndex++
	}
	if neighborMagnitude >= 33 {
		contextIndex++
	}
	readerCopy := *decoder.bits
	trial := *decoder
	trial.bits = &readerCopy
	trialContexts := *contexts
	greaterThanZero, err := trial.DecodeBin(&trialContexts[contextIndex])
	if err != nil {
		return 0, err
	}
	if !greaterThanZero {
		decoder.commitTrial(&trial, &readerCopy)
		*contexts = trialContexts
		return 0, nil
	}

	absolute := uint32(1)
	contextIndex = 3
	for absolute < 9 {
		greater, err := trial.DecodeBin(&trialContexts[contextIndex])
		if err != nil {
			return 0, err
		}
		if !greater {
			break
		}
		if absolute < 4 {
			contextIndex++
		}
		absolute++
	}
	if absolute >= 9 {
		k := uint32(3)
		for {
			more, err := trial.DecodeBypassBin()
			if err != nil {
				return 0, err
			}
			if !more {
				break
			}
			if k > 30 {
				return 0, ErrCABACMotionVectorDifferenceOutOfRange
			}
			increment := uint32(1) << k
			if absolute > cabacMaxMotionVectorDifference-increment {
				return 0, ErrCABACMotionVectorDifferenceOutOfRange
			}
			absolute += increment
			k++
		}
		if k > 30 {
			return 0, ErrCABACMotionVectorDifferenceOutOfRange
		}
		for k > 0 {
			k--
			bit, err := trial.DecodeBypassBin()
			if err != nil {
				return 0, err
			}
			if bit {
				increment := uint32(1) << k
				if absolute > cabacMaxMotionVectorDifference-increment {
					return 0, ErrCABACMotionVectorDifferenceOutOfRange
				}
				absolute += increment
			}
		}
	}
	negative, err := trial.DecodeBypassBin()
	if err != nil {
		return 0, err
	}
	result := int32(absolute)
	if negative {
		result = -result
	}
	decoder.commitTrial(&trial, &readerCopy)
	*contexts = trialContexts
	return result, nil
}

// DecodeLuma4x4ResidualLevels decodes signed levels in reverse scan order using contexts 227-236.
// Significant flags are frame-scan positions; output levels remain in scan order, not block placement.
func (decoder *CABACArithmeticDecoder) DecodeLuma4x4ResidualLevels(significance *[16]bool, contexts *[10]CABACContextModel) ([16]int32, error) {
	var levels [16]int32
	if significance == nil || contexts == nil {
		return levels, ErrCABACContextState
	}
	if !anySignificantCoefficient(significance) {
		return levels, ErrCABACEmptyResidualBlock
	}
	for _, model := range contexts {
		if model.stateIndex >= 64 {
			return levels, ErrCABACContextState
		}
	}
	readerCopy := *decoder.bits
	trial := *decoder
	trial.bits = &readerCopy
	trialContexts := *contexts
	nodeContext := uint8(0)
	for scanIndex := len(significance) - 1; scanIndex >= 0; scanIndex-- {
		if !significance[scanIndex] {
			continue
		}
		firstContext := &trialContexts[cabacCoeffAbsLevel1Context[nodeContext]]
		greaterOneContext := &trialContexts[cabacCoeffAbsLevelGreater1Context[nodeContext]]
		levelMinus1, err := trial.DecodeCoeffAbsLevelMinus1(firstContext, greaterOneContext)
		if err != nil {
			return levels, err
		}
		level, err := trial.DecodeCoeffSign(levelMinus1)
		if err != nil {
			return levels, err
		}
		levels[scanIndex] = level
		if levelMinus1 == 0 {
			nodeContext = cabacCoeffLevel1Transition[nodeContext]
		} else {
			nodeContext = cabacCoeffLevelGreater1Transition[nodeContext]
		}
	}
	decoder.commitTrial(&trial, &readerCopy)
	*contexts = trialContexts
	return levels, nil
}

// DecodeLuma4x4ResidualBlock decodes and places one coded luma 4x4 residual block.
func (decoder *CABACArithmeticDecoder) DecodeLuma4x4ResidualBlock(significantContexts, lastContexts *[15]CABACContextModel, coefficientContexts *[10]CABACContextModel) ([16]int32, error) {
	var rasterLevels [16]int32
	if significantContexts == nil || lastContexts == nil || coefficientContexts == nil {
		return rasterLevels, ErrCABACContextState
	}
	readerCopy := *decoder.bits
	trial := *decoder
	trial.bits = &readerCopy
	trialSignificantContexts := *significantContexts
	trialLastContexts := *lastContexts
	trialCoefficientContexts := *coefficientContexts
	significance, err := trial.DecodeLuma4x4SignificanceMap(&trialSignificantContexts, &trialLastContexts)
	if err != nil {
		return rasterLevels, err
	}
	scanLevels, err := trial.DecodeLuma4x4ResidualLevels(&significance, &trialCoefficientContexts)
	if err != nil {
		return rasterLevels, err
	}
	rasterLevels = PlaceLuma4x4ScanLevels(scanLevels)
	decoder.commitTrial(&trial, &readerCopy)
	*significantContexts = trialSignificantContexts
	*lastContexts = trialLastContexts
	*coefficientContexts = trialCoefficientContexts
	return rasterLevels, nil
}

func anySignificantCoefficient(significance *[16]bool) bool {
	for _, significant := range significance {
		if significant {
			return true
		}
	}
	return false
}

// PlaceLuma4x4ScanLevels maps frame-scan coefficient levels into 4x4 raster order.
func PlaceLuma4x4ScanLevels(scanLevels [16]int32) [16]int32 {
	scanToRaster := [...]uint8{0, 1, 4, 8, 5, 2, 3, 6, 9, 12, 13, 10, 7, 11, 14, 15}
	var rasterLevels [16]int32
	for scanIndex, rasterIndex := range scanToRaster {
		rasterLevels[rasterIndex] = scanLevels[scanIndex]
	}
	return rasterLevels
}

// PlaceChroma4x4ScanLevels inserts scaled DC and scan-ordered AC levels before inverse scanning.
func PlaceChroma4x4ScanLevels(dcLevel int32, acScanLevels [15]int32) [16]int32 {
	var scanLevels [16]int32
	scanLevels[0] = dcLevel
	copy(scanLevels[1:], acScanLevels[:])
	return PlaceLuma4x4ScanLevels(scanLevels)
}

func boolToUint32(value bool) uint32 {
	if value {
		return 1
	}
	return 0
}
