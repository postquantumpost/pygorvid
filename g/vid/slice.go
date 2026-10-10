package vid

import "fmt"

// SliceHeader contains the common slice syntax needed to identify a primary picture.
type SliceHeader struct {
	FirstMacroblockInSlice     uint32
	SliceType                  uint8
	PictureParameterSetID      uint32
	ColourPlaneID              uint8
	HasColourPlaneID           bool
	FrameNum                   uint32
	FieldPicFlag               bool
	BottomFieldFlag            bool
	IDR                        bool
	IDRPicID                   uint32
	PicOrderCntLSB             uint32
	HasPicOrderCntLSB          bool
	DeltaPicOrderBottom        int64
	HasDeltaPicOrderBottom     bool
	DeltaPicOrderCnt0          int64
	DeltaPicOrderCnt1          int64
	HasDeltaPicOrderCnt0       bool
	HasDeltaPicOrderCnt1       bool
	PicOrderCntType            uint8
	NALRefIDC                  uint8
	SeparateColourPlane        bool
	RedundantPicCnt            uint32
	HasRedundantPicCnt         bool
	DirectSpatialMVPred        bool
	HasDirectSpatialMVPred     bool
	NumRefIdxActiveOverride    bool
	NumRefIdxL0ActiveMinus1    uint32
	NumRefIdxL1ActiveMinus1    uint32
	RefPicListReorderingL0     bool
	RefPicListReorderingL1     bool
	RefPicListL0               []RefPicListModification
	RefPicListL1               []RefPicListModification
	NoOutputOfPriorPics        bool
	LongTermReference          bool
	AdaptiveRefPicMarking      bool
	MemoryManagementOps        []MemoryManagementOperation
	CabacInitIDC               uint8
	HasCabacInitIDC            bool
	SliceQPDelta               int64
	DisableDeblockingFilterIDC uint8
	HasDeblockingFilterIDC     bool
	SliceAlphaC0OffsetDiv2     int64
	SliceBetaOffsetDiv2        int64
	SliceDataBitOffset         uint64
}

type RefPicListModification struct {
	ModificationOfPicNumsIDC uint32
	Value                    uint32
}

type MemoryManagementOperation struct {
	Operation uint32
	Operands  []uint32
}

// PictureIdentity contains the slice-header fields used to distinguish primary pictures.
type PictureIdentity struct {
	FrameNum               uint32
	PictureParameterSetID  uint32
	SeparateColourPlane    bool
	ColourPlaneID          uint8
	FieldPicFlag           bool
	BottomFieldFlag        bool
	NALRefIDCZero          bool
	IDR                    bool
	IDRPicID               uint32
	HasIDRPicID            bool
	PicOrderCntLSB         uint32
	HasPicOrderCntLSB      bool
	DeltaPicOrderBottom    int64
	HasDeltaPicOrderBottom bool
	DeltaPicOrderCnt0      int64
	DeltaPicOrderCnt1      int64
	HasDeltaPicOrderCnt0   bool
	HasDeltaPicOrderCnt1   bool
	PicOrderCntType        uint8
}

// PictureIdentity projects the current header to fields that define picture identity.
func (h SliceHeader) PictureIdentity() PictureIdentity {
	return PictureIdentity{
		FrameNum:               h.FrameNum,
		PictureParameterSetID:  h.PictureParameterSetID,
		SeparateColourPlane:    h.SeparateColourPlane,
		ColourPlaneID:          h.ColourPlaneID,
		FieldPicFlag:           h.FieldPicFlag,
		BottomFieldFlag:        h.BottomFieldFlag,
		NALRefIDCZero:          h.NALRefIDC == 0,
		IDR:                    h.IDR,
		IDRPicID:               h.IDRPicID,
		HasIDRPicID:            h.IDR,
		PicOrderCntLSB:         h.PicOrderCntLSB,
		HasPicOrderCntLSB:      h.HasPicOrderCntLSB,
		DeltaPicOrderBottom:    h.DeltaPicOrderBottom,
		HasDeltaPicOrderBottom: h.HasDeltaPicOrderBottom,
		DeltaPicOrderCnt0:      h.DeltaPicOrderCnt0,
		DeltaPicOrderCnt1:      h.DeltaPicOrderCnt1,
		HasDeltaPicOrderCnt0:   h.HasDeltaPicOrderCnt0,
		HasDeltaPicOrderCnt1:   h.HasDeltaPicOrderCnt1,
		PicOrderCntType:        h.PicOrderCntType,
	}
}

// SamePrimaryPicture reports whether two VCL slice headers identify the same primary picture.
func SamePrimaryPicture(previous, current SliceHeader) bool {
	left, right := previous.PictureIdentity(), current.PictureIdentity()
	if left.FrameNum != right.FrameNum || left.PictureParameterSetID != right.PictureParameterSetID ||
		left.FieldPicFlag != right.FieldPicFlag || left.NALRefIDCZero != right.NALRefIDCZero ||
		left.IDR != right.IDR || left.SeparateColourPlane != right.SeparateColourPlane ||
		left.PicOrderCntType != right.PicOrderCntType {
		return false
	}
	if left.FieldPicFlag && left.BottomFieldFlag != right.BottomFieldFlag {
		return false
	}
	if left.SeparateColourPlane && left.ColourPlaneID != right.ColourPlaneID {
		return false
	}
	if left.IDR && left.IDRPicID != right.IDRPicID {
		return false
	}
	switch left.PicOrderCntType {
	case 0:
		if left.PicOrderCntLSB != right.PicOrderCntLSB || left.DeltaPicOrderBottom != right.DeltaPicOrderBottom ||
			left.HasDeltaPicOrderBottom != right.HasDeltaPicOrderBottom {
			return false
		}
	case 1:
		if left.DeltaPicOrderCnt0 != right.DeltaPicOrderCnt0 || left.DeltaPicOrderCnt1 != right.DeltaPicOrderCnt1 ||
			left.HasDeltaPicOrderCnt0 != right.HasDeltaPicOrderCnt0 || left.HasDeltaPicOrderCnt1 != right.HasDeltaPicOrderCnt1 {
			return false
		}
	case 2:
	default:
		return false
	}
	return true
}

// PictureGroup contains consecutive slices that share a primary-picture identity.
type PictureGroup struct {
	Identity PictureIdentity
	Slices   []SliceHeader
}

// GroupSlicesIntoPictures groups consecutive VCL slice headers by primary-picture identity.
func GroupSlicesIntoPictures(slices []SliceHeader) []PictureGroup {
	if len(slices) == 0 {
		return nil
	}
	groups := make([]PictureGroup, 0)
	for _, slice := range slices {
		if len(groups) == 0 || !SamePrimaryPicture(groups[len(groups)-1].Slices[0], slice) {
			groups = append(groups, PictureGroup{Identity: slice.PictureIdentity(), Slices: []SliceHeader{slice}})
			continue
		}
		lastGroup := len(groups) - 1
		groups[lastGroup].Slices = append(groups[lastGroup].Slices, slice)
	}
	return groups
}

// ParseSliceHeader parses common slice syntax through the picture-identity fields.
func ParseSliceHeader(nal []byte, sps SPSInfo, pps PPSInfo) (SliceHeader, error) {
	nalHeader, err := ParseNALHeader(nal)
	if err != nil {
		return SliceHeader{}, err
	}
	if nalHeader.UnitType != 1 && nalHeader.UnitType != 5 {
		return SliceHeader{}, fmt.Errorf("NAL unit type %d is not a coded slice", nalHeader.UnitType)
	}
	if pps.SequenceParameterSetID != sps.ID {
		return SliceHeader{}, fmt.Errorf("PPS references SPS %d, supplied SPS is %d", pps.SequenceParameterSetID, sps.ID)
	}
	rbsp, err := EBSPToRBSP(nal[1:])
	if err != nil {
		return SliceHeader{}, err
	}
	reader := NewBitReader(rbsp)
	firstMacroblock, err := readSliceUE(reader, "first_mb_in_slice")
	if err != nil {
		return SliceHeader{}, err
	}
	sliceType, err := readSliceUE(reader, "slice_type")
	if err != nil {
		return SliceHeader{}, err
	}
	if sliceType > 9 {
		return SliceHeader{}, fmt.Errorf("slice_type %d exceeds 9", sliceType)
	}
	ppsID, err := readSliceUE(reader, "pic_parameter_set_id")
	if err != nil {
		return SliceHeader{}, err
	}
	if ppsID != pps.PictureParameterSetID {
		return SliceHeader{}, fmt.Errorf("slice references PPS %d, supplied PPS is %d", ppsID, pps.PictureParameterSetID)
	}
	header := SliceHeader{
		FirstMacroblockInSlice: firstMacroblock,
		SliceType:              uint8(sliceType),
		PictureParameterSetID:  ppsID,
		NALRefIDC:              nalHeader.ReferenceIDC,
		IDR:                    nalHeader.UnitType == 5,
		SeparateColourPlane:    sps.SeparateColourPlane,
		PicOrderCntType:        sps.PicOrderCntType,
	}
	if sps.SeparateColourPlane {
		colourPlaneID, err := reader.ReadBits(2)
		if err != nil {
			return SliceHeader{}, fmt.Errorf("slice colour_plane_id: %w", err)
		}
		if colourPlaneID > 2 {
			return SliceHeader{}, fmt.Errorf("colour_plane_id %d is reserved", colourPlaneID)
		}
		header.ColourPlaneID = uint8(colourPlaneID)
		header.HasColourPlaneID = true
	}
	frameNumBits := int(sps.Log2MaxFrameNumMinus4) + 4
	frameNum, err := reader.ReadBits(frameNumBits)
	if err != nil {
		return SliceHeader{}, fmt.Errorf("slice frame_num: %w", err)
	}
	header.FrameNum = frameNum
	if !sps.FrameMbsOnly {
		header.FieldPicFlag, err = reader.ReadBit()
		if err != nil {
			return SliceHeader{}, fmt.Errorf("slice field_pic_flag: %w", err)
		}
		if header.FieldPicFlag {
			header.BottomFieldFlag, err = reader.ReadBit()
			if err != nil {
				return SliceHeader{}, fmt.Errorf("slice bottom_field_flag: %w", err)
			}
		}
	}
	if header.IDR {
		header.IDRPicID, err = readSliceUE(reader, "idr_pic_id")
		if err != nil {
			return SliceHeader{}, err
		}
		if header.IDRPicID > 65535 {
			return SliceHeader{}, fmt.Errorf("idr_pic_id %d exceeds 65535", header.IDRPicID)
		}
	}
	switch sps.PicOrderCntType {
	case 0:
		if !sps.HasPicOrderCntLsb {
			return SliceHeader{}, fmt.Errorf("SPS POC type 0 lacks log2_max_pic_order_cnt_lsb_minus4")
		}
		pocBits := int(sps.Log2MaxPicOrderCntLsbMinus4) + 4
		header.PicOrderCntLSB, err = reader.ReadBits(pocBits)
		if err != nil {
			return SliceHeader{}, fmt.Errorf("slice pic_order_cnt_lsb: %w", err)
		}
		header.HasPicOrderCntLSB = true
		if pps.BottomFieldPicOrderInFramePresent && !header.FieldPicFlag {
			header.DeltaPicOrderBottom, err = reader.ReadSE()
			if err != nil {
				return SliceHeader{}, fmt.Errorf("slice delta_pic_order_bottom: %w", err)
			}
			header.HasDeltaPicOrderBottom = true
		}
	case 1:
		if !sps.DeltaPicOrderAlwaysZero {
			header.DeltaPicOrderCnt0, err = reader.ReadSE()
			if err != nil {
				return SliceHeader{}, fmt.Errorf("slice delta_pic_order_cnt[0]: %w", err)
			}
			header.HasDeltaPicOrderCnt0 = true
			if pps.BottomFieldPicOrderInFramePresent && !header.FieldPicFlag {
				header.DeltaPicOrderCnt1, err = reader.ReadSE()
				if err != nil {
					return SliceHeader{}, fmt.Errorf("slice delta_pic_order_cnt[1]: %w", err)
				}
				header.HasDeltaPicOrderCnt1 = true
			}
		}
	case 2:
	default:
		return SliceHeader{}, fmt.Errorf("unsupported SPS pic_order_cnt_type %d", sps.PicOrderCntType)
	}
	if pps.RedundantPicCntPresent {
		header.RedundantPicCnt, err = readSliceUE(reader, "redundant_pic_cnt")
		if err != nil {
			return SliceHeader{}, err
		}
		if header.RedundantPicCnt > 127 {
			return SliceHeader{}, fmt.Errorf("redundant_pic_cnt %d exceeds 127", header.RedundantPicCnt)
		}
		header.HasRedundantPicCnt = true
	}
	normalizedSliceType := header.SliceType % 5
	switch normalizedSliceType {
	case 2:
	case 0, 1:
		if normalizedSliceType == 1 {
			header.DirectSpatialMVPred, err = reader.ReadBit()
			if err != nil {
				return SliceHeader{}, fmt.Errorf("slice direct_spatial_mv_pred_flag: %w", err)
			}
			header.HasDirectSpatialMVPred = true
		}
		header.NumRefIdxActiveOverride, err = reader.ReadBit()
		if err != nil {
			return SliceHeader{}, fmt.Errorf("slice num_ref_idx_active_override_flag: %w", err)
		}
		if header.NumRefIdxActiveOverride {
			header.NumRefIdxL0ActiveMinus1, err = readSliceUE(reader, "num_ref_idx_l0_active_minus1")
			if err != nil {
				return SliceHeader{}, err
			}
			if header.NumRefIdxL0ActiveMinus1 > 31 {
				return SliceHeader{}, fmt.Errorf("num_ref_idx_l0_active_minus1 exceeds 31")
			}
			if normalizedSliceType == 1 {
				header.NumRefIdxL1ActiveMinus1, err = readSliceUE(reader, "num_ref_idx_l1_active_minus1")
				if err != nil {
					return SliceHeader{}, err
				}
				if header.NumRefIdxL1ActiveMinus1 > 31 {
					return SliceHeader{}, fmt.Errorf("num_ref_idx_l1_active_minus1 exceeds 31")
				}
			}
		}
	default:
		return SliceHeader{}, fmt.Errorf("unsupported slice_type %d", header.SliceType)
	}
	if normalizedSliceType != 2 {
		header.RefPicListReorderingL0, header.RefPicListL0, err = parseRefPicListReordering(reader, "l0")
		if err != nil {
			return SliceHeader{}, err
		}
		if normalizedSliceType == 1 {
			header.RefPicListReorderingL1, header.RefPicListL1, err = parseRefPicListReordering(reader, "l1")
			if err != nil {
				return SliceHeader{}, err
			}
		}
	}
	weightedPredApplicable := pps.WeightedPred && (normalizedSliceType == 0 || normalizedSliceType == 3)
	weightedBipredApplicable := pps.WeightedBipredIDC == 1 && normalizedSliceType == 1
	if weightedPredApplicable || weightedBipredApplicable {
		return SliceHeader{}, fmt.Errorf("unsupported weighted prediction in slice header")
	}
	if nalHeader.ReferenceIDC != 0 {
		if header.IDR {
			header.NoOutputOfPriorPics, err = reader.ReadBit()
			if err != nil {
				return SliceHeader{}, fmt.Errorf("slice no_output_of_prior_pics_flag: %w", err)
			}
			header.LongTermReference, err = reader.ReadBit()
			if err != nil {
				return SliceHeader{}, fmt.Errorf("slice long_term_reference_flag: %w", err)
			}
		} else {
			header.AdaptiveRefPicMarking, err = reader.ReadBit()
			if err != nil {
				return SliceHeader{}, fmt.Errorf("slice adaptive_ref_pic_marking_mode_flag: %w", err)
			}
			if header.AdaptiveRefPicMarking {
				header.MemoryManagementOps, err = parseMemoryManagementOperations(reader)
				if err != nil {
					return SliceHeader{}, err
				}
			}
		}
	}
	if pps.EntropyCodingMode && normalizedSliceType != 2 {
		cabacInitIDC, err := readSliceUE(reader, "cabac_init_idc")
		if err != nil {
			return SliceHeader{}, err
		}
		if cabacInitIDC > 2 {
			return SliceHeader{}, fmt.Errorf("cabac_init_idc %d exceeds 2", cabacInitIDC)
		}
		header.CabacInitIDC = uint8(cabacInitIDC)
		header.HasCabacInitIDC = true
	}
	header.SliceQPDelta, err = reader.ReadSE()
	if err != nil {
		return SliceHeader{}, fmt.Errorf("slice slice_qp_delta: %w", err)
	}
	qpBdOffsetY := int64(6) * int64(sps.BitDepthLuma-8)
	sliceQPY := int64(26) + pps.PicInitQpMinus26 + header.SliceQPDelta
	if sliceQPY < -qpBdOffsetY || sliceQPY > 51 {
		return SliceHeader{}, fmt.Errorf("slice QP %d is out of range", sliceQPY)
	}
	if pps.DeblockingFilterControlPresent {
		disableIDC, err := readSliceUE(reader, "disable_deblocking_filter_idc")
		if err != nil {
			return SliceHeader{}, err
		}
		if disableIDC > 2 {
			return SliceHeader{}, fmt.Errorf("disable_deblocking_filter_idc %d exceeds 2", disableIDC)
		}
		header.DisableDeblockingFilterIDC = uint8(disableIDC)
		header.HasDeblockingFilterIDC = true
		if disableIDC != 1 {
			header.SliceAlphaC0OffsetDiv2, err = reader.ReadSE()
			if err != nil {
				return SliceHeader{}, fmt.Errorf("slice slice_alpha_c0_offset_div2: %w", err)
			}
			header.SliceBetaOffsetDiv2, err = reader.ReadSE()
			if err != nil {
				return SliceHeader{}, fmt.Errorf("slice slice_beta_offset_div2: %w", err)
			}
			if header.SliceAlphaC0OffsetDiv2 < -6 || header.SliceAlphaC0OffsetDiv2 > 6 ||
				header.SliceBetaOffsetDiv2 < -6 || header.SliceBetaOffsetDiv2 > 6 {
				return SliceHeader{}, fmt.Errorf("slice deblocking offset exceeds [-6,6]")
			}
		}
	}
	if pps.EntropyCodingMode {
		for reader.bitOffset%8 != 0 {
			alignmentBit, err := reader.ReadBit()
			if err != nil {
				return SliceHeader{}, fmt.Errorf("slice cabac_alignment_one_bit: %w", err)
			}
			if !alignmentBit {
				return SliceHeader{}, fmt.Errorf("slice cabac_alignment_one_bit is not 1")
			}
		}
	}
	header.SliceDataBitOffset = reader.bitOffset
	return header, nil
}

func parseRefPicListReordering(reader *BitReader, listName string) (bool, []RefPicListModification, error) {
	reordering, err := reader.ReadBit()
	if err != nil {
		return false, nil, fmt.Errorf("slice ref_pic_list_reordering_flag_%s: %w", listName, err)
	}
	if !reordering {
		return false, nil, nil
	}
	modifications := make([]RefPicListModification, 0)
	for index := 0; index < 64; index++ {
		idc, err := readSliceUE(reader, fmt.Sprintf("modification_of_pic_nums_idc_%s", listName))
		if err != nil {
			return false, nil, err
		}
		if idc == 3 {
			return true, modifications, nil
		}
		if idc > 2 {
			return false, nil, fmt.Errorf("slice modification_of_pic_nums_idc %d is invalid", idc)
		}
		value, err := readSliceUE(reader, fmt.Sprintf("ref_pic_list_modification_%s[%d]", listName, index))
		if err != nil {
			return false, nil, err
		}
		modifications = append(modifications, RefPicListModification{ModificationOfPicNumsIDC: idc, Value: value})
	}
	return false, nil, fmt.Errorf("slice ref-list reordering exceeds 64 operations")
}

func parseMemoryManagementOperations(reader *BitReader) ([]MemoryManagementOperation, error) {
	operations := make([]MemoryManagementOperation, 0)
	for index := 0; index < 32; index++ {
		operation, err := readSliceUE(reader, "memory_management_control_operation")
		if err != nil {
			return nil, err
		}
		if operation == 0 {
			return operations, nil
		}
		if operation > 6 {
			return nil, fmt.Errorf("memory_management_control_operation %d is invalid", operation)
		}
		operands := make([]uint32, 0, 2)
		operandCount := map[uint32]int{1: 1, 2: 1, 3: 2, 4: 1, 5: 0, 6: 1}[operation]
		for operandIndex := 0; operandIndex < operandCount; operandIndex++ {
			operand, err := readSliceUE(reader, "memory_management_operation_operand")
			if err != nil {
				return nil, err
			}
			operands = append(operands, operand)
		}
		operations = append(operations, MemoryManagementOperation{Operation: operation, Operands: operands})
	}
	return nil, fmt.Errorf("decoded-reference marking exceeds 32 operations")
}

func readSliceUE(reader *BitReader, field string) (uint32, error) {
	value, err := reader.ReadUE()
	if err != nil {
		return 0, fmt.Errorf("slice %s: %w", field, err)
	}
	return value, nil
}
