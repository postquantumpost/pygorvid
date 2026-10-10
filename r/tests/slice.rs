use pygorvid::{
    ebsp_to_rbsp, group_slices_into_pictures, new_cabac_i_chroma_coded_block_pattern_contexts,
    new_cabac_i_intra4x4_pred_mode_contexts, new_cabac_i_intra_chroma_pred_mode_contexts,
    new_cabac_i_intra_mb_type_contexts, new_cabac_i_luma4x4_coded_block_flag_contexts,
    new_cabac_i_luma_coded_block_pattern_contexts, new_cabac_i_mb_qp_delta_contexts,
    new_cabac_i_transform_size_8x8_contexts, parse_nal_header, same_primary_picture,
    CabacArithmeticDecoder, VideoSampleReader,
};
use pygorvid::{parse_pps, parse_slice_header, parse_sps, PpsInfo, SpsInfo};
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

fn fixed_bits(value: u32, width: usize) -> String {
    format!("{value:0width$b}")
}

fn make_sps(poc_type: u32, delta_always_zero: bool, frame_mbs_only: bool) -> SpsInfo {
    let mut bits = ue_bits(0) + &ue_bits(1) + &ue_bits(0) + &ue_bits(0) + "00";
    bits += &ue_bits(0);
    bits += &ue_bits(poc_type);
    match poc_type {
        0 => bits += &ue_bits(0),
        1 => {
            bits.push(if delta_always_zero { '1' } else { '0' });
            bits += &se_bits(-2);
            bits += &se_bits(3);
            bits += &ue_bits(2);
            bits += &se_bits(-1);
            bits += &se_bits(2);
        }
        2 => {}
        _ => unreachable!(),
    }
    bits += &ue_bits(0);
    bits += "0";
    bits += &ue_bits(0);
    bits += &ue_bits(0);
    bits.push(if frame_mbs_only { '1' } else { '0' });
    if !frame_mbs_only {
        bits.push('0');
    }
    bits += "10";
    bits += "01";
    let mut nal = vec![0x67, 100, 0, 42];
    nal.extend(pack_bits(&bits));
    parse_sps(&nal).unwrap()
}

fn make_pps(bottom_field_poc: bool) -> PpsInfo {
    make_pps_with_ids(0, 0, bottom_field_poc, false)
}

fn make_pps_with_redundant(bottom_field_poc: bool, redundant_pic_cnt: bool) -> PpsInfo {
    make_pps_with_ids(0, 0, bottom_field_poc, redundant_pic_cnt)
}

fn make_pps_with_ids(
    pps_id: u32,
    sps_id: u32,
    bottom_field_poc: bool,
    redundant_pic_cnt: bool,
) -> PpsInfo {
    let mut bits = ue_bits(pps_id) + &ue_bits(sps_id) + "1";
    bits.push(if bottom_field_poc { '1' } else { '0' });
    bits += &ue_bits(0);
    bits += &ue_bits(0);
    bits += &ue_bits(0);
    bits += "0";
    bits += "00";
    bits += &se_bits(0);
    bits += &se_bits(0);
    bits += &se_bits(0);
    bits += "10";
    bits.push(if redundant_pic_cnt { '1' } else { '0' });
    bits.push('1');
    let mut nal = vec![0x68];
    nal.extend(pack_bits(&bits));
    parse_pps(&nal).unwrap()
}

fn make_slice(header: u8, first_mb: u32, slice_type: u32, pps_id: u32, suffix: &str) -> Vec<u8> {
    let normalized_type = slice_type % 5;
    let type_prefix = match normalized_type {
        0 => "0",
        1 => "00",
        _ => "",
    };
    make_slice_with_prefix(
        header,
        first_mb,
        slice_type,
        pps_id,
        suffix,
        type_prefix,
        None,
    )
}

fn make_slice_with_prefix(
    header: u8,
    first_mb: u32,
    slice_type: u32,
    pps_id: u32,
    suffix: &str,
    type_prefix: &str,
    redundant_pic_cnt: Option<u32>,
) -> Vec<u8> {
    let normalized_type = slice_type % 5;
    let mut bits = ue_bits(first_mb) + &ue_bits(slice_type) + &ue_bits(pps_id) + suffix;
    if let Some(redundant_pic_cnt) = redundant_pic_cnt {
        bits += &ue_bits(redundant_pic_cnt);
    }
    bits += type_prefix;
    if normalized_type == 0 || normalized_type == 1 {
        bits += "0";
        if normalized_type == 1 {
            bits += "0";
        }
    }
    if header & 0x1f == 5 {
        bits += "00";
    } else if header & 0x60 != 0 {
        bits += "0";
    }
    if normalized_type != 2 {
        bits += &ue_bits(0); // cabac_init_idc
    }
    bits += &se_bits(0); // slice_qp_delta
    bits += &ue_bits(0); // disable_deblocking_filter_idc
    bits += &se_bits(0);
    bits += &se_bits(0);
    while bits.len() % 8 != 0 {
        bits.push('1');
    }
    let mut nal = vec![header];
    nal.extend(pack_bits(&bits));
    nal
}

#[test]
fn parses_idr_slice_picture_identity() {
    let sps = make_sps(0, false, true);
    let pps = make_pps(false);
    let nal = make_slice(
        0x65,
        0,
        2,
        0,
        &(fixed_bits(5, 4) + &ue_bits(7) + &fixed_bits(3, 4)),
    );
    let header = parse_slice_header(&nal, &sps, &pps).unwrap();
    assert_eq!(
        (
            header.first_macroblock_in_slice,
            header.slice_type,
            header.frame_num
        ),
        (0, 2, 5)
    );
    assert!(header.idr);
    assert_eq!(header.idr_pic_id, 7);
    assert_eq!(header.pic_order_cnt_lsb, 3);
    let identity = header.picture_identity();
    assert!(identity.idr);
    assert_eq!(identity.idr_pic_id, 7);
    assert_eq!(identity.frame_num, 5);
    assert!(!identity.nal_ref_idc_zero);
}

#[test]
fn parses_poc_modes_and_field_picture_identity() {
    let pps_bottom = make_pps(true);
    let poc_zero = make_slice(
        0x41,
        1,
        0,
        0,
        &(fixed_bits(2, 4) + &fixed_bits(6, 4) + &se_bits(-2)),
    );
    let header = parse_slice_header(&poc_zero, &make_sps(0, false, true), &pps_bottom).unwrap();
    assert!(header.has_delta_pic_order_bottom);
    assert_eq!(header.delta_pic_order_bottom, -2);

    let poc_one = make_slice(
        0x01,
        2,
        1,
        0,
        &(fixed_bits(3, 4) + &se_bits(2) + &se_bits(-1)),
    );
    let header = parse_slice_header(&poc_one, &make_sps(1, false, true), &pps_bottom).unwrap();
    assert_eq!(header.delta_pic_order_cnt0, 2);
    assert_eq!(header.delta_pic_order_cnt1, -1);
    assert!(header.has_delta_pic_order_cnt0 && header.has_delta_pic_order_cnt1);

    let poc_one_zero = make_slice(0x41, 0, 1, 0, &fixed_bits(1, 4));
    let header = parse_slice_header(&poc_one_zero, &make_sps(1, true, true), &pps_bottom).unwrap();
    assert!(!header.has_delta_pic_order_cnt0);

    let poc_two = make_slice(0x41, 0, 0, 0, &fixed_bits(2, 4));
    assert_eq!(
        parse_slice_header(&poc_two, &make_sps(2, false, true), &make_pps(false))
            .unwrap()
            .frame_num,
        2
    );

    let field = make_slice(
        0x41,
        0,
        0,
        0,
        &(fixed_bits(3, 4) + "11" + &fixed_bits(5, 4)),
    );
    let header = parse_slice_header(&field, &make_sps(0, false, false), &pps_bottom).unwrap();
    assert!(header.field_pic_flag && header.bottom_field_flag);
    assert_eq!(header.pic_order_cnt_lsb, 5);
    assert!(!header.has_delta_pic_order_bottom);
}

#[test]
fn parses_i_p_b_prefixes_and_redundant_picture_count() {
    let sps = make_sps(0, false, true);
    let pps = make_pps(false);
    let frame_and_poc = fixed_bits(3, 4) + &fixed_bits(5, 4);

    let i_slice = make_slice(0x41, 0, 2, 0, &frame_and_poc);
    let header = parse_slice_header(&i_slice, &sps, &pps).unwrap();
    assert!(!header.num_ref_idx_active_override);
    assert!(!header.has_direct_spatial_mv_pred);

    let p_slice = make_slice_with_prefix(
        0x41,
        0,
        0,
        0,
        &frame_and_poc,
        &format!("1{}", ue_bits(3)),
        None,
    );
    let header = parse_slice_header(&p_slice, &sps, &pps).unwrap();
    assert!(header.num_ref_idx_active_override);
    assert_eq!(header.num_ref_idx_l0_active_minus1, 3);
    assert!(!header.has_direct_spatial_mv_pred);

    let b_slice = make_slice_with_prefix(
        0x41,
        0,
        1,
        0,
        &frame_and_poc,
        &format!("11{}{}", ue_bits(2), ue_bits(1)),
        None,
    );
    let header = parse_slice_header(&b_slice, &sps, &pps).unwrap();
    assert!(header.direct_spatial_mv_pred && header.has_direct_spatial_mv_pred);
    assert!(header.num_ref_idx_active_override);
    assert_eq!(
        (
            header.num_ref_idx_l0_active_minus1,
            header.num_ref_idx_l1_active_minus1
        ),
        (2, 1)
    );

    let redundant = make_slice_with_prefix(0x41, 0, 2, 0, &frame_and_poc, "", Some(5));
    let header =
        parse_slice_header(&redundant, &sps, &make_pps_with_redundant(false, true)).unwrap();
    assert!(header.has_redundant_pic_cnt);
    assert_eq!(header.redundant_pic_cnt, 5);
}

#[test]
fn parses_slice_headers_in_compact_fixtures() {
    let fixture_dir = PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("../testdata/h264");
    for name in ["high42-1080p.mp4", "high52-2160p.mp4"] {
        let mut reader = VideoSampleReader::open(fixture_dir.join(name)).unwrap();
        let sps = parse_sps(&reader.configuration().sequence_parameter_sets[0]).unwrap();
        let pps = parse_pps(&reader.configuration().picture_parameter_sets[0]).unwrap();
        let mut picture_types = [false; 3];
        loop {
            let Some(sample) = reader.next_sample().unwrap() else {
                break;
            };
            let mut offset = 0;
            while offset < sample.data.len() {
                assert!(
                    sample.data.len() - offset >= 4,
                    "{name} sample {} NAL prefix",
                    sample.index
                );
                let size = u32::from_be_bytes(sample.data[offset..offset + 4].try_into().unwrap())
                    as usize;
                offset += 4;
                assert!(
                    size > 0 && size <= sample.data.len() - offset,
                    "{name} sample {} NAL size",
                    sample.index
                );
                let nal = &sample.data[offset..offset + size];
                let header = parse_nal_header(nal).unwrap();
                if header.unit_type == 1 || header.unit_type == 5 {
                    let slice = parse_slice_header(nal, &sps, &pps).unwrap();
                    if slice.idr && pps.entropy_coding_mode {
                        assert_eq!(slice.slice_data_bit_offset % 8, 0, "{name}");
                        let rbsp = ebsp_to_rbsp(&nal[1..]).unwrap();
                        let byte_offset = slice.slice_data_bit_offset / 8;
                        assert!(byte_offset < rbsp.len(), "{name}");
                        let mut cabac = CabacArithmeticDecoder::new(&rbsp[byte_offset..]).unwrap();
                        if slice.first_macroblock_in_slice == 0 && slice.slice_type % 5 == 2 {
                            let reference_contexts =
                                new_cabac_i_intra_mb_type_contexts(26).unwrap();
                            let states: Vec<_> = reference_contexts
                                .iter()
                                .map(|model| (model.state_index(), model.mps()))
                                .collect();
                            assert_eq!(
                                states,
                                [
                                    (46, false),
                                    (6, false),
                                    (14, true),
                                    (17, true),
                                    (2, true),
                                    (20, false),
                                    (11, false),
                                    (1, false),
                                ]
                            );
                            let mode_reference_contexts =
                                new_cabac_i_intra4x4_pred_mode_contexts(26).unwrap();
                            let mode_states: Vec<_> = mode_reference_contexts
                                .iter()
                                .map(|model| (model.state_index(), model.mps()))
                                .collect();
                            assert_eq!(mode_states, [(1, false), (2, true)]);
                            let slice_qpy = 26 + pps.pic_init_qp_minus26 + slice.slice_qp_delta;
                            let mut contexts =
                                new_cabac_i_intra_mb_type_contexts(slice_qpy as i32).unwrap();
                            let mb_type = cabac
                                .decode_i_intra_mb_type(
                                    slice.slice_type,
                                    &mut contexts,
                                    false,
                                    false,
                                    false,
                                    false,
                                )
                                .unwrap();
                            assert_eq!(mb_type, 0, "{name} first IDR mb_type should be Intra_NxN");
                            let transform_contexts =
                                new_cabac_i_transform_size_8x8_contexts(26).unwrap();
                            let transform_states: Vec<_> = transform_contexts
                                .iter()
                                .map(|model| (model.state_index(), model.mps()))
                                .collect();
                            assert_eq!(transform_states, [(7, true), (17, true), (26, true)]);
                            if pps.transform_8x8_mode {
                                let mut transform_contexts =
                                    new_cabac_i_transform_size_8x8_contexts(slice_qpy as i32)
                                        .unwrap();
                                let transform_size_8x8 = cabac
                                    .decode_transform_size_8x8_flag(
                                        &mut transform_contexts,
                                        false,
                                        false,
                                    )
                                    .unwrap();
                                assert!(
                                    !transform_size_8x8,
                                    "{name} first IDR transform flag should be false"
                                );
                            }
                            let mut mode_contexts =
                                new_cabac_i_intra4x4_pred_mode_contexts(slice_qpy as i32).unwrap();
                            let block_positions = [
                                (0, 0),
                                (1, 0),
                                (0, 1),
                                (1, 1),
                                (2, 0),
                                (3, 0),
                                (2, 1),
                                (3, 1),
                                (0, 2),
                                (1, 2),
                                (0, 3),
                                (1, 3),
                                (2, 2),
                                (3, 2),
                                (2, 3),
                                (3, 3),
                            ];
                            let mut modes_by_position = [[2_u8; 4]; 4];
                            let mut decoded_modes = [0_u8; 16];
                            for (block_index, (x, y)) in block_positions.into_iter().enumerate() {
                                let left_mode = if x > 0 {
                                    modes_by_position[y][x - 1]
                                } else {
                                    2
                                };
                                let top_mode = if y > 0 {
                                    modes_by_position[y - 1][x]
                                } else {
                                    2
                                };
                                let predicted_mode = left_mode.min(top_mode);
                                let mode = cabac
                                    .decode_intra4x4_pred_mode(predicted_mode, &mut mode_contexts)
                                    .unwrap();
                                modes_by_position[y][x] = mode;
                                decoded_modes[block_index] = mode;
                            }
                            assert_eq!(
                                decoded_modes, [2_u8; 16],
                                "{name} first IDR luma prediction modes"
                            );
                            let chroma_reference_contexts =
                                new_cabac_i_intra_chroma_pred_mode_contexts(26).unwrap();
                            let chroma_states: Vec<_> = chroma_reference_contexts
                                .iter()
                                .map(|model| (model.state_index(), model.mps()))
                                .collect();
                            assert_eq!(
                                chroma_states,
                                [(4, true), (28, true), (33, true), (3, false)]
                            );
                            let mut chroma_contexts =
                                new_cabac_i_intra_chroma_pred_mode_contexts(slice_qpy as i32)
                                    .unwrap();
                            let chroma_mode = cabac
                                .decode_intra_chroma_pred_mode(&mut chroma_contexts, false, false)
                                .unwrap();
                            assert_eq!(
                                chroma_mode, 0,
                                "{name} first IDR chroma prediction mode should be DC"
                            );
                            let mut luma_cbp_contexts =
                                new_cabac_i_luma_coded_block_pattern_contexts(slice_qpy as i32)
                                    .unwrap();
                            let luma_cbp = cabac
                                .decode_luma_coded_block_pattern(0, 0, &mut luma_cbp_contexts)
                                .unwrap();
                            assert_eq!(luma_cbp, 13, "{name} first IDR luma coded-block-pattern");
                            let mut chroma_cbp_contexts =
                                new_cabac_i_chroma_coded_block_pattern_contexts(slice_qpy as i32)
                                    .unwrap();
                            let chroma_cbp = cabac
                                .decode_chroma_coded_block_pattern(0, 0, &mut chroma_cbp_contexts)
                                .unwrap();
                            assert_eq!(
                                chroma_cbp, 2,
                                "{name} first IDR chroma coded-block-pattern"
                            );
                            let mut qp_delta_contexts =
                                new_cabac_i_mb_qp_delta_contexts(slice_qpy as i32).unwrap();
                            let qp_delta =
                                cabac.decode_mb_qp_delta(&mut qp_delta_contexts, 0).unwrap();
                            let expected_qp_delta = match name {
                                "high42-1080p.mp4" => -1,
                                "high52-2160p.mp4" => 0,
                                _ => unreachable!("unexpected compact fixture {name}"),
                            };
                            assert_eq!(qp_delta, expected_qp_delta, "{name} first IDR mb_qp_delta");
                            let mut coded_block_flag_contexts =
                                new_cabac_i_luma4x4_coded_block_flag_contexts(
                                    slice_qpy as i32 + qp_delta,
                                )
                                .unwrap();
                            let coded_block_flag = cabac
                                .decode_luma4x4_coded_block_flag(
                                    0,
                                    0,
                                    &mut coded_block_flag_contexts,
                                )
                                .unwrap();
                            let expected_coded_block_flag = match name {
                                "high42-1080p.mp4" => true,
                                "high52-2160p.mp4" => false,
                                _ => unreachable!("unexpected compact fixture {name}"),
                            };
                            assert_eq!(
                                coded_block_flag, expected_coded_block_flag,
                                "{name} first IDR luma coded-block-flag"
                            );
                        }
                    }
                    picture_types[usize::from(slice.slice_type % 5)] = true;
                }
                offset += size;
            }
        }
        assert_eq!(picture_types, [true, true, true], "{name}");
    }
}

#[test]
fn parses_sps_pps_and_slice_metadata_for_every_eligible_input() {
    let repository_root = PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("..");
    let manifest = std::fs::read_to_string(repository_root.join("silkroad1.sha256")).unwrap();
    let active_paths: Vec<_> = manifest
        .lines()
        .filter_map(|line| line.split_whitespace().nth(1))
        .collect();
    assert_eq!(active_paths.len(), 10);
    let mut all_slice_types = [false; 3];

    for relative_path in active_paths {
        let mut reader = VideoSampleReader::open(repository_root.join(relative_path)).unwrap();
        let configuration = reader.configuration();
        assert_eq!(configuration.nal_length_size, 4, "{relative_path}");
        assert_eq!(
            configuration.sequence_parameter_sets.len(),
            1,
            "{relative_path}"
        );
        assert_eq!(
            configuration.picture_parameter_sets.len(),
            1,
            "{relative_path}"
        );
        let sps = parse_sps(&configuration.sequence_parameter_sets[0]).unwrap();
        let pps = parse_pps(&configuration.picture_parameter_sets[0]).unwrap();
        assert_eq!(sps.profile_idc, 100, "{relative_path}");
        assert!(
            matches!(sps.level_idc, 42 | 52),
            "{relative_path}: {}",
            sps.level_idc
        );
        assert_eq!(sps.chroma_format_idc, 1, "{relative_path}");
        assert_eq!(
            (sps.bit_depth_luma, sps.bit_depth_chroma),
            (8, 8),
            "{relative_path}"
        );
        assert!(
            matches!((sps.width, sps.height), (1920, 1080) | (3840, 2160)),
            "{relative_path}"
        );
        assert!(
            sps.frame_mbs_only && !sps.separate_colour_plane,
            "{relative_path}"
        );
        assert_eq!(pps.sequence_parameter_set_id, sps.sps_id, "{relative_path}");
        assert!(pps.entropy_coding_mode, "{relative_path}");
        assert_eq!(pps.num_slice_groups_minus1, 0, "{relative_path}");

        let mut slice_count = 0;
        while let Some(sample) = reader.next_sample().unwrap() {
            let mut offset = 0;
            while offset < sample.data.len() {
                assert!(
                    sample.data.len() - offset >= 4,
                    "{relative_path} sample {} NAL prefix",
                    sample.index
                );
                let size = u32::from_be_bytes(sample.data[offset..offset + 4].try_into().unwrap())
                    as usize;
                offset += 4;
                assert!(
                    size > 0 && size <= sample.data.len() - offset,
                    "{relative_path} sample {} NAL size",
                    sample.index
                );
                let nal = &sample.data[offset..offset + size];
                let header = parse_nal_header(nal).unwrap();
                if header.unit_type == 1 || header.unit_type == 5 {
                    let slice = parse_slice_header(nal, &sps, &pps).unwrap_or_else(|error| {
                        panic!("{relative_path} sample {}: {error}", sample.index)
                    });
                    assert_eq!(
                        slice.picture_parameter_set_id, pps.picture_parameter_set_id,
                        "{relative_path}"
                    );
                    assert_eq!(
                        slice.pic_order_cnt_type, sps.pic_order_cnt_type,
                        "{relative_path}"
                    );
                    assert_eq!(slice.idr, header.unit_type == 5, "{relative_path}");
                    let slice_type = usize::from(slice.slice_type % 5);
                    assert!(
                        slice_type <= 2,
                        "{relative_path} unsupported slice type {}",
                        slice.slice_type
                    );
                    all_slice_types[slice_type] = true;
                    slice_count += 1;
                }
                offset += size;
            }
        }
        assert!(slice_count > 0, "{relative_path} has no parsed VCL slices");
    }

    assert_eq!(all_slice_types, [true, true, true]);
}

#[test]
fn rejects_slice_identity_mismatches_and_truncation() {
    let sps = make_sps(0, false, true);
    let pps = make_pps(false);
    let wrong_pps = make_slice(0x41, 0, 0, 1, &(fixed_bits(0, 4) + &fixed_bits(0, 4)));
    assert!(parse_slice_header(&wrong_pps, &sps, &pps)
        .unwrap_err()
        .to_string()
        .contains("supplied PPS"));
    let invalid_type = make_slice(0x41, 0, 10, 0, &(fixed_bits(0, 4) + &fixed_bits(0, 4)));
    assert!(parse_slice_header(&invalid_type, &sps, &pps)
        .unwrap_err()
        .to_string()
        .contains("slice_type"));
    assert!(parse_slice_header(&[0x67, 0x80], &sps, &pps)
        .unwrap_err()
        .to_string()
        .contains("not a coded slice"));
    let mut truncated = vec![0x41];
    truncated.extend(pack_bits(&(ue_bits(0) + &ue_bits(0) + &ue_bits(0) + "00")));
    assert_eq!(
        parse_slice_header(&truncated, &sps, &pps)
            .unwrap_err()
            .kind(),
        std::io::ErrorKind::UnexpectedEof
    );
}

#[test]
fn rejects_pps_referencing_a_different_sps() {
    let sps = make_sps(0, false, true);
    let wrong_sps_pps = make_pps_with_ids(0, 1, false, false);
    let nal = make_slice(0x41, 0, 2, 0, &(fixed_bits(0, 4) + &fixed_bits(0, 4)));
    assert!(parse_slice_header(&nal, &sps, &wrong_sps_pps)
        .unwrap_err()
        .to_string()
        .contains("PPS references SPS"));
}

#[test]
fn rejects_unhandled_slice_classes_and_override_bounds() {
    let sps = make_sps(0, false, true);
    let pps = make_pps(false);
    let prefix = fixed_bits(0, 4) + &fixed_bits(0, 4);
    let sp_slice = make_slice(0x41, 0, 3, 0, &prefix);
    assert!(parse_slice_header(&sp_slice, &sps, &pps)
        .unwrap_err()
        .to_string()
        .contains("unsupported slice_type"));
    let l0_slice =
        make_slice_with_prefix(0x41, 0, 0, 0, &prefix, &format!("1{}", ue_bits(32)), None);
    assert!(parse_slice_header(&l0_slice, &sps, &pps)
        .unwrap_err()
        .to_string()
        .contains("l0_active_minus1 exceeds 31"));
    let l1_slice = make_slice_with_prefix(
        0x41,
        0,
        1,
        0,
        &prefix,
        &format!("11{}{}", ue_bits(0), ue_bits(32)),
        None,
    );
    assert!(parse_slice_header(&l1_slice, &sps, &pps)
        .unwrap_err()
        .to_string()
        .contains("l1_active_minus1 exceeds 31"));
}

#[test]
fn detects_primary_picture_identity_boundaries() {
    let base = parse_slice_header(
        &make_slice(0x41, 0, 2, 0, &(fixed_bits(3, 4) + &fixed_bits(4, 4))),
        &make_sps(0, false, true),
        &make_pps(false),
    )
    .unwrap();
    let mut same = base.clone();
    same.first_macroblock_in_slice = 12;
    assert!(same_primary_picture(&base, &same));

    let mut different = base.clone();
    different.frame_num += 1;
    assert!(!same_primary_picture(&base, &different));
    different = base.clone();
    different.picture_parameter_set_id += 1;
    assert!(!same_primary_picture(&base, &different));
    different = base.clone();
    different.field_pic_flag = true;
    assert!(!same_primary_picture(&base, &different));
    different = base.clone();
    different.nal_ref_idc = 0;
    assert!(!same_primary_picture(&base, &different));
    different = base.clone();
    different.idr = true;
    assert!(!same_primary_picture(&base, &different));
    different = base.clone();
    different.pic_order_cnt_lsb += 1;
    assert!(!same_primary_picture(&base, &different));
    different = base.clone();
    different.has_delta_pic_order_bottom = true;
    different.delta_pic_order_bottom = 1;
    assert!(!same_primary_picture(&base, &different));

    different = base.clone();
    different.separate_colour_plane = true;
    assert!(!same_primary_picture(&base, &different));
    different = base.clone();
    different.separate_colour_plane = true;
    different.colour_plane_id = 1;
    assert!(!same_primary_picture(&base, &different));

    let mut poc_one = base.clone();
    poc_one.pic_order_cnt_type = 1;
    poc_one.has_pic_order_cnt_lsb = false;
    poc_one.has_delta_pic_order_cnt0 = true;
    poc_one.delta_pic_order_cnt0 = -1;
    let mut poc_one_different = poc_one.clone();
    poc_one_different.delta_pic_order_cnt0 = 1;
    assert!(!same_primary_picture(&poc_one, &poc_one_different));
    let mut poc_two = base.clone();
    poc_two.pic_order_cnt_type = 2;
    poc_two.has_pic_order_cnt_lsb = false;
    let mut poc_two_other = poc_two.clone();
    poc_two_other.pic_order_cnt_lsb = 99;
    assert!(same_primary_picture(&poc_two, &poc_two_other));
}

#[test]
fn groups_consecutive_slices_by_primary_picture() {
    let first = parse_slice_header(
        &make_slice(0x41, 0, 2, 0, &(fixed_bits(1, 4) + &fixed_bits(2, 4))),
        &make_sps(2, false, true),
        &make_pps(false),
    )
    .unwrap();
    let mut another_slice = first.clone();
    another_slice.first_macroblock_in_slice = 10;
    let mut next_picture = first.clone();
    next_picture.frame_num += 1;
    let groups = group_slices_into_pictures(&[first, another_slice, next_picture]);
    assert_eq!(groups.iter().map(Vec::len).collect::<Vec<_>>(), vec![2, 1]);
    assert!(group_slices_into_pictures(&[]).is_empty());
}
