use pygorvid::{parse_pps, VideoSampleReader};
use std::io::ErrorKind;
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

fn make_pps(pps_id: u32, sps_id: u32, entropy: bool, bottom_field_poc: bool) -> Vec<u8> {
    make_pps_with_tools(
        pps_id,
        sps_id,
        entropy,
        bottom_field_poc,
        0,
        false,
        0,
        0,
        0,
        0,
        true,
        false,
        false,
        None,
    )
}

#[allow(clippy::too_many_arguments)]
fn make_pps_with_tools(
    pps_id: u32,
    sps_id: u32,
    entropy: bool,
    bottom_field_poc: bool,
    slice_groups: u32,
    weighted_pred: bool,
    weighted_bipred_idc: u8,
    init_qp: i64,
    init_qs: i64,
    chroma_qp: i64,
    deblocking: bool,
    constrained_intra: bool,
    redundant_pic_cnt: bool,
    extension: Option<(bool, bool, i64)>,
) -> Vec<u8> {
    let mut bits = ue_bits(pps_id) + &ue_bits(sps_id);
    bits.push(if entropy { '1' } else { '0' });
    bits.push(if bottom_field_poc { '1' } else { '0' });
    bits.push_str(&ue_bits(slice_groups));
    bits.push_str(&ue_bits(0));
    bits.push_str(&ue_bits(0));
    bits.push(if weighted_pred { '1' } else { '0' });
    bits.push_str(&format!("{weighted_bipred_idc:02b}"));
    bits.push_str(&se_bits(init_qp));
    bits.push_str(&se_bits(init_qs));
    bits.push_str(&se_bits(chroma_qp));
    bits.push(if deblocking { '1' } else { '0' });
    bits.push(if constrained_intra { '1' } else { '0' });
    bits.push(if redundant_pic_cnt { '1' } else { '0' });
    if let Some((transform_8x8, scaling_matrix, second_chroma_qp)) = extension {
        bits.push(if transform_8x8 { '1' } else { '0' });
        bits.push(if scaling_matrix { '1' } else { '0' });
        if scaling_matrix {
            bits.push('1');
        }
        bits.push_str(&se_bits(second_chroma_qp));
    }
    bits.push('1');
    let mut nal = vec![0x68];
    nal.extend(pack_bits(&bits));
    nal
}

#[test]
fn parses_pps_identifiers_and_core_flags() {
    for (nal, expected) in [
        (make_pps(0, 0, true, false), (0, 0, true, false)),
        (make_pps(255, 31, false, true), (255, 31, false, true)),
    ] {
        let info = parse_pps(&nal).unwrap();
        assert_eq!(
            (
                info.picture_parameter_set_id,
                info.sequence_parameter_set_id,
                info.entropy_coding_mode,
                info.bottom_field_pic_order_in_frame_present,
            ),
            expected
        );
    }
}

#[test]
fn parses_pps_weighting_deblocking_and_extension() {
    let info = parse_pps(&make_pps_with_tools(
        0,
        0,
        true,
        false,
        0,
        true,
        1,
        0,
        0,
        0,
        true,
        true,
        true,
        Some((true, false, 0)),
    ))
    .unwrap();
    assert_eq!(info.num_slice_groups_minus1, 0);
    assert_eq!(info.num_ref_idx_l0_default_active_minus1, 0);
    assert_eq!(info.num_ref_idx_l1_default_active_minus1, 0);
    assert!(info.weighted_pred);
    assert_eq!(info.weighted_bipred_idc, 1);
    assert_eq!((info.pic_init_qp_minus26, info.pic_init_qs_minus26), (0, 0));
    assert_eq!(info.chroma_qp_index_offset, 0);
    assert!(info.deblocking_filter_control_present);
    assert!(info.constrained_intra_pred);
    assert!(info.redundant_pic_cnt_present);
    assert!(info.has_extension && info.transform_8x8_mode);
    assert!(!info.pic_scaling_matrix_present);
    assert_eq!(info.second_chroma_qp_index_offset, 0);
}

#[test]
fn rejects_invalid_pps_prefixes() {
    for nal in [
        vec![0x67, 0x80],
        vec![0xe8, 0x80],
        make_pps(256, 0, false, false),
        make_pps(0, 32, false, false),
        vec![0x68, 0x80],
    ] {
        let error = parse_pps(&nal).unwrap_err();
        assert!(matches!(
            error.kind(),
            ErrorKind::InvalidInput | ErrorKind::InvalidData | ErrorKind::UnexpectedEof
        ));
    }
}

#[test]
fn rejects_unsupported_or_invalid_pps_tools() {
    for nal in [
        make_pps_with_tools(
            0, 0, true, false, 1, false, 0, 0, 0, 0, true, false, false, None,
        ),
        make_pps_with_tools(
            0, 0, true, false, 0, false, 3, 0, 0, 0, true, false, false, None,
        ),
        make_pps_with_tools(
            0,
            0,
            true,
            false,
            0,
            false,
            0,
            0,
            0,
            0,
            true,
            false,
            false,
            Some((false, true, 0)),
        ),
        make_pps_with_tools(
            0, 0, true, false, 0, false, 0, 26, 0, 0, true, false, false, None,
        ),
        make_pps_with_tools(
            0, 0, true, false, 0, false, 0, 0, 0, 13, true, false, false, None,
        ),
        make_pps_with_tools(
            0,
            0,
            true,
            false,
            0,
            false,
            0,
            0,
            0,
            0,
            true,
            false,
            false,
            Some((true, false, 13)),
        ),
    ] {
        assert!(parse_pps(&nal).is_err());
    }
}

#[test]
fn parses_pps_from_compact_fixtures() {
    let fixture_dir = PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("../testdata/h264");
    for (name, has_extension) in [("high42-1080p.mp4", false), ("high52-2160p.mp4", true)] {
        let reader = VideoSampleReader::open(fixture_dir.join(name)).unwrap();
        for picture_set in &reader.configuration().picture_parameter_sets {
            let info = parse_pps(picture_set).unwrap();
            assert_eq!(info.picture_parameter_set_id, 0, "{name}");
            assert_eq!(info.sequence_parameter_set_id, 0, "{name}");
            assert!(info.entropy_coding_mode, "{name}");
            assert!(!info.bottom_field_pic_order_in_frame_present, "{name}");
            assert_eq!(info.num_slice_groups_minus1, 0, "{name}");
            assert_eq!(info.num_ref_idx_l0_default_active_minus1, 0, "{name}");
            assert_eq!(info.num_ref_idx_l1_default_active_minus1, 0, "{name}");
            assert!(!info.weighted_pred, "{name}");
            assert_eq!(info.weighted_bipred_idc, 0, "{name}");
            assert_eq!(info.pic_init_qp_minus26, 0, "{name}");
            assert_eq!(info.pic_init_qs_minus26, 0, "{name}");
            assert_eq!(info.chroma_qp_index_offset, 0, "{name}");
            assert!(info.deblocking_filter_control_present, "{name}");
            assert!(!info.constrained_intra_pred, "{name}");
            assert!(!info.redundant_pic_cnt_present, "{name}");
            assert_eq!(info.has_extension, has_extension, "{name}");
            assert_eq!(info.transform_8x8_mode, has_extension, "{name}");
            assert!(!info.pic_scaling_matrix_present, "{name}");
            assert_eq!(info.second_chroma_qp_index_offset, 0, "{name}");
        }
    }
}
