//! Deterministic Annex-B corpora for H.264 profile-extension feature tests.

use crate::mp4writer::{full_box, mp4_box, track, u16s, u32s};

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct H264Companion {
    pub suffix: &'static str,
    pub data: Vec<u8>,
}

pub const H264_COMPANION_SUFFIXES: [&str; 15] = [
    "frame-40-422.mp4",
    "frame-41-444.mp4",
    "frame-41-separate.mp4",
    "frame-42-10bit.mp4",
    "frame-42-12bit.mp4",
    "frame-43-fields.mp4",
    "frame-44-mbaff.mp4",
    "frame-45-fmo.mp4",
    "frame-46-redundant.mp4",
    "frame-47-partitions.mp4",
    "frame-48-sp-si.mp4",
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

fn annex_b_nals(data: &[u8]) -> Vec<&[u8]> {
    let starts: Vec<_> = (0..data.len().saturating_sub(3))
        .filter(|&index| data[index..index + 4] == [0, 0, 0, 1])
        .collect();
    starts
        .iter()
        .enumerate()
        .map(|(index, start)| {
            &data[start + 4..starts.get(index + 1).copied().unwrap_or(data.len())]
        })
        .collect()
}

fn mux_avc_mp4(annex: &[u8], sample_count: usize) -> Vec<u8> {
    let nals = annex_b_nals(annex);
    let sequence_sets: Vec<_> = nals
        .iter()
        .copied()
        .filter(|nal| nal[0] & 31 == 7)
        .collect();
    let picture_sets: Vec<_> = nals
        .iter()
        .copied()
        .filter(|nal| nal[0] & 31 == 8)
        .collect();
    let picture_nals: Vec<_> = nals
        .iter()
        .copied()
        .filter(|nal| !matches!(nal[0] & 31, 7 | 8))
        .collect();
    assert_eq!(sequence_sets.len(), 1, "companion must contain one SPS");
    assert_eq!(picture_sets.len(), 1, "companion must contain one PPS");

    let mut grouped = vec![Vec::new(); sample_count];
    if sample_count == 1 {
        grouped[0] = picture_nals;
    } else {
        let mut aud_groups: Vec<Vec<&[u8]>> = Vec::new();
        let mut current = Vec::new();
        for nal in picture_nals {
            if nal[0] & 31 == 9 && !current.is_empty() {
                aud_groups.push(current);
                current = Vec::new();
            }
            current.push(nal);
        }
        if !current.is_empty() {
            aud_groups.push(current);
        }
        if aud_groups.len() == sample_count {
            grouped = aud_groups;
        } else {
            let slices: Vec<_> = nals
                .iter()
                .copied()
                .filter(|nal| matches!(nal[0] & 31, 1 | 5))
                .collect();
            assert_eq!(
                slices.len(),
                sample_count,
                "companion sample grouping is ambiguous"
            );
            grouped = slices.into_iter().map(|nal| vec![nal]).collect();
        }
    }

    let samples: Vec<_> = grouped
        .iter()
        .map(|group| {
            let mut sample = Vec::new();
            for nal in group {
                sample.extend(u32s(&[nal.len() as u32]));
                sample.extend_from_slice(nal);
            }
            sample
        })
        .collect();
    assert_eq!(samples.len(), sample_count);
    let sps = sequence_sets[0];
    let pps = picture_sets[0];
    let avcc = mp4_box(
        b"avcC",
        &[
            &[1, sps[1], sps[2], sps[3], 0xff, 0xe1],
            &u16s(&[sps.len() as u16]),
            sps,
            &[1],
            &u16s(&[pps.len() as u16]),
            pps,
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
    let mut size_values = vec![0, samples.len() as u32];
    size_values.extend(samples.iter().map(|sample| sample.len() as u32));
    let sizes = full_box(b"stsz", 0, &[&u32s(&size_values)]);
    let ftyp = mp4_box(b"ftyp", &[b"isom", &u32s(&[0x200]), b"isomiso2avc1mp41"]);
    let sync_samples: Vec<_> = (1..=sample_count as u32).collect();
    let placeholder = track(
        1,
        &visual,
        &sizes,
        sample_count as u32,
        60,
        sample_count as u32,
        ftyp.len() as u32 + 8,
        true,
        &sync_samples,
        &[],
    );
    let data: Vec<_> = samples.iter().flatten().copied().collect();
    let movie_header = full_box(
        b"mvhd",
        0,
        &[
            &u32s(&[0, 0, 600, sample_count as u32 * 10]),
            &u16s(&[1, 0]),
            &[0; 8],
            &u32s(&[0x00010000, 0x01000000, 0]),
            &[0; 36],
            &u32s(&[0, 0, 0, 0, 0, 0]),
            &u32s(&[2]),
        ],
    );
    let movie = mp4_box(b"moov", &[&movie_header, &placeholder]);
    let video_offset = ftyp.len() as u32 + movie.len() as u32 + 8;
    let final_track = track(
        1,
        &visual,
        &sizes,
        sample_count as u32,
        60,
        sample_count as u32,
        video_offset,
        true,
        &sync_samples,
        &[],
    );
    let movie = mp4_box(b"moov", &[&movie_header, &final_track]);
    let mut result = ftyp;
    result.extend(movie);
    result.extend(mp4_box(b"mdat", &[&data]));
    result
}

fn generate_h264_companion_streams() -> Vec<Vec<u8>> {
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
    data
}

pub fn generate_h264_companion_corpus() -> Vec<H264Companion> {
    let streams = generate_h264_companion_streams();
    let sample_counts = [1, 1, 1, 1, 1, 2, 1, 1, 3, 2, 3];
    let files = streams[..11]
        .iter()
        .zip(sample_counts)
        .map(|(stream, sample_count)| mux_avc_mp4(stream, sample_count))
        .chain(streams[11..].iter().cloned())
        .collect::<Vec<_>>();
    H264_COMPANION_SUFFIXES
        .into_iter()
        .zip(files)
        .map(|(suffix, data)| H264Companion { suffix, data })
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;

    fn sha256(data: &[u8]) -> [u8; 32] {
        const K: [u32; 64] = [
            0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1, 0x923f82a4,
            0xab1c5ed5, 0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3, 0x72be5d74, 0x80deb1fe,
            0x9bdc06a7, 0xc19bf174, 0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc, 0x2de92c6f,
            0x4a7484aa, 0x5cb0a9dc, 0x76f988da, 0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7,
            0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967, 0x27b70a85, 0x2e1b2138, 0x4d2c6dfc,
            0x53380d13, 0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85, 0xa2bfe8a1, 0xa81a664b,
            0xc24b8b70, 0xc76c51a3, 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070, 0x19a4c116,
            0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
            0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208, 0x90befffa, 0xa4506ceb, 0xbef9a3f7,
            0xc67178f2,
        ];
        let mut hash = [
            0x6a09e667u32,
            0xbb67ae85,
            0x3c6ef372,
            0xa54ff53a,
            0x510e527f,
            0x9b05688c,
            0x1f83d9ab,
            0x5be0cd19,
        ];
        let bit_len = (data.len() as u64) * 8;
        let mut padded = data.to_vec();
        padded.push(0x80);
        while padded.len() % 64 != 56 {
            padded.push(0);
        }
        padded.extend(bit_len.to_be_bytes());
        for chunk in padded.chunks_exact(64) {
            let mut words = [0; 64];
            for (index, bytes) in chunk.chunks_exact(4).enumerate() {
                words[index] = u32::from_be_bytes(bytes.try_into().unwrap());
            }
            for index in 16..64 {
                let a = words[index - 15];
                let b = words[index - 2];
                let s0 = a.rotate_right(7) ^ a.rotate_right(18) ^ (a >> 3);
                let s1 = b.rotate_right(17) ^ b.rotate_right(19) ^ (b >> 10);
                words[index] = words[index - 16]
                    .wrapping_add(s0)
                    .wrapping_add(words[index - 7])
                    .wrapping_add(s1);
            }
            let [mut a, mut b, mut c, mut d, mut e, mut f, mut g, mut h] = hash;
            for index in 0..64 {
                let s1 = e.rotate_right(6) ^ e.rotate_right(11) ^ e.rotate_right(25);
                let choice = (e & f) ^ (!e & g);
                let t1 = h
                    .wrapping_add(s1)
                    .wrapping_add(choice)
                    .wrapping_add(K[index])
                    .wrapping_add(words[index]);
                let s0 = a.rotate_right(2) ^ a.rotate_right(13) ^ a.rotate_right(22);
                let majority = (a & b) ^ (a & c) ^ (b & c);
                let t2 = s0.wrapping_add(majority);
                h = g;
                g = f;
                f = e;
                e = d.wrapping_add(t1);
                d = c;
                c = b;
                b = a;
                a = t1.wrapping_add(t2);
            }
            for (word, value) in hash.iter_mut().zip([a, b, c, d, e, f, g, h]) {
                *word = word.wrapping_add(value);
            }
        }
        let mut result = [0; 32];
        for (chunk, word) in result.chunks_exact_mut(4).zip(hash) {
            chunk.copy_from_slice(&word.to_be_bytes());
        }
        result
    }

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
        let expected_hashes = [
            0xe5a6_6d62,
            0x2bee_ee98,
            0x3538_16dc,
            0xc4a1_65a1,
            0xfe92_c204,
            0x74b2_17b8,
            0x2633_954c,
            0x2307_1982,
            0xdc57_0e9b,
            0x55d0_581a,
            0xcc2e_ca15,
        ];
        let expected_samples = [1, 1, 1, 1, 1, 2, 1, 1, 3, 2, 3];
        assert_eq!(corpus.len(), 15);
        for (((entry, suffix), hash), sample_count) in corpus[..11]
            .iter()
            .zip(H264_COMPANION_SUFFIXES)
            .zip(expected_hashes)
            .zip(expected_samples)
        {
            assert_eq!(entry.suffix, suffix);
            assert_eq!(entry.data[4..8], *b"ftyp", "{suffix}");
            assert_eq!(
                u32::from_be_bytes(sha256(&entry.data)[..4].try_into().unwrap()),
                hash,
                "{suffix}"
            );
            let stsz = entry
                .data
                .windows(4)
                .position(|bytes| bytes == b"stsz")
                .unwrap()
                - 4;
            assert_eq!(
                u32::from_be_bytes(entry.data[stsz + 16..stsz + 20].try_into().unwrap()),
                sample_count,
                "{suffix}"
            );
            let avcc = entry
                .data
                .windows(4)
                .position(|bytes| bytes == b"avcC")
                .unwrap();
            let avcc_size =
                u32::from_be_bytes(entry.data[avcc - 4..avcc].try_into().unwrap()) as usize;
            let configuration = &entry.data[avcc + 4..avcc - 4 + avcc_size];
            assert_eq!(configuration[0], 1);
            assert_eq!(configuration[4], 0xff);
            assert_eq!(configuration[5], 0xe1);
            let sps_size = u16::from_be_bytes(configuration[6..8].try_into().unwrap()) as usize;
            assert_eq!(&configuration[8 + sps_size..=8 + sps_size], &[1]);
            let pps_size = u16::from_be_bytes(
                configuration[9 + sps_size..11 + sps_size]
                    .try_into()
                    .unwrap(),
            ) as usize;
            assert_eq!(configuration.len(), 11 + sps_size + pps_size);
            let mdat = entry
                .data
                .windows(4)
                .position(|bytes| bytes == b"mdat")
                .unwrap()
                - 4;
            let sample_sizes = &entry.data[stsz + 20..stsz + 20 + sample_count as usize * 4];
            let mut cursor = mdat + 8;
            for bytes in sample_sizes.chunks_exact(4) {
                let size = u32::from_be_bytes(bytes.try_into().unwrap()) as usize;
                assert!(
                    !crate::split_h264_sample_nals(&entry.data[cursor..cursor + size], 4)
                        .unwrap()
                        .is_empty()
                );
                cursor += size;
            }
            assert!(!entry.data.windows(4).any(|bytes| bytes == b"soun"));
        }
        for (entry, suffix) in corpus[11..].iter().zip(&H264_COMPANION_SUFFIXES[11..]) {
            assert_eq!(&entry.suffix, suffix);
            assert_eq!(entry.data[..4], [0, 0, 0, 1]);
        }
        assert_eq!(corpus, generate_h264_companion_corpus());
        assert_eq!(nals(&corpus[11].data).last().unwrap()[0] & 31, 20);
        assert_eq!(nals(&corpus[12].data).last().unwrap()[1] & 0x80, 0);
        assert_ne!(nals(&corpus[13].data).last().unwrap()[1] & 0x80, 0);
        assert_eq!(nals(&corpus[14].data).last().unwrap()[1] & 0xe0, 0xc0);
    }
}
