use crate::H264Encoder;
use std::io;

#[derive(Clone, Debug, Default)]
pub struct Mp4Metadata {
    pub title: String,
    pub artist: String,
    pub album_artist: String,
    pub genre: String,
    pub track: String,
    pub disc: String,
    pub date: String,
    pub composer: String,
    pub comment: String,
    pub description: String,
    pub copyright: String,
    pub make: String,
    pub model: String,
    pub firmware: String,
    pub location: String,
    pub altitude: String,
    pub heading: String,
    pub encoder: String,
}

impl Mp4Metadata {
    fn entries(&self) -> [(&str, &String); 18] {
        [
            ("title", &self.title),
            ("artist", &self.artist),
            ("album_artist", &self.album_artist),
            ("genre", &self.genre),
            ("track", &self.track),
            ("disc", &self.disc),
            ("date", &self.date),
            ("composer", &self.composer),
            ("comment", &self.comment),
            ("description", &self.description),
            ("copyright", &self.copyright),
            ("make", &self.make),
            ("model", &self.model),
            ("firmware", &self.firmware),
            ("location", &self.location),
            ("altitude", &self.altitude),
            ("heading", &self.heading),
            ("encoder", &self.encoder),
        ]
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn optional_metadata_preserves_utf8() {
        let writer = Mp4Writer::new();
        let encoder = H264Encoder::new();
        let plain = writer.encode(&encoder);
        assert_eq!(plain, writer.encode_with_metadata(&encoder, None).unwrap());
        assert_eq!(
            plain,
            writer
                .encode_with_metadata(&encoder, Some(&Mp4Metadata::default()))
                .unwrap()
        );
        let value = "Caf\u{e9} \u{65e5}\u{672c} \u{1f4f7}\0tail";
        let metadata = Mp4Metadata {
            title: value.into(),
            ..Mp4Metadata::default()
        };
        let tagged = writer
            .encode_with_metadata(&encoder, Some(&metadata))
            .unwrap();
        assert!(tagged
            .windows(value.len())
            .any(|bytes| bytes == value.as_bytes()));
        assert_eq!(
            metadata_box(Some(&metadata)).unwrap().len(),
            69 + 32 + "title".len() + value.len()
        );
        assert!(!plain.windows(4).any(|bytes| bytes == b"mdta"));
        assert!(tagged.windows(4).any(|bytes| bytes == b"ilst"));
        assert!(tagged.windows(8).any(|bytes| bytes == b"mdirappl"));
        let mdta = metadata_box(Some(&metadata)).unwrap();
        assert!(tagged.windows(mdta.len()).any(|bytes| bytes == mdta));
    }

    #[test]
    fn compatibility_numbers_preserve_raw_text() {
        for value in ["0", "1", "001", "65535", "65536", "-1", "1/2", "\u{661}"] {
            let metadata = Mp4Metadata {
                track: value.into(),
                disc: value.into(),
                ..Default::default()
            };
            let payload = compatibility_metadata_box(Some(&metadata)).unwrap();
            assert!(payload
                .windows(value.len())
                .any(|bytes| bytes == value.as_bytes()));
            let binary = ["0", "1", "001", "65535"].contains(&value);
            assert_eq!(payload.windows(4).any(|bytes| bytes == b"trkn"), binary);
            assert_eq!(payload.windows(4).any(|bytes| bytes == b"disk"), binary);
        }
    }
}

fn metadata_box(metadata: Option<&Mp4Metadata>) -> io::Result<Vec<u8>> {
    let Some(metadata) = metadata else {
        return Ok(Vec::new());
    };
    let entries = metadata.entries();
    let entries: Vec<_> = entries
        .into_iter()
        .filter(|(_, value)| !value.is_empty())
        .collect();
    if entries.is_empty() {
        return Ok(Vec::new());
    }
    let length = 69u64
        + entries
            .iter()
            .map(|(key, value)| 32 + key.len() as u64 + value.len() as u64)
            .sum::<u64>();
    if length > u64::from(u32::MAX) {
        return Err(io::Error::new(
            io::ErrorKind::InvalidInput,
            "MP4 metadata exceeds the 32-bit box size limit",
        ));
    }
    let mut keys = Vec::new();
    let mut items = Vec::new();
    for (index, (key, value)) in entries.iter().enumerate() {
        keys.extend(mp4_box(b"mdta", &[key.as_bytes()]));
        items.extend(mp4_box(
            &((index + 1) as u32).to_be_bytes(),
            &[&full_box(b"data", 1, &[&u32s(&[0]), value.as_bytes()])],
        ));
    }
    let handler = full_box(b"hdlr", 0, &[&u32s(&[0]), b"mdta", &[0; 12], &[0]]);
    Ok(full_box(
        b"meta",
        0,
        &[
            &handler,
            &full_box(b"keys", 0, &[&u32s(&[entries.len() as u32]), &keys]),
            &mp4_box(b"ilst", &[&items]),
        ],
    ))
}

fn u32s(values: &[u32]) -> Vec<u8> {
    values
        .iter()
        .flat_map(|value| value.to_be_bytes())
        .collect()
}

fn compatibility_metadata_box(metadata: Option<&Mp4Metadata>) -> io::Result<Vec<u8>> {
    let Some(metadata) = metadata else {
        return Ok(Vec::new());
    };
    let entries = metadata.entries();
    if entries.iter().all(|(_, value)| value.is_empty()) {
        return Ok(Vec::new());
    }
    let text_tags: [(&[u8; 4], &String); 12] = [
        (b"\xa9nam", &metadata.title),
        (b"\xa9ART", &metadata.artist),
        (b"aART", &metadata.album_artist),
        (b"\xa9gen", &metadata.genre),
        (b"\xa9day", &metadata.date),
        (b"\xa9wrt", &metadata.composer),
        (b"\xa9com", &metadata.composer),
        (b"\xa9cmt", &metadata.comment),
        (b"desc", &metadata.description),
        (b"cprt", &metadata.copyright),
        (b"\xa9cpy", &metadata.copyright),
        (b"\xa9too", &metadata.encoder),
    ];
    let length = 61u64
        + 32
        + 30
        + entries
            .iter()
            .filter(|(_, value)| !value.is_empty())
            .map(|(key, value)| 64 + key.len() as u64 + value.len() as u64)
            .sum::<u64>()
        + text_tags
            .iter()
            .filter(|(_, value)| !value.is_empty())
            .map(|(_, value)| 24 + value.len() as u64)
            .sum::<u64>();
    if length > u64::from(u32::MAX) {
        return Err(io::Error::new(
            io::ErrorKind::InvalidInput,
            "MP4 compatibility metadata exceeds the 32-bit box size limit",
        ));
    }
    let mut items = Vec::new();
    for (tag, value) in text_tags {
        if !value.is_empty() {
            items.extend(mp4_box(
                tag,
                &[&full_box(b"data", 1, &[&u32s(&[0]), value.as_bytes()])],
            ));
        }
    }
    for (tag, value, trailing) in [
        (b"trkn", &metadata.track, true),
        (b"disk", &metadata.disc, false),
    ] {
        if value.is_empty() || value.len() > 5 || !value.bytes().all(|byte| byte.is_ascii_digit()) {
            continue;
        }
        // Text-only values remain available through mdta and free-form tags.
        if let Ok(number) = value.parse::<u16>() {
            let mut data = u16s(&[0, number, 0]);
            if trailing {
                data.extend([0, 0]);
            }
            items.extend(mp4_box(
                tag,
                &[&full_box(b"data", 0, &[&u32s(&[0]), &data])],
            ));
        }
    }
    for (key, value) in entries {
        if !value.is_empty() {
            items.extend(mp4_box(
                b"----",
                &[
                    &full_box(b"mean", 0, &[b"com.apple.iTunes"]),
                    &full_box(b"name", 0, &[key.as_bytes()]),
                    &full_box(b"data", 1, &[&u32s(&[0]), value.as_bytes()]),
                ],
            ));
        }
    }
    let handler = full_box(b"hdlr", 0, &[&u32s(&[0]), b"mdirappl", &[0; 8], &[0]]);
    Ok(mp4_box(
        b"udta",
        &[&full_box(
            b"meta",
            0,
            &[&handler, &mp4_box(b"ilst", &[&items])],
        )],
    ))
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
        self.encode_with_metadata(encoder, None)
            .expect("fixed metadata-free MP4 fits 32-bit boxes")
    }

    pub fn encode_with_metadata(
        &self,
        encoder: &H264Encoder,
        metadata: Option<&Mp4Metadata>,
    ) -> io::Result<Vec<u8>> {
        let compatibility = compatibility_metadata_box(metadata)?;
        let metadata = metadata_box(metadata)?;
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
        if 8 + mvhd.len() as u64
            + video_track.len() as u64
            + audio_track.len() as u64
            + metadata.len() as u64
            + compatibility.len() as u64
            > u64::from(u32::MAX)
        {
            return Err(io::Error::new(
                io::ErrorKind::InvalidInput,
                "MP4 movie metadata exceeds the 32-bit box size limit",
            ));
        }
        result.extend(mp4_box(
            b"moov",
            &[&mvhd, &video_track, &audio_track, &metadata, &compatibility],
        ));
        Ok(result)
    }
}
