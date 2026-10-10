import pytest

from pygorvid import (
    BitReader,
    CABACArithmeticDecoder,
    CABACContextModel,
    CABACError,
    CABACTerminatedError,
    place_luma4x4_scan_levels,
)


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