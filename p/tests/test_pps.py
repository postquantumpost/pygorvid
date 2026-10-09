from pathlib import Path

import pytest

from pygorvid import PPSParseError, VideoSampleReader, parse_pps


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


def make_pps(
    pps_id: int,
    sps_id: int,
    entropy: bool,
    bottom_field_poc: bool,
    *,
    num_slice_groups: int = 0,
    num_ref_idx_l0: int = 0,
    num_ref_idx_l1: int = 0,
    weighted_pred: bool = False,
    weighted_bipred_idc: int = 0,
    pic_init_qp: int = 0,
    pic_init_qs: int = 0,
    chroma_qp: int = 0,
    deblocking: bool = True,
    constrained_intra: bool = False,
    redundant_pic_cnt: bool = False,
    extension: tuple[bool, bool, int] | None = None,
) -> bytes:
    bits = ue_bits(pps_id) + ue_bits(sps_id)
    bits += "1" if entropy else "0"
    bits += "1" if bottom_field_poc else "0"
    bits += ue_bits(num_slice_groups) + ue_bits(num_ref_idx_l0) + ue_bits(num_ref_idx_l1)
    bits += "1" if weighted_pred else "0"
    bits += f"{weighted_bipred_idc:02b}"
    bits += se_bits(pic_init_qp) + se_bits(pic_init_qs) + se_bits(chroma_qp)
    bits += "1" if deblocking else "0"
    bits += "1" if constrained_intra else "0"
    bits += "1" if redundant_pic_cnt else "0"
    if extension is not None:
        transform_8x8, scaling_matrix, second_chroma_qp = extension
        bits += "1" if transform_8x8 else "0"
        bits += "1" if scaling_matrix else "0"
        if scaling_matrix:
            bits += "1"
        bits += se_bits(second_chroma_qp)
    bits += "1"
    return b"\x68" + pack_bits(bits)


@pytest.mark.parametrize(
    ("nal", "expected"),
    [
        (make_pps(0, 0, True, False), (0, 0, True, False)),
        (make_pps(255, 31, False, True), (255, 31, False, True)),
    ],
    ids=["cabac-default", "cavlc-bottom-field"],
)
def test_parse_pps_identifiers_and_core_flags(nal, expected):
    info = parse_pps(nal)
    assert (
        info.picture_parameter_set_id,
        info.sequence_parameter_set_id,
        info.entropy_coding_mode,
        info.bottom_field_pic_order_in_frame_present,
    ) == expected


@pytest.mark.parametrize(
    "nal",
    [
        b"\x67\x80",
        b"\xe8\x80",
        make_pps(256, 0, False, False),
        make_pps(0, 32, False, False),
        b"\x68\x80",
    ],
    ids=["wrong-nal-type", "forbidden-bit", "pps-id-out-of-range", "sps-id-out-of-range", "truncated-prefix"],
)
def test_parse_pps_rejects_invalid_prefix(nal):
    with pytest.raises(PPSParseError):
        parse_pps(nal)


def test_parse_pps_weighting_deblocking_and_extension():
    info = parse_pps(
        make_pps(
            0,
            0,
            True,
            False,
            weighted_pred=True,
            weighted_bipred_idc=1,
            constrained_intra=True,
            redundant_pic_cnt=True,
            extension=(True, False, 0),
        )
    )
    assert info.weighted_pred and info.weighted_bipred_idc == 1
    assert info.deblocking_filter_control_present
    assert info.constrained_intra_pred and info.redundant_pic_cnt_present
    assert info.has_extension and info.transform_8x8_mode
    assert not info.pic_scaling_matrix_present
    assert info.second_chroma_qp_index_offset == 0


@pytest.mark.parametrize(
    "nal",
    [
        make_pps(0, 0, True, False, num_slice_groups=1),
        make_pps(0, 0, True, False, weighted_bipred_idc=3),
        make_pps(0, 0, True, False, extension=(False, True, 0)),
        make_pps(0, 0, True, False, pic_init_qp=26),
        make_pps(0, 0, True, False, chroma_qp=13),
        make_pps(0, 0, True, False, extension=(True, False, 13)),
    ],
    ids=["fmo", "reserved-weighting", "scaling-matrix", "qp-range", "chroma-qp-range", "second-chroma-qp-range"],
)
def test_parse_pps_rejects_unsupported_or_invalid_tools(nal):
    with pytest.raises(PPSParseError):
        parse_pps(nal)


def test_parse_pps_compact_fixtures():
    repository_root = Path(__file__).resolve().parents[2]
    for name, has_extension in (("high42-1080p.mp4", False), ("high52-2160p.mp4", True)):
        with VideoSampleReader(repository_root / "testdata" / "h264" / name) as reader:
            for picture_set in reader.configuration.picture_parameter_sets:
                info = parse_pps(picture_set)
                assert (info.picture_parameter_set_id, info.sequence_parameter_set_id) == (0, 0)
                assert info.entropy_coding_mode
                assert not info.bottom_field_pic_order_in_frame_present
                assert info.num_slice_groups_minus1 == 0
                assert info.num_ref_idx_l0_default_active_minus1 == 0
                assert info.num_ref_idx_l1_default_active_minus1 == 0
                assert not info.weighted_pred and info.weighted_bipred_idc == 0
                assert (info.pic_init_qp_minus26, info.pic_init_qs_minus26, info.chroma_qp_index_offset) == (0, 0, 0)
                assert info.deblocking_filter_control_present
                assert not info.constrained_intra_pred and not info.redundant_pic_cnt_present
                assert info.has_extension == has_extension
                assert info.transform_8x8_mode == has_extension
                assert not info.pic_scaling_matrix_present
                assert info.second_chroma_qp_index_offset == 0