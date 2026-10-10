use pygorvid::{parse_sps, SpsInfo, VideoSampleReader};
use std::path::PathBuf;

fn pack_bits(bits: &str) -> Vec<u8> {
    let mut data = vec![0u8; (bits.len() + 7) / 8];
    for (index, bit) in bits.bytes().enumerate() {
        if bit == b'1' {
            data[index / 8] |= 1 << (7 - index % 8);
        }
    }
    data
}

fn ue_bits(value: u32) -> String {
    let code = format!("{:b}", u64::from(value) + 1);
    format!("{}{}", "0".repeat(code.len() - 1), code)
}

fn se_bits(value: i64) -> String {
    ue_bits(if value > 0 {
        (2 * value - 1) as u32
    } else {
        (-2 * value) as u32
    })
}

fn make_raw_sps(profile: u8, constraints: u8, level: u8, syntax_bits: &str) -> Vec<u8> {
    let mut nal = vec![0x67, profile, constraints, level];
    nal.extend(pack_bits(syntax_bits));
    nal
}

fn make_sps(profile: u8, constraints: u8, level: u8, syntax_bits: &str) -> Vec<u8> {
    make_sps_geometry(profile, constraints, level, syntax_bits, 0, 0, true, [0; 4])
}

fn make_sps_geometry(
    profile: u8,
    constraints: u8,
    level: u8,
    syntax_bits: &str,
    width_mbs_minus_1: u32,
    height_map_units_minus_1: u32,
    frame_mbs_only: bool,
    crop: [u32; 4],
) -> Vec<u8> {
    make_sps_poc_geometry(
        profile,
        constraints,
        level,
        syntax_bits,
        width_mbs_minus_1,
        height_map_units_minus_1,
        frame_mbs_only,
        crop,
        0,
        0,
        &ue_bits(0),
    )
}

fn make_sps_poc_geometry(
    profile: u8,
    constraints: u8,
    level: u8,
    syntax_bits: &str,
    width_mbs_minus_1: u32,
    height_map_units_minus_1: u32,
    frame_mbs_only: bool,
    crop: [u32; 4],
    frame_num_minus4: u32,
    poc_type: u32,
    poc_syntax: &str,
) -> Vec<u8> {
    let mut syntax = syntax_bits.to_string();
    if matches!(
        profile,
        44 | 83 | 86 | 100 | 110 | 118 | 122 | 128 | 134 | 135 | 138 | 139 | 144 | 244
    ) {
        syntax.push_str("00");
    }
    syntax.push_str(&ue_bits(frame_num_minus4));
    syntax.push_str(&ue_bits(poc_type));
    syntax.push_str(poc_syntax);
    syntax.push_str(&ue_bits(0));
    syntax.push('0');
    syntax.push_str(&ue_bits(width_mbs_minus_1));
    syntax.push_str(&ue_bits(height_map_units_minus_1));
    syntax.push_str(if frame_mbs_only { "1" } else { "00" });
    syntax.push('1');
    let cropping = crop.iter().any(|offset| *offset != 0);
    syntax.push(if cropping { '1' } else { '0' });
    if cropping {
        for offset in crop {
            syntax.push_str(&ue_bits(offset));
        }
    }
    syntax.push_str("01");
    make_raw_sps(profile, constraints, level, &syntax)
}

fn make_sps_with_frame_flags(
    max_num_ref_frames: u32,
    gaps_allowed: bool,
    frame_mbs_only: bool,
    mb_adaptive_frame_field: bool,
    direct_8x8_inference: bool,
) -> Vec<u8> {
    let mut syntax = ue_bits(0); // SPS id
    syntax.push_str(&ue_bits(0)); // log2_max_frame_num_minus4
    syntax.push_str(&ue_bits(0)); // pic_order_cnt_type
    syntax.push_str(&ue_bits(0)); // log2_max_pic_order_cnt_lsb_minus4
    syntax.push_str(&ue_bits(max_num_ref_frames));
    syntax.push(if gaps_allowed { '1' } else { '0' });
    syntax.push_str(&ue_bits(0)); // width
    syntax.push_str(&ue_bits(0)); // height
    syntax.push(if frame_mbs_only { '1' } else { '0' });
    if !frame_mbs_only {
        syntax.push(if mb_adaptive_frame_field { '1' } else { '0' });
    }
    syntax.push(if direct_8x8_inference { '1' } else { '0' });
    syntax.push_str("001"); // no crop, no VUI, RBSP stop bit
    make_raw_sps(66, 0, 30, &syntax)
}

fn expected(
    profile_idc: u8,
    constraint_flags: u8,
    level_idc: u8,
    sps_id: u32,
    chroma_format_idc: u8,
    separate_colour_plane: bool,
    bit_depth_luma: u8,
    bit_depth_chroma: u8,
) -> SpsInfo {
    SpsInfo {
        profile_idc,
        constraint_flags,
        level_idc,
        sps_id,
        chroma_format_idc,
        separate_colour_plane,
        bit_depth_luma,
        bit_depth_chroma,
        log2_max_frame_num_minus4: 0,
        pic_order_cnt_type: 0,
        log2_max_pic_order_cnt_lsb_minus4: Some(0),
        delta_pic_order_always_zero: false,
        offset_for_non_ref_pic: None,
        offset_for_top_to_bottom_field: None,
        offset_for_ref_frame: Vec::new(),
        max_num_ref_frames: 0,
        gaps_in_frame_num_value_allowed: false,
        frame_mbs_only: true,
        mb_adaptive_frame_field: false,
        direct_8x8_inference: true,
        coded_width: 16,
        coded_height: 16,
        width: 16,
        height: 16,
        frame_crop_left: 0,
        frame_crop_right: 0,
        frame_crop_top: 0,
        frame_crop_bottom: 0,
    }
}

#[test]
fn rejects_unsupported_sps_feature_set() {
    for nal in [
        make_sps(118, 0, 42, concat!("1", "010", "1", "1")),
        make_sps(100, 0, 42, concat!("1", "00100", "1", "011", "010")),
        make_sps_geometry(
            100,
            0,
            42,
            concat!("1", "010", "1", "1"),
            0,
            0,
            false,
            [0; 4],
        ),
        make_sps(100, 0, 42, concat!("1", "00100", "1", "1", "1")),
    ] {
        assert!(parse_sps(&nal).is_err(), "accepted unsupported SPS {nal:?}");
    }
}

#[test]
fn parses_sps_dimensions_and_crop_units() {
    let progressive = make_sps_geometry(
        100,
        0,
        42,
        concat!("1", "010", "1", "1"),
        119,
        67,
        true,
        [0, 0, 0, 4],
    );
    let info = parse_sps(&progressive).unwrap();
    assert_eq!((info.coded_width, info.coded_height), (1920, 1088));
    assert_eq!((info.width, info.height), (1920, 1080));
    assert_eq!(info.frame_crop_bottom, 8);

    let progressive_cropped = make_sps_geometry(100, 0, 42, concat!("1", "010", "1", "1"), 0, 1, true, [0, 0, 0, 1]);
    let info = parse_sps(&progressive_cropped).unwrap();
    assert_eq!((info.coded_width, info.coded_height), (16, 32));
    assert_eq!((info.width, info.height), (16, 30));
    assert_eq!(info.frame_crop_bottom, 2);
    assert!(info.frame_mbs_only);
    assert!(!info.mb_adaptive_frame_field);
    assert!(info.direct_8x8_inference);
}

#[test]
fn parses_sps_reference_and_frame_flags() {
    let nal = make_sps_geometry(100, 0, 42, concat!("1", "010", "1", "1"), 0, 0, true, [0; 4]);
    let info = parse_sps(&nal).unwrap();
    assert_eq!(info.max_num_ref_frames, 0);
    assert!(!info.gaps_in_frame_num_value_allowed);
    assert!(info.frame_mbs_only);
    assert!(!info.mb_adaptive_frame_field);
    assert!(info.direct_8x8_inference);
}

#[test]
fn parses_sps_frame_number_and_poc_modes() {
    let profile_fields = concat!("1", "010", "1", "1");
    for (poc_type, poc_syntax, expected_lsb, expected_delta, expected_offsets) in [
        (0, ue_bits(3), Some(3), false, (None, None, vec![])),
        (
            1,
            format!(
                "1{}{}{}{}{}",
                se_bits(-2),
                se_bits(3),
                ue_bits(2),
                se_bits(-1),
                se_bits(2)
            ),
            None,
            true,
            (Some(-2), Some(3), vec![-1, 2]),
        ),
        (2, String::new(), None, false, (None, None, vec![])),
    ] {
        let nal = make_sps_poc_geometry(
            100,
            0,
            42,
            profile_fields,
            0,
            0,
            true,
            [0; 4],
            4,
            poc_type,
            &poc_syntax,
        );
        let info = parse_sps(&nal).unwrap();
        assert_eq!(info.log2_max_frame_num_minus4, 4);
        assert_eq!(info.pic_order_cnt_type, poc_type as u8);
        assert_eq!(info.log2_max_pic_order_cnt_lsb_minus4, expected_lsb);
        assert_eq!(info.delta_pic_order_always_zero, expected_delta);
        assert_eq!(info.offset_for_non_ref_pic, expected_offsets.0);
        assert_eq!(info.offset_for_top_to_bottom_field, expected_offsets.1);
        assert_eq!(info.offset_for_ref_frame, expected_offsets.2);
    }
}

#[test]
fn rejects_invalid_sps_profile_fields() {
    for nal in [
        vec![0x65, 66, 0, 30, 0x80],
        make_sps(66, 1, 30, "1"),
        make_sps(66, 0, 30, "00000100001"),
        make_sps(100, 0, 42, concat!("1", "00101")),
        make_sps(100, 0, 42, concat!("1", "010", "0001000")),
        make_raw_sps(100, 0, 42, "1"),
        make_sps_geometry(
            100,
            0,
            42,
            concat!("1", "010", "1", "1"),
            0,
            0,
            true,
            [4, 4, 0, 0],
        ),
        make_sps_poc_geometry(
            100,
            0,
            42,
            concat!("1", "010", "1", "1"),
            0,
            0,
            true,
            [0; 4],
            0,
            3,
            "",
        ),
        make_sps_poc_geometry(
            100,
            0,
            42,
            concat!("1", "010", "1", "1"),
            0,
            0,
            true,
            [0; 4],
            13,
            0,
            &ue_bits(0),
        ),
    ] {
        assert!(parse_sps(&nal).is_err(), "accepted malformed SPS {nal:?}");
    }
}

#[test]
fn parses_sps_from_compact_fixtures() {
    let fixture_dir = PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("../testdata/h264");
    for (name, expected_refs) in [("high42-1080p.mp4", 4), ("high52-2160p.mp4", 3)] {
        let reader = VideoSampleReader::open(fixture_dir.join(name)).unwrap();
        for sequence_set in &reader.configuration().sequence_parameter_sets {
            let info = parse_sps(sequence_set).unwrap();
            assert_eq!(info.profile_idc, 100, "{name}");
            assert_eq!(info.chroma_format_idc, 1, "{name}");
            assert_eq!(info.bit_depth_luma, 8, "{name}");
            assert_eq!(info.bit_depth_chroma, 8, "{name}");
            assert_eq!(info.max_num_ref_frames, expected_refs, "{name}");
            assert!(!info.gaps_in_frame_num_value_allowed, "{name}");
            assert!(info.frame_mbs_only, "{name}");
            assert!(!info.mb_adaptive_frame_field, "{name}");
            assert!(info.direct_8x8_inference, "{name}");
            let expected_dimensions = if name == "high42-1080p.mp4" {
                (1920, 1080)
            } else {
                (3840, 2160)
            };
            assert_eq!((info.width, info.height), expected_dimensions, "{name}");
        }
    }
}
