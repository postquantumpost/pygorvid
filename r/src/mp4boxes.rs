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
        return Err(invalid(format!("invalid box size {size} at offset {start}")));
    }
    Ok(Some(BoxHeader { kind, start, header_size, end: start + size }))
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
        Self { header, children: Vec::new() }
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
        b"hdlr" => Box::new(HdlrBox { header, handler: [0; 4] }),
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
        let matched: Vec<&'a dyn Mp4Box> =
            level.into_iter().filter(|b| &b.header().kind == *kind).collect();
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
