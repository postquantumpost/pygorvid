package vid

import "fmt"

// PPSInfo contains the identifier and core coding flags from a picture parameter set.
type PPSInfo struct {
	PictureParameterSetID             uint32
	SequenceParameterSetID            uint32
	EntropyCodingMode                 bool
	BottomFieldPicOrderInFramePresent bool
	NumSliceGroupsMinus1              uint32
	NumRefIdxL0DefaultActiveMinus1    uint32
	NumRefIdxL1DefaultActiveMinus1    uint32
	WeightedPred                      bool
	WeightedBipredIDC                 uint8
	PicInitQpMinus26                  int64
	PicInitQsMinus26                  int64
	ChromaQPIndexOffset               int64
	DeblockingFilterControlPresent    bool
	ConstrainedIntraPred              bool
	RedundantPicCntPresent            bool
	HasExtension                      bool
	Transform8x8Mode                  bool
	PicScalingMatrixPresent           bool
	SecondChromaQPIndexOffset         int64
}

// ParsePPS parses PPS identifiers and the core entropy/field-order flags.
func ParsePPS(nal []byte) (PPSInfo, error) {
	header, err := ParseNALHeader(nal)
	if err != nil {
		return PPSInfo{}, err
	}
	if header.UnitType != 8 {
		return PPSInfo{}, fmt.Errorf("NAL unit type %d is not a PPS", header.UnitType)
	}
	rbsp, err := EBSPToRBSP(nal[1:])
	if err != nil {
		return PPSInfo{}, err
	}
	reader := NewBitReader(rbsp)
	ppsID, err := reader.ReadUE()
	if err != nil {
		return PPSInfo{}, fmt.Errorf("PPS pic_parameter_set_id: %w", err)
	}
	if ppsID > 255 {
		return PPSInfo{}, fmt.Errorf("PPS pic_parameter_set_id %d exceeds 255", ppsID)
	}
	spsID, err := reader.ReadUE()
	if err != nil {
		return PPSInfo{}, fmt.Errorf("PPS seq_parameter_set_id: %w", err)
	}
	if spsID > 31 {
		return PPSInfo{}, fmt.Errorf("PPS seq_parameter_set_id %d exceeds 31", spsID)
	}
	entropyCodingMode, err := reader.ReadBit()
	if err != nil {
		return PPSInfo{}, fmt.Errorf("PPS entropy_coding_mode_flag: %w", err)
	}
	bottomFieldPOCPresent, err := reader.ReadBit()
	if err != nil {
		return PPSInfo{}, fmt.Errorf("PPS bottom_field_pic_order_in_frame_present_flag: %w", err)
	}
	info := PPSInfo{
		PictureParameterSetID:             ppsID,
		SequenceParameterSetID:            spsID,
		EntropyCodingMode:                 entropyCodingMode,
		BottomFieldPicOrderInFramePresent: bottomFieldPOCPresent,
	}
	info.NumSliceGroupsMinus1, err = readPPSUE(reader, "num_slice_groups_minus1")
	if err != nil {
		return PPSInfo{}, err
	}
	if info.NumSliceGroupsMinus1 != 0 {
		return PPSInfo{}, fmt.Errorf("unsupported PPS slice groups: num_slice_groups_minus1=%d", info.NumSliceGroupsMinus1)
	}
	info.NumRefIdxL0DefaultActiveMinus1, err = readPPSUE(reader, "num_ref_idx_l0_default_active_minus1")
	if err != nil {
		return PPSInfo{}, err
	}
	info.NumRefIdxL1DefaultActiveMinus1, err = readPPSUE(reader, "num_ref_idx_l1_default_active_minus1")
	if err != nil {
		return PPSInfo{}, err
	}
	if info.NumRefIdxL0DefaultActiveMinus1 > 31 || info.NumRefIdxL1DefaultActiveMinus1 > 31 {
		return PPSInfo{}, fmt.Errorf("PPS default reference index exceeds 31")
	}
	info.WeightedPred, err = reader.ReadBit()
	if err != nil {
		return PPSInfo{}, fmt.Errorf("PPS weighted_pred_flag: %w", err)
	}
	weightedBipred, err := reader.ReadBits(2)
	if err != nil {
		return PPSInfo{}, fmt.Errorf("PPS weighted_bipred_idc: %w", err)
	}
	if weightedBipred > 2 {
		return PPSInfo{}, fmt.Errorf("PPS weighted_bipred_idc %d is reserved", weightedBipred)
	}
	info.WeightedBipredIDC = uint8(weightedBipred)
	info.PicInitQpMinus26, err = readPPSSE(reader, "pic_init_qp_minus26")
	if err != nil {
		return PPSInfo{}, err
	}
	info.PicInitQsMinus26, err = readPPSSE(reader, "pic_init_qs_minus26")
	if err != nil {
		return PPSInfo{}, err
	}
	info.ChromaQPIndexOffset, err = readPPSSE(reader, "chroma_qp_index_offset")
	if err != nil {
		return PPSInfo{}, err
	}
	if info.PicInitQpMinus26 < -26 || info.PicInitQpMinus26 > 25 || info.PicInitQsMinus26 < -26 || info.PicInitQsMinus26 > 25 {
		return PPSInfo{}, fmt.Errorf("PPS initial QP/QS offset is outside [-26,25]")
	}
	if info.ChromaQPIndexOffset < -12 || info.ChromaQPIndexOffset > 12 {
		return PPSInfo{}, fmt.Errorf("PPS chroma QP index offset is outside [-12,12]")
	}
	info.DeblockingFilterControlPresent, err = reader.ReadBit()
	if err != nil {
		return PPSInfo{}, fmt.Errorf("PPS deblocking_filter_control_present_flag: %w", err)
	}
	info.ConstrainedIntraPred, err = reader.ReadBit()
	if err != nil {
		return PPSInfo{}, fmt.Errorf("PPS constrained_intra_pred_flag: %w", err)
	}
	info.RedundantPicCntPresent, err = reader.ReadBit()
	if err != nil {
		return PPSInfo{}, fmt.Errorf("PPS redundant_pic_cnt_present_flag: %w", err)
	}
	info.SecondChromaQPIndexOffset = info.ChromaQPIndexOffset
	if reader.MoreRBSPData() {
		info.HasExtension = true
		info.Transform8x8Mode, err = reader.ReadBit()
		if err != nil {
			return PPSInfo{}, fmt.Errorf("PPS transform_8x8_mode_flag: %w", err)
		}
		info.PicScalingMatrixPresent, err = reader.ReadBit()
		if err != nil {
			return PPSInfo{}, fmt.Errorf("PPS pic_scaling_matrix_present_flag: %w", err)
		}
		if info.PicScalingMatrixPresent {
			return PPSInfo{}, fmt.Errorf("unsupported PPS pic_scaling_matrix_present_flag")
		}
		info.SecondChromaQPIndexOffset, err = readPPSSE(reader, "second_chroma_qp_index_offset")
		if err != nil {
			return PPSInfo{}, err
		}
		if info.SecondChromaQPIndexOffset < -12 || info.SecondChromaQPIndexOffset > 12 {
			return PPSInfo{}, fmt.Errorf("PPS second chroma QP index offset is outside [-12,12]")
		}
	}
	return info, nil
}

func readPPSUE(reader *BitReader, field string) (uint32, error) {
	value, err := reader.ReadUE()
	if err != nil {
		return 0, fmt.Errorf("PPS %s: %w", field, err)
	}
	return value, nil
}

func readPPSSE(reader *BitReader, field string) (int64, error) {
	value, err := reader.ReadSE()
	if err != nil {
		return 0, fmt.Errorf("PPS %s: %w", field, err)
	}
	return value, nil
}
