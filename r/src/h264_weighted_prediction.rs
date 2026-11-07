use std::io::{self, ErrorKind};

use crate::{BitReader, SpsInfo};

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct PredictionWeight {
    pub weights: [i16; 3],
    pub offsets: [i16; 3],
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct PredictionWeightTable {
    pub denominators: [u8; 3],
    pub lists: [Vec<PredictionWeight>; 2],
}

impl PredictionWeightTable {
    pub(crate) fn read(
        reader: &mut BitReader<'_>,
        sps: &SpsInfo,
        maxima: [u32; 2],
        b_slice: bool,
    ) -> io::Result<Self> {
        let invalid = |message| io::Error::new(ErrorKind::InvalidData, message);
        let luma = reader.read_ue()?;
        if luma > 7 {
            return Err(invalid("luma_log2_weight_denom exceeds 7"));
        }
        let chroma_present = sps.chroma_format_idc != 0 && !sps.separate_colour_plane;
        let chroma = if chroma_present { reader.read_ue()? } else { 0 };
        if chroma > 7 {
            return Err(invalid("chroma_log2_weight_denom exceeds 7"));
        }
        let mut table = Self {
            denominators: [luma as u8, chroma as u8, chroma as u8],
            lists: [Vec::new(), Vec::new()],
        };
        for list in 0..if b_slice { 2 } else { 1 } {
            if maxima[list] > 31 {
                return Err(invalid("prediction weight reference maximum exceeds 31"));
            }
            for _ in 0..=maxima[list] {
                let mut entry = PredictionWeight {
                    weights: [1 << luma, 1 << chroma, 1 << chroma],
                    offsets: [0; 3],
                };
                let luma_present = reader.read_bit()?;
                if luma_present {
                    read_component(reader, &mut entry, 0)?;
                }
                if chroma_present && reader.read_bit()? {
                    read_component(reader, &mut entry, 1)?;
                    read_component(reader, &mut entry, 2)?;
                }
                table.lists[list].push(entry);
            }
        }
        Ok(table)
    }
}

fn read_component(
    reader: &mut BitReader<'_>,
    entry: &mut PredictionWeight,
    component: usize,
) -> io::Result<()> {
    let weight = reader.read_se()?;
    let offset = reader.read_se()?;
    if !(-128..=127).contains(&weight) || !(-128..=127).contains(&offset) {
        return Err(io::Error::new(
            ErrorKind::InvalidData,
            "prediction weight or offset exceeds [-128,127]",
        ));
    }
    entry.weights[component] = weight as i16;
    entry.offsets[component] = offset as i16;
    Ok(())
}

#[derive(Clone, Debug, Default)]
pub struct InterWeighting {
    pub table: Option<PredictionWeightTable>,
    pub implicit: bool,
    pub current_picture_order_count: i64,
}

#[derive(Clone, Copy, Debug)]
pub(crate) struct SampleWeights {
    denominators: [u8; 3],
    lists: [PredictionWeight; 2],
}

impl InterWeighting {
    pub(crate) fn select(
        &self,
        indices: [i32; 2],
        references: [Option<&crate::CabacBReferencePicture>; 2],
    ) -> io::Result<Option<SampleWeights>> {
        if let Some(table) = &self.table {
            let mut lists = [PredictionWeight {
                weights: [1; 3],
                offsets: [0; 3],
            }; 2];
            for list in 0..2 {
                if indices[list] >= 0 {
                    lists[list] =
                        *table.lists[list]
                            .get(indices[list] as usize)
                            .ok_or_else(|| {
                                io::Error::new(
                                    ErrorKind::InvalidData,
                                    "prediction weight reference is unavailable",
                                )
                            })?;
                }
            }
            return Ok(Some(SampleWeights {
                denominators: table.denominators,
                lists,
            }));
        }
        if self.implicit {
            if let [Some(first), Some(second)] = references {
                let td = second
                    .picture_order_count
                    .saturating_sub(first.picture_order_count)
                    .clamp(-128, 127);
                let mut w1 = 32;
                if td != 0 && !first.long_term && !second.long_term {
                    let tb = self
                        .current_picture_order_count
                        .saturating_sub(first.picture_order_count)
                        .clamp(-128, 127);
                    let tx = (16_384 + (td / 2).abs()) / td;
                    let candidate = ((tb * tx + 32) >> 6).clamp(-1024, 1023) >> 2;
                    if (-64..=128).contains(&candidate) {
                        w1 = candidate as i16;
                    }
                }
                return Ok(Some(SampleWeights {
                    denominators: [5; 3],
                    lists: [
                        PredictionWeight {
                            weights: [64 - w1; 3],
                            offsets: [0; 3],
                        },
                        PredictionWeight {
                            weights: [w1; 3],
                            offsets: [0; 3],
                        },
                    ],
                }));
            }
        }
        Ok(None)
    }
}

pub(crate) fn combine(
    first: Option<u8>,
    second: Option<u8>,
    weights: Option<SampleWeights>,
    component: usize,
) -> io::Result<u8> {
    let Some(weights) = weights else {
        return match (first, second) {
            (Some(a), Some(b)) => Ok(((u16::from(a) + u16::from(b) + 1) >> 1) as u8),
            (Some(a), None) | (None, Some(a)) => Ok(a),
            (None, None) => Err(io::Error::new(
                ErrorKind::InvalidData,
                "inter partition has no active prediction list",
            )),
        };
    };
    let denominator = weights.denominators[component];
    let [a, b] = weights.lists;
    let value = match (first, second) {
        (Some(first), Some(second)) => {
            ((i32::from(first) * i32::from(a.weights[component])
                + i32::from(second) * i32::from(b.weights[component])
                + (1 << denominator))
                >> (denominator + 1))
                + ((i32::from(a.offsets[component]) + i32::from(b.offsets[component]) + 1) >> 1)
        }
        (Some(sample), None) | (None, Some(sample)) => {
            let entry = if first.is_some() { a } else { b };
            let rounding = if denominator == 0 {
                0
            } else {
                1 << (denominator - 1)
            };
            ((i32::from(sample) * i32::from(entry.weights[component]) + rounding) >> denominator)
                + i32::from(entry.offsets[component])
        }
        (None, None) => {
            return Err(io::Error::new(
                ErrorKind::InvalidData,
                "inter partition has no active prediction list",
            ))
        }
    };
    Ok(value.clamp(0, 255) as u8)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn weighted_samples_round_offset_and_clip_after_combining() {
        let weights = SampleWeights {
            denominators: [1, 0, 7],
            lists: [
                PredictionWeight {
                    weights: [3, -1, 128],
                    offsets: [-10, 127, 0],
                },
                PredictionWeight {
                    weights: [-1, 1, 128],
                    offsets: [3, -128, 0],
                },
            ],
        };
        assert_eq!(combine(Some(101), None, Some(weights), 0).unwrap(), 142);
        assert_eq!(
            combine(Some(200), Some(100), Some(weights), 0).unwrap(),
            122
        );
        assert_eq!(combine(None, Some(100), Some(weights), 0).unwrap(), 0);
        assert_eq!(combine(Some(200), None, Some(weights), 1).unwrap(), 0);
        assert_eq!(combine(Some(255), None, Some(weights), 2).unwrap(), 255);
        assert_eq!(
            combine(Some(255), Some(255), Some(weights), 2).unwrap(),
            255
        );
        assert_eq!(combine(Some(200), Some(100), None, 0).unwrap(), 150);
        assert!(combine(None, None, None, 0).is_err());
    }

    #[test]
    fn implicit_weights_use_poc_and_default_for_long_term_or_single_list() {
        let frame = std::sync::Arc::new(crate::Yuv420Frame {
            width: 16,
            height: 16,
            y_stride: 16,
            u_stride: 8,
            v_stride: 8,
            y: vec![0; 256],
            u: vec![0; 64],
            v: vec![0; 64],
        });
        let first = crate::CabacBReferencePicture {
            frame: frame.clone(),
            picture_order_count: 0,
            long_term: false,
        };
        let mut second = crate::CabacBReferencePicture {
            frame,
            picture_order_count: 8,
            long_term: false,
        };
        let weighting = InterWeighting {
            table: None,
            implicit: true,
            current_picture_order_count: 2,
        };
        let selected = weighting
            .select([0, 0], [Some(&first), Some(&second)])
            .unwrap();
        assert_eq!(combine(Some(100), Some(200), selected, 0).unwrap(), 125);
        second.long_term = true;
        let selected = weighting
            .select([0, 0], [Some(&first), Some(&second)])
            .unwrap();
        assert_eq!(combine(Some(100), Some(200), selected, 0).unwrap(), 150);
        let selected = weighting.select([0, -1], [Some(&first), None]).unwrap();
        assert_eq!(combine(Some(100), None, selected, 0).unwrap(), 100);
    }
}
