use std::path::PathBuf;

use pygorvid::{
    DecodeError, H264Decoder, PresentationOrderBuffer, PresentationPicture, ReferencePicture,
    VideoSampleReader, Yuv420Frame,
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
fn h264_decoder_contract_marks_frame_decode_as_unimplemented() {
    let mut decoder = H264Decoder::with_sample_count(1);
    let err = decoder.decode_frame(0).unwrap_err();
    assert_eq!(err, DecodeError::NotImplemented);
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
fn h264_decoder_rejects_runtime_ffmpeg_subprocesses() {
    let fixture = PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .join("..")
        .join("testdata")
        .join("h264")
        .join("high42-1080p.mp4");
    let reader = VideoSampleReader::open(&fixture).unwrap();
    let mut decoder = H264Decoder::new(reader);
    assert_eq!(decoder.decode_frame(0), Err(DecodeError::NotImplemented));
    assert!(decoder.reference_pictures().is_empty());
}
