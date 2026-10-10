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
	ErrCABACIntraPredNeighbors   = errors.New("CABAC intra4x4 prediction neighbors are invalid")
	ErrCABACCBPOutOfRange        = errors.New("CABAC luma coded_block_pattern is outside [0,15]")
	ErrCABACChromaCBPOutOfRange  = errors.New("CABAC chroma coded_block_pattern is outside [0,2]")
	ErrCABACNonzeroCountRange    = errors.New("CABAC luma 4x4 nonzero count is outside [0,16]")
	ErrCABACCoeffLevelOutOfRange = errors.New("CABAC coefficient level exceeds the supported bypass prefix")
	ErrCABACCoeffSignRange       = errors.New("CABAC coefficient magnitude exceeds the signed output range")
	ErrCABACEmptyResidualBlock   = errors.New("CABAC residual block has no significant coefficients")
	ErrCABACTerminated           = errors.New("CABAC decoder is already terminated")
	ErrCABACPCMAlignment         = errors.New("I_PCM alignment bit is not zero")
)

var ErrCABACMotionVectorDifferenceOutOfRange = errors.New("CABAC motion-vector difference exceeds the signed output range")

const cabacInitialRange uint32 = 510
const cabacMaxQPY uint32 = 51
const cabacMaxMotionVectorDifference uint32 = uint32(^uint32(0) >> 1)

var cabacIIntraMBTypeInit = [8][2]int{
	{20, -15}, {2, 54}, {3, 74}, {-28, 127},
	{-23, 104}, {-6, 53}, {-1, 54}, {7, 51},
}
var cabacPInterMBTypeInit = [3][4][2]int{
	{{1, 9}, {0, 49}, {-37, 118}, {5, 57}},
	{{-2, 9}, {4, 41}, {-29, 118}, {2, 65}},
	{{-10, 51}, {-3, 62}, {-27, 99}, {26, 16}},
}
var cabacInterMVDInit = [3][14][2]int{
	{{-3, 69}, {-6, 81}, {-11, 96}, {6, 55}, {7, 67}, {-5, 86}, {2, 88}, {0, 58}, {-3, 76}, {-10, 94}, {5, 54}, {4, 69}, {-3, 81}, {0, 88}},
	{{-2, 69}, {-5, 82}, {-10, 96}, {2, 59}, {2, 75}, {-3, 87}, {-3, 100}, {1, 56}, {-3, 74}, {-6, 85}, {0, 59}, {-3, 81}, {-7, 86}, {-5, 95}},
	{{-11, 89}, {-15, 103}, {-21, 116}, {19, 57}, {20, 58}, {4, 84}, {6, 96}, {1, 63}, {-5, 85}, {-13, 106}, {5, 63}, {6, 75}, {-3, 90}, {-1, 101}},
}
var cabacInterRefIdxInit = [3][6][2]int{
	{{-7, 67}, {-5, 74}, {-4, 74}, {-5, 80}, {-7, 72}, {1, 58}},
	{{-1, 66}, {-1, 77}, {1, 70}, {-2, 86}, {-5, 72}, {0, 61}},
	{{3, 55}, {-4, 79}, {-2, 75}, {-12, 97}, {-7, 50}, {1, 60}},
}
var cabacIMBQPDeltaInit = [4][2]int{{0, 41}, {0, 63}, {0, 63}, {0, 63}}
var cabacIIntraChromaPredModeInit = [4][2]int{{-9, 83}, {4, 86}, {0, 97}, {-7, 72}}
var cabacIIntra4x4PredModeInit = [2][2]int{{13, 41}, {3, 62}}
var cabacITransformSize8x8Init = [3][2]int{{31, 21}, {31, 31}, {25, 50}}
var cabacILumaCodedBlockPatternInit = [4][2]int{{-17, 127}, {-13, 102}, {0, 82}, {-7, 74}}
var cabacIChromaCodedBlockPatternInit = [8][2]int{{-21, 107}, {-27, 127}, {-31, 127}, {-24, 127}, {-18, 95}, {-27, 127}, {-21, 114}, {-30, 127}}
var cabacILuma4x4CodedBlockFlagInit = [4][2]int{{-3, 70}, {-8, 93}, {-10, 90}, {-30, 127}}

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

// NewCABACIIntraMBTypeContexts initializes ctxIdx 3-10 from H.264 Table 9-12.
func NewCABACIIntraMBTypeContexts(sliceQPY int) ([8]CABACContextModel, error) {
	var contexts [8]CABACContextModel
	for index, values := range cabacIIntraMBTypeInit {
		model, err := NewCABACContextModel(values[0], values[1], sliceQPY)
		if err != nil {
			return [8]CABACContextModel{}, err
		}
		contexts[index] = *model
	}
	return contexts, nil
}

// NewCABACPInterMBTypeContexts initializes ctxIdx 14-17 from H.264 Table 9-13.
func NewCABACPInterMBTypeContexts(cabacInitIDC uint8, sliceQPY int) ([4]CABACContextModel, error) {
	if cabacInitIDC > 2 {
		return [4]CABACContextModel{}, ErrCABACContextInitRange
	}
	var contexts [4]CABACContextModel
	for index, values := range cabacPInterMBTypeInit[cabacInitIDC] {
		model, err := NewCABACContextModel(values[0], values[1], sliceQPY)
		if err != nil {
			return [4]CABACContextModel{}, err
		}
		contexts[index] = *model
	}
	return contexts, nil
}

// NewCABACInterPredictionContexts initializes MVD contexts 40-53 and the ref_idx contexts 54-59
// that ref_idx_l0 and ref_idx_l1 share (Table 9-34 assigns both ctxIdxOffset 54).
func NewCABACInterPredictionContexts(cabacInitIDC uint8, sliceQPY int) ([7]CABACContextModel, [7]CABACContextModel, [6]CABACContextModel, error) {
	var mvdX, mvdY [7]CABACContextModel
	var refIdx [6]CABACContextModel
	if cabacInitIDC > 2 {
		return mvdX, mvdY, refIdx, ErrCABACContextInitRange
	}
	for index := range mvdX {
		model, err := NewCABACContextModel(cabacInterMVDInit[cabacInitIDC][index][0], cabacInterMVDInit[cabacInitIDC][index][1], sliceQPY)
		if err != nil {
			return [7]CABACContextModel{}, [7]CABACContextModel{}, [6]CABACContextModel{}, err
		}
		mvdX[index] = *model
		model, err = NewCABACContextModel(cabacInterMVDInit[cabacInitIDC][index+7][0], cabacInterMVDInit[cabacInitIDC][index+7][1], sliceQPY)
		if err != nil {
			return [7]CABACContextModel{}, [7]CABACContextModel{}, [6]CABACContextModel{}, err
		}
		mvdY[index] = *model
	}
	for index, parameters := range cabacInterRefIdxInit[cabacInitIDC] {
		model, err := NewCABACContextModel(parameters[0], parameters[1], sliceQPY)
		if err != nil {
			return [7]CABACContextModel{}, [7]CABACContextModel{}, [6]CABACContextModel{}, err
		}
		refIdx[index] = *model
	}
	return mvdX, mvdY, refIdx, nil
}

// NewCABACIMBQPDeltaContexts initializes ctxIdx 60-63 from H.264 Table 9-17.
func NewCABACIMBQPDeltaContexts(sliceQPY int) ([4]CABACContextModel, error) {
	var contexts [4]CABACContextModel
	for index, values := range cabacIMBQPDeltaInit {
		model, err := NewCABACContextModel(values[0], values[1], sliceQPY)
		if err != nil {
			return [4]CABACContextModel{}, err
		}
		contexts[index] = *model
	}
	return contexts, nil
}

// NewCABACIIntraChromaPredModeContexts initializes ctxIdx 64-67 from H.264 Table 9-17.
func NewCABACIIntraChromaPredModeContexts(sliceQPY int) ([4]CABACContextModel, error) {
	var contexts [4]CABACContextModel
	for index, values := range cabacIIntraChromaPredModeInit {
		model, err := NewCABACContextModel(values[0], values[1], sliceQPY)
		if err != nil {
			return [4]CABACContextModel{}, err
		}
		contexts[index] = *model
	}
	return contexts, nil
}

// NewCABACIIntra4x4PredModeContexts initializes ctxIdx 68-69 from H.264 Table 9-17.
func NewCABACIIntra4x4PredModeContexts(sliceQPY int) ([2]CABACContextModel, error) {
	var contexts [2]CABACContextModel
	for index, values := range cabacIIntra4x4PredModeInit {
		model, err := NewCABACContextModel(values[0], values[1], sliceQPY)
		if err != nil {
			return [2]CABACContextModel{}, err
		}
		contexts[index] = *model
	}
	return contexts, nil
}

// NewCABACITransformSize8x8Contexts initializes ctxIdx 399-401 from H.264 Table 9-16.
func NewCABACITransformSize8x8Contexts(sliceQPY int) ([3]CABACContextModel, error) {
	var contexts [3]CABACContextModel
	for index, values := range cabacITransformSize8x8Init {
		model, err := NewCABACContextModel(values[0], values[1], sliceQPY)
		if err != nil {
			return [3]CABACContextModel{}, err
		}
		contexts[index] = *model
	}
	return contexts, nil
}

// NewCABACILumaCodedBlockPatternContexts initializes ctxIdx 73-76 from H.264 Table 9-18.
func NewCABACILumaCodedBlockPatternContexts(sliceQPY int) ([4]CABACContextModel, error) {
	var contexts [4]CABACContextModel
	for index, values := range cabacILumaCodedBlockPatternInit {
		model, err := NewCABACContextModel(values[0], values[1], sliceQPY)
		if err != nil {
			return [4]CABACContextModel{}, err
		}
		contexts[index] = *model
	}
	return contexts, nil
}

// NewCABACIChromaCodedBlockPatternContexts initializes ctxIdx 77-84 from H.264 Table 9-18.
func NewCABACIChromaCodedBlockPatternContexts(sliceQPY int) ([8]CABACContextModel, error) {
	var contexts [8]CABACContextModel
	for index, values := range cabacIChromaCodedBlockPatternInit {
		model, err := NewCABACContextModel(values[0], values[1], sliceQPY)
		if err != nil {
			return [8]CABACContextModel{}, err
		}
		contexts[index] = *model
	}
	return contexts, nil
}

// NewCABACILuma4x4CodedBlockFlagContexts initializes ctxIdx 93-96 from H.264 Table 9-18.
func NewCABACILuma4x4CodedBlockFlagContexts(sliceQPY int) ([4]CABACContextModel, error) {
	var contexts [4]CABACContextModel
	for index, values := range cabacILuma4x4CodedBlockFlagInit {
		model, err := NewCABACContextModel(values[0], values[1], sliceQPY)
		if err != nil {
			return [4]CABACContextModel{}, err
		}
		contexts[index] = *model
	}
	return contexts, nil
}

// CABACContextCount covers ctxIdx 0-459, every context used by 4:2:0 CABAC slices.
const CABACContextCount = 460

// NewCABACSliceContexts initializes ctxIdx 0-459 for one slice from Tables 9-12 to 9-24.
// I/SI slices ignore cabacInitIDC; entries unused by the slice type initialize from (0, 0).
func NewCABACSliceContexts(sliceType, cabacInitIDC uint8, sliceQPY int) ([CABACContextCount]CABACContextModel, error) {
	var contexts [CABACContextCount]CABACContextModel
	if sliceType > 9 {
		return contexts, ErrCABACUnsupportedSyntax
	}
	column := 0
	if kind := sliceType % 5; kind != 2 && kind != 4 {
		if cabacInitIDC > 2 {
			return contexts, ErrCABACContextInitRange
		}
		column = int(cabacInitIDC) + 1
	}
	for index, values := range cabacContextInitTable[column] {
		model, err := NewCABACContextModel(int(values[0]), int(values[1]), sliceQPY)
		if err != nil {
			return [CABACContextCount]CABACContextModel{}, err
		}
		contexts[index] = *model
	}
	return contexts, nil
}

// CABACResidualContextBases holds the first frame-coded ctxIdx of each residual syntax element.
// CodedBlockFlag is -1 for ctxBlockCat 5, whose flag is inferred when ChromaArrayType != 3.
type CABACResidualContextBases struct {
	CodedBlockFlag int
	Significant    int
	Last           int
	AbsLevel       int
}

// CABACResidualContextBasesForCategory adds Table 9-40 ctxIdxBlockCatOffset to the Table 9-34 offsets.
func CABACResidualContextBasesForCategory(ctxBlockCat uint8) (CABACResidualContextBases, error) {
	switch {
	case ctxBlockCat < 5:
		return CABACResidualContextBases{
			CodedBlockFlag: 85 + [5]int{0, 4, 8, 12, 16}[ctxBlockCat],
			Significant:    105 + [5]int{0, 15, 29, 44, 47}[ctxBlockCat],
			Last:           166 + [5]int{0, 15, 29, 44, 47}[ctxBlockCat],
			AbsLevel:       227 + [5]int{0, 10, 20, 30, 39}[ctxBlockCat],
		}, nil
	case ctxBlockCat == 5:
		return CABACResidualContextBases{CodedBlockFlag: -1, Significant: 402, Last: 417, AbsLevel: 426}, nil
	}
	return CABACResidualContextBases{}, ErrCABACUnsupportedSyntax
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

// CABACInterNeighbor contains the parsed neighbour facts needed by clauses 9.3.3.1.1.6 and 9.3.3.1.1.7.
type CABACInterNeighbor struct {
	Available              bool
	Skip                   bool
	Intra                  bool
	PredictionModeMatches  bool
	ReferenceIndex         uint8
	MotionVectorDifference [2]int32
	IsField                bool
}

// DeriveCABACReferenceIndexContextIncrement derives ctxIdxInc 0-3 for ref_idx_lX.
func DeriveCABACReferenceIndexContextIncrement(left, top CABACInterNeighbor, mbaffFrame, currentIsField bool) uint8 {
	contextIncrement := uint8(0)
	for index, neighbor := range [2]CABACInterNeighbor{left, top} {
		if !neighbor.Available || neighbor.Skip || neighbor.Intra || !neighbor.PredictionModeMatches {
			continue
		}
		zeroThreshold := uint8(0)
		if mbaffFrame && !currentIsField && neighbor.IsField {
			zeroThreshold = 1
		}
		if neighbor.ReferenceIndex > zeroThreshold {
			if index == 0 {
				contextIncrement |= 1
			} else {
				contextIncrement |= 2
			}
		}
	}
	return contextIncrement
}

// DeriveCABACMotionVectorDifferenceContextIncrement derives the 0-2 increment for one MVD component.
func DeriveCABACMotionVectorDifferenceContextIncrement(left, top CABACInterNeighbor, component uint8, mbaffFrame, currentIsField bool) (uint8, error) {
	if component > 1 {
		return 0, ErrCABACContextState
	}
	leftAbs := cabacMVDNeighborMagnitude(left, component, mbaffFrame, currentIsField)
	topAbs := cabacMVDNeighborMagnitude(top, component, mbaffFrame, currentIsField)
	if leftAbs > 32 || topAbs > 32 || leftAbs+topAbs > 32 {
		return 2, nil
	}
	if leftAbs+topAbs > 2 {
		return 1, nil
	}
	return 0, nil
}

func cabacMVDNeighborMagnitude(neighbor CABACInterNeighbor, component uint8, mbaffFrame, currentIsField bool) uint64 {
	if !neighbor.Available || neighbor.Skip || neighbor.Intra || !neighbor.PredictionModeMatches {
		return 0
	}
	value := int64(neighbor.MotionVectorDifference[component])
	if value < 0 {
		value = -value
	}
	if component == 1 && mbaffFrame {
		if !currentIsField && neighbor.IsField {
			value *= 2
		} else if currentIsField && !neighbor.IsField {
			value /= 2
		}
	}
	return uint64(value)
}

// DecodeReferenceIndexForPartition derives the neighbour context before decoding ref_idx_lX.
func (decoder *CABACArithmeticDecoder) DecodeReferenceIndexForPartition(maxRefIdxMinus1 uint32, left, top CABACInterNeighbor, mbaffFrame, currentIsField bool, contexts *[6]CABACContextModel) (uint8, error) {
	contextIncrement := DeriveCABACReferenceIndexContextIncrement(left, top, mbaffFrame, currentIsField)
	return decoder.DecodeReferenceIndex(maxRefIdxMinus1, contextIncrement, contexts)
}

// DecodeMotionVectorDifferenceForPartition derives absMvdComp from the A/B partitions before decoding one component.
func (decoder *CABACArithmeticDecoder) DecodeMotionVectorDifferenceForPartition(component uint8, left, top CABACInterNeighbor, mbaffFrame, currentIsField bool, contexts *[7]CABACContextModel) (int32, error) {
	contextIncrement, err := DeriveCABACMotionVectorDifferenceContextIncrement(left, top, component, mbaffFrame, currentIsField)
	if err != nil {
		return 0, err
	}
	return decoder.decodeMotionVectorDifferenceWithContext(contextIncrement, contexts)
}

// DecodeReferenceIndex decodes ref_idx_l0/l1 truncated unary syntax.
// contexts represent six consecutive models from ctxIdxOffset; neighborContextInc is derived per clause 9.3.3.1.1.6.
func (decoder *CABACArithmeticDecoder) DecodeReferenceIndex(maxRefIdxMinus1 uint32, neighborContextInc uint8, contexts *[6]CABACContextModel) (uint8, error) {
	if contexts == nil || neighborContextInc > 3 {
		return 0, ErrCABACContextState
	}
	for _, context := range contexts {
		if context.stateIndex >= 64 {
			return 0, ErrCABACContextState
		}
	}
	if maxRefIdxMinus1 > 31 {
		return 0, ErrCABACUnsupportedSyntax
	}
	if maxRefIdxMinus1 == 0 {
		return 0, nil
	}
	readerCopy := *decoder.bits
	trial := *decoder
	trial.bits = &readerCopy
	trialContexts := *contexts
	first, err := trial.DecodeBin(&trialContexts[neighborContextInc])
	if err != nil {
		return 0, err
	}
	refIdx := uint8(0)
	if first {
		refIdx = 1
		for uint32(refIdx) < maxRefIdxMinus1 {
			contextIndex := 5
			if refIdx == 1 {
				contextIndex = 4
			}
			continued, err := trial.DecodeBin(&trialContexts[contextIndex])
			if err != nil {
				return 0, err
			}
			if !continued {
				break
			}
			refIdx++
		}
	}
	decoder.commitTrial(&trial, &readerCopy)
	*contexts = trialContexts
	return refIdx, nil
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

// DecodeIPCMIntraMacroblock decodes an 8-bit 4:2:0 I_PCM macroblock and places it into the frame builder.
// The arithmetic engine is reinitialized after the raw PCM samples as required by clause 9.3.1.2.
func (decoder *CABACArithmeticDecoder) DecodeIPCMIntraMacroblock(sliceType uint8, contexts *[8]CABACContextModel, leftAvailable, leftIntra16OrPCM, topAvailable, topIntra16OrPCM bool, builder *Yuv420FrameBuilder, address int) error {
	if decoder == nil || decoder.bits == nil || contexts == nil || builder == nil {
		return ErrCABACContextState
	}
	readerCopy := *decoder.bits
	trial := *decoder
	trial.bits = &readerCopy
	trialContexts := *contexts
	mbType, err := trial.DecodeIIntraMBType(sliceType, &trialContexts, leftAvailable, leftIntra16OrPCM, topAvailable, topIntra16OrPCM)
	if err != nil {
		return err
	}
	if mbType != 25 {
		return ErrCABACUnsupportedSyntax
	}

	for readerCopy.bitOffset%8 != 0 {
		zero, err := trial.bits.ReadBit()
		if err != nil {
			return fmt.Errorf("I_PCM alignment: %w", err)
		}
		if zero {
			return ErrCABACPCMAlignment
		}
	}
	var yBlock [256]uint8
	var uBlock, vBlock [64]uint8
	for index := range yBlock {
		sample, err := trial.bits.ReadBits(8)
		if err != nil {
			return fmt.Errorf("I_PCM luma samples: %w", err)
		}
		yBlock[index] = uint8(sample)
	}
	for index := range uBlock {
		sample, err := trial.bits.ReadBits(8)
		if err != nil {
			return fmt.Errorf("I_PCM Cb samples: %w", err)
		}
		uBlock[index] = uint8(sample)
	}
	for index := range vBlock {
		sample, err := trial.bits.ReadBits(8)
		if err != nil {
			return fmt.Errorf("I_PCM Cr samples: %w", err)
		}
		vBlock[index] = uint8(sample)
	}
	offset, err := trial.bits.ReadBits(9)
	if err != nil {
		return fmt.Errorf("I_PCM CABAC reinitialization: %w", err)
	}
	if offset >= cabacInitialRange {
		return ErrCABACOffsetOutOfRange
	}
	if err := builder.PlaceMacroblock(address, yBlock, uBlock, vBlock); err != nil {
		return err
	}
	trial.codeRange = cabacInitialRange
	trial.codeOffset = offset
	trial.terminated = false
	decoder.commitTrial(&trial, &readerCopy)
	*contexts = trialContexts
	return nil
}

// CABACIntra4x4EdgeState supplies the coded-block facts for one external macroblock edge.
type CABACIntra4x4EdgeState struct {
	Available               bool
	IsIPCM                  bool
	TransformBlockAvailable [4]bool
	TransformBlockCoded     [4]bool
}

// Intra4x4LumaMacroblockResult contains syntax-order modes/flags and a raster-order luma block.
type Intra4x4LumaMacroblockResult struct {
	Modes           [16]uint8
	CodedBlockFlags [16]bool
	Residuals       [16][16]int64
	Samples         [256]uint8
}

type Intra8x8LumaMacroblockResult struct {
	TransformSize8x8 bool
	Modes            [4]uint8
	Residuals        [4][64]int64
	Samples          [256]uint8
}

type CABACIntra16x16EdgeState struct {
	Available                 bool
	IsIPCM                    bool
	DCTransformBlockAvailable bool
	DCTransformBlockCoded     bool
	ACTransformBlockAvailable [4]bool
	ACTransformBlockCoded     [4]bool
}

type Intra16x16LumaMacroblockResult struct {
	PredictionMode        uint8
	CodedBlockPatternLuma uint8
	DCCoded               bool
	DCLevels              [16]int64
	ACCodedBlockFlags     [16]bool
	Residual              [256]int64
	Samples               [256]uint8
}

type CABACChroma420References struct {
	TopAvailable     bool
	LeftAvailable    bool
	TopLeftAvailable bool
	Top              [8]uint8
	Left             [8]uint8
	TopLeft          uint8
}

// AC edge entries are ordered top-to-bottom for a left edge and left-to-right for a top edge.
type CABACChroma420EdgeState struct {
	Available        bool
	IsIPCM           bool
	DCBlockAvailable bool
	DCBlockCoded     bool
	ACBlockAvailable [2]bool
	ACBlockCoded     [2]bool
}

type IntraChroma420MacroblockResult struct {
	PredictionMode    uint8
	QPC               [2]int
	DCCoded           [2]bool
	ACCodedBlockFlags [2][4]bool
	Cb                [64]uint8
	Cr                [64]uint8
	CbResidual        [64]int64
	CrResidual        [64]int64
}

type CABACIIntraMacroblockInput struct {
	SliceType                  uint8
	LeftAvailable              bool
	LeftIntra16OrPCM           bool
	TopAvailable               bool
	TopIntra16OrPCM            bool
	PreviousQPY                int
	PreviousQPDelta            int
	LeftLumaCBP                uint8
	TopLumaCBP                 uint8
	LeftChromaCBP              uint8
	TopChromaCBP               uint8
	Transform8x8ModeEnabled    bool
	LeftHas8x8Transform        bool
	TopHas8x8Transform         bool
	TopModes4x4                [4]uint8
	LeftModes4x4               [4]uint8
	TopModes8x8                [2]uint8
	LeftModes8x8               [2]uint8
	TopModeAvailable           bool
	LeftModeAvailable          bool
	Luma4x4TopEdge             CABACIntra4x4EdgeState
	Luma4x4LeftEdge            CABACIntra4x4EdgeState
	Luma16x16TopEdge           CABACIntra16x16EdgeState
	Luma16x16LeftEdge          CABACIntra16x16EdgeState
	Luma4x4Blocks              [16]LumaIntra4x4Block
	Luma8x8Blocks              [4]LumaIntra8x8Block
	Intra16x16Top              [16]uint8
	Intra16x16Left             [16]uint8
	Intra16x16TopAvailable     bool
	Intra16x16LeftAvailable    bool
	Intra16x16TopLeft          uint8
	Intra16x16TopLeftAvailable bool
	ChromaLeftModeNonzero      bool
	ChromaTopModeNonzero       bool
	ChromaReferences           [2]CABACChroma420References
	ChromaLeftEdges            [2]CABACChroma420EdgeState
	ChromaTopEdges             [2]CABACChroma420EdgeState
	Luma4x4ScalingList         [16]uint8
	Luma8x8ScalingList         [64]uint8
	ChromaScalingLists         [2][16]uint8
	ChromaQPIndexOffsets       [2]int
}

type CABACIIntraMacroblockResult struct {
	MacroblockType          uint8
	CodedBlockPatternLuma   uint8
	CodedBlockPatternChroma uint8
	TransformSize8x8        bool
	Intra16x16LumaMode      uint8
	ChromaPredictionMode    uint8
	Luma4x4Modes            [16]uint8
	Luma8x8Modes            [4]uint8
	QPDelta                 int
	QPY                     int
	Luma                    [256]uint8
	Cb                      [64]uint8
	Cr                      [64]uint8
	Luma4x4CodedBlockFlags  [16]bool
	ChromaDCCoded           [2]bool
	ChromaACCodedFlags      [2][4]bool
}

var luma4x4BlockScanToRaster = [16]uint8{0, 1, 4, 5, 2, 3, 6, 7, 8, 9, 12, 13, 10, 11, 14, 15}

// DecodeIntraNxN4x4LumaMacroblock decodes Intra_4x4 modes and luma residuals for an I_NxN macroblock.
// Neighbor samples in blocks are caller-gathered; top/left modes and edge CBF state describe external neighbors.
func (decoder *CABACArithmeticDecoder) DecodeIntraNxN4x4LumaMacroblock(contexts *[CABACContextCount]CABACContextModel, codedBlockPatternLuma uint8, qpy int, scalingList [16]uint8, topModes, leftModes [4]uint8, topModeAvailable, leftModeAvailable bool, topEdge, leftEdge CABACIntra4x4EdgeState, blocks [16]LumaIntra4x4Block) (Intra4x4LumaMacroblockResult, error) {
	var result Intra4x4LumaMacroblockResult
	if contexts == nil || codedBlockPatternLuma > 15 {
		return result, ErrCABACContextState
	}
	readerCopy := *decoder.bits
	trial := *decoder
	trial.bits = &readerCopy
	trialContexts := *contexts
	var modeContexts [2]CABACContextModel
	copy(modeContexts[:], trialContexts[68:70])
	modes, err := trial.DecodeIntra4x4PredModes(&modeContexts, topModes, leftModes, topModeAvailable, leftModeAvailable)
	if err != nil {
		return Intra4x4LumaMacroblockResult{}, err
	}
	copy(trialContexts[68:70], modeContexts[:])
	result.Modes = modes
	for blockIndex := range blocks {
		rasterIndex := int(luma4x4BlockScanToRaster[blockIndex])
		blockX, blockY := rasterIndex%4, rasterIndex/4
		blocks[blockIndex].Mode = modes[blockIndex]
		cbpIndex := blockY/2*2 + blockX/2
		if codedBlockPatternLuma&(1<<cbpIndex) == 0 {
			blocks[blockIndex].Residual = [16]int64{}
			continue
		}
		leftCond := deriveIntra4x4LumaCondTerm(blockIndex, blockX, blockY, true, result.CodedBlockFlags, leftEdge, true)
		topCond := deriveIntra4x4LumaCondTerm(blockIndex, blockX, blockY, false, result.CodedBlockFlags, topEdge, true)
		levels, coded, err := trial.DecodeResidualBlock(0, leftCond, topCond, &trialContexts)
		if err != nil {
			return Intra4x4LumaMacroblockResult{}, err
		}
		result.CodedBlockFlags[blockIndex] = coded
		if !coded {
			blocks[blockIndex].Residual = [16]int64{}
			continue
		}
		rasterLevels := PlaceLuma4x4ScanLevels(levels)
		residual, err := reconstructLuma4x4Residual(rasterLevels, scalingList, qpy)
		if err != nil {
			return Intra4x4LumaMacroblockResult{}, err
		}
		blocks[blockIndex].Residual = residual
		result.Residuals[blockIndex] = residual
	}
	result.Samples, err = ReconstructLumaIntra4x4Macroblock(blocks)
	if err != nil {
		return Intra4x4LumaMacroblockResult{}, err
	}
	decoder.commitTrial(&trial, &readerCopy)
	*contexts = trialContexts
	return result, nil
}

var luma8x8ScanToRaster = [64]uint8{
	0, 1, 8, 16, 9, 2, 3, 10, 17, 24, 32, 25, 18, 11, 4, 5,
	12, 19, 26, 33, 40, 48, 41, 34, 27, 20, 13, 6, 7, 14, 21, 28,
	35, 42, 49, 56, 57, 50, 43, 36, 29, 22, 15, 23, 30, 37, 44, 51,
	58, 59, 52, 45, 38, 31, 39, 46, 53, 60, 61, 54, 47, 55, 62, 63,
}

func PlaceLuma8x8ScanLevels(scanLevels [64]int32) (rasterLevels [64]int32) {
	for scanIndex, rasterIndex := range luma8x8ScanToRaster {
		rasterLevels[rasterIndex] = scanLevels[scanIndex]
	}
	return rasterLevels
}

// DecodeIntraNxN8x8LumaMacroblock handles I_NxN macroblocks whose CABAC transform flag selects 8x8.
// Sample references in blocks must already be gathered and filtered per clause 8.3.2.2.1.
func (decoder *CABACArithmeticDecoder) DecodeIntraNxN8x8LumaMacroblock(contexts *[CABACContextCount]CABACContextModel, transform8x8ModeEnabled, leftHas8x8Transform, topHas8x8Transform bool, codedBlockPatternLuma uint8, qpy int, scalingList [64]uint8, topModes, leftModes [2]uint8, topModeAvailable, leftModeAvailable bool, blocks [4]LumaIntra8x8Block) (Intra8x8LumaMacroblockResult, error) {
	var result Intra8x8LumaMacroblockResult
	if contexts == nil || codedBlockPatternLuma > 15 {
		return result, ErrCABACContextState
	}
	if !transform8x8ModeEnabled {
		return result, ErrCABACUnsupportedSyntax
	}
	if _, err := inverseScaleLuma8x8([64]int32{}, scalingList, qpy); err != nil {
		return result, err
	}
	readerCopy := *decoder.bits
	trial := *decoder
	trial.bits = &readerCopy
	trialContexts := *contexts
	var transformContexts [3]CABACContextModel
	copy(transformContexts[:], trialContexts[399:402])
	transform8x8, err := trial.DecodeTransformSize8x8Flag(&transformContexts, leftHas8x8Transform, topHas8x8Transform)
	if err != nil {
		return Intra8x8LumaMacroblockResult{}, err
	}
	copy(trialContexts[399:402], transformContexts[:])
	if !transform8x8 {
		return Intra8x8LumaMacroblockResult{}, ErrCABACUnsupportedSyntax
	}
	var modeContexts [2]CABACContextModel
	copy(modeContexts[:], trialContexts[68:70])
	for blockIndex := 0; blockIndex < 4; blockIndex++ {
		blockX, blockY := blockIndex%2, blockIndex/2
		leftAvailable, topAvailable := leftModeAvailable, topModeAvailable
		leftMode, topMode := leftModes[blockY], topModes[blockX]
		if blockX > 0 {
			leftAvailable = true
			leftMode = result.Modes[blockIndex-1]
		}
		if blockY > 0 {
			topAvailable = true
			topMode = result.Modes[blockIndex-2]
		}
		predictedMode := uint8(2)
		if leftAvailable && topAvailable {
			predictedMode = min(leftMode, topMode)
		}
		mode, err := trial.DecodeIntra4x4PredMode(predictedMode, &modeContexts)
		if err != nil {
			return Intra8x8LumaMacroblockResult{}, err
		}
		result.Modes[blockIndex] = mode
	}
	copy(trialContexts[68:70], modeContexts[:])
	for blockIndex := range blocks {
		blocks[blockIndex].Mode = result.Modes[blockIndex]
		if codedBlockPatternLuma&(1<<blockIndex) == 0 {
			blocks[blockIndex].Residual = [64]int64{}
			continue
		}
		levels, err := trial.DecodeLuma8x8ResidualBlock(&trialContexts)
		if err != nil {
			return Intra8x8LumaMacroblockResult{}, err
		}
		rasterLevels := PlaceLuma8x8ScanLevels(levels)
		coefficients, err := inverseScaleLuma8x8(rasterLevels, scalingList, qpy)
		if err != nil {
			return Intra8x8LumaMacroblockResult{}, err
		}
		blocks[blockIndex].Residual = inverseTransformLuma8x8(coefficients)
		result.Residuals[blockIndex] = blocks[blockIndex].Residual
	}
	result.Samples, err = ReconstructLumaIntra8x8Macroblock(blocks)
	if err != nil {
		return Intra8x8LumaMacroblockResult{}, err
	}
	result.TransformSize8x8 = true
	decoder.commitTrial(&trial, &readerCopy)
	*contexts = trialContexts
	return result, nil
}

// DecodeIntra16x16LumaMacroblock decodes I16x16 luma DC/AC residuals and reconstructs its luma plane.
// The I16x16 mb_type and QPY are supplied by the enclosing macroblock-layer parser.
func (decoder *CABACArithmeticDecoder) DecodeIntra16x16LumaMacroblock(mbType uint8, contexts *[CABACContextCount]CABACContextModel, qpy int, scalingList [16]uint8, top, left *[16]uint8, topLeft uint8, topLeftAvailable bool, topEdge, leftEdge CABACIntra16x16EdgeState) (Intra16x16LumaMacroblockResult, error) {
	var result Intra16x16LumaMacroblockResult
	if contexts == nil || mbType < 1 || mbType > 24 {
		return result, ErrCABACUnsupportedSyntax
	}
	if _, err := reconstructIntra16x16LumaDC([16]int32{}, scalingList, qpy); err != nil {
		return result, err
	}
	readerCopy := *decoder.bits
	trial := *decoder
	trial.bits = &readerCopy
	trialContexts := *contexts
	result.PredictionMode = (mbType - 1) % 4
	if mbType >= 13 {
		result.CodedBlockPatternLuma = 15
	}

	dcCondLeft := DeriveCABACCodedBlockFlagCondTerm(leftEdge.Available, true, leftEdge.IsIPCM, leftEdge.DCTransformBlockAvailable, leftEdge.DCTransformBlockCoded)
	dcCondTop := DeriveCABACCodedBlockFlagCondTerm(topEdge.Available, true, topEdge.IsIPCM, topEdge.DCTransformBlockAvailable, topEdge.DCTransformBlockCoded)
	dcScanLevels, dcCoded, err := trial.DecodeResidualBlock(2, dcCondLeft, dcCondTop, &trialContexts)
	if err != nil {
		return Intra16x16LumaMacroblockResult{}, err
	}
	result.DCCoded = dcCoded
	if dcCoded {
		dcRasterLevels := PlaceLuma4x4ScanLevels(dcScanLevels)
		result.DCLevels, err = reconstructIntra16x16LumaDC(dcRasterLevels, scalingList, qpy)
		if err != nil {
			return Intra16x16LumaMacroblockResult{}, err
		}
	}

	for blockIndex := 0; blockIndex < 16; blockIndex++ {
		rasterIndex := int(luma4x4BlockScanToRaster[blockIndex])
		blockX, blockY := rasterIndex%4, rasterIndex/4
		if result.CodedBlockPatternLuma&(1<<((blockY/2)*2+blockX/2)) != 0 {
			condLeft := deriveIntra16x16ACCondTerm(blockIndex, blockX, blockY, true, result.ACCodedBlockFlags, leftEdge)
			condTop := deriveIntra16x16ACCondTerm(blockIndex, blockX, blockY, false, result.ACCodedBlockFlags, topEdge)
			acLevels, coded, err := trial.DecodeResidualBlock(1, condLeft, condTop, &trialContexts)
			if err != nil {
				return Intra16x16LumaMacroblockResult{}, err
			}
			result.ACCodedBlockFlags[blockIndex] = coded
			if coded {
				var scanLevels [16]int32
				copy(scanLevels[1:], acLevels[:15])
				rasterLevels := PlaceLuma4x4ScanLevels(scanLevels)
				rasterLevels[0] = 0
				coefficients, err := inverseScaleLuma4x4(rasterLevels, scalingList, qpy)
				if err != nil {
					return Intra16x16LumaMacroblockResult{}, err
				}
				coefficients[0] = result.DCLevels[rasterIndex]
				blockResidual := inverseTransformLuma4x4(coefficients)
				for row := 0; row < 4; row++ {
					destination := (blockY*4+row)*16 + blockX*4
					copy(result.Residual[destination:destination+4], blockResidual[row*4:row*4+4])
				}
				continue
			}
		}
		coefficients, err := inverseScaleLuma4x4([16]int32{}, scalingList, qpy)
		if err != nil {
			return Intra16x16LumaMacroblockResult{}, err
		}
		coefficients[0] = result.DCLevels[rasterIndex]
		blockResidual := inverseTransformLuma4x4(coefficients)
		for row := 0; row < 4; row++ {
			destination := (blockY*4+row)*16 + blockX*4
			copy(result.Residual[destination:destination+4], blockResidual[row*4:row*4+4])
		}
	}
	result.Samples, err = ReconstructLumaIntra16x16Macroblock(result.PredictionMode, top, left, topLeft, topLeftAvailable, result.Residual)
	if err != nil {
		return Intra16x16LumaMacroblockResult{}, err
	}
	decoder.commitTrial(&trial, &readerCopy)
	*contexts = trialContexts
	return result, nil
}

// DecodeIntraChroma420Macroblock decodes/reconstructs 8-bit 4:2:0 chroma.
// intra_chroma_pred_mode is separate from mb_type; I16x16 mb_type only supplies chroma CBP.
func (decoder *CABACArithmeticDecoder) DecodeIntraChroma420Macroblock(contexts *[CABACContextCount]CABACContextModel, mode uint8, modeAlreadyDecoded bool, mbType uint8, intra16x16 bool, leftModeNonzero, topModeNonzero bool, codedBlockPatternChroma uint8, qpy int, qpOffsets [2]int, scalingLists [2][16]uint8, references [2]CABACChroma420References, leftEdges, topEdges [2]CABACChroma420EdgeState) (IntraChroma420MacroblockResult, error) {
	var result IntraChroma420MacroblockResult
	if contexts == nil {
		return result, ErrCABACUnsupportedSyntax
	}
	if intra16x16 {
		if mbType < 1 || mbType > 24 {
			return result, ErrCABACUnsupportedSyntax
		}
		codedBlockPatternChroma = ((mbType - 1) / 4) % 3
	} else if codedBlockPatternChroma > 2 || mbType != 0 {
		return result, ErrCABACUnsupportedSyntax
	}
	if modeAlreadyDecoded && mode > 3 {
		return result, ErrCABACIntraPredModeRange
	}
	readerCopy := *decoder.bits
	trial := *decoder
	trial.bits = &readerCopy
	trialContexts := *contexts
	if !modeAlreadyDecoded {
		var modeContexts [4]CABACContextModel
		copy(modeContexts[:], trialContexts[64:68])
		decodedMode, err := trial.DecodeIntraChromaPredMode(&modeContexts, leftModeNonzero, topModeNonzero)
		if err != nil {
			return IntraChroma420MacroblockResult{}, err
		}
		mode = decodedMode
		copy(trialContexts[64:68], modeContexts[:])
	}
	result.PredictionMode = mode
	for component := 0; component < 2; component++ {
		qpc, err := deriveChromaQPC(qpy, qpOffsets[component])
		if err != nil {
			return IntraChroma420MacroblockResult{}, err
		}
		result.QPC[component] = qpc
		var dcLevels [4]int32
		var dcSamples [4]int64
		if codedBlockPatternChroma != 0 {
			dcCoded, topCoded := chromaDCCondTerms(leftEdges[component], topEdges[component])
			decoded, coded, err := trial.DecodeResidualBlock(3, dcCoded, topCoded, &trialContexts)
			if err != nil {
				return IntraChroma420MacroblockResult{}, err
			}
			if coded {
				result.DCCoded[component] = true
				copy(dcLevels[:], decoded[:4])
				transformed := inverseTransformChromaDC2x2(dcLevels)
				dcSamples, err = inverseScaleChromaDC2x2(transformed, qpc)
				if err != nil {
					return IntraChroma420MacroblockResult{}, err
				}
			}
		}
		var residualBlocks [4][16]int64
		var acCodedFlags [4]bool
		for blockIndex := 0; blockIndex < 4; blockIndex++ {
			if codedBlockPatternChroma == 2 {
				blockX, blockY := blockIndex%2, blockIndex/2
				condLeft := chromaACCondTerm(blockIndex, blockX, blockY, true, acCodedFlags, leftEdges[component])
				condTop := chromaACCondTerm(blockIndex, blockX, blockY, false, acCodedFlags, topEdges[component])
				acLevels, coded, err := trial.DecodeResidualBlock(4, condLeft, condTop, &trialContexts)
				if err != nil {
					return IntraChroma420MacroblockResult{}, err
				}
				acCodedFlags[blockIndex] = coded
				if coded {
					var acScan [15]int32
					copy(acScan[:], acLevels[:15])
					residualBlocks[blockIndex], err = reconstructChroma4x4Residual(dcSamples[blockIndex], acScan, scalingLists[component], qpc)
					if err != nil {
						return IntraChroma420MacroblockResult{}, err
					}
					continue
				}
			}
			residualBlocks[blockIndex], err = reconstructChroma4x4Residual(dcSamples[blockIndex], [15]int32{}, scalingLists[component], qpc)
			if err != nil {
				return IntraChroma420MacroblockResult{}, err
			}
		}
		result.ACCodedBlockFlags[component] = acCodedFlags
		macroblockResidual := assembleChroma420ResidualMacroblock(residualBlocks)
		var top, left *[8]uint8
		var topLeft *uint8
		if references[component].TopAvailable {
			top = &references[component].Top
		}
		if references[component].LeftAvailable {
			left = &references[component].Left
		}
		if references[component].TopLeftAvailable {
			topLeft = &references[component].TopLeft
		}
		prediction, err := predictChromaIntra8x8(mode, top, left, topLeft)
		if err != nil {
			return IntraChroma420MacroblockResult{}, err
		}
		samples := reconstructChroma420Macroblock(prediction, macroblockResidual)
		if component == 0 {
			result.CbResidual, result.Cb = macroblockResidual, samples
		} else {
			result.CrResidual, result.Cr = macroblockResidual, samples
		}
	}
	decoder.commitTrial(&trial, &readerCopy)
	*contexts = trialContexts
	return result, nil
}

// DecodeIIntraMacroblock decodes one I-slice macroblock in syntax order and places its Y/U/V planes atomically.
// Per-block sample references and neighbor CBF/CBP facts are caller-gathered from already decoded neighbors.
func (decoder *CABACArithmeticDecoder) DecodeIIntraMacroblock(input CABACIIntraMacroblockInput, contexts *[CABACContextCount]CABACContextModel, builder *Yuv420FrameBuilder, address int) (CABACIIntraMacroblockResult, error) {
	var result CABACIIntraMacroblockResult
	if decoder == nil || decoder.bits == nil || contexts == nil || builder == nil || input.PreviousQPY < 0 || input.PreviousQPY > 51 {
		return result, ErrCABACContextState
	}
	readerCopy := *decoder.bits
	trial := *decoder
	trial.bits = &readerCopy
	trialContexts := *contexts
	var mbTypeContexts [8]CABACContextModel
	copy(mbTypeContexts[:], trialContexts[3:11])
	mbType, err := trial.DecodeIIntraMBType(input.SliceType, &mbTypeContexts, input.LeftAvailable, input.LeftIntra16OrPCM, input.TopAvailable, input.TopIntra16OrPCM)
	if err != nil {
		return result, err
	}
	copy(trialContexts[3:11], mbTypeContexts[:])
	result.MacroblockType = mbType
	result.QPY = input.PreviousQPY
	if mbType == 25 {
		var yBlock [256]uint8
		var cbBlock, crBlock [64]uint8
		for readerCopy.bitOffset%8 != 0 {
			bit, err := trial.bits.ReadBit()
			if err != nil {
				return CABACIIntraMacroblockResult{}, fmt.Errorf("I_PCM alignment: %w", err)
			}
			if bit {
				return CABACIIntraMacroblockResult{}, ErrCABACPCMAlignment
			}
		}
		for index := range yBlock {
			sample, err := trial.bits.ReadBits(8)
			if err != nil {
				return CABACIIntraMacroblockResult{}, fmt.Errorf("I_PCM luma samples: %w", err)
			}
			yBlock[index] = uint8(sample)
		}
		for index := range cbBlock {
			sample, err := trial.bits.ReadBits(8)
			if err != nil {
				return CABACIIntraMacroblockResult{}, fmt.Errorf("I_PCM Cb samples: %w", err)
			}
			cbBlock[index] = uint8(sample)
		}
		for index := range crBlock {
			sample, err := trial.bits.ReadBits(8)
			if err != nil {
				return CABACIIntraMacroblockResult{}, fmt.Errorf("I_PCM Cr samples: %w", err)
			}
			crBlock[index] = uint8(sample)
		}
		offset, err := trial.bits.ReadBits(9)
		if err != nil {
			return CABACIIntraMacroblockResult{}, fmt.Errorf("I_PCM CABAC reinitialization: %w", err)
		}
		if offset >= cabacInitialRange {
			return CABACIIntraMacroblockResult{}, ErrCABACOffsetOutOfRange
		}
		if err := builder.PlaceMacroblock(address, yBlock, cbBlock, crBlock); err != nil {
			return CABACIIntraMacroblockResult{}, err
		}
		trial.codeRange, trial.codeOffset, trial.terminated = cabacInitialRange, offset, false
		result.Luma, result.Cb, result.Cr = yBlock, cbBlock, crBlock
		decoder.commitTrial(&trial, &readerCopy)
		*contexts = trialContexts
		return result, nil
	}

	var modes4x4 [16]uint8
	var modes8x8 [4]uint8
	if mbType == 0 {
		if input.Transform8x8ModeEnabled {
			var transformContexts [3]CABACContextModel
			copy(transformContexts[:], trialContexts[399:402])
			result.TransformSize8x8, err = trial.DecodeTransformSize8x8Flag(&transformContexts, input.LeftHas8x8Transform, input.TopHas8x8Transform)
			if err != nil {
				return CABACIIntraMacroblockResult{}, err
			}
			copy(trialContexts[399:402], transformContexts[:])
		}
		var modeContexts [2]CABACContextModel
		copy(modeContexts[:], trialContexts[68:70])
		if result.TransformSize8x8 {
			for blockIndex := range modes8x8 {
				blockX, blockY := blockIndex%2, blockIndex/2
				leftAvailable, topAvailable := input.LeftModeAvailable, input.TopModeAvailable
				leftMode, topMode := input.LeftModes8x8[blockY], input.TopModes8x8[blockX]
				if blockX > 0 {
					leftAvailable, leftMode = true, modes8x8[blockIndex-1]
				}
				if blockY > 0 {
					topAvailable, topMode = true, modes8x8[blockIndex-2]
				}
				predictedMode := uint8(2)
				if leftAvailable && topAvailable {
					predictedMode = min(leftMode, topMode)
				}
				modes8x8[blockIndex], err = trial.DecodeIntra4x4PredMode(predictedMode, &modeContexts)
				if err != nil {
					return CABACIIntraMacroblockResult{}, err
				}
			}
		} else {
			modes4x4, err = trial.DecodeIntra4x4PredModes(&modeContexts, input.TopModes4x4, input.LeftModes4x4, input.TopModeAvailable, input.LeftModeAvailable)
			if err != nil {
				return CABACIIntraMacroblockResult{}, err
			}
		}
		copy(trialContexts[68:70], modeContexts[:])
		var chromaModeContexts [4]CABACContextModel
		copy(chromaModeContexts[:], trialContexts[64:68])
		resultChromaMode, modeErr := trial.DecodeIntraChromaPredMode(&chromaModeContexts, input.ChromaLeftModeNonzero, input.ChromaTopModeNonzero)
		if modeErr != nil {
			return CABACIIntraMacroblockResult{}, modeErr
		}
		resultChromaModeForReconstruction := resultChromaMode
		result.ChromaPredictionMode = resultChromaMode
		result.Luma4x4Modes = modes4x4
		result.Luma8x8Modes = modes8x8
		copy(trialContexts[64:68], chromaModeContexts[:])
		var lumaContexts [4]CABACContextModel
		copy(lumaContexts[:], trialContexts[73:77])
		result.CodedBlockPatternLuma, err = trial.DecodeLumaCodedBlockPattern(input.LeftLumaCBP, input.TopLumaCBP, &lumaContexts)
		if err != nil {
			return CABACIIntraMacroblockResult{}, err
		}
		copy(trialContexts[73:77], lumaContexts[:])
		var chromaContexts [8]CABACContextModel
		copy(chromaContexts[:], trialContexts[77:85])
		result.CodedBlockPatternChroma, err = trial.DecodeChromaCodedBlockPattern(input.LeftChromaCBP, input.TopChromaCBP, &chromaContexts)
		if err != nil {
			return CABACIIntraMacroblockResult{}, err
		}
		copy(trialContexts[77:85], chromaContexts[:])
		result.QPY, result.QPDelta, err = decodeIntraMacroblockQP(&trial, &trialContexts, input, result.CodedBlockPatternLuma, result.CodedBlockPatternChroma, mbType)
		if err != nil {
			return CABACIIntraMacroblockResult{}, err
		}
		if result.TransformSize8x8 {
			lumaResult, err := decodeIntraNxN8x8LumaResiduals(&trial, &trialContexts, result.CodedBlockPatternLuma, result.QPY, input.Luma8x8ScalingList, modes8x8, input.Luma8x8Blocks)
			if err != nil {
				return CABACIIntraMacroblockResult{}, err
			}
			result.Luma = lumaResult.Samples
		} else {
			lumaResult, err := decodeIntraNxN4x4LumaResiduals(&trial, &trialContexts, result.CodedBlockPatternLuma, result.QPY, input.Luma4x4ScalingList, modes4x4, input.Luma4x4TopEdge, input.Luma4x4LeftEdge, input.Luma4x4Blocks)
			if err != nil {
				return CABACIIntraMacroblockResult{}, err
			}
			result.Luma, result.Luma4x4CodedBlockFlags = lumaResult.Samples, lumaResult.CodedBlockFlags
		}
		chromaResult, err := trial.DecodeIntraChroma420Macroblock(&trialContexts, resultChromaModeForReconstruction, true, 0, false, input.ChromaLeftModeNonzero, input.ChromaTopModeNonzero, result.CodedBlockPatternChroma, result.QPY, input.ChromaQPIndexOffsets, input.ChromaScalingLists, input.ChromaReferences, input.ChromaLeftEdges, input.ChromaTopEdges)
		if err != nil {
			return CABACIIntraMacroblockResult{}, err
		}
		result.Cb, result.Cr = chromaResult.Cb, chromaResult.Cr
		result.ChromaDCCoded, result.ChromaACCodedFlags = chromaResult.DCCoded, chromaResult.ACCodedBlockFlags
	} else {
		result.CodedBlockPatternLuma = 0
		if mbType >= 13 {
			result.CodedBlockPatternLuma = 15
		}
		result.CodedBlockPatternChroma = ((mbType - 1) / 4) % 3
		chromaModeContexts := [4]CABACContextModel{}
		copy(chromaModeContexts[:], trialContexts[64:68])
		chromaMode, err := trial.DecodeIntraChromaPredMode(&chromaModeContexts, input.ChromaLeftModeNonzero, input.ChromaTopModeNonzero)
		if err != nil {
			return CABACIIntraMacroblockResult{}, err
		}
		copy(trialContexts[64:68], chromaModeContexts[:])
		result.ChromaPredictionMode = chromaMode
		result.Intra16x16LumaMode = (mbType - 1) % 4
		result.QPY, result.QPDelta, err = decodeIntraMacroblockQP(&trial, &trialContexts, input, result.CodedBlockPatternLuma, result.CodedBlockPatternChroma, mbType)
		if err != nil {
			return CABACIIntraMacroblockResult{}, err
		}
		top, left := (*[16]uint8)(nil), (*[16]uint8)(nil)
		if input.Intra16x16TopAvailable {
			top = &input.Intra16x16Top
		}
		if input.Intra16x16LeftAvailable {
			left = &input.Intra16x16Left
		}
		lumaResult, err := trial.DecodeIntra16x16LumaMacroblock(mbType, &trialContexts, result.QPY, input.Luma4x4ScalingList, top, left, input.Intra16x16TopLeft, input.Intra16x16TopLeftAvailable, input.Luma16x16TopEdge, input.Luma16x16LeftEdge)
		if err != nil {
			return CABACIIntraMacroblockResult{}, err
		}
		result.Luma = lumaResult.Samples
		chromaResult, err := trial.DecodeIntraChroma420Macroblock(&trialContexts, chromaMode, true, mbType, true, input.ChromaLeftModeNonzero, input.ChromaTopModeNonzero, result.CodedBlockPatternChroma, result.QPY, input.ChromaQPIndexOffsets, input.ChromaScalingLists, input.ChromaReferences, input.ChromaLeftEdges, input.ChromaTopEdges)
		if err != nil {
			return CABACIIntraMacroblockResult{}, err
		}
		result.Cb, result.Cr = chromaResult.Cb, chromaResult.Cr
		result.ChromaDCCoded, result.ChromaACCodedFlags = chromaResult.DCCoded, chromaResult.ACCodedBlockFlags
	}
	if err := builder.PlaceMacroblock(address, result.Luma, result.Cb, result.Cr); err != nil {
		return CABACIIntraMacroblockResult{}, err
	}
	decoder.commitTrial(&trial, &readerCopy)
	*contexts = trialContexts
	return result, nil
}

func decodeIntraMacroblockQP(decoder *CABACArithmeticDecoder, contexts *[CABACContextCount]CABACContextModel, input CABACIIntraMacroblockInput, cbpLuma, cbpChroma, mbType uint8) (int, int, error) {
	qpy := input.PreviousQPY
	if cbpLuma == 0 && cbpChroma == 0 && mbType == 0 {
		return qpy, 0, nil
	}
	var deltaContexts [4]CABACContextModel
	copy(deltaContexts[:], contexts[60:64])
	delta, err := decoder.DecodeMBQPDelta(&deltaContexts, input.PreviousQPDelta)
	if err != nil {
		return 0, 0, err
	}
	copy(contexts[60:64], deltaContexts[:])
	return (qpy + delta + 52) % 52, delta, nil
}

func decodeIntraNxN4x4LumaResiduals(decoder *CABACArithmeticDecoder, contexts *[CABACContextCount]CABACContextModel, cbp uint8, qpy int, scalingList [16]uint8, modes [16]uint8, topEdge, leftEdge CABACIntra4x4EdgeState, blocks [16]LumaIntra4x4Block) (Intra4x4LumaMacroblockResult, error) {
	var result Intra4x4LumaMacroblockResult
	result.Modes = modes
	for blockIndex := range blocks {
		rasterIndex := int(luma4x4BlockScanToRaster[blockIndex])
		blockX, blockY := rasterIndex%4, rasterIndex/4
		blocks[blockIndex].Mode = modes[blockIndex]
		cbpIndex := blockY/2*2 + blockX/2
		if cbp&(1<<cbpIndex) == 0 {
			blocks[blockIndex].Residual = [16]int64{}
			continue
		}
		condLeft := deriveIntra4x4LumaCondTerm(blockIndex, blockX, blockY, true, result.CodedBlockFlags, leftEdge, true)
		condTop := deriveIntra4x4LumaCondTerm(blockIndex, blockX, blockY, false, result.CodedBlockFlags, topEdge, true)
		levels, coded, err := decoder.DecodeResidualBlock(2, condLeft, condTop, contexts)
		if err != nil {
			return Intra4x4LumaMacroblockResult{}, err
		}
		result.CodedBlockFlags[blockIndex] = coded
		if coded {
			residual, err := reconstructLuma4x4Residual(PlaceLuma4x4ScanLevels(levels), scalingList, qpy)
			if err != nil {
				return Intra4x4LumaMacroblockResult{}, err
			}
			blocks[blockIndex].Residual = residual
			result.Residuals[blockIndex] = residual
		} else {
			blocks[blockIndex].Residual = [16]int64{}
		}
	}
	var err error
	result.Samples, err = ReconstructLumaIntra4x4Macroblock(blocks)
	return result, err
}

func decodeIntraNxN8x8LumaResiduals(decoder *CABACArithmeticDecoder, contexts *[CABACContextCount]CABACContextModel, cbp uint8, qpy int, scalingList [64]uint8, modes [4]uint8, blocks [4]LumaIntra8x8Block) (Intra8x8LumaMacroblockResult, error) {
	var result Intra8x8LumaMacroblockResult
	result.TransformSize8x8, result.Modes = true, modes
	for blockIndex := range blocks {
		blocks[blockIndex].Mode = modes[blockIndex]
		if cbp&(1<<blockIndex) == 0 {
			blocks[blockIndex].Residual = [64]int64{}
			continue
		}
		levels, err := decoder.DecodeLuma8x8ResidualBlock(contexts)
		if err != nil {
			return Intra8x8LumaMacroblockResult{}, err
		}
		coefficients, err := inverseScaleLuma8x8(PlaceLuma8x8ScanLevels(levels), scalingList, qpy)
		if err != nil {
			return Intra8x8LumaMacroblockResult{}, err
		}
		blocks[blockIndex].Residual = inverseTransformLuma8x8(coefficients)
		result.Residuals[blockIndex] = blocks[blockIndex].Residual
	}
	var err error
	result.Samples, err = ReconstructLumaIntra8x8Macroblock(blocks)
	return result, err
}

func chromaDCCondTerms(left, top CABACChroma420EdgeState) (bool, bool) {
	return DeriveCABACCodedBlockFlagCondTerm(left.Available, true, left.IsIPCM, left.DCBlockAvailable, left.DCBlockCoded),
		DeriveCABACCodedBlockFlagCondTerm(top.Available, true, top.IsIPCM, top.DCBlockAvailable, top.DCBlockCoded)
}

func chromaACCondTerm(blockIndex, blockX, blockY int, isLeft bool, codedFlags [4]bool, edge CABACChroma420EdgeState) bool {
	neighborCoordinate, edgeCoordinate := blockY, blockX
	if isLeft {
		neighborCoordinate, edgeCoordinate = blockX, blockY
	}
	if neighborCoordinate > 0 {
		neighborIndex := blockIndex - 1
		if !isLeft {
			neighborIndex = blockIndex - 2
		}
		return DeriveCABACCodedBlockFlagCondTerm(true, true, false, true, codedFlags[neighborIndex])
	}
	if !edge.Available || edge.IsIPCM {
		return true
	}
	return DeriveCABACCodedBlockFlagCondTerm(true, true, false, edge.ACBlockAvailable[edgeCoordinate], edge.ACBlockCoded[edgeCoordinate])
}

func deriveIntra4x4LumaCondTerm(blockIndex, blockX, blockY int, isLeft bool, codedFlags [16]bool, edge CABACIntra4x4EdgeState, currentIntra bool) bool {
	neighborCoordinate := blockY
	edgeCoordinate := blockX
	if isLeft {
		neighborCoordinate, edgeCoordinate = blockX, blockY
	}
	if neighborCoordinate > 0 {
		rasterIndex := int(luma4x4BlockScanToRaster[blockIndex])
		neighborRaster := rasterIndex - 1
		if !isLeft {
			neighborRaster = rasterIndex - 4
		}
		for neighborIndex, candidate := range luma4x4BlockScanToRaster {
			if int(candidate) == neighborRaster {
				return DeriveCABACCodedBlockFlagCondTerm(true, currentIntra, false, true, codedFlags[neighborIndex])
			}
		}
		return false
	}
	if !edge.Available {
		return currentIntra
	}
	if edge.IsIPCM {
		return true
	}
	return DeriveCABACCodedBlockFlagCondTerm(true, currentIntra, false, edge.TransformBlockAvailable[edgeCoordinate], edge.TransformBlockCoded[edgeCoordinate])
}

func deriveIntra16x16ACCondTerm(blockIndex, blockX, blockY int, isLeft bool, codedFlags [16]bool, edge CABACIntra16x16EdgeState) bool {
	neighborCoordinate, edgeCoordinate := blockY, blockX
	if isLeft {
		neighborCoordinate, edgeCoordinate = blockX, blockY
	}
	if neighborCoordinate > 0 {
		rasterIndex := int(luma4x4BlockScanToRaster[blockIndex])
		neighborRaster := rasterIndex - 1
		if !isLeft {
			neighborRaster = rasterIndex - 4
		}
		for neighborIndex, candidate := range luma4x4BlockScanToRaster {
			if int(candidate) == neighborRaster {
				return DeriveCABACCodedBlockFlagCondTerm(true, true, false, true, codedFlags[neighborIndex])
			}
		}
		return false
	}
	if !edge.Available || edge.IsIPCM {
		return true
	}
	return DeriveCABACCodedBlockFlagCondTerm(true, true, false, edge.ACTransformBlockAvailable[edgeCoordinate], edge.ACTransformBlockCoded[edgeCoordinate])
}

// DecodeInterMBType decodes P/SP mb_type (0-3, intra 5-30) or B mb_type (0-22, intra 23-48)
// from whole-slice contexts (Tables 9-37, 9-39, and 9-41). B neighbor flags report available
// neighbors coded as B_Skip or B_Direct_16x16 (clause 9.3.3.1.1.3); P slices ignore neighbors.
func (decoder *CABACArithmeticDecoder) DecodeInterMBType(sliceType uint8, contexts *[CABACContextCount]CABACContextModel, leftAvailable, topAvailable, leftBSkipOrDirect, topBSkipOrDirect bool) (uint8, error) {
	if contexts == nil {
		return 0, ErrCABACContextState
	}
	if sliceType > 9 || (sliceType%5 != 0 && sliceType%5 != 1) {
		return 0, ErrCABACUnsupportedSyntax
	}
	readerCopy := *decoder.bits
	trial := *decoder
	trial.bits = &readerCopy
	trialContexts := *contexts
	var mbType uint8
	var err error
	if sliceType%5 == 0 {
		mbType, err = trial.decodePMBType(&trialContexts)
	} else {
		increment := 0
		if leftAvailable && !leftBSkipOrDirect {
			increment++
		}
		if topAvailable && !topBSkipOrDirect {
			increment++
		}
		mbType, err = trial.decodeBMBType(&trialContexts, increment)
	}
	if err != nil {
		return 0, err
	}
	decoder.commitTrial(&trial, &readerCopy)
	*contexts = trialContexts
	return mbType, nil
}

func (decoder *CABACArithmeticDecoder) decodePMBType(contexts *[CABACContextCount]CABACContextModel) (uint8, error) {
	intra, err := decoder.DecodeBin(&contexts[14])
	if err != nil {
		return 0, err
	}
	if intra {
		suffix, err := decoder.decodeIntraMBTypeSuffix(contexts, 17)
		return 5 + suffix, err
	}
	second, err := decoder.DecodeBin(&contexts[15])
	if err != nil {
		return 0, err
	}
	thirdContext := 16
	if second {
		thirdContext = 17
	}
	third, err := decoder.DecodeBin(&contexts[thirdContext])
	if err != nil {
		return 0, err
	}
	switch {
	case second && third:
		return 1, nil
	case second:
		return 2, nil
	case third:
		return 3, nil
	}
	return 0, nil
}

func (decoder *CABACArithmeticDecoder) decodeBMBType(contexts *[CABACContextCount]CABACContextModel, increment int) (uint8, error) {
	bins := func(ctxIdx int, count int) (uint8, error) {
		value := uint8(0)
		for range count {
			bin, err := decoder.DecodeBin(&contexts[ctxIdx])
			if err != nil {
				return 0, err
			}
			value = value<<1 | boolToUint8(bin)
		}
		return value, nil
	}
	if first, err := bins(27+increment, 1); err != nil || first == 0 {
		return 0, err
	}
	second, err := bins(30, 1)
	if err != nil {
		return 0, err
	}
	if second == 0 {
		third, err := bins(32, 1)
		return 1 + third, err
	}
	third, err := bins(31, 1)
	if err != nil {
		return 0, err
	}
	rest, err := bins(32, 3)
	if err != nil {
		return 0, err
	}
	if third == 0 {
		return 3 + rest, nil
	}
	switch rest {
	case 0b101:
		suffix, err := decoder.decodeIntraMBTypeSuffix(contexts, 32)
		return 23 + suffix, err
	case 0b110:
		return 11, nil
	case 0b111:
		return 22, nil
	}
	last, err := bins(32, 1)
	return 12 + rest<<1 + last, err
}

// decodeIntraMBTypeSuffix decodes the Table 9-36 suffix of an intra mb_type in P (17) or B (32) slices.
func (decoder *CABACArithmeticDecoder) decodeIntraMBTypeSuffix(contexts *[CABACContextCount]CABACContextModel, offset int) (uint8, error) {
	bin := func(ctxIdx int) (uint8, error) {
		value, err := decoder.DecodeBin(&contexts[ctxIdx])
		return boolToUint8(value), err
	}
	if first, err := bin(offset); err != nil || first == 0 {
		return 0, err
	}
	pcm, err := decoder.DecodeTerminateBin()
	if err != nil || pcm {
		return 25, err
	}
	mbType := uint8(1)
	cbpLuma, err := bin(offset + 1)
	if err != nil {
		return 0, err
	}
	mbType += 12 * cbpLuma
	cbpChroma, err := bin(offset + 2)
	if err != nil {
		return 0, err
	}
	if cbpChroma == 1 {
		chromaTwo, err := bin(offset + 2)
		if err != nil {
			return 0, err
		}
		mbType += 4 + 4*chromaTwo
	}
	high, err := bin(offset + 3)
	if err != nil {
		return 0, err
	}
	low, err := bin(offset + 3)
	if err != nil {
		return 0, err
	}
	return mbType + 2*high + low, nil
}

// DecodeSubMBType decodes sub_mb_type for P/SP (0-3, ctxIdx 21-23) or B (0-12, ctxIdx 36-39) slices.
func (decoder *CABACArithmeticDecoder) DecodeSubMBType(sliceType uint8, contexts *[CABACContextCount]CABACContextModel) (uint8, error) {
	if contexts == nil {
		return 0, ErrCABACContextState
	}
	if sliceType > 9 || (sliceType%5 != 0 && sliceType%5 != 1) {
		return 0, ErrCABACUnsupportedSyntax
	}
	readerCopy := *decoder.bits
	trial := *decoder
	trial.bits = &readerCopy
	trialContexts := *contexts
	var binErr error
	bin := func(ctxIdx int) uint8 {
		if binErr != nil {
			return 0
		}
		value, err := trial.DecodeBin(&trialContexts[ctxIdx])
		binErr = err
		return boolToUint8(value)
	}
	var subType uint8
	switch {
	case sliceType%5 == 0 && bin(21) == 1:
		subType = 0
	case sliceType%5 == 0 && bin(22) == 0:
		subType = 1
	case sliceType%5 == 0:
		subType = 3 - bin(23)
	case bin(36) == 0:
		subType = 0
	case bin(37) == 0:
		subType = 1 + bin(39)
	case bin(38) == 0:
		high := bin(39)
		subType = 3 + 2*high + bin(39)
	case bin(39) == 1:
		subType = 11 + bin(39)
	default:
		high := bin(39)
		subType = 7 + 2*high + bin(39)
	}
	if binErr != nil {
		return 0, binErr
	}
	decoder.commitTrial(&trial, &readerCopy)
	*contexts = trialContexts
	return subType, nil
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

// DeriveIntra4x4PredictedMode derives predIntra4x4PredMode from decoded A/B neighbors.
// Modes use luma4x4BlkIdx syntax order, not raster order.
func DeriveIntra4x4PredictedMode(decodedModes [16]uint8, blockIndex int, topModes, leftModes [4]uint8, topAvailable, leftAvailable bool) (uint8, error) {
	blockScanToRaster := [16]int{0, 1, 4, 5, 2, 3, 6, 7, 8, 9, 12, 13, 10, 11, 14, 15}
	if blockIndex < 0 || blockIndex >= len(blockScanToRaster) {
		return 0, ErrCABACIntraPredNeighbors
	}
	rasterIndex := blockScanToRaster[blockIndex]
	column, row := rasterIndex%4, rasterIndex/4
	var leftMode, topMode uint8
	leftOK, topOK := false, false
	if column > 0 {
		neighborRaster := rasterIndex - 1
		for scanIndex, candidate := range blockScanToRaster {
			if candidate == neighborRaster {
				if scanIndex >= blockIndex || decodedModes[scanIndex] > 8 {
					return 0, ErrCABACIntraPredNeighbors
				}
				leftMode, leftOK = decodedModes[scanIndex], true
				break
			}
		}
	} else if leftAvailable {
		leftMode = leftModes[row]
		if leftMode > 8 {
			return 0, ErrCABACIntraPredNeighbors
		}
		leftOK = true
	}
	if row > 0 {
		neighborRaster := rasterIndex - 4
		for scanIndex, candidate := range blockScanToRaster {
			if candidate == neighborRaster {
				if scanIndex >= blockIndex || decodedModes[scanIndex] > 8 {
					return 0, ErrCABACIntraPredNeighbors
				}
				topMode, topOK = decodedModes[scanIndex], true
				break
			}
		}
	} else if topAvailable {
		topMode = topModes[column]
		if topMode > 8 {
			return 0, ErrCABACIntraPredNeighbors
		}
		topOK = true
	}
	if !leftOK || !topOK {
		return 2, nil
	}
	if leftMode < topMode {
		return leftMode, nil
	}
	return topMode, nil
}

// DecodeIntra4x4PredModes decodes all luma prediction modes in syntax scan order.
func (decoder *CABACArithmeticDecoder) DecodeIntra4x4PredModes(contexts *[2]CABACContextModel, topModes, leftModes [4]uint8, topAvailable, leftAvailable bool) ([16]uint8, error) {
	var modes [16]uint8
	if contexts == nil {
		return modes, ErrCABACContextState
	}
	readerCopy := *decoder.bits
	trial := *decoder
	trial.bits = &readerCopy
	trialContexts := *contexts
	for blockIndex := range modes {
		predictedMode, err := DeriveIntra4x4PredictedMode(modes, blockIndex, topModes, leftModes, topAvailable, leftAvailable)
		if err != nil {
			return [16]uint8{}, err
		}
		mode, err := trial.DecodeIntra4x4PredMode(predictedMode, &trialContexts)
		if err != nil {
			return [16]uint8{}, err
		}
		modes[blockIndex] = mode
	}
	decoder.commitTrial(&trial, &readerCopy)
	*contexts = trialContexts
	return modes, nil
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

	contextIndex := uint8(0)
	if neighborMagnitude >= 3 {
		contextIndex++
	}
	if neighborMagnitude >= 33 {
		contextIndex++
	}
	return decoder.decodeMotionVectorDifferenceWithContext(contextIndex, contexts)
}

func (decoder *CABACArithmeticDecoder) decodeMotionVectorDifferenceWithContext(contextIndex uint8, contexts *[7]CABACContextModel) (int32, error) {
	if contexts == nil || contextIndex > 2 {
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
	coefficientContextIndex := 3
	for absolute < 9 {
		greater, err := trial.DecodeBin(&trialContexts[coefficientContextIndex])
		if err != nil {
			return 0, err
		}
		if !greater {
			break
		}
		if absolute < 4 {
			coefficientContextIndex++
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

// DecodeLuma4x4ResidualBlockWithFlag decodes coded_block_flag and, when set, its residual block.
func (decoder *CABACArithmeticDecoder) DecodeLuma4x4ResidualBlockWithFlag(leftNonzero, topNonzero uint8, codedFlagContexts *[4]CABACContextModel, significantContexts, lastContexts *[15]CABACContextModel, coefficientContexts *[10]CABACContextModel) ([16]int32, bool, error) {
	var levels [16]int32
	if codedFlagContexts == nil || significantContexts == nil || lastContexts == nil || coefficientContexts == nil {
		return levels, false, ErrCABACContextState
	}
	for _, bank := range [][]*CABACContextModel{
		{&codedFlagContexts[0], &codedFlagContexts[1], &codedFlagContexts[2], &codedFlagContexts[3]},
		{&significantContexts[0], &significantContexts[1], &significantContexts[2], &significantContexts[3], &significantContexts[4], &significantContexts[5], &significantContexts[6], &significantContexts[7], &significantContexts[8], &significantContexts[9], &significantContexts[10], &significantContexts[11], &significantContexts[12], &significantContexts[13], &significantContexts[14]},
		{&lastContexts[0], &lastContexts[1], &lastContexts[2], &lastContexts[3], &lastContexts[4], &lastContexts[5], &lastContexts[6], &lastContexts[7], &lastContexts[8], &lastContexts[9], &lastContexts[10], &lastContexts[11], &lastContexts[12], &lastContexts[13], &lastContexts[14]},
		{&coefficientContexts[0], &coefficientContexts[1], &coefficientContexts[2], &coefficientContexts[3], &coefficientContexts[4], &coefficientContexts[5], &coefficientContexts[6], &coefficientContexts[7], &coefficientContexts[8], &coefficientContexts[9]},
	} {
		for _, context := range bank {
			if context.stateIndex >= 64 {
				return levels, false, ErrCABACContextState
			}
		}
	}
	if leftNonzero > 16 || topNonzero > 16 {
		return levels, false, ErrCABACNonzeroCountRange
	}
	readerCopy := *decoder.bits
	trial := *decoder
	trial.bits = &readerCopy
	trialCodedFlagContexts := *codedFlagContexts
	trialSignificantContexts := *significantContexts
	trialLastContexts := *lastContexts
	trialCoefficientContexts := *coefficientContexts
	coded, err := trial.DecodeLuma4x4CodedBlockFlag(leftNonzero, topNonzero, &trialCodedFlagContexts)
	if err != nil {
		return levels, false, err
	}
	if coded {
		levels, err = trial.DecodeLuma4x4ResidualBlock(&trialSignificantContexts, &trialLastContexts, &trialCoefficientContexts)
		if err != nil {
			return [16]int32{}, false, err
		}
	}
	decoder.commitTrial(&trial, &readerCopy)
	*codedFlagContexts = trialCodedFlagContexts
	*significantContexts = trialSignificantContexts
	*lastContexts = trialLastContexts
	*coefficientContexts = trialCoefficientContexts
	return levels, coded, nil
}

// DecodeChroma4x4ACResidualBlock decodes one 4:2:0 chroma AC block (scan positions 1-15).
// Context models are caller-initialized for ctxBlockCat 4 and updated transactionally.
func (decoder *CABACArithmeticDecoder) DecodeChroma4x4ACResidualBlock(leftHasNonzero, topHasNonzero bool, codedFlagContexts *[4]CABACContextModel, significantContexts, lastContexts *[15]CABACContextModel, coefficientContexts *[10]CABACContextModel) ([16]int32, bool, error) {
	var rasterLevels [16]int32
	if codedFlagContexts == nil || significantContexts == nil || lastContexts == nil || coefficientContexts == nil {
		return rasterLevels, false, ErrCABACContextState
	}
	for _, contexts := range [][]CABACContextModel{codedFlagContexts[:], significantContexts[:], lastContexts[:], coefficientContexts[:]} {
		for _, context := range contexts {
			if context.stateIndex >= 64 {
				return rasterLevels, false, ErrCABACContextState
			}
		}
	}
	readerCopy := *decoder.bits
	trial := *decoder
	trial.bits = &readerCopy
	trialCodedFlagContexts := *codedFlagContexts
	trialSignificantContexts := *significantContexts
	trialLastContexts := *lastContexts
	trialCoefficientContexts := *coefficientContexts
	coded, err := trial.DecodeLuma4x4CodedBlockFlag(boolToUint8(leftHasNonzero), boolToUint8(topHasNonzero), &trialCodedFlagContexts)
	if err != nil {
		return rasterLevels, false, err
	}
	if coded {
		var significance [16]bool
		lastFound := false
		for scanIndex := 0; scanIndex < 14; scanIndex++ {
			isSignificant, err := trial.DecodeBin(&trialSignificantContexts[scanIndex])
			if err != nil {
				return [16]int32{}, false, err
			}
			if !isSignificant {
				continue
			}
			significance[scanIndex+1] = true
			isLast, err := trial.DecodeBin(&trialLastContexts[scanIndex])
			if err != nil {
				return [16]int32{}, false, err
			}
			if isLast {
				lastFound = true
				break
			}
		}
		if !lastFound {
			significance[15] = true
		}
		scanLevels, err := trial.DecodeLuma4x4ResidualLevels(&significance, &trialCoefficientContexts)
		if err != nil {
			return [16]int32{}, false, err
		}
		var acScanLevels [15]int32
		for scanIndex := 1; scanIndex < 16; scanIndex++ {
			acScanLevels[scanIndex-1] = scanLevels[scanIndex]
		}
		rasterLevels = PlaceChroma4x4ScanLevels(0, acScanLevels)
	}
	decoder.commitTrial(&trial, &readerCopy)
	*codedFlagContexts = trialCodedFlagContexts
	*significantContexts = trialSignificantContexts
	*lastContexts = trialLastContexts
	*coefficientContexts = trialCoefficientContexts
	return rasterLevels, coded, nil
}

// DecodeAndReconstructLuma4x4Residual returns sample-domain luma residuals for one 4x4 block.
func (decoder *CABACArithmeticDecoder) DecodeAndReconstructLuma4x4Residual(leftNonzero, topNonzero uint8, codedFlagContexts *[4]CABACContextModel, significantContexts, lastContexts *[15]CABACContextModel, coefficientContexts *[10]CABACContextModel, scalingList [16]uint8, qpy int) ([16]int64, bool, error) {
	if _, err := inverseScaleLuma4x4([16]int32{}, scalingList, qpy); err != nil {
		return [16]int64{}, false, err
	}
	levels, coded, err := decoder.DecodeLuma4x4ResidualBlockWithFlag(leftNonzero, topNonzero, codedFlagContexts, significantContexts, lastContexts, coefficientContexts)
	if err != nil {
		return [16]int64{}, false, err
	}
	if !coded {
		return [16]int64{}, false, nil
	}
	residual, err := reconstructLuma4x4Residual(levels, scalingList, qpy)
	if err != nil {
		return [16]int64{}, false, err
	}
	return residual, true, nil
}

// cabacResidualMaxNumCoeff is maxNumCoeff for ctxBlockCat 0-4 with 4:2:0 chroma (4*NumC8x8 = 4 chroma DC).
var cabacResidualMaxNumCoeff = [5]int{16, 15, 16, 4, 15}

// DeriveCABACCodedBlockFlagCondTerm returns condTermFlagN for coded_block_flag (clause 9.3.3.1.1.9).
// transBlockAvailable/transBlockCoded describe neighbor block N of the same ctxBlockCat (and iCbCr);
// the slice-data-partitioning rule is omitted because data partitioning is unsupported.
func DeriveCABACCodedBlockFlagCondTerm(neighborAvailable, currentIntra, neighborIPCM, transBlockAvailable, transBlockCoded bool) bool {
	switch {
	case !neighborAvailable:
		return currentIntra
	case neighborIPCM:
		return true
	case !transBlockAvailable:
		return false
	}
	return transBlockCoded
}

// DecodeResidualBlock decodes coded_block_flag and residual_block_cabac for ctxBlockCat 0-4 (4:2:0),
// selecting contexts from a whole-slice context array and updating it transactionally.
// Levels are in coefficient-list order: list index i is scan position i for categories 0, 2, and 3,
// and scan position i+1 for AC categories 1 and 4. Entries past maxNumCoeff are zero.
func (decoder *CABACArithmeticDecoder) DecodeResidualBlock(ctxBlockCat uint8, condTermFlagA, condTermFlagB bool, contexts *[CABACContextCount]CABACContextModel) ([16]int32, bool, error) {
	var levels [16]int32
	if contexts == nil {
		return levels, false, ErrCABACContextState
	}
	if ctxBlockCat > 4 {
		return levels, false, ErrCABACUnsupportedSyntax
	}
	bases, err := CABACResidualContextBasesForCategory(ctxBlockCat)
	if err != nil {
		return levels, false, err
	}
	maxNumCoeff := cabacResidualMaxNumCoeff[ctxBlockCat]
	readerCopy := *decoder.bits
	trial := *decoder
	trial.bits = &readerCopy
	trialContexts := *contexts
	codedInc := int(boolToUint8(condTermFlagA)) + 2*int(boolToUint8(condTermFlagB))
	coded, err := trial.DecodeBin(&trialContexts[bases.CodedBlockFlag+codedInc])
	if err != nil {
		return levels, false, err
	}
	if coded {
		var significant [16]bool
		lastFound := false
		for index := 0; index < maxNumCoeff-1; index++ {
			increment := index
			if ctxBlockCat == 3 {
				increment = min(index, 2) // Min(numDecodAbsLevel/NumC8x8, 2) with NumC8x8 = 1.
			}
			isSignificant, err := trial.DecodeBin(&trialContexts[bases.Significant+increment])
			if err != nil {
				return [16]int32{}, false, err
			}
			if !isSignificant {
				continue
			}
			significant[index] = true
			isLast, err := trial.DecodeBin(&trialContexts[bases.Last+increment])
			if err != nil {
				return [16]int32{}, false, err
			}
			if isLast {
				lastFound = true
				break
			}
		}
		if !lastFound {
			significant[maxNumCoeff-1] = true
		}
		greaterCap := 4
		if ctxBlockCat == 3 {
			greaterCap = 3
		}
		if err := trial.decodeCoefficientLevels(trialContexts[bases.AbsLevel:bases.AbsLevel+10], greaterCap, significant[:maxNumCoeff], levels[:maxNumCoeff]); err != nil {
			return [16]int32{}, false, err
		}
	}
	decoder.commitTrial(&trial, &readerCopy)
	*contexts = trialContexts
	return levels, coded, nil
}

// decodeCoefficientLevels decodes signed levels in reverse list order with clause 9.3.3.1.3 contexts.
func (decoder *CABACArithmeticDecoder) decodeCoefficientLevels(absContexts []CABACContextModel, greaterCap int, significant []bool, levels []int32) error {
	equalOne, greaterOne := 0, 0
	for index := len(significant) - 1; index >= 0; index-- {
		if !significant[index] {
			continue
		}
		firstInc := 0
		if greaterOne == 0 {
			firstInc = min(4, 1+equalOne)
		}
		greaterInc := 5 + min(greaterCap, greaterOne)
		levelMinus1, err := decoder.DecodeCoeffAbsLevelMinus1(&absContexts[firstInc], &absContexts[greaterInc])
		if err != nil {
			return err
		}
		level, err := decoder.DecodeCoeffSign(levelMinus1)
		if err != nil {
			return err
		}
		levels[index] = level
		if levelMinus1 == 0 {
			equalOne++
		} else {
			greaterOne++
		}
	}
	return nil
}

// Table 9-43 ctxIdxInc by levelListIdx for frame-coded significant_coeff_flag and last_significant_coeff_flag.
var cabacLuma8x8FrameSignificantInc = [63]uint8{
	0, 1, 2, 3, 4, 5, 5, 4, 4, 3, 3, 4, 4, 4, 5, 5, 4, 4, 4, 4, 3, 3, 6, 7, 7, 7, 8, 9, 10, 9, 8, 7,
	7, 6, 11, 12, 13, 11, 6, 7, 8, 9, 14, 10, 9, 8, 6, 11, 12, 13, 11, 6, 9, 14, 10, 9, 11, 12, 13, 11, 14, 10, 12,
}
var cabacLuma8x8LastInc = [63]uint8{
	0, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2,
	3, 3, 3, 3, 3, 3, 3, 3, 4, 4, 4, 4, 4, 4, 4, 4, 5, 5, 5, 5, 6, 6, 6, 6, 7, 7, 7, 7, 8, 8, 8,
}

// DecodeLuma8x8ResidualBlock decodes residual_block_cabac for ctxBlockCat 5 in a frame macroblock.
// coded_block_flag is not parsed because it is inferred to be 1 when ChromaArrayType != 3;
// callers invoke this only for 8x8 blocks whose coded_block_pattern bit is set.
// Levels are in 8x8 frame-scan list order and contexts update transactionally.
func (decoder *CABACArithmeticDecoder) DecodeLuma8x8ResidualBlock(contexts *[CABACContextCount]CABACContextModel) ([64]int32, error) {
	var levels [64]int32
	if contexts == nil {
		return levels, ErrCABACContextState
	}
	bases, err := CABACResidualContextBasesForCategory(5)
	if err != nil {
		return levels, err
	}
	readerCopy := *decoder.bits
	trial := *decoder
	trial.bits = &readerCopy
	trialContexts := *contexts
	var significant [64]bool
	lastFound := false
	for index := 0; index < 63; index++ {
		isSignificant, err := trial.DecodeBin(&trialContexts[bases.Significant+int(cabacLuma8x8FrameSignificantInc[index])])
		if err != nil {
			return levels, err
		}
		if !isSignificant {
			continue
		}
		significant[index] = true
		isLast, err := trial.DecodeBin(&trialContexts[bases.Last+int(cabacLuma8x8LastInc[index])])
		if err != nil {
			return levels, err
		}
		if isLast {
			lastFound = true
			break
		}
	}
	if !lastFound {
		significant[63] = true
	}
	if err := trial.decodeCoefficientLevels(trialContexts[bases.AbsLevel:bases.AbsLevel+10], 4, significant[:], levels[:]); err != nil {
		return [64]int32{}, err
	}
	decoder.commitTrial(&trial, &readerCopy)
	*contexts = trialContexts
	return levels, nil
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
