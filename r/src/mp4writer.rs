use crate::H264Encoder;

fn u32s(values: &[u32]) -> Vec<u8> {
    values
        .iter()
        .flat_map(|value| value.to_be_bytes())
        .collect()
}

fn u16s(values: &[u16]) -> Vec<u8> {
    values
        .iter()
        .flat_map(|value| value.to_be_bytes())
        .collect()
}

fn mp4_box(kind: &[u8; 4], parts: &[&[u8]]) -> Vec<u8> {
    let length = 8 + parts.iter().map(|part| part.len()).sum::<usize>();
    let mut result = Vec::with_capacity(length);
    result.extend_from_slice(&(length as u32).to_be_bytes());
    result.extend_from_slice(kind);
    for part in parts {
        result.extend_from_slice(part);
    }
    result
}

fn full_box(kind: &[u8; 4], flags: u32, parts: &[&[u8]]) -> Vec<u8> {
    let flags = flags.to_be_bytes();
    let mut contents: Vec<&[u8]> = vec![&flags];
    contents.extend_from_slice(parts);
    mp4_box(kind, &contents)
}

fn matrix() -> Vec<u8> {
    u32s(&[0x10000, 0, 0, 0, 0x10000, 0, 0, 0, 0x40000000])
}

fn track(
    id: u32,
    entry: &[u8],
    sizes: &[u8],
    count: u32,
    timescale: u32,
    offset: u32,
    video: bool,
) -> Vec<u8> {
    let dimension = if video { 64 << 16 } else { 0 };
    let volume = if video { 0 } else { 0x100 };
    let tkhd = full_box(
        b"tkhd",
        7,
        &[
            &u32s(&[0, 0, id, 0, 1000]),
            &[0; 8],
            &u16s(&[0, 0, volume, 0]),
            &matrix(),
            &u32s(&[dimension, dimension]),
        ],
    );
    let mdhd = full_box(
        b"mdhd",
        0,
        &[&u32s(&[0, 0, timescale, count]), &u16s(&[0x55c4, 0])],
    );
    let hdlr = full_box(
        b"hdlr",
        0,
        &[
            &u32s(&[0]),
            if video { b"vide" } else { b"soun" },
            &[0; 12],
            if video { b"Video\0" } else { b"Audio\0" },
        ],
    );
    let header = if video {
        full_box(b"vmhd", 1, &[&[0; 8]])
    } else {
        full_box(b"smhd", 0, &[&[0; 4]])
    };
    let dinf = mp4_box(
        b"dinf",
        &[&full_box(
            b"dref",
            0,
            &[&u32s(&[1]), &full_box(b"url ", 1, &[])],
        )],
    );
    let stbl = mp4_box(
        b"stbl",
        &[
            &full_box(b"stsd", 0, &[&u32s(&[1]), entry]),
            &full_box(b"stts", 0, &[&u32s(&[1, count, 1])]),
            &full_box(b"stsc", 0, &[&u32s(&[1, 1, count, 1])]),
            sizes,
            &full_box(b"stco", 0, &[&u32s(&[1, offset])]),
        ],
    );
    mp4_box(
        b"trak",
        &[
            &tkhd,
            &mp4_box(
                b"mdia",
                &[&mdhd, &hdlr, &mp4_box(b"minf", &[&header, &dinf, &stbl])],
            ),
        ],
    )
}

/// Initial subset: one second of 64x64 H.264 and stereo PCM silence.
#[derive(Default)]
pub struct Mp4Writer;

impl Mp4Writer {
    pub fn new() -> Self {
        Self
    }

    pub fn encode(&self, encoder: &H264Encoder) -> Vec<u8> {
        let mut video = Vec::new();
        let mut sizes = vec![0, 25];
        for index in 0..25 {
            let sample = encoder.encode_grey_frame(index);
            sizes.push(sample.len() as u32);
            video.extend(sample);
        }
        let audio = vec![0; 48000 * 2 * 2];
        let sps = encoder.sequence_parameter_set();
        let pps = encoder.picture_parameter_set();
        let avcc = mp4_box(
            b"avcC",
            &[
                &[1, sps[1], sps[2], sps[3], 0xff, 0xe1],
                &u16s(&[sps.len() as u16]),
                &sps,
                &[1],
                &u16s(&[pps.len() as u16]),
                &pps,
            ],
        );
        let visual = mp4_box(
            b"avc1",
            &[
                &[0; 6],
                &u16s(&[1]),
                &[0; 16],
                &u16s(&[64, 64]),
                &u32s(&[0x480000, 0x480000, 0]),
                &u16s(&[1]),
                &[0; 32],
                &u16s(&[24, 0xffff]),
                &avcc,
            ],
        );
        let sound = mp4_box(
            b"sowt",
            &[
                &[0; 6],
                &u16s(&[1]),
                &[0; 8],
                &u16s(&[2, 16, 0, 0]),
                &u32s(&[48000 << 16]),
            ],
        );
        let ftyp = mp4_box(b"ftyp", &[b"isom", &u32s(&[0x200]), b"isomiso2avc1qt  "]);
        let offset = ftyp.len() as u32 + 8;
        let video_track = track(
            1,
            &visual,
            &full_box(b"stsz", 0, &[&u32s(&sizes)]),
            25,
            25,
            offset,
            true,
        );
        let audio_track = track(
            2,
            &sound,
            &full_box(b"stsz", 0, &[&u32s(&[4, 48000])]),
            48000,
            48000,
            offset + video.len() as u32,
            false,
        );
        let mvhd = full_box(
            b"mvhd",
            0,
            &[
                &u32s(&[0, 0, 1000, 1000, 0x10000]),
                &u16s(&[0x100, 0]),
                &[0; 8],
                &matrix(),
                &[0; 24],
                &u32s(&[3]),
            ],
        );
        let mut result = ftyp;
        result.extend(mp4_box(b"mdat", &[&video, &audio]));
        result.extend(mp4_box(b"moov", &[&mvhd, &video_track, &audio_track]));
        result
    }
}
