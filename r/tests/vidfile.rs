use std::fs;

use pygorvid::{open_file, VidFile};

#[test]
fn open_and_close() {
    let dir = std::env::temp_dir().join(format!("pygorvid-test-{}", std::process::id()));
    fs::create_dir_all(&dir).unwrap();
    let good = dir.join("a.mkv");
    let bad = dir.join("a.bin");
    fs::write(&good, b"\x1a\x45\xdf\xa3rest").unwrap();
    fs::write(&bad, b"hello").unwrap();

    let mut v = open_file(good.to_str().unwrap());
    assert!(v.isopen());
    assert_eq!(v.errorinfo().0, "");
    assert_eq!(v.format(), "matroska");
    v.close();
    assert!(!v.isopen());

    let v = open_file(bad.to_str().unwrap());
    assert!(!v.isopen() && !v.errorinfo().0.is_empty());
    let mut v = VidFile::new();
    assert!(!v.open("/nonexistent/x.mp4"));
    assert!(!v.errorinfo().0.is_empty());

    fs::remove_dir_all(&dir).unwrap();
}

#[test]
fn construct_is_idle() {
    let v = pygorvid::construct();
    assert!(!v.isopen());
    assert_eq!(v.errorinfo().0, "");
}
