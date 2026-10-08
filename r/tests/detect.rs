use pygorvid::detect_format;

#[test]
fn detects_containers() {
    assert_eq!(detect_format(b"\0\0\0\x18ftypisom"), "mp4");
    assert_eq!(detect_format(b"\0\0\0\x14ftypqt  "), "mov");
    assert_eq!(detect_format(b"\x1a\x45\xdf\xa3rest"), "matroska");
    assert_eq!(detect_format(b"RIFF\0\0\0\0AVI LIST"), "avi");
    assert_eq!(detect_format(b""), "unknown");
}
