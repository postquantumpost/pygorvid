use crate::cabac::CabacContextModel;
use crate::cabac_init::CONTEXT_INIT_TABLE;

const RANGE_LPS: [[u8; 64]; 4] = [
    [
        128, 128, 128, 123, 116, 111, 105, 100, 95, 90, 85, 81, 77, 73, 69, 66, 62, 59, 56, 53, 51,
        48, 46, 43, 41, 39, 37, 35, 33, 32, 30, 29, 27, 26, 24, 23, 22, 21, 20, 19, 18, 17, 16, 15,
        14, 14, 13, 12, 12, 11, 11, 10, 10, 9, 9, 8, 8, 7, 7, 7, 6, 6, 6, 2,
    ],
    [
        176, 167, 158, 150, 142, 135, 128, 122, 116, 110, 104, 99, 94, 89, 85, 80, 76, 72, 69, 65,
        62, 59, 56, 53, 50, 48, 45, 43, 41, 39, 37, 35, 33, 31, 30, 28, 27, 26, 24, 23, 22, 21, 20,
        19, 18, 17, 16, 15, 14, 14, 13, 12, 12, 11, 11, 10, 9, 9, 9, 8, 8, 7, 7, 2,
    ],
    [
        208, 197, 187, 178, 169, 160, 152, 144, 137, 130, 123, 117, 111, 105, 100, 95, 90, 86, 81,
        77, 73, 69, 66, 63, 59, 56, 54, 51, 48, 46, 43, 41, 39, 37, 35, 33, 32, 30, 29, 27, 26, 25,
        23, 22, 21, 20, 19, 18, 17, 16, 15, 15, 14, 13, 12, 12, 11, 11, 10, 10, 9, 9, 8, 2,
    ],
    [
        240, 227, 216, 205, 195, 185, 175, 166, 158, 150, 142, 135, 128, 122, 116, 110, 104, 99,
        94, 89, 85, 80, 76, 72, 69, 65, 62, 59, 56, 53, 50, 48, 45, 43, 41, 39, 37, 35, 33, 31, 30,
        28, 27, 25, 24, 23, 22, 21, 20, 19, 18, 17, 16, 15, 14, 14, 13, 12, 12, 11, 11, 10, 9, 2,
    ],
];

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

struct CabacWriter {
    contexts: Vec<CabacContextModel>,
    low: u32,
    range: u32,
    bits_to_follow: usize,
    bits: Vec<u8>,
    first_bit: bool,
}

impl CabacWriter {
    fn new(slice_type: u8) -> Self {
        let column = if matches!(slice_type % 5, 2 | 4) {
            0
        } else {
            1
        };
        let contexts = CONTEXT_INIT_TABLE[column]
            .iter()
            .map(|(m, n)| CabacContextModel::new(i32::from(*m), i32::from(*n), 26).unwrap())
            .collect();
        Self::with_contexts(contexts)
    }

    fn with_contexts(contexts: Vec<CabacContextModel>) -> Self {
        Self {
            contexts,
            low: 0,
            range: 510,
            bits_to_follow: 0,
            bits: Vec::new(),
            first_bit: true,
        }
    }

    fn put(&mut self, bit: u8) {
        if self.first_bit {
            self.first_bit = false;
        } else {
            self.bits.push(bit);
        }
        while self.bits_to_follow != 0 {
            self.bits.push(bit ^ 1);
            self.bits_to_follow -= 1;
        }
    }

    fn renormalize(&mut self) {
        while self.range < 256 {
            if self.low < 256 {
                self.put(0);
            } else if self.low >= 512 {
                self.put(1);
                self.low -= 512;
            } else {
                self.bits_to_follow += 1;
                self.low -= 256;
            }
            self.low <<= 1;
            self.range <<= 1;
        }
    }

    fn decision(&mut self, value: bool, context_index: usize) {
        let model = &mut self.contexts[context_index];
        let range_lps =
            u32::from(RANGE_LPS[((self.range >> 6) & 3) as usize][model.state_index() as usize]);
        if value == model.mps() {
            self.range -= range_lps;
            model.update(value);
            self.renormalize();
            return;
        }
        self.low += self.range - range_lps;
        self.range = range_lps;
        model.update(value);
        self.renormalize();
    }

    fn luma_cbp(&mut self, left: u8, top: u8, desired: u8) {
        let mut context_index = usize::from(left & 0x02 == 0) + 2 * usize::from(top & 0x04 == 0);
        self.decision(desired & 1 != 0, 73 + context_index);
        let mut current = desired & 1;
        context_index = usize::from(current & 1 == 0) + 2 * usize::from(top & 0x08 == 0);
        self.decision((desired >> 1) & 1 != 0, 73 + context_index);
        current |= desired & 2;
        context_index = usize::from(left & 0x08 == 0) + 2 * usize::from(current & 1 == 0);
        self.decision((desired >> 2) & 1 != 0, 73 + context_index);
        current |= desired & 4;
        context_index = usize::from(current & 4 == 0) + 2 * usize::from(current & 2 == 0);
        self.decision((desired >> 3) & 1 != 0, 73 + context_index);
    }

    fn zero_luma_cbp(&mut self, left: u8, top: u8) {
        self.luma_cbp(left, top, 0);
    }

    fn bypass(&mut self, value: bool) {
        self.low <<= 1;
        if value {
            self.low += self.range;
        }
        if self.low >= 1024 {
            self.low -= 1024;
            self.put(1);
        } else if self.low < 512 {
            self.put(0);
        } else {
            self.low -= 512;
            self.bits_to_follow += 1;
        }
    }

    fn terminate_false(&mut self) {
        self.range -= 2;
        self.renormalize();
    }

    fn end(&mut self) {
        self.range -= 2;
        self.low += self.range;
        self.range = 2;
        self.renormalize();
        self.put(((self.low >> 9) & 1) as u8);
        self.bits.push(((self.low >> 8) & 1) as u8);
        self.bits.push(1);
    }

    fn data(&self, padding: usize) -> Vec<u8> {
        let mut result = vec![0; (self.bits.len() + 7) / 8];
        for (index, bit) in self.bits.iter().copied().enumerate() {
            if bit != 0 {
                result[index / 8] |= 0x80 >> (index % 8);
            }
        }
        result.resize(result.len() + padding, 0);
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
        self.picture_parameter_set_with(0, false, 0)
    }

    pub(crate) fn picture_parameter_set_with(
        &self,
        pps_id: u32,
        cabac: bool,
        weighted_bipred_idc: u32,
    ) -> Vec<u8> {
        let mut writer = BitWriter::default();
        writer.ue(pps_id);
        writer.ue(0);
        writer.bits(u32::from(cabac), 1);
        writer.bits(0, 1);
        for _ in 0..3 {
            writer.ue(0);
        }
        writer.bits(1, 1);
        writer.bits(weighted_bipred_idc, 2);
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
        if (21..=23).contains(&frame_index) {
            return self.encode_b_feature_frame(frame_index);
        }
        if frame_index == 24 {
            return self.encode_b_weighted_frame(true);
        }
        if frame_index == 25 {
            return self.encode_b_weighted_frame(false);
        }
        if frame_index == 26 {
            return self.encode_inter_residual_frame();
        }
        if frame_index == 27 {
            return self.encode_cabac_i_frame();
        }
        if frame_index == 28 {
            return self.encode_cabac_p_frame();
        }
        if frame_index == 29 {
            return self.encode_cabac_b_frame();
        }
        if frame_index == 30 {
            return self.encode_cabac_residual_frame();
        }
        self.encode_pcm_frame(frame_index, frame_index == 1)
    }

    fn sample(nal: Vec<u8>) -> Vec<u8> {
        let mut sample = (nal.len() as u32).to_be_bytes().to_vec();
        sample.extend(nal);
        sample
    }

    fn cabac_nal(header: u8, rbsp: &[u8]) -> Vec<u8> {
        let mut result = vec![header];
        let mut zeros = 0;
        for value in rbsp {
            if zeros == 2 && *value <= 3 {
                result.push(3);
                zeros = 0;
            }
            result.push(*value);
            zeros = if *value == 0 { zeros + 1 } else { 0 };
        }
        result
    }

    fn write_b_macroblock(writer: &mut BitWriter, kind: u32, sub_types: &[u32]) {
        writer.ue(kind);
        if kind == 22 {
            for sub_type in sub_types {
                writer.ue(*sub_type);
            }
            writer.ue(0);
            return;
        }
        if kind == 0 {
            writer.ue(0);
            return;
        }
        if (1..=3).contains(&kind) {
            for list_index in 0..2 {
                if (kind == 1 && list_index == 1) || (kind == 2 && list_index == 0) {
                    continue;
                }
                writer.se(0);
                writer.se(0);
            }
            writer.ue(0);
            return;
        }
        if (4..=21).contains(&kind) {
            let flags_by_kind = [
                [1, 0],
                [1, 0],
                [0, 1],
                [0, 1],
                [1, 0],
                [1, 0],
                [0, 1],
                [0, 1],
                [1, 1],
                [1, 1],
                [0, 1],
                [0, 1],
                [1, 1],
                [1, 1],
                [0, 1],
                [0, 1],
                [1, 1],
                [1, 1],
            ];
            for _ in 0..2 {
                for active in flags_by_kind[(kind - 4) as usize] {
                    if active != 0 {
                        writer.se(0);
                        writer.se(0);
                    }
                }
            }
            writer.ue(0);
        }
    }

    fn begin_b_slice(
        writer: &mut BitWriter,
        frame_num: u32,
        spatial_direct: bool,
        pps_id: u32,
        explicit_weights: bool,
    ) {
        writer.ue(0);
        writer.ue(1);
        writer.ue(pps_id);
        writer.bits(frame_num, 4);
        writer.bits(u32::from(spatial_direct), 1);
        writer.bits(0, 1);
        writer.bits(0, 1);
        writer.bits(0, 1);
        if explicit_weights {
            writer.ue(0);
            writer.ue(0);
            for _ in 0..2 {
                writer.bits(1, 1);
                writer.se(2);
                writer.se(-8);
                writer.bits(1, 1);
                writer.se(2);
                writer.se(4);
                writer.se(2);
                writer.se(-4);
            }
        }
        writer.bits(0, 1);
        writer.ue(0);
        writer.ue(1);
    }

    fn encode_b_feature_frame(&self, frame_index: u16) -> Vec<u8> {
        let mut writer = BitWriter::default();
        let spatial_direct = frame_index != 22;
        Self::begin_b_slice(
            &mut writer,
            u32::from((frame_index - 16) & 15),
            spatial_direct,
            0,
            false,
        );
        for macroblock in 0..16 {
            writer.ue(0);
            if frame_index == 23 {
                Self::write_b_macroblock(&mut writer, 1 + (macroblock % 11 % 3) as u32, &[]);
            } else if frame_index == 21 && macroblock % 2 != 0 {
                Self::write_b_macroblock(&mut writer, 22, &[0, 0, 0, 0]);
            } else {
                Self::write_b_macroblock(&mut writer, 0, &[]);
            }
        }
        Self::sample(writer.nal(0x21))
    }

    fn encode_b_weighted_frame(&self, explicit: bool) -> Vec<u8> {
        let mut writer = BitWriter::default();
        Self::begin_b_slice(
            &mut writer,
            if explicit { 9 } else { 10 },
            true,
            if explicit { 1 } else { 2 },
            explicit,
        );
        for _ in 0..16 {
            writer.ue(0);
            Self::write_b_macroblock(&mut writer, 3, &[]);
        }
        Self::sample(writer.nal(0x21))
    }

    fn encode_inter_residual_frame(&self) -> Vec<u8> {
        let mut writer = BitWriter::default();
        writer.ue(0);
        writer.ue(0);
        writer.ue(0);
        writer.bits(11, 4);
        writer.bits(0, 1);
        writer.bits(0, 1);
        Self::write_prediction_weight_table(&mut writer, 1, false);
        writer.bits(0, 1);
        writer.ue(0);
        writer.ue(1);
        for macroblock in 0..16 {
            writer.ue(0);
            writer.ue(0);
            writer.se(0);
            writer.se(0);
            if matches!(macroblock, 0 | 15) {
                writer.ue(2);
                writer.se(1);
                for block in 0..4 {
                    if matches!(block, 0 | 3) {
                        writer.bits(0b01, 2);
                        writer.bits(0, 1);
                        writer.bits(1, 1);
                    } else {
                        writer.bits(1, 1);
                    }
                }
            } else {
                writer.ue(0);
            }
        }
        Self::sample(writer.nal(0x21))
    }

    fn cabac_i_header(slice_type: u32, frame_num: u32, pps_id: u32) -> BitWriter {
        let mut header = BitWriter::default();
        header.ue(0);
        header.ue(slice_type);
        header.ue(pps_id);
        header.bits(frame_num, 4);
        header.bits(0, 1);
        header.ue(0);
        header.ue(1);
        while header.bit_count % 8 != 0 {
            header.bits(1, 1);
        }
        header
    }

    fn encode_cabac_i_frame(&self) -> Vec<u8> {
        let header = Self::cabac_i_header(2, 12, 3);
        let mut cabac = CabacWriter::new(2);
        for macroblock in 0..16 {
            cabac.decision(false, 3);
            for _ in 0..16 {
                cabac.decision(true, 68);
            }
            cabac.decision(false, 64);
            cabac.zero_luma_cbp(
                if macroblock % 4 == 0 { 15 } else { 0 },
                if macroblock < 4 { 15 } else { 0 },
            );
            cabac.decision(false, 77);
            if macroblock != 15 {
                cabac.terminate_false();
            }
        }
        cabac.end();
        let mut rbsp = header.data;
        rbsp.extend(cabac.data(4));
        Self::sample(Self::cabac_nal(0x21, &rbsp))
    }

    fn encode_cabac_p_frame(&self) -> Vec<u8> {
        let mut header = BitWriter::default();
        header.ue(0);
        header.ue(0);
        header.ue(3);
        header.bits(13, 4);
        header.bits(0, 1);
        header.bits(0, 1);
        header.ue(0);
        header.ue(0);
        header.bits(0, 1);
        header.bits(0, 1);
        header.bits(0, 1);
        header.ue(0);
        header.ue(0);
        header.ue(1);
        while header.bit_count % 8 != 0 {
            header.bits(1, 1);
        }
        let mut cabac = CabacWriter::new(0);
        for macroblock in 0..16 {
            let skipped = macroblock % 3 == 1;
            let left_skip = macroblock % 4 != 0 && (macroblock - 1) % 3 == 1;
            let top_skip = macroblock >= 4 && (macroblock - 4) % 3 == 1;
            let increment = usize::from(macroblock % 4 != 0 && !left_skip)
                + usize::from(macroblock >= 4 && !top_skip);
            cabac.decision(skipped, 11 + increment);
            if skipped {
                if macroblock != 15 {
                    cabac.terminate_false();
                }
                continue;
            }
            cabac.decision(false, 14);
            cabac.decision(false, 15);
            cabac.decision(false, 16);
            cabac.decision(false, 40);
            cabac.decision(false, 47);
            cabac.zero_luma_cbp(
                if macroblock % 4 == 0 { 15 } else { 0 },
                if macroblock < 4 { 15 } else { 0 },
            );
            cabac.decision(false, 77);
            if macroblock != 15 {
                cabac.terminate_false();
            }
        }
        cabac.end();
        let mut rbsp = header.data;
        rbsp.extend(cabac.data(4));
        Self::sample(Self::cabac_nal(0x21, &rbsp))
    }

    fn encode_cabac_b_frame(&self) -> Vec<u8> {
        let mut header = BitWriter::default();
        header.ue(0);
        header.ue(1);
        header.ue(3);
        header.bits(14, 4);
        header.bits(1, 1);
        for _ in 0..4 {
            header.bits(0, 1);
        }
        header.ue(0);
        header.ue(0);
        header.ue(1);
        while header.bit_count % 8 != 0 {
            header.bits(1, 1);
        }
        let mut cabac = CabacWriter::new(1);
        for macroblock in 0..16 {
            cabac.decision(
                false,
                24 + usize::from(macroblock % 4 != 0) + usize::from(macroblock >= 4),
            );
            cabac.decision(false, 27);
            cabac.zero_luma_cbp(
                if macroblock % 4 == 0 { 15 } else { 0 },
                if macroblock < 4 { 15 } else { 0 },
            );
            cabac.decision(false, 77);
            if macroblock != 15 {
                cabac.terminate_false();
            }
        }
        cabac.end();
        let mut rbsp = header.data;
        rbsp.extend(cabac.data(4));
        Self::sample(Self::cabac_nal(0x21, &rbsp))
    }

    fn encode_cabac_residual_frame(&self) -> Vec<u8> {
        let header = Self::cabac_i_header(2, 15, 3);
        let mut cabac = CabacWriter::new(2);
        for macroblock in 0..16 {
            cabac.decision(false, 3);
            for _ in 0..16 {
                cabac.decision(true, 68);
            }
            cabac.decision(false, 64);
            cabac.luma_cbp(
                if macroblock % 4 == 0 { 15 } else { 0 },
                if macroblock < 4 { 15 } else { 0 },
                if macroblock == 15 { 1 } else { 0 },
            );
            cabac.decision(false, 77);
            if macroblock == 15 {
                cabac.decision(false, 60);
                cabac.decision(true, 93);
                cabac.decision(true, 134);
                cabac.decision(true, 195);
                cabac.decision(false, 248);
                cabac.bypass(false);
                cabac.decision(false, 94);
                cabac.decision(false, 95);
                cabac.decision(false, 93);
            }
            if macroblock != 15 {
                cabac.terminate_false();
            }
        }
        cabac.end();
        let mut rbsp = header.data;
        rbsp.extend(cabac.data(4));
        Self::sample(Self::cabac_nal(0x21, &rbsp))
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
