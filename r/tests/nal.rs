use pygorvid::{ebsp_to_rbsp, parse_nal_header};
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
