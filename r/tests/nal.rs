use pygorvid::{ebsp_to_rbsp, parse_nal_header, split_length_prefixed_nals};
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
