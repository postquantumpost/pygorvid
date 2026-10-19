use std::io::{self, ErrorKind};

use crate::{ebsp_to_rbsp, parse_nal_header, BitReader};

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct PpsInfo {
    pub picture_parameter_set_id: u32,
    pub sequence_parameter_set_id: u32,
    pub entropy_coding_mode: bool,
    pub bottom_field_pic_order_in_frame_present: bool,
    pub num_slice_groups_minus1: u32,
    pub num_ref_idx_l0_default_active_minus1: u32,
    pub num_ref_idx_l1_default_active_minus1: u32,
    pub weighted_pred: bool,
    pub weighted_bipred_idc: u8,
    pub pic_init_qp_minus26: i64,
    pub pic_init_qs_minus26: i64,
    pub chroma_qp_index_offset: i64,
    pub deblocking_filter_control_present: bool,
    pub constrained_intra_pred: bool,
    pub redundant_pic_cnt_present: bool,
    pub has_extension: bool,
    pub transform_8x8_mode: bool,
    pub pic_scaling_matrix_present: bool,
    pub second_chroma_qp_index_offset: i64,
}

pub fn parse_pps(nal: &[u8]) -> io::Result<PpsInfo> {
    let header = parse_nal_header(nal)?;
    if header.unit_type != 8 {
        return Err(invalid(format!(
            "NAL unit type {} is not a PPS",
            header.unit_type
        )));
    }
    let rbsp = ebsp_to_rbsp(&nal[1..])?;
    let mut reader = BitReader::new(&rbsp);
    let picture_parameter_set_id = read_ue(&mut reader, "pic_parameter_set_id")?;
    if picture_parameter_set_id > 255 {
        return Err(invalid(format!(
            "PPS pic_parameter_set_id {picture_parameter_set_id} exceeds 255"
        )));
    }
    let sequence_parameter_set_id = read_ue(&mut reader, "seq_parameter_set_id")?;
    if sequence_parameter_set_id > 31 {
        return Err(invalid(format!(
            "PPS seq_parameter_set_id {sequence_parameter_set_id} exceeds 31"
        )));
    }
    let entropy_coding_mode = reader
        .read_bit()
        .map_err(|error| context("entropy_coding_mode_flag", error))?;
    let bottom_field_pic_order_in_frame_present = reader
        .read_bit()
        .map_err(|error| context("bottom_field_pic_order_in_frame_present_flag", error))?;
    let num_slice_groups_minus1 = read_ue(&mut reader, "num_slice_groups_minus1")?;
    if num_slice_groups_minus1 != 0 {
        return Err(invalid(format!(
            "unsupported PPS slice groups: num_slice_groups_minus1={num_slice_groups_minus1}"
        )));
    }
    let num_ref_idx_l0_default_active_minus1 =
        read_ue(&mut reader, "num_ref_idx_l0_default_active_minus1")?;
    let num_ref_idx_l1_default_active_minus1 =
        read_ue(&mut reader, "num_ref_idx_l1_default_active_minus1")?;
    if num_ref_idx_l0_default_active_minus1 > 31 || num_ref_idx_l1_default_active_minus1 > 31 {
        return Err(invalid("PPS default reference index exceeds 31"));
    }
    let weighted_pred = reader
        .read_bit()
        .map_err(|error| context("weighted_pred_flag", error))?;
    let weighted_bipred_idc = reader
        .read_bits(2)
        .map_err(|error| context("weighted_bipred_idc", error))?
        as u8;
    if weighted_bipred_idc > 2 {
        return Err(invalid(format!(
            "PPS weighted_bipred_idc {weighted_bipred_idc} is reserved"
        )));
    }
    let pic_init_qp_minus26 = read_se(&mut reader, "pic_init_qp_minus26")?;
    let pic_init_qs_minus26 = read_se(&mut reader, "pic_init_qs_minus26")?;
    let chroma_qp_index_offset = read_se(&mut reader, "chroma_qp_index_offset")?;
    if !(-26..=25).contains(&pic_init_qp_minus26) || !(-26..=25).contains(&pic_init_qs_minus26) {
        return Err(invalid("PPS initial QP/QS offset is outside [-26,25]"));
    }
    if !(-12..=12).contains(&chroma_qp_index_offset) {
        return Err(invalid("PPS chroma QP index offset is outside [-12,12]"));
    }
    let deblocking_filter_control_present = reader
        .read_bit()
        .map_err(|error| context("deblocking_filter_control_present_flag", error))?;
    let constrained_intra_pred = reader
        .read_bit()
        .map_err(|error| context("constrained_intra_pred_flag", error))?;
    let redundant_pic_cnt_present = reader
        .read_bit()
        .map_err(|error| context("redundant_pic_cnt_present_flag", error))?;
    let has_extension = reader.more_rbsp_data();
    let mut transform_8x8_mode = false;
    let mut pic_scaling_matrix_present = false;
    let mut second_chroma_qp_index_offset = chroma_qp_index_offset;
    if has_extension {
        transform_8x8_mode = reader
            .read_bit()
            .map_err(|error| context("transform_8x8_mode_flag", error))?;
        pic_scaling_matrix_present = reader
            .read_bit()
            .map_err(|error| context("pic_scaling_matrix_present_flag", error))?;
        if pic_scaling_matrix_present {
            return Err(invalid("unsupported PPS pic_scaling_matrix_present_flag"));
        }
        second_chroma_qp_index_offset = read_se(&mut reader, "second_chroma_qp_index_offset")?;
        if !(-12..=12).contains(&second_chroma_qp_index_offset) {
            return Err(invalid(
                "PPS second chroma QP index offset is outside [-12,12]",
            ));
        }
    }
    Ok(PpsInfo {
        picture_parameter_set_id,
        sequence_parameter_set_id,
        entropy_coding_mode,
        bottom_field_pic_order_in_frame_present,
        num_slice_groups_minus1,
        num_ref_idx_l0_default_active_minus1,
        num_ref_idx_l1_default_active_minus1,
        weighted_pred,
        weighted_bipred_idc,
        pic_init_qp_minus26,
        pic_init_qs_minus26,
        chroma_qp_index_offset,
        deblocking_filter_control_present,
        constrained_intra_pred,
        redundant_pic_cnt_present,
        has_extension,
        transform_8x8_mode,
        pic_scaling_matrix_present,
        second_chroma_qp_index_offset,
    })
}

fn read_ue(reader: &mut BitReader<'_>, field: &str) -> io::Result<u32> {
    reader.read_ue().map_err(|error| context(field, error))
}

fn read_se(reader: &mut BitReader<'_>, field: &str) -> io::Result<i64> {
    reader.read_se().map_err(|error| context(field, error))
}

fn context(field: &str, error: io::Error) -> io::Error {
    io::Error::new(error.kind(), format!("PPS {field}: {error}"))
}

fn invalid(message: impl Into<String>) -> io::Error {
    io::Error::new(ErrorKind::InvalidData, message.into())
}
