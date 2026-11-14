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
        let mut writer = BitWriter::default();
        for value in [0, 2, 0] {
            writer.ue(value);
        }
        writer.bits(0, 4);
        writer.ue(u32::from(idr_pic_id));
        writer.bits(0, 2);
        writer.ue(0);
        writer.ue(1);
        for _ in 0..16 {
            writer.ue(25);
            writer.align();
            writer.data.extend_from_slice(&[128; 384]);
            writer.bit_count += 384 * 8;
        }
        let nal = writer.nal(0x65);
        let mut sample = (nal.len() as u32).to_be_bytes().to_vec();
        sample.extend(nal);
        sample
    }
}
