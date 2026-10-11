use std::path::PathBuf;

use pygorvid::{
    CabacDecodedSliceMacroblock, CabacInterMacroblockPartition, CabacInterMacroblockResult,
    CabacPBMacroblockLayerResult, CabacPBMacroblockPrediction, CabacSliceMacroblockInput,
    CabacSliceMacroblockResult, DecodeError, H264Decoder, MotionVector, PresentationOrderBuffer,
    PresentationPicture, ReferencePicture, VideoSampleReader, Yuv420Frame, Yuv420FrameBuilder,
};

#[test]
fn h264_decoder_tracks_reader_state() {
    let mut decoder = H264Decoder::with_sample_count(2);
    assert_eq!(decoder.sample_count(), 2);
    assert!(decoder.reference_pictures().is_empty());

    let frame = Yuv420Frame {
        width: 2,
        height: 2,
        y_stride: 2,
        u_stride: 1,
        v_stride: 1,
        y: vec![1, 2, 3, 4],
        u: vec![5],
        v: vec![6],
    };
    let reference = ReferencePicture {
        identifier: 7,
        frame_num: 3,
        picture_order_cnt: 8,
        long_term_frame_idx: None,
    };
    decoder.store_reference_picture(reference, &frame).unwrap();
    let stored = decoder
        .reference_picture_buffer
        .get(reference.identifier)
        .unwrap();
    assert_eq!(stored.reference.identifier, reference.identifier);
    assert_eq!(stored.frame.y[0], 1);

    decoder.presentation_order_buffer = PresentationOrderBuffer::new(1);
    let queued = decoder
        .queue_presentation(&PresentationPicture {
            picture_order_cnt: 8,
            frame: frame.clone(),
        })
        .unwrap();
    assert!(queued.is_none());
}

#[test]
fn h264_decoder_requires_sample_reader() {
    let mut decoder = H264Decoder::default();
    let err = decoder.decode_frame(0).unwrap_err();
    assert_eq!(err, DecodeError::MissingSampleReader);
}

#[test]
fn h264_decoder_rejects_out_of_range_frame_requests() {
    let mut decoder = H264Decoder::with_sample_count(0);
    let err = decoder.decode_frame(0).unwrap_err();
    assert_eq!(err, DecodeError::FrameIndexOutOfRange);
}

#[test]
fn h264_decoder_uncached_decode_requires_sample_reader() {
    let mut decoder = H264Decoder::with_sample_count(1);
    let err = decoder.decode_frame(0).unwrap_err();
    assert_eq!(err, DecodeError::MissingSampleReader);
}

#[test]
fn decoder_builds_next_real_idr_macroblock_input_from_history() {
    let fixture = PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .join("..")
        .join("testdata")
        .join("h264")
        .join("high42-1080p.mp4");
    let reader = VideoSampleReader::open(&fixture).unwrap();
    let mut decoder = H264Decoder::new(reader);
    let slices = decoder.dependency_slice_pictures(0).unwrap();
    let dependency_slice = &slices[0][0];
    assert!(matches!(
        decoder
            .macroblock_input_from_history(dependency_slice, 0, &[])
            .unwrap(),
        CabacSliceMacroblockInput::Intra(_)
    ));
    let sps = decoder
        .sequence_parameter_sets
        .iter()
        .find(|sps| sps.sps_id == decoder.picture_parameter_sets[0].sequence_parameter_set_id)
        .unwrap();
    let mut builder = Yuv420FrameBuilder::new(
        (sps.coded_width / 16) as usize,
        (sps.coded_height / 16) as usize,
        sps.frame_crop_left as usize,
        sps.frame_crop_top as usize,
        sps.frame_crop_right as usize,
        sps.frame_crop_bottom as usize,
    )
    .unwrap();
    let mut addresses = Vec::new();
    let error = decoder
        .decode_dependency_slice_data(dependency_slice, &mut builder, |address, history| {
            addresses.push((address, history.len()));
            let input =
                decoder.intra_macroblock_input_from_history(dependency_slice, address, history)?;
            assert_eq!(input.top_luma_cbp, 15);
            if address == 0 {
                assert_eq!(input.left_luma_cbp, 15);
            }
            if address == 1 {
                let CabacSliceMacroblockResult::Intra(first) = &history[0].result else {
                    panic!("expected intra macroblock");
                };
                assert_eq!(first.luma[0], 29);
                assert!(input.left_available);
                assert!(!input.top_available);
                assert_eq!(input.left_luma_cbp, 1);
                assert!(input.chroma_references[0].left_available);
                assert!(!input.chroma_references[0].top_available);
                assert!(!input.luma_8x8_blocks[0].references_filtered);
                let mut pcm_history = history.to_vec();
                let CabacSliceMacroblockResult::Intra(pcm) = &mut pcm_history[0].result else {
                    panic!("expected intra macroblock");
                };
                pcm.macroblock_type = 25;
                let pcm_input = decoder.intra_macroblock_input_from_history(
                    dependency_slice,
                    address,
                    &pcm_history,
                )?;
                assert_eq!(pcm_input.left_luma_cbp, 15);
                assert_eq!(pcm_input.left_chroma_cbp, 2);
                assert_eq!(pcm_input.top_chroma_cbp, 0);
            }
            if address == 2 {
                let CabacSliceMacroblockResult::Intra(second) = &history[1].result else {
                    panic!("expected intra macroblock");
                };
                assert_eq!(second.luma[0], 29);
                assert_eq!(second.chroma_prediction_mode, 0);
                return Err(std::io::Error::other("stop after provider build"));
            }
            Ok(CabacSliceMacroblockInput::Intra(input))
        })
        .unwrap_err();
    assert_eq!(error.to_string(), "stop after provider build");
    assert_eq!(addresses, [(0, 0), (1, 1), (2, 2)]);
    assert!(builder
        .place_macroblock(0, &[0; 256], &[0; 64], &[0; 64])
        .is_err());
}

#[test]
fn decoder_reconstructs_complete_idr_row_against_unfiltered_reference() {
    let fixture =
        PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("../testdata/h264/high42-1080p.mp4");
    let mut decoder = H264Decoder::new(VideoSampleReader::open(&fixture).unwrap());
    let pictures = decoder.dependency_slice_pictures(0).unwrap();
    let dependency_slice = &pictures[0][0];
    let width_in_mbs = (decoder.sequence_parameter_sets[0].coded_width / 16) as usize;
    let mut builder = Yuv420FrameBuilder::new(width_in_mbs, 1, 0, 0, 0, 0).unwrap();
    let error = decoder
        .decode_dependency_slice_data(dependency_slice, &mut builder, |address, history| {
            if address == width_in_mbs {
                assert_eq!(history.len(), width_in_mbs);
                return Err(std::io::Error::other("row complete"));
            }
            decoder
                .intra_macroblock_input_from_history(dependency_slice, address, history)
                .map(CabacSliceMacroblockInput::Intra)
        })
        .unwrap_err();
    assert_eq!(error.to_string(), "row complete");
    let frame = builder.finish().unwrap();
    assert_eq!((frame.width, frame.height), (1920, 16));
    let hashes = [&frame.y, &frame.u, &frame.v].map(|plane| {
        plane.iter().fold(0xcbf29ce484222325_u64, |hash, sample| {
            (hash ^ u64::from(*sample)).wrapping_mul(0x100000001b3)
        })
    });
    assert_eq!(
        hashes,
        [0xa4db5b9b973ce344, 0xaa5d2bcc4a3f0937, 0x9b3c93ac11e69ff6]
    );
}

#[test]
fn decoder_assembles_complete_native_idr_picture() {
    for (name, width, height, expected) in [(
        "high42-1080p.mp4",
        1920,
        1080,
        [0xb145f5f14c00f7d0, 0x9b481b664ec50ee2, 0x26868f6247d8e216],
    )] {
        let fixture = PathBuf::from(env!("CARGO_MANIFEST_DIR"))
            .join("../testdata/h264")
            .join(name);
        let mut decoder = H264Decoder::new(VideoSampleReader::open(&fixture).unwrap());
        let frame = decoder.decode_frame(0).unwrap();
        assert_eq!((frame.width, frame.height), (width, height));
        assert_eq!(frame.y.len(), width * height);
        assert_eq!(frame.u.len(), width * height / 4);
        assert_eq!(frame.v.len(), width * height / 4);
        let hashes = [&frame.y, &frame.u, &frame.v].map(|plane| {
            plane.iter().fold(0xcbf29ce484222325_u64, |hash, sample| {
                (hash ^ u64::from(*sample)).wrapping_mul(0x100000001b3)
            })
        });
        assert_eq!(hashes, expected, "{name}: unfiltered IDR plane mismatch");
        assert_eq!(decoder.decode_frame(0).unwrap(), frame);
    }
}

#[test]
fn decoder_builds_p_and_spatial_b_macroblock_inputs_from_reference_state() {
    let fixture = PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .join("..")
        .join("testdata")
        .join("h264")
        .join("high42-1080p.mp4");
    let reader = VideoSampleReader::open(&fixture).unwrap();
    let mut decoder = H264Decoder::new(reader);
    let pictures = decoder.dependency_slice_pictures(1).unwrap();
    let p_slice = &pictures[1][0];
    let b_slice = &pictures[2][0];
    let sps = decoder
        .sequence_parameter_sets
        .iter()
        .find(|sps| sps.sps_id == decoder.picture_parameter_sets[0].sequence_parameter_set_id)
        .unwrap();
    let width = sps.width as usize;
    let height = sps.height as usize;
    let chroma_width = width.div_ceil(2);
    let chroma_height = height.div_ceil(2);
    let frame = Yuv420Frame {
        width,
        height,
        y_stride: width,
        u_stride: chroma_width,
        v_stride: chroma_width,
        y: vec![0; width * height],
        u: vec![0; chroma_width * chroma_height],
        v: vec![0; chroma_width * chroma_height],
    };
    decoder
        .store_reference_picture(
            ReferencePicture {
                identifier: 10,
                frame_num: 0,
                picture_order_cnt: 0,
                long_term_frame_idx: None,
            },
            &frame,
        )
        .unwrap();

    let CabacSliceMacroblockInput::Inter(p_input) = decoder
        .macroblock_input_from_history(p_slice, 0, &[])
        .unwrap()
    else {
        panic!("P slice did not produce an inter macroblock input");
    };
    assert_eq!(p_input.residual_input.left_luma_cbp, 15);
    assert_eq!(p_input.residual_input.top_luma_cbp, 15);
    assert_eq!(p_input.residual_input.left_chroma_cbp, 0);
    assert_eq!(p_input.residual_input.top_chroma_cbp, 0);
    assert_eq!(p_input.inter_input.reference_lists[0].len(), 1);
    assert_eq!(
        p_input.inter_input.reference_lists[0][0].picture_order_count,
        0
    );
    assert_eq!(p_input.inter_input.partition_neighbors.len(), 16);
    assert!(p_input.b_skip_input.is_none());

    let colocated_history = [CabacDecodedSliceMacroblock {
        address: 0,
        result: CabacSliceMacroblockResult::Inter(CabacPBMacroblockLayerResult {
            skipped: false,
            macroblock: CabacPBMacroblockPrediction::Inter(CabacInterMacroblockResult {
                macroblock_type: 2,
                sub_macroblock_types: Vec::new(),
                partitions: vec![
                    CabacInterMacroblockPartition {
                        x: 0,
                        y: 0,
                        width: 8,
                        height: 16,
                        prediction_flags: [true, false],
                        reference_indices: [0, -1],
                        motion_vectors: [MotionVector { x: 0, y: 1 }, MotionVector::default()],
                        motion_vector_differences: [MotionVector::default(); 2],
                    },
                    CabacInterMacroblockPartition {
                        x: 8,
                        y: 0,
                        width: 8,
                        height: 16,
                        prediction_flags: [true, false],
                        reference_indices: [0, -1],
                        motion_vectors: [MotionVector { x: 4, y: 0 }, MotionVector::default()],
                        motion_vector_differences: [MotionVector::default(); 2],
                    },
                ],
                luma: [0; 256],
                cb: [0; 64],
                cr: [0; 64],
                coded_block_pattern_luma: 0,
                coded_block_pattern_chroma: 0,
                transform_size_8x8: false,
                qp_delta: 0,
                qpy: 26,
                luma_4x4_coded_block_flags: [false; 16],
                chroma_dc_coded: [false; 2],
                chroma_ac_coded_flags: [[false; 4]; 2],
            }),
        }),
    }];
    let CabacSliceMacroblockInput::Inter(next_p_input) = decoder
        .macroblock_input_from_history(p_slice, 1, &colocated_history)
        .unwrap()
    else {
        panic!("P slice did not produce an inter macroblock input");
    };
    assert_eq!(next_p_input.residual_input.left_luma_cbp, 0);
    assert_eq!(next_p_input.residual_input.top_luma_cbp, 15);
    decoder
        .store_reference_picture_with_macroblocks(
            ReferencePicture {
                identifier: 11,
                frame_num: 1,
                picture_order_cnt: 6,
                long_term_frame_idx: None,
            },
            &frame,
            &colocated_history,
        )
        .unwrap();
    let CabacSliceMacroblockInput::Inter(b_input) = decoder
        .macroblock_input_from_history(b_slice, 0, &[])
        .unwrap()
    else {
        panic!("B slice did not produce an inter macroblock input");
    };
    assert_eq!(
        b_input.inter_input.reference_lists[0][0].picture_order_count,
        0
    );
    assert_eq!(
        b_input.inter_input.reference_lists[1][0].picture_order_count,
        6
    );
    let b_direct_input = b_input.b_skip_input.unwrap();
    assert!(b_direct_input.direct_spatial);
    assert_eq!(b_direct_input.colocated_reference_index, 0);
    assert_eq!(
        b_direct_input.colocated_motion_vector,
        MotionVector { x: 0, y: 1 }
    );
    assert_eq!(
        b_direct_input.colocated_motion_field.unwrap()[0]
            .motion_vector
            .y,
        1
    );
    assert_eq!(
        b_direct_input.colocated_motion_field.unwrap()[3]
            .motion_vector
            .x,
        4
    );
}

#[test]
fn h264_decoder_tracks_p_frame_reference_cache_validation() {
    let fixture = PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .join("..")
        .join("testdata")
        .join("h264")
        .join("high42-1080p.mp4");
    let reader = VideoSampleReader::open(&fixture).unwrap();
    let mut decoder = H264Decoder::new(reader);
    let frame = Yuv420Frame {
        width: 2,
        height: 2,
        y_stride: 3,
        u_stride: 2,
        v_stride: 2,
        y: vec![1, 2, 99, 3, 4, 99],
        u: vec![5, 99],
        v: vec![6, 99],
    };
    let packed = Yuv420Frame {
        width: 2,
        height: 2,
        y_stride: 2,
        u_stride: 1,
        v_stride: 1,
        y: vec![1, 2, 3, 4],
        u: vec![5],
        v: vec![6],
    };
    let reference = ReferencePicture {
        identifier: 3,
        frame_num: 3,
        picture_order_cnt: 3,
        long_term_frame_idx: None,
    };
    decoder.store_reference_picture(reference, &frame).unwrap();
    let mut decoded = decoder.decode_frame(3).unwrap();
    assert_eq!(decoded, packed);
    assert_eq!(decoder.reference_pictures().len(), 1);
    decoded.y[0] = 88;
    assert_eq!(decoder.decode_frame(3).unwrap(), packed);
    assert_eq!(
        decoder.reference_picture_buffer.get(3).unwrap().frame,
        frame
    );
}

#[test]
fn h264_decoder_tracks_b_frame_cache_and_presentation_order() {
    let fixture = PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .join("..")
        .join("testdata")
        .join("h264")
        .join("high42-1080p.mp4");
    let reader = VideoSampleReader::open(&fixture).unwrap();
    let mut decoder = H264Decoder::new(reader);
    let frame = Yuv420Frame {
        width: 2,
        height: 2,
        y_stride: 2,
        u_stride: 1,
        v_stride: 1,
        y: vec![1, 2, 3, 4],
        u: vec![5],
        v: vec![6],
    };
    decoder
        .store_reference_picture(
            ReferencePicture {
                identifier: 1,
                frame_num: 1,
                picture_order_cnt: 1,
                long_term_frame_idx: None,
            },
            &frame,
        )
        .unwrap();
    decoder
        .store_reference_picture(
            ReferencePicture {
                identifier: 2,
                frame_num: 2,
                picture_order_cnt: 2,
                long_term_frame_idx: None,
            },
            &frame,
        )
        .unwrap();
    assert_eq!(decoder.decode_frame(1).unwrap(), frame);
    assert_eq!(decoder.decode_frame(2).unwrap(), frame);

    decoder.presentation_order_buffer = PresentationOrderBuffer::new(2);
    let mut released = Vec::new();
    for (picture_order_cnt, sample) in [(0, 0), (6, 6), (2, 2), (4, 4)] {
        let picture = PresentationPicture {
            picture_order_cnt,
            frame: Yuv420Frame {
                width: 2,
                height: 2,
                y_stride: 2,
                u_stride: 1,
                v_stride: 1,
                y: vec![sample; 4],
                u: vec![5],
                v: vec![6],
            },
        };
        released.push(
            decoder
                .queue_presentation(&picture)
                .unwrap()
                .map(|ready| ready.picture_order_cnt),
        );
    }
    assert_eq!(released, [None, None, Some(0), Some(2)]);
    let drained = decoder.presentation_order_buffer.drain();
    assert_eq!(
        drained
            .iter()
            .map(|picture| picture.picture_order_cnt)
            .collect::<Vec<_>>(),
        [4, 6]
    );
    assert_eq!(
        drained
            .iter()
            .map(|picture| picture.frame.y[0])
            .collect::<Vec<_>>(),
        [4, 6]
    );
    assert_eq!(decoder.decode_frame(1).unwrap(), frame);
    assert_eq!(decoder.decode_frame(2).unwrap(), frame);
    assert_eq!(decoder.reference_pictures().len(), 2);
}

#[test]
fn h264_decoder_tracks_first_sync_index_and_reference_validation() {
    let fixture = PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .join("..")
        .join("testdata")
        .join("h264")
        .join("high42-1080p.mp4");
    let reader = VideoSampleReader::open(&fixture).unwrap();
    let mut decoder = H264Decoder::new(reader);
    assert_eq!(decoder.first_sync_sample_index(), Some(0));

    let frame = Yuv420Frame {
        width: 2,
        height: 2,
        y_stride: 2,
        u_stride: 1,
        v_stride: 1,
        y: vec![1, 2, 3, 4],
        u: vec![5],
        v: vec![6],
    };
    decoder
        .store_reference_picture(
            ReferencePicture {
                identifier: 0,
                frame_num: 0,
                picture_order_cnt: 0,
                long_term_frame_idx: None,
            },
            &frame,
        )
        .unwrap();
    decoder
        .validate_reference_frame(0, &frame.y, &frame.u, &frame.v)
        .unwrap();
    let mismatch = decoder.validate_reference_frame(0, &[9, 2, 3, 4], &frame.u, &frame.v);
    assert_eq!(mismatch, Err(DecodeError::ReferenceMismatch));
}

#[test]
fn h264_decoder_reads_dependencies_without_advancing_owned_reader() {
    let fixture = PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .join("..")
        .join("testdata")
        .join("h264")
        .join("high42-1080p.mp4");
    let reader = VideoSampleReader::open(&fixture).unwrap();
    let mut decoder = H264Decoder::new(reader);
    let dependencies = decoder.decode_order_dependency_samples(3).unwrap();
    assert!(!dependencies.is_empty());
    assert!(dependencies
        .windows(2)
        .all(|samples| samples[0].index < samples[1].index));
    assert_eq!(dependencies[0].index, 0);
    assert!(dependencies[0].is_sync);
}

#[test]
fn h264_decoder_parses_dependency_slice_headers_without_advancing_reader() {
    let fixture = PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .join("..")
        .join("testdata")
        .join("h264")
        .join("high42-1080p.mp4");
    let reader = VideoSampleReader::open(&fixture).unwrap();
    let mut decoder = H264Decoder::new(reader);
    let pictures = decoder.dependency_slice_pictures(0).unwrap();
    assert!(!pictures.is_empty());
    assert!(!pictures[0].is_empty());
    assert_eq!(pictures[0][0].sample_index, 0);
    assert!(pictures[0][0].header.idr);
    assert_eq!(pictures[0][0].header.first_macroblock_in_slice, 0);
}

#[test]
fn h264_decoder_rejects_runtime_ffmpeg_subprocesses() {
    let fixture = PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .join("..")
        .join("testdata")
        .join("h264")
        .join("high42-1080p.mp4");
    let reader = VideoSampleReader::open(&fixture).unwrap();
    let mut decoder = H264Decoder::new(reader);
    match decoder.decode_frame(0) {
        Ok(frame) => assert!(frame.width > 0 && frame.height > 0),
        Err(DecodeError::DecodeFailure(message)) => {
            assert!(!message.to_lowercase().contains("ffmpeg"));
        }
        Err(error) => panic!("unexpected native decode error: {error}"),
    }
}
