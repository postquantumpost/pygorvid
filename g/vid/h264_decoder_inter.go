package vid

import "fmt"

type nativePartition struct {
	x, y, width, height, group int
	lists                      [2]bool
	refs                       [2]int32
	direct                     bool
}

func nativePPartitions(mbType uint8, subTypes [4]uint8) ([]nativePartition, error) {
	partition := func(x, y, width, height, group int) nativePartition {
		return nativePartition{x: x, y: y, width: width, height: height, group: group, lists: [2]bool{true, false}, refs: [2]int32{-1, -1}}
	}
	switch mbType {
	case 0:
		return []nativePartition{partition(0, 0, 16, 16, 0)}, nil
	case 1:
		return []nativePartition{partition(0, 0, 16, 8, 0), partition(0, 8, 16, 8, 1)}, nil
	case 2:
		return []nativePartition{partition(0, 0, 8, 16, 0), partition(8, 0, 8, 16, 1)}, nil
	case 3, 4:
		var parts []nativePartition
		for group, subType := range subTypes {
			x, y := group%2*8, group/2*8
			switch subType {
			case 0:
				parts = append(parts, partition(x, y, 8, 8, group))
			case 1:
				parts = append(parts, partition(x, y, 8, 4, group), partition(x, y+4, 8, 4, group))
			case 2:
				parts = append(parts, partition(x, y, 4, 8, group), partition(x+4, y, 4, 8, group))
			case 3:
				for row := 0; row < 2; row++ {
					for column := 0; column < 2; column++ {
						parts = append(parts, partition(x+column*4, y+row*4, 4, 4, group))
					}
				}
			default:
				return nil, ErrDecoderUnsupportedFeature
			}
		}
		return parts, nil
	}
	return nil, ErrDecoderUnsupportedFeature
}

func (decoder *H264Decoder) nativeNeighbor(slice nativeSlice, address, x, y, list int) (nativePrediction, CABACInterNeighbor, MotionVectorCandidate) {
	width := int(slice.sps.CodedWidth) / 16
	height := int(slice.sps.CodedHeight) / 16
	globalX, globalY := address%width*16+x, address/width*16+y
	if globalX < 0 || globalY < 0 || globalX >= width*16 || globalY >= height*16 {
		return nativePrediction{}, CABACInterNeighbor{}, MotionVectorCandidate{RefIdx: -1}
	}
	neighborAddress := globalY/16*width + globalX/16
	if neighborAddress < int(slice.header.FirstMacroblockInSlice) || neighborAddress > address {
		return nativePrediction{}, CABACInterNeighbor{}, MotionVectorCandidate{RefIdx: -1}
	}
	block := &decoder.nativeBlocks[neighborAddress]
	if block.slice.header.FirstMacroblockInSlice != slice.header.FirstMacroblockInSlice {
		return nativePrediction{}, CABACInterNeighbor{}, MotionVectorCandidate{RefIdx: -1}
	}
	prediction := block.predictions[(globalY%16)/4*4+(globalX%16)/4][list]
	if neighborAddress == address && !prediction.referenceDecoded {
		return nativePrediction{}, CABACInterNeighbor{}, MotionVectorCandidate{RefIdx: -1}
	}
	fact := CABACInterNeighbor{Available: true, Skip: block.skipped || block.direct || prediction.direct, Intra: block.intra, PredictionModeMatches: prediction.used, ReferenceIndex: uint8(max(prediction.refIndex, 0)), MotionVectorDifference: [2]int32{prediction.difference.X, prediction.difference.Y}}
	candidate := MotionVectorCandidate{Available: true, RefIdx: -1}
	if prediction.used && prediction.motionDecoded {
		candidate.RefIdx, candidate.Vector = prediction.refIndex, prediction.motion
	}
	if neighborAddress == address && !prediction.motionDecoded {
		candidate.Available = false
	}
	return prediction, fact, candidate
}

func (decoder *H264Decoder) nativeNeighbors(slice nativeSlice, address int, part nativePartition, list int) ([4]CABACInterNeighbor, [4]MotionVectorCandidate) {
	coordinates := [4][2]int{{part.x - 1, part.y}, {part.x, part.y - 1}, {part.x + part.width, part.y - 1}, {part.x - 1, part.y - 1}}
	var facts [4]CABACInterNeighbor
	var candidates [4]MotionVectorCandidate
	for index, coordinate := range coordinates {
		_, facts[index], candidates[index] = decoder.nativeNeighbor(slice, address, coordinate[0], coordinate[1], list)
	}
	return facts, candidates
}

func nativeMotion(part nativePartition, refIndex int32, candidates [4]MotionVectorCandidate, difference MotionVector) MotionVector {
	left, top, right, corner := candidates[0], candidates[1], candidates[2], candidates[3]
	if !right.Available {
		right = corner
	}
	var predicted MotionVector
	special := false
	if part.width == 16 && part.height == 8 {
		candidate := top
		if part.y > 0 {
			candidate = left
		}
		if candidate.Available && candidate.RefIdx == refIndex {
			predicted, special = candidate.Vector, true
		}
	} else if part.width == 8 && part.height == 16 {
		candidate := left
		if part.x > 0 {
			candidate = right
		}
		if candidate.Available && candidate.RefIdx == refIndex {
			predicted, special = candidate.Vector, true
		}
	}
	if !special {
		if left.Available && !top.Available && !right.Available {
			predicted = left.Vector
		} else {
			predicted = PredictMotionVector(refIndex, left, top, right, corner)
		}
	}
	return ApplyMotionVectorDifference(predicted, difference)
}

func setNativePrediction(block *nativeMacroblock, part nativePartition, list int, prediction nativePrediction) {
	for row := part.y / 4; row < (part.y+part.height)/4; row++ {
		for column := part.x / 4; column < (part.x+part.width)/4; column++ {
			block.predictions[row*4+column][list] = prediction
		}
	}
}

func compensateNativePartition(result *CABACIIntraMacroblockResult, frame Yuv420Frame, address, pictureWidth int, part nativePartition, motion MotionVector, average bool) error {
	originX, originY := address%pictureWidth*16, address/pictureWidth*16
	for row := part.y; row < part.y+part.height; row++ {
		for column := part.x; column < part.x+part.width; column++ {
			value, err := interpolateLumaFractionalSample(frame.Y, frame.Width, frame.Height, frame.YStride, originX+column+int(motion.X>>2), originY+row+int(motion.Y>>2), uint8(motion.X&3), uint8(motion.Y&3))
			if err != nil {
				return err
			}
			index := row*16 + column
			if average {
				value = uint8((uint16(value) + uint16(result.Luma[index]) + 1) >> 1)
			}
			result.Luma[index] = value
		}
	}
	for component, plane := range [][]byte{frame.U, frame.V} {
		for row := part.y / 2; row < (part.y+part.height)/2; row++ {
			for column := part.x / 2; column < (part.x+part.width)/2; column++ {
				value, err := interpolateChromaFractionalSample(plane, frame.Width/2, frame.Height/2, frame.Width/2, originX/2+column+int(motion.X>>3), originY/2+row+int(motion.Y>>3), uint8(motion.X&7), uint8(motion.Y&7))
				if err != nil {
					return err
				}
				index := row*8 + column
				target := &result.Cb[index]
				if component == 1 {
					target = &result.Cr[index]
				}
				if average {
					value = uint8((uint16(value) + uint16(*target) + 1) >> 1)
				}
				*target = value
			}
		}
	}
	return nil
}

func (decoder *H264Decoder) nativeReferenceLists(slice nativeSlice, poc int64) ([2][]DecodedReferencePicture, error) {
	var lists [2][]ReferencePicture
	var decoded [2][]DecodedReferencePicture
	references := decoder.referencePictureBuffer.References()
	if slice.header.SliceType%5 == 0 {
		list, err := BuildPReferenceList(references, slice.header.FrameNum, 1<<(slice.sps.Log2MaxFrameNumMinus4+4))
		if err != nil {
			return decoded, err
		}
		lists[0] = list
	} else {
		initial := BuildBReferenceLists(references, poc)
		lists[0], lists[1] = initial.List0, initial.List1
	}
	for index := range lists {
		modifications := slice.header.RefPicListL0
		if index == 1 {
			modifications = slice.header.RefPicListL1
		}
		modified, err := ApplyReferenceListModifications(lists[index], references, slice.header.FrameNum, 1<<(slice.sps.Log2MaxFrameNumMinus4+4), modifications)
		if err != nil {
			return decoded, err
		}
		for _, reference := range modified {
			stored, ok := decoder.referencePictureBuffer.Get(reference.ID)
			if !ok {
				return decoded, fmt.Errorf("reference picture %d is unavailable", reference.ID)
			}
			decoded[index] = append(decoded[index], stored)
		}
	}
	return decoded, nil
}

func addNativeResidual(samples *uint8, residual int64) {
	value := int64(*samples)
	if residual > 255-value {
		*samples = 255
	} else if residual < -value {
		*samples = 0
	} else {
		*samples = uint8(value + residual)
	}
}

func decodeNativeInterResidual(arithmetic *CABACArithmeticDecoder, contexts *[CABACContextCount]CABACContextModel, input CABACIIntraMacroblockInput, result *CABACIIntraMacroblockResult, output *nativeInterResidual) error {
	if result.TransformSize8x8 {
		for block := 0; block < 4; block++ {
			if result.CodedBlockPatternLuma&(1<<block) == 0 {
				continue
			}
			levels, err := arithmetic.DecodeLuma8x8ResidualBlock(contexts)
			if err != nil {
				return err
			}
			coefficients, err := inverseScaleLuma8x8(PlaceLuma8x8ScanLevels(levels), input.Luma8x8ScalingList, result.QPY)
			if err != nil {
				return err
			}
			residual := inverseTransformLuma8x8(coefficients)
			for row := 0; row < 8; row++ {
				for column := 0; column < 8; column++ {
					output.luma[(block/2*8+row)*16+block%2*8+column] = residual[row*8+column]
				}
			}
		}
	} else {
		for scan, raster := range luma4x4BlockScanToRaster {
			column, row := int(raster)%4, int(raster)/4
			if result.CodedBlockPatternLuma&(1<<(row/2*2+column/2)) == 0 {
				continue
			}
			left := deriveIntra4x4LumaCondTerm(scan, column, row, true, result.Luma4x4CodedBlockFlags, input.Luma4x4LeftEdge, false)
			top := deriveIntra4x4LumaCondTerm(scan, column, row, false, result.Luma4x4CodedBlockFlags, input.Luma4x4TopEdge, false)
			levels, coded, err := arithmetic.DecodeResidualBlock(2, left, top, contexts)
			if err != nil {
				return err
			}
			result.Luma4x4CodedBlockFlags[scan] = coded
			if coded {
				residual, err := reconstructLuma4x4Residual(PlaceLuma4x4ScanLevels(levels), input.Luma4x4ScalingList, result.QPY)
				if err != nil {
					return err
				}
				for blockRow := 0; blockRow < 4; blockRow++ {
					for blockColumn := 0; blockColumn < 4; blockColumn++ {
						output.luma[(row*4+blockRow)*16+column*4+blockColumn] = residual[blockRow*4+blockColumn]
					}
				}
			}
		}
	}
	var dc [2][4]int64
	var qpc [2]int
	for component := 0; component < 2; component++ {
		var err error
		qpc[component], err = deriveChromaQPC(result.QPY, input.ChromaQPIndexOffsets[component])
		if err != nil {
			return err
		}
		if result.CodedBlockPatternChroma == 0 {
			continue
		}
		left, top := input.ChromaLeftEdges[component], input.ChromaTopEdges[component]
		condLeft := DeriveCABACCodedBlockFlagCondTerm(left.Available, false, left.IsIPCM, left.DCBlockAvailable, left.DCBlockCoded)
		condTop := DeriveCABACCodedBlockFlagCondTerm(top.Available, false, top.IsIPCM, top.DCBlockAvailable, top.DCBlockCoded)
		levels, coded, err := arithmetic.DecodeResidualBlock(3, condLeft, condTop, contexts)
		if err != nil {
			return err
		}
		result.ChromaDCCoded[component] = coded
		if coded {
			dc[component], err = inverseScaleChromaDC2x2(inverseTransformChromaDC2x2([4]int32(levels[:4])), qpc[component])
			if err != nil {
				return err
			}
		}
	}
	for component := 0; component < 2; component++ {
		var residualBlocks [4][16]int64
		for block := 0; block < 4; block++ {
			var ac [15]int32
			if result.CodedBlockPatternChroma == 2 {
				condition := func(left bool) bool {
					coordinate, edgeCoordinate := block/2, block%2
					edge := input.ChromaTopEdges[component]
					neighbor := block - 2
					if left {
						coordinate, edgeCoordinate = block%2, block/2
						edge = input.ChromaLeftEdges[component]
						neighbor = block - 1
					}
					if coordinate > 0 {
						return result.ChromaACCodedFlags[component][neighbor]
					}
					return DeriveCABACCodedBlockFlagCondTerm(edge.Available, false, edge.IsIPCM, edge.ACBlockAvailable[edgeCoordinate], edge.ACBlockCoded[edgeCoordinate])
				}
				levels, coded, err := arithmetic.DecodeResidualBlock(4, condition(true), condition(false), contexts)
				if err != nil {
					return err
				}
				result.ChromaACCodedFlags[component][block] = coded
				copy(ac[:], levels[:15])
			}
			residual, err := reconstructChroma4x4Residual(dc[component][block], ac, input.ChromaScalingLists[component], qpc[component])
			if err != nil {
				return err
			}
			residualBlocks[block] = residual
		}
		residual := assembleChroma420ResidualMacroblock(residualBlocks)
		copy(output.chroma[component][:], residual[:])
	}
	return nil
}

func (decoder *H264Decoder) decodeNativeInterSample(sample CompressedSample, poc int64) (Yuv420Frame, error) {
	slices, err := decoder.nativeSlices(sample)
	if err != nil {
		return Yuv420Frame{}, err
	}
	first := slices[0]
	if first.header.SliceType%5 != 0 && first.header.SliceType%5 != 1 {
		return Yuv420Frame{}, ErrDecoderUnsupportedFeature
	}
	width, height := int(first.sps.CodedWidth/16), int(first.sps.CodedHeight/16)
	builder, err := NewYuv420FrameBuilder(width, height, 0, 0, 0, 0)
	if err != nil {
		return Yuv420Frame{}, err
	}
	history := make([]*CABACIIntraMacroblockResult, width*height)
	decoder.nativeBlocks = make([]nativeMacroblock, len(history))
	pool := newNativeReconstructionPool(decoder.threads, len(history))
	defer pool.finish()
	for _, slice := range slices {
		bSlice := slice.header.SliceType%5 == 1
		lists, err := decoder.nativeReferenceLists(slice, poc)
		if err != nil {
			return Yuv420Frame{}, err
		}
		qpy, previousDelta := int(26+slice.pps.PicInitQpMinus26+slice.header.SliceQPDelta), 0
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
		coefficientCounts := make([][24]uint8, 0)
		for address := int(slice.header.FirstMacroblockInSlice); ; address++ {
			if address >= len(history) {
				return Yuv420Frame{}, fmt.Errorf("slice exceeds the picture")
			}
			if arithmetic.cavlc != nil {
				arithmetic.cavlc.begin(address, int(slice.header.FirstMacroblockInSlice), builder.pictureWidthInMbs, coefficientCounts)
			}
			input := nativeIntraInput(slice, address, history, qpy, previousDelta, false)
			current := nativeMacroblock{slice: slice, result: CABACIIntraMacroblockResult{MacroblockType: 255, QPY: qpy}}
			var compensations []nativeCompensation
			var residual *nativeInterResidual
			decoder.nativeBlocks[address] = current
			leftSkipped, topSkipped := false, false
			if input.LeftAvailable {
				leftSkipped = decoder.nativeBlocks[address-1].skipped
			}
			if input.TopAvailable {
				topSkipped = decoder.nativeBlocks[address-width].skipped
			}
			skipOffset := 11
			if bSlice {
				skipOffset = 24
			}
			var skipContexts [3]CABACContextModel
			copy(skipContexts[:], contexts[skipOffset:skipOffset+3])
			skipped, err := arithmetic.DecodeMBSkipFlag(slice.header.SliceType, &skipContexts, input.LeftAvailable, leftSkipped, input.TopAvailable, topSkipped)
			if err != nil {
				return Yuv420Frame{}, err
			}
			copy(contexts[skipOffset:skipOffset+3], skipContexts[:])
			current.skipped = skipped
			if skipped && !bSlice {
				if len(lists[0]) == 0 {
					return Yuv420Frame{}, fmt.Errorf("P-skip has no reference picture")
				}
				part := nativePartition{width: 16, height: 16, lists: [2]bool{true, false}, refs: [2]int32{0, -1}}
				_, candidates := decoder.nativeNeighbors(slice, address, part, 0)
				motion := MotionVector{}
				if candidates[0].Available && candidates[1].Available && !(candidates[0].RefIdx == 0 && candidates[0].Vector == (MotionVector{})) && !(candidates[1].RefIdx == 0 && candidates[1].Vector == (MotionVector{})) {
					motion = nativeMotion(part, 0, candidates, MotionVector{})
				}
				prediction := nativePrediction{used: true, reference: lists[0][0].Reference.ID, refIndex: 0, motion: motion, referenceDecoded: true, motionDecoded: true}
				setNativePrediction(&current, part, 0, prediction)
				compensations = append(compensations, nativeCompensation{frame: lists[0][0].Frame, part: part, motion: motion})
				previousDelta = 0
			} else if skipped {
				current.direct = true
				parts, err := nativeBPartitions(0, [4]uint8{}, slice.sps.Direct8x8Inference)
				if err != nil {
					return Yuv420Frame{}, err
				}
				if err := decoder.compensateNativeDirect(&current, &compensations, slice, address, poc, parts, lists, true); err != nil {
					return Yuv420Frame{}, err
				}
				previousDelta = 0
			} else {
				leftDirect, topDirect := leftSkipped, topSkipped
				if input.LeftAvailable {
					leftDirect = leftDirect || decoder.nativeBlocks[address-1].direct
				}
				if input.TopAvailable {
					topDirect = topDirect || decoder.nativeBlocks[address-width].direct
				}
				mbType, err := arithmetic.DecodeInterMBType(slice.header.SliceType, &contexts, input.LeftAvailable, input.TopAvailable, leftDirect, topDirect)
				if err != nil {
					return Yuv420Frame{}, err
				}
				intraOffset := uint8(5)
				if bSlice {
					intraOffset = 23
				}
				if mbType >= intraOffset {
					intraType := mbType - intraOffset
					for _, neighbor := range [4]int{address - 1, address - width - 1, address - width, address - width + 1} {
						pool.waitFor(neighbor)
					}
					input = nativeIntraInput(slice, address, history, qpy, previousDelta, true)
					current.result, err = arithmetic.decodeIntraMacroblock(input, &contexts, builder, address, &intraType)
					if err != nil {
						return Yuv420Frame{}, err
					}
					current.intra = true
					qpy, previousDelta = current.result.QPY, current.result.QPDelta
				} else {
					var subTypes [4]uint8
					if (!bSlice && (mbType == 3 || mbType == 4)) || (bSlice && mbType == 22) {
						for index := range subTypes {
							subTypes[index], err = arithmetic.DecodeSubMBType(slice.header.SliceType, &contexts)
							if err != nil {
								return Yuv420Frame{}, err
							}
						}
					}
					var parts []nativePartition
					if bSlice {
						parts, err = nativeBPartitions(mbType, subTypes, slice.sps.Direct8x8Inference)
					} else {
						parts, err = nativePPartitions(mbType, subTypes)
					}
					if err != nil {
						return Yuv420Frame{}, err
					}
					current.direct = bSlice && mbType == 0
					if err := decoder.compensateNativeDirect(&current, &compensations, slice, address, poc, parts, lists, current.direct); err != nil {
						return Yuv420Frame{}, err
					}
					for list := 0; list < 2; list++ {
						for index := range parts {
							part := &parts[index]
							if part.direct || !part.lists[list] {
								continue
							}
							if index > 0 && parts[index-1].group == part.group {
								part.refs[list] = parts[index-1].refs[list]
							} else {
								maximum := slice.header.NumRefIdxL0ActiveMinus1
								if list == 1 {
									maximum = slice.header.NumRefIdxL1ActiveMinus1
								}
								facts, _ := decoder.nativeNeighbors(slice, address, *part, list)
								var refContexts [6]CABACContextModel
								copy(refContexts[:], contexts[54:60])
								refIndex := uint8(0)
								if bSlice || mbType != 4 {
									refIndex, err = arithmetic.DecodeReferenceIndexForPartition(maximum, facts[0], facts[1], false, false, &refContexts)
									if err != nil {
										return Yuv420Frame{}, err
									}
								}
								copy(contexts[54:60], refContexts[:])
								part.refs[list] = int32(refIndex)
							}
							if part.refs[list] < 0 || int(part.refs[list]) >= len(lists[list]) {
								return Yuv420Frame{}, fmt.Errorf("partition references an unavailable picture")
							}
							prediction := nativePrediction{used: true, refIndex: part.refs[list], reference: lists[list][part.refs[list]].Reference.ID, referenceDecoded: true}
							setNativePrediction(&decoder.nativeBlocks[address], *part, list, prediction)
						}
					}
					for list := 0; list < 2; list++ {
						for _, part := range parts {
							if part.direct || !part.lists[list] {
								continue
							}
							facts, candidates := decoder.nativeNeighbors(slice, address, part, list)
							var difference MotionVector
							for component := 0; component < 2; component++ {
								offset := 40 + component*7
								var mvdContexts [7]CABACContextModel
								copy(mvdContexts[:], contexts[offset:offset+7])
								value, err := arithmetic.DecodeMotionVectorDifferenceForPartition(uint8(component), facts[0], facts[1], false, false, &mvdContexts)
								if err != nil {
									return Yuv420Frame{}, err
								}
								copy(contexts[offset:offset+7], mvdContexts[:])
								if component == 0 {
									difference.X = value
								} else {
									difference.Y = value
								}
							}
							motion := nativeMotion(part, part.refs[list], candidates, difference)
							prediction := nativePrediction{used: true, refIndex: part.refs[list], reference: lists[list][part.refs[list]].Reference.ID, motion: motion, difference: difference, referenceDecoded: true, motionDecoded: true}
							setNativePrediction(&decoder.nativeBlocks[address], part, list, prediction)
							setNativePrediction(&current, part, list, prediction)
							compensations = append(compensations, nativeCompensation{frame: lists[list][part.refs[list]].Frame, part: part, motion: motion, average: list == 1 && part.lists[0]})
						}
					}
					var lumaContexts [4]CABACContextModel
					copy(lumaContexts[:], contexts[73:77])
					current.result.CodedBlockPatternLuma, err = arithmetic.DecodeLumaCodedBlockPattern(input.LeftLumaCBP, input.TopLumaCBP, &lumaContexts)
					if err != nil {
						return Yuv420Frame{}, err
					}
					copy(contexts[73:77], lumaContexts[:])
					var chromaContexts [8]CABACContextModel
					copy(chromaContexts[:], contexts[77:85])
					current.result.CodedBlockPatternChroma, err = arithmetic.DecodeChromaCodedBlockPattern(input.LeftChromaCBP, input.TopChromaCBP, &chromaContexts)
					if err != nil {
						return Yuv420Frame{}, err
					}
					copy(contexts[77:85], chromaContexts[:])
					allow8x8 := slice.pps.Transform8x8Mode && current.result.CodedBlockPatternLuma != 0
					for _, part := range parts {
						if part.width < 8 || part.height < 8 {
							allow8x8 = false
						}
					}
					if allow8x8 {
						var transformContexts [3]CABACContextModel
						copy(transformContexts[:], contexts[399:402])
						current.result.TransformSize8x8, err = arithmetic.DecodeTransformSize8x8Flag(&transformContexts, input.LeftHas8x8Transform, input.TopHas8x8Transform)
						if err != nil {
							return Yuv420Frame{}, err
						}
						copy(contexts[399:402], transformContexts[:])
					}
					previousDelta = 0
					if current.result.CodedBlockPatternLuma != 0 || current.result.CodedBlockPatternChroma != 0 {
						var qpContexts [4]CABACContextModel
						copy(qpContexts[:], contexts[60:64])
						delta, err := arithmetic.DecodeMBQPDelta(&qpContexts, input.PreviousQPDelta)
						if err != nil {
							return Yuv420Frame{}, err
						}
						copy(contexts[60:64], qpContexts[:])
						qpy = (qpy + delta + 52) % 52
						current.result.QPDelta, previousDelta = delta, delta
					}
					current.result.QPY = qpy
					residual = &nativeInterResidual{}
					if err := decodeNativeInterResidual(arithmetic, &contexts, input, &current.result, residual); err != nil {
						return Yuv420Frame{}, fmt.Errorf("inter residual at macroblock %d: %w", address, err)
					}
				}
			}
			decoder.nativeBlocks[address] = current
			history[address] = &decoder.nativeBlocks[address].result
			if !current.intra {
				job := &nativeReconstruction{result: history[address], builder: builder, address: address, width: width, compensations: compensations, residual: residual}
				if err := pool.submit(job); err != nil {
					return Yuv420Frame{}, err
				}
			}
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
	if err := pool.finish(); err != nil {
		return Yuv420Frame{}, err
	}
	frame, err := builder.Finish()
	if err != nil {
		written := 0
		for _, complete := range builder.written {
			if complete {
				written++
			}
		}
		return Yuv420Frame{}, fmt.Errorf("native inter picture has %d of %d macroblocks: %w", written, len(builder.written), err)
	}
	return frame, nil
}
