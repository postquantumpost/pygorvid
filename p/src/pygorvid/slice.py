"""Common H.264 slice-header parsing through primary-picture identity."""

from dataclasses import dataclass

from .bitreader import BitReader, BitstreamError
from .nal import NALUnitError, ebsp_to_rbsp, parse_nal_header
from .pps import PPSInfo
from .sps import SPSInfo


class SliceHeaderError(ValueError):
    """Raised when common slice-header syntax is malformed or inconsistent."""


@dataclass(frozen=True)
class RefPicListModification:
    modification_of_pic_nums_idc: int
    value: int


@dataclass(frozen=True)
class MemoryManagementOperation:
    operation: int
    operands: tuple[int, ...]


@dataclass(frozen=True)
class SliceHeader:
    first_macroblock_in_slice: int
    slice_type: int
    picture_parameter_set_id: int
    colour_plane_id: int
    has_colour_plane_id: bool
    frame_num: int
    field_pic_flag: bool
    bottom_field_flag: bool
    idr: bool
    idr_pic_id: int
    pic_order_cnt_lsb: int
    has_pic_order_cnt_lsb: bool
    delta_pic_order_bottom: int
    has_delta_pic_order_bottom: bool
    delta_pic_order_cnt0: int
    delta_pic_order_cnt1: int
    has_delta_pic_order_cnt0: bool
    has_delta_pic_order_cnt1: bool
    nal_ref_idc: int
    separate_colour_plane: bool
    redundant_pic_cnt: int
    has_redundant_pic_cnt: bool
    direct_spatial_mv_pred: bool
    has_direct_spatial_mv_pred: bool
    num_ref_idx_active_override: bool
    num_ref_idx_l0_active_minus1: int
    num_ref_idx_l1_active_minus1: int
    ref_pic_list_reordering_l0: tuple[RefPicListModification, ...]
    ref_pic_list_reordering_l1: tuple[RefPicListModification, ...]
    no_output_of_prior_pics: bool
    long_term_reference: bool
    adaptive_ref_pic_marking: bool
    memory_management_operations: tuple[MemoryManagementOperation, ...]
    cabac_init_idc: int | None
    slice_qp_delta: int
    disable_deblocking_filter_idc: int | None
    slice_alpha_c0_offset_div2: int
    slice_beta_offset_div2: int
    slice_data_bit_offset: int
    pic_order_cnt_type: int

    def picture_identity(self) -> "PictureIdentity":
        return PictureIdentity(
            frame_num=self.frame_num,
            picture_parameter_set_id=self.picture_parameter_set_id,
            separate_colour_plane=self.separate_colour_plane,
            colour_plane_id=self.colour_plane_id,
            field_pic_flag=self.field_pic_flag,
            bottom_field_flag=self.bottom_field_flag,
            nal_ref_idc_zero=self.nal_ref_idc == 0,
            idr=self.idr,
            idr_pic_id=self.idr_pic_id,
            has_idr_pic_id=self.idr,
            pic_order_cnt_lsb=self.pic_order_cnt_lsb,
            has_pic_order_cnt_lsb=self.has_pic_order_cnt_lsb,
            delta_pic_order_bottom=self.delta_pic_order_bottom,
            has_delta_pic_order_bottom=self.has_delta_pic_order_bottom,
            delta_pic_order_cnt0=self.delta_pic_order_cnt0,
            delta_pic_order_cnt1=self.delta_pic_order_cnt1,
            has_delta_pic_order_cnt0=self.has_delta_pic_order_cnt0,
            has_delta_pic_order_cnt1=self.has_delta_pic_order_cnt1,
            pic_order_cnt_type=self.pic_order_cnt_type,
        )


@dataclass(frozen=True)
class PictureIdentity:
    frame_num: int
    picture_parameter_set_id: int
    separate_colour_plane: bool
    colour_plane_id: int
    field_pic_flag: bool
    bottom_field_flag: bool
    nal_ref_idc_zero: bool
    idr: bool
    idr_pic_id: int
    has_idr_pic_id: bool
    pic_order_cnt_lsb: int
    has_pic_order_cnt_lsb: bool
    delta_pic_order_bottom: int
    has_delta_pic_order_bottom: bool
    delta_pic_order_cnt0: int
    delta_pic_order_cnt1: int
    has_delta_pic_order_cnt0: bool
    has_delta_pic_order_cnt1: bool
    pic_order_cnt_type: int


def parse_slice_header(nal: bytes, sps: SPSInfo, pps: PPSInfo) -> SliceHeader:
    try:
        nal_header = parse_nal_header(nal)
        if nal_header.unit_type not in (1, 5):
            raise SliceHeaderError(f"NAL unit type {nal_header.unit_type} is not a coded slice")
        rbsp = ebsp_to_rbsp(nal[1:])
    except NALUnitError as error:
        raise SliceHeaderError(str(error)) from error
    if pps.sequence_parameter_set_id != sps.sps_id:
        raise SliceHeaderError(
            f"PPS references SPS {pps.sequence_parameter_set_id}, supplied SPS is {sps.sps_id}"
        )

    reader = BitReader(rbsp)
    first_macroblock = _read_ue(reader, "first_mb_in_slice")
    slice_type = _read_ue(reader, "slice_type")
    if slice_type > 9:
        raise SliceHeaderError(f"slice_type {slice_type} exceeds 9")
    pps_id = _read_ue(reader, "pic_parameter_set_id")
    if pps_id != pps.picture_parameter_set_id:
        raise SliceHeaderError(f"slice references PPS {pps_id}, supplied PPS is {pps.picture_parameter_set_id}")

    colour_plane_id = 0
    if sps.separate_colour_plane:
        try:
            colour_plane_id = reader.read_bits(2)
        except BitstreamError as error:
            raise SliceHeaderError(f"slice colour_plane_id: {error}") from error
        if colour_plane_id > 2:
            raise SliceHeaderError(f"colour_plane_id {colour_plane_id} is reserved")

    frame_num_bits = sps.log2_max_frame_num_minus4 + 4
    try:
        frame_num = reader.read_bits(frame_num_bits)
    except BitstreamError as error:
        raise SliceHeaderError(f"slice frame_num: {error}") from error

    field_pic_flag = False
    bottom_field_flag = False
    if not sps.frame_mbs_only:
        try:
            field_pic_flag = reader.read_bit()
            if field_pic_flag:
                bottom_field_flag = reader.read_bit()
        except BitstreamError as error:
            raise SliceHeaderError(f"slice field flags: {error}") from error

    is_idr = nal_header.unit_type == 5
    idr_pic_id = _read_ue(reader, "idr_pic_id") if is_idr else 0
    if idr_pic_id > 65535:
        raise SliceHeaderError(f"idr_pic_id {idr_pic_id} exceeds 65535")

    result = {
        "first_macroblock_in_slice": first_macroblock,
        "slice_type": slice_type,
        "picture_parameter_set_id": pps_id,
        "colour_plane_id": colour_plane_id,
        "has_colour_plane_id": sps.separate_colour_plane,
        "frame_num": frame_num,
        "field_pic_flag": field_pic_flag,
        "bottom_field_flag": bottom_field_flag,
        "idr": is_idr,
        "idr_pic_id": idr_pic_id,
        "pic_order_cnt_lsb": 0,
        "has_pic_order_cnt_lsb": False,
        "delta_pic_order_bottom": 0,
        "has_delta_pic_order_bottom": False,
        "delta_pic_order_cnt0": 0,
        "delta_pic_order_cnt1": 0,
        "has_delta_pic_order_cnt0": False,
        "has_delta_pic_order_cnt1": False,
        "nal_ref_idc": nal_header.reference_idc,
        "separate_colour_plane": sps.separate_colour_plane,
        "redundant_pic_cnt": 0,
        "has_redundant_pic_cnt": False,
        "direct_spatial_mv_pred": False,
        "has_direct_spatial_mv_pred": False,
        "num_ref_idx_active_override": False,
        "num_ref_idx_l0_active_minus1": 0,
        "num_ref_idx_l1_active_minus1": 0,
        "ref_pic_list_reordering_l0": (),
        "ref_pic_list_reordering_l1": (),
        "no_output_of_prior_pics": False,
        "long_term_reference": False,
        "adaptive_ref_pic_marking": False,
        "memory_management_operations": (),
        "cabac_init_idc": None,
        "slice_qp_delta": 0,
        "disable_deblocking_filter_idc": None,
        "slice_alpha_c0_offset_div2": 0,
        "slice_beta_offset_div2": 0,
        "slice_data_bit_offset": 0,
        "pic_order_cnt_type": sps.pic_order_cnt_type,
    }
    if sps.pic_order_cnt_type == 0:
        if sps.log2_max_pic_order_cnt_lsb_minus4 is None:
            raise SliceHeaderError("SPS POC type 0 lacks log2_max_pic_order_cnt_lsb_minus4")
        poc_bits = sps.log2_max_pic_order_cnt_lsb_minus4 + 4
        try:
            result["pic_order_cnt_lsb"] = reader.read_bits(poc_bits)
        except BitstreamError as error:
            raise SliceHeaderError(f"slice pic_order_cnt_lsb: {error}") from error
        result["has_pic_order_cnt_lsb"] = True
        if pps.bottom_field_pic_order_in_frame_present and not field_pic_flag:
            result["delta_pic_order_bottom"] = _read_se(reader, "delta_pic_order_bottom")
            result["has_delta_pic_order_bottom"] = True
    elif sps.pic_order_cnt_type == 1:
        if not sps.delta_pic_order_always_zero:
            result["delta_pic_order_cnt0"] = _read_se(reader, "delta_pic_order_cnt[0]")
            result["has_delta_pic_order_cnt0"] = True
            if pps.bottom_field_pic_order_in_frame_present and not field_pic_flag:
                result["delta_pic_order_cnt1"] = _read_se(reader, "delta_pic_order_cnt[1]")
                result["has_delta_pic_order_cnt1"] = True
    elif sps.pic_order_cnt_type != 2:
        raise SliceHeaderError(f"unsupported SPS pic_order_cnt_type {sps.pic_order_cnt_type}")

    if pps.redundant_pic_cnt_present:
        result["redundant_pic_cnt"] = _read_ue(reader, "redundant_pic_cnt")
        if result["redundant_pic_cnt"] > 127:
            raise SliceHeaderError("redundant_pic_cnt exceeds 127")
        result["has_redundant_pic_cnt"] = True

    normalized_slice_type = slice_type % 5
    if normalized_slice_type in (0, 1):
        if normalized_slice_type == 1:
            try:
                result["direct_spatial_mv_pred"] = reader.read_bit()
            except BitstreamError as error:
                raise SliceHeaderError(f"slice direct_spatial_mv_pred_flag: {error}") from error
            result["has_direct_spatial_mv_pred"] = True
        try:
            result["num_ref_idx_active_override"] = reader.read_bit()
        except BitstreamError as error:
            raise SliceHeaderError(f"slice num_ref_idx_active_override_flag: {error}") from error
        if result["num_ref_idx_active_override"]:
            result["num_ref_idx_l0_active_minus1"] = _read_ue(reader, "num_ref_idx_l0_active_minus1")
            if result["num_ref_idx_l0_active_minus1"] > 31:
                raise SliceHeaderError("num_ref_idx_l0_active_minus1 exceeds 31")
            if normalized_slice_type == 1:
                result["num_ref_idx_l1_active_minus1"] = _read_ue(reader, "num_ref_idx_l1_active_minus1")
                if result["num_ref_idx_l1_active_minus1"] > 31:
                    raise SliceHeaderError("num_ref_idx_l1_active_minus1 exceeds 31")
    elif normalized_slice_type not in (2,):
        raise SliceHeaderError(f"unsupported slice_type {slice_type}")

    if normalized_slice_type in (0, 1):
        result["ref_pic_list_reordering_l0"] = _read_ref_pic_list_reordering(reader, "l0")
        if normalized_slice_type == 1:
            result["ref_pic_list_reordering_l1"] = _read_ref_pic_list_reordering(reader, "l1")

    weighted_pred_applicable = pps.weighted_pred and normalized_slice_type in (0, 3)
    weighted_bipred_applicable = pps.weighted_bipred_idc == 1 and normalized_slice_type == 1
    if weighted_pred_applicable or weighted_bipred_applicable:
        raise SliceHeaderError("unsupported weighted prediction in slice header")

    if nal_header.reference_idc:
        if is_idr:
            try:
                result["no_output_of_prior_pics"] = reader.read_bit()
                result["long_term_reference"] = reader.read_bit()
            except BitstreamError as error:
                raise SliceHeaderError(f"slice IDR reference marking: {error}") from error
        else:
            try:
                result["adaptive_ref_pic_marking"] = reader.read_bit()
            except BitstreamError as error:
                raise SliceHeaderError(f"slice adaptive_ref_pic_marking_mode_flag: {error}") from error
            if result["adaptive_ref_pic_marking"]:
                result["memory_management_operations"] = _read_memory_management_operations(reader)

    if pps.entropy_coding_mode and normalized_slice_type != 2:
        cabac_init_idc = _read_ue(reader, "cabac_init_idc")
        if cabac_init_idc > 2:
            raise SliceHeaderError(f"cabac_init_idc {cabac_init_idc} exceeds 2")
        result["cabac_init_idc"] = cabac_init_idc

    result["slice_qp_delta"] = _read_se(reader, "slice_qp_delta")
    slice_qpy = 26 + pps.pic_init_qp_minus26 + result["slice_qp_delta"]
    qp_bd_offset_y = 6 * (sps.bit_depth_luma - 8)
    if not -qp_bd_offset_y <= slice_qpy <= 51:
        raise SliceHeaderError(f"slice QP {slice_qpy} is out of range")

    if pps.deblocking_filter_control_present:
        disable_idc = _read_ue(reader, "disable_deblocking_filter_idc")
        if disable_idc > 2:
            raise SliceHeaderError(f"disable_deblocking_filter_idc {disable_idc} exceeds 2")
        result["disable_deblocking_filter_idc"] = disable_idc
        if disable_idc != 1:
            alpha = _read_se(reader, "slice_alpha_c0_offset_div2")
            beta = _read_se(reader, "slice_beta_offset_div2")
            if not -6 <= alpha <= 6 or not -6 <= beta <= 6:
                raise SliceHeaderError("slice deblocking offset exceeds [-6,6]")
            result["slice_alpha_c0_offset_div2"] = alpha
            result["slice_beta_offset_div2"] = beta
    if pps.entropy_coding_mode:
        while reader.bit_offset % 8:
            try:
                alignment_bit = reader.read_bit()
            except BitstreamError as error:
                raise SliceHeaderError(f"slice cabac_alignment_one_bit: {error}") from error
            if not alignment_bit:
                raise SliceHeaderError("slice cabac_alignment_one_bit is not 1")
    result["slice_data_bit_offset"] = reader.bit_offset
    return SliceHeader(**result)


def _read_ue(reader: BitReader, field: str) -> int:
    try:
        return reader.read_ue()
    except BitstreamError as error:
        raise SliceHeaderError(f"slice {field}: {error}") from error


def _read_se(reader: BitReader, field: str) -> int:
    try:
        return reader.read_se()
    except BitstreamError as error:
        raise SliceHeaderError(f"slice {field}: {error}") from error


def _read_ref_pic_list_reordering(reader: BitReader, list_name: str) -> tuple[RefPicListModification, ...]:
    try:
        reordering = reader.read_bit()
    except BitstreamError as error:
        raise SliceHeaderError(f"slice ref_pic_list_reordering_flag_{list_name}: {error}") from error
    if not reordering:
        return ()
    modifications = []
    for index in range(64):
        modification_idc = _read_ue(reader, f"modification_of_pic_nums_idc_{list_name}")
        if modification_idc == 3:
            return tuple(modifications)
        if modification_idc > 2:
            raise SliceHeaderError(f"slice modification_of_pic_nums_idc {modification_idc} is invalid")
        value = _read_ue(reader, f"ref_pic_list_modification_{list_name}[{index}]")
        modifications.append(RefPicListModification(modification_idc, value))
    raise SliceHeaderError("slice ref-list reordering exceeds 64 operations")


def _read_memory_management_operations(reader: BitReader) -> tuple[MemoryManagementOperation, ...]:
    operations = []
    operand_counts = {1: 1, 2: 1, 3: 2, 4: 1, 5: 0, 6: 1}
    for _ in range(32):
        operation = _read_ue(reader, "memory_management_control_operation")
        if operation == 0:
            return tuple(operations)
        if operation not in operand_counts:
            raise SliceHeaderError(f"memory_management_control_operation {operation} is invalid")
        operands = tuple(
            _read_ue(reader, "memory_management_operation_operand")
            for _ in range(operand_counts[operation])
        )
        operations.append(MemoryManagementOperation(operation, operands))
    raise SliceHeaderError("decoded-reference marking exceeds 32 operations")


def same_primary_picture(previous: SliceHeader, current: SliceHeader) -> bool:
    left = previous.picture_identity()
    right = current.picture_identity()
    if (
        left.frame_num != right.frame_num
        or left.picture_parameter_set_id != right.picture_parameter_set_id
        or left.field_pic_flag != right.field_pic_flag
        or left.nal_ref_idc_zero != right.nal_ref_idc_zero
        or left.idr != right.idr
        or left.separate_colour_plane != right.separate_colour_plane
        or left.pic_order_cnt_type != right.pic_order_cnt_type
    ):
        return False
    if left.field_pic_flag and left.bottom_field_flag != right.bottom_field_flag:
        return False
    if left.separate_colour_plane and left.colour_plane_id != right.colour_plane_id:
        return False
    if left.idr and left.idr_pic_id != right.idr_pic_id:
        return False
    if left.pic_order_cnt_type == 0:
        return (
            left.has_pic_order_cnt_lsb == right.has_pic_order_cnt_lsb
            and left.pic_order_cnt_lsb == right.pic_order_cnt_lsb
            and left.has_delta_pic_order_bottom == right.has_delta_pic_order_bottom
            and left.delta_pic_order_bottom == right.delta_pic_order_bottom
        )
    if left.pic_order_cnt_type == 1:
        return (
            left.has_delta_pic_order_cnt0 == right.has_delta_pic_order_cnt0
            and left.delta_pic_order_cnt0 == right.delta_pic_order_cnt0
            and left.has_delta_pic_order_cnt1 == right.has_delta_pic_order_cnt1
            and left.delta_pic_order_cnt1 == right.delta_pic_order_cnt1
        )
    return left.pic_order_cnt_type == 2


def group_slices_into_pictures(slices: list[SliceHeader]) -> list[list[SliceHeader]]:
    groups: list[list[SliceHeader]] = []
    for slice_header in slices:
        if not groups or not same_primary_picture(groups[-1][0], slice_header):
            groups.append([slice_header])
        else:
            groups[-1].append(slice_header)
    return groups