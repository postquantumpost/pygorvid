from pathlib import Path
from dataclasses import replace

import pytest

from pygorvid import (
    PPSInfo,
    SPSInfo,
    SliceHeaderError,
    VideoSampleReader,
    parse_nal_header,
    parse_pps,
    parse_slice_header,
    parse_sps,
    group_slices_into_pictures,
    same_primary_picture,
)


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


def fixed_bits(value: int, width: int) -> str:
    return f"{value:0{width}b}"


def make_sps(poc_type=0, *, delta_always_zero=False, frame_mbs_only=True):
    bits = ue_bits(0) + ue_bits(1) + ue_bits(0) + ue_bits(0) + "00"
    bits += ue_bits(0) + ue_bits(poc_type)
    if poc_type == 0:
        bits += ue_bits(0)
    elif poc_type == 1:
        bits += "1" if delta_always_zero else "0"
        bits += se_bits(0) + se_bits(0) + ue_bits(0)
    bits += ue_bits(0) + "0" + ue_bits(0) + ue_bits(0)
    bits += "1" if frame_mbs_only else "00"
    bits += "10"  # direct_8x8_inference, no cropping
    bits += "01"  # no VUI and RBSP stop bit
    return b"\x67\x64\x00\x2a" + pack_bits(bits)


def make_pps(bottom_field_poc=False, pps_id=0, redundant_pic_cnt=False, sps_id=0):
    bits = ue_bits(pps_id) + ue_bits(sps_id) + "1" + ("1" if bottom_field_poc else "0")
    bits += ue_bits(0) + ue_bits(0) + ue_bits(0) + "0" + "00"
    bits += se_bits(0) * 3 + "10" + ("1" if redundant_pic_cnt else "0") + "1"
    return b"\x68" + pack_bits(bits)


def make_slice(
    header: int,
    first_mb: int,
    slice_type: int,
    pps_id: int,
    syntax: str,
    *,
    type_prefix: str | None = None,
    redundant_pic_count: int | None = None,
) -> bytes:
    normalized_type = slice_type % 5
    bits = ue_bits(first_mb) + ue_bits(slice_type) + ue_bits(pps_id) + syntax
    if redundant_pic_count is not None:
        bits += ue_bits(redundant_pic_count)
    if type_prefix is None:
        type_prefix = "0" if normalized_type == 0 else "00" if normalized_type == 1 else ""
    bits += type_prefix
    if normalized_type in (0, 1):
        bits += "0" + ("0" if normalized_type == 1 else "")
    if header & 0x1F == 5:
        bits += "00"
    elif header & 0x60:
        bits += "0"
    if normalized_type != 2:
        bits += ue_bits(0)
    bits += se_bits(0) + ue_bits(0) + se_bits(0) + se_bits(0)
    return bytes((header,)) + pack_bits(bits)


def parse_with(nal, sps=None, pps=None):
    sps_info = sps or parse_slice_sps()
    pps_info = pps or parse_slice_pps()
    return parse_slice_header(nal, sps_info, pps_info)


def parse_slice_sps(poc_type=0, *, delta_always_zero=False, frame_mbs_only=True):
    from pygorvid import parse_sps

    return parse_sps(make_sps(poc_type, delta_always_zero=delta_always_zero, frame_mbs_only=frame_mbs_only))


def parse_slice_pps(bottom_field_poc=False, redundant_pic_cnt=False):
    from pygorvid import parse_pps

    return parse_pps(make_pps(bottom_field_poc, redundant_pic_cnt=redundant_pic_cnt))


def test_idr_picture_identity_with_poc_zero():
    nal = make_slice(0x65, 0, 2, 0, fixed_bits(5, 4) + ue_bits(7) + fixed_bits(3, 4))
    header = parse_with(nal)
    assert (header.first_macroblock_in_slice, header.slice_type, header.frame_num) == (0, 2, 5)
    assert header.idr and header.idr_pic_id == 7 and header.pic_order_cnt_lsb == 3
    identity = header.picture_identity()
    assert identity.idr and identity.idr_pic_id == 7 and identity.frame_num == 5
    assert not identity.nal_ref_idc_zero


def test_poc_zero_frame_delta_and_poc_one_deltas():
    poc_zero = make_slice(0x41, 1, 0, 0, fixed_bits(2, 4) + fixed_bits(6, 4) + se_bits(-2))
    header = parse_with(poc_zero, pps=parse_slice_pps(bottom_field_poc=True))
    assert header.has_delta_pic_order_bottom and header.delta_pic_order_bottom == -2

    sps = parse_slice_sps(poc_type=1)
    poc_one = make_slice(0x01, 2, 1, 0, fixed_bits(3, 4) + se_bits(2) + se_bits(-1))
    header = parse_with(poc_one, sps=sps, pps=parse_slice_pps(bottom_field_poc=True))
    assert header.has_delta_pic_order_cnt0 and header.delta_pic_order_cnt0 == 2
    assert header.has_delta_pic_order_cnt1 and header.delta_pic_order_cnt1 == -1
    assert header.picture_identity().nal_ref_idc_zero


def test_poc_one_zero_deltas_poc_two_and_field_picture():
    poc_one_zero = make_slice(0x41, 0, 1, 0, fixed_bits(1, 4))
    header = parse_with(poc_one_zero, sps=parse_slice_sps(1, delta_always_zero=True))
    assert not header.has_delta_pic_order_cnt0

    poc_two = make_slice(0x41, 0, 0, 0, fixed_bits(2, 4))
    assert parse_with(poc_two, sps=parse_slice_sps(2)).frame_num == 2

    field = make_slice(0x41, 0, 0, 0, fixed_bits(3, 4) + "11" + fixed_bits(5, 4))
    header = parse_with(
        field,
        sps=parse_slice_sps(frame_mbs_only=False),
        pps=parse_slice_pps(bottom_field_poc=True),
    )
    assert header.field_pic_flag and header.bottom_field_flag
    assert header.pic_order_cnt_lsb == 5 and not header.has_delta_pic_order_bottom


def test_i_p_b_specific_slice_prefixes_and_redundant_count():
    sps = parse_slice_sps()
    pps = parse_slice_pps()
    identity_bits = fixed_bits(3, 4) + fixed_bits(5, 4)

    i_slice = parse_slice_header(make_slice(0x41, 0, 2, 0, identity_bits), sps, pps)
    assert not i_slice.num_ref_idx_active_override
    assert not i_slice.has_direct_spatial_mv_pred

    p_slice = parse_slice_header(
        make_slice(0x41, 0, 0, 0, identity_bits, type_prefix="1" + ue_bits(3)), sps, pps
    )
    assert p_slice.num_ref_idx_active_override
    assert p_slice.num_ref_idx_l0_active_minus1 == 3
    assert not p_slice.has_direct_spatial_mv_pred

    b_slice = parse_slice_header(
        make_slice(
            0x41,
            0,
            1,
            0,
            identity_bits,
            type_prefix="1" + "1" + ue_bits(2) + ue_bits(1),
        ),
        sps,
        pps,
    )
    assert b_slice.direct_spatial_mv_pred and b_slice.has_direct_spatial_mv_pred
    assert b_slice.num_ref_idx_active_override
    assert (b_slice.num_ref_idx_l0_active_minus1, b_slice.num_ref_idx_l1_active_minus1) == (2, 1)

    redundant = parse_slice_header(
        make_slice(0x41, 0, 2, 0, identity_bits, redundant_pic_count=4),
        sps,
        parse_slice_pps(redundant_pic_cnt=True),
    )
    assert redundant.has_redundant_pic_cnt and redundant.redundant_pic_cnt == 4


def test_slice_header_rejects_mismatched_or_invalid_prefix():
    nal = make_slice(0x41, 0, 0, 1, fixed_bits(0, 4) + fixed_bits(0, 4))
    with pytest.raises(SliceHeaderError, match="supplied PPS"):
        parse_with(nal)
    with pytest.raises(SliceHeaderError, match="slice_type"):
        parse_with(make_slice(0x41, 0, 10, 0, ""))
    with pytest.raises(SliceHeaderError, match="not a coded slice"):
        parse_with(b"\x67\x80")
    with pytest.raises(SliceHeaderError):
        truncated = b"\x41" + pack_bits(ue_bits(0) + ue_bits(0) + ue_bits(0) + "00")
        parse_with(truncated)
    with pytest.raises(SliceHeaderError, match="unsupported slice_type"):
        parse_with(make_slice(0x41, 0, 3, 0, fixed_bits(0, 4) + fixed_bits(0, 4)))
    identity_bits = fixed_bits(0, 4) + fixed_bits(0, 4)
    with pytest.raises(SliceHeaderError, match="l0_active_minus1 exceeds 31"):
        parse_with(make_slice(0x41, 0, 0, 0, identity_bits, type_prefix="1" + ue_bits(32)))
    with pytest.raises(SliceHeaderError, match="l1_active_minus1 exceeds 31"):
        parse_with(
            make_slice(
                0x41,
                0,
                1,
                0,
                identity_bits,
                type_prefix="1" + "1" + ue_bits(0) + ue_bits(32),
            )
        )


def test_slice_header_rejects_pps_referencing_a_different_sps():
    sps = parse_slice_sps()
    pps = parse_pps(make_pps())
    wrong_sps_pps = parse_pps(make_pps(sps_id=1))
    nal = make_slice(0x41, 0, 2, 0, fixed_bits(0, 4) + fixed_bits(0, 4))
    assert pps.sequence_parameter_set_id == sps.sps_id
    with pytest.raises(SliceHeaderError, match="PPS references SPS"):
        parse_slice_header(nal, sps, wrong_sps_pps)


def test_parse_slice_headers_in_compact_fixtures():
    repository_root = Path(__file__).resolve().parents[2]
    for name in ("high42-1080p.mp4", "high52-2160p.mp4"):
        fixture_path = repository_root / "testdata" / "h264" / name
        with VideoSampleReader(fixture_path) as reader:
            sps = parse_sps(reader.configuration.sequence_parameter_sets[0])
            pps = parse_pps(reader.configuration.picture_parameter_sets[0])
            picture_types = set()
            for sample in reader:
                offset = 0
                while offset < len(sample.data):
                    if len(sample.data) - offset < 4:
                        pytest.fail(f"{name} sample {sample.index} has truncated NAL prefix")
                    size = int.from_bytes(sample.data[offset : offset + 4], "big")
                    offset += 4
                    if size == 0 or size > len(sample.data) - offset:
                        pytest.fail(f"{name} sample {sample.index} has invalid NAL size")
                    nal = sample.data[offset : offset + size]
                    header = parse_nal_header(nal)
                    if header.unit_type in (1, 5):
                        slice_header = parse_slice_header(nal, sps, pps)
                        picture_types.add(slice_header.slice_type % 5)
                    offset += size
            assert {0, 1, 2} <= picture_types


def test_parsed_sps_pps_and_slice_metadata_for_every_eligible_input():
    repository_root = Path(__file__).resolve().parents[2]
    active_paths = [
        line.split("  ", 1)[1]
        for line in (repository_root / "silkroad1.sha256").read_text().splitlines()
        if line.strip()
    ]
    assert len(active_paths) == 10
    all_slice_types = set()

    for relative_path in active_paths:
        with VideoSampleReader(repository_root / relative_path) as reader:
            configuration = reader.configuration
            assert configuration is not None
            assert configuration.nal_length_size == 4
            assert len(configuration.sequence_parameter_sets) == 1
            assert len(configuration.picture_parameter_sets) == 1
            sps = parse_sps(configuration.sequence_parameter_sets[0])
            pps = parse_pps(configuration.picture_parameter_sets[0])
            assert sps.profile_idc == 100
            assert sps.level_idc in (42, 52)
            assert sps.chroma_format_idc == 1
            assert (sps.bit_depth_luma, sps.bit_depth_chroma) == (8, 8)
            assert (sps.width, sps.height) in ((1920, 1080), (3840, 2160))
            assert sps.frame_mbs_only and not sps.separate_colour_plane
            assert pps.sequence_parameter_set_id == sps.sps_id
            assert pps.entropy_coding_mode
            assert pps.num_slice_groups_minus1 == 0

            sample_slice_count = 0
            for sample in reader:
                offset = 0
                while offset < len(sample.data):
                    if len(sample.data) - offset < configuration.nal_length_size:
                        pytest.fail(f"{relative_path} sample {sample.index} has truncated NAL prefix")
                    size = int.from_bytes(
                        sample.data[offset : offset + configuration.nal_length_size], "big"
                    )
                    offset += configuration.nal_length_size
                    if size == 0 or size > len(sample.data) - offset:
                        pytest.fail(f"{relative_path} sample {sample.index} has invalid NAL size")
                    nal = sample.data[offset : offset + size]
                    header = parse_nal_header(nal)
                    if header.unit_type in (1, 5):
                        slice_header = parse_slice_header(nal, sps, pps)
                        assert slice_header.picture_parameter_set_id == pps.picture_parameter_set_id
                        assert slice_header.pic_order_cnt_type == sps.pic_order_cnt_type
                        assert slice_header.idr == (header.unit_type == 5)
                        slice_type = slice_header.slice_type % 5
                        assert slice_type in (0, 1, 2)
                        all_slice_types.add(slice_type)
                        sample_slice_count += 1
                    offset += size
            assert sample_slice_count > 0, f"{relative_path} has no parsed VCL slices"

    assert all_slice_types == {0, 1, 2}


def test_same_primary_picture_boundaries():
    base = parse_with(make_slice(0x41, 0, 2, 0, fixed_bits(2, 4) + fixed_bits(3, 4)))
    same = replace(base, first_macroblock_in_slice=12)
    assert same_primary_picture(base, same)

    for changed in (
        replace(base, frame_num=base.frame_num + 1),
        replace(base, picture_parameter_set_id=base.picture_parameter_set_id + 1),
        replace(base, field_pic_flag=True),
        replace(base, nal_ref_idc=0),
        replace(base, idr=True),
        replace(base, pic_order_cnt_lsb=base.pic_order_cnt_lsb + 1),
        replace(base, has_delta_pic_order_bottom=True, delta_pic_order_bottom=1),
        replace(base, separate_colour_plane=True),
        replace(base, separate_colour_plane=True, colour_plane_id=1),
    ):
        assert not same_primary_picture(base, changed)

    idr = parse_with(make_slice(0x65, 0, 2, 0, fixed_bits(2, 4) + ue_bits(1) + fixed_bits(3, 4)))
    assert not same_primary_picture(idr, replace(idr, idr_pic_id=2))

    poc_one = replace(base, pic_order_cnt_type=1, has_delta_pic_order_cnt0=True, delta_pic_order_cnt0=-1)
    assert not same_primary_picture(poc_one, replace(poc_one, delta_pic_order_cnt0=1))
    poc_two = replace(base, pic_order_cnt_type=2, has_pic_order_cnt_lsb=False)
    assert same_primary_picture(poc_two, replace(poc_two, pic_order_cnt_lsb=99))


def test_group_slices_into_primary_pictures():
    first = parse_with(make_slice(0x41, 0, 2, 0, fixed_bits(1, 4) + fixed_bits(2, 4)))
    another_slice = replace(first, first_macroblock_in_slice=10)
    next_picture = replace(first, frame_num=first.frame_num + 1)
    groups = group_slices_into_pictures([first, another_slice, next_picture])
    assert [[item.first_macroblock_in_slice for item in group] for group in groups] == [[0, 10], [0]]
    assert group_slices_into_pictures([]) == []