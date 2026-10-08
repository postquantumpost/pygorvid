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

pub struct StsdBox {
    header: BoxHeader,
    sample_rate: u32,
    channels: u16,
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
        if u32::from_be_bytes(count_bytes) == 0 {
            return Ok(());
        }
        let entry = read_header(r, self.header.end)?
            .ok_or_else(|| invalid("stsd entry missing".to_string()))?;
        if entry.end - entry.payload_start() < 28 {
            return Ok(());
        }
        r.seek(SeekFrom::Start(entry.payload_start() + 16))?;
        let mut channels = [0u8; 2];
        r.read_exact(&mut channels)?;
        self.channels = u16::from_be_bytes(channels);
        r.seek(SeekFrom::Start(entry.payload_start() + 24))?;
        let mut sample_rate = [0u8; 4];
        r.read_exact(&mut sample_rate)?;
        self.sample_rate = u32::from_be_bytes(sample_rate) >> 16;
        Ok(())
    }
    fn audio_info(&self) -> Option<(u32, u16)> {
        Some((self.sample_rate, self.channels))
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
        b"stsd" => Box::new(StsdBox {
            header,
            sample_rate: 0,
            channels: 0,
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
