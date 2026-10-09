"""Parsing of H.264 sequence parameter set profile and sample format fields."""

from dataclasses import dataclass

from .bitreader import BitReader, BitstreamError
from .nal import NALUnitError, ebsp_to_rbsp, parse_nal_header


class SPSParseError(ValueError):
    """Raised when an SPS is malformed or contains invalid profile fields."""


@dataclass(frozen=True)
class SPSInfo:
    profile_idc: int
    constraint_flags: int
    level_idc: int
    sps_id: int
    chroma_format_idc: int
    separate_colour_plane: bool
    bit_depth_luma: int
    bit_depth_chroma: int
    coded_width: int
    coded_height: int
    width: int
    height: int
    frame_crop_left: int
    frame_crop_right: int
    frame_crop_top: int
    frame_crop_bottom: int
    log2_max_frame_num_minus4: int
    pic_order_cnt_type: int
    log2_max_pic_order_cnt_lsb_minus4: int | None
    delta_pic_order_always_zero: bool
    offset_for_non_ref_pic: int | None
    offset_for_top_to_bottom_field: int | None
    offset_for_ref_frame: tuple[int, ...]
    max_num_ref_frames: int
    gaps_in_frame_num_value_allowed: bool
    frame_mbs_only: bool
    mb_adaptive_frame_field: bool
    direct_8x8_inference: bool


_PROFILES_WITH_CHROMA_DEPTH_SYNTAX = {
    44,
    83,
    86,
    100,
    110,
    118,
    122,
    128,
    134,
    135,
    138,
    139,
    144,
    244,
}


def parse_sps(nal: bytes) -> SPSInfo:
    try:
        header = parse_nal_header(nal)
        if header.unit_type != 7:
            raise SPSParseError(f"NAL unit type {header.unit_type} is not an SPS")
        rbsp = ebsp_to_rbsp(nal[1:])
    except NALUnitError as error:
        raise SPSParseError(str(error)) from error

    reader = BitReader(rbsp)
    profile_idc = _read_byte(reader, "profile_idc")
    constraint_flags = _read_byte(reader, "constraint flags")
    if constraint_flags & 0x03:
        raise SPSParseError("SPS reserved constraint bits are nonzero")
    level_idc = _read_byte(reader, "level_idc")
    sps_id = _read_ue(reader, "seq_parameter_set_id")
    if sps_id > 31:
        raise SPSParseError(f"SPS seq_parameter_set_id {sps_id} exceeds 31")

    chroma_format_idc = 1
    separate_colour_plane = False
    bit_depth_luma = 8
    bit_depth_chroma = 8
    if profile_idc in _PROFILES_WITH_CHROMA_DEPTH_SYNTAX:
        chroma_format_idc = _read_ue(reader, "chroma_format_idc")
        if chroma_format_idc > 3:
            raise SPSParseError(f"SPS chroma_format_idc {chroma_format_idc} exceeds 3")
        if chroma_format_idc == 3:
            try:
                separate_colour_plane = reader.read_bit()
            except BitstreamError as error:
                raise SPSParseError(f"SPS separate_colour_plane_flag: {error}") from error
        luma_minus_8 = _read_ue(reader, "bit_depth_luma_minus8")
        chroma_minus_8 = _read_ue(reader, "bit_depth_chroma_minus8")
        if luma_minus_8 > 6 or chroma_minus_8 > 6:
            raise SPSParseError("SPS bit depth minus 8 exceeds 6")
        bit_depth_luma = 8 + luma_minus_8
        bit_depth_chroma = 8 + chroma_minus_8

        try:
            reader.read_bit()  # qpprime_y_zero_transform_bypass_flag
        except BitstreamError as error:
            raise SPSParseError(f"SPS qpprime_y_zero_transform_bypass_flag: {error}") from error
        _skip_scaling_lists(reader, chroma_format_idc)

    log2_max_frame_num_minus4 = _read_ue(reader, "log2_max_frame_num_minus4")
    if log2_max_frame_num_minus4 > 12:
        raise SPSParseError("SPS log2_max_frame_num_minus4 exceeds 12")
    poc_type = _read_ue(reader, "pic_order_cnt_type")
    log2_max_pic_order_cnt_lsb_minus4 = None
    delta_pic_order_always_zero = False
    offset_for_non_ref_pic = None
    offset_for_top_to_bottom_field = None
    offset_for_ref_frame = ()
    if poc_type == 0:
        log2_max_poc_lsb_minus4 = _read_ue(reader, "log2_max_pic_order_cnt_lsb_minus4")
        if log2_max_poc_lsb_minus4 > 12:
            raise SPSParseError("SPS log2_max_pic_order_cnt_lsb_minus4 exceeds 12")
        log2_max_pic_order_cnt_lsb_minus4 = log2_max_poc_lsb_minus4
    elif poc_type == 1:
        try:
            delta_pic_order_always_zero = reader.read_bit()
        except BitstreamError as error:
            raise SPSParseError(f"SPS delta_pic_order_always_zero_flag: {error}") from error
        offset_for_non_ref_pic = _read_se(reader, "offset_for_non_ref_pic")
        offset_for_top_to_bottom_field = _read_se(reader, "offset_for_top_to_bottom_field")
        cycle_count = _read_ue(reader, "num_ref_frames_in_pic_order_cnt_cycle")
        if cycle_count > 255:
            raise SPSParseError("SPS POC cycle count exceeds 255")
        cycle_offsets = []
        for index in range(cycle_count):
            cycle_offsets.append(_read_se(reader, f"offset_for_ref_frame[{index}]"))
        offset_for_ref_frame = tuple(cycle_offsets)
    elif poc_type != 2:
        raise SPSParseError(f"SPS pic_order_cnt_type {poc_type} exceeds 2")

    max_num_ref_frames = _read_ue(reader, "max_num_ref_frames")
    try:
        gaps_in_frame_num_value_allowed = reader.read_bit()
    except BitstreamError as error:
        raise SPSParseError(f"SPS gaps_in_frame_num_value_allowed_flag: {error}") from error
    width_mbs_minus_1 = _read_ue(reader, "pic_width_in_mbs_minus1")
    height_map_units_minus_1 = _read_ue(reader, "pic_height_in_map_units_minus1")
    try:
        frame_mbs_only = reader.read_bit()
        mb_adaptive_frame_field = False
        if not frame_mbs_only:
            mb_adaptive_frame_field = reader.read_bit()
        direct_8x8_inference = reader.read_bit()
        cropping = reader.read_bit()
    except BitstreamError as error:
        raise SPSParseError(f"SPS frame dimensions: {error}") from error
    crop = [0, 0, 0, 0]
    if cropping:
        crop = [
            _read_ue(reader, "frame_crop_left_offset"),
            _read_ue(reader, "frame_crop_right_offset"),
            _read_ue(reader, "frame_crop_top_offset"),
            _read_ue(reader, "frame_crop_bottom_offset"),
        ]
    try:
        reader.read_bit()  # vui_parameters_present_flag
    except BitstreamError as error:
        raise SPSParseError(f"SPS vui_parameters_present_flag: {error}") from error
    dimensions = _dimensions(
        chroma_format_idc,
        separate_colour_plane,
        width_mbs_minus_1,
        height_map_units_minus_1,
        frame_mbs_only,
        crop,
    )

    return SPSInfo(
        profile_idc=profile_idc,
        constraint_flags=constraint_flags,
        level_idc=level_idc,
        sps_id=sps_id,
        chroma_format_idc=chroma_format_idc,
        separate_colour_plane=separate_colour_plane,
        bit_depth_luma=bit_depth_luma,
        bit_depth_chroma=bit_depth_chroma,
        log2_max_frame_num_minus4=log2_max_frame_num_minus4,
        pic_order_cnt_type=poc_type,
        log2_max_pic_order_cnt_lsb_minus4=log2_max_pic_order_cnt_lsb_minus4,
        delta_pic_order_always_zero=delta_pic_order_always_zero,
        offset_for_non_ref_pic=offset_for_non_ref_pic,
        offset_for_top_to_bottom_field=offset_for_top_to_bottom_field,
        offset_for_ref_frame=offset_for_ref_frame,
        max_num_ref_frames=max_num_ref_frames,
        gaps_in_frame_num_value_allowed=gaps_in_frame_num_value_allowed,
        frame_mbs_only=frame_mbs_only,
        mb_adaptive_frame_field=mb_adaptive_frame_field,
        direct_8x8_inference=direct_8x8_inference,
        **dimensions,
    )


def _read_byte(reader: BitReader, field: str) -> int:
    try:
        return reader.read_bits(8)
    except BitstreamError as error:
        raise SPSParseError(f"SPS {field}: {error}") from error


def _read_ue(reader: BitReader, field: str) -> int:
    try:
        return reader.read_ue()
    except BitstreamError as error:
        raise SPSParseError(f"SPS {field}: {error}") from error


def _read_se(reader: BitReader, field: str) -> int:
    try:
        return reader.read_se()
    except BitstreamError as error:
        raise SPSParseError(f"SPS {field}: {error}") from error


def _skip_scaling_lists(reader: BitReader, chroma_format_idc: int) -> None:
    try:
        matrix_present = reader.read_bit()
    except BitstreamError as error:
        raise SPSParseError(f"SPS seq_scaling_matrix_present_flag: {error}") from error
    if not matrix_present:
        return
    list_count = 12 if chroma_format_idc == 3 else 8
    for list_index in range(list_count):
        try:
            list_present = reader.read_bit()
        except BitstreamError as error:
            raise SPSParseError(f"SPS seq_scaling_list_present_flag[{list_index}]: {error}") from error
        if not list_present:
            continue
        last_scale = next_scale = 8
        for _ in range(16 if list_index < 6 else 64):
            if next_scale:
                delta_scale = _read_se(reader, f"scaling_list[{list_index}].delta_scale")
                next_scale = (last_scale + delta_scale + 256) % 256
            if next_scale:
                last_scale = next_scale


def _dimensions(
    chroma_format_idc: int,
    separate_colour_plane: bool,
    width_mbs_minus_1: int,
    height_map_units_minus_1: int,
    frame_mbs_only: bool,
    crop: list[int],
) -> dict[str, int]:
    frame_factor = 1 if frame_mbs_only else 2
    coded_width = (width_mbs_minus_1 + 1) * 16
    coded_height = (height_map_units_minus_1 + 1) * frame_factor * 16
    chroma_array_type = 0 if separate_colour_plane else chroma_format_idc
    sub_width, sub_height = {
        0: (1, 1),
        1: (2, 2),
        2: (2, 1),
        3: (1, 1),
    }[chroma_array_type]
    crop_unit_x = 1 if chroma_array_type == 0 else sub_width
    crop_unit_y = frame_factor if chroma_array_type == 0 else sub_height * frame_factor
    crop_left, crop_right, crop_top, crop_bottom = (
        crop[0] * crop_unit_x,
        crop[1] * crop_unit_x,
        crop[2] * crop_unit_y,
        crop[3] * crop_unit_y,
    )
    if crop_left + crop_right >= coded_width or crop_top + crop_bottom >= coded_height:
        raise SPSParseError("SPS frame cropping removes the entire picture")
    if coded_width > 0xFFFFFFFF or coded_height > 0xFFFFFFFF:
        raise SPSParseError("SPS coded dimensions exceed uint32")
    return {
        "coded_width": coded_width,
        "coded_height": coded_height,
        "width": coded_width - crop_left - crop_right,
        "height": coded_height - crop_top - crop_bottom,
        "frame_crop_left": crop_left,
        "frame_crop_right": crop_right,
        "frame_crop_top": crop_top,
        "frame_crop_bottom": crop_bottom,
    }