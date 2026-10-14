package vid

import "fmt"

func nativeBPartitions(mbType uint8, subTypes [4]uint8, direct8x8 bool) ([]nativePartition, error) {
	partition := func(x, y, width, height, group int, lists [2]bool, direct bool) nativePartition {
		return nativePartition{x: x, y: y, width: width, height: height, group: group, lists: lists, refs: [2]int32{-1, -1}, direct: direct}
	}
	appendSub := func(parts []nativePartition, group int, subType uint8) ([]nativePartition, error) {
		width, height := 8, 8
		lists, direct := [2]bool{}, false
		switch subType {
		case 0:
			lists, direct = [2]bool{true, true}, true
			if !direct8x8 {
				width, height = 4, 4
			}
		case 1:
			lists = [2]bool{true, false}
		case 2:
			lists = [2]bool{false, true}
		case 3:
			lists = [2]bool{true, true}
		case 4, 5:
			lists = [2]bool{true, false}
			if subType == 4 {
				height = 4
			} else {
				width = 4
			}
		case 6, 7:
			lists = [2]bool{false, true}
			if subType == 6 {
				height = 4
			} else {
				width = 4
			}
		case 8, 9:
			lists = [2]bool{true, true}
			if subType == 8 {
				height = 4
			} else {
				width = 4
			}
		case 10, 11, 12:
			width, height = 4, 4
			lists = [2]bool{subType != 11, subType != 10}
		default:
			return nil, ErrDecoderUnsupportedFeature
		}
		for row := 0; row < 8; row += height {
			for column := 0; column < 8; column += width {
				parts = append(parts, partition(group%2*8+column, group/2*8+row, width, height, group, lists, direct))
			}
		}
		return parts, nil
	}
	if mbType == 0 || mbType == 22 {
		var parts []nativePartition
		for group := 0; group < 4; group++ {
			subType := uint8(0)
			if mbType == 22 {
				subType = subTypes[group]
			}
			var err error
			parts, err = appendSub(parts, group, subType)
			if err != nil {
				return nil, err
			}
		}
		return parts, nil
	}
	if mbType <= 3 {
		return []nativePartition{partition(0, 0, 16, 16, 0, [2]bool{mbType != 2, mbType != 1}, false)}, nil
	}
	if mbType > 21 {
		return nil, ErrDecoderUnsupportedFeature
	}
	modePairs := [9][2][2]bool{
		{{true, false}, {true, false}}, {{false, true}, {false, true}}, {{true, false}, {false, true}},
		{{false, true}, {true, false}}, {{true, false}, {true, true}}, {{false, true}, {true, true}},
		{{true, true}, {true, false}}, {{true, true}, {false, true}}, {{true, true}, {true, true}},
	}
	modes := modePairs[(mbType-4)/2]
	if mbType%2 == 0 {
		return []nativePartition{partition(0, 0, 16, 8, 0, modes[0], false), partition(0, 8, 16, 8, 1, modes[1], false)}, nil
	}
	return []nativePartition{partition(0, 0, 8, 16, 0, modes[0], false), partition(8, 0, 8, 16, 1, modes[1], false)}, nil
}

func nativeMinPositive(left, right int32) int32 {
	if left >= 0 && right >= 0 {
		return min(left, right)
	}
	return max(left, right)
}

func (decoder *H264Decoder) compensateNativeDirect(current *nativeMacroblock, slice nativeSlice, address int, poc int64, parts []nativePartition, lists [2][]DecodedReferencePicture, whole bool) error {
	for _, part := range parts {
		if !part.direct {
			continue
		}
		predictions, err := decoder.nativeBDirect(slice, address, poc, part, lists, whole)
		if err != nil {
			return err
		}
		active := false
		for list, prediction := range predictions {
			if !prediction.used {
				continue
			}
			prediction.direct = true
			setNativePrediction(current, part, list, prediction)
			setNativePrediction(&decoder.nativeBlocks[address], part, list, prediction)
			if err := compensateNativePartition(&current.result, lists[list][prediction.refIndex].Frame, address, int(slice.sps.CodedWidth)/16, part, prediction.motion, active); err != nil {
				return err
			}
			active = true
		}
	}
	return nil
}

func (decoder *H264Decoder) nativeBDirect(slice nativeSlice, address int, poc int64, part nativePartition, lists [2][]DecodedReferencePicture, whole bool) ([2]nativePrediction, error) {
	var predictions [2]nativePrediction
	if len(lists[0]) == 0 || len(lists[1]) == 0 {
		return predictions, fmt.Errorf("B direct reference list is empty")
	}
	colocated := nativePrediction{refIndex: -1}
	colocatedIntra := true
	if blocks := decoder.nativeReferenceBlocks[lists[1][0].Reference.ID]; address < len(blocks) {
		block := blocks[address]
		colocatedIntra = block.intra
		raster := part.y/4*4 + part.x/4
		if slice.sps.Direct8x8Inference {
			raster = int(luma4x4BlockScanToRaster[5*(part.y/8*2+part.x/8)])
		}
		colocated = block.predictions[raster][0]
		if !colocated.used {
			colocated = block.predictions[raster][1]
		}
	}
	if slice.header.DirectSpatialMVPred {
		geometry := part
		geometry.width = 16
		if whole {
			geometry.x, geometry.y, geometry.height = 0, 0, 16
		}
		var references [2]int32
		var neighbors [2][4]MotionVectorCandidate
		for list := 0; list < 2; list++ {
			_, neighbors[list] = decoder.nativeNeighbors(slice, address, geometry, list)
			if !neighbors[list][2].Available {
				neighbors[list][2] = neighbors[list][3]
			}
			indices := [3]int32{-1, -1, -1}
			for index := range indices {
				candidate := neighbors[list][index]
				if candidate.Available {
					indices[index] = candidate.RefIdx
				}
			}
			references[list] = nativeMinPositive(indices[0], nativeMinPositive(indices[1], indices[2]))
		}
		zero := references == ([2]int32{-1, -1})
		if zero {
			references = [2]int32{0, 0}
		}
		colocatedZero := !colocatedIntra && !lists[1][0].Reference.IsLongTerm && colocated.used && colocated.refIndex == 0 && absInt(int(colocated.motion.X)) <= 1 && absInt(int(colocated.motion.Y)) <= 1
		for list, refIndex := range references {
			if refIndex < 0 {
				continue
			}
			if int(refIndex) >= len(lists[list]) {
				return predictions, fmt.Errorf("B direct references an unavailable picture")
			}
			motion := MotionVector{}
			if !zero && !(refIndex == 0 && colocatedZero) {
				candidates := neighbors[list]
				motion = nativeMotion(nativePartition{width: 16, height: 16}, refIndex, candidates, MotionVector{})
			}
			predictions[list] = nativePrediction{used: true, reference: lists[list][refIndex].Reference.ID, refIndex: refIndex, motion: motion, referenceDecoded: true, motionDecoded: true}
		}
		return predictions, nil
	}
	refIndex := int32(0)
	if !colocatedIntra && colocated.used {
		refIndex = -1
		for index, reference := range lists[0] {
			if reference.Reference.ID == colocated.reference {
				refIndex = int32(index)
				break
			}
		}
		if refIndex < 0 {
			return predictions, fmt.Errorf("temporal direct colocated reference is unavailable")
		}
	}
	motion0, motion1 := MotionVector{}, MotionVector{}
	if !colocatedIntra && colocated.used {
		td := min(int64(127), max(int64(-128), lists[1][0].Reference.PictureOrderCnt-lists[0][refIndex].Reference.PictureOrderCnt))
		tb := min(int64(127), max(int64(-128), poc-lists[0][refIndex].Reference.PictureOrderCnt))
		motion0 = colocated.motion
		if !lists[0][refIndex].Reference.IsLongTerm && td != 0 {
			tx := (16384 + int64(absInt(int(td/2)))) / td
			scale := min(int64(1023), max(int64(-1024), (tb*tx+32)>>6))
			motion0 = MotionVector{X: int32((scale*int64(colocated.motion.X) + 128) >> 8), Y: int32((scale*int64(colocated.motion.Y) + 128) >> 8)}
		}
		motion1 = MotionVector{X: motion0.X - colocated.motion.X, Y: motion0.Y - colocated.motion.Y}
	}
	predictions[0] = nativePrediction{used: true, reference: lists[0][refIndex].Reference.ID, refIndex: refIndex, motion: motion0, referenceDecoded: true, motionDecoded: true}
	predictions[1] = nativePrediction{used: true, reference: lists[1][0].Reference.ID, refIndex: 0, motion: motion1, referenceDecoded: true, motionDecoded: true}
	return predictions, nil
}
