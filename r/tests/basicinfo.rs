use std::fs;

use pygorvid::{open_file, BasicInfo, Mp4File};

fn mkbox(kind: &[u8; 4], payload: &[u8]) -> Vec<u8> {
    let mut b = ((8 + payload.len()) as u32).to_be_bytes().to_vec();
    b.extend_from_slice(kind);
    b.extend_from_slice(payload);
    b
}

fn mktrack(handler: &[u8; 4]) -> Vec<u8> {
    let mut hdlr = vec![0u8; 8];
    hdlr.extend_from_slice(handler);
    hdlr.extend_from_slice(&[0u8; 13]);
    let mut mdia = mkbox(b"mdhd", &[0u8; 4]);
    mdia.extend(mkbox(b"hdlr", &hdlr));
    let mut trak = mkbox(b"tkhd", &[0u8; 8]);
    trak.extend(mkbox(b"mdia", &mdia));
    mkbox(b"trak", &trak)
}

fn write(name: &str, tail: &[u8]) -> (std::path::PathBuf, String) {
    let dir = std::env::temp_dir().join(format!("pygorvid-{name}-{}", std::process::id()));
    fs::create_dir_all(&dir).unwrap();
    let mut data = mkbox(b"ftyp", b"isom\0\0\0\0isom");
    data.extend(mkbox(b"free", b"xx"));
    data.extend_from_slice(tail);
    let p = dir.join("a.mp4");
    fs::write(&p, data).unwrap();
    (dir, p.to_str().unwrap().to_string())
}

#[test]
fn basic_info_by_tracks() {
    let cases: Vec<(&str, Vec<Vec<u8>>, BasicInfo)> = vec![
        ("video", vec![mktrack(b"vide")], BasicInfo { hasvideo: true, hasaudio: false }),
        (
            "both",
            vec![mktrack(b"vide"), mktrack(b"soun"), mktrack(b"soun")],
            BasicInfo { hasvideo: true, hasaudio: true },
        ),
        ("audio", vec![mktrack(b"soun")], BasicInfo { hasvideo: false, hasaudio: true }),
        ("none", vec![], BasicInfo::default()),
    ];
    for (name, tracks, want) in cases {
        let (dir, path) = write(name, &mkbox(b"moov", &tracks.concat()));
        let mut m = Mp4File::new();
        assert!(m.open(&path), "{name}");
        assert_eq!(m.getbasicinfo(), want, "{name}");
        assert_eq!(m.errorinfo().0, "", "{name}");
        let mut v = open_file(&path);
        assert_eq!(v.getbasicinfo(), want, "{name} via VidFile");
        fs::remove_dir_all(dir).unwrap();
    }
}

#[test]
fn basic_info_errors() {
    let mut bad = 1000u32.to_be_bytes().to_vec();
    bad.extend_from_slice(b"moov");
    bad.extend_from_slice(&[0u8; 8]);
    let (dir, path) = write("trunc", &bad);
    let mut m = Mp4File::new();
    assert!(m.open(&path));
    assert_eq!(m.getbasicinfo(), BasicInfo::default());
    assert!(!m.errorinfo().0.is_empty());
    fs::remove_dir_all(dir).unwrap();

    let mut idle = Mp4File::new();
    assert_eq!(idle.getbasicinfo(), BasicInfo::default());
    assert!(!idle.errorinfo().0.is_empty());

    let dir = std::env::temp_dir().join(format!("pygorvid-mkv-{}", std::process::id()));
    fs::create_dir_all(&dir).unwrap();
    let mkv = dir.join("a.mkv");
    fs::write(&mkv, b"\x1a\x45\xdf\xa3rest").unwrap();
    let mut v = open_file(mkv.to_str().unwrap());
    assert!(v.isopen());
    assert_eq!(v.getbasicinfo(), BasicInfo::default());
    assert!(!v.errorinfo().0.is_empty());
    fs::remove_dir_all(dir).unwrap();
}
