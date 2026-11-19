//! Deterministic Annex-B corpora for H.264 profile-extension feature tests.

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct H264Companion {
    pub suffix: &'static str,
    pub data: Vec<u8>,
}

pub const H264_COMPANION_SUFFIXES: [&str; 15] = [
    "frame-40-422.264",
    "frame-41-444.264",
    "frame-41-separate.264",
    "frame-42-10bit.264",
    "frame-42-12bit.264",
    "frame-43-fields.264",
    "frame-44-mbaff.264",
    "frame-45-fmo.264",
    "frame-46-redundant.264",
    "frame-47-partitions.264",
    "frame-48-sp-si.264",
    "frame-49-svc.264",
    "frame-49-mvc.264",
    "frame-49-mvcd.264",
    "frame-49-3davc.264",
];

#[derive(Default)]
struct BitWriter {
    data: Vec<u8>,
    bit_count: usize,
}

impl BitWriter {
    fn bits(&mut self, value: u32, count: usize) {
        for shift in (0..count).rev() {
            if self.bit_count % 8 == 0 {
                self.data.push(0);
            }
            *self.data.last_mut().unwrap() |=
                (((value >> shift) & 1) as u8) << (7 - self.bit_count % 8);
            self.bit_count += 1;
        }
    }

    fn ue(&mut self, value: u32) {
        let code = value + 1;
        let count = 32 - code.leading_zeros() as usize;
        self.bits(0, count - 1);
        self.bits(code, count);
    }

    fn se(&mut self, value: i32) {
        self.ue(if value > 0 {
            (2 * value - 1) as u32
        } else {
            (-2 * value) as u32
        });
    }

    fn align(&mut self) {
        while self.bit_count % 8 != 0 {
            self.bits(0, 1);
        }
    }

    fn nal(mut self, header: u8) -> Vec<u8> {
        self.bits(1, 1);
        self.align();
        let mut result = vec![header];
        let mut zeros = 0;
        for value in self.data {
            if zeros == 2 && value <= 3 {
                result.push(3);
                zeros = 0;
            }
            result.push(value);
            zeros = if value == 0 { zeros + 1 } else { 0 };
        }
        result
    }
}

#[derive(Clone, Copy)]
struct Sequence {
    profile_idc: u8,
    chroma_format_idc: u8,
    separate_colour_plane: bool,
    bit_depth_luma: u8,
    bit_depth_chroma: u8,
    frame_mbs_only: bool,
    mb_adaptive_frame_field: bool,
    gaps_allowed: bool,
    poc_type: u32,
    width_mbs: usize,
    height_mbs: usize,
    sps_id: u32,
}

impl Sequence {
    fn new(profile_idc: u8) -> Self {
        Self {
            profile_idc,
            chroma_format_idc: 1,
            separate_colour_plane: false,
            bit_depth_luma: 8,
            bit_depth_chroma: 8,
            frame_mbs_only: true,
            mb_adaptive_frame_field: false,
            gaps_allowed: false,
            poc_type: 2,
            width_mbs: 4,
            height_mbs: 4,
            sps_id: 0,
        }
    }
}

fn sps(sequence: Sequence) -> Vec<u8> {
    let mut writer = BitWriter::default();
    writer.bits(sequence.profile_idc.into(), 8);
    writer.bits(0, 8);
    writer.bits(20, 8);
    writer.ue(sequence.sps_id);
    if matches!(
        sequence.profile_idc,
        44 | 83 | 86 | 100 | 110 | 118 | 122 | 128 | 134 | 135 | 138 | 139 | 144 | 244
    ) {
        writer.ue(sequence.chroma_format_idc.into());
        if sequence.chroma_format_idc == 3 {
            writer.bits(sequence.separate_colour_plane.into(), 1);
        }
        writer.ue((sequence.bit_depth_luma - 8).into());
        writer.ue((sequence.bit_depth_chroma - 8).into());
        writer.bits(0, 2);
    }
    writer.ue(0);
    writer.ue(sequence.poc_type);
    if sequence.poc_type == 0 {
        writer.ue(2);
    } else if sequence.poc_type == 1 {
        writer.bits(0, 1);
        writer.se(0);
        writer.se(0);
        writer.ue(0);
    }
    writer.ue(2);
    writer.bits(sequence.gaps_allowed.into(), 1);
    writer.ue((sequence.width_mbs - 1) as u32);
    let height_units = if sequence.frame_mbs_only {
        sequence.height_mbs
    } else {
        sequence.height_mbs / 2
    };
    writer.ue((height_units - 1) as u32);
    writer.bits(sequence.frame_mbs_only.into(), 1);
    if !sequence.frame_mbs_only {
        writer.bits(sequence.mb_adaptive_frame_field.into(), 1);
    }
    writer.bits(1, 1);
    writer.bits(0, 2);
    writer.nal(0x67)
}

fn pps(
    sps_id: u32,
    redundant: bool,
    transform_8x8: bool,
    slice_groups: u32,
    map_type: u32,
) -> Vec<u8> {
    let mut writer = BitWriter::default();
    writer.ue(0);
    writer.ue(sps_id);
    writer.bits(0, 1);
    writer.bits(0, 1);
    writer.ue(slice_groups);
    if slice_groups != 0 {
        writer.ue(map_type);
        if map_type == 0 {
            writer.ue(7);
            writer.ue(7);
        }
    }
    writer.ue(0);
    writer.ue(0);
    writer.bits(0, 1);
    writer.bits(0, 2);
    writer.se(0);
    writer.se(0);
    writer.se(0);
    writer.bits(1, 1);
    writer.bits(0, 1);
    writer.bits(redundant.into(), 1);
    if transform_8x8 {
        writer.bits(1, 1);
        writer.bits(0, 1);
        writer.se(0);
    }
    writer.nal(0x68)
}

#[derive(Clone, Copy, Default)]
struct SliceHeader {
    first_mb: u32,
    idr: bool,
    field: Option<u32>,
    colour_plane: Option<u32>,
    frame_num: u32,
    pps_id: u32,
    redundant_pic_cnt: Option<u32>,
    slice_type: u32,
    bottom_field_present: bool,
}

fn slice_header(writer: &mut BitWriter, sequence: Sequence, options: SliceHeader) {
    writer.ue(options.first_mb);
    writer.ue(options.slice_type);
    writer.ue(options.pps_id);
    if sequence.separate_colour_plane {
        writer.bits(options.colour_plane.unwrap_or(0), 2);
    }
    writer.bits(options.frame_num, 4);
    if !sequence.frame_mbs_only {
        if let Some(field) = options.field {
            writer.bits(1, 1);
            writer.bits(field, 1);
        } else {
            writer.bits(0, 1);
        }
    }
    if options.idr {
        writer.ue(0);
    }
    if sequence.poc_type == 0 {
        writer.bits(0, 6);
        if options.bottom_field_present {
            writer.se(0);
        }
    }
    if let Some(redundant) = options.redundant_pic_cnt {
        writer.ue(redundant);
    }
    writer.bits(0, if options.idr { 2 } else { 1 });
    writer.se(0);
    writer.ue(1);
}

fn pcm_macroblocks(writer: &mut BitWriter, sequence: Sequence, count: usize, field_mbaff: bool) {
    for macroblock in 0..count {
        if field_mbaff && macroblock % 2 == 0 {
            writer.bits(((macroblock / 2) % 2) as u32, 1);
        }
        writer.ue(25);
        writer.align();
        let planes = if sequence.separate_colour_plane { 1 } else { 3 };
        for plane in 0..planes {
            let mut sample_count = 256;
            let mut depth = sequence.bit_depth_luma;
            if !sequence.separate_colour_plane && plane != 0 {
                sample_count = match sequence.chroma_format_idc {
                    1 => 64,
                    2 => 128,
                    3 => 256,
                    _ => unreachable!(),
                };
                depth = sequence.bit_depth_chroma;
            }
            let value = ((macroblock * 11 + 32 + plane * 47) as u32) & ((1 << depth) - 1);
            if depth == 8 {
                writer
                    .data
                    .extend(std::iter::repeat_n(value as u8, sample_count));
                writer.bit_count += sample_count * 8;
            } else {
                for _ in 0..sample_count {
                    writer.bits(value, depth.into());
                }
            }
        }
    }
}

fn idr_picture(
    sequence: Sequence,
    options: SliceHeader,
    macroblock_count: Option<usize>,
) -> Vec<u8> {
    let mut writer = BitWriter::default();
    slice_header(&mut writer, sequence, options);
    let count = macroblock_count.unwrap_or_else(|| {
        let mut count = sequence.width_mbs * sequence.height_mbs;
        if options.field.is_some() {
            count /= 2;
        }
        count
    });
    pcm_macroblocks(
        &mut writer,
        sequence,
        count,
        !sequence.frame_mbs_only && sequence.mb_adaptive_frame_field,
    );
    writer.nal(if options.idr { 0x65 } else { 0x21 })
}

fn aud(primary_picture_type: u8) -> Vec<u8> {
    let mut writer = BitWriter::default();
    writer.bits((primary_picture_type << 5).into(), 8);
    writer.nal(0x09)
}

fn annex_b(nals: &[Vec<u8>]) -> Vec<u8> {
    let mut output = Vec::new();
    for nal in nals {
        output.extend_from_slice(&[0, 0, 0, 1]);
        output.extend_from_slice(nal);
    }
    output
}

fn partition_nals(mb: u32) -> [Vec<u8>; 3] {
    let mut a = BitWriter::default();
    a.ue(mb);
    a.ue(2);
    a.ue(0);
    a.bits(1, 4);
    a.bits(0, 1);
    a.se(0);
    a.ue(1);
    a.ue(mb);
    a.ue(25);

    let mut b = BitWriter::default();
    b.ue(mb);
    b.align();
    for value in [112 + mb, 144 + mb, 160 + mb] {
        let count = if value == 112 + mb { 256 } else { 64 };
        b.data.extend(std::iter::repeat_n(value as u8, count));
        b.bit_count += count * 8;
    }

    let mut writer = BitWriter::default();
    writer.ue(mb);
    [a.nal(0x42), b.nal(0x43), writer.nal(0x44)]
}

fn switching_slice(sp: bool) -> Vec<u8> {
    let mut writer = BitWriter::default();
    writer.ue(0);
    writer.ue(if sp { 3 } else { 4 });
    writer.ue(0);
    writer.bits(if sp { 1 } else { 2 }, 4);
    if sp {
        writer.bits(0, 2);
    }
    writer.bits(0, 1);
    writer.se(0);
    if sp {
        writer.bits(0, 1);
    }
    writer.se(0);
    writer.ue(1);
    if sp {
        writer.ue(16);
    } else {
        for _ in 0..16 {
            writer.ue(26);
            writer.align();
            writer.data.extend(std::iter::repeat_n(128, 384));
            writer.bit_count += 384 * 8;
        }
    }
    writer.nal(0x21)
}

fn extension_nal(family: usize) -> Vec<u8> {
    let mut output = Vec::new();
    if family == 0 {
        output.extend_from_slice(&[0, 0, 0, 1, 0x6e, 0x80]);
    }
    output.extend_from_slice(&[0, 0, 0, 1]);
    output.extend_from_slice(match family {
        0 => &[0x74, 0x80, 0, 0, 0x80],
        1 => &[0x74, 0, 0, 0, 0x80],
        2 => &[0x75, 0x80, 0, 0, 0x80],
        _ => &[0x75, 0xc0, 0, 0, 0x80],
    });
    output
}

pub fn generate_h264_companion_corpus() -> Vec<H264Companion> {
    let base = Sequence::new(100);
    let chroma422 = Sequence {
        profile_idc: 122,
        chroma_format_idc: 2,
        ..base
    };
    let chroma444 = Sequence {
        profile_idc: 244,
        chroma_format_idc: 3,
        ..base
    };
    let separate444 = Sequence {
        separate_colour_plane: true,
        ..chroma444
    };
    let bit10 = Sequence {
        profile_idc: 110,
        bit_depth_luma: 10,
        bit_depth_chroma: 10,
        ..base
    };
    let bit12 = Sequence {
        bit_depth_luma: 12,
        bit_depth_chroma: 12,
        ..chroma444
    };
    let fields = Sequence {
        frame_mbs_only: false,
        ..base
    };
    let mbaff = Sequence {
        frame_mbs_only: false,
        mb_adaptive_frame_field: true,
        ..base
    };
    let idr = SliceHeader {
        idr: true,
        slice_type: 2,
        ..Default::default()
    };
    let extension_base = annex_b(&[
        sps(base),
        pps(0, false, false, 0, 0),
        idr_picture(base, idr, None),
    ]);
    let mut data = vec![
        annex_b(&[
            sps(chroma422),
            pps(0, false, false, 0, 0),
            idr_picture(chroma422, idr, None),
        ]),
        annex_b(&[
            sps(chroma444),
            pps(0, false, true, 0, 0),
            idr_picture(chroma444, idr, None),
        ]),
        annex_b(&[
            sps(separate444),
            pps(0, false, true, 0, 0),
            idr_picture(
                separate444,
                SliceHeader {
                    colour_plane: Some(0),
                    ..idr
                },
                None,
            ),
            idr_picture(
                separate444,
                SliceHeader {
                    colour_plane: Some(1),
                    ..idr
                },
                None,
            ),
            idr_picture(
                separate444,
                SliceHeader {
                    colour_plane: Some(2),
                    ..idr
                },
                None,
            ),
        ]),
        annex_b(&[
            sps(bit10),
            pps(0, false, false, 0, 0),
            idr_picture(bit10, idr, None),
        ]),
        annex_b(&[
            sps(bit12),
            pps(0, false, false, 0, 0),
            idr_picture(bit12, idr, None),
        ]),
        annex_b(&[
            sps(fields),
            pps(0, false, false, 0, 0),
            idr_picture(
                fields,
                SliceHeader {
                    field: Some(0),
                    ..idr
                },
                None,
            ),
            idr_picture(
                fields,
                SliceHeader {
                    idr: false,
                    field: Some(1),
                    frame_num: 1,
                    ..idr
                },
                None,
            ),
        ]),
        annex_b(&[
            sps(mbaff),
            pps(0, false, false, 0, 0),
            idr_picture(mbaff, idr, None),
        ]),
        annex_b(&[
            sps(base),
            pps(0, false, false, 1, 0),
            idr_picture(base, idr, Some(8)),
            idr_picture(base, SliceHeader { first_mb: 1, ..idr }, Some(8)),
        ]),
        annex_b(&[
            sps(base),
            pps(0, true, false, 0, 0),
            aud(0),
            idr_picture(
                base,
                SliceHeader {
                    redundant_pic_cnt: Some(0),
                    ..idr
                },
                None,
            ),
            aud(0),
            idr_picture(
                base,
                SliceHeader {
                    idr: false,
                    frame_num: 1,
                    redundant_pic_cnt: Some(0),
                    ..idr
                },
                None,
            ),
            aud(0),
            idr_picture(
                base,
                SliceHeader {
                    idr: false,
                    frame_num: 1,
                    redundant_pic_cnt: Some(1),
                    ..idr
                },
                None,
            ),
        ]),
        {
            let mut nals = vec![
                sps(base),
                pps(0, false, false, 0, 0),
                aud(0),
                idr_picture(base, idr, None),
                aud(0),
            ];
            for mb in 0..16 {
                nals.extend(partition_nals(mb));
            }
            annex_b(&nals)
        },
        {
            let extended = Sequence::new(88);
            annex_b(&[
                sps(extended),
                pps(0, false, false, 0, 0),
                aud(0),
                idr_picture(extended, idr, None),
                aud(0),
                switching_slice(true),
                aud(0),
                switching_slice(false),
            ])
        },
    ];
    for family in 0..4 {
        let mut stream = extension_base.clone();
        stream.extend(extension_nal(family));
        data.push(stream);
    }
    H264_COMPANION_SUFFIXES
        .into_iter()
        .zip(data)
        .map(|(suffix, data)| H264Companion { suffix, data })
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;

    fn nals(stream: &[u8]) -> Vec<&[u8]> {
        let mut result = Vec::new();
        let mut cursor = 0;
        while cursor + 4 <= stream.len() {
            if stream[cursor..cursor + 4] != [0, 0, 0, 1] {
                cursor += 1;
                continue;
            }
            let start = cursor + 4;
            let end = (start + 1..stream.len())
                .find(|index| stream[*index..].starts_with(&[0, 0, 0, 1]))
                .unwrap_or(stream.len());
            result.push(&stream[start..end]);
            cursor = end;
        }
        result
    }

    #[test]
    fn companion_corpus_has_canonical_keys_bytes_and_structure() {
        let corpus = generate_h264_companion_corpus();
        let expected = [
            8252, 12349, 12429, 7741, 18494, 6212, 6204, 6214, 18592, 12730, 12417, 6219, 6213,
            6213, 6213,
        ];
        assert_eq!(corpus.len(), 15);
        for ((entry, suffix), size) in corpus.iter().zip(H264_COMPANION_SUFFIXES).zip(expected) {
            assert_eq!(entry.suffix, suffix);
            assert_eq!(entry.data.len(), size, "{suffix}");
            assert_eq!(entry.data[..4], [0, 0, 0, 1], "{suffix}");
            assert!(entry.data[4..]
                .windows(4)
                .any(|bytes| bytes == [0, 0, 0, 1]));
        }
        assert_eq!(corpus, generate_h264_companion_corpus());
        for entry in &corpus[..11] {
            let units = nals(&entry.data);
            let sps = crate::parse_sps_syntax(units[0]).unwrap();
            let pps = crate::parse_pps_syntax(units[1]).unwrap();
            assert_eq!(pps.sequence_parameter_set_id, sps.sps_id);
            assert!(units.iter().skip(2).any(|nal| matches!(nal[0] & 31, 1..=5)));
        }
        assert_eq!(
            nals(&corpus[9].data)
                .iter()
                .filter(|nal| matches!(nal[0] & 31, 2..=4))
                .count(),
            48
        );
        assert_eq!(nals(&corpus[11].data).last().unwrap()[0] & 31, 20);
        assert_eq!(nals(&corpus[12].data).last().unwrap()[1] & 0x80, 0);
        assert_ne!(nals(&corpus[13].data).last().unwrap()[1] & 0x80, 0);
        assert_eq!(nals(&corpus[14].data).last().unwrap()[1] & 0xe0, 0xc0);
    }
}
