use std::io::{self, ErrorKind};

use pygorvid::{
    ebsp_to_rbsp, parse_nal_header, parse_pps, parse_slice_header, parse_sps,
    split_h264_sample_nals, split_length_prefixed_nals, BitReader, VideoSampleReader,
};

fn main() -> Result<(), Box<dyn std::error::Error>> {
    let args: Vec<String> = std::env::args().skip(1).collect();
    if args.len() != 2 {
        return Err(io::Error::new(
            ErrorKind::InvalidInput,
            "usage: check_h264_sample INPUT.mp4 PRESENTATION_FRAME_INDEX",
        )
        .into());
    }
    let frame_index: usize = args[1].parse()?;
    let mut reader = VideoSampleReader::open(&args[0])?;
    let config = reader.avc_configuration().ok_or_else(|| {
        io::Error::new(
            ErrorKind::InvalidInput,
            "sample check requires an H.264 track",
        )
    })?;
    let length_size = usize::from(config.nal_length_size);
    let sequence_sets = config
        .sequence_parameter_sets
        .iter()
        .map(|nal| parse_sps(nal))
        .collect::<io::Result<Vec<_>>>()?;
    let picture_sets = config
        .picture_parameter_sets
        .iter()
        .map(|nal| parse_pps(nal))
        .collect::<io::Result<Vec<_>>>()?;
    let sample_index = reader.presentation_sample_index(frame_index)?;
    let sample = reader.sample_at(sample_index)?;
    println!(
        "frame {frame_index}: sample {sample_index}, {} compressed bytes, sync={}",
        sample.data.len(),
        sample.is_sync
    );
    let raw_nals = split_length_prefixed_nals(&sample.data, length_size)?;
    let nals = split_h264_sample_nals(&sample.data, length_size)?;
    if nals.len() != raw_nals.len() {
        println!("compatibility recovery: separated an appended Annex B end marker");
    }
    for (nal_index, nal) in nals.iter().enumerate() {
        let header = parse_nal_header(nal)?;
        println!(
            "NAL {nal_index}: type {}, {} bytes, tail {:02x?}",
            header.unit_type,
            nal.len(),
            &nal[nal.len().saturating_sub(12)..]
        );
        let rbsp = ebsp_to_rbsp(&nal[1..]).map_err(|error| {
            io::Error::new(
                error.kind(),
                format!("sample {sample_index}, NAL {nal_index}: {error}"),
            )
        })?;
        println!("  RBSP: {} bytes", rbsp.len());
        if matches!(header.unit_type, 1 | 5) {
            let mut bits = BitReader::new(&rbsp);
            bits.read_ue()?;
            bits.read_ue()?;
            let pps_id = bits.read_ue()?;
            let pps = picture_sets
                .iter()
                .find(|pps| pps.picture_parameter_set_id == pps_id)
                .ok_or_else(|| {
                    io::Error::new(
                        ErrorKind::InvalidData,
                        "slice references an unavailable PPS",
                    )
                })?;
            let sps = sequence_sets
                .iter()
                .find(|sps| sps.sps_id == pps.sequence_parameter_set_id)
                .ok_or_else(|| {
                    io::Error::new(
                        ErrorKind::InvalidData,
                        "slice references an unavailable SPS",
                    )
                })?;
            let slice = parse_slice_header(nal, sps, pps)?;
            println!(
                "  slice: type {}, first macroblock {}, data bit offset {}",
                slice.slice_type, slice.first_macroblock_in_slice, slice.slice_data_bit_offset
            );
        }
    }
    Ok(())
}
