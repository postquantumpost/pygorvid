//! Reader for the mp4 box structure. Each box type is a named type.
//!
//! To read more of the format, add a box type and register it in `new_box`.
//! Unregistered box types are skipped without being parsed.

use std::io::{self, Read, Seek, SeekFrom};

pub trait ReadSeek: Read + Seek {}
impl<T: Read + Seek> ReadSeek for T {}

#[derive(Clone, Debug)]
pub struct BoxHeader {
    pub kind: [u8; 4],
    pub start: u64,
    pub header_size: u64,
    pub end: u64,
}

impl BoxHeader {
    pub fn payload_start(&self) -> u64 {
        self.start + self.header_size
    }
}

fn invalid(msg: String) -> io::Error {
    io::Error::new(io::ErrorKind::InvalidData, msg)
}

/// Reads a box header at the current position; `None` if no room is left before `limit`.
pub fn read_header(r: &mut dyn ReadSeek, limit: u64) -> io::Result<Option<BoxHeader>> {
    let start = r.stream_position()?;
    if start + 8 > limit {
        return Ok(None);
    }
    let mut b = [0u8; 8];
    r.read_exact(&mut b)?;
    let size32 = u32::from_be_bytes([b[0], b[1], b[2], b[3]]) as u64;
    let kind = [b[4], b[5], b[6], b[7]];
    let (size, header_size) = match size32 {
        0 => (limit - start, 8),
        1 => {
            let mut l = [0u8; 8];
            r.read_exact(&mut l)?;
            (u64::from_be_bytes(l), 16)
        }
        n => (n, 8),
    };
    if size < header_size || size > limit - start {
        return Err(invalid(format!(
            "invalid box size {size} at offset {start}"
        )));
    }
    Ok(Some(BoxHeader {
        kind,
        start,
        header_size,
        end: start + size,
    }))
}

pub trait Mp4Box {
    fn header(&self) -> &BoxHeader;
    /// Parses the payload. No position is guaranteed; seek as needed.
    fn read(&mut self, _r: &mut dyn ReadSeek) -> io::Result<()> {
        Ok(())
    }
    fn children(&self) -> &[Box<dyn Mp4Box>] {
        &[]
    }
    /// Handler type, for boxes that have one.
    fn handler_type(&self) -> Option<[u8; 4]> {
        None
    }
    fn dimensions(&self) -> Option<(u32, u32)> {
        None
    }
    fn timescale(&self) -> Option<u32> {
        None
    }
    fn sample_timing(&self) -> Option<&[(u32, u32)]> {
        None
    }
    fn audio_info(&self) -> Option<(u32, u16)> {
        None
    }
    fn avc_configuration(&self) -> Option<&AvcConfiguration> {
        None
    }
    fn sample_size_table(&self) -> Option<(u32, u32, &[u32])> {
        None
    }
    fn sample_to_chunk(&self) -> Option<&[(u32, u32, u32)]> {
        None
    }
    fn chunk_offsets(&self) -> Option<&[u64]> {
        None
    }
    fn composition_timing(&self) -> Option<(u8, &[(u32, i64)])> {
        None
    }
    fn sync_samples(&self) -> Option<&[u32]> {
        None
    }
    fn data_range(&self) -> Option<(u64, u64)> {
        None
    }
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct AvcConfiguration {
    pub profile: u8,
    pub profile_compat: u8,
    pub level: u8,
    pub nal_length_size: u8,
    pub sequence_parameter_sets: Vec<Vec<u8>>,
    pub picture_parameter_sets: Vec<Vec<u8>>,
}

pub struct UnknownBox(BoxHeader);

impl Mp4Box for UnknownBox {
    fn header(&self) -> &BoxHeader {
        &self.0
    }
}

pub struct ContainerBox {
    header: BoxHeader,
    children: Vec<Box<dyn Mp4Box>>,
}

impl ContainerBox {
    fn new(header: BoxHeader) -> Self {
        Self {
            header,
            children: Vec::new(),
        }
    }
}

impl Mp4Box for ContainerBox {
    fn header(&self) -> &BoxHeader {
        &self.header
    }
    fn read(&mut self, r: &mut dyn ReadSeek) -> io::Result<()> {
        self.children = read_boxes(r, self.header.payload_start(), self.header.end)?;
        Ok(())
    }
    fn children(&self) -> &[Box<dyn Mp4Box>] {
        &self.children
    }
}

// Container boxes that differ only by name.
macro_rules! container_box {
    ($name:ident) => {
        pub struct $name(ContainerBox);

        impl $name {
            fn new(header: BoxHeader) -> Self {
                Self(ContainerBox::new(header))
            }
        }

        impl Mp4Box for $name {
            fn header(&self) -> &BoxHeader {
                self.0.header()
            }
            fn read(&mut self, r: &mut dyn ReadSeek) -> io::Result<()> {
                self.0.read(r)
            }
            fn children(&self) -> &[Box<dyn Mp4Box>] {
                self.0.children()
            }
        }
    };
}

container_box!(MoovBox);
container_box!(TrakBox);
container_box!(MdiaBox);
container_box!(MinfBox);
container_box!(StblBox);

pub struct TkhdBox {
    header: BoxHeader,
    width: u32,
    height: u32,
}

impl Mp4Box for TkhdBox {
    fn header(&self) -> &BoxHeader {
        &self.header
    }
    fn read(&mut self, r: &mut dyn ReadSeek) -> io::Result<()> {
        let payload_size = self.header.end - self.header.payload_start();
        if payload_size < 84 {
            return Err(invalid("tkhd box too short".to_string()));
        }
        r.seek(SeekFrom::Start(self.header.payload_start()))?;
        let mut version = [0u8; 1];
        r.read_exact(&mut version)?;
        let width_offset = if version[0] == 1 { 88 } else { 76 };
        if payload_size < width_offset + 8 {
            return Err(invalid("tkhd box too short".to_string()));
        }
        r.seek(SeekFrom::Start(self.header.payload_start() + width_offset))?;
        let mut dimensions = [0u8; 8];
        r.read_exact(&mut dimensions)?;
        self.width = u32::from_be_bytes(dimensions[..4].try_into().unwrap()) >> 16;
        self.height = u32::from_be_bytes(dimensions[4..].try_into().unwrap()) >> 16;
        Ok(())
    }
    fn dimensions(&self) -> Option<(u32, u32)> {
        Some((self.width, self.height))
    }
}

pub struct MdhdBox {
    header: BoxHeader,
    timescale: u32,
}

impl Mp4Box for MdhdBox {
    fn header(&self) -> &BoxHeader {
        &self.header
    }
    fn read(&mut self, r: &mut dyn ReadSeek) -> io::Result<()> {
        let payload_size = self.header.end - self.header.payload_start();
        if payload_size < 20 {
            return Err(invalid("mdhd box too short".to_string()));
        }
        r.seek(SeekFrom::Start(self.header.payload_start()))?;
        let mut version = [0u8; 1];
        r.read_exact(&mut version)?;
        let timescale_offset = if version[0] == 1 { 20 } else { 12 };
        if payload_size < timescale_offset + 4 {
            return Err(invalid("mdhd box too short".to_string()));
        }
        r.seek(SeekFrom::Start(
            self.header.payload_start() + timescale_offset,
        ))?;
        let mut bytes = [0u8; 4];
        r.read_exact(&mut bytes)?;
        self.timescale = u32::from_be_bytes(bytes);
        Ok(())
    }
    fn timescale(&self) -> Option<u32> {
        Some(self.timescale)
    }
}

pub struct SttsBox {
    header: BoxHeader,
    entries: Vec<(u32, u32)>,
}

impl Mp4Box for SttsBox {
    fn header(&self) -> &BoxHeader {
        &self.header
    }
    fn read(&mut self, r: &mut dyn ReadSeek) -> io::Result<()> {
        let payload_size = self.header.end - self.header.payload_start();
        if payload_size < 8 {
            return Err(invalid("stts box too short".to_string()));
        }
        r.seek(SeekFrom::Start(self.header.payload_start() + 4))?;
        let mut count_bytes = [0u8; 4];
        r.read_exact(&mut count_bytes)?;
        let count = u32::from_be_bytes(count_bytes) as u64;
        if count * 8 > payload_size - 8 {
            return Err(invalid("stts entries exceed box size".to_string()));
        }
        self.entries.clear();
        for _ in 0..count {
            let mut entry = [0u8; 8];
            r.read_exact(&mut entry)?;
            self.entries.push((
                u32::from_be_bytes(entry[..4].try_into().unwrap()),
                u32::from_be_bytes(entry[4..].try_into().unwrap()),
            ));
        }
        Ok(())
    }
    fn sample_timing(&self) -> Option<&[(u32, u32)]> {
        Some(&self.entries)
    }
}

pub struct CttsBox {
    header: BoxHeader,
    version: u8,
    entries: Vec<(u32, i64)>,
}

impl Mp4Box for CttsBox {
    fn header(&self) -> &BoxHeader {
        &self.header
    }
    fn read(&mut self, r: &mut dyn ReadSeek) -> io::Result<()> {
        let payload_size = self.header.end - self.header.payload_start();
        if payload_size < 8 {
            return Err(invalid("ctts box too short".to_string()));
        }
        r.seek(SeekFrom::Start(self.header.payload_start()))?;
        let mut version_flags = [0u8; 4];
        r.read_exact(&mut version_flags)?;
        self.version = version_flags[0];
        if self.version > 1 {
            return Err(invalid(format!(
                "unsupported ctts version {}",
                self.version
            )));
        }
        let mut count_bytes = [0u8; 4];
        r.read_exact(&mut count_bytes)?;
        let count = u32::from_be_bytes(count_bytes) as u64;
        if count * 8 > payload_size - 8 {
            return Err(invalid("ctts entries exceed box size".to_string()));
        }
        self.entries.clear();
        for _ in 0..count {
            let mut entry = [0u8; 8];
            r.read_exact(&mut entry)?;
            let offset = u32::from_be_bytes(entry[4..].try_into().unwrap());
            let offset = if self.version == 1 {
                i64::from(offset as i32)
            } else {
                i64::from(offset)
            };
            self.entries
                .push((u32::from_be_bytes(entry[..4].try_into().unwrap()), offset));
        }
        Ok(())
    }
    fn composition_timing(&self) -> Option<(u8, &[(u32, i64)])> {
        Some((self.version, &self.entries))
    }
}

pub struct StssBox {
    header: BoxHeader,
    sample_numbers: Vec<u32>,
}

impl Mp4Box for StssBox {
    fn header(&self) -> &BoxHeader {
        &self.header
    }
    fn read(&mut self, r: &mut dyn ReadSeek) -> io::Result<()> {
        let payload_size = self.header.end - self.header.payload_start();
        if payload_size < 8 {
            return Err(invalid("stss box too short".to_string()));
        }
        r.seek(SeekFrom::Start(self.header.payload_start() + 4))?;
        let mut count_bytes = [0u8; 4];
        r.read_exact(&mut count_bytes)?;
        let count = u32::from_be_bytes(count_bytes) as u64;
        if count * 4 > payload_size - 8 {
            return Err(invalid("stss entries exceed box size".to_string()));
        }
        self.sample_numbers.clear();
        for _ in 0..count {
            let mut bytes = [0u8; 4];
            r.read_exact(&mut bytes)?;
            let number = u32::from_be_bytes(bytes);
            if number == 0
                || self
                    .sample_numbers
                    .last()
                    .is_some_and(|last| number <= *last)
            {
                return Err(invalid(
                    "stss sample numbers must be positive and increasing".to_string(),
                ));
            }
            self.sample_numbers.push(number);
        }
        Ok(())
    }
    fn sync_samples(&self) -> Option<&[u32]> {
        Some(&self.sample_numbers)
    }
}

pub struct StszBox {
    header: BoxHeader,
    constant_size: u32,
    sample_count: u32,
    sizes: Vec<u32>,
}

impl Mp4Box for StszBox {
    fn header(&self) -> &BoxHeader {
        &self.header
    }
    fn read(&mut self, r: &mut dyn ReadSeek) -> io::Result<()> {
        let payload_size = self.header.end - self.header.payload_start();
        if payload_size < 12 {
            return Err(invalid("stsz box too short".to_string()));
        }
        r.seek(SeekFrom::Start(self.header.payload_start() + 4))?;
        let mut fields = [0u8; 8];
        r.read_exact(&mut fields)?;
        self.constant_size = u32::from_be_bytes(fields[..4].try_into().unwrap());
        self.sample_count = u32::from_be_bytes(fields[4..].try_into().unwrap());
        self.sizes.clear();
        if self.constant_size != 0 {
            return Ok(());
        }
        if u64::from(self.sample_count) * 4 > payload_size - 12 {
            return Err(invalid("stsz entries exceed box size".to_string()));
        }
        for _ in 0..self.sample_count {
            let mut bytes = [0u8; 4];
            r.read_exact(&mut bytes)?;
            self.sizes.push(u32::from_be_bytes(bytes));
        }
        Ok(())
    }
    fn sample_size_table(&self) -> Option<(u32, u32, &[u32])> {
        Some((self.constant_size, self.sample_count, &self.sizes))
    }
}

pub struct StscBox {
    header: BoxHeader,
    entries: Vec<(u32, u32, u32)>,
}

impl Mp4Box for StscBox {
    fn header(&self) -> &BoxHeader {
        &self.header
    }
    fn read(&mut self, r: &mut dyn ReadSeek) -> io::Result<()> {
        let payload_size = self.header.end - self.header.payload_start();
        if payload_size < 8 {
            return Err(invalid("stsc box too short".to_string()));
        }
        r.seek(SeekFrom::Start(self.header.payload_start() + 4))?;
        let mut count_bytes = [0u8; 4];
        r.read_exact(&mut count_bytes)?;
        let count = u32::from_be_bytes(count_bytes) as u64;
        if count * 12 > payload_size - 8 {
            return Err(invalid("stsc entries exceed box size".to_string()));
        }
        self.entries.clear();
        for _ in 0..count {
            let mut bytes = [0u8; 12];
            r.read_exact(&mut bytes)?;
            let entry = (
                u32::from_be_bytes(bytes[..4].try_into().unwrap()),
                u32::from_be_bytes(bytes[4..8].try_into().unwrap()),
                u32::from_be_bytes(bytes[8..].try_into().unwrap()),
            );
            if entry.0 == 0 || entry.1 == 0 || entry.2 == 0 {
                return Err(invalid("invalid stsc entry values".to_string()));
            }
            if (self.entries.is_empty() && entry.0 != 1)
                || self.entries.last().is_some_and(|last| entry.0 <= last.0)
            {
                return Err(invalid(
                    "stsc first_chunk values must start at 1 and increase".to_string(),
                ));
            }
            self.entries.push(entry);
        }
        Ok(())
    }
    fn sample_to_chunk(&self) -> Option<&[(u32, u32, u32)]> {
        Some(&self.entries)
    }
}

pub struct StcoBox {
    header: BoxHeader,
    offsets: Vec<u64>,
}

pub struct Co64Box {
    header: BoxHeader,
    offsets: Vec<u64>,
}

fn read_chunk_offsets(
    r: &mut dyn ReadSeek,
    header: &BoxHeader,
    entry_size: u64,
) -> io::Result<Vec<u64>> {
    let payload_size = header.end - header.payload_start();
    if payload_size < 8 {
        return Err(invalid("chunk offset box too short".to_string()));
    }
    r.seek(SeekFrom::Start(header.payload_start() + 4))?;
    let mut count_bytes = [0u8; 4];
    r.read_exact(&mut count_bytes)?;
    let count = u32::from_be_bytes(count_bytes) as u64;
    if count * entry_size > payload_size - 8 {
        return Err(invalid("chunk offsets exceed box size".to_string()));
    }
    let mut offsets = Vec::with_capacity(count as usize);
    for _ in 0..count {
        if entry_size == 4 {
            let mut bytes = [0u8; 4];
            r.read_exact(&mut bytes)?;
            offsets.push(u64::from(u32::from_be_bytes(bytes)));
        } else {
            let mut bytes = [0u8; 8];
            r.read_exact(&mut bytes)?;
            offsets.push(u64::from_be_bytes(bytes));
        }
    }
    Ok(offsets)
}

impl Mp4Box for StcoBox {
    fn header(&self) -> &BoxHeader {
        &self.header
    }
    fn read(&mut self, r: &mut dyn ReadSeek) -> io::Result<()> {
        self.offsets = read_chunk_offsets(r, &self.header, 4)?;
        Ok(())
    }
    fn chunk_offsets(&self) -> Option<&[u64]> {
        Some(&self.offsets)
    }
}

impl Mp4Box for Co64Box {
    fn header(&self) -> &BoxHeader {
        &self.header
    }
    fn read(&mut self, r: &mut dyn ReadSeek) -> io::Result<()> {
        self.offsets = read_chunk_offsets(r, &self.header, 8)?;
        Ok(())
    }
    fn chunk_offsets(&self) -> Option<&[u64]> {
        Some(&self.offsets)
    }
}

pub struct MdatBox {
    header: BoxHeader,
}

impl Mp4Box for MdatBox {
    fn header(&self) -> &BoxHeader {
        &self.header
    }
    fn data_range(&self) -> Option<(u64, u64)> {
        Some((self.header.payload_start(), self.header.end))
    }
}

pub struct Avc1Box {
    header: BoxHeader,
    children: Vec<Box<dyn Mp4Box>>,
    width: u16,
    height: u16,
}

impl Mp4Box for Avc1Box {
    fn header(&self) -> &BoxHeader {
        &self.header
    }
    fn read(&mut self, r: &mut dyn ReadSeek) -> io::Result<()> {
        if self.header.end - self.header.payload_start() < 78 {
            return Err(invalid("avc1 sample entry too short".to_string()));
        }
        r.seek(SeekFrom::Start(self.header.payload_start() + 24))?;
        let mut dimensions = [0u8; 4];
        r.read_exact(&mut dimensions)?;
        self.width = u16::from_be_bytes(dimensions[..2].try_into().unwrap());
        self.height = u16::from_be_bytes(dimensions[2..].try_into().unwrap());
        self.children = read_boxes(r, self.header.payload_start() + 78, self.header.end)?;
        Ok(())
    }
    fn children(&self) -> &[Box<dyn Mp4Box>] {
        &self.children
    }
}

pub struct AvcCBox {
    header: BoxHeader,
    configuration: AvcConfiguration,
}

impl Mp4Box for AvcCBox {
    fn header(&self) -> &BoxHeader {
        &self.header
    }
    fn read(&mut self, r: &mut dyn ReadSeek) -> io::Result<()> {
        let payload_size = self.header.end - self.header.payload_start();
        if payload_size < 7 {
            return Err(invalid("avcC box too short".to_string()));
        }
        let size =
            usize::try_from(payload_size).map_err(|_| invalid("avcC box too large".to_string()))?;
        r.seek(SeekFrom::Start(self.header.payload_start()))?;
        let mut data = vec![0u8; size];
        r.read_exact(&mut data)?;
        if data[0] != 1 {
            return Err(invalid("unsupported AVC configuration version".to_string()));
        }
        if data[4] & 0xfc != 0xfc || data[5] & 0xe0 != 0xe0 {
            return Err(invalid("avcC reserved bits are invalid".to_string()));
        }
        if data[4] & 0x03 != 0x03 {
            return Err(invalid(
                "only four-byte AVC NAL length prefixes are supported".to_string(),
            ));
        }
        let nal_length_size = 4;
        let sequence_count = (data[5] & 0x1f) as usize;
        if sequence_count == 0 {
            return Err(invalid(
                "avcC contains no sequence parameter sets".to_string(),
            ));
        }
        let (sequence_parameter_sets, mut offset) = read_avc_nals(&data, 6, sequence_count, 7)?;
        if offset >= data.len() {
            return Err(invalid("avcC picture-set count missing".to_string()));
        }
        let picture_count = data[offset] as usize;
        if picture_count == 0 {
            return Err(invalid(
                "avcC contains no picture parameter sets".to_string(),
            ));
        }
        offset += 1;
        let (picture_parameter_sets, mut offset) = read_avc_nals(&data, offset, picture_count, 8)?;
        if offset < data.len() {
            let has_extension = matches!(
                data[1],
                44 | 83 | 86 | 100 | 110 | 118 | 122 | 128 | 134 | 135 | 138 | 139 | 144 | 244
            );
            if !has_extension || data.len() - offset < 4 {
                return Err(invalid("avcC has invalid trailing data".to_string()));
            }
            if data[offset] & 0xfc != 0xfc
                || data[offset + 1] & 0xf8 != 0xf8
                || data[offset + 2] & 0xf8 != 0xf8
            {
                return Err(invalid(
                    "avcC extension reserved bits are invalid".to_string(),
                ));
            }
            let extension_count = data[offset + 3] as usize;
            let (_, extension_end) = read_avc_nals(&data, offset + 4, extension_count, 13)?;
            offset = extension_end;
        }
        if offset != data.len() {
            return Err(invalid("avcC has invalid trailing data".to_string()));
        }
        self.configuration = AvcConfiguration {
            profile: data[1],
            profile_compat: data[2],
            level: data[3],
            nal_length_size,
            sequence_parameter_sets,
            picture_parameter_sets,
        };
        Ok(())
    }
    fn avc_configuration(&self) -> Option<&AvcConfiguration> {
        Some(&self.configuration)
    }
}

fn read_avc_nals(
    data: &[u8],
    mut offset: usize,
    count: usize,
    nal_type: u8,
) -> io::Result<(Vec<Vec<u8>>, usize)> {
    let mut nals = Vec::with_capacity(count);
    for _ in 0..count {
        if offset + 2 > data.len() {
            return Err(invalid("avcC NAL length truncated".to_string()));
        }
        let size = u16::from_be_bytes(data[offset..offset + 2].try_into().unwrap()) as usize;
        offset += 2;
        if size == 0 || size > data.len() - offset {
            return Err(invalid("avcC NAL exceeds box size".to_string()));
        }
        if data[offset] & 0x80 != 0 || data[offset] & 0x1f != nal_type {
            return Err(invalid(format!(
                "avcC parameter-set NAL has unexpected type; want {nal_type}"
            )));
        }
        nals.push(data[offset..offset + size].to_vec());
        offset += size;
    }
    Ok((nals, offset))
}

pub struct StsdBox {
    header: BoxHeader,
    sample_rate: u32,
    channels: u16,
    entries: Vec<Box<dyn Mp4Box>>,
}

impl Mp4Box for StsdBox {
    fn header(&self) -> &BoxHeader {
        &self.header
    }
    fn read(&mut self, r: &mut dyn ReadSeek) -> io::Result<()> {
        let payload_size = self.header.end - self.header.payload_start();
        if payload_size < 8 {
            return Err(invalid("stsd box too short".to_string()));
        }
        r.seek(SeekFrom::Start(self.header.payload_start() + 4))?;
        let mut count_bytes = [0u8; 4];
        r.read_exact(&mut count_bytes)?;
        let count = u32::from_be_bytes(count_bytes) as u64;
        self.entries.clear();
        for _ in 0..count {
            let entry = read_header(r, self.header.end)?
                .ok_or_else(|| invalid("stsd entry missing".to_string()))?;
            let mut sample_entry = new_box(entry.clone());
            if entry.kind == *b"mp4a" && entry.end - entry.payload_start() >= 28 {
                r.seek(SeekFrom::Start(entry.payload_start() + 16))?;
                let mut channels = [0u8; 2];
                r.read_exact(&mut channels)?;
                self.channels = u16::from_be_bytes(channels);
                r.seek(SeekFrom::Start(entry.payload_start() + 24))?;
                let mut sample_rate = [0u8; 4];
                r.read_exact(&mut sample_rate)?;
                self.sample_rate = u32::from_be_bytes(sample_rate) >> 16;
            }
            sample_entry.read(r)?;
            self.entries.push(sample_entry);
            r.seek(SeekFrom::Start(entry.end))?;
        }
        Ok(())
    }
    fn audio_info(&self) -> Option<(u32, u16)> {
        Some((self.sample_rate, self.channels))
    }
    fn children(&self) -> &[Box<dyn Mp4Box>] {
        &self.entries
    }
}

pub struct HdlrBox {
    header: BoxHeader,
    handler: [u8; 4],
}

impl Mp4Box for HdlrBox {
    fn header(&self) -> &BoxHeader {
        &self.header
    }
    fn read(&mut self, r: &mut dyn ReadSeek) -> io::Result<()> {
        // payload: version/flags (4), pre_defined (4), handler_type (4), ...
        if self.header.end - self.header.payload_start() < 12 {
            return Err(invalid("hdlr box too short".to_string()));
        }
        r.seek(SeekFrom::Start(self.header.payload_start() + 8))?;
        r.read_exact(&mut self.handler)
    }
    fn handler_type(&self) -> Option<[u8; 4]> {
        Some(self.handler)
    }
}

fn new_box(header: BoxHeader) -> Box<dyn Mp4Box> {
    match &header.kind {
        b"moov" => Box::new(MoovBox::new(header)),
        b"trak" => Box::new(TrakBox::new(header)),
        b"mdia" => Box::new(MdiaBox::new(header)),
        b"minf" => Box::new(MinfBox::new(header)),
        b"stbl" => Box::new(StblBox::new(header)),
        b"hdlr" => Box::new(HdlrBox {
            header,
            handler: [0; 4],
        }),
        b"tkhd" => Box::new(TkhdBox {
            header,
            width: 0,
            height: 0,
        }),
        b"mdhd" => Box::new(MdhdBox {
            header,
            timescale: 0,
        }),
        b"stts" => Box::new(SttsBox {
            header,
            entries: Vec::new(),
        }),
        b"ctts" => Box::new(CttsBox {
            header,
            version: 0,
            entries: Vec::new(),
        }),
        b"stss" => Box::new(StssBox {
            header,
            sample_numbers: Vec::new(),
        }),
        b"stsz" => Box::new(StszBox {
            header,
            constant_size: 0,
            sample_count: 0,
            sizes: Vec::new(),
        }),
        b"stsc" => Box::new(StscBox {
            header,
            entries: Vec::new(),
        }),
        b"stco" => Box::new(StcoBox {
            header,
            offsets: Vec::new(),
        }),
        b"co64" => Box::new(Co64Box {
            header,
            offsets: Vec::new(),
        }),
        b"mdat" => Box::new(MdatBox { header }),
        b"avc1" => Box::new(Avc1Box {
            header,
            children: Vec::new(),
            width: 0,
            height: 0,
        }),
        b"avcC" => Box::new(AvcCBox {
            header,
            configuration: AvcConfiguration {
                profile: 0,
                profile_compat: 0,
                level: 0,
                nal_length_size: 0,
                sequence_parameter_sets: Vec::new(),
                picture_parameter_sets: Vec::new(),
            },
        }),
        b"stsd" => Box::new(StsdBox {
            header,
            sample_rate: 0,
            channels: 0,
            entries: Vec::new(),
        }),
        _ => Box::new(UnknownBox(header)),
    }
}

/// Reads the sibling boxes occupying `[start, end)`.
pub fn read_boxes(r: &mut dyn ReadSeek, start: u64, end: u64) -> io::Result<Vec<Box<dyn Mp4Box>>> {
    r.seek(SeekFrom::Start(start))?;
    let mut boxes = Vec::new();
    while let Some(header) = read_header(r, end)? {
        let box_end = header.end;
        let mut b = new_box(header);
        b.read(r)?;
        r.seek(SeekFrom::Start(box_end))?;
        boxes.push(b);
    }
    Ok(boxes)
}

/// Boxes reached by matching `path[0]` among `boxes`, then `path[1]` among their children, ...
pub fn find<'a>(boxes: &'a [Box<dyn Mp4Box>], path: &[&[u8; 4]]) -> Vec<&'a dyn Mp4Box> {
    let mut level: Vec<&'a dyn Mp4Box> = boxes.iter().map(|b| &**b).collect();
    for (i, kind) in path.iter().enumerate() {
        let matched: Vec<&'a dyn Mp4Box> = level
            .into_iter()
            .filter(|b| &b.header().kind == *kind)
            .collect();
        if i + 1 == path.len() {
            return matched;
        }
        level = matched
            .iter()
            .copied()
            .flat_map(|b| b.children().iter().map(|c| &**c))
            .collect();
    }
    Vec::new()
}
