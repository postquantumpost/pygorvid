package vid

import (
	"fmt"
)

type nativeSlice struct {
	header SliceHeader
	sps    SPSInfo
	pps    PPSInfo
	rbsp   []byte
}

type nativePrediction struct {
	used             bool
	reference        uint32
	refIndex         int32
	motion           MotionVector
	difference       MotionVector
	referenceDecoded bool
	motionDecoded    bool
	direct           bool
}

type nativeMacroblock struct {
	result      CABACIIntraMacroblockResult
	intra       bool
	skipped     bool
	direct      bool
	slice       nativeSlice
	predictions [16][2]nativePrediction
}

func (decoder *H264Decoder) nativeSlices(sample CompressedSample) ([]nativeSlice, error) {
	configuration := decoder.sampleReader.Configuration()
	lengthSize := int(configuration.NALLengthSize)
	if lengthSize < 1 || lengthSize > 4 {
		return nil, ErrDecoderUnsupportedFeature
	}
	var slices []nativeSlice
	for offset := 0; offset < len(sample.Data); {
		if len(sample.Data)-offset < lengthSize {
			return nil, fmt.Errorf("truncated NAL length")
		}
		length := uint32(0)
		for _, value := range sample.Data[offset : offset+lengthSize] {
			length = length<<8 | uint32(value)
		}
		offset += lengthSize
		if length == 0 || uint64(length) > uint64(len(sample.Data)-offset) {
			return nil, fmt.Errorf("invalid NAL length")
		}
		nal := sample.Data[offset : offset+int(length)]
		offset += int(length)
		nalHeader, err := ParseNALHeader(nal)
		if err != nil {
			return nil, err
		}
		if nalHeader.UnitType != 1 && nalHeader.UnitType != 5 {
			continue
		}
		rbsp, err := EBSPToRBSP(nal[1:])
		if err != nil {
			return nil, err
		}
		bits := NewBitReader(rbsp)
		if _, err = bits.ReadUE(); err != nil {
			return nil, err
		}
		if _, err = bits.ReadUE(); err != nil {
			return nil, err
		}
		ppsID, err := bits.ReadUE()
		if err != nil {
			return nil, err
		}
		found := false
		for _, pps := range decoder.pictureParameterSets {
			if pps.PictureParameterSetID != uint32(ppsID) {
				continue
			}
			for _, sps := range decoder.sequenceParameterSets {
				if sps.ID != pps.SequenceParameterSetID {
					continue
				}
				header, err := ParseSliceHeader(nal, sps, pps)
				if err != nil {
					return nil, err
				}
				slices = append(slices, nativeSlice{header: header, sps: sps, pps: pps, rbsp: rbsp})
				found = true
				break
			}
			break
		}
		if !found {
			return nil, fmt.Errorf("slice references an unavailable parameter set")
		}
	}
	if len(slices) == 0 {
		return nil, fmt.Errorf("sample contains no coded picture")
	}
	return slices, nil
}

func (decoder *H264Decoder) decodeNativeIntraSample(sample CompressedSample) (Yuv420Frame, error) {
	frame, err := decoder.decodeNativeCodedIntraSample(sample)
	if err != nil {
		return Yuv420Frame{}, err
	}
	return cropNativeFrame(frame, decoder.nativeBlocks[0].slice.sps), nil
}

func (decoder *H264Decoder) decodeNativeCodedIntraSample(sample CompressedSample) (Yuv420Frame, error) {
	slices, err := decoder.nativeSlices(sample)
	if err != nil {
		return Yuv420Frame{}, err
	}
	first := slices[0]
	if first.header.SliceType%5 != 2 {
		return Yuv420Frame{}, ErrDecoderUnsupportedFeature
	}
	builder, err := NewYuv420FrameBuilder(int(first.sps.CodedWidth/16), int(first.sps.CodedHeight/16), 0, 0, 0, 0)
	if err != nil {
		return Yuv420Frame{}, err
	}
	history := make([]*CABACIIntraMacroblockResult, len(builder.written))
	decoder.nativeBlocks = make([]nativeMacroblock, len(history))
	for _, slice := range slices {
		qpy := int(26 + slice.pps.PicInitQpMinus26 + slice.header.SliceQPDelta)
		contexts, err := NewCABACSliceContexts(slice.header.SliceType, slice.header.CabacInitIDC, qpy)
		if err != nil {
			return Yuv420Frame{}, err
		}
		if (slice.pps.EntropyCodingMode && slice.header.SliceDataBitOffset%8 != 0) || slice.header.SliceDataBitOffset/8 >= uint64(len(slice.rbsp)) {
			return Yuv420Frame{}, ErrBitstreamExhausted
		}
		var arithmetic *CABACArithmeticDecoder
		if slice.pps.EntropyCodingMode {
			arithmetic, err = NewCABACArithmeticDecoder(slice.rbsp[slice.header.SliceDataBitOffset/8:])
		} else {
			arithmetic, err = newCAVLCSyntaxDecoder(slice.rbsp, slice.header.SliceDataBitOffset)
		}
		if err != nil {
			return Yuv420Frame{}, err
		}
		previousDelta := 0
		coefficientCounts := make([][24]uint8, 0)
		for address := int(slice.header.FirstMacroblockInSlice); ; address++ {
			if address >= len(history) {
				return Yuv420Frame{}, fmt.Errorf("slice exceeds the picture")
			}
			if arithmetic.cavlc != nil {
				arithmetic.cavlc.begin(address, int(slice.header.FirstMacroblockInSlice), builder.pictureWidthInMbs, coefficientCounts)
			}
			input := nativeIntraInput(slice, address, history, qpy, previousDelta, true)
			result, err := arithmetic.DecodeIIntraMacroblock(input, &contexts, builder, address)
			if err != nil {
				return Yuv420Frame{}, fmt.Errorf("macroblock %d: %w", address, err)
			}
			history[address] = &result
			decoder.nativeBlocks[address] = nativeMacroblock{result: result, intra: true, slice: slice}
			qpy, previousDelta = result.QPY, result.QPDelta
			if arithmetic.cavlc != nil {
				coefficientCounts = append(coefficientCounts, arithmetic.cavlc.counts)
			}
			end, err := arithmetic.DecodeTerminateBin()
			if err != nil {
				return Yuv420Frame{}, err
			}
			if end {
				break
			}
		}
	}
	frame, err := builder.Finish()
	if err != nil {
		written := 0
		for _, complete := range builder.written {
			if complete {
				written++
			}
		}
		return Yuv420Frame{}, fmt.Errorf("native picture has %d of %d macroblocks: %w", written, len(builder.written), err)
	}
	return frame, nil
}

func cropNativeFrame(coded Yuv420Frame, sps SPSInfo) Yuv420Frame {
	width, height := int(sps.Width), int(sps.Height)
	frame := Yuv420Frame{Width: width, Height: height, YStride: width, UStride: width / 2, VStride: width / 2}
	planes := [3][]byte{coded.Y, coded.U, coded.V}
	packed := [3][]byte{}
	for component, plane := range planes {
		divisor := 1
		if component > 0 {
			divisor = 2
		}
		stride := coded.Width / divisor
		left, top := int(sps.FrameCropLeft)/divisor, int(sps.FrameCropTop)/divisor
		packed[component] = make([]byte, width/divisor*height/divisor)
		for row := 0; row < height/divisor; row++ {
			copy(packed[component][row*(width/divisor):], plane[(top+row)*stride+left:(top+row)*stride+left+width/divisor])
		}
	}
	frame.Y, frame.U, frame.V = packed[0], packed[1], packed[2]
	return frame
}

func nativeCoefficients(block *nativeMacroblock, raster int) bool {
	if block.result.TransformSize8x8 {
		return block.result.Luma8x8Nonzero[raster/8*2+(raster%4)/2]
	}
	return block.result.Luma4x4CodedBlockFlags[luma4x4BlockScanToRaster[raster]]
}

func nativePredictionDiffers(left, right [2]nativePrediction) bool {
	var first, second []nativePrediction
	for _, prediction := range left {
		if prediction.used {
			first = append(first, prediction)
		}
	}
	for _, prediction := range right {
		if prediction.used {
			second = append(second, prediction)
		}
	}
	if len(first) != len(second) {
		return true
	}
	match := func(left, right nativePrediction) bool {
		return left.reference == right.reference && absInt(int(left.motion.X)-int(right.motion.X)) < 4 && absInt(int(left.motion.Y)-int(right.motion.Y)) < 4
	}
	if len(first) == 0 {
		return false
	}
	if len(first) == 1 {
		return !match(first[0], second[0])
	}
	return !(match(first[0], second[0]) && match(first[1], second[1]) || match(first[0], second[1]) && match(first[1], second[0]))
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func deblockNativePicture(frame *Yuv420Frame, blocks []nativeMacroblock) error {
	for address := range blocks {
		if err := deblockNativeMacroblock(frame, blocks, address); err != nil {
			return err
		}
	}
	return nil
}

func deblockNativeMacroblock(frame *Yuv420Frame, blocks []nativeMacroblock, address int) error {
	width := frame.Width / 16
	current := &blocks[address]
	{
		header := current.slice.header
		if header.DisableDeblockingFilterIDC == 1 {
			return nil
		}
		originX, originY := address%width*16, address/width*16
		for direction := 0; direction < 2; direction++ {
			vertical := direction == 0
			for edge := 0; edge < 16; edge += 4 {
				neighborAddress := address
				if edge == 0 {
					if vertical {
						if originX == 0 {
							continue
						}
						neighborAddress--
					} else {
						if originY == 0 {
							continue
						}
						neighborAddress -= width
					}
				}
				neighbor := &blocks[neighborAddress]
				boundary := neighbor.slice.header.FirstMacroblockInSlice != header.FirstMacroblockInSlice
				if edge == 0 && boundary && header.DisableDeblockingFilterIDC == 2 {
					continue
				}
				parameters, err := DeriveLumaDeblockingParameters(neighbor.result.QPY, current.result.QPY, header.DisableDeblockingFilterIDC, header.SliceAlphaC0OffsetDiv2, header.SliceBetaOffsetDiv2)
				if err != nil {
					return err
				}
				var strengths [4]uint8
				for segment := 0; segment < 4; segment++ {
					leftRaster, rightRaster := segment*4+edge/4-1, segment*4+edge/4
					if !vertical {
						leftRaster, rightRaster = (edge/4-1)*4+segment, edge/4*4+segment
					}
					if edge == 0 {
						if vertical {
							leftRaster = segment*4 + 3
						} else {
							leftRaster = 12 + segment
						}
					}
					strength := uint8(0)
					if neighbor.intra || current.intra {
						strength = 3
						if edge == 0 {
							strength = 4
						}
					} else if nativeCoefficients(neighbor, leftRaster) || nativeCoefficients(current, rightRaster) {
						strength = 2
					} else if nativePredictionDiffers(neighbor.predictions[leftRaster], current.predictions[rightRaster]) {
						strength = 1
					}
					strengths[segment] = strength
				}
				if edge == 0 || !current.result.TransformSize8x8 || edge == 8 {
					x, y := originX, originY
					if vertical {
						x += edge
					} else {
						y += edge
					}
					if err := ApplyLumaDeblockingPlaneMacroblockEdge(frame.Y, frame.Width, frame.Height, frame.YStride, x, y, vertical, strengths, parameters, boundary); err != nil {
						return err
					}
				}
				if edge != 0 && edge != 8 {
					continue
				}
				for component, plane := range [][]byte{frame.U, frame.V} {
					offsetsCurrent := [2]int{int(current.slice.pps.ChromaQPIndexOffset), int(current.slice.pps.SecondChromaQPIndexOffset)}
					offsetsNeighbor := [2]int{int(neighbor.slice.pps.ChromaQPIndexOffset), int(neighbor.slice.pps.SecondChromaQPIndexOffset)}
					qpCurrent, err := deriveChromaQPC(current.result.QPY, offsetsCurrent[component])
					if err != nil {
						return err
					}
					qpNeighbor, err := deriveChromaQPC(neighbor.result.QPY, offsetsNeighbor[component])
					if err != nil {
						return err
					}
					chromaParameters, err := DeriveLumaDeblockingParameters(qpNeighbor, qpCurrent, header.DisableDeblockingFilterIDC, header.SliceAlphaC0OffsetDiv2, header.SliceBetaOffsetDiv2)
					if err != nil {
						return err
					}
					alpha, beta := lumaAlphaTable[chromaParameters.IndexA], lumaBetaTable[chromaParameters.IndexB]
					stride := frame.Width / 2
					step := 1
					if !vertical {
						step = stride
					}
					for position := 0; position < 8; position++ {
						strength := strengths[position/2]
						if strength == 0 {
							continue
						}
						x, y := originX/2, originY/2
						if vertical {
							x += edge / 2
							y += position
						} else {
							x += position
							y += edge / 2
						}
						offset := y*stride + x
						samples := ChromaEdgeSamples{P0: plane[offset-step], P1: plane[offset-2*step], Q0: plane[offset], Q1: plane[offset+step]}
						if absInt(int(samples.P0)-int(samples.Q0)) >= int(alpha) || absInt(int(samples.P1)-int(samples.P0)) >= int(beta) || absInt(int(samples.Q1)-int(samples.Q0)) >= int(beta) {
							continue
						}
						filtered := samples
						if strength == 4 {
							filtered.P0 = uint8((2*int(samples.P1) + int(samples.P0) + int(samples.Q1) + 2) >> 2)
							filtered.Q0 = uint8((2*int(samples.Q1) + int(samples.Q0) + int(samples.P1) + 2) >> 2)
						} else {
							filtered = FilterChromaWeakEdge(samples, lumaTC0Table[strength-1][chromaParameters.IndexA])
						}
						plane[offset-step], plane[offset] = filtered.P0, filtered.Q0
					}
				}
			}
		}
	}
	return nil
}

// nativeIntraInput reads neighbor samples only when samples is set, so inter parsing never races reconstruction workers.
func nativeIntraInput(slice nativeSlice, address int, history []*CABACIIntraMacroblockResult, qpy, previousDelta int, samples bool) CABACIIntraMacroblockInput {
	width := int(slice.sps.CodedWidth / 16)
	first := int(slice.header.FirstMacroblockInSlice)
	neighbor := func(index int) *CABACIIntraMacroblockResult {
		if index < first || index >= address || index < 0 {
			return nil
		}
		return history[index]
	}
	var left, top, topRight, topLeft *CABACIIntraMacroblockResult
	if address%width > 0 {
		left = neighbor(address - 1)
		topLeft = neighbor(address - width - 1)
	}
	top = neighbor(address - width)
	if address%width+1 < width {
		topRight = neighbor(address - width + 1)
	}
	input := CABACIIntraMacroblockInput{
		SliceType: slice.header.SliceType, PreviousQPY: qpy, PreviousQPDelta: previousDelta,
		LeftLumaCBP: 15, TopLumaCBP: 15, Transform8x8ModeEnabled: slice.pps.Transform8x8Mode,
		ChromaQPIndexOffsets: [2]int{int(slice.pps.ChromaQPIndexOffset), int(slice.pps.SecondChromaQPIndexOffset)},
	}
	for index := range input.Luma4x4ScalingList {
		input.Luma4x4ScalingList[index] = 16
	}
	for index := range input.Luma8x8ScalingList {
		input.Luma8x8ScalingList[index] = 16
	}
	for component := range input.ChromaScalingLists {
		for index := range input.ChromaScalingLists[component] {
			input.ChromaScalingLists[component][index] = 16
		}
	}
	scanForRaster := [16]int{0, 1, 4, 5, 2, 3, 6, 7, 8, 9, 12, 13, 10, 11, 14, 15}
	setEdge := func(result *CABACIIntraMacroblockResult, horizontal bool, luma *CABACIntra4x4EdgeState, dc *CABACIntra16x16EdgeState, chroma *[2]CABACChroma420EdgeState, modes4 *[4]uint8, modes8 *[2]uint8) {
		if result == nil {
			return
		}
		ipcm := result.MacroblockType == 25
		intra16 := result.MacroblockType > 0 && result.MacroblockType < 25
		*luma = CABACIntra4x4EdgeState{Available: true, IsIPCM: ipcm}
		*dc = CABACIntra16x16EdgeState{Available: true, IsIPCM: ipcm, DCTransformBlockAvailable: intra16, DCTransformBlockCoded: result.Luma16x16DCCoded}
		for block := 0; block < 4; block++ {
			raster := block*4 + 3
			if horizontal {
				raster = 12 + block
			}
			scan := scanForRaster[raster]
			group := raster/8*2 + (raster%4)/2
			available := result.CodedBlockPatternLuma&(1<<group) != 0
			luma.TransformBlockAvailable[block] = available
			luma.TransformBlockCoded[block] = result.Luma4x4CodedBlockFlags[scan]
			if result.TransformSize8x8 {
				luma.TransformBlockCoded[block] = available
			}
			dc.ACTransformBlockAvailable[block], dc.ACTransformBlockCoded[block] = available, luma.TransformBlockCoded[block]
			modes4[block] = 2
			if result.MacroblockType == 0 {
				if result.TransformSize8x8 {
					modes4[block] = result.Luma8x8Modes[group]
				} else {
					modes4[block] = result.Luma4x4Modes[scan]
				}
			}
		}
		for block := 0; block < 2; block++ {
			modes8[block] = modes4[block*2]
		}
		for component := 0; component < 2; component++ {
			chroma[component] = CABACChroma420EdgeState{Available: true, IsIPCM: ipcm, DCBlockAvailable: result.CodedBlockPatternChroma > 0, DCBlockCoded: result.ChromaDCCoded[component]}
			for block := 0; block < 2; block++ {
				raster := block*2 + 1
				if horizontal {
					raster = 2 + block
				}
				chroma[component].ACBlockAvailable[block] = result.CodedBlockPatternChroma == 2
				chroma[component].ACBlockCoded[block] = result.ChromaACCodedFlags[component][raster]
			}
		}
	}
	setEdge(left, false, &input.Luma4x4LeftEdge, &input.Luma16x16LeftEdge, &input.ChromaLeftEdges, &input.LeftModes4x4, &input.LeftModes8x8)
	setEdge(top, true, &input.Luma4x4TopEdge, &input.Luma16x16TopEdge, &input.ChromaTopEdges, &input.TopModes4x4, &input.TopModes8x8)
	input.LeftAvailable, input.LeftModeAvailable, input.Intra16x16LeftAvailable = left != nil, left != nil, left != nil
	input.TopAvailable, input.TopModeAvailable, input.Intra16x16TopAvailable = top != nil, top != nil, top != nil
	if left != nil {
		input.LeftIntra16OrPCM = left.MacroblockType > 0 && left.MacroblockType <= 25
		input.LeftHas8x8Transform = left.TransformSize8x8
		if left.MacroblockType != 25 {
			input.LeftLumaCBP = left.CodedBlockPatternLuma
		}
		input.LeftChromaCBP = left.CodedBlockPatternChroma
		if left.MacroblockType == 25 {
			input.LeftChromaCBP = 2
		}
		input.ChromaLeftModeNonzero = left.ChromaPredictionMode != 0
		if samples {
			for row := 0; row < 16; row++ {
				input.Intra16x16Left[row] = left.Luma[row*16+15]
			}
		}
	}
	if top != nil {
		input.TopIntra16OrPCM = top.MacroblockType > 0 && top.MacroblockType <= 25
		input.TopHas8x8Transform = top.TransformSize8x8
		if top.MacroblockType != 25 {
			input.TopLumaCBP = top.CodedBlockPatternLuma
		}
		input.TopChromaCBP = top.CodedBlockPatternChroma
		if top.MacroblockType == 25 {
			input.TopChromaCBP = 2
		}
		input.ChromaTopModeNonzero = top.ChromaPredictionMode != 0
		if samples {
			copy(input.Intra16x16Top[:], top.Luma[240:])
		}
	}
	if !samples {
		return input
	}
	if topLeft != nil {
		input.Intra16x16TopLeft, input.Intra16x16TopLeftAvailable = topLeft.Luma[255], true
	}
	for scan, raster := range luma4x4BlockScanToRaster {
		column, row := int(raster)%4*4, int(raster)/4*4
		block := &input.Luma4x4Blocks[scan]
		block.Mode = 2
		if row == 0 && top != nil {
			block.TopAvailable = true
			for index := range block.Top {
				position := column + index
				if position < 16 {
					block.Top[index] = top.Luma[240+position]
				} else if topRight != nil {
					block.Top[index] = topRight.Luma[240+position-16]
				} else {
					block.Top[index] = top.Luma[255]
				}
			}
		}
		if column == 0 && left != nil {
			block.LeftAvailable = true
			for index := range block.Left {
				block.Left[index] = left.Luma[min(row+index, 15)*16+15]
			}
		}
		if row == 0 && column > 0 && top != nil {
			block.TopLeft, block.TopLeftAvailable = top.Luma[240+column-1], true
		}
		if column == 0 && row > 0 && left != nil {
			block.TopLeft, block.TopLeftAvailable = left.Luma[(row-1)*16+15], true
		}
		if column == 0 && row == 0 && topLeft != nil {
			block.TopLeft, block.TopLeftAvailable = topLeft.Luma[255], true
		}
	}
	for blockIndex := range input.Luma8x8Blocks {
		column, row := blockIndex%2*8, blockIndex/2*8
		block := &input.Luma8x8Blocks[blockIndex]
		block.Mode = 2
		if row == 0 && top != nil {
			block.TopAvailable = true
			for index := range block.Top {
				position := column + index
				block.Top[index] = top.Luma[255]
				if position < 16 {
					block.Top[index] = top.Luma[240+position]
				} else if topRight != nil {
					block.Top[index] = topRight.Luma[240+position-16]
				}
			}
		}
		if column == 0 && left != nil {
			block.LeftAvailable = true
			for index := range block.Left {
				block.Left[index] = left.Luma[min(row+index, 15)*16+15]
			}
		}
		if row == 0 && column > 0 && top != nil {
			block.TopLeft, block.TopLeftAvailable = top.Luma[240+column-1], true
		}
		if column == 0 && row > 0 && left != nil {
			block.TopLeft, block.TopLeftAvailable = left.Luma[(row-1)*16+15], true
		}
		if column == 0 && row == 0 && topLeft != nil {
			block.TopLeft, block.TopLeftAvailable = topLeft.Luma[255], true
		}
	}
	for component := 0; component < 2; component++ {
		references := &input.ChromaReferences[component]
		if top != nil {
			values := top.Cb
			if component == 1 {
				values = top.Cr
			}
			references.TopAvailable = true
			copy(references.Top[:], values[56:])
		}
		if left != nil {
			values := left.Cb
			if component == 1 {
				values = left.Cr
			}
			references.LeftAvailable = true
			for row := range references.Left {
				references.Left[row] = values[row*8+7]
			}
		}
		if topLeft != nil {
			values := topLeft.Cb
			if component == 1 {
				values = topLeft.Cr
			}
			references.TopLeftAvailable, references.TopLeft = true, values[63]
		}
	}
	return input
}
