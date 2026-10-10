"""CABAC arithmetic-state initialization and renormalization."""

from .bitreader import BitReader, BitstreamError


class CABACError(ValueError):
    """Raised when CABAC arithmetic state or initialization bits are invalid."""


class CABACTerminatedError(CABACError):
    """Raised when decoding is attempted after a terminate bin was true."""


_TRANSITION_LPS = (
    0, 0, 1, 2, 2, 4, 4, 5, 6, 7, 8, 9, 9, 11, 11, 12,
    13, 13, 15, 15, 16, 16, 18, 18, 19, 19, 21, 21, 22, 22, 23, 24,
    24, 25, 26, 26, 27, 27, 28, 29, 29, 30, 30, 30, 31, 32, 32, 33,
    33, 33, 34, 34, 35, 35, 35, 36, 36, 37, 37, 37, 38, 38, 63, 63,
)
_MAX_QPY = 51
_MAX_MOTION_VECTOR_DIFFERENCE = 0x7FFFFFFF
_COEFF_ABS_LEVEL1_CONTEXT = (1, 2, 3, 4, 0, 0, 0, 0)
_COEFF_ABS_LEVEL_GREATER1_CONTEXT = (5, 5, 5, 5, 6, 7, 8, 9)
_COEFF_LEVEL1_TRANSITION = (1, 2, 3, 3, 4, 5, 6, 7)
_COEFF_LEVEL_GREATER1_TRANSITION = (4, 4, 4, 4, 5, 6, 7, 7)
_RANGE_LPS = (
    (128, 128, 128, 123, 116, 111, 105, 100, 95, 90, 85, 81, 77, 73, 69, 66, 62, 59, 56, 53, 51, 48, 46, 43, 41, 39, 37, 35, 33, 32, 30, 29, 27, 26, 24, 23, 22, 21, 20, 19, 18, 17, 16, 15, 14, 14, 13, 12, 12, 11, 11, 10, 10, 9, 9, 8, 8, 7, 7, 7, 6, 6, 6, 2),
    (176, 167, 158, 150, 142, 135, 128, 122, 116, 110, 104, 99, 94, 89, 85, 80, 76, 72, 69, 65, 62, 59, 56, 53, 50, 48, 45, 43, 41, 39, 37, 35, 33, 31, 30, 28, 27, 26, 24, 23, 22, 21, 20, 19, 18, 17, 16, 15, 14, 14, 13, 12, 12, 11, 11, 10, 9, 9, 9, 8, 8, 7, 7, 2),
    (208, 197, 187, 178, 169, 160, 152, 144, 137, 130, 123, 117, 111, 105, 100, 95, 90, 86, 81, 77, 73, 69, 66, 63, 59, 56, 54, 51, 48, 46, 43, 41, 39, 37, 35, 33, 32, 30, 29, 27, 26, 25, 23, 22, 21, 20, 19, 18, 17, 16, 15, 15, 14, 13, 12, 12, 11, 11, 10, 10, 9, 9, 8, 2),
    (240, 227, 216, 205, 195, 185, 175, 166, 158, 150, 142, 135, 128, 122, 116, 110, 104, 99, 94, 89, 85, 80, 76, 72, 69, 65, 62, 59, 56, 53, 50, 48, 45, 43, 41, 39, 37, 35, 33, 31, 30, 28, 27, 25, 24, 23, 22, 21, 20, 19, 18, 17, 16, 15, 14, 14, 13, 12, 12, 11, 11, 10, 9, 2),
)
_LUMA4X4_SCAN_TO_RASTER = (0, 1, 4, 8, 5, 2, 3, 6, 9, 12, 13, 10, 7, 11, 14, 15)


class CABACContextModel:
    """One H.264 CABAC context's probability state and most-probable bin."""

    def __init__(self, m: int, n: int, slice_qpy: int):
        if not -128 <= m <= 127 or not -128 <= n <= 127 or not 0 <= slice_qpy <= 51:
            raise CABACError("CABAC context initialization value is outside its valid range")
        pre_context_state = ((m * slice_qpy) >> 4) + n
        pre_context_state = min(126, max(1, pre_context_state))
        if pre_context_state <= 63:
            self._state_index = 63 - pre_context_state
            self._value_mps = False
        else:
            self._state_index = pre_context_state - 64
            self._value_mps = True

    @property
    def state_index(self) -> int:
        return self._state_index

    @property
    def mps(self) -> bool:
        return self._value_mps

    def update(self, bin_value: bool) -> None:
        if bin_value == self._value_mps:
            if self._state_index < 62:
                self._state_index += 1
            return
        if self._state_index == 0:
            self._value_mps = not self._value_mps
        self._state_index = _TRANSITION_LPS[self._state_index]


class CABACArithmeticDecoder:
    """Own one CABAC substream's arithmetic range and code offset.

    Input must start at the byte-aligned CABAC RBSP data, after removing NAL
    emulation-prevention bytes and consuming slice-header alignment bits.
    """

    INITIAL_RANGE = 510

    def __init__(self, data: bytes):
        self._bits = BitReader(data)
        try:
            self._code_offset = self._bits.read_bits(9)
        except BitstreamError as error:
            raise CABACError(f"CABAC initial offset: {error}") from error
        if self._code_offset >= self.INITIAL_RANGE:
            raise CABACError("CABAC initial offset is outside the arithmetic range")
        self._code_range = self.INITIAL_RANGE
        self._terminated = False

    @property
    def code_range(self) -> int:
        return self._code_range

    @property
    def code_offset(self) -> int:
        return self._code_offset

    def decode_bin(self, model: CABACContextModel) -> bool:
        if not isinstance(model, CABACContextModel) or not 0 <= model._state_index < 64:
            raise CABACError("CABAC context state index is outside [0,63]")
        self._validate_bin_state()
        original_range, original_offset = self._code_range, self._code_offset
        start_offset = self._bits._bit_offset
        range_lps = _RANGE_LPS[(self._code_range >> 6) & 3][model._state_index]
        range_mps = self._code_range - range_lps
        decoded = model._value_mps
        if self._code_offset >= range_mps:
            decoded = not decoded
            self._code_offset -= range_mps
            self._code_range = range_lps
        else:
            self._code_range = range_mps
        try:
            self.renormalize()
        except CABACError:
            self._code_range, self._code_offset = original_range, original_offset
            self._bits._bit_offset = start_offset
            raise
        model.update(decoded)
        return decoded

    def decode_mb_qp_delta(self, contexts: list[CABACContextModel], previous_delta: int) -> int:
        if not isinstance(contexts, list) or len(contexts) != 4 or any(
            not isinstance(model, CABACContextModel) or not 0 <= model._state_index < 64
            for model in contexts
        ):
            raise CABACError("mb_qp_delta requires context models 60 through 63")
        if len({id(model) for model in contexts}) != 4:
            raise CABACError("mb_qp_delta context models must be distinct")

        trial_bits = BitReader(self._bits._data)
        trial_bits._bit_offset = self._bits._bit_offset
        trial = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
        trial._bits = trial_bits
        trial._code_range = self._code_range
        trial._code_offset = self._code_offset
        trial._terminated = self._terminated
        trial_contexts = [CABACContextModel.__new__(CABACContextModel) for _ in range(4)]
        for target, source in zip(trial_contexts, contexts):
            target._state_index = source._state_index
            target._value_mps = source._value_mps

        context_index = int(previous_delta != 0)
        first = trial.decode_bin(trial_contexts[context_index])
        if first:
            value = 1
            context_index = 2
            while trial.decode_bin(trial_contexts[context_index]):
                value += 1
                if value > 2 * _MAX_QPY:
                    raise CABACError("CABAC mb_qp_delta exceeds the 8-bit QP range")
                context_index = 3
            delta = (value + 1) >> 1
            if value % 2 == 0:
                delta = -delta
        else:
            delta = 0

        self._bits._bit_offset = trial._bits._bit_offset
        self._code_range = trial._code_range
        self._code_offset = trial._code_offset
        self._terminated = trial._terminated
        for target, source in zip(contexts, trial_contexts):
            target._state_index = source._state_index
            target._value_mps = source._value_mps
        return delta

    def decode_mb_skip_flag(
        self,
        slice_type: int,
        contexts: list[CABACContextModel],
        left_available: bool,
        left_skipped: bool,
        top_available: bool,
        top_skipped: bool,
    ) -> bool:
        if slice_type > 9 or slice_type % 5 not in (0, 1):
            raise CABACError("CABAC mb_skip_flag is unsupported for this slice type")
        if not isinstance(contexts, list) or len(contexts) != 3:
            raise CABACError("mb_skip_flag requires three P- or B-slice context models")
        context_index = int(left_available and not left_skipped) + int(top_available and not top_skipped)
        return self.decode_bin(contexts[context_index])

    def decode_i_intra_mb_type(
        self,
        slice_type: int,
        contexts: list[CABACContextModel],
        left_available: bool,
        left_intra16_or_pcm: bool,
        top_available: bool,
        top_intra16_or_pcm: bool,
    ) -> int:
        if slice_type > 9 or slice_type % 5 != 2:
            raise CABACError("I-slice mb_type is unsupported for this slice type")
        if not isinstance(contexts, list) or len(contexts) != 8 or any(
            not isinstance(model, CABACContextModel) or not 0 <= model._state_index < 64
            for model in contexts
        ):
            raise CABACError("I-slice mb_type requires context models 3 through 10")
        if len({id(model) for model in contexts}) != 8:
            raise CABACError("I-slice mb_type context models must be distinct")

        trial_bits = BitReader(self._bits._data)
        trial_bits._bit_offset = self._bits._bit_offset
        trial = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
        trial._bits = trial_bits
        trial._code_range = self._code_range
        trial._code_offset = self._code_offset
        trial._terminated = self._terminated
        trial_contexts = [CABACContextModel.__new__(CABACContextModel) for _ in range(8)]
        for target, source in zip(trial_contexts, contexts):
            target._state_index = source._state_index
            target._value_mps = source._value_mps

        first_context = int(left_available and left_intra16_or_pcm)
        first_context += int(top_available and top_intra16_or_pcm)
        if not trial.decode_bin(trial_contexts[first_context]):
            mb_type = 0
        elif trial.decode_terminate_bin():
            mb_type = 25
        else:
            mb_type = 1 + 12 * int(trial.decode_bin(trial_contexts[3]))
            if trial.decode_bin(trial_contexts[4]):
                mb_type += 4 + 4 * int(trial.decode_bin(trial_contexts[5]))
            mb_type += 2 * int(trial.decode_bin(trial_contexts[6]))
            mb_type += int(trial.decode_bin(trial_contexts[7]))

        self._bits._bit_offset = trial._bits._bit_offset
        self._code_range = trial._code_range
        self._code_offset = trial._code_offset
        self._terminated = trial._terminated
        for target, source in zip(contexts, trial_contexts):
            target._state_index = source._state_index
            target._value_mps = source._value_mps
        return mb_type

    def decode_intra_chroma_pred_mode(
        self,
        contexts: list[CABACContextModel],
        left_has_nonzero_mode: bool,
        top_has_nonzero_mode: bool,
    ) -> int:
        if not isinstance(contexts, list) or len(contexts) != 4 or any(
            not isinstance(model, CABACContextModel) or not 0 <= model._state_index < 64
            for model in contexts
        ):
            raise CABACError("intra_chroma_pred_mode requires context models 64 through 67")
        if len({id(model) for model in contexts}) != 4:
            raise CABACError("intra_chroma_pred_mode context models must be distinct")

        trial_bits = BitReader(self._bits._data)
        trial_bits._bit_offset = self._bits._bit_offset
        trial = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
        trial._bits = trial_bits
        trial._code_range = self._code_range
        trial._code_offset = self._code_offset
        trial._terminated = self._terminated
        trial_contexts = [CABACContextModel.__new__(CABACContextModel) for _ in range(4)]
        for target, source in zip(trial_contexts, contexts):
            target._state_index = source._state_index
            target._value_mps = source._value_mps

        context_index = int(left_has_nonzero_mode) + int(top_has_nonzero_mode)
        if not trial.decode_bin(trial_contexts[context_index]):
            mode = 0
        elif not trial.decode_bin(trial_contexts[3]):
            mode = 1
        elif not trial.decode_bin(trial_contexts[3]):
            mode = 2
        else:
            mode = 3

        self._bits._bit_offset = trial._bits._bit_offset
        self._code_range = trial._code_range
        self._code_offset = trial._code_offset
        self._terminated = trial._terminated
        for target, source in zip(contexts, trial_contexts):
            target._state_index = source._state_index
            target._value_mps = source._value_mps
        return mode

    def decode_intra4x4_pred_mode(
        self, predicted_mode: int, contexts: list[CABACContextModel]
    ) -> int:
        if not 0 <= predicted_mode <= 8:
            raise CABACError("CABAC intra4x4 prediction mode is outside [0,8]")
        if not isinstance(contexts, list) or len(contexts) != 2 or any(
            not isinstance(model, CABACContextModel) or not 0 <= model._state_index < 64
            for model in contexts
        ):
            raise CABACError("intra4x4 prediction mode requires context models 68 and 69")
        if contexts[0] is contexts[1]:
            raise CABACError("intra4x4 prediction mode contexts must be distinct")

        trial_bits = BitReader(self._bits._data)
        trial_bits._bit_offset = self._bits._bit_offset
        trial = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
        trial._bits = trial_bits
        trial._code_range = self._code_range
        trial._code_offset = self._code_offset
        trial._terminated = self._terminated
        trial_contexts = [CABACContextModel.__new__(CABACContextModel) for _ in range(2)]
        for target, source in zip(trial_contexts, contexts):
            target._state_index = source._state_index
            target._value_mps = source._value_mps

        if trial.decode_bin(trial_contexts[0]):
            mode = predicted_mode
        else:
            remaining_mode = sum(
                int(trial.decode_bin(trial_contexts[1])) << bit_index
                for bit_index in range(3)
            )
            mode = remaining_mode + int(remaining_mode >= predicted_mode)

        self._bits._bit_offset = trial._bits._bit_offset
        self._code_range = trial._code_range
        self._code_offset = trial._code_offset
        self._terminated = trial._terminated
        for target, source in zip(contexts, trial_contexts):
            target._state_index = source._state_index
            target._value_mps = source._value_mps
        return mode

    def decode_transform_size_8x8_flag(
        self,
        contexts: list[CABACContextModel],
        left_has_8x8_transform: bool,
        top_has_8x8_transform: bool,
    ) -> bool:
        if not isinstance(contexts, list) or len(contexts) != 3:
            raise CABACError("transform_size_8x8_flag requires context models 399 through 401")
        context_index = int(left_has_8x8_transform) + int(top_has_8x8_transform)
        return self.decode_bin(contexts[context_index])

    def decode_luma_coded_block_pattern(
        self,
        left_cbp: int,
        top_cbp: int,
        contexts: list[CABACContextModel],
    ) -> int:
        if not 0 <= left_cbp <= 15 or not 0 <= top_cbp <= 15:
            raise CABACError("CABAC luma coded_block_pattern is outside [0,15]")
        if not isinstance(contexts, list) or len(contexts) != 4 or any(
            not isinstance(model, CABACContextModel) or not 0 <= model._state_index < 64
            for model in contexts
        ):
            raise CABACError("luma coded_block_pattern requires context models 73 through 76")
        if len({id(model) for model in contexts}) != 4:
            raise CABACError("luma coded_block_pattern context models must be distinct")

        trial_bits = BitReader(self._bits._data)
        trial_bits._bit_offset = self._bits._bit_offset
        trial = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
        trial._bits = trial_bits
        trial._code_range = self._code_range
        trial._code_offset = self._code_offset
        trial._terminated = self._terminated
        trial_contexts = [CABACContextModel.__new__(CABACContextModel) for _ in range(4)]
        for target, source in zip(trial_contexts, contexts):
            target._state_index = source._state_index
            target._value_mps = source._value_mps

        pattern = 0
        context_index = int(not left_cbp & 0x02) + 2 * int(not top_cbp & 0x04)
        pattern |= int(trial.decode_bin(trial_contexts[context_index]))
        context_index = int(not pattern & 0x01) + 2 * int(not top_cbp & 0x08)
        pattern |= int(trial.decode_bin(trial_contexts[context_index])) << 1
        context_index = int(not left_cbp & 0x08) + 2 * int(not pattern & 0x01)
        pattern |= int(trial.decode_bin(trial_contexts[context_index])) << 2
        context_index = int(not pattern & 0x04) + 2 * int(not pattern & 0x02)
        pattern |= int(trial.decode_bin(trial_contexts[context_index])) << 3

        self._bits._bit_offset = trial._bits._bit_offset
        self._code_range = trial._code_range
        self._code_offset = trial._code_offset
        self._terminated = trial._terminated
        for target, source in zip(contexts, trial_contexts):
            target._state_index = source._state_index
            target._value_mps = source._value_mps
        return pattern

    def decode_chroma_coded_block_pattern(
        self,
        left_cbp: int,
        top_cbp: int,
        contexts: list[CABACContextModel],
    ) -> int:
        if not 0 <= left_cbp <= 2 or not 0 <= top_cbp <= 2:
            raise CABACError("CABAC chroma coded_block_pattern is outside [0,2]")
        if not isinstance(contexts, list) or len(contexts) != 8 or any(
            not isinstance(model, CABACContextModel) or not 0 <= model._state_index < 64
            for model in contexts
        ):
            raise CABACError("chroma coded_block_pattern requires context models 77 through 84")
        if len({id(model) for model in contexts}) != 8:
            raise CABACError("chroma coded_block_pattern context models must be distinct")

        trial_bits = BitReader(self._bits._data)
        trial_bits._bit_offset = self._bits._bit_offset
        trial = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
        trial._bits = trial_bits
        trial._code_range = self._code_range
        trial._code_offset = self._code_offset
        trial._terminated = self._terminated
        trial_contexts = [CABACContextModel.__new__(CABACContextModel) for _ in range(8)]
        for target, source in zip(trial_contexts, contexts):
            target._state_index = source._state_index
            target._value_mps = source._value_mps

        context_index = int(left_cbp > 0) + 2 * int(top_cbp > 0)
        if not trial.decode_bin(trial_contexts[context_index]):
            pattern = 0
        else:
            context_index = 4 + int(left_cbp == 2) + 2 * int(top_cbp == 2)
            pattern = 1 + int(trial.decode_bin(trial_contexts[context_index]))

        self._bits._bit_offset = trial._bits._bit_offset
        self._code_range = trial._code_range
        self._code_offset = trial._code_offset
        self._terminated = trial._terminated
        for target, source in zip(contexts, trial_contexts):
            target._state_index = source._state_index
            target._value_mps = source._value_mps
        return pattern

    def decode_luma4x4_coded_block_flag(
        self,
        left_nonzero: int,
        top_nonzero: int,
        contexts: list[CABACContextModel],
    ) -> bool:
        if not 0 <= left_nonzero <= 16 or not 0 <= top_nonzero <= 16:
            raise CABACError("CABAC luma 4x4 nonzero count is outside [0,16]")
        if not isinstance(contexts, list) or len(contexts) != 4:
            raise CABACError("luma 4x4 coded_block_flag requires context models 93 through 96")
        context_index = int(left_nonzero > 0) + 2 * int(top_nonzero > 0)
        return self.decode_bin(contexts[context_index])

    def decode_luma4x4_significance_map(
        self,
        significant_contexts: list[CABACContextModel],
        last_contexts: list[CABACContextModel],
    ) -> list[bool]:
        for contexts, name in (
            (significant_contexts, "significant_coeff_flag"),
            (last_contexts, "last_significant_coeff_flag"),
        ):
            if not isinstance(contexts, list) or len(contexts) != 15 or any(
                not isinstance(model, CABACContextModel) or not 0 <= model._state_index < 64
                for model in contexts
            ):
                raise CABACError(f"luma4x4 {name} requires 15 valid frame-scan contexts")
            if len({id(model) for model in contexts}) != 15:
                raise CABACError(f"luma4x4 {name} contexts must be distinct")

        trial_bits = BitReader(self._bits._data)
        trial_bits._bit_offset = self._bits._bit_offset
        trial = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
        trial._bits = trial_bits
        trial._code_range = self._code_range
        trial._code_offset = self._code_offset
        trial._terminated = self._terminated
        trial_significant = [CABACContextModel.__new__(CABACContextModel) for _ in range(15)]
        trial_last = [CABACContextModel.__new__(CABACContextModel) for _ in range(15)]
        for target, source in zip(trial_significant, significant_contexts):
            target._state_index = source._state_index
            target._value_mps = source._value_mps
        for target, source in zip(trial_last, last_contexts):
            target._state_index = source._state_index
            target._value_mps = source._value_mps

        significance = [False] * 16
        last_found = False
        for scan_index in range(15):
            if not trial.decode_bin(trial_significant[scan_index]):
                continue
            significance[scan_index] = True
            if trial.decode_bin(trial_last[scan_index]):
                last_found = True
                break
        if not last_found:
            significance[15] = True

        self._bits._bit_offset = trial._bits._bit_offset
        self._code_range = trial._code_range
        self._code_offset = trial._code_offset
        self._terminated = trial._terminated
        for target, source in zip(significant_contexts, trial_significant):
            target._state_index = source._state_index
            target._value_mps = source._value_mps
        for target, source in zip(last_contexts, trial_last):
            target._state_index = source._state_index
            target._value_mps = source._value_mps
        return significance

    def decode_coeff_abs_level_minus1(
        self,
        first_context: CABACContextModel,
        greater_one_context: CABACContextModel,
    ) -> int:
        if (
            not isinstance(first_context, CABACContextModel)
            or not isinstance(greater_one_context, CABACContextModel)
            or first_context is greater_one_context
            or not 0 <= first_context._state_index < 64
            or not 0 <= greater_one_context._state_index < 64
        ):
            raise CABACError("coefficient level requires two distinct valid contexts")

        trial_bits = BitReader(self._bits._data)
        trial_bits._bit_offset = self._bits._bit_offset
        trial = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
        trial._bits = trial_bits
        trial._code_range = self._code_range
        trial._code_offset = self._code_offset
        trial._terminated = self._terminated
        trial_first = CABACContextModel.__new__(CABACContextModel)
        trial_greater = CABACContextModel.__new__(CABACContextModel)
        trial_first._state_index = first_context._state_index
        trial_first._value_mps = first_context._value_mps
        trial_greater._state_index = greater_one_context._state_index
        trial_greater._value_mps = greater_one_context._value_mps

        if not trial.decode_bin(trial_first):
            absolute_level = 1
        else:
            absolute_level = 2
            while absolute_level < 15 and trial.decode_bin(trial_greater):
                absolute_level += 1
            if absolute_level == 15:
                prefix_length = 0
                while trial.decode_bypass_bin():
                    prefix_length += 1
                    if prefix_length >= 23:
                        raise CABACError("CABAC coefficient level exceeds supported bypass prefix")
                suffix = 0
                for _ in range(prefix_length):
                    suffix = (suffix << 1) | int(trial.decode_bypass_bin())
                absolute_level = 14 + (1 << prefix_length) + suffix

        self._bits._bit_offset = trial._bits._bit_offset
        self._code_range = trial._code_range
        self._code_offset = trial._code_offset
        self._terminated = trial._terminated
        first_context._state_index = trial_first._state_index
        first_context._value_mps = trial_first._value_mps
        greater_one_context._state_index = trial_greater._state_index
        greater_one_context._value_mps = trial_greater._value_mps
        return absolute_level - 1

    def decode_coeff_sign(self, abs_level_minus1: int) -> int:
        if not 0 <= abs_level_minus1 < 0x7FFFFFFF:
            raise CABACError("CABAC coefficient magnitude exceeds the signed output range")
        negative = self.decode_bypass_bin()
        magnitude = abs_level_minus1 + 1
        return -magnitude if negative else magnitude

    def decode_motion_vector_difference(
        self, neighbor_magnitude: int, contexts: list[CABACContextModel]
    ) -> int:
        """Decode one MVD component using seven contexts starting at 40 or 47."""
        if (
            not isinstance(neighbor_magnitude, int)
            or isinstance(neighbor_magnitude, bool)
            or neighbor_magnitude < 0
        ):
            raise CABACError("MVD neighbor magnitude must be a nonnegative integer")
        if not isinstance(contexts, list) or len(contexts) != 7 or any(
            not isinstance(model, CABACContextModel)
            or not 0 <= model._state_index < 64
            for model in contexts
        ):
            raise CABACError("MVD decoding requires seven valid consecutive contexts")
        if len({id(model) for model in contexts}) != 7:
            raise CABACError("MVD contexts must be distinct")

        context_index = int(neighbor_magnitude >= 3) + int(neighbor_magnitude >= 33)
        trial_bits = BitReader(self._bits._data)
        trial_bits._bit_offset = self._bits._bit_offset
        trial = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
        trial._bits = trial_bits
        trial._code_range = self._code_range
        trial._code_offset = self._code_offset
        trial._terminated = self._terminated
        trial_contexts = [CABACContextModel.__new__(CABACContextModel) for _ in range(7)]
        for target, source in zip(trial_contexts, contexts):
            target._state_index = source._state_index
            target._value_mps = source._value_mps

        if not trial.decode_bin(trial_contexts[context_index]):
            magnitude = 0
        else:
            magnitude = 1
            context_index = 3
            while magnitude < 9 and trial.decode_bin(trial_contexts[context_index]):
                if magnitude < 4:
                    context_index += 1
                magnitude += 1

            if magnitude >= 9:
                suffix_length = 3
                while trial.decode_bypass_bin():
                    if suffix_length > 30:
                        raise CABACError("CABAC MVD exceeds the signed output range")
                    increment = 1 << suffix_length
                    if magnitude > _MAX_MOTION_VECTOR_DIFFERENCE - increment:
                        raise CABACError("CABAC MVD exceeds the signed output range")
                    magnitude += increment
                    suffix_length += 1
                if suffix_length > 30:
                    raise CABACError("CABAC MVD exceeds the signed output range")
                for bit_index in range(suffix_length - 1, -1, -1):
                    if trial.decode_bypass_bin():
                        increment = 1 << bit_index
                        if magnitude > _MAX_MOTION_VECTOR_DIFFERENCE - increment:
                            raise CABACError("CABAC MVD exceeds the signed output range")
                        magnitude += increment

            negative = trial.decode_bypass_bin()
            if negative:
                magnitude = -magnitude

        self._bits._bit_offset = trial._bits._bit_offset
        self._code_range = trial._code_range
        self._code_offset = trial._code_offset
        self._terminated = trial._terminated
        for target, source in zip(contexts, trial_contexts):
            target._state_index = source._state_index
            target._value_mps = source._value_mps
        return magnitude

    def decode_luma4x4_residual_levels(
        self,
        significance: list[bool],
        contexts: list[CABACContextModel],
    ) -> list[int]:
        if not isinstance(significance, list) or len(significance) != 16:
            raise CABACError("luma4x4 significance map must contain 16 scan positions")
        if not any(significance):
            raise CABACError("CABAC residual block has no significant coefficients")
        if not isinstance(contexts, list) or len(contexts) != 10 or any(
            not isinstance(model, CABACContextModel) or not 0 <= model._state_index < 64
            for model in contexts
        ):
            raise CABACError("luma4x4 residual decoding requires ten valid coefficient contexts")
        if len({id(model) for model in contexts}) != 10:
            raise CABACError("luma4x4 coefficient contexts must be distinct")

        trial_bits = BitReader(self._bits._data)
        trial_bits._bit_offset = self._bits._bit_offset
        trial = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
        trial._bits = trial_bits
        trial._code_range = self._code_range
        trial._code_offset = self._code_offset
        trial._terminated = self._terminated
        trial_contexts = [CABACContextModel.__new__(CABACContextModel) for _ in range(10)]
        for target, source in zip(trial_contexts, contexts):
            target._state_index = source._state_index
            target._value_mps = source._value_mps

        levels = [0] * 16
        node_context = 0
        for scan_index in range(15, -1, -1):
            if not significance[scan_index]:
                continue
            level_minus1 = trial.decode_coeff_abs_level_minus1(
                trial_contexts[_COEFF_ABS_LEVEL1_CONTEXT[node_context]],
                trial_contexts[_COEFF_ABS_LEVEL_GREATER1_CONTEXT[node_context]],
            )
            levels[scan_index] = trial.decode_coeff_sign(level_minus1)
            if level_minus1 == 0:
                node_context = _COEFF_LEVEL1_TRANSITION[node_context]
            else:
                node_context = _COEFF_LEVEL_GREATER1_TRANSITION[node_context]

        self._bits._bit_offset = trial._bits._bit_offset
        self._code_range = trial._code_range
        self._code_offset = trial._code_offset
        self._terminated = trial._terminated
        for target, source in zip(contexts, trial_contexts):
            target._state_index = source._state_index
            target._value_mps = source._value_mps
        return levels

    def decode_luma4x4_residual_block(
        self,
        significant_contexts: list[CABACContextModel],
        last_contexts: list[CABACContextModel],
        coefficient_contexts: list[CABACContextModel],
    ) -> list[int]:
        context_banks = (significant_contexts, last_contexts, coefficient_contexts)
        expected_lengths = (15, 15, 10)
        if any(
            not isinstance(bank, list)
            or len(bank) != expected_length
            or any(not isinstance(model, CABACContextModel) for model in bank)
            for bank, expected_length in zip(context_banks, expected_lengths)
        ):
            raise CABACError("luma4x4 residual block requires 15, 15, and 10 context models")
        if len({id(model) for bank in context_banks for model in bank}) != 40:
            raise CABACError("luma4x4 residual block contexts must be distinct")

        trial_bits = BitReader(self._bits._data)
        trial_bits._bit_offset = self._bits._bit_offset
        trial = CABACArithmeticDecoder.__new__(CABACArithmeticDecoder)
        trial._bits = trial_bits
        trial._code_range = self._code_range
        trial._code_offset = self._code_offset
        trial._terminated = self._terminated
        trial_banks = [
            [CABACContextModel.__new__(CABACContextModel) for _ in bank]
            for bank in context_banks
        ]
        for targets, sources in zip(trial_banks, context_banks):
            for target, source in zip(targets, sources):
                target._state_index = source._state_index
                target._value_mps = source._value_mps

        significance = trial.decode_luma4x4_significance_map(trial_banks[0], trial_banks[1])
        scan_levels = trial.decode_luma4x4_residual_levels(significance, trial_banks[2])
        raster_levels = place_luma4x4_scan_levels(scan_levels)

        self._bits._bit_offset = trial._bits._bit_offset
        self._code_range = trial._code_range
        self._code_offset = trial._code_offset
        self._terminated = trial._terminated
        for targets, sources in zip(context_banks, trial_banks):
            for target, source in zip(targets, sources):
                target._state_index = source._state_index
                target._value_mps = source._value_mps
        return raster_levels

    def decode_bypass_bin(self) -> bool:
        self._validate_bin_state()
        start_offset = self._bits._bit_offset
        try:
            bit = self._bits.read_bit()
        except BitstreamError as error:
            self._bits._bit_offset = start_offset
            raise CABACError(f"CABAC bypass bin: {error}") from error
        code_offset = (self._code_offset << 1) | int(bit)
        decoded = code_offset >= self._code_range
        if decoded:
            code_offset -= self._code_range
        self._code_offset = code_offset
        return decoded

    def decode_terminate_bin(self) -> bool:
        self._validate_bin_state()
        start_offset = self._bits._bit_offset
        original_range, original_offset = self._code_range, self._code_offset
        self._code_range -= 2
        if self._code_offset >= self._code_range:
            self._terminated = True
            return True
        try:
            self.renormalize()
        except CABACError:
            self._code_range, self._code_offset = original_range, original_offset
            self._bits._bit_offset = start_offset
            raise
        return False

    def _validate_bin_state(self) -> None:
        if self._terminated:
            raise CABACTerminatedError("CABAC decoder is already terminated")
        if not 256 <= self._code_range <= self.INITIAL_RANGE:
            raise CABACError("CABAC bin decoding range is outside [256,510]")
        if self._code_offset >= self._code_range:
            raise CABACError("CABAC code offset is outside the arithmetic range")

    def renormalize(self) -> None:
        if getattr(self, "_terminated", False):
            raise CABACTerminatedError("CABAC decoder is already terminated")
        if not 0 < self._code_range <= self.INITIAL_RANGE:
            raise CABACError("CABAC arithmetic range is outside [1,510]")
        start_offset = self._bits._bit_offset
        code_range = self._code_range
        code_offset = self._code_offset
        try:
            while code_range < 256:
                bit = self._bits.read_bit()
                code_range <<= 1
                code_offset = (code_offset << 1) | int(bit)
                if code_offset >= code_range:
                    raise CABACError("CABAC code offset is outside the arithmetic range")
        except (BitstreamError, CABACError) as error:
            self._bits._bit_offset = start_offset
            if isinstance(error, CABACError):
                raise
            raise CABACError(f"CABAC renormalization: {error}") from error
        self._code_range = code_range
        self._code_offset = code_offset


def place_luma4x4_scan_levels(scan_levels: list[int]) -> list[int]:
    """Map frame-scan luma 4x4 coefficient levels into raster order."""
    if not isinstance(scan_levels, list) or len(scan_levels) != 16:
        raise CABACError("luma4x4 scan levels must contain 16 positions")
    raster_levels = [0] * 16
    for scan_index, raster_index in enumerate(_LUMA4X4_SCAN_TO_RASTER):
        raster_levels[raster_index] = scan_levels[scan_index]
    return raster_levels