use pygorvid::{
    ebsp_to_rbsp, parse_nal_header, parse_pps, parse_slice_header, parse_sps,
    split_h264_sample_nals, split_length_prefixed_nals,
};
use std::io::ErrorKind;

#[test]
fn parses_nal_header_fields() {
    for (value, expected_reference_idc, expected_type) in
        [(0x65, 3, 5), (0x41, 2, 1), (0x06, 0, 6), (0x1f, 0, 31)]
    {
        let header = parse_nal_header(&[value]).unwrap();
        assert_eq!(header.reference_idc, expected_reference_idc);
        assert_eq!(header.unit_type, expected_type);
    }
    assert_eq!(
        parse_nal_header(&[]).unwrap_err().kind(),
        ErrorKind::InvalidInput
    );
    assert_eq!(
        parse_nal_header(&[0xe5]).unwrap_err().kind(),
        ErrorKind::InvalidData
    );
}

#[test]
fn splits_length_prefixed_nals_for_configured_widths() {
    for length_size in [1, 2, 4] {
        let expected = [vec![0x65, 0xaa], vec![0x41, 0xbb, 0xcc]];
        let mut sample = Vec::new();
        for nal in &expected {
            let prefix = (nal.len() as u32).to_be_bytes();
            sample.extend_from_slice(&prefix[4 - length_size..]);
            sample.extend_from_slice(nal);
        }
        assert_eq!(
            split_length_prefixed_nals(&sample, length_size).unwrap(),
            expected
        );
    }
}

#[test]
fn rejects_invalid_length_prefixed_nal_boundaries() {
    for (sample, length_size, expected_kind, message) in [
        (
            &[0][..],
            2,
            ErrorKind::UnexpectedEof,
            "truncated NAL length prefix",
        ),
        (&[0, 0][..], 2, ErrorKind::InvalidData, "zero length"),
        (
            &[0, 2, 0x65][..],
            2,
            ErrorKind::UnexpectedEof,
            "NAL unit is truncated",
        ),
    ] {
        let error = split_length_prefixed_nals(sample, length_size).unwrap_err();
        assert_eq!(error.kind(), expected_kind);
        assert!(error.to_string().contains(message));
    }
    assert_eq!(
        split_length_prefixed_nals(&[], 3).unwrap_err().kind(),
        ErrorKind::InvalidInput
    );
}

#[test]
fn removes_valid_emulation_prevention_bytes() {
    for value in 0..=3 {
        assert_eq!(ebsp_to_rbsp(&[0, 0, 3, value]).unwrap(), [0, 0, value]);
    }
    let encoded = [0x67, 0, 0, 3, 0, 0, 3, 0, 1, 0x80];
    assert_eq!(
        ebsp_to_rbsp(&encoded).unwrap(),
        [0x67, 0, 0, 0, 0, 0, 1, 0x80]
    );
    assert_eq!(
        ebsp_to_rbsp(&[0x67, 0x12, 0x34]).unwrap(),
        [0x67, 0x12, 0x34]
    );
}

#[test]
fn rejects_malformed_emulation_prevention_sequences() {
    for encoded in [
        &[0, 0, 0][..],
        &[0, 0, 1][..],
        &[0, 0, 2][..],
        &[0, 0, 3][..],
        &[0, 0, 3, 4][..],
    ] {
        let error = ebsp_to_rbsp(encoded).unwrap_err();
        assert_eq!(error.kind(), ErrorKind::InvalidData);
        assert!(error.to_string().contains("emulation-prevention"));
    }
}

fn sample_bytes(nals: &[&[u8]], length_size: usize) -> Vec<u8> {
    let mut sample = Vec::new();
    for nal in nals {
        sample.extend_from_slice(&(nal.len() as u32).to_be_bytes()[4 - length_size..]);
        sample.extend_from_slice(nal);
    }
    sample
}

#[test]
fn splits_appended_annex_b_end_markers_without_relaxing_ebsp_validation() {
    for length_size in [1, 2, 4] {
        for header in [0x41, 0x65] {
            for marker in [0x0a, 0x0b] {
                for prefix in [&[0, 0, 1][..], &[0, 0, 0, 1][..]] {
                    let slice = [header, 0x80];
                    let nal = [&slice[..], prefix, &[marker]].concat();
                    let sample = sample_bytes(&[&nal], length_size);
                    assert!(ebsp_to_rbsp(&nal[1..]).is_err());
                    assert_eq!(
                        split_length_prefixed_nals(&sample, length_size).unwrap(),
                        [&nal[..]]
                    );
                    let nals = split_h264_sample_nals(&sample, length_size).unwrap();
                    assert_eq!(nals, [&slice[..], &[marker]]);
                    assert_eq!(ebsp_to_rbsp(&nals[0][1..]).unwrap(), [0x80]);
                }
            }
        }
    }
}

#[test]
fn leaves_other_nal_payloads_and_nonfinal_markers_untouched() {
    for nal in [
        &[0x41, 0x80][..],
        &[0x06, 0x80, 0, 0, 0, 1, 0x0a][..],
        &[0x41, 0x80, 0, 0, 3, 1, 0x0a][..],
        &[0x41, 0x80, 0, 0, 0, 1, 0x0c][..],
        &[0x41, 0x80, 0, 0, 0, 1, 0x0a, 0x80][..],
        &[0x41, 0, 0, 0, 1, 0x0a][..],
    ] {
        let sample = sample_bytes(&[nal], 4);
        assert_eq!(split_h264_sample_nals(&sample, 4).unwrap(), [nal]);
    }
    let bad = [0x41, 0x80, 0, 0, 0, 1, 0x0a];
    let next = [0x41, 0x80];
    let sample = sample_bytes(&[&bad, &next], 4);
    let nals = split_h264_sample_nals(&sample, 4).unwrap();
    assert_eq!(nals, [&bad[..], &next[..]]);
    assert!(ebsp_to_rbsp(&nals[0][1..]).is_err());
    let bad = [0x41, 0, 0, 2, 0x80, 0, 0, 0, 1, 0x0a];
    let sample = sample_bytes(&[&bad], 4);
    let nals = split_h264_sample_nals(&sample, 4).unwrap();
    assert!(ebsp_to_rbsp(&nals[0][1..]).is_err());
    for (sample, width) in [
        (&[0][..], 2),
        (&[0][..], 1),
        (&[2, 0x41][..], 1),
        (&[][..], 3),
    ] {
        assert!(split_h264_sample_nals(sample, width).is_err());
    }
}

#[test]
fn rannirune_final_slice_passes_strict_validation_after_framing_recovery() {
    let records: Vec<Vec<u8>> = include_str!("../../testdata/h264/rannirune-eos.hex")
        .split_whitespace()
        .map(|hex| {
            (0..hex.len())
                .step_by(2)
                .map(|offset| u8::from_str_radix(&hex[offset..offset + 2], 16).unwrap())
                .collect()
        })
        .collect();
    assert_eq!(records.len(), 3);
    let sps = parse_sps(&records[0]).unwrap();
    let pps = parse_pps(&records[1]).unwrap();
    let nal = &records[2];
    assert_eq!(nal.len(), 1092);
    assert!(parse_slice_header(nal, &sps, &pps).is_err());
    let sample = sample_bytes(&[nal], 4);
    let nals = split_h264_sample_nals(&sample, 4).unwrap();
    assert_eq!(nals, [&nal[..1087], &[0x0a]]);
    assert_eq!(ebsp_to_rbsp(&nals[0][1..]).unwrap().len(), 1086);
    let header = parse_slice_header(nals[0], &sps, &pps).unwrap();
    assert_eq!(
        header,
        parse_slice_header(&nal[..1087], &sps, &pps).unwrap()
    );
}
