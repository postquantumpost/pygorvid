use std::fs;

use pygorvid::{open_file, BasicAudioStreamInfo, BasicInfo, BasicVideoStreamInfo, Mp4File};

fn mkbox(kind: &[u8; 4], payload: &[u8]) -> Vec<u8> {
    let mut b = ((8 + payload.len()) as u32).to_be_bytes().to_vec();
    b.extend_from_slice(kind);
    b.extend_from_slice(payload);
    b
}

fn mktrack(handler: &[u8; 4]) -> Vec<u8> {
    mktrack_with_video_info(handler, 0, 0, 0, 0, 1000)
}

fn mktrack_with_video_info(
    handler: &[u8; 4],
    width: u32,
    height: u32,
    sample_count: u32,
    sample_delta: u32,
    timescale: u32,
) -> Vec<u8> {
    let mut hdlr = vec![0u8; 8];
    hdlr.extend_from_slice(handler);
    hdlr.extend_from_slice(&[0u8; 13]);

    let mut tkhd = vec![0u8; 84];
    tkhd[76..80].copy_from_slice(&(width << 16).to_be_bytes());
    tkhd[80..84].copy_from_slice(&(height << 16).to_be_bytes());
    let mut mdhd = vec![0u8; 20];
    mdhd[12..16].copy_from_slice(&timescale.to_be_bytes());
    let mut stts = vec![0u8; 16];
    stts[4..8].copy_from_slice(&1u32.to_be_bytes());
    stts[8..12].copy_from_slice(&sample_count.to_be_bytes());
    stts[12..16].copy_from_slice(&sample_delta.to_be_bytes());

    let mut mdia = mkbox(b"mdhd", &mdhd);
    mdia.extend(mkbox(b"hdlr", &hdlr));
    mdia.extend(mkbox(b"minf", &mkbox(b"stbl", &mkbox(b"stts", &stts))));
    let mut trak = mkbox(b"tkhd", &tkhd);
    trak.extend(mkbox(b"mdia", &mdia));
    mkbox(b"trak", &trak)
}

fn mktrack_with_audio_info(sample_rate: u32, channels: u16) -> Vec<u8> {
    let mut hdlr = vec![0u8; 8];
    hdlr.extend_from_slice(b"soun");
    hdlr.extend_from_slice(&[0u8; 13]);
    let mut mdhd = vec![0u8; 20];
    mdhd[12..16].copy_from_slice(&sample_rate.to_be_bytes());
    let mut entry = vec![0u8; 28];
    entry[16..18].copy_from_slice(&channels.to_be_bytes());
    entry[24..28].copy_from_slice(&(sample_rate << 16).to_be_bytes());
    let mut stsd = vec![0u8; 8];
    stsd[4..8].copy_from_slice(&1u32.to_be_bytes());
    stsd.extend(mkbox(b"mp4a", &entry));
    let mut stbl = mkbox(b"stsd", &stsd);
    stbl.extend(mkbox(b"stts", &[0u8; 8]));
    let minf = mkbox(b"stbl", &stbl);
    let mut mdia = mkbox(b"mdhd", &mdhd);
    mdia.extend(mkbox(b"hdlr", &hdlr));
    mdia.extend(mkbox(b"minf", &minf));
    let mut trak = mkbox(b"tkhd", &[0u8; 84]);
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
        (
            "video",
            vec![mktrack(b"vide")],
            BasicInfo {
                hasvideo: true,
                hasaudio: false,
                videostreams: vec![BasicVideoStreamInfo::default()],
                audiostreams: vec![],
            },
        ),
        (
            "both",
            vec![mktrack(b"vide"), mktrack(b"soun"), mktrack(b"soun")],
            BasicInfo {
                hasvideo: true,
                hasaudio: true,
                videostreams: vec![BasicVideoStreamInfo::default()],
                audiostreams: vec![
                    BasicAudioStreamInfo::default(),
                    BasicAudioStreamInfo::default(),
                ],
            },
        ),
        (
            "audio",
            vec![mktrack(b"soun")],
            BasicInfo {
                hasvideo: false,
                hasaudio: true,
                videostreams: vec![],
                audiostreams: vec![BasicAudioStreamInfo::default()],
            },
        ),
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
fn basic_info_audio_stream_values() {
    let tracks = vec![
        mktrack_with_audio_info(44100, 2),
        mktrack_with_audio_info(48000, 6),
    ];
    let (dir, path) = write("audio-streams", &mkbox(b"moov", &tracks.concat()));
    let mut file = Mp4File::new();
    assert!(file.open(&path));
    assert_eq!(
        file.getbasicinfo().audiostreams,
        vec![
            BasicAudioStreamInfo {
                sample_rate: 44100,
                channels: 2,
            },
            BasicAudioStreamInfo {
                sample_rate: 48000,
                channels: 6,
            },
        ]
    );
    fs::remove_dir_all(dir).unwrap();
}

#[test]
fn basic_info_video_stream_values() {
    let tracks = vec![
        mktrack_with_video_info(b"vide", 1920, 1080, 300, 1000, 30000),
        mktrack_with_video_info(b"vide", 640, 480, 240, 1000, 24000),
    ];
    let (dir, path) = write("video-streams", &mkbox(b"moov", &tracks.concat()));
    let mut file = Mp4File::new();
    assert!(file.open(&path));
    assert_eq!(
        file.getbasicinfo().videostreams,
        vec![
            BasicVideoStreamInfo {
                width: 1920,
                height: 1080,
                framerate: 30.0
            },
            BasicVideoStreamInfo {
                width: 640,
                height: 480,
                framerate: 24.0
            },
        ]
    );
    fs::remove_dir_all(dir).unwrap();
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
