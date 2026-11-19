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
            let index = self.data.len() - 1;
            self.data[index] |= (((value >> shift) & 1) as u8) << (7 - self.bit_count % 8);
            self.bit_count += 1;
        }
    }

    fn ue(&mut self, value: u32) {
        let count = (32 - (value + 1).leading_zeros()) as usize;
        self.bits(0, count - 1);
        self.bits(value + 1, count);
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

/// Initial subset: 64x64 grey, 8-bit YUV420 Baseline I_PCM IDR pictures.
#[derive(Default)]
pub struct H264Encoder;

impl H264Encoder {
    pub fn new() -> Self {
        Self
    }

    pub fn sequence_parameter_set(&self) -> Vec<u8> {
        let mut writer = BitWriter::default();
        writer.bits(66, 8);
        writer.bits(0xc0, 8);
        writer.bits(20, 8);
        for value in [0, 0, 2, 1] {
            writer.ue(value);
        }
        writer.bits(0, 1);
        writer.ue(3);
        writer.ue(3);
        writer.bits(0b1100, 4);
        writer.nal(0x67)
    }

    pub fn picture_parameter_set(&self) -> Vec<u8> {
        let mut writer = BitWriter::default();
        writer.ue(0);
        writer.ue(0);
        writer.bits(0, 2);
        for _ in 0..3 {
            writer.ue(0);
        }
        writer.bits(0, 3);
        for _ in 0..3 {
            writer.ue(0);
        }
        writer.bits(0b100, 3);
        writer.nal(0x68)
    }

    pub fn encode_grey_frame(&self, idr_pic_id: u16) -> Vec<u8> {
        self.encode_pcm_frame(idr_pic_id, false)
    }

    pub fn encode_generated_frame(&self, frame_index: u16) -> Vec<u8> {
        if frame_index == 3 {
            return self.encode_intra16x16_dc_frame();
        }
        if frame_index == 4 {
            return self.encode_intra16x16_prediction_modes_frame();
        }
        if frame_index == 5 {
            return self.encode_intra4x4_dc_frame();
        }
        if frame_index == 6 {
            return self.encode_intra4x4_directional_frame(5, false);
        }
        if frame_index == 7 {
            return self.encode_intra4x4_directional_frame(6, true);
        }
        self.encode_pcm_frame(frame_index, frame_index == 1)
    }

    fn encode_intra16x16_dc_frame(&self) -> Vec<u8> {
        let mut writer = BitWriter::default();
        for value in [0, 2, 0] {
            writer.ue(value);
        }
        writer.bits(2, 4);
        writer.bits(0, 1);
        writer.ue(0);
        writer.ue(1);
        for _ in 0..16 {
            writer.ue(3);
            writer.ue(0);
            writer.ue(0);
            writer.bits(1, 1);
        }
        let nal = writer.nal(0x21);
        let mut sample = (nal.len() as u32).to_be_bytes().to_vec();
        sample.extend(nal);
        sample
    }

    fn encode_intra16x16_prediction_modes_frame(&self) -> Vec<u8> {
        let mut writer = BitWriter::default();
        for value in [0, 2, 0] {
            writer.ue(value);
        }
        writer.bits(3, 4);
        writer.bits(0, 1);
        writer.ue(0);
        writer.ue(1);
        let luma_modes = [2, 2, 2, 2, 0, 3, 2, 1, 0, 0, 1, 2, 0, 3, 2, 1];
        let chroma_modes = [0, 1, 1, 1, 2, 3, 0, 1, 2, 2, 3, 0, 2, 3, 2, 1];
        for (index, (luma_mode, chroma_mode)) in
            luma_modes.into_iter().zip(chroma_modes).enumerate()
        {
            writer.ue(1 + luma_mode + if index == 0 { 8 } else { 0 });
            writer.ue(chroma_mode);
            writer.ue(0);
            if index == 0 {
                writer.bits(0b0100_0011, 8);
                writer.bits(0b1_0000, 5);
                writer.bits(0b1_0000, 5);
                writer.bits(0xff, 8);
            } else {
                writer.bits(1, 1);
            }
        }
        let nal = writer.nal(0x21);
        let mut sample = (nal.len() as u32).to_be_bytes().to_vec();
        sample.extend(nal);
        sample
    }

    fn encode_intra4x4_dc_frame(&self) -> Vec<u8> {
        let mut writer = BitWriter::default();
        for value in [0, 2, 0] {
            writer.ue(value);
        }
        writer.bits(4, 4);
        writer.bits(0, 1);
        writer.ue(0);
        writer.ue(1);
        for _ in 0..16 {
            writer.ue(0);
            for _ in 0..16 {
                writer.bits(1, 1);
            }
            writer.ue(0);
            writer.ue(3);
        }
        let nal = writer.nal(0x21);
        let mut sample = (nal.len() as u32).to_be_bytes().to_vec();
        sample.extend(nal);
        sample
    }

    fn encode_intra4x4_directional_frame(&self, frame_num: u32, residual: bool) -> Vec<u8> {
        let mut writer = BitWriter::default();
        for value in [0, 2, 0] {
            writer.ue(value);
        }
        writer.bits(frame_num, 4);
        writer.bits(0, 1);
        writer.ue(0);
        writer.ue(1);

        writer.ue(25);
        writer.align();
        for row in 0..16 {
            for column in 0..16 {
                writer.data.push((32 + 3 * column + 5 * row) as u8);
            }
        }
        for base in [80, 176] {
            for row in 0..8 {
                for column in 0..8 {
                    writer.data.push((base + column + 2 * row) as u8);
                }
            }
        }
        writer.bit_count += 384 * 8;

        let modes = [2, 2, 2, 4, 8, 1, 5, 6, 7, 0, 3, 4, 5, 7, 6, 8];
        let scan = [0, 1, 4, 5, 2, 3, 6, 7, 8, 9, 12, 13, 10, 11, 14, 15];
        let scan_index = |raster: usize| scan.iter().position(|value| *value == raster).unwrap();
        let mut macroblock_modes = vec![[2; 16]];
        for macroblock in 1..16 {
            writer.ue(0);
            let mb_x = macroblock % 4;
            let mb_y = macroblock / 4;
            for (block, desired) in modes.into_iter().enumerate() {
                let raster = scan[block];
                let column = raster % 4;
                let row = raster / 4;
                let left_mode = if column != 0 {
                    Some(modes[scan_index(raster - 1)])
                } else if mb_x != 0 {
                    Some(macroblock_modes[macroblock - 1][scan_index(row * 4 + 3)])
                } else {
                    None
                };
                let top_mode = if row != 0 {
                    Some(modes[scan_index(raster - 4)])
                } else if mb_y != 0 {
                    Some(macroblock_modes[macroblock - 4][scan_index(12 + column)])
                } else {
                    None
                };
                let predicted = match (left_mode, top_mode) {
                    (Some(left), Some(top)) => left.min(top),
                    _ => 2,
                };
                if desired == predicted {
                    writer.bits(1, 1);
                } else {
                    writer.bits(0, 1);
                    writer.bits((desired - usize::from(desired >= predicted)) as u32, 3);
                }
            }
            writer.ue(0);
            if residual && macroblock == 15 {
                writer.ue(29);
                writer.se(1);
                // Positive trailing-one token, zero runs, then three empty blocks.
                writer.bits(0b01, 2);
                writer.bits(0, 1);
                writer.bits(1, 1);
                writer.bits(1, 1);
                writer.bits(1, 1);
                writer.bits(1, 1);
            } else {
                writer.ue(3);
            }
            macroblock_modes.push(modes);
        }
        let nal = writer.nal(0x21);
        let mut sample = (nal.len() as u32).to_be_bytes().to_vec();
        sample.extend(nal);
        sample
    }

    fn encode_pcm_frame(&self, idr_pic_id: u16, emulation_prevention_test: bool) -> Vec<u8> {
        let mut writer = BitWriter::default();
        for value in [0, 2, 0] {
            writer.ue(value);
        }
        let non_idr_reference = idr_pic_id == 2;
        if non_idr_reference {
            writer.bits(1, 4);
            writer.bits(0, 1);
        } else {
            writer.bits(0, 4);
            writer.ue(u32::from(idr_pic_id));
            writer.bits(0, 2);
        }
        writer.ue(0);
        writer.ue(1);
        for macroblock in 0..16 {
            writer.ue(25);
            writer.align();
            if emulation_prevention_test && macroblock == 0 {
                writer.data.extend_from_slice(&[0, 0, 1]);
                writer.data.extend_from_slice(&[128; 253]);
            } else {
                writer.data.extend_from_slice(&[128; 256]);
            }
            writer.data.extend_from_slice(&[128; 128]);
            writer.bit_count += 384 * 8;
        }
        let nal = writer.nal(if non_idr_reference { 0x21 } else { 0x65 });
        let mut sample = (nal.len() as u32).to_be_bytes().to_vec();
        sample.extend(nal);
        sample
    }
}
