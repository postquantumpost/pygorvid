"""Parsing of H.264 picture parameter set identifiers and core flags."""

from dataclasses import dataclass

from .bitreader import BitReader, BitstreamError
from .nal import NALUnitError, ebsp_to_rbsp, parse_nal_header


class PPSParseError(ValueError):
    """Raised when a PPS header or core syntax is malformed."""


@dataclass(frozen=True)
class PPSInfo:
    picture_parameter_set_id: int
    sequence_parameter_set_id: int
    entropy_coding_mode: bool
    bottom_field_pic_order_in_frame_present: bool
    num_slice_groups_minus1: int
    num_ref_idx_l0_default_active_minus1: int
    num_ref_idx_l1_default_active_minus1: int
    weighted_pred: bool
    weighted_bipred_idc: int
    pic_init_qp_minus26: int
    pic_init_qs_minus26: int
    chroma_qp_index_offset: int
    deblocking_filter_control_present: bool
    constrained_intra_pred: bool
    redundant_pic_cnt_present: bool
    has_extension: bool
    transform_8x8_mode: bool
    pic_scaling_matrix_present: bool
    second_chroma_qp_index_offset: int


def parse_pps(nal: bytes) -> PPSInfo:
    try:
        header = parse_nal_header(nal)
        if header.unit_type != 8:
            raise PPSParseError(f"NAL unit type {header.unit_type} is not a PPS")
        rbsp = ebsp_to_rbsp(nal[1:])
    except NALUnitError as error:
        raise PPSParseError(str(error)) from error

    reader = BitReader(rbsp)
    picture_parameter_set_id = _read_ue(reader, "pic_parameter_set_id")
    if picture_parameter_set_id > 255:
        raise PPSParseError(f"PPS pic_parameter_set_id {picture_parameter_set_id} exceeds 255")
    sequence_parameter_set_id = _read_ue(reader, "seq_parameter_set_id")
    if sequence_parameter_set_id > 31:
        raise PPSParseError(f"PPS seq_parameter_set_id {sequence_parameter_set_id} exceeds 31")
    try:
        entropy_coding_mode = reader.read_bit()
    except BitstreamError as error:
        raise PPSParseError(f"PPS entropy_coding_mode_flag: {error}") from error
    try:
        bottom_field_pic_order_in_frame_present = reader.read_bit()
    except BitstreamError as error:
        raise PPSParseError(f"PPS bottom_field_pic_order_in_frame_present_flag: {error}") from error
    num_slice_groups_minus1 = _read_ue(reader, "num_slice_groups_minus1")
    if num_slice_groups_minus1:
        raise PPSParseError(f"unsupported PPS slice groups: num_slice_groups_minus1={num_slice_groups_minus1}")
    num_ref_idx_l0 = _read_ue(reader, "num_ref_idx_l0_default_active_minus1")
    num_ref_idx_l1 = _read_ue(reader, "num_ref_idx_l1_default_active_minus1")
    if num_ref_idx_l0 > 31 or num_ref_idx_l1 > 31:
        raise PPSParseError("PPS default reference index exceeds 31")
    try:
        weighted_pred = reader.read_bit()
    except BitstreamError as error:
        raise PPSParseError(f"PPS weighted_pred_flag: {error}") from error
    try:
        weighted_bipred_idc = reader.read_bits(2)
    except BitstreamError as error:
        raise PPSParseError(f"PPS weighted_bipred_idc: {error}") from error
    if weighted_bipred_idc > 2:
        raise PPSParseError(f"PPS weighted_bipred_idc {weighted_bipred_idc} is reserved")
    pic_init_qp_minus26 = _read_se(reader, "pic_init_qp_minus26")
    pic_init_qs_minus26 = _read_se(reader, "pic_init_qs_minus26")
    chroma_qp_index_offset = _read_se(reader, "chroma_qp_index_offset")
    if not -26 <= pic_init_qp_minus26 <= 25 or not -26 <= pic_init_qs_minus26 <= 25:
        raise PPSParseError("PPS initial QP/QS offset is outside [-26,25]")
    if not -12 <= chroma_qp_index_offset <= 12:
        raise PPSParseError("PPS chroma QP index offset is outside [-12,12]")
    try:
        deblocking_filter_control_present = reader.read_bit()
        constrained_intra_pred = reader.read_bit()
        redundant_pic_cnt_present = reader.read_bit()
    except BitstreamError as error:
        raise PPSParseError(f"PPS core flags: {error}") from error

    has_extension = reader.more_rbsp_data()
    transform_8x8_mode = False
    pic_scaling_matrix_present = False
    second_chroma_qp_index_offset = chroma_qp_index_offset
    if has_extension:
        try:
            transform_8x8_mode = reader.read_bit()
            pic_scaling_matrix_present = reader.read_bit()
        except BitstreamError as error:
            raise PPSParseError(f"PPS extension flags: {error}") from error
        if pic_scaling_matrix_present:
            raise PPSParseError("unsupported PPS pic_scaling_matrix_present_flag")
        second_chroma_qp_index_offset = _read_se(reader, "second_chroma_qp_index_offset")
        if not -12 <= second_chroma_qp_index_offset <= 12:
            raise PPSParseError("PPS second chroma QP index offset is outside [-12,12]")
    return PPSInfo(
        picture_parameter_set_id=picture_parameter_set_id,
        sequence_parameter_set_id=sequence_parameter_set_id,
        entropy_coding_mode=entropy_coding_mode,
        bottom_field_pic_order_in_frame_present=bottom_field_pic_order_in_frame_present,
        num_slice_groups_minus1=num_slice_groups_minus1,
        num_ref_idx_l0_default_active_minus1=num_ref_idx_l0,
        num_ref_idx_l1_default_active_minus1=num_ref_idx_l1,
        weighted_pred=weighted_pred,
        weighted_bipred_idc=weighted_bipred_idc,
        pic_init_qp_minus26=pic_init_qp_minus26,
        pic_init_qs_minus26=pic_init_qs_minus26,
        chroma_qp_index_offset=chroma_qp_index_offset,
        deblocking_filter_control_present=deblocking_filter_control_present,
        constrained_intra_pred=constrained_intra_pred,
        redundant_pic_cnt_present=redundant_pic_cnt_present,
        has_extension=has_extension,
        transform_8x8_mode=transform_8x8_mode,
        pic_scaling_matrix_present=pic_scaling_matrix_present,
        second_chroma_qp_index_offset=second_chroma_qp_index_offset,
    )


def _read_ue(reader: BitReader, field: str) -> int:
    try:
        return reader.read_ue()
    except BitstreamError as error:
        raise PPSParseError(f"PPS {field}: {error}") from error


def _read_se(reader: BitReader, field: str) -> int:
    try:
        return reader.read_se()
    except BitstreamError as error:
        raise PPSParseError(f"PPS {field}: {error}") from error