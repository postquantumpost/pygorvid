use crate::mp4boxes::{invalid, BoxHeader, Mp4Box, ReadSeek};
use std::io::{self, SeekFrom};

pub(crate) fn new_box(header: BoxHeader) -> Box<dyn Mp4Box> {
    if matches!(&header.kind, b"hvc1" | b"hev1") {
        Box::new(crate::mp4boxes::VisualSampleEntryBox::new(header))
    } else {
        Box::new(HvcCBox {
            header,
            configuration: None,
        })
    }
}

pub(crate) fn sample_configuration(entry: &dyn Mp4Box) -> io::Result<HevcConfiguration> {
    let configurations: Vec<_> = entry
        .children()
        .iter()
        .filter_map(|child| child.hevc_configuration())
        .collect();
    if configurations.len() != 1 {
        return Err(invalid(
            "HEVC sample entry must contain one hvcC box".to_string(),
        ));
    }
    Ok(configurations[0].clone())
}

#[cfg(test)]
use crate::mp4boxes::read_boxes;

#[cfg(test)]
mod hevc_tests {
    use super::*;

    fn configuration_fixture() -> Vec<u8> {
        let mut data = vec![0; 23];
        data[0] = 1;
        data[1] = 2;
        data[12] = 120;
        data[13] = 0xf0;
        data[15] = 0xfc;
        data[16] = 0xfd;
        data[17] = 0xfa;
        data[18] = 0xfa;
        data[21] = 3;
        data[22] = 3;
        for nal_type in [32u8, 33, 34] {
            data.extend_from_slice(&[0x80 | nal_type, 0, 1, 0, 3, nal_type << 1, 1, 0x80]);
        }
        data
    }

    fn parse_fixture(kind: &[u8; 4], data: &[u8]) -> io::Result<Vec<Box<dyn Mp4Box>>> {
        let mut bytes = ((8 + 78 + 8 + data.len()) as u32).to_be_bytes().to_vec();
        bytes.extend_from_slice(kind);
        bytes.extend_from_slice(&[0; 78]);
        bytes.extend_from_slice(&((8 + data.len()) as u32).to_be_bytes());
        bytes.extend_from_slice(b"hvcC");
        bytes.extend_from_slice(data);
        let size = bytes.len() as u64;
        read_boxes(&mut std::io::Cursor::new(bytes), 0, size)
    }

    #[test]
    fn hevc_sample_entries_and_configuration() {
        let data = configuration_fixture();
        for kind in [b"hvc1", b"hev1"] {
            let boxes = parse_fixture(kind, &data).unwrap();
            let config = boxes[0].children()[0].hevc_configuration().unwrap();
            assert_eq!(
                (
                    config.profile,
                    config.level,
                    config.bit_depth_luma,
                    config.bit_depth_chroma,
                    config.nal_length_size
                ),
                (2, 120, 10, 10, 4)
            );
            assert_eq!(config.arrays.len(), 3);
            for (array, nal_type) in config.arrays.iter().zip([32, 33, 34]) {
                assert!(array.complete);
                assert_eq!(array.nal_type, nal_type);
                assert_eq!(array.units, vec![vec![nal_type << 1, 1, 0x80]]);
            }
        }
        for length in 0..data.len() {
            assert!(parse_fixture(b"hvc1", &data[..length]).is_err());
        }
        for (offset, value) in [
            (0, 2),
            (13, 0),
            (15, 0),
            (16, 0),
            (17, 0),
            (18, 0),
            (21, 2),
            (23, 0x60),
            (26, 0xff),
            (28, 0),
            (29, 0),
        ] {
            let mut invalid = data.clone();
            invalid[offset] = value;
            assert!(parse_fixture(b"hvc1", &invalid).is_err(), "offset {offset}");
        }
        let mut trailing = data;
        trailing.push(0);
        assert!(parse_fixture(b"hvc1", &trailing).is_err());
    }
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct HevcConfiguration {
    pub profile: u8,
    pub level: u8,
    pub bit_depth_luma: u8,
    pub bit_depth_chroma: u8,
    pub nal_length_size: u8,
    pub arrays: Vec<HevcNalArray>,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct HevcNalArray {
    pub complete: bool,
    pub nal_type: u8,
    pub units: Vec<Vec<u8>>,
}

pub struct HvcCBox {
    pub(crate) header: BoxHeader,
    pub(crate) configuration: Option<HevcConfiguration>,
}

impl Mp4Box for HvcCBox {
    fn header(&self) -> &BoxHeader {
        &self.header
    }
    fn hevc_configuration(&self) -> Option<&HevcConfiguration> {
        self.configuration.as_ref()
    }
    fn read(&mut self, r: &mut dyn ReadSeek) -> io::Result<()> {
        let size = usize::try_from(self.header.end - self.header.payload_start())
            .map_err(|_| invalid("hvcC box too large".to_string()))?;
        if size < 23 {
            return Err(invalid("hvcC box too short".to_string()));
        }
        r.seek(SeekFrom::Start(self.header.payload_start()))?;
        let mut data = vec![0; size];
        r.read_exact(&mut data)?;
        if data[0] != 1 {
            return Err(invalid(
                "unsupported HEVC configuration version".to_string(),
            ));
        }
        if data[13] & 0xf0 != 0xf0
            || data[15] & 0xfc != 0xfc
            || data[16] & 0xfc != 0xfc
            || data[17] & 0xf8 != 0xf8
            || data[18] & 0xf8 != 0xf8
        {
            return Err(invalid("hvcC reserved bits are invalid".to_string()));
        }
        let nal_length_size = (data[21] & 3) + 1;
        if nal_length_size == 3 {
            return Err(invalid("hvcC uses a reserved NAL length size".to_string()));
        }
        let mut arrays = Vec::new();
        let mut offset = 23;
        for _ in 0..data[22] {
            if data.len() - offset < 3 {
                return Err(invalid("hvcC array header truncated".to_string()));
            }
            let flags = data[offset];
            if flags & 0x40 != 0 {
                return Err(invalid("hvcC array reserved bit is invalid".to_string()));
            }
            let count = u16::from_be_bytes([data[offset + 1], data[offset + 2]]);
            offset += 3;
            let mut units = Vec::new();
            for _ in 0..count {
                if data.len() - offset < 2 {
                    return Err(invalid("hvcC NAL length truncated".to_string()));
                }
                let length = usize::from(u16::from_be_bytes([data[offset], data[offset + 1]]));
                offset += 2;
                if length < 2 || length > data.len() - offset {
                    return Err(invalid(
                        "hvcC NAL exceeds box size or is too short".to_string(),
                    ));
                }
                if data[offset] & 0x80 != 0
                    || data[offset + 1] & 7 == 0
                    || (data[offset] >> 1) & 0x3f != flags & 0x3f
                {
                    return Err(invalid(
                        "hvcC NAL header does not match array type".to_string(),
                    ));
                }
                units.push(data[offset..offset + length].to_vec());
                offset += length;
            }
            arrays.push(HevcNalArray {
                complete: flags & 0x80 != 0,
                nal_type: flags & 0x3f,
                units,
            });
        }
        if offset != data.len() {
            return Err(invalid("hvcC has invalid trailing data".to_string()));
        }
        self.configuration = Some(HevcConfiguration {
            profile: data[1] & 0x1f,
            level: data[12],
            bit_depth_luma: (data[17] & 7) + 8,
            bit_depth_chroma: (data[18] & 7) + 8,
            nal_length_size,
            arrays,
        });
        Ok(())
    }
}
