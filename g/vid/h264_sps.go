package vid

import (
	"fmt"
)

// SPSInfo contains the profile and sample-format fields parsed from an SPS.
type SPSInfo struct {
	ProfileIDC                  uint8
	ConstraintFlags             uint8
	LevelIDC                    uint8
	ID                          uint32
	ChromaFormatIDC             uint8
	SeparateColourPlane         bool
	BitDepthLuma                uint8
	BitDepthChroma              uint8
	CodedWidth                  uint32
	CodedHeight                 uint32
	Width                       uint32
	Height                      uint32
	FrameCropLeft               uint32
	FrameCropRight              uint32
	FrameCropTop                uint32
	FrameCropBottom             uint32
	Log2MaxFrameNumMinus4       uint32
	PicOrderCntType             uint8
	Log2MaxPicOrderCntLsbMinus4 uint32
	HasPicOrderCntLsb           bool
	DeltaPicOrderAlwaysZero     bool
	OffsetForNonRefPic          int64
	OffsetForTopToBottomField   int64
	OffsetForRefFrame           []int64
	MaxNumRefFrames             uint32
	GapsInFrameNumValueAllowed  bool
	FrameMbsOnly                bool
	MbAdaptiveFrameField        bool
	Direct8x8Inference          bool
}

// ParseSPS parses SPS identity, profile, chroma format, and bit-depth fields.
func ParseSPS(nal []byte) (SPSInfo, error) {
	header, err := ParseNALHeader(nal)
	if err != nil {
		return SPSInfo{}, err
	}
	if header.UnitType != 7 {
		return SPSInfo{}, fmt.Errorf("NAL unit type %d is not an SPS", header.UnitType)
	}
	rbsp, err := EBSPToRBSP(nal[1:])
	if err != nil {
		return SPSInfo{}, err
	}
	reader := NewBitReader(rbsp)
	readByte := func(name string) (uint8, error) {
		value, err := reader.ReadBits(8)
		if err != nil {
			return 0, fmt.Errorf("SPS %s: %w", name, err)
		}
		return uint8(value), nil
	}
	profileIDC, err := readByte("profile_idc")
	if err != nil {
		return SPSInfo{}, err
	}
	constraintFlags, err := readByte("constraint flags")
	if err != nil {
		return SPSInfo{}, err
	}
	if constraintFlags&0x03 != 0 {
		return SPSInfo{}, fmt.Errorf("SPS reserved constraint bits are nonzero")
	}
	levelIDC, err := readByte("level_idc")
	if err != nil {
		return SPSInfo{}, err
	}
	spsID, err := reader.ReadUE()
	if err != nil {
		return SPSInfo{}, fmt.Errorf("SPS seq_parameter_set_id: %w", err)
	}
	if spsID > 31 {
		return SPSInfo{}, fmt.Errorf("SPS seq_parameter_set_id %d exceeds 31", spsID)
	}

	info := SPSInfo{
		ProfileIDC:      profileIDC,
		ConstraintFlags: constraintFlags,
		LevelIDC:        levelIDC,
		ID:              spsID,
		ChromaFormatIDC: 1,
		BitDepthLuma:    8,
		BitDepthChroma:  8,
	}
	if profileHasChromaDepthSyntax(profileIDC) {
		chromaFormat, err := readSPSUE(reader, "chroma_format_idc")
		if err != nil {
			return SPSInfo{}, err
		}
		if chromaFormat > 3 {
			return SPSInfo{}, fmt.Errorf("SPS chroma_format_idc %d exceeds 3", chromaFormat)
		}
		info.ChromaFormatIDC = uint8(chromaFormat)
		if chromaFormat == 3 {
			info.SeparateColourPlane, err = reader.ReadBit()
			if err != nil {
				return SPSInfo{}, fmt.Errorf("SPS separate_colour_plane_flag: %w", err)
			}
		}
		lumaDepth, err := readSPSUE(reader, "bit_depth_luma_minus8")
		if err != nil {
			return SPSInfo{}, err
		}
		chromaDepth, err := readSPSUE(reader, "bit_depth_chroma_minus8")
		if err != nil {
			return SPSInfo{}, err
		}
		if lumaDepth > 6 || chromaDepth > 6 {
			return SPSInfo{}, fmt.Errorf("SPS bit depth minus 8 exceeds 6")
		}
		info.BitDepthLuma = uint8(8 + lumaDepth)
		info.BitDepthChroma = uint8(8 + chromaDepth)
		if _, err := reader.ReadBit(); err != nil {
			return SPSInfo{}, fmt.Errorf("SPS qpprime_y_zero_transform_bypass_flag: %w", err)
		}
		if err := skipSPSScalingLists(reader, chromaFormat); err != nil {
			return SPSInfo{}, err
		}
	}
	maxFrameNumBits, err := readSPSUE(reader, "log2_max_frame_num_minus4")
	if err != nil {
		return SPSInfo{}, err
	}
	if maxFrameNumBits > 12 {
		return SPSInfo{}, fmt.Errorf("SPS log2_max_frame_num_minus4 exceeds 12")
	}
	info.Log2MaxFrameNumMinus4 = maxFrameNumBits
	pocType, err := readSPSUE(reader, "pic_order_cnt_type")
	if err != nil {
		return SPSInfo{}, err
	}
	if pocType > 2 {
		return SPSInfo{}, fmt.Errorf("SPS pic_order_cnt_type %d exceeds 2", pocType)
	}
	info.PicOrderCntType = uint8(pocType)
	switch pocType {
	case 0:
		maxPOCBits, err := readSPSUE(reader, "log2_max_pic_order_cnt_lsb_minus4")
		if err != nil {
			return SPSInfo{}, err
		}
		if maxPOCBits > 12 {
			return SPSInfo{}, fmt.Errorf("SPS log2_max_pic_order_cnt_lsb_minus4 exceeds 12")
		}
		info.Log2MaxPicOrderCntLsbMinus4 = maxPOCBits
		info.HasPicOrderCntLsb = true
	case 1:
		info.DeltaPicOrderAlwaysZero, err = reader.ReadBit()
		if err != nil {
			return SPSInfo{}, fmt.Errorf("SPS delta_pic_order_always_zero_flag: %w", err)
		}
		info.OffsetForNonRefPic, err = reader.ReadSE()
		if err != nil {
			return SPSInfo{}, fmt.Errorf("SPS offset_for_non_ref_pic: %w", err)
		}
		info.OffsetForTopToBottomField, err = reader.ReadSE()
		if err != nil {
			return SPSInfo{}, fmt.Errorf("SPS offset_for_top_to_bottom_field: %w", err)
		}
		cycleCount, err := readSPSUE(reader, "num_ref_frames_in_pic_order_cnt_cycle")
		if err != nil {
			return SPSInfo{}, err
		}
		if cycleCount > 255 {
			return SPSInfo{}, fmt.Errorf("SPS POC cycle count %d exceeds 255", cycleCount)
		}
		info.OffsetForRefFrame = make([]int64, 0, cycleCount)
		for index := uint32(0); index < cycleCount; index++ {
			offset, err := reader.ReadSE()
			if err != nil {
				return SPSInfo{}, fmt.Errorf("SPS offset_for_ref_frame[%d]: %w", index, err)
			}
			info.OffsetForRefFrame = append(info.OffsetForRefFrame, offset)
		}
	case 2:
	}
	info.MaxNumRefFrames, err = readSPSUE(reader, "max_num_ref_frames")
	if err != nil {
		return SPSInfo{}, err
	}
	info.GapsInFrameNumValueAllowed, err = reader.ReadBit()
	if err != nil {
		return SPSInfo{}, fmt.Errorf("SPS gaps_in_frame_num_value_allowed_flag: %w", err)
	}
	widthMbsMinus1, err := readSPSUE(reader, "pic_width_in_mbs_minus1")
	if err != nil {
		return SPSInfo{}, err
	}
	heightMapUnitsMinus1, err := readSPSUE(reader, "pic_height_in_map_units_minus1")
	if err != nil {
		return SPSInfo{}, err
	}
	info.FrameMbsOnly, err = reader.ReadBit()
	if err != nil {
		return SPSInfo{}, fmt.Errorf("SPS frame_mbs_only_flag: %w", err)
	}
	if !info.FrameMbsOnly {
		info.MbAdaptiveFrameField, err = reader.ReadBit()
		if err != nil {
			return SPSInfo{}, fmt.Errorf("SPS mb_adaptive_frame_field_flag: %w", err)
		}
	}
	info.Direct8x8Inference, err = reader.ReadBit()
	if err != nil {
		return SPSInfo{}, fmt.Errorf("SPS direct_8x8_inference_flag: %w", err)
	}
	cropping, err := reader.ReadBit()
	if err != nil {
		return SPSInfo{}, fmt.Errorf("SPS frame_cropping_flag: %w", err)
	}
	var cropLeft, cropRight, cropTop, cropBottom uint32
	if cropping {
		cropLeft, err = readSPSUE(reader, "frame_crop_left_offset")
		if err == nil {
			cropRight, err = readSPSUE(reader, "frame_crop_right_offset")
		}
		if err == nil {
			cropTop, err = readSPSUE(reader, "frame_crop_top_offset")
		}
		if err == nil {
			cropBottom, err = readSPSUE(reader, "frame_crop_bottom_offset")
		}
		if err != nil {
			return SPSInfo{}, err
		}
	}
	if _, err := reader.ReadBit(); err != nil {
		return SPSInfo{}, fmt.Errorf("SPS vui_parameters_present_flag: %w", err)
	}
	if err := setSPSDimensions(&info, widthMbsMinus1, heightMapUnitsMinus1, info.FrameMbsOnly, cropLeft, cropRight, cropTop, cropBottom); err != nil {
		return SPSInfo{}, err
	}
	if err := validateSupportedSPS(info); err != nil {
		return SPSInfo{}, err
	}
	return info, nil
}

func validateSupportedSPS(info SPSInfo) error {
	supportedProfiles := map[uint8]struct{}{
		66: {}, 77: {}, 88: {}, 100: {}, 110: {}, 122: {}, 244: {},
	}
	if _, ok := supportedProfiles[info.ProfileIDC]; !ok {
		return fmt.Errorf("unsupported SPS profile_idc %d: only progressive 8-bit 4:2:0 profiles are supported", info.ProfileIDC)
	}
	if info.ChromaFormatIDC != 1 {
		return fmt.Errorf("unsupported SPS chroma_format_idc %d: only 4:2:0 is supported", info.ChromaFormatIDC)
	}
	if info.SeparateColourPlane {
		return fmt.Errorf("unsupported SPS separate_colour_plane_flag: only interleaved 4:2:0 is supported")
	}
	if info.BitDepthLuma != 8 || info.BitDepthChroma != 8 {
		return fmt.Errorf("unsupported SPS bit depth %d/%d: only 8-bit 4:2:0 is supported", info.BitDepthLuma, info.BitDepthChroma)
	}
	if !info.FrameMbsOnly || info.MbAdaptiveFrameField {
		return fmt.Errorf("unsupported interlaced or adaptive-frame-field SPS: only progressive 8-bit 4:2:0 is supported")
	}
	return nil
}

func readSPSUE(reader *BitReader, field string) (uint32, error) {
	value, err := reader.ReadUE()
	if err != nil {
		return 0, fmt.Errorf("SPS %s: %w", field, err)
	}
	return value, nil
}

func skipSPSScalingLists(reader *BitReader, chromaFormat uint32) error {
	seqScalingMatrixPresent, err := reader.ReadBit()
	if err != nil {
		return fmt.Errorf("SPS seq_scaling_matrix_present_flag: %w", err)
	}
	if !seqScalingMatrixPresent {
		return nil
	}
	listCount := 8
	if chromaFormat == 3 {
		listCount = 12
	}
	for index := 0; index < listCount; index++ {
		present, err := reader.ReadBit()
		if err != nil {
			return fmt.Errorf("SPS seq_scaling_list_present_flag[%d]: %w", index, err)
		}
		if !present {
			continue
		}
		size := 16
		if index >= 6 {
			size = 64
		}
		lastScale, nextScale := int64(8), int64(8)
		for listIndex := 0; listIndex < size; listIndex++ {
			if nextScale != 0 {
				deltaScale, err := reader.ReadSE()
				if err != nil {
					return fmt.Errorf("SPS scaling list %d delta_scale: %w", index, err)
				}
				nextScale = ((lastScale+deltaScale+256)%256 + 256) % 256
			}
			if nextScale != 0 {
				lastScale = nextScale
			}
		}
	}
	return nil
}

func setSPSDimensions(info *SPSInfo, widthMbsMinus1, heightMapUnitsMinus1 uint32, frameMbsOnly bool, cropLeft, cropRight, cropTop, cropBottom uint32) error {
	frameFactor := uint64(1)
	if !frameMbsOnly {
		frameFactor = 2
	}
	codedWidth := (uint64(widthMbsMinus1) + 1) * 16
	codedHeight := (uint64(heightMapUnitsMinus1) + 1) * frameFactor * 16
	chromaArrayType := info.ChromaFormatIDC
	if info.SeparateColourPlane {
		chromaArrayType = 0
	}
	subWidthC, subHeightC := uint64(1), uint64(1)
	switch chromaArrayType {
	case 1:
		subWidthC, subHeightC = 2, 2
	case 2:
		subWidthC, subHeightC = 2, 1
	case 3:
		subWidthC, subHeightC = 1, 1
	}
	cropUnitX, cropUnitY := uint64(1), frameFactor
	if chromaArrayType != 0 {
		cropUnitX = subWidthC
		cropUnitY = subHeightC * frameFactor
	}
	cropPixelsLeft := uint64(cropLeft) * cropUnitX
	cropPixelsRight := uint64(cropRight) * cropUnitX
	cropPixelsTop := uint64(cropTop) * cropUnitY
	cropPixelsBottom := uint64(cropBottom) * cropUnitY
	if cropPixelsLeft+cropPixelsRight >= codedWidth || cropPixelsTop+cropPixelsBottom >= codedHeight {
		return fmt.Errorf("SPS frame cropping removes the entire picture: coded %dx%d, pixel crop %d,%d,%d,%d", codedWidth, codedHeight, cropPixelsLeft, cropPixelsRight, cropPixelsTop, cropPixelsBottom)
	}
	displayWidth := codedWidth - cropPixelsLeft - cropPixelsRight
	displayHeight := codedHeight - cropPixelsTop - cropPixelsBottom
	if codedWidth > uint64(^uint32(0)) || codedHeight > uint64(^uint32(0)) {
		return fmt.Errorf("SPS coded dimensions exceed uint32")
	}
	info.CodedWidth = uint32(codedWidth)
	info.CodedHeight = uint32(codedHeight)
	info.Width = uint32(displayWidth)
	info.Height = uint32(displayHeight)
	info.FrameCropLeft = uint32(cropPixelsLeft)
	info.FrameCropRight = uint32(cropPixelsRight)
	info.FrameCropTop = uint32(cropPixelsTop)
	info.FrameCropBottom = uint32(cropPixelsBottom)
	return nil
}

func profileHasChromaDepthSyntax(profileIDC uint8) bool {
	switch profileIDC {
	case 44, 83, 86, 100, 110, 118, 122, 128, 134, 135, 138, 139, 144, 244:
		return true
	default:
		return false
	}
}
