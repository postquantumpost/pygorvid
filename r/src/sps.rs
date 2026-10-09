use std::io::{self, ErrorKind};

use crate::{ebsp_to_rbsp, parse_nal_header, BitReader};

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct SpsInfo {
    pub profile_idc: u8,
    pub constraint_flags: u8,
    pub level_idc: u8,
    pub sps_id: u32,
    pub chroma_format_idc: u8,
    pub separate_colour_plane: bool,
    pub bit_depth_luma: u8,
    pub bit_depth_chroma: u8,
    pub coded_width: u32,
    pub coded_height: u32,
    pub width: u32,
    pub height: u32,
    pub frame_crop_left: u32,
    pub frame_crop_right: u32,
    pub frame_crop_top: u32,
    pub frame_crop_bottom: u32,
    pub log2_max_frame_num_minus4: u32,
    pub pic_order_cnt_type: u8,
    pub log2_max_pic_order_cnt_lsb_minus4: Option<u32>,
    pub delta_pic_order_always_zero: bool,
    pub offset_for_non_ref_pic: Option<i64>,
    pub offset_for_top_to_bottom_field: Option<i64>,
    pub offset_for_ref_frame: Vec<i64>,
    pub max_num_ref_frames: u32,
    pub gaps_in_frame_num_value_allowed: bool,
    pub frame_mbs_only: bool,
    pub mb_adaptive_frame_field: bool,
    pub direct_8x8_inference: bool,
}

pub fn parse_sps(nal: &[u8]) -> io::Result<SpsInfo> {
    let header = parse_nal_header(nal)?;
    if header.unit_type != 7 {
        return Err(invalid(format!(
            "NAL unit type {} is not an SPS",
            header.unit_type
        )));
    }
    let rbsp = ebsp_to_rbsp(&nal[1..])?;
    let mut reader = BitReader::new(&rbsp);
    let profile_idc = read_byte(&mut reader, "profile_idc")?;
    let constraint_flags = read_byte(&mut reader, "constraint flags")?;
    if constraint_flags & 0x03 != 0 {
        return Err(invalid("SPS reserved constraint bits are nonzero"));
    }
    let level_idc = read_byte(&mut reader, "level_idc")?;
    let sps_id = read_ue(&mut reader, "seq_parameter_set_id")?;
    if sps_id > 31 {
        return Err(invalid(format!(
            "SPS seq_parameter_set_id {sps_id} exceeds 31"
        )));
    }

    let mut info = SpsInfo {
        profile_idc,
        constraint_flags,
        level_idc,
        sps_id,
        chroma_format_idc: 1,
        separate_colour_plane: false,
        bit_depth_luma: 8,
        bit_depth_chroma: 8,
        coded_width: 0,
        coded_height: 0,
        width: 0,
        height: 0,
        frame_crop_left: 0,
        frame_crop_right: 0,
        frame_crop_top: 0,
        frame_crop_bottom: 0,
        log2_max_frame_num_minus4: 0,
        pic_order_cnt_type: 0,
        log2_max_pic_order_cnt_lsb_minus4: None,
        delta_pic_order_always_zero: false,
        offset_for_non_ref_pic: None,
        offset_for_top_to_bottom_field: None,
        offset_for_ref_frame: Vec::new(),
        max_num_ref_frames: 0,
        gaps_in_frame_num_value_allowed: false,
        frame_mbs_only: true,
        mb_adaptive_frame_field: false,
        direct_8x8_inference: true,
    };
    if profile_has_chroma_depth_syntax(profile_idc) {
        let chroma_format_idc = read_ue(&mut reader, "chroma_format_idc")?;
        if chroma_format_idc > 3 {
            return Err(invalid(format!(
                "SPS chroma_format_idc {chroma_format_idc} exceeds 3"
            )));
        }
        info.chroma_format_idc = chroma_format_idc as u8;
        if chroma_format_idc == 3 {
            info.separate_colour_plane = reader
                .read_bit()
                .map_err(|error| context("separate_colour_plane_flag", error))?;
        }
        let luma_depth = read_ue(&mut reader, "bit_depth_luma_minus8")?;
        let chroma_depth = read_ue(&mut reader, "bit_depth_chroma_minus8")?;
        if luma_depth > 6 || chroma_depth > 6 {
            return Err(invalid("SPS bit depth minus 8 exceeds 6"));
        }
        info.bit_depth_luma = 8 + luma_depth as u8;
        info.bit_depth_chroma = 8 + chroma_depth as u8;
        reader
            .read_bit()
            .map_err(|error| context("qpprime_y_zero_transform_bypass_flag", error))?;
        skip_scaling_lists(&mut reader, chroma_format_idc)?;
    }

    let log2_max_frame_num_minus4 = read_ue(&mut reader, "log2_max_frame_num_minus4")?;
    if log2_max_frame_num_minus4 > 12 {
        return Err(invalid("SPS log2_max_frame_num_minus4 exceeds 12"));
    }
    info.log2_max_frame_num_minus4 = log2_max_frame_num_minus4;
    let poc_type = read_ue(&mut reader, "pic_order_cnt_type")?;
    if poc_type > 2 {
        return Err(invalid(format!(
            "SPS pic_order_cnt_type {poc_type} exceeds 2"
        )));
    }
    info.pic_order_cnt_type = poc_type as u8;
    match poc_type {
        0 => {
            let log2_max_poc_lsb_minus4 =
                read_ue(&mut reader, "log2_max_pic_order_cnt_lsb_minus4")?;
            if log2_max_poc_lsb_minus4 > 12 {
                return Err(invalid("SPS log2_max_pic_order_cnt_lsb_minus4 exceeds 12"));
            }
            info.log2_max_pic_order_cnt_lsb_minus4 = Some(log2_max_poc_lsb_minus4);
        }
        1 => {
            info.delta_pic_order_always_zero = reader
                .read_bit()
                .map_err(|error| context("delta_pic_order_always_zero_flag", error))?;
            info.offset_for_non_ref_pic = Some(read_se(&mut reader, "offset_for_non_ref_pic")?);
            info.offset_for_top_to_bottom_field =
                Some(read_se(&mut reader, "offset_for_top_to_bottom_field")?);
            let cycle_count = read_ue(&mut reader, "num_ref_frames_in_pic_order_cnt_cycle")?;
            if cycle_count > 255 {
                return Err(invalid("SPS POC cycle count exceeds 255"));
            }
            info.offset_for_ref_frame.reserve(cycle_count as usize);
            for index in 0..cycle_count {
                info.offset_for_ref_frame.push(read_se(
                    &mut reader,
                    &format!("offset_for_ref_frame[{index}]"),
                )?);
            }
        }
        2 => {}
        _ => unreachable!(),
    }
    info.max_num_ref_frames = read_ue(&mut reader, "max_num_ref_frames")?;
    info.gaps_in_frame_num_value_allowed = reader
        .read_bit()
        .map_err(|error| context("gaps_in_frame_num_value_allowed_flag", error))?;
    let width_mbs_minus_1 = read_ue(&mut reader, "pic_width_in_mbs_minus1")?;
    let height_map_units_minus_1 = read_ue(&mut reader, "pic_height_in_map_units_minus1")?;
    info.frame_mbs_only = reader
        .read_bit()
        .map_err(|error| context("frame_mbs_only_flag", error))?;
    if !info.frame_mbs_only {
        info.mb_adaptive_frame_field = reader
            .read_bit()
            .map_err(|error| context("mb_adaptive_frame_field_flag", error))?;
    }
    info.direct_8x8_inference = reader
        .read_bit()
        .map_err(|error| context("direct_8x8_inference_flag", error))?;
    let cropping = reader
        .read_bit()
        .map_err(|error| context("frame_cropping_flag", error))?;
    let crop = if cropping {
        [
            read_ue(&mut reader, "frame_crop_left_offset")?,
            read_ue(&mut reader, "frame_crop_right_offset")?,
            read_ue(&mut reader, "frame_crop_top_offset")?,
            read_ue(&mut reader, "frame_crop_bottom_offset")?,
        ]
    } else {
        [0; 4]
    };
    reader
        .read_bit()
        .map_err(|error| context("vui_parameters_present_flag", error))?;
    let frame_mbs_only = info.frame_mbs_only;
    set_dimensions(
        &mut info,
        width_mbs_minus_1,
        height_map_units_minus_1,
        frame_mbs_only,
        crop,
    )?;
    Ok(info)
}

fn read_byte(reader: &mut BitReader<'_>, field: &str) -> io::Result<u8> {
    reader
        .read_bits(8)
        .map(|value| value as u8)
        .map_err(|error| context(field, error))
}

fn read_ue(reader: &mut BitReader<'_>, field: &str) -> io::Result<u32> {
    reader.read_ue().map_err(|error| context(field, error))
}

fn read_se(reader: &mut BitReader<'_>, field: &str) -> io::Result<i64> {
    reader.read_se().map_err(|error| context(field, error))
}

fn skip_scaling_lists(reader: &mut BitReader<'_>, chroma_format_idc: u32) -> io::Result<()> {
    let matrix_present = reader
        .read_bit()
        .map_err(|error| context("seq_scaling_matrix_present_flag", error))?;
    if !matrix_present {
        return Ok(());
    }
    let list_count = if chroma_format_idc == 3 { 12 } else { 8 };
    for list_index in 0..list_count {
        let list_present = reader
            .read_bit()
            .map_err(|error| context("seq_scaling_list_present_flag", error))?;
        if !list_present {
            continue;
        }
        let mut last_scale = 8i64;
        let mut next_scale = 8i64;
        let list_size = if list_index < 6 { 16 } else { 64 };
        for _ in 0..list_size {
            if next_scale != 0 {
                let delta_scale = read_se(reader, "scaling_list.delta_scale")?;
                next_scale = (last_scale + delta_scale + 256).rem_euclid(256);
            }
            if next_scale != 0 {
                last_scale = next_scale;
            }
        }
    }
    Ok(())
}

fn set_dimensions(
    info: &mut SpsInfo,
    width_mbs_minus_1: u32,
    height_map_units_minus_1: u32,
    frame_mbs_only: bool,
    crop: [u32; 4],
) -> io::Result<()> {
    let frame_factor = if frame_mbs_only { 1u64 } else { 2 };
    let coded_width = (u64::from(width_mbs_minus_1) + 1) * 16;
    let coded_height = (u64::from(height_map_units_minus_1) + 1) * frame_factor * 16;
    let chroma_array_type = if info.separate_colour_plane {
        0
    } else {
        info.chroma_format_idc
    };
    let (sub_width, sub_height) = match chroma_array_type {
        1 => (2u64, 2u64),
        2 => (2, 1),
        _ => (1, 1),
    };
    let (crop_unit_x, crop_unit_y) = if chroma_array_type == 0 {
        (1, frame_factor)
    } else {
        (sub_width, sub_height * frame_factor)
    };
    let crop_pixels = [
        u64::from(crop[0]) * crop_unit_x,
        u64::from(crop[1]) * crop_unit_x,
        u64::from(crop[2]) * crop_unit_y,
        u64::from(crop[3]) * crop_unit_y,
    ];
    if crop_pixels[0] + crop_pixels[1] >= coded_width
        || crop_pixels[2] + crop_pixels[3] >= coded_height
    {
        return Err(invalid("SPS frame cropping removes the entire picture"));
    }
    if coded_width > u64::from(u32::MAX) || coded_height > u64::from(u32::MAX) {
        return Err(invalid("SPS coded dimensions exceed uint32"));
    }
    info.coded_width = coded_width as u32;
    info.coded_height = coded_height as u32;
    info.width = (coded_width - crop_pixels[0] - crop_pixels[1]) as u32;
    info.height = (coded_height - crop_pixels[2] - crop_pixels[3]) as u32;
    info.frame_crop_left = crop_pixels[0] as u32;
    info.frame_crop_right = crop_pixels[1] as u32;
    info.frame_crop_top = crop_pixels[2] as u32;
    info.frame_crop_bottom = crop_pixels[3] as u32;
    Ok(())
}

fn context(field: &str, error: io::Error) -> io::Error {
    io::Error::new(error.kind(), format!("SPS {field}: {error}"))
}

fn invalid(message: impl Into<String>) -> io::Error {
    io::Error::new(ErrorKind::InvalidData, message.into())
}

fn profile_has_chroma_depth_syntax(profile_idc: u8) -> bool {
    matches!(
        profile_idc,
        44 | 83 | 86 | 100 | 110 | 118 | 122 | 128 | 134 | 135 | 138 | 139 | 144 | 244
    )
}
