use crate::mp4boxes::{invalid, BoxHeader, Mp4Box, ReadSeek};
use std::io::{self, SeekFrom};

pub(crate) fn new_box(header: BoxHeader) -> Box<dyn Mp4Box> {
    if header.kind == *b"av01" {
        Box::new(crate::mp4boxes::VisualSampleEntryBox::new(header))
    } else {
        Box::new(Av1CBox {
            header,
            configuration: None,
        })
    }
}

pub(crate) fn sample_configuration(entry: &dyn Mp4Box) -> io::Result<Av1Configuration> {
    let configurations: Vec<_> = entry
        .children()
        .iter()
        .filter_map(|child| child.av1_configuration())
        .collect();
    if configurations.len() != 1 {
        return Err(invalid(
            "av01 sample entry must contain one av1C box".to_string(),
        ));
    }
    Ok(configurations[0].clone())
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Av1Configuration {
    pub profile: u8,
    pub level: u8,
    pub tier: u8,
    pub bit_depth: u8,
    pub monochrome: bool,
    pub chroma_subsampling_x: bool,
    pub chroma_subsampling_y: bool,
    pub chroma_sample_position: u8,
    pub config_obus: Vec<Vec<u8>>,
}

pub struct Av1CBox {
    pub(crate) header: BoxHeader,
    pub(crate) configuration: Option<Av1Configuration>,
}

impl Mp4Box for Av1CBox {
    fn header(&self) -> &BoxHeader {
        &self.header
    }

    fn av1_configuration(&self) -> Option<&Av1Configuration> {
        self.configuration.as_ref()
    }
    fn read(&mut self, r: &mut dyn ReadSeek) -> io::Result<()> {
        let size = usize::try_from(self.header.end - self.header.payload_start())
            .map_err(|_| invalid("av1C box too large".to_string()))?;
        if size < 4 {
            return Err(invalid("av1C box too short".to_string()));
        }
        r.seek(SeekFrom::Start(self.header.payload_start()))?;
        let mut data = vec![0; size];
        r.read_exact(&mut data)?;
        if data[0] != 0x81 {
            return Err(invalid(
                "invalid AV1DecoderConfigurationRecord header".to_string(),
            ));
        }
        if data[3] & 0xe0 != 0 {
            return Err(invalid("av1C reserved bits are invalid".to_string()));
        }
        let high_bitdepth = data[2] & 0x40 != 0;
        let twelve_bit = data[2] & 0x20 != 0;
        self.configuration = Some(Av1Configuration {
            profile: data[1] >> 5,
            level: data[1] & 0x1f,
            tier: (data[2] >> 7) & 1,
            bit_depth: if twelve_bit {
                12
            } else if high_bitdepth {
                10
            } else {
                8
            },
            monochrome: data[2] & 0x10 != 0,
            chroma_subsampling_x: data[2] & 0x08 != 0,
            chroma_subsampling_y: data[2] & 0x04 != 0,
            chroma_sample_position: data[2] & 0x03,
            config_obus: if size > 4 {
                vec![data[4..].to_vec()]
            } else {
                Vec::new()
            },
        });
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::mp4boxes::read_boxes;

    #[test]
    fn parses_av1_sample_entry_and_configuration() {
        let configuration = [0x81, 0x13, 0x0c, 0x00, 0x0a, 0x0a];
        let mut bytes = ((8 + 78 + 8 + configuration.len()) as u32)
            .to_be_bytes()
            .to_vec();
        bytes.extend_from_slice(b"av01");
        bytes.extend_from_slice(&[0; 78]);
        bytes.extend_from_slice(&((8 + configuration.len()) as u32).to_be_bytes());
        bytes.extend_from_slice(b"av1C");
        bytes.extend_from_slice(&configuration);
        let boxes = read_boxes(
            &mut std::io::Cursor::new(bytes.clone()),
            0,
            bytes.len() as u64,
        )
        .unwrap();
        let parsed = sample_configuration(&*boxes[0]).unwrap();
        assert_eq!((parsed.profile, parsed.level, parsed.bit_depth), (0, 19, 8));
        assert!(parsed.chroma_subsampling_x);
        assert!(parsed.chroma_subsampling_y);
        assert_eq!(parsed.config_obus, vec![vec![0x0a, 0x0a]]);
    }
}
