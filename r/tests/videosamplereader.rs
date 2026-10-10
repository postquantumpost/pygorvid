use std::fs;
use std::path::PathBuf;
use std::sync::atomic::{AtomicUsize, Ordering};

use pygorvid::VideoSampleReader;

static NEXT_DIR: AtomicUsize = AtomicUsize::new(0);

#[derive(Default)]
struct CorpusFileReference {
    packet_count: usize,
    parameter_sets_hex: String,
    packets: Vec<CorpusPacketReference>,
}

struct CorpusPacketReference {
    index: usize,
    offset: u64,
    size: usize,
    dts: i64,
    pts: i64,
    duration: u32,
    is_sync: bool,
}

fn mkbox(kind: &[u8; 4], payload: &[u8]) -> Vec<u8> {
    let mut bytes = ((payload.len() + 8) as u32).to_be_bytes().to_vec();
    bytes.extend_from_slice(kind);
    bytes.extend_from_slice(payload);
    bytes
}

fn avcc() -> Vec<u8> {
    let mut payload = vec![1, 100, 0, 42, 0xff, 0xe1];
    payload.extend_from_slice(&4u16.to_be_bytes());
    payload.extend_from_slice(&[0x67, 100, 0, 42]);
    payload.push(1);
    payload.extend_from_slice(&2u16.to_be_bytes());
    payload.extend_from_slice(&[0x68, 0xee]);
    payload
}

fn make_mp4(
    use_co64: bool,
    constant_sample_size: bool,
    configuration: &[u8],
) -> (Vec<u8>, Vec<Vec<u8>>) {
    let mut samples = vec![
        vec![0, 0, 0, 2, 0x65, 0x80],
        vec![0, 0, 0, 3, 0x41, 0x80, 0x80],
        vec![0, 0, 0, 4, 0x41, 0x80, 0x80, 0x80],
    ];
    if constant_sample_size {
        samples = vec![vec![0, 0, 0, 2, 0x65, 0x80]; 3];
    }

    let tkhd = vec![0u8; 84];
    let mut mdhd = vec![0u8; 20];
    mdhd[12..16].copy_from_slice(&1000u32.to_be_bytes());
    let mut hdlr = vec![0u8; 8];
    hdlr.extend_from_slice(b"vide");
    hdlr.extend_from_slice(&[0u8; 13]);

    let mut avc1_payload = vec![0u8; 78];
    avc1_payload[24..26].copy_from_slice(&16u16.to_be_bytes());
    avc1_payload[26..28].copy_from_slice(&16u16.to_be_bytes());
    avc1_payload.extend(mkbox(b"avcC", configuration));
    let avc1 = mkbox(b"avc1", &avc1_payload);
    let mut stsd_payload = vec![0u8; 4];
    stsd_payload.extend_from_slice(&1u32.to_be_bytes());
    stsd_payload.extend(avc1);
    let stsd = mkbox(b"stsd", &stsd_payload);

    let mut stsz_payload = vec![0u8; 4];
    if constant_sample_size {
        stsz_payload.extend_from_slice(&(samples[0].len() as u32).to_be_bytes());
        stsz_payload.extend_from_slice(&(samples.len() as u32).to_be_bytes());
    } else {
        stsz_payload.extend_from_slice(&0u32.to_be_bytes());
        stsz_payload.extend_from_slice(&(samples.len() as u32).to_be_bytes());
        for sample in &samples {
            stsz_payload.extend_from_slice(&(sample.len() as u32).to_be_bytes());
        }
    }
    let stsz = mkbox(b"stsz", &stsz_payload);

    let mut stsc_payload = vec![0u8; 4];
    stsc_payload.extend_from_slice(&2u32.to_be_bytes());
    for entry in [(1u32, 2u32, 1u32), (2, 1, 1)] {
        for value in [entry.0, entry.1, entry.2] {
            stsc_payload.extend_from_slice(&value.to_be_bytes());
        }
    }
    let stsc = mkbox(b"stsc", &stsc_payload);

    let mut stts_payload = vec![0u8; 4];
    stts_payload.extend_from_slice(&1u32.to_be_bytes());
    stts_payload.extend_from_slice(&3u32.to_be_bytes());
    stts_payload.extend_from_slice(&1000u32.to_be_bytes());
    let stts = mkbox(b"stts", &stts_payload);

    let mut ctts_payload = vec![1, 0, 0, 0];
    ctts_payload.extend_from_slice(&3u32.to_be_bytes());
    for (count, offset) in [(1u32, 0i32), (1, -500), (1, 1000)] {
        ctts_payload.extend_from_slice(&count.to_be_bytes());
        ctts_payload.extend_from_slice(&(offset as u32).to_be_bytes());
    }
    let ctts = mkbox(b"ctts", &ctts_payload);

    let mut stss_payload = vec![0u8; 4];
    stss_payload.extend_from_slice(&2u32.to_be_bytes());
    stss_payload.extend_from_slice(&1u32.to_be_bytes());
    stss_payload.extend_from_slice(&3u32.to_be_bytes());
    let stss = mkbox(b"stss", &stss_payload);

    let make_moov = |chunk_offsets: [u64; 2]| {
        let mut offset_payload = vec![0u8; 4];
        offset_payload.extend_from_slice(&2u32.to_be_bytes());
        if use_co64 {
            offset_payload.extend_from_slice(&chunk_offsets[0].to_be_bytes());
            offset_payload.extend_from_slice(&chunk_offsets[1].to_be_bytes());
        } else {
            offset_payload.extend_from_slice(&(chunk_offsets[0] as u32).to_be_bytes());
            offset_payload.extend_from_slice(&(chunk_offsets[1] as u32).to_be_bytes());
        }
        let offset_box = if use_co64 {
            mkbox(b"co64", &offset_payload)
        } else {
            mkbox(b"stco", &offset_payload)
        };
        let mut stbl_payload = Vec::new();
        for child in [&stsd, &stsz, &stsc, &stts, &ctts, &stss, &offset_box] {
            stbl_payload.extend_from_slice(child);
        }
        let stbl = mkbox(b"stbl", &stbl_payload);
        let minf = mkbox(b"minf", &stbl);
        let mut mdia_payload = mkbox(b"mdhd", &mdhd);
        mdia_payload.extend(mkbox(b"hdlr", &hdlr));
        mdia_payload.extend(minf);
        let mdia = mkbox(b"mdia", &mdia_payload);
        let mut trak_payload = mkbox(b"tkhd", &tkhd);
        trak_payload.extend(mdia);
        let trak = mkbox(b"trak", &trak_payload);
        mkbox(b"moov", &trak)
    };

    let ftyp = mkbox(b"ftyp", b"isom\0\0\0\0isom");
    let placeholder = make_moov([0, 0]);
    let mdat_start = (ftyp.len() + placeholder.len() + 8) as u64;
    let second_chunk = mdat_start + samples[0].len() as u64 + samples[1].len() as u64;
    let moov = make_moov([mdat_start, second_chunk]);
    let media = samples.concat();
    let mut file = ftyp;
    file.extend(moov);
    file.extend(mkbox(b"mdat", &media));
    (file, samples)
}

fn write_fixture(name: &str, bytes: &[u8]) -> (PathBuf, PathBuf) {
    let workspace = PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("..");
    let root = workspace.join("tmp");
    fs::create_dir_all(&root).unwrap();
    let dir = root.join(format!(
        "silkroad-reader-{}-{}-{}",
        name,
        std::process::id(),
        NEXT_DIR.fetch_add(1, Ordering::Relaxed)
    ));
    fs::create_dir(&dir).unwrap();
    let path = dir.join("fixture.mp4");
    fs::write(&path, bytes).unwrap();
    (dir, path)
}

#[test]
fn reads_samples_with_stco_and_variable_sizes() {
    assert_reader_case(false, false);
}

#[test]
fn reads_samples_with_co64_and_constant_size() {
    assert_reader_case(true, true);
}

#[test]
fn reads_compact_project_fixtures() {
    let fixture_dir = PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("../testdata/h264");
    for (name, profile, level) in [("high42-1080p.mp4", 100, 42), ("high52-2160p.mp4", 100, 52)] {
        let mut reader = VideoSampleReader::open(fixture_dir.join(name)).unwrap();
        let config = reader.configuration();
        assert_eq!(config.profile, profile, "{name}");
        assert_eq!(config.level, level, "{name}");
        assert_eq!(config.nal_length_size, 4, "{name}");
        assert_eq!(config.sequence_parameter_sets.len(), 1, "{name}");
        assert_eq!(config.picture_parameter_sets.len(), 1, "{name}");
        assert_eq!(reader.sample_count(), 48, "{name}");
        let samples: Vec<_> = std::iter::from_fn(|| reader.next_sample().unwrap()).collect();
        assert_eq!(samples.len(), 48, "{name}");
        assert!(
            samples.iter().all(|sample| !sample.data.is_empty()),
            "{name}"
        );
        assert!(
            samples.iter().all(|sample| sample.duration_ticks > 0),
            "{name}"
        );
        assert_eq!(samples[0].dts_ticks, 0, "{name}");
        assert!(
            samples
                .windows(2)
                .all(|pair| pair[1].dts_ticks >= pair[0].dts_ticks),
            "{name}"
        );
    }
}

#[test]
fn resolves_decode_order_dependencies_for_presentation_index() {
    let fixture = PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .join("..")
        .join("testdata")
        .join("h264")
        .join("high42-1080p.mp4");
    let mut reader = VideoSampleReader::open(fixture).unwrap();

    let dependencies = reader.decode_order_dependency_samples(1).unwrap();
    assert_eq!(
        dependencies
            .iter()
            .map(|sample| (sample.index, sample.dts_ticks, sample.pts_ticks))
            .collect::<Vec<_>>(),
        [(0, 0, 256), (1, 256, 1024), (2, 512, 512)]
    );
    assert_eq!(reader.next_sample().unwrap().unwrap().index, 0);
    assert_eq!(
        reader
            .decode_order_dependency_samples(3)
            .unwrap()
            .iter()
            .map(|sample| sample.index)
            .collect::<Vec<_>>(),
        [0, 1, 2, 3]
    );
    assert!(reader
        .decode_order_dependency_samples(reader.sample_count())
        .is_err());
}

fn assert_reader_case(use_co64: bool, constant_sample_size: bool) {
    let (bytes, expected_samples) = make_mp4(use_co64, constant_sample_size, &avcc());
    let (dir, path) = write_fixture("samples", &bytes);
    let mut reader = VideoSampleReader::open(&path).unwrap();
    let config = reader.configuration();
    assert_eq!(config.profile, 100);
    assert_eq!(config.level, 42);
    assert_eq!(config.nal_length_size, 4);
    assert_eq!(config.sequence_parameter_sets.len(), 1);
    assert_eq!(config.picture_parameter_sets.len(), 1);
    assert_eq!(reader.sample_count(), 3);

    let samples: Vec<_> = std::iter::from_fn(|| reader.next_sample().unwrap()).collect();
    assert_eq!(samples.len(), 3);
    assert_eq!(
        samples
            .iter()
            .map(|sample| sample.data.clone())
            .collect::<Vec<_>>(),
        expected_samples
    );
    assert_eq!(
        samples
            .iter()
            .map(|sample| sample.dts_ticks)
            .collect::<Vec<_>>(),
        vec![0, 1000, 2000]
    );
    assert_eq!(
        samples
            .iter()
            .map(|sample| sample.pts_ticks)
            .collect::<Vec<_>>(),
        vec![0, 500, 3000]
    );
    assert_eq!(
        samples
            .iter()
            .map(|sample| sample.duration_ticks)
            .collect::<Vec<_>>(),
        vec![1000, 1000, 1000]
    );
    assert_eq!(
        samples
            .iter()
            .map(|sample| sample.is_sync)
            .collect::<Vec<_>>(),
        vec![true, false, true]
    );
    assert!(reader.next_sample().unwrap().is_none());
    reader.close();
    reader.close();
    assert!(reader
        .next_sample()
        .unwrap_err()
        .to_string()
        .contains("closed"));
    fs::remove_dir_all(dir).unwrap();
}

#[test]
fn rejects_invalid_avc_configuration_records() {
    let base = avcc();
    let mut invalid_length_size = base.clone();
    invalid_length_size[4] = 0xfe;
    let mut invalid_reserved_bits = base.clone();
    invalid_reserved_bits[4] = 0x7f;
    let mut wrong_sps_type = base.clone();
    wrong_sps_type[8] = 0x68;
    let mut wrong_pps_type = base.clone();
    wrong_pps_type[15] = 0x67;
    let mut truncated_pps = base.clone();
    truncated_pps.pop();
    let mut trailing_data = base;
    trailing_data.push(0);

    for (name, configuration) in [
        ("unsupported-length-size", invalid_length_size),
        ("invalid-reserved-bits", invalid_reserved_bits),
        ("wrong-sps-type", wrong_sps_type),
        ("wrong-pps-type", wrong_pps_type),
        ("truncated-pps", truncated_pps),
        ("trailing-data", trailing_data),
    ] {
        let (bytes, _) = make_mp4(false, false, &configuration);
        let (dir, path) = write_fixture(name, &bytes);
        assert!(
            VideoSampleReader::open(&path).is_err(),
            "accepted invalid configuration: {name}"
        );
        fs::remove_dir_all(dir).unwrap();
    }
}

#[test]
fn rejects_truncated_nal_unit_without_advancing_reader() {
    let (mut bytes, _) = make_mp4(false, false, &avcc());
    let first_sample = [0, 0, 0, 2, 0x65, 0x80];
    let offset = bytes
        .windows(first_sample.len())
        .position(|window| window == first_sample)
        .expect("first sample exists in fixture");
    bytes[offset + 3] = 3;
    let (dir, path) = write_fixture("truncated-nal", &bytes);
    let mut reader = VideoSampleReader::open(&path).unwrap();

    let error = reader.next_sample().unwrap_err();
    assert_eq!(error.kind(), std::io::ErrorKind::InvalidData);
    assert!(error
        .to_string()
        .contains("sample 0: NAL unit is truncated"));
    assert!(reader
        .next_sample()
        .unwrap_err()
        .to_string()
        .contains("sample 0"));

    drop(reader);
    fs::remove_dir_all(dir).unwrap();
}

#[test]
fn reports_invalid_sample_tables_precisely() {
    for (name, box_type, relative_offset, value, expected) in [
        (
            "malformed-timing-table",
            b"stts",
            12,
            2u32,
            "timing entries do not cover all samples",
        ),
        (
            "invalid-media-offset",
            b"stco",
            12,
            0,
            "outside every mdat payload",
        ),
        (
            "unsupported-sample-description",
            b"stsc",
            20,
            2,
            "unsupported sample description",
        ),
    ] {
        let (mut bytes, _) = make_mp4(false, false, &avcc());
        let marker = bytes
            .windows(box_type.len())
            .position(|window| window == box_type)
            .expect("sample table box exists");
        bytes[marker + relative_offset..marker + relative_offset + 4]
            .copy_from_slice(&value.to_be_bytes());
        let (dir, path) = write_fixture(name, &bytes);
        let error = match VideoSampleReader::open(&path) {
            Ok(_) => panic!("accepted invalid sample table: {name}"),
            Err(error) => error,
        };
        assert!(error.to_string().contains(expected), "{name}: {error}");
        fs::remove_dir_all(dir).unwrap();
    }
}

#[test]
fn matches_active_corpus_demux_vectors() {
    let repository_root = PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("..");
    let reference_text = fs::read_to_string(repository_root.join("silkroad1-demux.tsv")).unwrap();
    let mut references = std::collections::HashMap::<String, CorpusFileReference>::new();
    for line in reference_text
        .lines()
        .filter(|line| !line.is_empty() && !line.starts_with('#'))
    {
        let fields: Vec<_> = line.split('\t').collect();
        match fields[0] {
            "S" => {
                assert_eq!(fields.len(), 4, "invalid sample reference row: {line}");
                let previous = references.insert(
                    fields[1].to_string(),
                    CorpusFileReference {
                        packet_count: fields[2].parse().unwrap(),
                        parameter_sets_hex: fields[3].to_string(),
                        packets: Vec::new(),
                    },
                );
                assert!(previous.is_none(), "duplicate sample reference: {line}");
            }
            "P" => {
                assert_eq!(fields.len(), 9, "invalid packet reference row: {line}");
                let reference = references
                    .get_mut(fields[1])
                    .unwrap_or_else(|| panic!("packet row precedes sample row: {line}"));
                reference.packets.push(CorpusPacketReference {
                    index: fields[2].parse().unwrap(),
                    offset: fields[3].parse().unwrap(),
                    size: fields[4].parse().unwrap(),
                    dts: fields[5].parse().unwrap(),
                    pts: fields[6].parse().unwrap(),
                    duration: fields[7].parse().unwrap(),
                    is_sync: match fields[8] {
                        "0" => false,
                        "1" => true,
                        _ => panic!("invalid sync flag in packet row: {line}"),
                    },
                });
            }
            _ => panic!("unknown demux reference row: {line}"),
        }
    }

    let manifest = fs::read_to_string(repository_root.join("silkroad1.sha256")).unwrap();
    let active_paths: std::collections::HashSet<_> = manifest
        .lines()
        .filter_map(|line| line.split_once("  ").map(|(_, path)| path.to_string()))
        .collect();
    let referenced_paths: std::collections::HashSet<_> = references.keys().cloned().collect();
    assert_eq!(referenced_paths, active_paths);

    for (relative_path, expected) in references {
        let path = repository_root.join(&relative_path);
        let source = fs::read(&path).unwrap();
        let mut reader = VideoSampleReader::open(&path).unwrap();
        assert_eq!(
            reader.sample_count(),
            expected.packet_count,
            "{relative_path}"
        );
        assert_eq!(
            expected.packets.len(),
            expected.packet_count,
            "{relative_path}"
        );

        let configuration = reader.configuration();
        let mut parameter_sets = Vec::new();
        for sets in [
            &configuration.sequence_parameter_sets,
            &configuration.picture_parameter_sets,
        ] {
            for parameter_set in sets {
                parameter_sets.extend_from_slice(&(parameter_set.len() as u32).to_be_bytes());
                parameter_sets.extend_from_slice(parameter_set);
            }
        }
        let actual_parameter_sets_hex: String = parameter_sets
            .iter()
            .map(|byte| format!("{byte:02x}"))
            .collect();
        assert_eq!(
            actual_parameter_sets_hex, expected.parameter_sets_hex,
            "{relative_path}"
        );

        for packet in expected.packets {
            let sample = reader
                .next_sample()
                .unwrap()
                .unwrap_or_else(|| panic!("missing sample {} in {relative_path}", packet.index));
            assert_eq!(sample.index as usize, packet.index, "{relative_path}");
            assert_eq!(
                sample.dts_ticks, packet.dts,
                "{relative_path} sample {}",
                packet.index
            );
            assert_eq!(
                sample.pts_ticks, packet.pts,
                "{relative_path} sample {}",
                packet.index
            );
            assert_eq!(
                sample.duration_ticks, packet.duration,
                "{relative_path} sample {}",
                packet.index
            );
            assert_eq!(
                sample.is_sync, packet.is_sync,
                "{relative_path} sample {}",
                packet.index
            );
            assert_eq!(
                sample.data.len(),
                packet.size,
                "{relative_path} sample {}",
                packet.index
            );
            let start = usize::try_from(packet.offset).unwrap();
            let end = start
                .checked_add(packet.size)
                .expect("packet range overflow");
            assert!(
                end <= source.len(),
                "{relative_path} sample {} range exceeds file",
                packet.index
            );
            assert_eq!(
                sample.data,
                source[start..end],
                "{relative_path} sample {} bytes",
                packet.index
            );
        }
        assert!(reader.next_sample().unwrap().is_none(), "{relative_path}");
    }
}
