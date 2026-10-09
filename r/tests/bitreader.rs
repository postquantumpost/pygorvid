use pygorvid::BitReader;
use std::io::ErrorKind;

fn pack_bits(bits: &str) -> Vec<u8> {
    let mut data = vec![0u8; (bits.len() + 7) / 8];
    for (index, bit) in bits.bytes().enumerate() {
        if bit == b'1' {
            data[index / 8] |= 1 << (7 - index % 8);
        }
    }
    data
}

#[test]
fn reads_bounded_bits_and_aligns_to_byte() {
    let data = [0b1011_0011, 0b0110_1010];
    let mut reader = BitReader::new(&data);
    assert_eq!(reader.read_bits(3).unwrap(), 5);
    reader.align_to_byte();
    assert_eq!(reader.read_bits(8).unwrap(), 0x6a);
    assert_eq!(reader.read_bits(0).unwrap(), 0);
    assert_eq!(
        reader.read_bits(1).unwrap_err().kind(),
        ErrorKind::UnexpectedEof
    );
}

#[test]
fn invalid_and_truncated_reads_do_not_consume_bits() {
    let data = [0xa5];
    let mut reader = BitReader::new(&data);
    assert_eq!(
        reader.read_bits(33).unwrap_err().kind(),
        ErrorKind::InvalidInput
    );
    assert_eq!(
        reader.read_bits(9).unwrap_err().kind(),
        ErrorKind::UnexpectedEof
    );
    assert_eq!(reader.read_bits(8).unwrap(), 0xa5);
}

#[test]
fn more_rbsp_data_distinguishes_syntax_from_trailing_bits() {
    let data = [0xb0];
    let mut reader = BitReader::new(&data);
    assert!(reader.more_rbsp_data());
    assert_eq!(reader.read_bits(3).unwrap(), 5);
    assert!(!reader.more_rbsp_data());
    assert!(!BitReader::new(&[]).more_rbsp_data());
}

#[test]
fn decodes_unsigned_exp_golomb_vectors_and_boundary() {
    let data = pack_bits("1010011001000010100110");
    let mut reader = BitReader::new(&data);
    for expected in [0, 1, 2, 3, 4, 5] {
        assert_eq!(reader.read_ue().unwrap(), expected);
    }

    let maximum = pack_bits(&format!("{}1{}", "0".repeat(32), "0".repeat(32)));
    assert_eq!(BitReader::new(&maximum).read_ue().unwrap(), u32::MAX);
}

#[test]
fn decodes_signed_exp_golomb_vectors() {
    let data = pack_bits("1010011001000010100110");
    let mut reader = BitReader::new(&data);
    for expected in [0, 1, -1, 2, -2, 3] {
        assert_eq!(reader.read_se().unwrap(), expected);
    }
}

#[test]
fn exp_golomb_errors_do_not_consume_bits() {
    let truncated = [0u8];
    let mut reader = BitReader::new(&truncated);
    assert_eq!(
        reader.read_ue().unwrap_err().kind(),
        ErrorKind::UnexpectedEof
    );
    assert!(!reader.read_bit().unwrap());

    let overflow = pack_bits(&"0".repeat(33));
    let mut reader = BitReader::new(&overflow);
    assert_eq!(reader.read_ue().unwrap_err().kind(), ErrorKind::InvalidData);
    assert!(!reader.read_bit().unwrap());

    let too_large = pack_bits(&format!("{}1{}1", "0".repeat(32), "0".repeat(31)));
    let mut reader = BitReader::new(&too_large);
    assert_eq!(reader.read_ue().unwrap_err().kind(), ErrorKind::InvalidData);
}
