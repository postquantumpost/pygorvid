from pathlib import Path

import pytest

from pygorvid import SPSParseError, VideoSampleReader, parse_sps


def pack_bits(bits: str) -> bytes:
    data = bytearray((len(bits) + 7) // 8)
    for index, bit in enumerate(bits):
        if bit == "1":
            data[index // 8] |= 1 << (7 - index % 8)
    return bytes(data)


def ue_bits(value: int) -> str:
    code = f"{value + 1:b}"
    return "0" * (len(code) - 1) + code


def se_bits(value: int) -> str:
    return ue_bits(2 * value - 1 if value > 0 else -2 * value)


def make_raw_sps(profile: int, constraints: int, level: int, syntax_bits: str) -> bytes:
    return bytes((0x67, profile, constraints, level)) + pack_bits(syntax_bits)


def make_sps(
    profile: int,
    constraints: int,
    level: int,
    syntax_bits: str,
    width_mbs_minus_1: int = 0,
    height_map_units_minus_1: int = 0,
    frame_mbs_only: bool = True,
    crop: tuple[int, int, int, int] = (0, 0, 0, 0),
    frame_num_minus4: int = 0,
    poc_type: int = 0,
    poc_syntax: str | None = None,
    max_num_ref_frames: int = 0,
    gaps_allowed: bool = False,
    mb_adaptive_frame_field: bool = False,
    direct_8x8_inference: bool = True,
) -> bytes:
    if profile in {44, 83, 86, 100, 110, 118, 122, 128, 134, 135, 138, 139, 144, 244}:
        syntax_bits += "00"  # qpprime bypass and absent scaling matrix
    syntax_bits += ue_bits(frame_num_minus4) + ue_bits(poc_type)
    if poc_syntax is None:
        poc_syntax = ue_bits(0) if poc_type == 0 else ""
    syntax_bits += poc_syntax + ue_bits(max_num_ref_frames)
    syntax_bits += "1" if gaps_allowed else "0"
    syntax_bits += ue_bits(width_mbs_minus_1) + ue_bits(height_map_units_minus_1)
    syntax_bits += "1" if frame_mbs_only else "0"
    if not frame_mbs_only:
        syntax_bits += "1" if mb_adaptive_frame_field else "0"
    syntax_bits += "1" if direct_8x8_inference else "0"
    cropping = any(crop)
    syntax_bits += "1" if cropping else "0"
    if cropping:
        syntax_bits += "".join(ue_bits(offset) for offset in crop)
    syntax_bits += "01"  # no VUI, followed by rbsp_stop_one_bit
    return make_raw_sps(profile, constraints, level, syntax_bits)


def test_parse_sps_rejects_unsupported_feature_set():
    unsupported_cases = [
        make_sps(118, 0, 42, "1" + "010" + "1" + "1"),
        make_sps(100, 0, 42, "1" + "00100" + "1" + "011" + "010"),
        make_sps(100, 0, 42, "1" + "010" + "1" + "1", frame_mbs_only=False),
        make_sps(100, 0, 42, "1" + "00100" + "1" + "1" + "1"),
    ]
    for nal in unsupported_cases:
        with pytest.raises(SPSParseError, match="unsupported|not supported"):
            parse_sps(nal)


def test_parse_sps_dimensions_and_crop_units():
    progressive = make_sps(
        100, 0, 42, "1" + "010" + "1" + "1", 119, 67, True, (0, 0, 0, 4)
    )
    info = parse_sps(progressive)
    assert (info.coded_width, info.coded_height, info.width, info.height) == (
        1920,
        1088,
        1920,
        1080,
    )
    assert info.frame_crop_bottom == 8

    progressive_cropped = make_sps(100, 0, 42, "1" + "010" + "1" + "1", 0, 1, True, (0, 0, 0, 1))
    info = parse_sps(progressive_cropped)
    assert (info.coded_width, info.coded_height, info.width, info.height) == (16, 32, 16, 30)
    assert info.frame_crop_bottom == 2
    assert info.frame_mbs_only
    assert not info.mb_adaptive_frame_field
    assert info.direct_8x8_inference


def test_parse_sps_reference_and_frame_flags():
    nal = make_sps(
        100,
        0,
        42,
        "1" + "010" + "1" + "1",
        frame_mbs_only=True,
        max_num_ref_frames=4,
        gaps_allowed=True,
        mb_adaptive_frame_field=False,
        direct_8x8_inference=False,
    )
    info = parse_sps(nal)
    assert info.max_num_ref_frames == 4
    assert info.gaps_in_frame_num_value_allowed
    assert info.frame_mbs_only
    assert not info.mb_adaptive_frame_field
    assert not info.direct_8x8_inference


@pytest.mark.parametrize(
    ("poc_type", "poc_syntax", "expected"),
    [
        (0, ue_bits(3), (True, 3, False, None, None, ())),
        (
            1,
            "1" + se_bits(-2) + se_bits(3) + ue_bits(2) + se_bits(-1) + se_bits(2),
            (False, None, True, -2, 3, (-1, 2)),
        ),
        (2, "", (False, None, False, None, None, ())),
    ],
    ids=["poc-zero", "poc-one", "poc-two"],
)
def test_parse_sps_frame_number_and_poc_modes(poc_type, poc_syntax, expected):
    nal = make_sps(
        100,
        0,
        42,
        "1" + "010" + "1" + "1",
        frame_num_minus4=4,
        poc_type=poc_type,
        poc_syntax=poc_syntax,
    )
    info = parse_sps(nal)
    assert info.log2_max_frame_num_minus4 == 4
    assert info.pic_order_cnt_type == poc_type
    assert (
        info.log2_max_pic_order_cnt_lsb_minus4 is not None,
        info.log2_max_pic_order_cnt_lsb_minus4,
        info.delta_pic_order_always_zero,
        info.offset_for_non_ref_pic,
        info.offset_for_top_to_bottom_field,
        info.offset_for_ref_frame,
    ) == expected


@pytest.mark.parametrize(
    "nal",
    [
        bytes((0x65, 66, 0, 30, 0x80)),
        make_sps(66, 1, 30, "1"),
        make_sps(66, 0, 30, "00000100001"),
        make_sps(100, 0, 42, "1" + "00101"),
        make_sps(100, 0, 42, "1" + "010" + "0001000"),
        make_raw_sps(100, 0, 42, "1"),
        make_sps(100, 0, 42, "1" + "010" + "1" + "1", 0, 0, True, (4, 4, 0, 0)),
        make_sps(100, 0, 42, "1" + "010" + "1" + "1", poc_type=3, poc_syntax=""),
        make_sps(100, 0, 42, "1" + "010" + "1" + "1", frame_num_minus4=13),
    ],
    ids=[
        "wrong-nal-type",
        "reserved-constraint-bits",
        "sps-id-out-of-range",
        "chroma-out-of-range",
        "bit-depth-out-of-range",
        "truncated-high-profile-fields",
        "crop-removes-picture",
        "invalid-poc-type",
        "frame-number-width-out-of-range",
    ],
)
def test_parse_sps_rejects_invalid_fields(nal):
    with pytest.raises(SPSParseError):
        parse_sps(nal)


def test_parse_sps_compact_fixtures():
    repository_root = Path(__file__).resolve().parents[2]
    for name, expected_refs in (("high42-1080p.mp4", 4), ("high52-2160p.mp4", 3)):
        with VideoSampleReader(repository_root / "testdata" / "h264" / name) as reader:
            for sequence_set in reader.configuration.sequence_parameter_sets:
                info = parse_sps(sequence_set)
                assert (info.profile_idc, info.chroma_format_idc) == (100, 1)
                assert (info.bit_depth_luma, info.bit_depth_chroma) == (8, 8)
                assert info.max_num_ref_frames == expected_refs
                assert not info.gaps_in_frame_num_value_allowed
                assert info.frame_mbs_only
                assert not info.mb_adaptive_frame_field
                assert info.direct_8x8_inference
                dimensions = (info.width, info.height)
                expected_dimensions = (1920, 1080) if name == "high42-1080p.mp4" else (3840, 2160)
                assert dimensions == expected_dimensions