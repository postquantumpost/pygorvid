use std::io;

use crate::Yuv420Frame;

pub(crate) struct RgbImage {
    pub width: usize,
    pub height: usize,
    pub data: Vec<u8>,
}

pub(crate) fn frame_to_rgb(frame: &Yuv420Frame) -> io::Result<RgbImage> {
    let size = frame
        .width
        .checked_mul(frame.height)
        .and_then(|pixels| pixels.checked_mul(3))
        .ok_or_else(|| {
            io::Error::new(
                io::ErrorKind::InvalidInput,
                "image dimensions are too large",
            )
        })?;
    let luma = frame.luma_plane_bytes()?;
    let chroma_u = frame.u_plane_bytes()?;
    let chroma_v = frame.v_plane_bytes()?;
    let chroma_width = frame.width.div_ceil(2);
    let mut data = Vec::with_capacity(size);
    for row in 0..frame.height {
        for column in 0..frame.width {
            let luma_value = f64::from(luma[row * frame.width + column]) - 16.0;
            let chroma_offset = (row / 2) * chroma_width + column / 2;
            let blue_difference = f64::from(chroma_u[chroma_offset]) - 128.0;
            let red_difference = f64::from(chroma_v[chroma_offset]) - 128.0;
            for value in [
                1.164 * luma_value + 1.596 * red_difference,
                1.164 * luma_value - 0.392 * blue_difference - 0.813 * red_difference,
                1.164 * luma_value + 2.017 * blue_difference,
            ] {
                data.push(value.round().clamp(0.0, 255.0) as u8);
            }
        }
    }
    Ok(RgbImage {
        width: frame.width,
        height: frame.height,
        data,
    })
}

pub fn encode_jpeg_frame(frame: &Yuv420Frame) -> io::Result<Vec<u8>> {
    crate::jpegencoder::encode(&frame_to_rgb(frame)?)
}

pub fn encode_png_frame(frame: &Yuv420Frame) -> io::Result<Vec<u8>> {
    let rgb = frame_to_rgb(frame)?;
    let width = u32::try_from(rgb.width).map_err(|_| {
        io::Error::new(
            io::ErrorKind::InvalidInput,
            "PNG width exceeds the format limit",
        )
    })?;
    let height = u32::try_from(rgb.height).map_err(|_| {
        io::Error::new(
            io::ErrorKind::InvalidInput,
            "PNG height exceeds the format limit",
        )
    })?;
    let mut scanlines = Vec::new();
    for row in rgb.data.chunks_exact(rgb.width * 3) {
        scanlines.push(0);
        scanlines.extend_from_slice(row);
    }
    let mut zlib = vec![0x78, 0x01];
    let block_count = scanlines.len().div_ceil(65535);
    for (index, block) in scanlines.chunks(65535).enumerate() {
        let length = block.len() as u16;
        zlib.push(u8::from(index + 1 == block_count));
        zlib.extend_from_slice(&length.to_le_bytes());
        zlib.extend_from_slice(&(!length).to_le_bytes());
        zlib.extend_from_slice(block);
    }
    zlib.extend_from_slice(&adler32(&scanlines).to_be_bytes());
    let mut output = b"\x89PNG\r\n\x1a\n".to_vec();
    let mut header = width.to_be_bytes().to_vec();
    header.extend_from_slice(&height.to_be_bytes());
    header.extend_from_slice(&[8, 2, 0, 0, 0]);
    append_png_chunk(&mut output, b"IHDR", &header)?;
    append_png_chunk(&mut output, b"IDAT", &zlib)?;
    append_png_chunk(&mut output, b"IEND", &[])?;
    Ok(output)
}

fn append_png_chunk(output: &mut Vec<u8>, kind: &[u8; 4], data: &[u8]) -> io::Result<()> {
    let length = u32::try_from(data.len()).map_err(|_| {
        io::Error::new(
            io::ErrorKind::InvalidInput,
            "PNG chunk exceeds the format limit",
        )
    })?;
    output.extend_from_slice(&length.to_be_bytes());
    let start = output.len();
    output.extend_from_slice(kind);
    output.extend_from_slice(data);
    let checksum = crc32(&output[start..]);
    output.extend_from_slice(&checksum.to_be_bytes());
    Ok(())
}

fn crc32(data: &[u8]) -> u32 {
    let mut checksum = u32::MAX;
    for value in data {
        checksum ^= u32::from(*value);
        for _ in 0..8 {
            checksum = if checksum & 1 != 0 {
                (checksum >> 1) ^ 0xedb88320
            } else {
                checksum >> 1
            };
        }
    }
    !checksum
}

fn adler32(data: &[u8]) -> u32 {
    let mut first = 1_u32;
    let mut second = 0_u32;
    for block in data.chunks(5552) {
        for value in block {
            first += u32::from(*value);
            second += first;
        }
        first %= 65521;
        second %= 65521;
    }
    (second << 16) | first
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn converts_limited_range_and_encodes_png() {
        let frame = Yuv420Frame {
            width: 2,
            height: 2,
            y_stride: 2,
            u_stride: 1,
            v_stride: 1,
            y: vec![16, 235, 81, 145],
            u: vec![128],
            v: vec![128],
        };
        assert_eq!(
            frame_to_rgb(&frame).unwrap().data,
            [0, 0, 0, 255, 255, 255, 76, 76, 76, 150, 150, 150]
        );
        let payload = encode_png_frame(&frame).unwrap();
        assert_eq!(&payload[..8], b"\x89PNG\r\n\x1a\n");
        assert_eq!(&payload[16..24], &[0, 0, 0, 2, 0, 0, 0, 2]);
        assert_eq!(crc32(b"123456789"), 0xcbf43926);
        assert_eq!(adler32(b"Wikipedia"), 0x11e60398);
    }
}
