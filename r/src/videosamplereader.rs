//! Read compressed AVC samples from the first video track in an MP4.

use std::fs::File;
use std::io::{self, Read, Seek, SeekFrom};
use std::path::Path;

use crate::mp4boxes::{find, read_boxes, AvcConfiguration, Mp4Box};

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct CompressedSample {
    pub index: u64,
    pub data: Vec<u8>,
    pub dts_ticks: i64,
    pub pts_ticks: i64,
    pub duration_ticks: u32,
    pub is_sync: bool,
}

#[derive(Clone, Debug)]
struct SampleLocation {
    offset: u64,
    size: u32,
    dts_ticks: i64,
    pts_ticks: i64,
    duration_ticks: u32,
    is_sync: bool,
}

pub struct VideoSampleReader {
    file: Option<File>,
    configuration: AvcConfiguration,
    samples: Vec<SampleLocation>,
    next_index: usize,
}

impl VideoSampleReader {
    /// Opens `path` and prepares sample locations for its first AVC video track.
    pub fn open<P: AsRef<Path>>(path: P) -> io::Result<Self> {
        let mut file = File::open(path)?;
        let file_size = file.metadata()?.len();
        let boxes = read_boxes(&mut file, 0, file_size)?;
        let track = find(&boxes, &[b"moov", b"trak"])
            .into_iter()
            .find(|track| {
                find(track.children(), &[b"mdia", b"hdlr"])
                    .iter()
                    .any(|handler| handler.handler_type() == Some(*b"vide"))
            })
            .ok_or_else(|| invalid("MP4 contains no video track"))?;
        let track_children = track.children();

        let timescale = one_box(track_children, &[b"mdia", b"mdhd"], "mdhd")?
            .timescale()
            .ok_or_else(|| invalid("mdhd timescale is unavailable"))?;
        if timescale == 0 {
            return Err(invalid("video track has a zero media timescale"));
        }

        let sample_description = one_box(
            track_children,
            &[b"mdia", b"minf", b"stbl", b"stsd"],
            "stsd",
        )?;
        let avc_entries: Vec<&dyn Mp4Box> = sample_description
            .children()
            .iter()
            .map(|entry| &**entry)
            .filter(|entry| &entry.header().kind == b"avc1")
            .collect();
        if avc_entries.len() != 1 {
            return Err(invalid(
                "video track must have exactly one avc1 sample entry",
            ));
        }
        let avc_configuration = avc_entries[0]
            .children()
            .iter()
            .find_map(|box_| box_.avc_configuration().cloned())
            .ok_or_else(|| invalid("avc1 sample entry has no usable avcC configuration"))?;

        let stsz = one_box(
            track_children,
            &[b"mdia", b"minf", b"stbl", b"stsz"],
            "stsz",
        )?
        .sample_size_table()
        .ok_or_else(|| invalid("stsz sample sizes are unavailable"))?;
        let stsc = one_box(
            track_children,
            &[b"mdia", b"minf", b"stbl", b"stsc"],
            "stsc",
        )?
        .sample_to_chunk()
        .ok_or_else(|| invalid("stsc mapping is unavailable"))?;
        let stts = one_box(
            track_children,
            &[b"mdia", b"minf", b"stbl", b"stts"],
            "stts",
        )?
        .sample_timing()
        .ok_or_else(|| invalid("stts timing is unavailable"))?;
        let sample_count = usize::try_from(stsz.1)
            .map_err(|_| invalid("sample count exceeds addressable memory"))?;
        let durations = expand_timing(stts, sample_count, "stts")?;

        let ctts_boxes = find(track_children, &[b"mdia", b"minf", b"stbl", b"ctts"]);
        if ctts_boxes.len() > 1 {
            return Err(invalid("multiple ctts boxes are unsupported"));
        }
        let composition_offsets = match ctts_boxes.first() {
            Some(box_) => {
                let (version, entries) = box_
                    .composition_timing()
                    .ok_or_else(|| invalid("ctts composition offsets are unavailable"))?;
                if version > 1 {
                    return Err(invalid("unsupported ctts version"));
                }
                expand_composition_timing(entries, sample_count)?
            }
            None => vec![0; sample_count],
        };

        let sync_boxes = find(track_children, &[b"mdia", b"minf", b"stbl", b"stss"]);
        if sync_boxes.len() > 1 {
            return Err(invalid("multiple stss boxes are unsupported"));
        }
        let sync_numbers = match sync_boxes.first() {
            Some(box_) => {
                let numbers = box_
                    .sync_samples()
                    .ok_or_else(|| invalid("stss sample list is unavailable"))?;
                if numbers.iter().any(|number| *number > stsz.1) {
                    return Err(invalid("stss sample number exceeds sample count"));
                }
                Some(
                    numbers
                        .iter()
                        .copied()
                        .collect::<std::collections::HashSet<_>>(),
                )
            }
            None => None,
        };

        let mut offset_boxes = find(track_children, &[b"mdia", b"minf", b"stbl", b"stco"]);
        offset_boxes.extend(find(track_children, &[b"mdia", b"minf", b"stbl", b"co64"]));
        if offset_boxes.len() != 1 {
            return Err(invalid("video track must contain one stco or co64 box"));
        }
        let chunk_offsets = offset_boxes[0]
            .chunk_offsets()
            .ok_or_else(|| invalid("chunk offsets are unavailable"))?;
        let media_ranges: Vec<(u64, u64)> = find(&boxes, &[b"mdat"])
            .into_iter()
            .filter_map(|box_| box_.data_range())
            .collect();
        if stsz.1 != 0 && media_ranges.is_empty() {
            return Err(invalid("video samples exist but no mdat box was found"));
        }

        let samples = map_samples(
            stsz,
            stsc,
            chunk_offsets,
            &durations,
            &composition_offsets,
            sync_numbers.as_ref(),
            &media_ranges,
        )?;

        Ok(Self {
            file: Some(file),
            configuration: avc_configuration,
            samples,
            next_index: 0,
        })
    }

    pub fn configuration(&self) -> &AvcConfiguration {
        &self.configuration
    }

    pub fn sample_count(&self) -> usize {
        self.samples.len()
    }

    /// Returns the next sample in decode order, or `None` at end of stream.
    pub fn next_sample(&mut self) -> io::Result<Option<CompressedSample>> {
        let Some(file) = self.file.as_mut() else {
            return Err(io::Error::new(
                io::ErrorKind::NotConnected,
                "sample reader is closed",
            ));
        };
        let Some(location) = self.samples.get(self.next_index) else {
            return Ok(None);
        };
        file.seek(SeekFrom::Start(location.offset))?;
        let mut data = vec![0u8; location.size as usize];
        file.read_exact(&mut data)?;
        validate_length_prefixed_nals(&data)
            .map_err(|error| invalid(format!("sample {}: {error}", self.next_index)))?;
        let sample = CompressedSample {
            index: self.next_index as u64,
            data,
            dts_ticks: location.dts_ticks,
            pts_ticks: location.pts_ticks,
            duration_ticks: location.duration_ticks,
            is_sync: location.is_sync,
        };
        self.next_index += 1;
        Ok(Some(sample))
    }

    pub fn close(&mut self) {
        self.file = None;
    }
}

fn one_box<'a>(
    boxes: &'a [Box<dyn Mp4Box>],
    path: &[&[u8; 4]],
    name: &str,
) -> io::Result<&'a dyn Mp4Box> {
    let matches = find(boxes, path);
    if matches.len() != 1 {
        return Err(invalid(format!("video track must contain one {name} box")));
    }
    Ok(matches[0])
}

fn expand_timing(entries: &[(u32, u32)], sample_count: usize, name: &str) -> io::Result<Vec<i64>> {
    let mut values = Vec::with_capacity(sample_count);
    for (count, value) in entries {
        if *count as usize > sample_count.saturating_sub(values.len()) {
            return Err(invalid(format!(
                "{name} timing entries exceed sample count"
            )));
        }
        values.extend(std::iter::repeat(i64::from(*value)).take(*count as usize));
    }
    if values.len() != sample_count {
        return Err(invalid(format!(
            "{name} timing entries do not cover all samples"
        )));
    }
    Ok(values)
}

fn expand_composition_timing(entries: &[(u32, i64)], sample_count: usize) -> io::Result<Vec<i64>> {
    let mut values = Vec::with_capacity(sample_count);
    for (count, offset) in entries {
        if *count as usize > sample_count.saturating_sub(values.len()) {
            return Err(invalid("ctts timing entries exceed sample count"));
        }
        values.extend(std::iter::repeat(*offset).take(*count as usize));
    }
    if values.len() != sample_count {
        return Err(invalid("ctts timing entries do not cover all samples"));
    }
    Ok(values)
}

fn map_samples(
    sample_sizes: (u32, u32, &[u32]),
    chunk_map: &[(u32, u32, u32)],
    chunk_offsets: &[u64],
    durations: &[i64],
    composition_offsets: &[i64],
    sync_numbers: Option<&std::collections::HashSet<u32>>,
    media_ranges: &[(u64, u64)],
) -> io::Result<Vec<SampleLocation>> {
    let (constant_size, sample_count, sizes) = sample_sizes;
    let sample_count = sample_count as usize;
    if sample_count == 0 {
        return Ok(Vec::new());
    }
    if chunk_map.is_empty() || chunk_offsets.is_empty() {
        return Err(invalid("sample table lacks chunk mapping entries"));
    }

    let mut samples = Vec::with_capacity(sample_count);
    let mut sample_index = 0usize;
    let mut run_index = 0usize;
    let mut dts = 0i64;
    for (chunk_index, chunk_offset) in chunk_offsets.iter().enumerate() {
        let chunk_number = u32::try_from(chunk_index + 1)
            .map_err(|_| invalid("chunk number exceeds 32-bit range"))?;
        while run_index + 1 < chunk_map.len() && chunk_map[run_index + 1].0 <= chunk_number {
            run_index += 1;
        }
        let (first_chunk, samples_per_chunk, description_index) = chunk_map[run_index];
        if first_chunk > chunk_number || description_index != 1 {
            return Err(invalid("chunk uses an unsupported sample description"));
        }
        let mut offset = *chunk_offset;
        for _ in 0..samples_per_chunk {
            if sample_index >= sample_count {
                return Err(invalid("chunk map contains more samples than stsz"));
            }
            let size = if constant_size != 0 {
                constant_size
            } else {
                *sizes
                    .get(sample_index)
                    .ok_or_else(|| invalid("stsz sample size is missing"))?
            };
            let end = offset
                .checked_add(u64::from(size))
                .ok_or_else(|| invalid("sample byte range overflows"))?;
            if size == 0
                || !media_ranges
                    .iter()
                    .any(|(start, limit)| *start <= offset && end <= *limit)
            {
                return Err(invalid("sample byte range is outside every mdat payload"));
            }
            let composition_offset = composition_offsets[sample_index];
            let pts = dts
                .checked_add(composition_offset)
                .ok_or_else(|| invalid("presentation timestamp overflows"))?;
            samples.push(SampleLocation {
                offset,
                size,
                dts_ticks: dts,
                pts_ticks: pts,
                duration_ticks: u32::try_from(durations[sample_index])
                    .map_err(|_| invalid("sample duration is negative or too large"))?,
                is_sync: sync_numbers
                    .map(|numbers| numbers.contains(&(sample_index as u32 + 1)))
                    .unwrap_or(true),
            });
            dts = dts
                .checked_add(durations[sample_index])
                .ok_or_else(|| invalid("decode timestamp overflows"))?;
            offset = end;
            sample_index += 1;
        }
    }
    if sample_index != sample_count {
        return Err(invalid("chunk map does not cover every stsz sample"));
    }
    Ok(samples)
}

fn invalid(message: impl Into<String>) -> io::Error {
    io::Error::new(io::ErrorKind::InvalidData, message.into())
}

fn validate_length_prefixed_nals(data: &[u8]) -> io::Result<()> {
    let mut offset = 0;
    while offset < data.len() {
        if data.len() - offset < 4 {
            return Err(invalid("truncated NAL length prefix"));
        }
        let nal_size = u32::from_be_bytes(data[offset..offset + 4].try_into().unwrap()) as usize;
        offset += 4;
        if nal_size == 0 {
            return Err(invalid("NAL unit has zero length"));
        }
        if nal_size > data.len() - offset {
            return Err(invalid("NAL unit is truncated"));
        }
        offset += nal_size;
    }
    Ok(())
}
