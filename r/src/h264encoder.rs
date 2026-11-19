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
        writer.bits(100, 8);
        writer.bits(0, 8);
        writer.bits(20, 8);
        writer.ue(0);
        for value in [1, 0, 0] {
            writer.ue(value);
        }
        writer.bits(0, 1);
        writer.bits(0, 1);
        for value in [0, 2, 2] {
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
        writer.bits(0b100, 3);
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
            return self.encode_intra4x4_directional_frame(5, false, false, false);
        }
        if frame_index == 7 {
            return self.encode_intra4x4_directional_frame(6, true, false, false);
        }
        if frame_index == 8 {
            return self.encode_intra4x4_directional_frame(7, true, true, false);
        }
        if frame_index == 9 {
            return self.encode_intra4x4_directional_frame(8, true, true, true);
        }
        if (10..=19).contains(&frame_index) {
            return self.encode_p_frame(frame_index);
        }
        if frame_index == 20 {
            return self.encode_b_frame();
        }
        self.encode_pcm_frame(frame_index, frame_index == 1)
    }

    fn write_prediction_weight_table(
        writer: &mut BitWriter,
        reference_count: usize,
        explicit: bool,
    ) {
        writer.ue(0);
        writer.ue(0);
        for _ in 0..reference_count {
            if explicit {
                writer.bits(1, 1);
                writer.se(2);
                writer.se(-16);
                writer.bits(1, 1);
                writer.se(2);
                writer.se(8);
                writer.se(2);
                writer.se(-8);
            } else {
                writer.bits(0, 1);
                writer.bits(0, 1);
            }
        }
    }

    fn write_p_macroblock(
        writer: &mut BitWriter,
        kind: u32,
        vectors: &[(i32, i32)],
        references: &[usize],
        multiple_references: bool,
        sub_types: &[u32],
    ) {
        writer.ue(kind);
        if matches!(kind, 3 | 4) {
            for sub_type in sub_types {
                writer.ue(*sub_type);
            }
        }
        for (partition, (x, y)) in vectors.iter().enumerate() {
            if multiple_references && kind != 4 {
                writer.bits(u32::from(references[partition] == 0), 1);
            }
            writer.se(*x);
            writer.se(*y);
        }
        writer.ue(0);
    }

    fn encode_p_frame(&self, frame_index: u16) -> Vec<u8> {
        let mut writer = BitWriter::default();
        writer.ue(0);
        writer.ue(0);
        writer.ue(0);
        writer.bits(u32::from((frame_index - 1) & 15), 4);
        let multiple_references = matches!(frame_index, 16 | 17);
        writer.bits(u32::from(multiple_references), 1);
        let reference_count = if multiple_references {
            writer.ue(1);
            2
        } else {
            1
        };
        let reordered = frame_index == 17;
        writer.bits(u32::from(reordered), 1);
        if reordered {
            writer.ue(0);
            writer.ue(1);
            writer.ue(3);
        }
        Self::write_prediction_weight_table(&mut writer, reference_count, frame_index == 18);
        writer.bits(u32::from(reordered), 1);
        if reordered {
            writer.ue(1);
            writer.ue(0);
            writer.ue(0);
        }
        writer.ue(0);
        writer.ue(1);

        match frame_index {
            10 => {
                for _ in 0..16 {
                    writer.ue(0);
                    Self::write_p_macroblock(&mut writer, 0, &[(0, 0)], &[], false, &[]);
                }
            }
            11 => {
                writer.ue(0);
                Self::write_p_macroblock(&mut writer, 0, &[(0, 0)], &[], false, &[]);
                writer.ue(14);
                Self::write_p_macroblock(&mut writer, 0, &[(0, 0)], &[], false, &[]);
            }
            12 => {
                let motion = [(0, 0), (4, 0), (-4, 0), (0, 4), (0, -4)];
                for macroblock in 0..16 {
                    writer.ue(0);
                    Self::write_p_macroblock(
                        &mut writer,
                        0,
                        &[motion[macroblock % motion.len()]],
                        &[],
                        false,
                        &[],
                    );
                }
            }
            13 => {
                for macroblock in 0..16 {
                    writer.ue(0);
                    Self::write_p_macroblock(
                        &mut writer,
                        1 + (macroblock % 2) as u32,
                        &[(4, 0), (-4, 4)],
                        &[],
                        false,
                        &[],
                    );
                }
            }
            14 => {
                let sub_types = [0_u32, 1, 2, 3];
                let counts = [1, 2, 2, 4];
                for macroblock in 0..16 {
                    writer.ue(0);
                    let mut vectors = Vec::with_capacity(9);
                    for (sub_index, sub_type) in sub_types.iter().enumerate() {
                        for part in 0..counts[*sub_type as usize] {
                            vectors.push(((sub_index as i32 - 1) * 4, (part as i32 - 1) * 4));
                        }
                    }
                    Self::write_p_macroblock(
                        &mut writer,
                        3 + (macroblock % 2) as u32,
                        &vectors,
                        &[],
                        false,
                        &sub_types,
                    );
                }
            }
            15 => {
                let motion = [(1, 0), (-1, 2), (2, -1), (-2, 1)];
                for macroblock in 0..16 {
                    writer.ue(0);
                    Self::write_p_macroblock(
                        &mut writer,
                        0,
                        &[motion[macroblock % 4]],
                        &[],
                        false,
                        &[],
                    );
                }
            }
            16 | 17 => {
                for macroblock in 0..16 {
                    writer.ue(0);
                    Self::write_p_macroblock(
                        &mut writer,
                        0,
                        &[(0, 0)],
                        &[macroblock % 2],
                        true,
                        &[],
                    );
                }
            }
            19 => {
                for _ in 0..16 {
                    writer.ue(0);
                    Self::write_p_macroblock(&mut writer, 0, &[(4, 0)], &[], false, &[]);
                }
            }
            _ => {
                for _ in 0..16 {
                    writer.ue(0);
                    Self::write_p_macroblock(&mut writer, 0, &[(0, 0)], &[], false, &[]);
                }
            }
        }
        let nal = writer.nal(0x21);
        let mut sample = (nal.len() as u32).to_be_bytes().to_vec();
        sample.extend(nal);
        sample
    }

    fn encode_b_frame(&self) -> Vec<u8> {
        let mut writer = BitWriter::default();
        writer.ue(0);
        writer.ue(1);
        writer.ue(0);
        writer.bits(2, 4);
        writer.bits(1, 1);
        writer.bits(0, 1);
        writer.bits(0, 1);
        writer.bits(0, 1);
        writer.ue(0);
        writer.ue(1);
        for macroblock in 0..16 {
            writer.ue(0);
            writer.ue(1 + (macroblock % 3) as u32);
            for _ in 0..if macroblock % 3 == 2 { 2 } else { 1 } {
                writer.se(0);
                writer.se(0);
            }
            writer.ue(0);
        }
        let nal = writer.nal(0x01);
        let mut sample = (nal.len() as u32).to_be_bytes().to_vec();
        sample.extend(nal);
        sample
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

    fn encode_intra4x4_directional_frame(
        &self,
        frame_num: u32,
        residual: bool,
        chroma_residual: bool,
        expanded_cavlc: bool,
    ) -> Vec<u8> {
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
                writer.ue(if chroma_residual { 42 } else { 29 });
                writer.se(1);
                if expanded_cavlc {
                    // nC = 0: sixteen alternating +2/-2 coefficients.
                    writer.bits(0b0000_0000_0000_0100, 16);
                    for index in 0..16 {
                        writer.bits(if index % 2 == 0 { 2 } else { 3 }, 2);
                    }
                    // nC >= 8: three trailing ones and two total zeros.
                    writer.bits(0b001011, 6);
                    writer.bits(0b010, 3);
                    writer.bits(0b110, 3);
                    writer.bits(0b010, 3);
                    // nC >= 8 followed by nC = 2 select different empty-block tables.
                    writer.bits(0b000011, 6);
                    writer.bits(0b11, 2);
                } else {
                    // Positive trailing-one token, zero runs, then three empty blocks.
                    writer.bits(0b01, 2);
                    writer.bits(0, 1);
                    writer.bits(1, 1);
                    writer.bits(1, 1);
                    writer.bits(1, 1);
                    writer.bits(1, 1);
                }
                if chroma_residual {
                    // Cb DC +1 and Cr DC -1, each at the sole 2x2 DC coefficient.
                    writer.bits(0b101, 3);
                    writer.bits(0b111, 3);
                    // One Cb and one Cr AC coefficient; remaining AC blocks are empty.
                    for sign in [0, 1] {
                        writer.bits(0b01, 2);
                        writer.bits(sign, 1);
                        writer.bits(1, 1);
                        writer.bits(0b111, 3);
                    }
                }
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
