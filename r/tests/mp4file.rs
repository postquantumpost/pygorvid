use std::fs;

use pygorvid::{open_file, Mp4File};

#[test]
fn mp4file_and_vidfile() {
    let dir = std::env::temp_dir().join(format!("pygorvid-mp4-{}", std::process::id()));
    fs::create_dir_all(&dir).unwrap();
    let mp4 = dir.join("a.mp4");
    let mkv = dir.join("a.mkv");
    fs::write(&mp4, b"\0\0\0\x18ftypisom").unwrap();
    fs::write(&mkv, b"\x1a\x45\xdf\xa3rest").unwrap();

    let mut m = Mp4File::new();
    assert!(!m.isopen() && m.errorinfo().0.is_empty());
    assert!(m.open(mp4.to_str().unwrap()) && m.isopen());
    m.close();
    assert!(!m.isopen());
    assert!(!m.open(mkv.to_str().unwrap()));
    assert!(!m.errorinfo().0.is_empty());

    let mut v = open_file(mp4.to_str().unwrap());
    assert!(v.isopen());
    assert_eq!(v.format(), "mp4");
    v.close();
    assert!(!v.isopen());

    fs::remove_dir_all(&dir).unwrap();
}
