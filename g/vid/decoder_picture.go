package vid

import (
	"fmt"
	"sort"
)

func cloneNativeFrame(frame Yuv420Frame) Yuv420Frame {
	frame.Y = append([]byte(nil), frame.Y...)
	frame.U = append([]byte(nil), frame.U...)
	frame.V = append([]byte(nil), frame.V...)
	return frame
}

func (decoder *H264Decoder) nativePictureOrder(slice nativeSlice) (int64, error) {
	header, sps := slice.header, slice.sps
	switch sps.PicOrderCntType {
	case 0:
		return decoder.nativePOC0.Calculate(1<<(sps.Log2MaxPicOrderCntLsbMinus4+4), header.PicOrderCntLSB, header.DeltaPicOrderBottom, header.NALRefIDC, header.IDR)
	case 1:
		return decoder.nativePOC12.CalculateType1(1<<(sps.Log2MaxFrameNumMinus4+4), header.FrameNum, header.DeltaPicOrderCnt0, header.DeltaPicOrderCnt1, sps.OffsetForNonRefPic, sps.OffsetForTopToBottomField, sps.OffsetForRefFrame, header.NALRefIDC, header.IDR)
	case 2:
		return decoder.nativePOC12.CalculateType2(1<<(sps.Log2MaxFrameNumMinus4+4), header.FrameNum, header.NALRefIDC, header.IDR)
	}
	return 0, ErrDecoderUnsupportedFeature
}

func (decoder *H264Decoder) removeNativeReference(identifier uint32) {
	decoder.referencePictureBuffer.Remove(identifier)
	delete(decoder.nativeReferenceBlocks, identifier)
}

func (decoder *H264Decoder) markNativeReference(slice nativeSlice, identifier uint32, poc int64, frame Yuv420Frame) error {
	header, sps := slice.header, slice.sps
	reference := ReferencePicture{ID: identifier, FrameNum: header.FrameNum, PictureOrderCnt: poc, IsLongTerm: header.IDR && header.LongTermReference}
	if header.IDR {
		decoder.nativeMaximumLongTerm = -1
		if header.LongTermReference {
			decoder.nativeMaximumLongTerm = 0
		}
	} else if header.AdaptiveRefPicMarking {
		maximum := int64(1 << (sps.Log2MaxFrameNumMinus4 + 4))
		picNum := func(candidate ReferencePicture) int64 {
			value := int64(candidate.FrameNum)
			if candidate.FrameNum > header.FrameNum {
				value -= maximum
			}
			return value
		}
		for _, operation := range header.MemoryManagementOps {
			operands := operation.Operands
			switch operation.Operation {
			case 1, 3:
				if len(operands) == 0 || (operation.Operation == 3 && len(operands) != 2) {
					return ErrDecoderUnsupportedFeature
				}
				target := int64(header.FrameNum) - int64(operands[0]) - 1
				found := false
				for _, candidate := range decoder.referencePictureBuffer.References() {
					if candidate.IsLongTerm || picNum(candidate) != target {
						continue
					}
					found = true
					if operation.Operation == 1 {
						decoder.removeNativeReference(candidate.ID)
					} else {
						if decoder.nativeMaximumLongTerm < 0 || int64(operands[1]) > decoder.nativeMaximumLongTerm {
							return fmt.Errorf("long-term reference index exceeds the marking limit")
						}
						for _, existing := range decoder.referencePictureBuffer.References() {
							if existing.IsLongTerm && existing.LongTermFrameIdx == operands[1] {
								decoder.removeNativeReference(existing.ID)
							}
						}
						stored, ok := decoder.referencePictureBuffer.Get(candidate.ID)
						if !ok {
							return fmt.Errorf("marking target is unavailable")
						}
						candidate.IsLongTerm, candidate.LongTermFrameIdx = true, operands[1]
						if err := decoder.referencePictureBuffer.Store(candidate, stored.Frame); err != nil {
							return err
						}
					}
					break
				}
				if !found {
					return fmt.Errorf("short-term marking target is unavailable")
				}
			case 2:
				if len(operands) != 1 {
					return ErrDecoderUnsupportedFeature
				}
				found := false
				for _, candidate := range decoder.referencePictureBuffer.References() {
					if candidate.IsLongTerm && candidate.LongTermFrameIdx == operands[0] {
						decoder.removeNativeReference(candidate.ID)
						found = true
						break
					}
				}
				if !found {
					return fmt.Errorf("long-term marking target is unavailable")
				}
			case 4:
				if len(operands) != 1 {
					return ErrDecoderUnsupportedFeature
				}
				decoder.nativeMaximumLongTerm = int64(operands[0]) - 1
				for _, candidate := range decoder.referencePictureBuffer.References() {
					if candidate.IsLongTerm && int64(candidate.LongTermFrameIdx) > decoder.nativeMaximumLongTerm {
						decoder.removeNativeReference(candidate.ID)
					}
				}
			case 5:
				for _, candidate := range decoder.referencePictureBuffer.References() {
					decoder.removeNativeReference(candidate.ID)
				}
				decoder.nativeMaximumLongTerm = -1
				reference.FrameNum, reference.PictureOrderCnt = 0, 0
				decoder.nativePOC0 = POCType0State{}
				decoder.nativePOC12 = POCType12State{}
			case 6:
				if len(operands) != 1 || decoder.nativeMaximumLongTerm < 0 || int64(operands[0]) > decoder.nativeMaximumLongTerm {
					return fmt.Errorf("current long-term reference index exceeds the marking limit")
				}
				for _, candidate := range decoder.referencePictureBuffer.References() {
					if candidate.IsLongTerm && candidate.LongTermFrameIdx == operands[0] {
						decoder.removeNativeReference(candidate.ID)
					}
				}
				reference.IsLongTerm, reference.LongTermFrameIdx = true, operands[0]
			default:
				return ErrDecoderUnsupportedFeature
			}
		}
	} else {
		references := decoder.referencePictureBuffer.References()
		if len(references) >= max(1, int(sps.MaxNumRefFrames)) {
			oldest := -1
			oldestWrap := int64(0)
			maximum := int64(1 << (sps.Log2MaxFrameNumMinus4 + 4))
			for index, candidate := range references {
				if candidate.IsLongTerm {
					continue
				}
				wrap := int64(candidate.FrameNum)
				if candidate.FrameNum > header.FrameNum {
					wrap -= maximum
				}
				if oldest < 0 || wrap < oldestWrap {
					oldest, oldestWrap = index, wrap
				}
			}
			if oldest < 0 {
				return fmt.Errorf("sliding-window marking has no short-term reference")
			}
			decoder.removeNativeReference(references[oldest].ID)
		}
	}
	if err := decoder.referencePictureBuffer.Store(reference, frame); err != nil {
		return err
	}
	decoder.nativeReferenceBlocks[identifier] = append([]nativeMacroblock(nil), decoder.nativeBlocks...)
	return nil
}

func (decoder *H264Decoder) decodeNativeDependencies(index uint64) (Yuv420Frame, error) {
	samples, err := decoder.sampleReader.DecodeOrderDependencySamples(index)
	if err != nil {
		return Yuv420Frame{}, err
	}
	if len(samples) == 0 {
		return Yuv420Frame{}, ErrDecoderFrameIndexOutOfRange
	}
	if decoder.nativeSyncSample != int64(samples[0].Index) {
		decoder.referencePictureBuffer = &ReferencePictureBuffer{}
		decoder.nativeReferenceBlocks = make(map[uint32][]nativeMacroblock)
		decoder.nativePOC0, decoder.nativePOC12 = POCType0State{}, POCType12State{}
		decoder.nativeSyncSample, decoder.nativeLastSample = int64(samples[0].Index), int64(samples[0].Index)-1
	}
	order := make([]int, len(decoder.sampleReader.samples))
	for position := range order {
		order[position] = position
	}
	sort.SliceStable(order, func(left, right int) bool {
		return decoder.sampleReader.samples[order[left]].ptsTicks < decoder.sampleReader.samples[order[right]].ptsTicks
	})
	displayIndices := make([]uint64, len(order))
	for position, codedIndex := range order {
		displayIndices[codedIndex] = uint64(position)
	}
	for _, sample := range samples {
		if int64(sample.Index) <= decoder.nativeLastSample {
			continue
		}
		slices, err := decoder.nativeSlices(sample)
		if err != nil {
			return Yuv420Frame{}, err
		}
		first := slices[0]
		for _, slice := range slices {
			if slice.header.PictureIdentity() != first.header.PictureIdentity() || slice.sps.ID != first.sps.ID {
				return Yuv420Frame{}, ErrDecoderUnsupportedFeature
			}
		}
		if first.header.IDR {
			decoder.referencePictureBuffer = &ReferencePictureBuffer{}
			decoder.nativeReferenceBlocks = make(map[uint32][]nativeMacroblock)
		}
		poc, err := decoder.nativePictureOrder(first)
		if err != nil {
			return Yuv420Frame{}, err
		}
		var coded Yuv420Frame
		if first.header.SliceType%5 == 2 {
			coded, err = decoder.decodeNativeCodedIntraSample(sample)
		} else {
			coded, err = decoder.decodeNativeInterSample(sample, poc)
		}
		if err != nil {
			return Yuv420Frame{}, fmt.Errorf("sample %d: %w", sample.Index, err)
		}
		if err := deblockNativePictureThreads(&coded, decoder.nativeBlocks, decoder.threads); err != nil {
			return Yuv420Frame{}, err
		}
		displayIndex := displayIndices[sample.Index]
		frame := cropNativeFrame(coded, first.sps)
		if first.header.NALRefIDC != 0 {
			if err := decoder.markNativeReference(first, uint32(displayIndex), poc, coded); err != nil {
				return Yuv420Frame{}, err
			}
		}
		decoder.decodedFrames[displayIndex] = frame
		decoder.nativeLastSample = int64(sample.Index)
	}
	frame, ok := decoder.decodedFrames[index]
	if !ok {
		return Yuv420Frame{}, fmt.Errorf("requested presentation frame is unavailable after dependency decoding")
	}
	return cloneNativeFrame(frame), nil
}
