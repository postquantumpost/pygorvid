use std::io::{self, ErrorKind};

use crate::{ebsp_to_rbsp, parse_nal_header, BitReader, PpsInfo, SpsInfo};

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct SliceHeader {
    pub first_macroblock_in_slice: u32,
    pub slice_type: u8,
    pub picture_parameter_set_id: u32,
    pub colour_plane_id: u8,
    pub has_colour_plane_id: bool,
    pub frame_num: u32,
    pub field_pic_flag: bool,
    pub bottom_field_flag: bool,
    pub idr: bool,
    pub idr_pic_id: u32,
    pub pic_order_cnt_lsb: u32,
    pub has_pic_order_cnt_lsb: bool,
    pub delta_pic_order_bottom: i64,
    pub has_delta_pic_order_bottom: bool,
    pub delta_pic_order_cnt0: i64,
    pub delta_pic_order_cnt1: i64,
    pub has_delta_pic_order_cnt0: bool,
    pub has_delta_pic_order_cnt1: bool,
    pub nal_ref_idc: u8,
    pub separate_colour_plane: bool,
    pub redundant_pic_cnt: u32,
    pub has_redundant_pic_cnt: bool,
    pub direct_spatial_mv_pred: bool,
    pub has_direct_spatial_mv_pred: bool,
    pub num_ref_idx_active_override: bool,
    pub num_ref_idx_l0_active_minus1: u32,
    pub num_ref_idx_l1_active_minus1: u32,
    pub ref_pic_list_reordering_l0: Vec<RefPicListModification>,
    pub ref_pic_list_reordering_l1: Vec<RefPicListModification>,
    pub no_output_of_prior_pics: bool,
    pub long_term_reference: bool,
    pub adaptive_ref_pic_marking: bool,
    pub memory_management_operations: Vec<MemoryManagementOperation>,
    pub cabac_init_idc: Option<u8>,
    pub slice_qp_delta: i64,
    pub disable_deblocking_filter_idc: Option<u8>,
    pub slice_alpha_c0_offset_div2: i64,
    pub slice_beta_offset_div2: i64,
    pub pic_order_cnt_type: u8,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct RefPicListModification {
    pub modification_of_pic_nums_idc: u32,
    pub value: u32,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct MemoryManagementOperation {
    pub operation: u32,
    pub operands: Vec<u32>,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct PictureIdentity {
    pub frame_num: u32,
    pub picture_parameter_set_id: u32,
    pub separate_colour_plane: bool,
    pub colour_plane_id: u8,
    pub field_pic_flag: bool,
    pub bottom_field_flag: bool,
    pub nal_ref_idc_zero: bool,
    pub idr: bool,
    pub idr_pic_id: u32,
    pub has_idr_pic_id: bool,
    pub pic_order_cnt_lsb: u32,
    pub has_pic_order_cnt_lsb: bool,
    pub delta_pic_order_bottom: i64,
    pub has_delta_pic_order_bottom: bool,
    pub delta_pic_order_cnt0: i64,
    pub delta_pic_order_cnt1: i64,
    pub has_delta_pic_order_cnt0: bool,
    pub has_delta_pic_order_cnt1: bool,
    pub pic_order_cnt_type: u8,
}

impl SliceHeader {
    pub fn picture_identity(&self) -> PictureIdentity {
        PictureIdentity {
            frame_num: self.frame_num,
            picture_parameter_set_id: self.picture_parameter_set_id,
            separate_colour_plane: self.separate_colour_plane,
            colour_plane_id: self.colour_plane_id,
            field_pic_flag: self.field_pic_flag,
            bottom_field_flag: self.bottom_field_flag,
            nal_ref_idc_zero: self.nal_ref_idc == 0,
            idr: self.idr,
            idr_pic_id: self.idr_pic_id,
            has_idr_pic_id: self.idr,
            pic_order_cnt_lsb: self.pic_order_cnt_lsb,
            has_pic_order_cnt_lsb: self.has_pic_order_cnt_lsb,
            delta_pic_order_bottom: self.delta_pic_order_bottom,
            has_delta_pic_order_bottom: self.has_delta_pic_order_bottom,
            delta_pic_order_cnt0: self.delta_pic_order_cnt0,
            delta_pic_order_cnt1: self.delta_pic_order_cnt1,
            has_delta_pic_order_cnt0: self.has_delta_pic_order_cnt0,
            has_delta_pic_order_cnt1: self.has_delta_pic_order_cnt1,
            pic_order_cnt_type: self.pic_order_cnt_type,
        }
    }
}

pub fn same_primary_picture(previous: &SliceHeader, current: &SliceHeader) -> bool {
    let left = previous.picture_identity();
    let right = current.picture_identity();
    if left.frame_num != right.frame_num
        || left.picture_parameter_set_id != right.picture_parameter_set_id
        || left.field_pic_flag != right.field_pic_flag
        || left.nal_ref_idc_zero != right.nal_ref_idc_zero
        || left.idr != right.idr
        || left.separate_colour_plane != right.separate_colour_plane
        || left.pic_order_cnt_type != right.pic_order_cnt_type
    {
        return false;
    }
    if left.field_pic_flag && left.bottom_field_flag != right.bottom_field_flag {
        return false;
    }
    if left.separate_colour_plane && left.colour_plane_id != right.colour_plane_id {
        return false;
    }
    if left.idr && left.idr_pic_id != right.idr_pic_id {
        return false;
    }
    match left.pic_order_cnt_type {
        0 => {
            left.has_pic_order_cnt_lsb == right.has_pic_order_cnt_lsb
                && left.pic_order_cnt_lsb == right.pic_order_cnt_lsb
                && left.has_delta_pic_order_bottom == right.has_delta_pic_order_bottom
                && left.delta_pic_order_bottom == right.delta_pic_order_bottom
        }
        1 => {
            left.has_delta_pic_order_cnt0 == right.has_delta_pic_order_cnt0
                && left.delta_pic_order_cnt0 == right.delta_pic_order_cnt0
                && left.has_delta_pic_order_cnt1 == right.has_delta_pic_order_cnt1
                && left.delta_pic_order_cnt1 == right.delta_pic_order_cnt1
        }
        2 => true,
        _ => false,
    }
}

pub fn group_slices_into_pictures(slices: &[SliceHeader]) -> Vec<Vec<SliceHeader>> {
    let mut groups: Vec<Vec<SliceHeader>> = Vec::new();
    for slice in slices {
        let starts_picture = groups
            .last()
            .and_then(|group| group.first())
            .map_or(true, |first| !same_primary_picture(first, slice));
        if starts_picture {
            groups.push(vec![slice.clone()]);
        } else if let Some(group) = groups.last_mut() {
            group.push(slice.clone());
        }
    }
    groups
}

pub fn parse_slice_header(nal: &[u8], sps: &SpsInfo, pps: &PpsInfo) -> io::Result<SliceHeader> {
    let nal_header = parse_nal_header(nal)?;
    if nal_header.unit_type != 1 && nal_header.unit_type != 5 {
        return Err(invalid(format!(
            "NAL unit type {} is not a coded slice",
            nal_header.unit_type
        )));
    }
    if pps.sequence_parameter_set_id != sps.sps_id {
        return Err(invalid(format!(
            "PPS references SPS {}, supplied SPS is {}",
            pps.sequence_parameter_set_id, sps.sps_id
        )));
    }
    let rbsp = ebsp_to_rbsp(&nal[1..])?;
    let mut reader = BitReader::new(&rbsp);
    let first_macroblock_in_slice = read_ue(&mut reader, "first_mb_in_slice")?;
    let slice_type = read_ue(&mut reader, "slice_type")?;
    if slice_type > 9 {
        return Err(invalid(format!("slice_type {slice_type} exceeds 9")));
    }
    let picture_parameter_set_id = read_ue(&mut reader, "pic_parameter_set_id")?;
    if picture_parameter_set_id != pps.picture_parameter_set_id {
        return Err(invalid(format!(
            "slice references PPS {picture_parameter_set_id}, supplied PPS is {}",
            pps.picture_parameter_set_id
        )));
    }
    let mut colour_plane_id = 0;
    if sps.separate_colour_plane {
        colour_plane_id = reader
            .read_bits(2)
            .map_err(|error| context("colour_plane_id", error))? as u8;
        if colour_plane_id > 2 {
            return Err(invalid(format!(
                "colour_plane_id {colour_plane_id} is reserved"
            )));
        }
    }
    let frame_num_bits = sps.log2_max_frame_num_minus4 + 4;
    let frame_num = reader
        .read_bits(frame_num_bits as u8)
        .map_err(|error| context("frame_num", error))?;
    let mut field_pic_flag = false;
    let mut bottom_field_flag = false;
    if !sps.frame_mbs_only {
        field_pic_flag = reader
            .read_bit()
            .map_err(|error| context("field_pic_flag", error))?;
        if field_pic_flag {
            bottom_field_flag = reader
                .read_bit()
                .map_err(|error| context("bottom_field_flag", error))?;
        }
    }
    let idr = nal_header.unit_type == 5;
    let idr_pic_id = if idr {
        read_ue(&mut reader, "idr_pic_id")?
    } else {
        0
    };
    if idr_pic_id > 65535 {
        return Err(invalid(format!("idr_pic_id {idr_pic_id} exceeds 65535")));
    }
    let mut header = SliceHeader {
        first_macroblock_in_slice,
        slice_type: slice_type as u8,
        picture_parameter_set_id,
        colour_plane_id,
        has_colour_plane_id: sps.separate_colour_plane,
        frame_num,
        field_pic_flag,
        bottom_field_flag,
        idr,
        idr_pic_id,
        pic_order_cnt_lsb: 0,
        has_pic_order_cnt_lsb: false,
        delta_pic_order_bottom: 0,
        has_delta_pic_order_bottom: false,
        delta_pic_order_cnt0: 0,
        delta_pic_order_cnt1: 0,
        has_delta_pic_order_cnt0: false,
        has_delta_pic_order_cnt1: false,
        nal_ref_idc: nal_header.reference_idc,
        separate_colour_plane: sps.separate_colour_plane,
        redundant_pic_cnt: 0,
        has_redundant_pic_cnt: false,
        direct_spatial_mv_pred: false,
        has_direct_spatial_mv_pred: false,
        num_ref_idx_active_override: false,
        num_ref_idx_l0_active_minus1: 0,
        num_ref_idx_l1_active_minus1: 0,
        ref_pic_list_reordering_l0: Vec::new(),
        ref_pic_list_reordering_l1: Vec::new(),
        no_output_of_prior_pics: false,
        long_term_reference: false,
        adaptive_ref_pic_marking: false,
        memory_management_operations: Vec::new(),
        cabac_init_idc: None,
        slice_qp_delta: 0,
        disable_deblocking_filter_idc: None,
        slice_alpha_c0_offset_div2: 0,
        slice_beta_offset_div2: 0,
        pic_order_cnt_type: sps.pic_order_cnt_type,
    };
    match sps.pic_order_cnt_type {
        0 => {
            let Some(poc_bits) = sps.log2_max_pic_order_cnt_lsb_minus4 else {
                return Err(invalid(
                    "SPS POC type 0 lacks log2_max_pic_order_cnt_lsb_minus4",
                ));
            };
            header.pic_order_cnt_lsb = reader
                .read_bits((poc_bits + 4) as u8)
                .map_err(|error| context("pic_order_cnt_lsb", error))?;
            header.has_pic_order_cnt_lsb = true;
            if pps.bottom_field_pic_order_in_frame_present && !field_pic_flag {
                header.delta_pic_order_bottom = read_se(&mut reader, "delta_pic_order_bottom")?;
                header.has_delta_pic_order_bottom = true;
            }
        }
        1 => {
            if !sps.delta_pic_order_always_zero {
                header.delta_pic_order_cnt0 = read_se(&mut reader, "delta_pic_order_cnt[0]")?;
                header.has_delta_pic_order_cnt0 = true;
                if pps.bottom_field_pic_order_in_frame_present && !field_pic_flag {
                    header.delta_pic_order_cnt1 = read_se(&mut reader, "delta_pic_order_cnt[1]")?;
                    header.has_delta_pic_order_cnt1 = true;
                }
            }
        }
        2 => {}
        _ => {
            return Err(invalid(format!(
                "unsupported SPS pic_order_cnt_type {}",
                sps.pic_order_cnt_type
            )))
        }
    }
    if pps.redundant_pic_cnt_present {
        header.redundant_pic_cnt = read_ue(&mut reader, "redundant_pic_cnt")?;
        if header.redundant_pic_cnt > 127 {
            return Err(invalid("redundant_pic_cnt exceeds 127"));
        }
        header.has_redundant_pic_cnt = true;
    }
    match header.slice_type % 5 {
        2 => {}
        0 | 1 => {
            if header.slice_type % 5 == 1 {
                header.direct_spatial_mv_pred = reader
                    .read_bit()
                    .map_err(|error| context("direct_spatial_mv_pred_flag", error))?;
                header.has_direct_spatial_mv_pred = true;
            }
            header.num_ref_idx_active_override = reader
                .read_bit()
                .map_err(|error| context("num_ref_idx_active_override_flag", error))?;
            if header.num_ref_idx_active_override {
                header.num_ref_idx_l0_active_minus1 =
                    read_ue(&mut reader, "num_ref_idx_l0_active_minus1")?;
                if header.num_ref_idx_l0_active_minus1 > 31 {
                    return Err(invalid("num_ref_idx_l0_active_minus1 exceeds 31"));
                }
                if header.slice_type % 5 == 1 {
                    header.num_ref_idx_l1_active_minus1 =
                        read_ue(&mut reader, "num_ref_idx_l1_active_minus1")?;
                    if header.num_ref_idx_l1_active_minus1 > 31 {
                        return Err(invalid("num_ref_idx_l1_active_minus1 exceeds 31"));
                    }
                }
            }
        }
        _ => {
            return Err(invalid(format!(
                "unsupported slice_type {}",
                header.slice_type
            )))
        }
    }
    let normalized_slice_type = header.slice_type % 5;
    if normalized_slice_type != 2 {
        header.ref_pic_list_reordering_l0 = read_ref_pic_list_reordering(&mut reader, "l0")?;
        if normalized_slice_type == 1 {
            header.ref_pic_list_reordering_l1 = read_ref_pic_list_reordering(&mut reader, "l1")?;
        }
    }
    let weighted_pred_applicable =
        pps.weighted_pred && (normalized_slice_type == 0 || normalized_slice_type == 3);
    let weighted_bipred_applicable = pps.weighted_bipred_idc == 1 && normalized_slice_type == 1;
    if weighted_pred_applicable || weighted_bipred_applicable {
        return Err(invalid("unsupported weighted prediction in slice header"));
    }
    if nal_header.reference_idc != 0 {
        if header.idr {
            header.no_output_of_prior_pics = reader
                .read_bit()
                .map_err(|error| context("no_output_of_prior_pics_flag", error))?;
            header.long_term_reference = reader
                .read_bit()
                .map_err(|error| context("long_term_reference_flag", error))?;
        } else {
            header.adaptive_ref_pic_marking = reader
                .read_bit()
                .map_err(|error| context("adaptive_ref_pic_marking_mode_flag", error))?;
            if header.adaptive_ref_pic_marking {
                header.memory_management_operations =
                    read_memory_management_operations(&mut reader)?;
            }
        }
    }
    if pps.entropy_coding_mode && normalized_slice_type != 2 {
        let cabac_init_idc = read_ue(&mut reader, "cabac_init_idc")?;
        if cabac_init_idc > 2 {
            return Err(invalid(format!(
                "cabac_init_idc {cabac_init_idc} exceeds 2"
            )));
        }
        header.cabac_init_idc = Some(cabac_init_idc as u8);
    }
    header.slice_qp_delta = read_se(&mut reader, "slice_qp_delta")?;
    let qp_bd_offset_y = 6 * (i64::from(sps.bit_depth_luma) - 8);
    let slice_qpy = 26 + pps.pic_init_qp_minus26 + header.slice_qp_delta;
    if slice_qpy < -qp_bd_offset_y || slice_qpy > 51 {
        return Err(invalid(format!("slice QP {slice_qpy} is out of range")));
    }
    if pps.deblocking_filter_control_present {
        let disable_idc = read_ue(&mut reader, "disable_deblocking_filter_idc")?;
        if disable_idc > 2 {
            return Err(invalid(format!(
                "disable_deblocking_filter_idc {disable_idc} exceeds 2"
            )));
        }
        header.disable_deblocking_filter_idc = Some(disable_idc as u8);
        if disable_idc != 1 {
            header.slice_alpha_c0_offset_div2 = read_se(&mut reader, "slice_alpha_c0_offset_div2")?;
            header.slice_beta_offset_div2 = read_se(&mut reader, "slice_beta_offset_div2")?;
            if !(-6..=6).contains(&header.slice_alpha_c0_offset_div2)
                || !(-6..=6).contains(&header.slice_beta_offset_div2)
            {
                return Err(invalid("slice deblocking offset exceeds [-6,6]"));
            }
        }
    }
    Ok(header)
}

fn read_ref_pic_list_reordering(
    reader: &mut BitReader<'_>,
    list_name: &str,
) -> io::Result<Vec<RefPicListModification>> {
    let reordering = reader
        .read_bit()
        .map_err(|error| context(&format!("ref_pic_list_reordering_flag_{list_name}"), error))?;
    if !reordering {
        return Ok(Vec::new());
    }
    let mut modifications = Vec::new();
    for index in 0..64 {
        let idc = read_ue(reader, &format!("modification_of_pic_nums_idc_{list_name}"))?;
        if idc == 3 {
            return Ok(modifications);
        }
        if idc > 2 {
            return Err(invalid(format!(
                "slice modification_of_pic_nums_idc {idc} is invalid"
            )));
        }
        let value = read_ue(
            reader,
            &format!("ref_pic_list_modification_{list_name}[{index}]"),
        )?;
        modifications.push(RefPicListModification {
            modification_of_pic_nums_idc: idc,
            value,
        });
    }
    Err(invalid("slice ref-list reordering exceeds 64 operations"))
}

fn read_memory_management_operations(
    reader: &mut BitReader<'_>,
) -> io::Result<Vec<MemoryManagementOperation>> {
    let mut operations = Vec::new();
    for _ in 0..32 {
        let operation = read_ue(reader, "memory_management_control_operation")?;
        if operation == 0 {
            return Ok(operations);
        }
        let operand_count = match operation {
            1 | 2 | 4 | 6 => 1,
            3 => 2,
            5 => 0,
            _ => {
                return Err(invalid(format!(
                    "memory_management_control_operation {operation} is invalid"
                )))
            }
        };
        let mut operands = Vec::with_capacity(operand_count);
        for _ in 0..operand_count {
            operands.push(read_ue(reader, "memory_management_operation_operand")?);
        }
        operations.push(MemoryManagementOperation {
            operation,
            operands,
        });
    }
    Err(invalid("decoded-reference marking exceeds 32 operations"))
}
fn read_ue(reader: &mut BitReader<'_>, field: &str) -> io::Result<u32> {
    reader.read_ue().map_err(|error| context(field, error))
}

fn read_se(reader: &mut BitReader<'_>, field: &str) -> io::Result<i64> {
    reader.read_se().map_err(|error| context(field, error))
}

fn context(field: &str, error: io::Error) -> io::Error {
    io::Error::new(error.kind(), format!("slice {field}: {error}"))
}

fn invalid(message: impl Into<String>) -> io::Error {
    io::Error::new(ErrorKind::InvalidData, message.into())
}
