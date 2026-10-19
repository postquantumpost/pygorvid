use crate::mp4boxes::{invalid, BoxHeader, Mp4Box, ReadSeek};
use std::io::{self, SeekFrom};

pub(crate) fn new_box(header: BoxHeader) -> Box<dyn Mp4Box> {
    if header.kind == *b"avc1" {
        Box::new(crate::mp4boxes::VisualSampleEntryBox::new(header))
    } else {
        Box::new(AvcCBox {
            header,
            configuration: AvcConfiguration::default(),
        })
    }
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct AvcConfiguration {
    pub profile: u8,
    pub profile_compat: u8,
    pub level: u8,
    pub nal_length_size: u8,
    pub sequence_parameter_sets: Vec<Vec<u8>>,
    pub picture_parameter_sets: Vec<Vec<u8>>,
}

pub(crate) fn sample_configuration(entry: &dyn Mp4Box) -> io::Result<AvcConfiguration> {
    let configurations: Vec<_> = entry
        .children()
        .iter()
        .filter_map(|child| child.avc_configuration())
        .collect();
    if configurations.len() != 1 {
        return Err(invalid(
            "avc1 sample entry must contain one avcC box".to_string(),
        ));
    }
    Ok(configurations[0].clone())
}

pub struct AvcCBox {
    pub(crate) header: BoxHeader,
    pub(crate) configuration: AvcConfiguration,
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
