import pytest

from pygorvid import (
    BitReader,
    CABACArithmeticDecoder,
    CABACContextModel,
    CABACError,
    CABACChroma420References,
    CABACChroma420EdgeState,
    CABACIIntraMacroblockInput,
    CABACIntra4x4EdgeState,
    CABACIntra16x16EdgeState,
    CABACInterNeighbor,
    CABACTerminatedError,
    derive_cabac_mvd_context_increment,
    derive_cabac_reference_index_context_increment,
    derive_coded_block_flag_cond_term,
    initialize_i_intra_chroma_pred_mode_contexts,
    initialize_i_intra4x4_pred_mode_contexts,
    initialize_i_intra_mb_type_contexts,
    initialize_i_transform_size_8x8_contexts,
    initialize_inter_prediction_contexts,
    initialize_i_chroma_coded_block_pattern_contexts,
    initialize_i_luma_coded_block_pattern_contexts,
    initialize_i_luma4x4_coded_block_flag_contexts,
    initialize_i_mb_qp_delta_contexts,
    initialize_p_inter_mb_type_contexts,
    initialize_slice_contexts,
    reconstruct_intra16x16_luma_dc,
    LumaIntra4x4Block,
    LumaIntra8x8Block,
    reconstruct_intra16x16_luma_dc,
    place_chroma4x4_scan_levels,
    place_luma4x4_scan_levels,
    residual_context_bases,
    Yuv420FrameBuilder,
)
from pygorvid.cabac import _RANGE_LPS, derive_intra4x4_predicted_mode


def test_initializes_range_and_nine_bit_offset():
    decoder = CABACArithmeticDecoder(bytes((0b01010101, 0b10000000)))
    assert (decoder.code_range, decoder.code_offset) == (510, 171)
    for data, expected_offset in ((b"\x00\x00", 0), (b"\xfe\x80", 509)):
        endpoint = CABACArithmeticDecoder(data)
        assert (endpoint.code_range, endpoint.code_offset) == (510, expected_offset)
        assert endpoint._bits.read_bits(7) == 0
    with pytest.raises(CABACError, match="outside the arithmetic range"):
        CABACArithmeticDecoder(b"\xff\x80")
    with pytest.raises(CABACError, match="outside the arithmetic range"):
        CABACArithmeticDecoder(b"\xff\x00")
    with pytest.raises(CABACError, match="truncated bitstream"):
        CABACArithmeticDecoder(b"\xff")


def test_renormalizes_range_and_offset():
    decoder = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
    decoder._bits = BitReader(b"\xa0")
    decoder._code_range = 100
    decoder._code_offset = 40
    decoder.renormalize()
    assert (decoder.code_range, decoder.code_offset) == (400, 162)
    assert decoder._bits.read_bit()


def test_truncated_renormalization_rolls_back_state_and_input():
    decoder = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
    decoder._bits = BitReader(b"\x80")
    decoder._bits.read_bits(7)
    decoder._code_range = 64
    decoder._code_offset = 32
    with pytest.raises(CABACError, match="truncated bitstream"):
        decoder.renormalize()
    assert (decoder.code_range, decoder.code_offset) == (64, 32)
    assert not decoder._bits.read_bit()


def test_rejects_invalid_range():
    decoder = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
    decoder._bits = BitReader(b"")
    decoder._code_range = 0
    decoder._code_offset = 0
    with pytest.raises(CABACError, match="range"):
        decoder.renormalize()


def test_context_initialization_clips_pre_state_and_sets_mps():
    assert (CABACContextModel(0, 63, 26).state_index, CABACContextModel(0, 63, 26).mps) == (0, False)
    assert (CABACContextModel(0, 64, 26).state_index, CABACContextModel(0, 64, 26).mps) == (0, True)
    assert (CABACContextModel(0, 0, 26).state_index, CABACContextModel(0, 0, 26).mps) == (62, False)
    assert (CABACContextModel(0, 127, 26).state_index, CABACContextModel(0, 127, 26).mps) == (62, True)


def test_context_initialization_matches_signed_shift_boundary_vectors():
    for m, n, slice_qpy, expected in (
        (1, 63, 15, (0, False)),
        (1, 63, 16, (0, True)),
        (-1, 64, 15, (0, False)),
        (2, 0, 51, (57, False)),
        (-2, 64, 51, (6, False)),
        (-128, -128, 0, (62, False)),
        (127, 127, 51, (62, True)),
    ):
        model = CABACContextModel(m, n, slice_qpy)
        assert (model.state_index, model.mps) == expected

    initialize_p_inter_mb_type_contexts,

def test_i_intra_mb_type_contexts_match_table_912():
    contexts = initialize_i_intra_mb_type_contexts(26)
    assert [(model.state_index, model.mps) for model in contexts] == [
        (46, False), (6, False), (14, True), (17, True),
        (2, True), (20, False), (11, False), (1, False),
    ]
    for slice_qpy in (-1, 52):
        with pytest.raises(CABACError, match="initialization value"):
            initialize_i_intra_mb_type_contexts(slice_qpy)


def test_p_inter_mb_type_contexts_match_table_913():
    expected = (
        ((54, False), (14, False), (54, True), (6, False)),
        ((54, False), (22, False), (54, True), (1, True)),
        ((12, False), (1, False), (35, True), (47, False)),
    )
    for cabac_init_idc, expected_contexts in enumerate(expected):
        contexts = initialize_p_inter_mb_type_contexts(cabac_init_idc, 0)
        assert [(model.state_index, model.mps) for model in contexts] == list(expected_contexts)
    for cabac_init_idc, slice_qpy in ((-1, 26), (3, 26), (0, -1), (0, 52)):
        with pytest.raises(CABACError, match="outside"):
            initialize_p_inter_mb_type_contexts(cabac_init_idc, slice_qpy)


def test_inter_prediction_contexts_match_tables_915_and_916():
    mvd_x, mvd_y, ref_idx = initialize_inter_prediction_contexts(0, 0)
    assert [(model.state_index, model.mps) for model in mvd_x] == [
        (5, True), (17, True), (32, True), (8, False), (3, True), (22, True), (24, True)
    ]
    assert [(model.state_index, model.mps) for model in mvd_y] == [
        (5, False), (12, True), (30, True), (9, False), (5, True), (17, True), (24, True)
    ]
    assert [(model.state_index, model.mps) for model in ref_idx] == [
        (3, True), (10, True), (10, True), (16, True), (8, True), (5, False)
    ]
    for cabac_init_idc, slice_qpy in ((-1, 26), (3, 26), (0, -1), (0, 52)):
        with pytest.raises(CABACError, match="outside"):
            initialize_inter_prediction_contexts(cabac_init_idc, slice_qpy)


def test_inter_reference_index_context_increment():
    neighbor = CABACInterNeighbor(available=True, prediction_mode_matches=True, reference_index=1)
    assert derive_cabac_reference_index_context_increment(neighbor, CABACInterNeighbor()) == 1
    assert derive_cabac_reference_index_context_increment(CABACInterNeighbor(), neighbor) == 2
    assert derive_cabac_reference_index_context_increment(neighbor, neighbor) == 3
    for blocked in (
        CABACInterNeighbor(),
        CABACInterNeighbor(available=True, skip=True, prediction_mode_matches=True, reference_index=1),
        CABACInterNeighbor(available=True, intra=True, prediction_mode_matches=True, reference_index=1),
        CABACInterNeighbor(available=True, prediction_mode_matches=False, reference_index=1),
        CABACInterNeighbor(available=True, prediction_mode_matches=True, reference_index=0),
    ):
        assert derive_cabac_reference_index_context_increment(blocked, CABACInterNeighbor()) == 0
    field_neighbor = CABACInterNeighbor(
        available=True, prediction_mode_matches=True, reference_index=1, is_field=True
    )
    assert derive_cabac_reference_index_context_increment(
        field_neighbor, CABACInterNeighbor(), mbaff_frame=True
    ) == 0
    field_neighbor = CABACInterNeighbor(
        available=True, prediction_mode_matches=True, reference_index=2, is_field=True
    )
    assert derive_cabac_reference_index_context_increment(
        field_neighbor, CABACInterNeighbor(), mbaff_frame=True
    ) == 1

    decoder = CABACArithmeticDecoder(bytes(8))
    contexts = [_model(index, False) for index in range(6)]
    decoder.decode_reference_index_for_partition(1, neighbor, neighbor, contexts)
    assert [model.state_index for model in contexts] == [0, 1, 2, 4, 4, 5]


def test_inter_mvd_context_increment():
    def neighbor(x=0, y=0, **kwargs):
        return CABACInterNeighbor(
            available=True,
            prediction_mode_matches=True,
            motion_vector_difference=(x, y),
            **kwargs,
        )

    vectors = (
        (neighbor(1, 1), neighbor(1, 1), 0, False, False, 0),
        (neighbor(2, 2), neighbor(1, 1), 0, False, False, 1),
        (neighbor(20, 20), neighbor(13, 13), 0, False, False, 2),
        (neighbor(33, 33), CABACInterNeighbor(), 0, False, False, 2),
        (CABACInterNeighbor(motion_vector_difference=(90, 90)), CABACInterNeighbor(), 0, False, False, 0),
        (CABACInterNeighbor(available=True, skip=True, motion_vector_difference=(90, 90)), CABACInterNeighbor(), 0, False, False, 0),
        (CABACInterNeighbor(available=True, intra=True, motion_vector_difference=(90, 90)), CABACInterNeighbor(), 0, False, False, 0),
        (CABACInterNeighbor(available=True, motion_vector_difference=(90, 90)), CABACInterNeighbor(), 0, False, False, 0),
        (neighbor(0, 2, is_field=True), CABACInterNeighbor(), 1, True, False, 1),
        (neighbor(0, 7), CABACInterNeighbor(), 1, True, True, 1),
        (neighbor(2, 0, is_field=True), CABACInterNeighbor(), 0, True, False, 0),
        (CABACInterNeighbor(available=True, prediction_mode_matches=True, motion_vector_difference=(-(1 << 31), 0)), CABACInterNeighbor(), 0, False, False, 2),
    )
    for left, top, component, mbaff, current_field, expected in vectors:
        assert derive_cabac_mvd_context_increment(
            left, top, component, mbaff, current_field
        ) == expected
    with pytest.raises(CABACError, match="component index"):
        derive_cabac_mvd_context_increment(CABACInterNeighbor(), CABACInterNeighbor(), 2)

    decoder = CABACArithmeticDecoder(bytes(8))
    contexts = [_model(index, False) for index in range(7)]
    decoder.decode_motion_vector_difference_for_partition(
        0, neighbor(2, 0), neighbor(1, 0), contexts
    )
    assert [model.state_index for model in contexts] == [0, 2, 2, 3, 4, 5, 6]


def _states(models):
    return [(model.state_index, model.mps) for model in models]


def test_slice_contexts_match_syntax_initializers():
    contexts = _states(initialize_slice_contexts(7, 3, 26))
    assert len(contexts) == 460
    for first, models in (
        (3, initialize_i_intra_mb_type_contexts(26)),
        (60, initialize_i_mb_qp_delta_contexts(26)),
        (64, initialize_i_intra_chroma_pred_mode_contexts(26)),
        (68, initialize_i_intra4x4_pred_mode_contexts(26)),
        (73, initialize_i_luma_coded_block_pattern_contexts(26)),
        (77, initialize_i_chroma_coded_block_pattern_contexts(26)),
        (93, initialize_i_luma4x4_coded_block_flag_contexts(26)),
        (399, initialize_i_transform_size_8x8_contexts(26)),
    ):
        assert contexts[first:first + len(models)] == _states(models)
    for cabac_init_idc in range(3):
        contexts = _states(initialize_slice_contexts(5, cabac_init_idc, 30))
        mvd_x, mvd_y, ref_idx = initialize_inter_prediction_contexts(cabac_init_idc, 30)
        assert contexts[14:18] == _states(initialize_p_inter_mb_type_contexts(cabac_init_idc, 30))
        assert contexts[40:47] == _states(mvd_x)
        assert contexts[47:54] == _states(mvd_y)
        assert contexts[54:60] == _states(ref_idx)


def test_slice_contexts_use_standard_columns():
    for slice_type, cabac_init_idc, ctx_idx, expected in (
        (2, 0, 85, (31, True)),
        (2, 0, 227, (2, True)),
        (2, 0, 402, (28, True)),
        (0, 0, 105, (17, True)),
        (1, 1, 30, (10, False)),
        (6, 2, 459, (32, True)),
    ):
        model = initialize_slice_contexts(slice_type, cabac_init_idc, 26)[ctx_idx]
        assert (model.state_index, model.mps) == expected
    with pytest.raises(CABACError, match="slice type"):
        initialize_slice_contexts(10, 0, 26)
    for slice_type in (0, 1):
        with pytest.raises(CABACError, match="init idc"):
            initialize_slice_contexts(slice_type, 3, 26)
    with pytest.raises(CABACError, match="initialization value"):
        initialize_slice_contexts(2, 0, 52)


def test_residual_context_bases_follow_tables_934_and_940():
    assert [residual_context_bases(category) for category in range(6)] == [
        (85, 105, 166, 227), (89, 120, 181, 237), (93, 134, 195, 247),
        (97, 149, 210, 257), (101, 152, 213, 266), (None, 402, 417, 426),
    ]
    with pytest.raises(CABACError, match="ctxBlockCat"):
        residual_context_bases(6)


def _model(state_index, mps):
    model = CABACContextModel.__new__(CABACContextModel)
    model._state_index = state_index
    model._value_mps = mps
    return model


def _forced_residual_contexts(*mps_true):
    return [_model(61, index in mps_true) for index in range(460)]


def test_coded_block_flag_cond_term():
    for arguments, expected in (
        ((False, True, False, False, False), True),
        ((False, False, False, False, False), False),
        ((True, False, True, False, False), True),
        ((True, True, False, False, True), False),
        ((True, False, False, True, False), False),
        ((True, False, False, True, True), True),
    ):
        assert derive_coded_block_flag_cond_term(*arguments) is expected


def test_residual_block_chroma_dc_forced_bins():
    # With a zero offset every regular bin decodes as its MPS and every bypass bin as 0.
    contexts = _forced_residual_contexts(98, 149, 151, 212, 259)
    decoder = CABACArithmeticDecoder(bytes(16))
    levels, coded = decoder.decode_residual_block(3, True, False, contexts)
    assert coded and levels == [2, 0, 1] + [0] * 13
    used = {98, 149, 210, 150, 151, 212, 258, 259, 262}
    assert [model.state_index for model in contexts] == [
        62 if index in used else 61 for index in range(460)
    ]

    contexts = _forced_residual_contexts(97, 149, 150, 151, *range(257, 267))
    decoder = CABACArithmeticDecoder(bytes(64))
    levels, coded = decoder.decode_residual_block(3, False, False, contexts)
    assert coded and levels[:4] == [15, 15, 15, 15]
    # Category 3 caps the greater-than-one context at ctxIdx 265, leaving 266 to category 4.
    assert [contexts[index].state_index for index in (257, 265, 266)] == [62, 62, 61]


def test_residual_block_uncoded_and_category_range():
    contexts = _forced_residual_contexts()
    decoder = CABACArithmeticDecoder(bytes(4))
    assert decoder.decode_residual_block(0, True, True, contexts) == ([0] * 16, False)
    assert contexts[88].state_index == 62
    with pytest.raises(CABACError, match="ctxBlockCat"):
        decoder.decode_residual_block(5, False, False, contexts)
    with pytest.raises(CABACError, match="460 slice contexts"):
        decoder.decode_residual_block(0, False, False, contexts[:459])


def test_residual_block_truncation_is_transactional():
    contexts = _forced_residual_contexts(85, *range(105, 120), *range(227, 237))
    original = _states(contexts)
    decoder = CABACArithmeticDecoder(b"\x00\x00")
    with pytest.raises(CABACError, match="truncated"):
        decoder.decode_residual_block(0, False, False, contexts)
    assert _states(contexts) == original
    assert (decoder.code_range, decoder.code_offset, decoder._bits._bit_offset) == (510, 0, 9)


def test_residual_block_matches_bank_decoders():
    slice_contexts = initialize_slice_contexts(2, 0, 28)
    patterns = (
        bytes((0x5A, 0x3C, 0x91, 0x07, 0xE2, 0x48, 0xB3, 0x6F, 0x12, 0xC5, 0x7E, 0x29, 0x84, 0xD1, 0x3B, 0xF6, 0x55, 0xAA, 0x0F, 0xF0)),
        bytes((0x00, 0x7F, 0x10, 0x20, 0x40, 0x80, 0xFF, 0x01, 0x33, 0xCC, 0x99, 0x66, 0x11, 0xEE, 0x22, 0xDD, 0x44, 0xBB, 0x88, 0x77)),
        bytes((0x1F, 0xE0, 0x3E, 0xC1, 0x7C, 0x83, 0xF8, 0x07, 0xAB, 0xCD, 0xEF, 0x01, 0x23, 0x45, 0x67, 0x89, 0x9A, 0xBC, 0xDE, 0xF0)),
    )
    for category in (0, 1, 2, 4):
        bases = residual_context_bases(category)
        coded_cases = 0
        for data in patterns:
            for cond_terms in range(4):
                cond_a, cond_b = bool(cond_terms & 1), bool(cond_terms & 2)
                contexts = [_model(model.state_index, model.mps) for model in slice_contexts]
                generic = CABACArithmeticDecoder(data)
                banks = [
                    [_model(model.state_index, model.mps) for model in slice_contexts[base:base + length]]
                    for base, length in zip(bases, (4, 15, 15, 10))
                ]
                bank = CABACArithmeticDecoder(data)
                generic_error = bank_error = None
                try:
                    levels, coded = generic.decode_residual_block(category, cond_a, cond_b, contexts)
                except CABACError as error:
                    generic_error = error
                try:
                    if category in (0, 2):
                        want_raster, want_coded = bank.decode_luma4x4_residual_block_with_flag(
                            int(cond_a), int(cond_b), *banks
                        )
                    else:
                        want_raster, want_coded = bank.decode_chroma4x4_ac_residual_block(
                            cond_a, cond_b, *banks
                        )
                except CABACError as error:
                    bank_error = error
                assert (generic_error is None) == (bank_error is None)
                if generic_error is not None:
                    continue
                coded_cases += coded
                got_raster = (
                    place_luma4x4_scan_levels(levels)
                    if category in (0, 2)
                    else place_chroma4x4_scan_levels(0, levels[:15])
                )
                assert (got_raster, coded) == (want_raster, want_coded)
                assert (generic.code_range, generic.code_offset, generic._bits._bit_offset) == (
                    bank.code_range, bank.code_offset, bank._bits._bit_offset
                )
                for base, models in zip(bases, banks):
                    assert _states(contexts[base:base + len(models)]) == _states(models)
        assert coded_cases > 0


def test_luma8x8_residual_block_follows_table_943():
    # Only significance ctxIdxInc 7 (ctxIdx 409) has MPS 1, so significant positions reveal Table 9-43.
    positions = (23, 24, 25, 31, 32, 39, 63)
    contexts = _forced_residual_contexts(409)
    levels = CABACArithmeticDecoder(bytes(64)).decode_luma8x8_residual_block(contexts)
    assert levels == [1 if index in positions else 0 for index in range(64)]
    used = {*range(402, 417), 419, 420, 427, 428, 429, 430}
    assert [model.state_index for model in contexts] == [
        62 if index in used else 61 for index in range(460)
    ]

    contexts = _forced_residual_contexts(409, *range(426, 436))
    levels = CABACArithmeticDecoder(bytes(256)).decode_luma8x8_residual_block(contexts)
    assert levels == [15 if index in positions else 0 for index in range(64)]
    assert [contexts[index].state_index for index in range(426, 436)] == [
        61 if index in (428, 429, 430) else 62 for index in range(426, 436)
    ]


def test_luma8x8_residual_block_last_flag_and_truncation():
    contexts = _forced_residual_contexts(402, 417)
    levels = CABACArithmeticDecoder(bytes(8)).decode_luma8x8_residual_block(contexts)
    assert levels == [1] + [0] * 63

    contexts = _forced_residual_contexts(*range(402, 417), *range(426, 436))
    original = _states(contexts)
    decoder = CABACArithmeticDecoder(b"\x00\x00")
    with pytest.raises(CABACError, match="truncated"):
        decoder.decode_luma8x8_residual_block(contexts)
    assert _states(contexts) == original
    assert (decoder.code_range, decoder.code_offset, decoder._bits._bit_offset) == (510, 0, 9)
    with pytest.raises(CABACError, match="460 slice contexts"):
        decoder.decode_luma8x8_residual_block(contexts[:459])


def test_i_intra_prediction_contexts_match_tables_916_and_917():
    mode_contexts = initialize_i_intra4x4_pred_mode_contexts(26)
    assert [(model.state_index, model.mps) for model in mode_contexts] == [
        (1, False), (2, True)
    ]
    transform_contexts = initialize_i_transform_size_8x8_contexts(26)
    assert [(model.state_index, model.mps) for model in transform_contexts] == [
        (7, True), (17, True), (26, True)
    ]
    for initialize in (
        initialize_i_intra_chroma_pred_mode_contexts,
        initialize_i_intra4x4_pred_mode_contexts,
        initialize_i_transform_size_8x8_contexts,
    ):
        for slice_qpy in (-1, 52):
            with pytest.raises(CABACError, match="initialization value"):
                initialize(slice_qpy)


def test_i_intra_chroma_pred_mode_contexts_match_table_917():
    contexts = initialize_i_intra_chroma_pred_mode_contexts(26)
    assert [(model.state_index, model.mps) for model in contexts] == [
        (4, True), (28, True), (33, True), (3, False)
    ]


def test_context_update_flips_mps_at_state_zero_and_saturates():
    model = CABACContextModel(0, 64, 26)
    model.update(False)
    assert (model.state_index, model.mps) == (0, False)
    model.update(False)
    assert (model.state_index, model.mps) == (1, False)

    lps = CABACContextModel(0, 53, 26)
    lps.update(True)
    assert (lps.state_index, lps.mps) == (8, False)

    saturated = CABACContextModel(0, 0, 26)
    saturated.update(False)
    assert (saturated.state_index, saturated.mps) == (62, False)


def test_context_transitions_match_standard_vectors():
    lps_transitions = (
        0, 0, 1, 2, 2, 4, 4, 5, 6, 7, 8, 9, 9, 11, 11, 12,
        13, 13, 15, 15, 16, 16, 18, 18, 19, 19, 21, 21, 22, 22, 23, 24,
        24, 25, 26, 26, 27, 27, 28, 29, 29, 30, 30, 30, 31, 32, 32, 33,
        33, 33, 34, 34, 35, 35, 35, 36, 36, 37, 37, 37, 38, 38, 63, 63,
    )
    for state_index, expected_state in enumerate(lps_transitions):
        model = CABACContextModel.__new__(CABACContextModel)
        model._state_index = state_index
        model._value_mps = False
        model.update(True)
        assert (model.state_index, model.mps) == (expected_state, state_index == 0)

    for state_index in range(64):
        model = CABACContextModel.__new__(CABACContextModel)
        model._state_index = state_index
        model._value_mps = False
        model.update(False)
        expected_state = state_index + 1 if state_index < 62 else state_index
        assert (model.state_index, model.mps) == (expected_state, False)


def test_context_initialization_rejects_invalid_parameters():
    for parameters in (
        (-129, 0, 26),
        (128, 0, 26),
        (0, -129, 26),
        (0, 128, 26),
        (0, 0, -1),
        (0, 0, 52),
    ):
        with pytest.raises(CABACError, match="initialization value"):
            CABACContextModel(*parameters)


def test_bypass_bin_updates_offset_without_changing_range():
    for data, offset, expected_bin, expected_offset in (
        (b"\x00", 200, False, 400),
        (b"\x80", 260, True, 11),
    ):
        decoder = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
        decoder._bits = BitReader(data)
        decoder._code_range = 510
        decoder._code_offset = offset
        decoder._terminated = False
        assert decoder.decode_bypass_bin() is expected_bin
        assert (decoder.code_range, decoder.code_offset) == (510, expected_offset)


def test_bypass_bin_matches_threshold_vectors():
    for code_offset, expected_bin, expected_offset in (
        (254, False, 508),
        (255, True, 0),
    ):
        decoder = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
        decoder._bits = BitReader(b"\x00")
        decoder._code_range = 510
        decoder._code_offset = code_offset
        decoder._terminated = False
        assert decoder.decode_bypass_bin() is expected_bin
        assert (decoder.code_range, decoder.code_offset) == (510, expected_offset)
        assert decoder._bits._bit_offset == 1


def test_bypass_truncation_preserves_state_and_input():
    decoder = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
    decoder._bits = BitReader(b"")
    decoder._code_range = 510
    decoder._code_offset = 200
    decoder._terminated = False
    with pytest.raises(CABACError, match="truncated bitstream"):
        decoder.decode_bypass_bin()
    assert (decoder.code_range, decoder.code_offset, decoder._bits._bit_offset) == (510, 200, 0)


def test_terminate_bins_stop_or_renormalize():
    terminated = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
    terminated._bits = BitReader(b"")
    terminated._code_range = 300
    terminated._code_offset = 299
    terminated._terminated = False
    assert terminated.decode_terminate_bin()
    assert terminated.code_range == 298
    with pytest.raises(CABACTerminatedError):
        terminated.decode_bypass_bin()
    model = CABACContextModel.__new__(CABACContextModel)
    model._state_index = 10
    model._value_mps = True
    with pytest.raises(CABACTerminatedError):
        terminated.decode_bin(model)
    with pytest.raises(CABACTerminatedError):
        terminated.decode_terminate_bin()
    assert (terminated.code_range, terminated.code_offset, terminated._bits._bit_offset) == (298, 299, 0)
    assert (model.state_index, model.mps) == (10, True)
    with pytest.raises(CABACTerminatedError):
        terminated.renormalize()

    continuing = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
    continuing._bits = BitReader(b"\x80")
    continuing._code_range = 257
    continuing._code_offset = 0
    continuing._terminated = False
    assert not continuing.decode_terminate_bin()
    assert (continuing.code_range, continuing.code_offset) == (510, 1)
    assert continuing._bits.read_bits(1) == 0


def test_terminate_bin_matches_threshold_vectors():
    below = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
    below._bits = BitReader(b"")
    below._code_range = 300
    below._code_offset = 297
    below._terminated = False
    assert not below.decode_terminate_bin()
    assert (below.code_range, below.code_offset) == (298, 297)

    at = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
    at._bits = BitReader(b"")
    at._code_range = 300
    at._code_offset = 298
    at._terminated = False
    assert at.decode_terminate_bin()
    assert (at.code_range, at.code_offset) == (298, 298)


def test_terminate_truncation_preserves_state_and_input():
    decoder = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
    decoder._bits = BitReader(b"")
    decoder._code_range = 257
    decoder._code_offset = 0
    decoder._terminated = False
    with pytest.raises(CABACError, match="truncated bitstream"):
        decoder.decode_terminate_bin()
    assert (decoder.code_range, decoder.code_offset, decoder._bits._bit_offset) == (257, 0, 0)


def test_regular_bin_decodes_mps_and_lps_and_updates_context():
    mps_decoder = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
    mps_decoder._bits = BitReader(b"")
    mps_decoder._code_range = 510
    mps_decoder._code_offset = 0
    mps_decoder._terminated = False
    mps_model = CABACContextModel(0, 63, 26)
    assert not mps_decoder.decode_bin(mps_model)
    assert (mps_decoder.code_range, mps_decoder.code_offset) == (270, 0)
    assert (mps_model.state_index, mps_model.mps) == (1, False)

    lps_decoder = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
    lps_decoder._bits = BitReader(b"\x80")
    lps_decoder._code_range = 510
    lps_decoder._code_offset = 270
    lps_decoder._terminated = False
    lps_model = CABACContextModel(0, 63, 26)
    assert lps_decoder.decode_bin(lps_model)
    assert (lps_decoder.code_range, lps_decoder.code_offset) == (480, 1)
    assert (lps_model.state_index, lps_model.mps) == (0, True)


def test_regular_bin_selects_range_class_and_rolls_back_on_truncation():
    decoder = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
    decoder._bits = BitReader(b"\x80")
    decoder._code_range = 300
    decoder._code_offset = 0
    decoder._terminated = False
    model = CABACContextModel(0, 63, 26)
    assert not decoder.decode_bin(model)
    assert (decoder.code_range, decoder.code_offset, decoder._bits._bit_offset) == (344, 1, 1)

    for code_range, data, expected_range, expected_offset in (
        (300, b"\x80", 344, 1),
        (350, b"\x80", 348, 1),
        (410, b"\x00", 404, 0),
        (510, b"", 270, 0),
    ):
        class_decoder = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
        class_decoder._bits = BitReader(data)
        class_decoder._code_range = code_range
        class_decoder._code_offset = 0
        class_decoder._terminated = False
        class_model = CABACContextModel(0, 63, 26)
        assert not class_decoder.decode_bin(class_model)
        assert (class_decoder.code_range, class_decoder.code_offset) == (expected_range, expected_offset)

    state_ten = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
    state_ten._bits = BitReader(b"\x00")
    state_ten._code_range = 510
    state_ten._code_offset = 368
    state_ten._terminated = False
    state_ten_model = CABACContextModel(0, 53, 26)
    assert state_ten.decode_bin(state_ten_model)
    assert (state_ten.code_range, state_ten.code_offset) == (284, 0)
    assert (state_ten_model.state_index, state_ten_model.mps) == (8, False)

    truncated = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
    truncated._bits = BitReader(b"")
    truncated._code_range = 510
    truncated._code_offset = 270
    truncated._terminated = False
    unchanged = CABACContextModel(0, 63, 26)
    with pytest.raises(CABACError, match="truncated bitstream"):
        truncated.decode_bin(unchanged)
    assert (truncated.code_range, truncated.code_offset, truncated._bits._bit_offset) == (510, 270, 0)
    assert (unchanged.state_index, unchanged.mps) == (0, False)


def test_regular_bin_matches_range_mps_boundary_vectors():
    below = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
    below._bits = BitReader(b"")
    below._code_range = 510
    below._code_offset = 269
    below._terminated = False
    below_model = CABACContextModel(0, 63, 26)
    assert not below.decode_bin(below_model)
    assert (below.code_range, below.code_offset) == (270, 269)
    assert (below_model.state_index, below_model.mps) == (1, False)

    above = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
    above._bits = BitReader(b"\x00")
    above._code_range = 510
    above._code_offset = 271
    above._terminated = False
    above_model = CABACContextModel(0, 63, 26)
    assert above.decode_bin(above_model)
    assert (above.code_range, above.code_offset, above._bits._bit_offset) == (480, 2, 1)
    assert (above_model.state_index, above_model.mps) == (0, True)


def test_range_lps_matches_standard_vectors():
    range_classes = (256, 320, 384, 448)
    range_lps = (
        (128, 128, 128, 123, 116, 111, 105, 100, 95, 90, 85, 81, 77, 73, 69, 66, 62, 59, 56, 53, 51, 48, 46, 43, 41, 39, 37, 35, 33, 32, 30, 29, 27, 26, 24, 23, 22, 21, 20, 19, 18, 17, 16, 15, 14, 14, 13, 12, 12, 11, 11, 10, 10, 9, 9, 8, 8, 7, 7, 7, 6, 6, 6, 2),
        (176, 167, 158, 150, 142, 135, 128, 122, 116, 110, 104, 99, 94, 89, 85, 80, 76, 72, 69, 65, 62, 59, 56, 53, 50, 48, 45, 43, 41, 39, 37, 35, 33, 31, 30, 28, 27, 26, 24, 23, 22, 21, 20, 19, 18, 17, 16, 15, 14, 14, 13, 12, 12, 11, 11, 10, 9, 9, 9, 8, 8, 7, 7, 2),
        (208, 197, 187, 178, 169, 160, 152, 144, 137, 130, 123, 117, 111, 105, 100, 95, 90, 86, 81, 77, 73, 69, 66, 63, 59, 56, 54, 51, 48, 46, 43, 41, 39, 37, 35, 33, 32, 30, 29, 27, 26, 25, 23, 22, 21, 20, 19, 18, 17, 16, 15, 15, 14, 13, 12, 12, 11, 11, 10, 10, 9, 9, 8, 2),
        (240, 227, 216, 205, 195, 185, 175, 166, 158, 150, 142, 135, 128, 122, 116, 110, 104, 99, 94, 89, 85, 80, 76, 72, 69, 65, 62, 59, 56, 53, 50, 48, 45, 43, 41, 39, 37, 35, 33, 31, 30, 28, 27, 25, 24, 23, 22, 21, 20, 19, 18, 17, 16, 15, 14, 14, 13, 12, 12, 11, 11, 10, 9, 2),
    )
    for class_index, code_range in enumerate(range_classes):
        for context_state, lps_range in enumerate(range_lps[class_index]):
            decoder = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
            decoder._bits = BitReader(b"\x00\x00")
            decoder._code_range = code_range
            decoder._code_offset = code_range - lps_range
            decoder._terminated = False
            model = CABACContextModel.__new__(CABACContextModel)
            model._state_index = context_state
            model._value_mps = False

            assert decoder.decode_bin(model)
            normalized_range = lps_range
            renormalization_bits = 0
            while normalized_range < 256:
                normalized_range <<= 1
                renormalization_bits += 1
            assert (decoder.code_range, decoder.code_offset) == (normalized_range, 0)
            assert decoder._bits._bit_offset == renormalization_bits
            assert model.mps is (context_state == 0)


def test_mb_qp_delta_decodes_signed_value_and_selects_context():
    make_contexts = lambda: [
        CABACContextModel(0, 63, 26),
        CABACContextModel(0, 63, 26),
        CABACContextModel(0, 64, 26),
        CABACContextModel(0, 64, 26),
    ]
    for data, offset, previous_delta, expected in (
        (b"", 0, 0, 0),
        (b"\x00", 390, 0, 1),
        (b"\x00", 330, 0, -1),
        (b"", 0, 1, 0),
    ):
        decoder = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
        decoder._bits = BitReader(data)
        decoder._code_range = 510
        decoder._code_offset = offset
        decoder._terminated = False
        contexts = make_contexts()
        assert decoder.decode_mb_qp_delta(contexts, previous_delta) == expected
        if previous_delta:
            assert (contexts[0].state_index, contexts[0].mps) == (0, False)
            assert contexts[1].state_index == 1


def test_mb_qp_delta_truncation_preserves_decoder_and_contexts():
    decoder = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
    decoder._bits = BitReader(b"")
    decoder._code_range = 510
    decoder._code_offset = 390
    decoder._terminated = False
    contexts = [
        CABACContextModel(0, 63, 26),
        CABACContextModel(0, 63, 26),
        CABACContextModel(0, 64, 26),
        CABACContextModel(0, 64, 26),
    ]
    with pytest.raises(CABACError, match="truncated bitstream"):
        decoder.decode_mb_qp_delta(contexts, 0)
    assert (decoder.code_range, decoder.code_offset, decoder._bits._bit_offset) == (510, 390, 0)
    assert (contexts[0].state_index, contexts[0].mps) == (0, False)


def test_mb_qp_delta_rejects_invalid_context_bank():
    decoder = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
    decoder._bits = BitReader(b"")
    decoder._code_range = 510
    decoder._code_offset = 0
    decoder._terminated = False
    with pytest.raises(CABACError, match="context models 60 through 63"):
        decoder.decode_mb_qp_delta([], 0)


def test_mb_skip_flag_derives_p_and_b_context_from_neighbors():
    for slice_type, neighbors, expected_context in (
        (0, (False, False, False, False), 0),
        (5, (True, False, True, True), 1),
        (6, (True, False, True, False), 2),
    ):
        decoder = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
        decoder._bits = BitReader(b"")
        decoder._code_range = 510
        decoder._code_offset = 0
        decoder._terminated = False
        contexts = [CABACContextModel(0, 64, 26) for _ in range(3)]
        contexts[expected_context] = CABACContextModel(0, 63, 26)
        assert not decoder.decode_mb_skip_flag(slice_type, contexts, *neighbors)
        assert [model.state_index for model in contexts] == [
            1 if index == expected_context else 0 for index in range(3)
        ]


def test_mb_skip_flag_rejects_intra_slice_types():
    decoder = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
    decoder._bits = BitReader(b"")
    decoder._code_range = 510
    decoder._code_offset = 0
    decoder._terminated = False
    contexts = [CABACContextModel(0, 63, 26) for _ in range(3)]
    for slice_type in (2, 3, 4, 7, 8, 9, 10):
        with pytest.raises(CABACError, match="unsupported for this slice type"):
            decoder.decode_mb_skip_flag(slice_type, contexts, False, False, False, False)


@pytest.mark.parametrize(
    ("maximum", "increment", "mps", "expected"),
    (
        (0, 0, [False] * 6, 0),
        (3, 2, [False] * 6, 0),
        (3, 1, [False, True, False, False, False, False], 1),
        (3, 0, [True, False, False, False, True, False], 2),
        (3, 3, [False, False, False, True, True, True], 3),
    ),
)
def test_reference_index_decodes_truncated_unary(maximum, increment, mps, expected):
    decoder = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
    decoder._bits = BitReader(b"")
    decoder._code_range = 510
    decoder._code_offset = 0
    decoder._terminated = False
    contexts = [CABACContextModel.__new__(CABACContextModel) for _ in range(6)]
    for model, value_mps in zip(contexts, mps):
        model._state_index = 63
        model._value_mps = value_mps
    assert decoder.decode_reference_index(maximum, increment, contexts) == expected


def test_i_intra_mb_type_decodes_intra_nxn_i16x16_and_pcm():
    def make_contexts():
        return [CABACContextModel(0, 63, 26) for _ in range(8)]

    intra_nxn = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
    intra_nxn._bits = BitReader(b"")
    intra_nxn._code_range = 510
    intra_nxn._code_offset = 0
    intra_nxn._terminated = False
    contexts = make_contexts()
    assert intra_nxn.decode_i_intra_mb_type(2, contexts, False, False, False, False) == 0

    i16x16 = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
    i16x16._bits = BitReader(b"\x00")
    i16x16._code_range = 510
    i16x16._code_offset = 0
    i16x16._terminated = False
    contexts = make_contexts()
    contexts[0] = CABACContextModel(0, 64, 26)
    assert i16x16.decode_i_intra_mb_type(7, contexts, False, False, False, False) == 1

    pcm = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
    pcm._bits = BitReader(b"\x80")
    pcm._code_range = 510
    pcm._code_offset = 509
    pcm._terminated = False
    contexts = make_contexts()
    assert pcm.decode_i_intra_mb_type(2, contexts, False, False, False, False) == 25
    assert pcm._terminated


def test_intra_nxn_4x4_luma_modes_and_assembly():
    decoder = CABACArithmeticDecoder(bytes(64))
    contexts = [_model(63, False) for _ in range(460)]
    blocks = [
        LumaIntra4x4Block(2, top=(0,) * 8, left=(0,) * 8, top_left=0)
        for _ in range(16)
    ]
    result = decoder.decode_intra_nxn_4x4_luma_macroblock(
        contexts,
        0,
        0,
        (16,) * 16,
        (2,) * 4,
        (2,) * 4,
        True,
        True,
        CABACIntra4x4EdgeState(),
        CABACIntra4x4EdgeState(),
        blocks,
    )
    assert result.modes[0] == 0
    assert result.coded_block_flags == (False,) * 16
    assert result.samples == (0,) * 256


def test_intra_nxn_4x4_luma_residual_and_neighbor_cbf():
    decoder = CABACArithmeticDecoder(bytes(128))
    contexts = [_model(63, False) for _ in range(460)]
    contexts[88]._value_mps = True
    contexts[105]._value_mps = True
    contexts[166]._value_mps = True
    blocks = [
        LumaIntra4x4Block(2, top=(100,) * 8, left=(100,) * 8, top_left=100)
        for _ in range(16)
    ]
    result = decoder.decode_intra_nxn_4x4_luma_macroblock(
        contexts,
        1,
        51,
        (16,) * 16,
        (2,) * 4,
        (2,) * 4,
        True,
        True,
        CABACIntra4x4EdgeState(),
        CABACIntra4x4EdgeState(),
        blocks,
    )
    assert result.coded_block_flags[0]
    assert any(result.residuals[0])


def test_intra_nxn_4x4_luma_failure_rolls_back():
    decoder = CABACArithmeticDecoder(bytes(64))
    contexts = [_model(63, False) for _ in range(460)]
    original_contexts = _states(contexts)
    original_state = (decoder.code_range, decoder.code_offset, decoder._bits._bit_offset)
    blocks = [LumaIntra4x4Block(2) for _ in range(16)]
    with pytest.raises(ValueError, match="top references"):
        decoder.decode_intra_nxn_4x4_luma_macroblock(
            contexts,
            0,
            0,
            (16,) * 16,
            (2,) * 4,
            (2,) * 4,
            False,
            False,
            CABACIntra4x4EdgeState(),
            CABACIntra4x4EdgeState(),
            blocks,
        )
    assert (decoder.code_range, decoder.code_offset, decoder._bits._bit_offset) == original_state
    assert _states(contexts) == original_contexts


def test_intra_nxn_8x8_luma_modes_and_transform_flag():
    decoder = CABACArithmeticDecoder(bytes(128))
    contexts = [_model(63, False) for _ in range(460)]
    contexts[399]._value_mps = True
    blocks = [
        LumaIntra8x8Block(
            2,
            top=(0,) * 16,
            left=(0,) * 16,
            top_left=0,
        )
        for _ in range(4)
    ]
    result = decoder.decode_intra_nxn_8x8_luma_macroblock(
        contexts, True, False, False, 0, 0, (16,) * 64,
        (2, 2), (2, 2), True, True, blocks,
    )
    assert result.transform_size_8x8
    assert result.modes[0] == 0
    assert result.samples == (0,) * 256
    assert result.residuals == ((0,) * 64,) * 4


def test_intra_chroma420_macroblock_decodes_dc_and_ac_cbp_for_both_components():
    for coded_block_pattern_chroma in (1, 2):
        decoder = CABACArithmeticDecoder(bytes(128))
        contexts = [_model(63, False) for _ in range(460)]
        result = decoder.decode_intra_chroma420_macroblock(
            contexts,
            0,
            False,
            0,
            False,
            False,
            False,
            coded_block_pattern_chroma,
            26,
            (0, 0),
            ((16,) * 16, (16,) * 16),
            (CABACChroma420References(), CABACChroma420References()),
            (CABACChroma420EdgeState(), CABACChroma420EdgeState()),
            (CABACChroma420EdgeState(), CABACChroma420EdgeState()),
        )
        assert result.prediction_mode == 0
        assert result.qpc == (26, 26)
        assert result.cb == result.cr == (128,) * 64
        assert result.cb_residual == result.cr_residual == (0,) * 64

    decoder = CABACArithmeticDecoder(bytes(128))
    contexts = _forced_residual_contexts(100, 104)
    result = decoder.decode_intra_chroma420_macroblock(
        contexts,
        0,
        False,
        0,
        False,
        False,
        False,
        2,
        26,
        (0, 0),
        ((16,) * 16, (16,) * 16),
        (CABACChroma420References(), CABACChroma420References()),
        (CABACChroma420EdgeState(), CABACChroma420EdgeState()),
        (CABACChroma420EdgeState(), CABACChroma420EdgeState()),
    )
    assert any(result.cb_residual)
    assert any(result.cr_residual)
    assert result.dc_coded == (True, True)
    assert result.ac_coded_block_flags[0][0]
    assert result.ac_coded_block_flags[1][0]


def test_i16x16_chroma_prediction_uses_encoded_mode_for_both_components():
    decoder = CABACArithmeticDecoder(bytes(16))
    contexts = [_model(63, False) for _ in range(460)]
    references = (
        CABACChroma420References(left_available=True, left=(20, 30, 40, 50, 60, 70, 80, 90)),
        CABACChroma420References(left_available=True, left=(100, 110, 120, 130, 140, 150, 160, 170)),
    )
    edges = (CABACChroma420EdgeState(), CABACChroma420EdgeState())
    result = decoder.decode_intra_chroma420_macroblock(
        contexts,
        1,
        True,
        2,
        True,
        False,
        False,
        0,
        26,
        (0, 0),
        ((16,) * 16, (16,) * 16),
        references,
        edges,
        edges,
    )
    assert result.prediction_mode == 1
    assert result.cb[:8] == (20,) * 8
    assert result.cb[56:] == (90,) * 8
    assert result.cr[:8] == (100,) * 8
    assert result.cr[56:] == (170,) * 8

    mb_type_contexts = _forced_residual_contexts(100, 104)
    mb_type_result = CABACArithmeticDecoder(bytes(128)).decode_intra_chroma420_macroblock(
        mb_type_contexts,
        0,
        True,
        9,
        True,
        False,
        False,
        0,
        26,
        (0, 0),
        ((16,) * 16, (16,) * 16),
        (CABACChroma420References(), CABACChroma420References()),
        edges,
        edges,
    )
    assert mb_type_result.prediction_mode == 0
    assert any(mb_type_result.cb_residual)
    assert any(mb_type_result.cr_residual)


def test_i_intra_macroblock_dispatches_and_places_all_planes():
    decoder = CABACArithmeticDecoder(bytes(128))
    contexts = [_model(63, False) for _ in range(460)]
    blocks = tuple(
        LumaIntra4x4Block(
            2,
            top=(90,) * 8,
            left=(90,) * 8,
            top_left=90,
        )
        for _ in range(16)
    )
    input = CABACIIntraMacroblockInput(
        slice_type=2,
        previous_qpy=26,
        luma_4x4_blocks=blocks,
    )
    builder = Yuv420FrameBuilder(1, 1)
    result = decoder.decode_i_intra_macroblock(input, contexts, builder, 0)
    frame = builder.finish()
    assert result.macroblock_type == 0
    assert result.coded_block_pattern_luma == result.coded_block_pattern_chroma == 0
    assert result.qpy == 26
    assert tuple(frame.y) == result.luma
    assert tuple(frame.u) == result.cb
    assert tuple(frame.v) == result.cr


def test_i16x16_dispatch_derives_chroma_cbp_but_decodes_chroma_mode():
    decoder = CABACArithmeticDecoder(bytes(128))
    contexts = [_model(63, False) for _ in range(460)]
    contexts[3]._value_mps = True
    contexts[7]._value_mps = True
    contexts[8]._value_mps = True
    input = CABACIIntraMacroblockInput(
        slice_type=2,
        previous_qpy=26,
        intra16x16_top=(90,) * 16,
        intra16x16_top_available=True,
    )
    builder = Yuv420FrameBuilder(1, 1)
    result = decoder.decode_i_intra_macroblock(input, contexts, builder, 0)
    assert result.macroblock_type == 9
    assert result.coded_block_pattern_chroma == 2
    assert result.qpy == 26
    assert result.luma == (90,) * 256
    assert builder.finish().u == bytes((128,)) * 64


def test_i_pcm_dispatch_places_planes_and_restarts_cabac():
    data = bytes((0x80,)) + bytes((0x11,)) * 256 + bytes((0x22,)) * 64 + bytes((0x33,)) * 64 + bytes(2)
    decoder = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
    decoder._bits = BitReader(data)
    decoder._code_range = 510
    decoder._code_offset = 509
    decoder._terminated = False
    contexts = [_model(0, False) for _ in range(460)]
    builder = Yuv420FrameBuilder(1, 1)
    result = decoder.decode_i_intra_macroblock(
        CABACIIntraMacroblockInput(slice_type=2), contexts, builder, 0
    )
    assert result.macroblock_type == 25
    assert (decoder.code_range, decoder.code_offset, decoder._terminated) == (510, 0, False)
    frame = builder.finish()
    assert frame.y == bytes((0x11,)) * 256
    assert frame.u == bytes((0x22,)) * 64
    assert frame.v == bytes((0x33,)) * 64


def test_i_nxn_dispatch_selects_8x8_transform_branch():
    decoder = CABACArithmeticDecoder(bytes(128))
    contexts = [_model(63, False) for _ in range(460)]
    contexts[399]._value_mps = True
    blocks = tuple(
        LumaIntra8x8Block(
            2,
            top=(90,) * 16,
            left=(90,) * 16,
            top_left=90,
        )
        for _ in range(4)
    )
    input = CABACIIntraMacroblockInput(
        slice_type=2,
        previous_qpy=26,
        transform_8x8_mode_enabled=True,
        luma_8x8_blocks=blocks,
    )
    result = decoder.decode_i_intra_macroblock(
        input, contexts, Yuv420FrameBuilder(1, 1), 0
    )
    assert result.transform_size_8x8
    assert result.luma == (90,) * 256


def test_i_intra_macroblock_builder_failure_rolls_back_cabac_state():
    decoder = CABACArithmeticDecoder(bytes(128))
    contexts = [_model(63, False) for _ in range(460)]
    original_contexts = _states(contexts)
    original_decoder = (decoder.code_range, decoder.code_offset, decoder._bits._bit_offset)
    builder = Yuv420FrameBuilder(1, 1)
    builder.place_macroblock(0, bytes(256), bytes(64), bytes(64))
    blocks = tuple(
        LumaIntra4x4Block(
            2,
            top=(90,) * 8,
            left=(90,) * 8,
            top_left=90,
        )
        for _ in range(16)
    )
    with pytest.raises(ValueError, match="invalid or duplicated"):
        decoder.decode_i_intra_macroblock(
            CABACIIntraMacroblockInput(slice_type=2, previous_qpy=26, luma_4x4_blocks=blocks),
            contexts,
            builder,
            0,
        )
    assert (decoder.code_range, decoder.code_offset, decoder._bits._bit_offset) == original_decoder
    assert _states(contexts) == original_contexts


def test_intra_nxn_8x8_luma_category5_residual_and_rollback():
    decoder = CABACArithmeticDecoder(bytes(128))
    contexts = [_model(63, False) for _ in range(460)]
    contexts[399]._value_mps = True
    contexts[402]._value_mps = True
    contexts[417]._value_mps = True
    blocks = [
        LumaIntra8x8Block(
            2,
            top=(100,) * 16,
            left=(100,) * 16,
            top_left=100,
        )
        for _ in range(4)
    ]
    result = decoder.decode_intra_nxn_8x8_luma_macroblock(
        contexts, True, False, False, 1, 51, (16,) * 64,
        (2, 2), (2, 2), True, True, blocks,
    )
    assert result.transform_size_8x8
    assert any(result.residuals[0])

    truncated = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
    truncated._bits = BitReader(b"")
    truncated._code_range = 510
    truncated._code_offset = 0
    truncated._terminated = False
    truncated_contexts = [_model(63, False) for _ in range(460)]
    truncated_contexts[399]._value_mps = True
    before = (truncated.code_range, truncated.code_offset, truncated._bits._bit_offset)
    original_contexts = _states(truncated_contexts)
    with pytest.raises(CABACError):
        truncated.decode_intra_nxn_8x8_luma_macroblock(
            truncated_contexts, True, False, False, 1, 0, (16,) * 64,
            (2, 2), (2, 2), True, True, blocks,
        )
    assert (truncated.code_range, truncated.code_offset, truncated._bits._bit_offset) == before
    assert _states(truncated_contexts) == original_contexts


def test_reconstruct_i16x16_luma_dc_hadamard_and_scaling():
    assert reconstruct_intra16x16_luma_dc([1] + [0] * 15, [16] * 16, 51) == [896] * 16
    with pytest.raises(ValueError, match="scaling list"):
        reconstruct_intra16x16_luma_dc([1] + [0] * 15, [0] * 16, 0)


def test_decode_i16x16_luma_dc_and_prediction():
    decoder = CABACArithmeticDecoder(bytes(64))
    contexts = [_model(63, False) for _ in range(460)]
    for index in (96, 134, 195):
        contexts[index]._value_mps = True
    result = decoder.decode_intra16x16_luma_macroblock(
        1,
        contexts,
        51,
        [16] * 16,
        [100] * 16,
        None,
        0,
        False,
        CABACIntra16x16EdgeState(),
        CABACIntra16x16EdgeState(),
    )
    assert (result.prediction_mode, result.coded_block_pattern_luma, result.dc_coded) == (0, 0, True)
    assert any(result.residual)
    assert result.samples[0] != 100


def test_decode_i16x16_luma_ac_residuals():
    decoder = CABACArithmeticDecoder(bytes(128))
    contexts = [_model(63, False) for _ in range(460)]
    for index in (92, 96, 120, 134, 166, 181):
        contexts[index]._value_mps = True
    result = decoder.decode_intra16x16_luma_macroblock(
        13,
        contexts,
        51,
        [16] * 16,
        [100] * 16,
        None,
        0,
        False,
        CABACIntra16x16EdgeState(),
        CABACIntra16x16EdgeState(),
    )
    assert result.coded_block_pattern_luma == 15
    assert all(result.ac_coded_block_flags)
    assert any(result.residual)


def test_i16x16_luma_dc_hadamard_scaling_and_decode():
    levels = [1] + [0] * 15
    scaled = reconstruct_intra16x16_luma_dc(levels, (16,) * 16, 51)
    assert scaled == [896] * 16

    decoder = CABACArithmeticDecoder(bytes(64))
    contexts = [_model(63, False) for _ in range(460)]
    for index in (96, 134, 195):
        contexts[index]._value_mps = True
    top = (100,) * 16
    result = decoder.decode_intra16x16_luma_macroblock(
        1, contexts, 51, (16,) * 16, top, None, 0, False,
        CABACIntra16x16EdgeState(), CABACIntra16x16EdgeState(),
    )
    assert result.prediction_mode == 0
    assert result.coded_block_pattern_luma == 0
    assert result.dc_coded
    assert any(result.residual)
    assert result.samples[0] != 100


def test_i16x16_luma_ac_residuals_decode():
    decoder = CABACArithmeticDecoder(bytes(128))
    contexts = [_model(63, False) for _ in range(460)]
    for index in (92, 96, 120, 134, 166, 181):
        contexts[index]._value_mps = True
    top = (100,) * 16
    result = decoder.decode_intra16x16_luma_macroblock(
        13, contexts, 51, (16,) * 16, top, None, 0, False,
        CABACIntra16x16EdgeState(), CABACIntra16x16EdgeState(),
    )
    assert result.coded_block_pattern_luma == 15
    assert all(result.ac_coded_block_flags)
    assert any(result.residual)


def test_ipcm_intra_macroblock_places_samples_and_restarts_cabac():
    data = bytearray(1 + 256 + 64 + 64 + 2)
    data[0] = 0x80
    data[1:257] = bytes((0x11,)) * 256
    data[257:321] = bytes((0x22,)) * 64
    data[321:385] = bytes((0x33,)) * 64
    decoder = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
    decoder._bits = BitReader(bytes(data))
    decoder._code_range = 510
    decoder._code_offset = 509
    decoder._terminated = False
    contexts = [_model(0, False) for _ in range(8)]
    builder = Yuv420FrameBuilder(1, 1)

    decoder.decode_ipcm_intra_macroblock(2, contexts, False, False, False, False, builder, 0)

    assert (decoder.code_range, decoder.code_offset, decoder._terminated) == (510, 0, False)
    assert decoder._bits._bit_offset == 8 + 384 * 8 + 9
    assert contexts[0].mps
    frame = builder.finish()
    assert len(frame.y) == 256 and frame.y[0] == frame.y[-1] == 0x11
    assert len(frame.u) == 64 and frame.u[0] == frame.u[-1] == 0x22
    assert len(frame.v) == 64 and frame.v[0] == frame.v[-1] == 0x33


@pytest.mark.parametrize(
    ("data", "message"),
    (
        (b"\x80\x11", "truncated I_PCM samples"),
        (b"\xc0", "alignment bit is not zero"),
        (b"\x80" + bytes((0x11,)) * 384 + b"\xff\x00", "initial offset"),
    ),
)
def test_ipcm_intra_macroblock_failures_are_transactional(data, message):
    decoder = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
    decoder._bits = BitReader(data)
    decoder._code_range = 510
    decoder._code_offset = 509
    decoder._terminated = False
    contexts = [_model(0, False) for _ in range(8)]
    original_contexts = _states(contexts)
    original_state = (decoder.code_range, decoder.code_offset, decoder._bits._bit_offset)
    builder = Yuv420FrameBuilder(1, 1)
    with pytest.raises(CABACError, match=message):
        decoder.decode_ipcm_intra_macroblock(2, contexts, False, False, False, False, builder, 0)
    assert (decoder.code_range, decoder.code_offset, decoder._bits._bit_offset) == original_state
    assert _states(contexts) == original_contexts
    assert not builder._written[0]


class _TestEncoder:
    """Clause 9.3.4.2 arithmetic encoder, used only to build decoder vectors."""

    def __init__(self):
        self.low, self.code_range, self.outstanding = 0, 510, 0
        self.first_bit, self.flushed = True, False
        self.bits = []

    def _put_bit(self, bit):
        if self.first_bit:
            self.first_bit = False
        else:
            self.bits.append(bit)
        self.bits.extend([1 - bit] * self.outstanding)
        self.outstanding = 0

    def _renormalize(self):
        while self.code_range < 256:
            if self.low < 256:
                self._put_bit(0)
            elif self.low >= 512:
                self.low -= 512
                self._put_bit(1)
            else:
                self.low -= 256
                self.outstanding += 1
            self.code_range <<= 1
            self.low <<= 1

    def decision(self, model, bin_value):
        range_lps = _RANGE_LPS[(self.code_range >> 6) & 3][model._state_index]
        self.code_range -= range_lps
        if bin_value != model._value_mps:
            self.low += self.code_range
            self.code_range = range_lps
        model.update(bin_value)
        self._renormalize()

    def terminate(self, bin_value):
        self.code_range -= 2
        if not bin_value:
            self._renormalize()
            return
        self.low += self.code_range
        self.code_range = 2
        self._renormalize()
        self._put_bit((self.low >> 9) & 1)
        self.bits.extend([(self.low >> 8) & 1, 1])
        self.flushed = True

    def encode_bin_string(self, contexts, bins, ctx_idx):
        for index, char in enumerate(bins):
            context = ctx_idx(index, bins[:index])
            if context == 276:
                self.terminate(char == "1")
            else:
                self.decision(contexts[context], char == "1")

    def data(self):
        if not self.flushed:
            self.terminate(True)
        data = bytearray((len(self.bits) + 7) // 8 + 4)
        for index, bit in enumerate(self.bits):
            if bit:
                data[index // 8] |= 0x80 >> (index % 8)
        return bytes(data)


# Table 9-36 I mb_type bin strings and Table 9-37 B mb_type bin strings.
_I_MB_TYPE_BINS = (
    "0", "100000", "100001", "100010", "100011", "1001000", "1001001", "1001010", "1001011",
    "1001100", "1001101", "1001110", "1001111", "101000", "101001", "101010", "101011",
    "1011000", "1011001", "1011010", "1011011", "1011100", "1011101", "1011110", "1011111", "11",
)
_B_MB_TYPE_BINS = (
    "0", "100", "101", "110000", "110001", "110010", "110011", "110100", "110101", "110110", "110111",
    "111110", "1110000", "1110001", "1110010", "1110011", "1110100", "1110101", "1110110", "1110111",
    "1111000", "1111001", "111111",
)


def _intra_suffix_context(offset):
    def context(bin_idx, prior):
        if bin_idx == 0:
            return offset
        if bin_idx == 1:
            return 276
        if bin_idx in (2, 3):
            return offset + bin_idx - 1
        if bin_idx == 4 and prior[3] != "0":
            return offset + 2
        return offset + 3

    return context


def _inter_mb_type_symbols(slice_type, increment):
    if slice_type % 5 == 0:
        def prefix_context(bin_idx, prior):
            return 17 if bin_idx == 2 and prior[1] == "1" else 14 + bin_idx

        plain = [(value, bins) for value, bins in enumerate(("000", "011", "010", "001"))]
        intra_prefix, intra_base, suffix_offset = "1", 5, 17
    else:
        def prefix_context(bin_idx, prior):
            if bin_idx == 0:
                return 27 + increment
            if bin_idx == 1:
                return 30
            return 31 if bin_idx == 2 and prior[1] != "0" else 32

        plain = list(enumerate(_B_MB_TYPE_BINS))
        intra_prefix, intra_base, suffix_offset = "111101", 23, 32
    symbols = [(value, bins, prefix_context, None) for value, bins in plain]
    symbols += [
        (intra_base + value, intra_prefix, prefix_context, suffix)
        for value, suffix in enumerate(_I_MB_TYPE_BINS)
    ]
    suffix_context = _intra_suffix_context(suffix_offset)

    def encode(encoder, contexts, symbol):
        _, bins, context, suffix = symbol
        encoder.encode_bin_string(contexts, bins, context)
        if suffix is not None:
            encoder.encode_bin_string(contexts, suffix, suffix_context)

    return symbols, encode


@pytest.mark.parametrize(
    ("slice_type", "left", "top", "left_direct", "top_direct", "increment"),
    (
        (0, True, True, False, False, 0),
        (5, False, False, False, False, 0),
        (1, False, False, False, False, 0),
        (6, True, False, False, False, 1),
        (1, True, True, False, False, 2),
        (1, True, True, True, False, 1),
    ),
)
def test_inter_mb_type_round_trip(slice_type, left, top, left_direct, top_direct, increment):
    symbols, encode = _inter_mb_type_symbols(slice_type, increment)
    # Every value is coded twice to exercise adaptation; I_PCM terminates the stream, so it is last.
    symbols = symbols[:-1] + symbols[:-1] + symbols[-1:]
    slice_contexts = initialize_slice_contexts(slice_type, 1, 30)
    encoder_contexts = [_model(m.state_index, m.mps) for m in slice_contexts]
    encoder = _TestEncoder()
    for symbol in symbols:
        encode(encoder, encoder_contexts, symbol)
    decoder = CABACArithmeticDecoder(encoder.data())
    decoder_contexts = [_model(m.state_index, m.mps) for m in slice_contexts]
    for symbol in symbols:
        assert decoder.decode_inter_mb_type(
            slice_type, decoder_contexts, left, top, left_direct, top_direct
        ) == symbol[0]
    assert _states(decoder_contexts) == _states(encoder_contexts)
    assert decoder._terminated


def test_sub_mb_type_round_trip():
    def b_context(bin_idx, prior):
        if bin_idx < 2:
            return 36 + bin_idx
        return 38 if bin_idx == 2 and prior[1] != "0" else 39

    for slice_type, bin_strings, context in (
        (0, ("1", "00", "011", "010"), lambda bin_idx, _: 21 + bin_idx),
        (1, ("0", "100", "101", "11000", "11001", "11010", "11011", "111000", "111001",
             "111010", "111011", "11110", "11111"), b_context),
    ):
        slice_contexts = initialize_slice_contexts(slice_type, 2, 33)
        encoder_contexts = [_model(m.state_index, m.mps) for m in slice_contexts]
        encoder = _TestEncoder()
        for bins in bin_strings * 2:
            encoder.encode_bin_string(encoder_contexts, bins, context)
        decoder = CABACArithmeticDecoder(encoder.data())
        decoder_contexts = [_model(m.state_index, m.mps) for m in slice_contexts]
        for want in list(range(len(bin_strings))) * 2:
            assert decoder.decode_sub_mb_type(slice_type, decoder_contexts) == want
        assert _states(decoder_contexts) == _states(encoder_contexts)


def test_inter_mb_type_and_sub_mb_type_reject_invalid_input_transactionally():
    contexts = initialize_slice_contexts(1, 0, 26)
    decoder = CABACArithmeticDecoder(b"\x5a\x3c")
    for slice_type in (2, 4, 10):
        with pytest.raises(CABACError, match="unsupported"):
            decoder.decode_inter_mb_type(slice_type, contexts, False, False)
        with pytest.raises(CABACError, match="unsupported"):
            decoder.decode_sub_mb_type(slice_type, contexts)
    with pytest.raises(CABACError, match="460 slice contexts"):
        decoder.decode_inter_mb_type(1, contexts[:459], False, False)
    while True:
        state = (decoder.code_range, decoder.code_offset, decoder._bits._bit_offset, _states(contexts))
        try:
            decoder.decode_inter_mb_type(1, contexts, False, False)
        except CABACError:
            break
    assert (decoder.code_range, decoder.code_offset, decoder._bits._bit_offset, _states(contexts)) == state


def test_i_intra_mb_type_context_selection_and_failure_are_transactional():
    decoder = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
    decoder._bits = BitReader(b"")
    decoder._code_range = 510
    decoder._code_offset = 0
    decoder._terminated = False
    contexts = [CABACContextModel(0, 64, 26) for _ in range(8)]
    contexts[2] = CABACContextModel(0, 63, 26)
    assert decoder.decode_i_intra_mb_type(2, contexts, True, True, True, True) == 0
    assert contexts[0].state_index == 0
    assert contexts[2].state_index == 1

    truncated = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
    truncated._bits = BitReader(b"")
    truncated._code_range = 510
    truncated._code_offset = 0
    truncated._terminated = False
    unchanged = [CABACContextModel(0, 63, 26) for _ in range(8)]
    unchanged[0] = CABACContextModel(0, 64, 26)
    with pytest.raises(CABACError, match="truncated bitstream"):
        truncated.decode_i_intra_mb_type(2, unchanged, False, False, False, False)
    assert (truncated.code_range, truncated.code_offset, truncated._bits._bit_offset) == (510, 0, 0)
    assert (unchanged[0].state_index, unchanged[0].mps) == (0, True)

    with pytest.raises(CABACError, match="unsupported for this slice type"):
        truncated.decode_i_intra_mb_type(0, unchanged, False, False, False, False)


def test_intra_chroma_pred_mode_decodes_all_modes():
    for mode, offset, first_mps, context_mps in (
        (0, 0, False, False),
        (1, 0, True, False),
        (2, 100, True, True),
        (3, 0, True, True),
    ):
        decoder = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
        decoder._bits = BitReader(b"\x00")
        decoder._code_range = 510
        decoder._code_offset = offset
        decoder._terminated = False
        contexts = [CABACContextModel(0, 63, 26) for _ in range(4)]
        contexts[0] = CABACContextModel(0, 64, 26) if first_mps else contexts[0]
        contexts[3] = CABACContextModel(0, 64, 26) if context_mps else contexts[3]
        assert decoder.decode_intra_chroma_pred_mode(contexts, False, False) == mode


def test_intra_chroma_pred_mode_derives_context_and_rolls_back_on_truncation():
    for context_index in range(3):
        decoder = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
        decoder._bits = BitReader(b"")
        decoder._code_range = 510
        decoder._code_offset = 0
        decoder._terminated = False
        contexts = [CABACContextModel(0, 64, 26) for _ in range(4)]
        contexts[context_index] = CABACContextModel(0, 63, 26)
        assert decoder.decode_intra_chroma_pred_mode(
            contexts, context_index > 0, context_index == 2
        ) == 0
        assert contexts[context_index].state_index == 1

    decoder = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
    decoder._bits = BitReader(b"")
    decoder._code_range = 510
    decoder._code_offset = 0
    decoder._terminated = False
    contexts = [CABACContextModel(0, 63, 26) for _ in range(4)]
    contexts[0] = CABACContextModel(0, 64, 26)
    with pytest.raises(CABACError, match="truncated bitstream"):
        decoder.decode_intra_chroma_pred_mode(contexts, False, False)
    assert (decoder.code_range, decoder.code_offset, decoder._bits._bit_offset) == (510, 0, 0)
    assert (contexts[0].state_index, contexts[0].mps) == (0, True)


def test_intra4x4_pred_mode_decodes_previous_flag_and_remainder():
    previous = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
    previous._bits = BitReader(b"")
    previous._code_range = 510
    previous._code_offset = 0
    previous._terminated = False
    contexts = [CABACContextModel(0, 64, 26), CABACContextModel(0, 63, 26)]
    assert previous.decode_intra4x4_pred_mode(6, contexts) == 6

    remainder = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
    remainder._bits = BitReader(b"\x00")
    remainder._code_range = 510
    remainder._code_offset = 0
    remainder._terminated = False
    contexts = [CABACContextModel(0, 63, 26), CABACContextModel.__new__(CABACContextModel)]
    contexts[1]._state_index = 62
    contexts[1]._value_mps = True
    assert remainder.decode_intra4x4_pred_mode(7, contexts) == 8


def test_derive_intra4x4_predicted_mode_uses_syntax_scan_neighbors():
    decoded = [7, 5, 4, 6, 3] + [0] * 11
    top = [3, 2, 1, 0]
    left = [5, 6, 7, 8]
    assert derive_intra4x4_predicted_mode(decoded, 0, top, left, True, True) == 3
    assert derive_intra4x4_predicted_mode(decoded, 1, top, left, True, True) == 2
    assert derive_intra4x4_predicted_mode(decoded, 2, top, left, True, True) == 6
    assert derive_intra4x4_predicted_mode(decoded, 4, top, left, True, True) == 1
    assert derive_intra4x4_predicted_mode(decoded, 0, top, left, False, True) == 2
    with pytest.raises(CABACError, match="neighbor state"):
        derive_intra4x4_predicted_mode(decoded, 16, top, left, True, True)


def test_decode_intra4x4_pred_modes_and_rollback():
    decoder = CABACArithmeticDecoder(bytes(128))
    contexts = [CABACContextModel(0, 63, 26), CABACContextModel(0, 63, 26)]
    modes = decoder.decode_intra4x4_pred_modes(contexts, [0] * 4, [0] * 4, False, False)
    assert len(modes) == 16
    assert all(0 <= mode <= 8 for mode in modes)

    truncated = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
    truncated._bits = BitReader(b"")
    truncated._code_range = 510
    truncated._code_offset = 0
    truncated._terminated = False
    contexts = [CABACContextModel(0, 63, 26), CABACContextModel(0, 63, 26)]
    with pytest.raises(CABACError, match="truncated bitstream"):
        truncated.decode_intra4x4_pred_modes(contexts, [0] * 4, [0] * 4, False, False)
    assert (truncated.code_range, truncated.code_offset, truncated._bits._bit_offset) == (510, 0, 0)
    assert [(model.state_index, model.mps) for model in contexts] == [(0, False), (0, False)]


def test_intra4x4_pred_mode_rejects_invalid_mode_and_rolls_back():
    decoder = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
    decoder._bits = BitReader(b"")
    decoder._code_range = 510
    decoder._code_offset = 0
    decoder._terminated = False
    contexts = [CABACContextModel(0, 63, 26), CABACContextModel(0, 63, 26)]
    with pytest.raises(CABACError, match=r"outside \[0,8\]"):
        decoder.decode_intra4x4_pred_mode(9, contexts)
    with pytest.raises(CABACError, match="truncated bitstream"):
        decoder.decode_intra4x4_pred_mode(0, contexts)
    assert (decoder.code_range, decoder.code_offset, decoder._bits._bit_offset) == (510, 0, 0)
    assert [(model.state_index, model.mps) for model in contexts] == [(0, False), (0, False)]


def test_transform_size_8x8_flag_derives_context_from_neighbors():
    for context_index in range(3):
        decoder = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
        decoder._bits = BitReader(b"")
        decoder._code_range = 510
        decoder._code_offset = 0
        decoder._terminated = False
        contexts = [CABACContextModel(0, 64, 26) for _ in range(3)]
        contexts[context_index] = CABACContextModel(0, 63, 26)
        assert not decoder.decode_transform_size_8x8_flag(
            contexts, context_index > 0, context_index == 2
        )
        assert contexts[context_index].state_index == 1


def test_luma_coded_block_pattern_values_and_context_reuse():
    zero = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
    zero._bits = BitReader(b"\x00")
    zero._code_range = 510
    zero._code_offset = 0
    zero._terminated = False
    zero_contexts = [CABACContextModel(0, 63, 26) for _ in range(4)]
    assert zero.decode_luma_coded_block_pattern(0, 0, zero_contexts) == 0
    assert zero_contexts[3].state_index == 4

    neighbor = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
    neighbor._bits = BitReader(b"\x00")
    neighbor._code_range = 510
    neighbor._code_offset = 0
    neighbor._terminated = False
    neighbor_contexts = [
        CABACContextModel(0, 63, 26),
        CABACContextModel(0, 64, 26),
        CABACContextModel(0, 64, 26),
        CABACContextModel(0, 64, 26),
    ]
    assert neighbor.decode_luma_coded_block_pattern(2, 4, neighbor_contexts) == 6
    assert (neighbor_contexts[0].state_index, neighbor_contexts[3].state_index) == (2, 2)

    all_coded = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
    all_coded._bits = BitReader(b"")
    all_coded._code_range = 510
    all_coded._code_offset = 0
    all_coded._terminated = False
    all_coded_contexts = [CABACContextModel(0, 127, 26) for _ in range(4)]
    assert all_coded.decode_luma_coded_block_pattern(15, 15, all_coded_contexts) == 15


def test_luma_coded_block_pattern_validation_and_rollback():
    decoder = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
    decoder._bits = BitReader(b"")
    decoder._code_range = 510
    decoder._code_offset = 0
    decoder._terminated = False
    contexts = [CABACContextModel(0, 63, 26) for _ in range(4)]
    with pytest.raises(CABACError, match=r"outside \[0,15\]"):
        decoder.decode_luma_coded_block_pattern(16, 0, contexts)
    with pytest.raises(CABACError, match="truncated bitstream"):
        decoder.decode_luma_coded_block_pattern(0, 0, contexts)
    assert (decoder.code_range, decoder.code_offset, decoder._bits._bit_offset) == (510, 0, 0)
    assert [model.state_index for model in contexts] == [0, 0, 0, 0]


def test_chroma_coded_block_pattern_values_and_context_derivation():
    for expected, first_mps, second_mps in ((0, False, False), (1, True, False), (2, True, True)):
        decoder = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
        decoder._bits = BitReader(b"" if expected == 0 else b"\x00")
        decoder._code_range = 510
        decoder._code_offset = 0
        decoder._terminated = False
        contexts = [CABACContextModel(0, 63, 26) for _ in range(8)]
        contexts[0] = CABACContextModel(0, 64, 26) if first_mps else contexts[0]
        contexts[4] = CABACContextModel(0, 64, 26) if second_mps else contexts[4]
        assert decoder.decode_chroma_coded_block_pattern(0, 0, contexts) == expected

    first_context = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
    first_context._bits = BitReader(b"")
    first_context._code_range = 510
    first_context._code_offset = 0
    first_context._terminated = False
    contexts = [CABACContextModel(0, 64, 26) for _ in range(8)]
    contexts[3] = CABACContextModel(0, 63, 26)
    assert first_context.decode_chroma_coded_block_pattern(1, 1, contexts) == 0
    assert contexts[3].state_index == 1

    second_context = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
    second_context._bits = BitReader(b"\x00")
    second_context._code_range = 510
    second_context._code_offset = 0
    second_context._terminated = False
    contexts = [CABACContextModel(0, 64, 26) for _ in range(8)]
    contexts[7] = CABACContextModel(0, 63, 26)
    assert second_context.decode_chroma_coded_block_pattern(2, 2, contexts) == 1
    assert contexts[7].state_index == 1


def test_chroma_coded_block_pattern_validation_and_rollback():
    decoder = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
    decoder._bits = BitReader(b"")
    decoder._code_range = 510
    decoder._code_offset = 0
    decoder._terminated = False
    contexts = [CABACContextModel(0, 63, 26) for _ in range(8)]
    with pytest.raises(CABACError, match=r"outside \[0,2\]"):
        decoder.decode_chroma_coded_block_pattern(3, 0, contexts)
    contexts[0] = CABACContextModel(0, 64, 26)
    with pytest.raises(CABACError, match="truncated bitstream"):
        decoder.decode_chroma_coded_block_pattern(0, 0, contexts)
    assert (decoder.code_range, decoder.code_offset, decoder._bits._bit_offset) == (510, 0, 0)
    assert (contexts[0].state_index, contexts[0].mps) == (0, True)


def test_luma4x4_coded_block_flag_derives_context_from_nonzero_counts():
    for left, top, context_index in ((0, 0, 0), (1, 0, 1), (0, 16, 2), (3, 7, 3)):
        decoder = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
        decoder._bits = BitReader(b"")
        decoder._code_range = 510
        decoder._code_offset = 0
        decoder._terminated = False
        contexts = [CABACContextModel(0, 64, 26) for _ in range(4)]
        contexts[context_index] = CABACContextModel(0, 63, 26)
        assert not decoder.decode_luma4x4_coded_block_flag(left, top, contexts)
        assert contexts[context_index].state_index == 1


def test_luma4x4_coded_block_flag_validates_counts_and_rolls_back():
    decoder = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
    decoder._bits = BitReader(b"")
    decoder._code_range = 510
    decoder._code_offset = 270
    decoder._terminated = False
    contexts = [CABACContextModel(0, 63, 26) for _ in range(4)]
    with pytest.raises(CABACError, match=r"outside \[0,16\]"):
        decoder.decode_luma4x4_coded_block_flag(17, 0, contexts)
    with pytest.raises(CABACError, match="truncated bitstream"):
        decoder.decode_luma4x4_coded_block_flag(0, 0, contexts)
    assert (decoder.code_range, decoder.code_offset, decoder._bits._bit_offset) == (510, 270, 0)
    assert contexts[0].state_index == 0


def test_luma4x4_significance_map_implies_final_coefficient():
    decoder = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
    decoder._bits = BitReader(b"")
    decoder._code_range = 510
    decoder._code_offset = 0
    decoder._terminated = False
    significant = [CABACContextModel(0, 0, 26) for _ in range(15)]
    last = [CABACContextModel(0, 0, 26) for _ in range(15)]
    for model in significant + last:
        model._state_index = 62
        model._value_mps = False
    flags = decoder.decode_luma4x4_significance_map(significant, last)
    assert flags == [False] * 15 + [True]


def test_luma4x4_significance_map_stops_on_last_flag():
    decoder = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
    decoder._bits = BitReader(b"")
    decoder._code_range = 510
    decoder._code_offset = 0
    decoder._terminated = False
    significant = [CABACContextModel(0, 0, 26) for _ in range(15)]
    last = [CABACContextModel(0, 0, 26) for _ in range(15)]
    for model in significant + last:
        model._state_index = 62
        model._value_mps = False
    significant[0]._value_mps = True
    significant[1]._value_mps = True
    last[1]._value_mps = True
    flags = decoder.decode_luma4x4_significance_map(significant, last)
    assert flags == [True, True] + [False] * 14


def test_luma4x4_significance_map_truncation_is_transactional():
    decoder = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
    decoder._bits = BitReader(b"")
    decoder._code_range = 510
    decoder._code_offset = 382
    decoder._terminated = False
    significant = [CABACContextModel(0, 0, 26) for _ in range(15)]
    last = [CABACContextModel(0, 0, 26) for _ in range(15)]
    significant[0] = CABACContextModel(0, 63, 26)
    with pytest.raises(CABACError, match="truncated bitstream"):
        decoder.decode_luma4x4_significance_map(significant, last)
    assert (decoder.code_range, decoder.code_offset, decoder._bits._bit_offset) == (510, 382, 0)
    assert (significant[0].state_index, significant[0].mps) == (0, False)


def test_motion_vector_difference_context_bands_and_values():
    for neighbor_magnitude, expected_context in (
        (0, 0),
        (2, 0),
        (3, 1),
        (32, 1),
        (33, 2),
        (1 << 32, 2),
    ):
        decoder = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
        decoder._bits = BitReader(b"")
        decoder._code_range = 510
        decoder._code_offset = 0
        decoder._terminated = False
        contexts = [CABACContextModel(0, 63, 26) for _ in range(7)]

        assert decoder.decode_motion_vector_difference(neighbor_magnitude, contexts) == 0
        assert [context.state_index for context in contexts] == [
            int(index == expected_context) for index in range(7)
        ]

    vectors = (
        (
            b"\x00",
            0,
            [CABACContextModel(0, 127, 26), *[CABACContextModel(0, 1, 26) for _ in range(6)]],
            1,
        ),
        (
            b"\x00",
            253,
            [CABACContextModel(0, 127, 26), CABACContextModel(0, 1, 26), CABACContextModel(0, 1, 26), CABACContextModel(0, 1, 26), *[CABACContextModel(0, 1, 26) for _ in range(3)]],
            -1,
        ),
        (
            b"\x00",
            0,
            [CABACContextModel(0, 127, 26) for _ in range(7)],
            9,
        ),
        (
            b"\x08",
            31,
            [CABACContextModel(0, 127, 26) for _ in range(7)],
            10,
        ),
    )
    for data, code_offset, contexts, expected in vectors:
        decoder = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
        decoder._bits = BitReader(data)
        decoder._code_range = 510
        decoder._code_offset = code_offset
        decoder._terminated = False
        assert decoder.decode_motion_vector_difference(0, contexts) == expected


def test_motion_vector_difference_truncation_is_transactional():
    decoder = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
    decoder._bits = BitReader(b"")
    decoder._code_range = 510
    decoder._code_offset = 509
    decoder._terminated = False
    contexts = [CABACContextModel(0, 127, 26)] + [
        CABACContextModel(0, 1, 26) for _ in range(6)
    ]
    with pytest.raises(CABACError, match="truncated bitstream"):
        decoder.decode_motion_vector_difference(0, contexts)
    assert (decoder.code_range, decoder.code_offset, decoder._bits._bit_offset) == (
        510,
        509,
        0,
    )
    assert (contexts[0].state_index, contexts[0].mps) == (62, True)


def test_motion_vector_difference_overlong_escape_is_transactional():
    decoder = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
    decoder._bits = BitReader(b"\xff" * 6)
    decoder._code_range = 510
    decoder._code_offset = 491
    decoder._terminated = False
    contexts = [CABACContextModel.__new__(CABACContextModel) for _ in range(7)]
    for context in contexts:
        context._state_index = 63
        context._value_mps = True
    with pytest.raises(CABACError, match="MVD exceeds the signed output range"):
        decoder.decode_motion_vector_difference(0, contexts)
    assert (decoder.code_range, decoder.code_offset, decoder._bits._bit_offset) == (
        510,
        491,
        0,
    )
    assert [(context.state_index, context.mps) for context in contexts] == [
        (63, True)
    ] * 7


def test_coeff_abs_level_minus1_decodes_zero_unit_and_escape():
    zero = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
    zero._bits = BitReader(b"")
    zero._code_range = 510
    zero._code_offset = 0
    zero._terminated = False
    assert zero.decode_coeff_abs_level_minus1(
        CABACContextModel(0, 0, 26), CABACContextModel(0, 0, 26)
    ) == 0

    unit = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
    unit._bits = BitReader(b"")
    unit._code_range = 510
    unit._code_offset = 0
    unit._terminated = False
    assert unit.decode_coeff_abs_level_minus1(
        CABACContextModel(0, 64, 26), CABACContextModel(0, 0, 26)
    ) == 1

    escaped = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
    escaped._bits = BitReader(b"\x40")
    escaped._code_range = 510
    escaped._code_offset = 0
    escaped._terminated = False
    first_context = CABACContextModel.__new__(CABACContextModel)
    first_context._state_index = 62
    first_context._value_mps = True
    greater_context = CABACContextModel.__new__(CABACContextModel)
    greater_context._state_index = 62
    greater_context._value_mps = True
    assert escaped.decode_coeff_abs_level_minus1(
        first_context, greater_context
    ) == 14


def test_coeff_abs_level_minus1_truncation_is_transactional():
    decoder = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
    decoder._bits = BitReader(b"")
    decoder._code_range = 510
    decoder._code_offset = 390
    decoder._terminated = False
    first = CABACContextModel(0, 64, 26)
    greater = CABACContextModel(0, 64, 26)
    with pytest.raises(CABACError, match="truncated bitstream"):
        decoder.decode_coeff_abs_level_minus1(first, greater)
    assert (decoder.code_range, decoder.code_offset, decoder._bits._bit_offset) == (510, 390, 0)
    assert (first.state_index, first.mps) == (0, True)
    assert (greater.state_index, greater.mps) == (0, True)


def test_coeff_abs_level_minus1_rejects_overlong_escape_prefix_transactionally():
    decoder = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
    decoder._bits = BitReader(b"\xff\xff\xff\xff")
    decoder._code_range = 510
    decoder._code_offset = 481
    decoder._terminated = False
    first = CABACContextModel.__new__(CABACContextModel)
    first._state_index = 63
    first._value_mps = True
    greater = CABACContextModel.__new__(CABACContextModel)
    greater._state_index = 63
    greater._value_mps = True

    with pytest.raises(CABACError, match="exceeds supported bypass prefix"):
        decoder.decode_coeff_abs_level_minus1(first, greater)

    assert (decoder.code_range, decoder.code_offset, decoder._bits._bit_offset) == (
        510,
        481,
        0,
    )
    assert (first.state_index, first.mps) == (63, True)
    assert (greater.state_index, greater.mps) == (63, True)


def test_coeff_abs_level_minus1_matches_standard_escape_vectors():
    for code_offset, data, expected, expected_offset in (
        (300, b"\x00", 15, 472),
        (302, b"\x20", 16, 7),
    ):
        decoder = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
        decoder._bits = BitReader(data)
        decoder._code_range = 510
        decoder._code_offset = code_offset
        decoder._terminated = False
        first = CABACContextModel.__new__(CABACContextModel)
        first._state_index = 63
        first._value_mps = True
        greater = CABACContextModel.__new__(CABACContextModel)
        greater._state_index = 63
        greater._value_mps = True

        assert decoder.decode_coeff_abs_level_minus1(first, greater) == expected
        assert (decoder.code_range, decoder.code_offset) == (482, expected_offset)
        assert decoder._bits._bit_offset == 3


def test_coeff_sign_applies_bypass_sign_and_rolls_back_on_truncation():
    positive = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
    positive._bits = BitReader(b"\x00")
    positive._code_range = 510
    positive._code_offset = 0
    positive._terminated = False
    assert positive.decode_coeff_sign(4) == 5

    negative = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
    negative._bits = BitReader(b"\x00")
    negative._code_range = 510
    negative._code_offset = 255
    negative._terminated = False
    assert negative.decode_coeff_sign(4) == -5

    max_positive = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
    max_positive._bits = BitReader(b"\x00")
    max_positive._code_range = 510
    max_positive._code_offset = 0
    max_positive._terminated = False
    assert max_positive.decode_coeff_sign(0x7FFFFFFE) == 0x7FFFFFFF

    max_negative = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
    max_negative._bits = BitReader(b"\x00")
    max_negative._code_range = 510
    max_negative._code_offset = 255
    max_negative._terminated = False
    assert max_negative.decode_coeff_sign(0x7FFFFFFE) == -0x7FFFFFFF

    overflow = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
    overflow._bits = BitReader(b"")
    overflow._code_range = 510
    overflow._code_offset = 0
    overflow._terminated = False
    with pytest.raises(CABACError, match="signed output range"):
        overflow.decode_coeff_sign(0x7FFFFFFF)
    assert (overflow.code_range, overflow.code_offset, overflow._bits._bit_offset) == (510, 0, 0)

    truncated = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
    truncated._bits = BitReader(b"")
    truncated._code_range = 510
    truncated._code_offset = 0
    truncated._terminated = False
    with pytest.raises(CABACError, match="truncated bitstream"):
        truncated.decode_coeff_sign(0)
    assert (truncated.code_range, truncated.code_offset, truncated._bits._bit_offset) == (510, 0, 0)


def test_luma4x4_residual_levels_return_signed_scan_order_levels():
    decoder = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
    decoder._bits = BitReader(b"\x00")
    decoder._code_range = 510
    decoder._code_offset = 0
    decoder._terminated = False
    significance = [False] * 16
    significance[2] = True
    contexts = [CABACContextModel(0, 63, 26) for _ in range(10)]
    levels = decoder.decode_luma4x4_residual_levels(significance, contexts)
    assert levels == [0, 0, 1] + [0] * 13

    negative = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
    negative._bits = BitReader(b"\x00")
    negative._code_range = 510
    negative._code_offset = 255
    negative._terminated = False
    significance = [False] * 16
    significance[15] = True
    contexts = [CABACContextModel(0, 63, 26) for _ in range(10)]
    assert negative.decode_luma4x4_residual_levels(significance, contexts)[15] == -1


def test_place_luma4x4_scan_levels_maps_all_positions_to_raster_order():
    assert place_luma4x4_scan_levels(list(range(16))) == [
        0, 1, 5, 6, 2, 4, 7, 12, 3, 8, 11, 13, 9, 10, 14, 15
    ]
    with pytest.raises(CABACError, match="16 positions"):
        place_luma4x4_scan_levels([1, 2, 3])


def test_place_chroma4x4_scan_levels_inserts_dc_and_maps_ac_positions():
    assert place_chroma4x4_scan_levels(100, list(range(1, 16))) == [
        100, 1, 5, 6, 2, 4, 7, 12, 3, 8, 11, 13, 9, 10, 14, 15
    ]
    with pytest.raises(CABACError, match="one integer DC and 15 integer AC"):
        place_chroma4x4_scan_levels(100, [1, 2, 3])


def test_luma4x4_residual_block_with_coded_flag():
    def make_contexts(length, state_index=0, mps=False):
        contexts = [CABACContextModel.__new__(CABACContextModel) for _ in range(length)]
        for context in contexts:
            context._state_index = state_index
            context._value_mps = mps
        return contexts

    def make_block_contexts():
        coded = make_contexts(4)
        significant = make_contexts(15, 63)
        last = make_contexts(15, 63)
        coefficients = make_contexts(10, 63)
        return coded, significant, last, coefficients

    decoder = CABACArithmeticDecoder(bytes(128))
    coded, significant, last, coefficients = make_block_contexts()
    levels, has_residual = decoder.decode_luma4x4_residual_block_with_flag(
        0, 0, coded, significant, last, coefficients
    )
    assert levels == [0] * 16
    assert not has_residual

    decoder = CABACArithmeticDecoder(bytes(128))
    coded, significant, last, coefficients = make_block_contexts()
    coded[0]._value_mps = True
    _, has_residual = decoder.decode_luma4x4_residual_block_with_flag(
        0, 0, coded, significant, last, coefficients
    )
    assert has_residual

    truncated = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
    truncated._bits = BitReader(b"")
    truncated._code_range = 510
    truncated._code_offset = 0
    truncated._terminated = False
    coded, significant, last, coefficients = make_block_contexts()
    coded[0]._value_mps = True
    with pytest.raises(CABACError, match="truncated bitstream"):
        truncated.decode_luma4x4_residual_block_with_flag(
            0, 0, coded, significant, last, coefficients
        )
    assert (truncated.code_range, truncated.code_offset, truncated._bits._bit_offset) == (510, 0, 0)
    assert (coded[0].state_index, coded[0].mps) == (0, True)
    assert significant[0].state_index == 63


def test_chroma4x4_ac_residual_block_places_implied_final_coefficient_and_rolls_back():
    def make_contexts(length, mps=False):
        contexts = [CABACContextModel.__new__(CABACContextModel) for _ in range(length)]
        for model in contexts:
            model._state_index = 63
            model._value_mps = mps
        return contexts

    def make_decoder(data):
        decoder = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
        decoder._bits = BitReader(data)
        decoder._code_range = 510
        decoder._code_offset = 0
        decoder._terminated = False
        return decoder

    coded = make_contexts(4)
    significant, last, coefficients = make_contexts(15), make_contexts(15), make_contexts(10)
    levels, has_residual = make_decoder(b"").decode_chroma4x4_ac_residual_block(
        False, False, coded, significant, last, coefficients
    )
    assert levels == [0] * 16
    assert not has_residual

    coded = make_contexts(4)
    coded[0]._value_mps = True
    significant, last, coefficients = make_contexts(15), make_contexts(15), make_contexts(10)
    levels, has_residual = make_decoder(b"\x00").decode_chroma4x4_ac_residual_block(
        False, False, coded, significant, last, coefficients
    )
    assert levels == [0] * 15 + [1]
    assert has_residual

    decoder = make_decoder(b"")
    coded = make_contexts(4)
    coded[0]._value_mps = True
    significant, last, coefficients = make_contexts(15), make_contexts(15), make_contexts(10)
    with pytest.raises(CABACError, match="truncated bitstream"):
        decoder.decode_chroma4x4_ac_residual_block(
            False, False, coded, significant, last, coefficients
        )
    assert (decoder.code_range, decoder.code_offset, decoder._bits._bit_offset) == (510, 0, 0)
    assert (coded[0].state_index, coded[0].mps) == (63, True)


def test_decode_and_reconstruct_luma4x4_residual_validates_before_consuming():
    decoder = CABACArithmeticDecoder(bytes(128))
    coded = [CABACContextModel(0, 63, 26) for _ in range(4)]
    significant = [CABACContextModel(0, 127, 26) for _ in range(15)]
    last = [CABACContextModel(0, 127, 26) for _ in range(15)]
    coefficients = [CABACContextModel(0, 127, 26) for _ in range(10)]
    residual, has_residual = decoder.decode_and_reconstruct_luma4x4_residual(
        0, 0, coded, significant, last, coefficients, [16] * 16, 0
    )
    assert residual == [0] * 16
    assert not has_residual

    bit_offset = decoder._bits._bit_offset
    with pytest.raises(ValueError, match="QPY"):
        decoder.decode_and_reconstruct_luma4x4_residual(
            0, 0, coded, significant, last, coefficients, [16] * 16, 52
        )
    assert decoder._bits._bit_offset == bit_offset


def test_luma4x4_residual_block_decodes_places_and_rolls_back():
    make_significance_contexts = lambda: [
        CABACContextModel.__new__(CABACContextModel) for _ in range(15)
    ]
    make_last_contexts = lambda: [
        CABACContextModel.__new__(CABACContextModel) for _ in range(15)
    ]
    make_coefficient_contexts = lambda: [
        CABACContextModel.__new__(CABACContextModel) for _ in range(10)
    ]

    def initialize_contexts(contexts, true_index=None, state_index=63):
        for context in contexts:
            context._state_index = state_index
            context._value_mps = False
        if true_index is not None:
            contexts[true_index]._value_mps = True

    significance = make_significance_contexts()
    last = make_last_contexts()
    coefficients = make_coefficient_contexts()
    initialize_contexts(significance, 2)
    initialize_contexts(last, 2)
    initialize_contexts(coefficients)
    decoder = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
    decoder._bits = BitReader(b"\x00")
    decoder._code_range = 510
    decoder._code_offset = 0
    decoder._terminated = False
    levels = decoder.decode_luma4x4_residual_block(significance, last, coefficients)
    assert levels == [0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0]

    significance = make_significance_contexts()
    last = make_last_contexts()
    coefficients = make_coefficient_contexts()
    initialize_contexts(significance, 2, state_index=61)
    initialize_contexts(last, 2, state_index=61)
    initialize_contexts(coefficients, state_index=61)
    truncated = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
    truncated._bits = BitReader(b"")
    truncated._code_range = 510
    truncated._code_offset = 0
    truncated._terminated = False
    with pytest.raises(CABACError, match="truncated bitstream"):
        truncated.decode_luma4x4_residual_block(significance, last, coefficients)
    assert (truncated.code_range, truncated.code_offset, truncated._bits._bit_offset) == (510, 0, 0)
    assert (significance[0].state_index, significance[2].state_index) == (61, 61)
    assert (last[2].state_index, coefficients[1].state_index) == (61, 61)


def test_luma4x4_residual_levels_validate_and_roll_back_block():
    decoder = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
    decoder._bits = BitReader(b"")
    decoder._code_range = 510
    decoder._code_offset = 390
    decoder._terminated = False
    significance = [False] * 16
    significance[0] = True
    contexts = [CABACContextModel(0, 64, 26) for _ in range(10)]
    with pytest.raises(CABACError, match="truncated bitstream"):
        decoder.decode_luma4x4_residual_levels(significance, contexts)
    assert (decoder.code_range, decoder.code_offset, decoder._bits._bit_offset) == (510, 390, 0)
    assert all((model.state_index, model.mps) == (0, True) for model in contexts)

    with pytest.raises(CABACError, match="no significant coefficients"):
        decoder.decode_luma4x4_residual_levels([False] * 16, contexts)