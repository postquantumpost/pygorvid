use std::path::PathBuf;

use pygorvid::{
    DecodeError, H264Decoder, PresentationOrderBuffer, PresentationPicture, ReferencePicture,
    Yuv420Frame, VideoSampleReader,
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
    let stored = decoder.reference_picture_buffer.get(reference.identifier).unwrap();
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
fn h264_decoder_uses_real_sample_reader_fixture() {
    let fixture = PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .join("..")
        .join("testdata")
        .join("h264")
        .join("high42-1080p.mp4");
    let reader = VideoSampleReader::open(&fixture).unwrap();
    let mut decoder = H264Decoder::new(reader);
    let frame = decoder.decode_frame(0).unwrap();
    assert_eq!(frame.width, 1920);
    assert_eq!(frame.height, 1080);
    assert_eq!(frame.y[0], 29);
    assert_eq!(frame.u[0], 129);
    assert_eq!(frame.v[0], 127);
}

#[test]
fn h264_decoder_uses_real_sample_reader_fixture_for_p_frame() {
    let fixture = PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .join("..")
        .join("testdata")
        .join("h264")
        .join("high42-1080p.mp4");
    let reader = VideoSampleReader::open(&fixture).unwrap();
    let mut decoder = H264Decoder::new(reader);
    let frame = decoder.decode_frame(3).unwrap();
    assert_eq!(frame.width, 1920);
    assert_eq!(frame.height, 1080);
    assert_eq!(frame.y[100_000], 41);
    assert_eq!(frame.u[100], 129);
    assert_eq!(frame.v[100], 127);
}

#[test]
fn h264_decoder_uses_real_sample_reader_fixture_for_b_frame() {
    let fixture = PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .join("..")
        .join("testdata")
        .join("h264")
        .join("high42-1080p.mp4");
    let reader = VideoSampleReader::open(&fixture).unwrap();
    let mut decoder = H264Decoder::new(reader);
    let frame = decoder.decode_frame(1).unwrap();
    assert_eq!(frame.width, 1920);
    assert_eq!(frame.height, 1080);
    assert_eq!(frame.y[0], 29);
    assert_eq!(frame.u[0], 129);
    assert_eq!(frame.v[0], 127);
    assert_eq!(decoder.reference_pictures().len(), 1);

    let cached = decoder.decode_frame(1).unwrap();
    assert_eq!(decoder.reference_pictures().len(), 1);
    assert_eq!(cached.y, frame.y);
    assert_eq!(cached.u, frame.u);
    assert_eq!(cached.v, frame.v);
}
