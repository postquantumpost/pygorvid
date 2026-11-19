class _BitWriter:
    def __init__(self) -> None:
        self.data = bytearray()
        self.bit_count = 0

    def bits(self, value: int, count: int) -> None:
        for shift in range(count - 1, -1, -1):
            if self.bit_count % 8 == 0:
                self.data.append(0)
            self.data[-1] |= ((value >> shift) & 1) << (7 - self.bit_count % 8)
            self.bit_count += 1

    def ue(self, value: int) -> None:
        count = (value + 1).bit_length()
        self.bits(0, count - 1)
        self.bits(value + 1, count)

    def se(self, value: int) -> None:
        self.ue(2 * value - 1 if value > 0 else -2 * value)

    def align(self) -> None:
        while self.bit_count % 8:
            self.bits(0, 1)

    def nal(self, header: int) -> bytes:
        self.bits(1, 1)
        self.align()
        result = bytearray([header])
        zeros = 0
        for value in self.data:
            if zeros == 2 and value <= 3:
                result.append(3)
                zeros = 0
            result.append(value)
            zeros = zeros + 1 if value == 0 else 0
        return bytes(result)


class _CABACWriter:
    def __init__(self, contexts) -> None:
        self.contexts = contexts
        self.low = 0
        self.range = 510
        self.bits_to_follow = 0
        self.bits: list[int] = []
        self.first_bit = True

    def renormalize(self) -> None:
        while self.range < 256:
            if self.low < 256:
                self.put(0)
            elif self.low >= 512:
                self.put(1)
                self.low -= 512
            else:
                self.bits_to_follow += 1
                self.low -= 256
            self.low <<= 1
            self.range <<= 1

    def put(self, bit: int) -> None:
        if self.first_bit:
            self.first_bit = False
        else:
            self.bits.append(bit)
        while self.bits_to_follow:
            self.bits.append(bit ^ 1)
            self.bits_to_follow -= 1

    def decision(self, value: int, context_index: int) -> None:
        from .h264_cabac import _RANGE_LPS

        model = self.contexts[context_index]
        range_lps = _RANGE_LPS[(self.range >> 6) & 3][model._state_index]
        if bool(value) == model._value_mps:
            self.range -= range_lps
            model.update(bool(value))
            self.renormalize()
            return
        self.low += self.range - range_lps
        self.range = range_lps
        model.update(bool(value))
        self.renormalize()

    def luma_cbp(self, left: int, top: int, desired: int) -> None:
        context_index = int(not left & 0x02) + 2 * int(not top & 0x04)
        self.decision(desired & 1, 73 + context_index)
        current = desired & 1
        context_index = int(not current & 0x01) + 2 * int(not top & 0x08)
        self.decision((desired >> 1) & 1, 73 + context_index)
        current |= desired & 2
        context_index = int(not left & 0x08) + 2 * int(not (current & 1))
        self.decision((desired >> 2) & 1, 73 + context_index)
        current |= desired & 4
        context_index = int(not (current & 4)) + 2 * int(not (current & 2))
        self.decision((desired >> 3) & 1, 73 + context_index)

    def zero_luma_cbp(self, left: int, top: int) -> None:
        self.luma_cbp(left, top, 0)

    def bypass(self, value: int) -> None:
        self.low <<= 1
        if value:
            self.low += self.range
        if self.low >= 1024:
            self.low -= 1024
            self.put(1)
        elif self.low < 512:
            self.put(0)
        else:
            self.low -= 512
            self.bits_to_follow += 1

    def terminate(self, value: int) -> None:
        self.range -= 2
        if not value:
            self.renormalize()
            return
        self.low += self.range
        self.range = 2
        self.renormalize()
        self.put((self.low >> 9) & 1)
        self.bits.extend([(self.low >> 8) & 1, 1])

    def level(self, magnitude: int, first_context: int, greater_context: int) -> None:
        if magnitude == 1:
            self.decision(0, first_context)
        else:
            self.decision(1, first_context)
            for _ in range(min(magnitude - 2, 13)):
                self.decision(1, greater_context + 5)
            if magnitude <= 14:
                self.decision(0, greater_context + 5)
        self.bypass(0)

    def end(self) -> None:
        self.range -= 2
        self.low += self.range
        self.range = 2
        self.renormalize()
        self.put((self.low >> 9) & 1)
        self.bits.extend([(self.low >> 8) & 1, 1])

    def data(self, padding: int = 4) -> bytes:
        result = bytearray((len(self.bits) + 7) // 8)
        for index, bit in enumerate(self.bits):
            if bit:
                result[index // 8] |= 0x80 >> (index % 8)
        result.extend(bytes(padding))
        return bytes(result)


class H264Encoder:
    """Initial subset: 64x64, 8-bit YUV420 grey, Baseline I_PCM IDR pictures."""

    def sequence_parameter_set(self) -> bytes:
        return self._sequence_parameter_set()

    def _sequence_parameter_set(
        self, sps_id: int = 0, poc_type: int = 2, transform_bypass: bool = False,
        scaling_lists: bool = False, gaps_allowed: bool = False,
        vui: bool = False,
    ) -> bytes:
        writer = _BitWriter()
        writer.bits(100, 8)
        writer.bits(0, 8)
        writer.bits(20, 8)
        writer.ue(sps_id)
        for value in (1, 0, 0):
            writer.ue(value)
        writer.bits(int(transform_bypass), 1)
        writer.bits(int(scaling_lists), 1)
        if scaling_lists:
            for list_index in range(8):
                present = list_index in (0, 6)
                writer.bits(int(present), 1)
                if present:
                    writer.se(4 if list_index == 0 else 12)
                    for _ in range((16 if list_index < 6 else 64) - 1):
                        writer.se(0)
        writer.ue(0)
        writer.ue(poc_type)
        if poc_type == 0:
            writer.ue(2)
        elif poc_type == 1:
            writer.bits(0, 1)
            writer.se(0)
            writer.se(0)
            writer.ue(1)
            writer.se(2)
        writer.ue(2)
        writer.bits(int(gaps_allowed), 1)
        writer.ue(3)
        writer.ue(3)
        writer.bits(0b110, 3)
        writer.bits(int(vui), 1)
        if vui:
            writer.bits(1, 1)
            writer.bits(1, 8)
            writer.bits(0, 1)
            writer.bits(1, 1)
            writer.bits(5, 3)
            writer.bits(1, 1)
            writer.bits(1, 1)
            writer.bits(1, 8)
            writer.bits(1, 8)
            writer.bits(1, 8)
            writer.bits(0, 1)
            writer.bits(1, 1)
            writer.bits(1, 32)
            writer.bits(120, 32)
            writer.bits(1, 1)
            for _ in range(4):
                writer.bits(0, 1)
        return writer.nal(0x67)

    def picture_parameter_set(self) -> bytes:
        return self._picture_parameter_set(0, False, 0)

    def _picture_parameter_set(
        self, pps_id: int, cabac: bool = False, weighted_bipred_idc: int = 0,
        sps_id: int = 0, transform_8x8: bool = False,
    ) -> bytes:
        writer = _BitWriter()
        writer.ue(pps_id)
        writer.ue(sps_id)
        writer.bits(int(cabac), 1)
        writer.bits(0, 1)
        for _ in range(3):
            writer.ue(0)
        writer.bits(1, 1)
        writer.bits(weighted_bipred_idc, 2)
        for _ in range(3):
            writer.ue(0)  # Signed Exp-Golomb zero has the same representation.
        writer.bits(0b100, 3)
        if transform_8x8:
            writer.bits(1, 1)
            writer.bits(0, 1)
            writer.se(0)
        return writer.nal(0x68)

    def encode_grey_frame(self, idr_pic_id: int = 0) -> bytes:
        return self._encode_pcm_frame(idr_pic_id, False)

    def encode_generated_frame(self, frame_index: int) -> bytes:
        if not 0 <= frame_index <= 65535:
            raise ValueError("H.264 IDR picture ID must be between 0 and 65535")
        if frame_index == 3:
            return self._encode_intra16x16_dc_frame()
        if frame_index == 4:
            return self._encode_intra16x16_prediction_modes_frame()
        if frame_index == 5:
            return self._encode_intra4x4_dc_frame()
        if frame_index == 6:
            return self._encode_intra4x4_directional_frame()
        if frame_index == 7:
            return self._encode_intra4x4_directional_frame(6, True)
        if frame_index == 8:
            return self._encode_intra4x4_directional_frame(7, True, True)
        if frame_index == 9:
            return self._encode_intra4x4_directional_frame(8, True, True, True)
        if 10 <= frame_index <= 18:
            return self._encode_p_frame(frame_index)
        if frame_index == 19:
            return self._encode_p_frame(19, future_reference=True)
        if frame_index == 20:
            return self._encode_b_frame()
        if frame_index in (21, 22, 23):
            return self._encode_b_feature_frame(frame_index)
        if frame_index == 24:
            return self._encode_b_weighted_frame(explicit=True)
        if frame_index == 25:
            return self._encode_b_weighted_frame(explicit=False)
        if frame_index == 26:
            return self._encode_inter_residual_frame()
        if frame_index == 27:
            return self._encode_cabac_i_frame()
        if frame_index == 28:
            return self._encode_cabac_p_frame()
        if frame_index == 29:
            return self._encode_cabac_b_frame()
        if frame_index == 30:
            return self._encode_cabac_residual_frame()
        if frame_index == 31:
            return self._encode_cabac_feature_frame(4, 2, -3)
        if frame_index == 32:
            return self._encode_feature_slices(
                ((0, 8, 0, 0, 2), (8, 8, 1, 0, 0))
            )
        if frame_index == 33:
            return self._encode_feature_slices(
                ((0, 8, 0, 1, 1), (8, 8, 0, -1, 1))
            )
        if frame_index == 34:
            return self._encode_poc_type1_idr_frame()
        if frame_index in (35, 36):
            return self._encode_cabac_feature_frame(
                5, 0, 0, transform_8x8=True,
                directional_modes=frame_index == 36,
            )
        if frame_index == 37:
            return self._encode_cabac_feature_frame(
                7, 0, 0, transform_8x8=True, idr=True
            )
        if frame_index in (38, 39):
            coded = self._encode_cabac_feature_frame(
                8, 0, 0, transform_8x8=True, idr=True
            )
            return coded if frame_index == 38 else self._encode_signaled_frame(coded)
        return self._encode_pcm_frame(frame_index, frame_index == 1)

    def _finish_cabac_slice(
        self, header: _BitWriter, cabac: _CABACWriter, idr: bool = False,
    ) -> bytes:
        cabac.end()
        return self._cabac_nal(
            0x65 if idr else 0x21, header.data + cabac.data()
        )

    def _encode_cabac_feature_slice(
        self, first_macroblock: int, macroblock_count: int, pps_id: int,
        qp_delta: int, disable_deblocking: int, alpha_offset: int,
        beta_offset: int, transform_8x8: bool = False,
        directional_modes: bool = False, idr: bool = False,
    ) -> bytes:
        from .h264_cabac import initialize_slice_contexts

        header = _BitWriter()
        header.ue(first_macroblock)
        header.ue(2)
        header.ue(pps_id)
        header.bits(0, 4)
        if idr:
            header.ue(0)
            header.bits(0, 2)
        else:
            header.bits(0, 1)
        header.se(qp_delta)
        header.ue(disable_deblocking)
        if disable_deblocking != 1:
            header.se(alpha_offset)
            header.se(beta_offset)
        while header.bit_count % 8:
            header.bits(1, 1)
        cabac = _CABACWriter(initialize_slice_contexts(2, 0, 26 + qp_delta))
        for offset in range(macroblock_count):
            macroblock = first_macroblock + offset
            cabac.decision(0, 3)
            if transform_8x8:
                context = 399 + int(macroblock % 4 != 0) + int(macroblock >= 4)
                cabac.decision(1, context)
                modes = (
                    (0, 1, 2, 3)
                    if directional_modes and macroblock >= 4
                    else (1, 2, 1, 2)
                    if directional_modes and macroblock
                    else (2, 2, 2, 2)
                )
                for mode in modes:
                    if mode == 2:
                        cabac.decision(1, 68)
                    else:
                        cabac.decision(0, 68)
                        encoded = mode - int(mode >= 2)
                        for bit in range(3):
                            cabac.decision((encoded >> bit) & 1, 69)
            else:
                for _ in range(16):
                    cabac.decision(1, 68)
            cabac.decision(0, 64)
            cabac.zero_luma_cbp(
                15 if macroblock % 4 == 0 else 0,
                15 if macroblock < 4 else 0,
            )
            cabac.decision(0, 77)
            if offset != macroblock_count - 1:
                cabac.terminate(False)
        return self._finish_cabac_slice(header, cabac, idr)

    def _encode_cabac_feature_frame(
        self, pps_id: int, alpha_offset: int, beta_offset: int,
        transform_8x8: bool = False,
        directional_modes: bool = False, idr: bool = False,
    ) -> bytes:
        return self._sample(
            self._encode_cabac_feature_slice(
                0, 16, pps_id, 0, 0, alpha_offset, beta_offset,
                transform_8x8, directional_modes, idr,
            )
        )

    def _encode_feature_slices(
        self, slices: tuple[tuple[int, int, int, int, int], ...],
    ) -> bytes:
        nals = []
        for first, count, disable, qp_delta, alpha in slices:
            writer = _BitWriter()
            writer.ue(first)
            writer.ue(2)
            writer.ue(9)
            writer.bits(0, 4)
            writer.bits(0, 1)
            writer.se(qp_delta)
            writer.ue(disable)
            if disable != 1:
                writer.se(alpha)
                writer.se(-alpha)
            for _ in range(count):
                writer.ue(0)
                for _ in range(16):
                    writer.bits(1, 1)
                writer.ue(0)
                writer.ue(3)
            nals.append(writer.nal(0x21))
        return b"".join(len(nal).to_bytes(4, "big") + nal for nal in nals)

    def _encode_poc_type1_idr_frame(self) -> bytes:
        writer = _BitWriter()
        for value in (0, 2, 6):
            writer.ue(value)
        writer.bits(0, 4)
        writer.ue(0)
        writer.se(0)
        writer.bits(0, 2)
        writer.se(0)
        writer.ue(0)
        writer.se(0)
        writer.se(0)
        for _ in range(16):
            writer.ue(25)
            writer.align()
            writer.data.extend(bytes([128]) * 384)
            writer.bit_count += 384 * 8
        return self._sample(writer.nal(0x65))

    def _encode_signaled_frame(self, coded: bytes) -> bytes:
        def rbsp_nal(header: int, payload: bytes) -> bytes:
            writer = _BitWriter()
            for value in payload:
                writer.bits(value, 8)
            return writer.nal(header)

        picture_length = int.from_bytes(coded[:4], "big")
        picture = coded[4:4 + picture_length]
        aud = rbsp_nal(0x09, b"\x10")
        sei = rbsp_nal(0x06, b"\x05\x12PYGORVID-FRAME-39!")
        sequence_end = rbsp_nal(0x0A, b"")
        stream_end = rbsp_nal(0x0B, b"")
        return b"".join(
            len(nal).to_bytes(4, "big") + nal
            for nal in (aud, sei, picture, sequence_end, stream_end)
        )

    @staticmethod
    def _b_macroblock(writer: _BitWriter, kind: int, spatial_direct: bool,
                      sub_types: tuple[int, ...] = ()) -> None:
        writer.ue(kind)
        if kind == 22:
            for sub_type in sub_types:
                writer.ue(sub_type)
            writer.ue(0)
            return
        if kind == 0:
            writer.ue(0)
            return
        if 1 <= kind <= 3:
            for list_index in range(2):
                if kind == 1 and list_index == 1 or kind == 2 and list_index == 0:
                    continue
                writer.se(0)
                writer.se(0)
            writer.ue(0)
            return
        if 4 <= kind <= 21:
            partition_count = 2
            flags_by_kind = (
                (1, 0), (1, 0), (0, 1), (0, 1), (1, 0),
                (1, 0), (0, 1), (0, 1), (1, 1), (1, 1),
                (0, 1), (0, 1), (1, 1), (1, 1), (0, 1),
                (0, 1), (1, 1), (1, 1),
            )
            flags = flags_by_kind[kind - 4]
            for _ in range(partition_count):
                for active in flags:
                    if active:
                        writer.se(0)
                        writer.se(0)
            writer.ue(0)

    def _begin_b_slice(self, writer: _BitWriter, frame_num: int, spatial_direct: bool,
                       pps_id: int = 0, explicit_weights: bool = False) -> None:
        writer.ue(0)
        writer.ue(1)
        writer.ue(pps_id)
        writer.bits(frame_num, 4)
        writer.bits(int(spatial_direct), 1)
        writer.bits(0, 1)
        writer.bits(0, 1)
        writer.bits(0, 1)
        if explicit_weights:
            writer.ue(0)
            writer.ue(0)
            for _ in range(2):
                writer.bits(1, 1)
                writer.se(2)
                writer.se(-8)
                writer.bits(1, 1)
                writer.se(2)
                writer.se(4)
                writer.se(2)
                writer.se(-4)
        writer.bits(0, 1)
        writer.ue(0)
        writer.ue(1)

    def _encode_b_feature_frame(self, frame_index: int) -> bytes:
        writer = _BitWriter()
        spatial_direct = frame_index != 22
        self._begin_b_slice(writer, (frame_index - 16) & 15, spatial_direct)
        for macroblock in range(16):
            writer.ue(0)
            if frame_index == 23:
                self._b_macroblock(writer, 1 + (macroblock % 11) % 3, spatial_direct)
            elif frame_index == 21 and macroblock % 2:
                self._b_macroblock(writer, 22, spatial_direct, (0, 0, 0, 0))
            else:
                self._b_macroblock(writer, 0, spatial_direct)
        return self._sample(writer.nal(0x21))

    def _encode_b_weighted_frame(self, explicit: bool) -> bytes:
        pps_id = 1 if explicit else 2
        writer = _BitWriter()
        self._begin_b_slice(writer, 9 if explicit else 10, True, pps_id, explicit)
        for macroblock in range(16):
            writer.ue(0)
            self._b_macroblock(writer, 3, True)
        return self._sample(writer.nal(0x21))

    def _encode_inter_residual_frame(self) -> bytes:
        writer = _BitWriter()
        writer.ue(0)
        writer.ue(0)
        writer.ue(0)
        writer.bits(11, 4)
        writer.bits(0, 1)
        writer.bits(0, 1)
        self._prediction_weight_table(writer, 1)
        writer.bits(0, 1)
        writer.ue(0)
        writer.ue(1)
        for macroblock in range(16):
            writer.ue(0)
            writer.ue(0)
            writer.se(0)
            writer.se(0)
            if macroblock in (0, 15):
                writer.ue(2)
                writer.se(1)
                for block in range(4):
                    if block in (0, 3):
                        writer.bits(0b01, 2)
                        writer.bits(0, 1)
                        writer.bits(1, 1)
                    else:
                        writer.bits(1, 1)
            else:
                writer.ue(0)
        return self._sample(writer.nal(0x21))

    @staticmethod
    def _sample(nal: bytes) -> bytes:
        return len(nal).to_bytes(4, "big") + nal

    @staticmethod
    def _cabac_nal(header: int, rbsp: bytes) -> bytes:
        result = bytearray([header])
        zeros = 0
        for value in rbsp:
            if zeros == 2 and value <= 3:
                result.append(3)
                zeros = 0
            result.append(value)
            zeros = zeros + 1 if value == 0 else 0
        return bytes(result)

    def _encode_cabac_i_frame(self) -> bytes:
        from .h264_cabac import initialize_slice_contexts

        pps = self._picture_parameter_set(3, True, 0)
        header = _BitWriter()
        header.ue(0)
        header.ue(2)
        header.ue(3)
        header.bits(12, 4)
        header.bits(0, 1)
        header.ue(0)
        header.ue(1)
        while header.bit_count % 8:
            header.bits(1, 1)
        cabac = _CABACWriter(initialize_slice_contexts(2, 0, 26))
        for macroblock in range(16):
            cabac.decision(0, 3)
            for _ in range(16):
                cabac.decision(1, 68)
            cabac.decision(0, 64)
            cabac.zero_luma_cbp(
                15 if macroblock % 4 == 0 else 0,
                15 if macroblock < 4 else 0,
            )
            cabac.decision(0, 77)
            if macroblock != 15:
                cabac.terminate(False)
        cabac.end()
        nal = self._cabac_nal(0x21, header.data + cabac.data())
        return self._sample(nal)

    def _encode_cabac_p_frame(self) -> bytes:
        from .h264_cabac import initialize_slice_contexts

        pps = self._picture_parameter_set(3, True, 0)
        header = _BitWriter()
        header.ue(0)
        header.ue(0)
        header.ue(3)
        header.bits(13, 4)
        header.bits(0, 1)
        header.bits(0, 1)
        header.ue(0)
        header.ue(0)
        header.bits(0, 1)
        header.bits(0, 1)
        header.bits(0, 1)
        header.ue(0)
        header.ue(0)
        header.ue(1)
        while header.bit_count % 8:
            header.bits(1, 1)
        cabac = _CABACWriter(initialize_slice_contexts(0, 0, 26))
        for macroblock in range(16):
            skipped = macroblock % 3 == 1
            left_skip = macroblock % 4 != 0 and (macroblock - 1) % 3 == 1
            top_skip = macroblock >= 4 and (macroblock - 4) % 3 == 1
            increment = int(macroblock % 4 != 0 and not left_skip)
            increment += int(macroblock >= 4 and not top_skip)
            cabac.decision(int(skipped), 11 + increment)
            if skipped:
                if macroblock != 15:
                    cabac.terminate(False)
                continue
            cabac.decision(0, 14)
            cabac.decision(0, 15)
            cabac.decision(0, 16)
            cabac.decision(0, 40)
            cabac.decision(0, 47)
            cabac.zero_luma_cbp(
                15 if macroblock % 4 == 0 else 0,
                15 if macroblock < 4 else 0,
            )
            cabac.decision(0, 77)
            if macroblock != 15:
                cabac.terminate(False)
        cabac.end()
        nal = self._cabac_nal(0x21, header.data + cabac.data())
        return self._sample(nal)

    def _encode_cabac_b_frame(self) -> bytes:
        from .h264_cabac import initialize_slice_contexts

        pps = self._picture_parameter_set(3, True, 0)
        header = _BitWriter()
        header.ue(0)
        header.ue(1)
        header.ue(3)
        header.bits(14, 4)
        header.bits(1, 1)
        header.bits(0, 1)
        header.bits(0, 1)
        header.bits(0, 1)
        header.bits(0, 1)
        header.ue(0)
        header.ue(0)
        header.ue(1)
        while header.bit_count % 8:
            header.bits(1, 1)
        cabac = _CABACWriter(initialize_slice_contexts(1, 0, 26))
        for macroblock in range(16):
            cabac.decision(
                0, 24 + int(macroblock % 4 != 0) + int(macroblock >= 4)
            )
            cabac.decision(0, 27)
            cabac.zero_luma_cbp(
                15 if macroblock % 4 == 0 else 0,
                15 if macroblock < 4 else 0,
            )
            cabac.decision(0, 77)
            if macroblock != 15:
                cabac.terminate(False)
        cabac.end()
        nal = self._cabac_nal(0x21, header.data + cabac.data())
        return self._sample(nal)

    def _encode_cabac_residual_frame(self) -> bytes:
        from .h264_cabac import initialize_slice_contexts

        pps = self._picture_parameter_set(3, True, 0)
        header = _BitWriter()
        header.ue(0)
        header.ue(2)
        header.ue(3)
        header.bits(15, 4)
        header.bits(0, 1)
        header.ue(0)
        header.ue(1)
        while header.bit_count % 8:
            header.bits(1, 1)
        cabac = _CABACWriter(initialize_slice_contexts(2, 0, 26))
        for macroblock in range(16):
            cabac.decision(0, 3)
            for _ in range(16):
                cabac.decision(1, 68)
            cabac.decision(0, 64)
            cabac.luma_cbp(
                15 if macroblock % 4 == 0 else 0,
                15 if macroblock < 4 else 0,
                1 if macroblock == 15 else 0,
            )
            cabac.decision(0, 77)
            if macroblock == 15:
                cabac.decision(0, 60)
                cabac.decision(1, 93)
                cabac.decision(1, 134)
                cabac.decision(1, 195)
                cabac.decision(0, 248)
                cabac.bypass(0)
                cabac.decision(0, 94)
                cabac.decision(0, 95)
                cabac.decision(0, 93)
            if macroblock != 15:
                cabac.terminate(False)
        cabac.end()
        nal = self._cabac_nal(0x21, header.data + cabac.data())
        return self._sample(nal)

    @staticmethod
    def _prediction_weight_table(writer: _BitWriter, reference_count: int,
                                 explicit: bool = False) -> None:
        writer.ue(0)
        writer.ue(0)
        for _ in range(reference_count):
            if explicit:
                writer.bits(1, 1)
                writer.se(2)
                writer.se(-16)
                writer.bits(1, 1)
                writer.se(2)
                writer.se(8)
                writer.se(2)
                writer.se(-8)
            else:
                writer.bits(0, 1)
                writer.bits(0, 1)

    @staticmethod
    def _write_p_macroblock(writer: _BitWriter, kind: int, vectors: tuple[tuple[int, int], ...],
                            references: tuple[int, ...] = (), multiple_references: bool = False,
                            sub_types: tuple[int, ...] = ()) -> None:
        writer.ue(kind)
        if kind in (3, 4):
            for sub_type in sub_types or (0, 1, 2, 3):
                writer.ue(sub_type)
        for partition, vector in enumerate(vectors):
            if multiple_references and kind != 4:
                writer.bits(int(references[partition] == 0), 1)
            writer.se(vector[0])
            writer.se(vector[1])
        writer.ue(0)

    def _encode_p_frame(self, frame_index: int, future_reference: bool = False) -> bytes:
        writer = _BitWriter()
        writer.ue(0)
        writer.ue(0)
        writer.ue(0)
        writer.bits((frame_index - 1) & 15, 4)

        multiple_references = frame_index in (16, 17)
        writer.bits(int(multiple_references), 1)
        reference_count = 2 if multiple_references else 1
        if multiple_references:
            writer.ue(1)

        reordered = frame_index == 17
        writer.bits(int(reordered), 1)
        if reordered:
            writer.ue(0)
            writer.ue(1)
            writer.ue(3)

        self._prediction_weight_table(writer, reference_count, frame_index == 18)
        adaptive_marking = reordered
        writer.bits(int(adaptive_marking), 1)
        if adaptive_marking:
            writer.ue(1)
            writer.ue(0)
            writer.ue(0)
        writer.ue(0)
        writer.ue(1)

        if frame_index == 10:
            for _ in range(16):
                writer.ue(0)
                self._write_p_macroblock(writer, 0, ((0, 0),))
        elif frame_index == 11:
            writer.ue(0)
            self._write_p_macroblock(writer, 0, ((0, 0),))
            writer.ue(14)
            self._write_p_macroblock(writer, 0, ((0, 0),))
        elif frame_index == 12:
            motion = ((0, 0), (4, 0), (-4, 0), (0, 4), (0, -4))
            for macroblock in range(16):
                writer.ue(0)
                self._write_p_macroblock(writer, 0, (motion[macroblock % len(motion)],))
        elif frame_index == 13:
            for macroblock in range(16):
                writer.ue(0)
                kind = 1 if macroblock % 2 == 0 else 2
                self._write_p_macroblock(writer, kind, ((4, 0), (-4, 4)))
        elif frame_index == 14:
            for macroblock in range(16):
                writer.ue(0)
                kind = 3 if macroblock % 2 == 0 else 4
                sub_types = (0, 1, 2, 3)
                counts = (1, 2, 2, 4)
                vectors = tuple(
                    ((sub_index - 1) * 4, (part - 1) * 4)
                    for sub_index, sub_type in enumerate(sub_types)
                    for part in range(counts[sub_type])
                )
                self._write_p_macroblock(writer, kind, vectors, sub_types=sub_types)
        elif frame_index == 15:
            vectors = ((1, 0), (-1, 2), (2, -1), (-2, 1))
            for macroblock in range(16):
                writer.ue(0)
                self._write_p_macroblock(writer, 0, (vectors[macroblock % 4],))
        elif frame_index in (16, 17):
            references = tuple(macroblock % 2 for macroblock in range(16))
            for macroblock, reference in enumerate(references):
                writer.ue(0)
                self._write_p_macroblock(
                    writer, 0, ((0, 0),), (reference,), True
                )
        elif frame_index == 19:
            for _ in range(16):
                writer.ue(0)
                self._write_p_macroblock(writer, 0, ((4, 0),))
        else:
            for _ in range(16):
                writer.ue(0)
                self._write_p_macroblock(writer, 0, ((0, 0),))
        nal = writer.nal(0x21)
        return len(nal).to_bytes(4, "big") + nal

    def _encode_b_frame(self) -> bytes:
        writer = _BitWriter()
        writer.ue(0)
        writer.ue(1)
        writer.ue(0)
        writer.bits(2, 4)
        writer.bits(1, 1)
        writer.bits(0, 1)
        writer.bits(0, 1)
        writer.bits(0, 1)
        writer.ue(0)
        writer.ue(1)
        for macroblock in range(16):
            writer.ue(0)
            writer.ue(1 + macroblock % 3)
            active_lists = 1 if macroblock % 3 < 2 else 2
            for _ in range(active_lists):
                writer.se(0)
                writer.se(0)
            writer.ue(0)
        nal = writer.nal(0x01)
        return len(nal).to_bytes(4, "big") + nal

    def _encode_intra4x4_directional_frame(
        self, frame_num: int = 5, residual: bool = False,
        chroma_residual: bool = False, expanded_cavlc: bool = False,
    ) -> bytes:
        writer = _BitWriter()
        for value in (0, 2, 0):
            writer.ue(value)
        writer.bits(frame_num, 4)
        writer.bits(0, 1)
        writer.ue(0)
        writer.ue(1)

        writer.ue(25)
        writer.align()
        for row in range(16):
            writer.data.extend(32 + 3 * column + 5 * row for column in range(16))
        for base in (80, 176):
            writer.data.extend(base + column + 2 * row for row in range(8) for column in range(8))
        writer.bit_count += 384 * 8

        modes = (2, 2, 2, 4, 8, 1, 5, 6, 7, 0, 3, 4, 5, 7, 6, 8)
        scan = (0, 1, 4, 5, 2, 3, 6, 7, 8, 9, 12, 13, 10, 11, 14, 15)
        macroblock_modes = [[2] * 16]
        for macroblock in range(1, 16):
            writer.ue(0)
            mb_x, mb_y = macroblock % 4, macroblock // 4
            for block, desired in enumerate(modes):
                raster = scan[block]
                column, row = raster % 4, raster // 4
                left_mode = (
                    modes[scan.index(raster - 1)]
                    if column
                    else macroblock_modes[macroblock - 1][scan.index(row * 4 + 3)]
                    if mb_x
                    else None
                )
                top_mode = (
                    modes[scan.index(raster - 4)]
                    if row
                    else macroblock_modes[macroblock - 4][scan.index(12 + column)]
                    if mb_y
                    else None
                )
                predicted = 2 if left_mode is None or top_mode is None else min(left_mode, top_mode)
                if desired == predicted:
                    writer.bits(1, 1)
                else:
                    writer.bits(0, 1)
                    writer.bits(desired - (desired >= predicted), 3)
            writer.ue(0)
            if residual and macroblock == 15:
                writer.ue(42 if chroma_residual else 29)
                writer.se(1)
                if expanded_cavlc:
                    # nC = 0: sixteen non-trailing coefficients.
                    writer.bits(0b0000000000000100, 16)
                    for sign in (0, 1) * 8:
                        writer.bits(2 if sign == 0 else 3, 2)
                    # nC >= 8: three trailing ones, then two total zeros.
                    writer.bits(0b001011, 6)
                    writer.bits(0b010, 3)
                    writer.bits(0b110, 3)
                    writer.bits(0b010, 3)
                    # nC >= 8 and then nC = 2 select different empty-block tables.
                    writer.bits(0b000011, 6)
                    writer.bits(0b11, 2)
                else:
                    # Positive trailing-one token, zero runs, then three empty blocks.
                    writer.bits(0b01, 2)
                    writer.bits(0, 1)
                    writer.bits(1, 1)
                    writer.bits(1, 1)
                    writer.bits(1, 1)
                    writer.bits(1, 1)
                if chroma_residual:
                    # Cb DC +1 and Cr DC -1, each at the sole 2x2 DC coefficient.
                    writer.bits(0b101, 3)
                    writer.bits(0b111, 3)
                    # One Cb and one Cr AC coefficient; remaining AC blocks are empty.
                    for sign in (0, 1):
                        writer.bits(0b01, 2)
                        writer.bits(sign, 1)
                        writer.bits(1, 1)
                        writer.bits(0b111, 3)
            else:
                writer.ue(3)
            macroblock_modes.append(list(modes))

        nal = writer.nal(0x21)
        return len(nal).to_bytes(4, "big") + nal

    def _encode_intra4x4_dc_frame(self) -> bytes:
        writer = _BitWriter()
        for value in (0, 2, 0):
            writer.ue(value)
        writer.bits(4, 4)
        writer.bits(0, 1)
        writer.ue(0)
        writer.ue(1)
        for _ in range(16):
            writer.ue(0)
            for _ in range(16):
                writer.bits(1, 1)
            writer.ue(0)
            writer.ue(3)
        nal = writer.nal(0x21)
        return len(nal).to_bytes(4, "big") + nal

    def _encode_intra16x16_dc_frame(self) -> bytes:
        writer = _BitWriter()
        for value in (0, 2, 0):
            writer.ue(value)
        writer.bits(2, 4)
        writer.bits(0, 1)
        writer.ue(0)
        writer.ue(1)
        for _ in range(16):
            writer.ue(3)
            writer.ue(0)
            writer.ue(0)
            writer.bits(1, 1)
        nal = writer.nal(0x21)
        return len(nal).to_bytes(4, "big") + nal

    def _encode_intra16x16_prediction_modes_frame(self) -> bytes:
        writer = _BitWriter()
        for value in (0, 2, 0):
            writer.ue(value)
        writer.bits(3, 4)
        writer.bits(0, 1)
        writer.ue(0)
        writer.ue(1)
        luma_modes = (2, 2, 2, 2, 0, 3, 2, 1, 0, 0, 1, 2, 0, 3, 2, 1)
        chroma_modes = (0, 1, 1, 1, 2, 3, 0, 1, 2, 2, 3, 0, 2, 3, 2, 1)
        for index, (luma_mode, chroma_mode) in enumerate(
            zip(luma_modes, chroma_modes)
        ):
            writer.ue(1 + luma_mode + (8 if index == 0 else 0))
            writer.ue(chroma_mode)
            writer.ue(0)
            if index == 0:
                writer.bits(0b01000011, 8)
                writer.bits(0b10000, 5)
                writer.bits(0b10000, 5)
                writer.bits(0xFF, 8)
            else:
                writer.bits(1, 1)
        nal = writer.nal(0x21)
        return len(nal).to_bytes(4, "big") + nal

    def _encode_pcm_frame(self, idr_pic_id: int, emulation_prevention_test: bool) -> bytes:
        if not 0 <= idr_pic_id <= 65535:
            raise ValueError("H.264 IDR picture ID must be between 0 and 65535")
        writer = _BitWriter()
        for value in (0, 2, 0):
            writer.ue(value)
        non_idr_reference = idr_pic_id == 2
        writer.bits(1 if non_idr_reference else 0, 4)
        if non_idr_reference:
            writer.bits(0, 1)
        else:
            writer.ue(idr_pic_id)
            writer.bits(0, 2)
        writer.ue(0)
        writer.ue(1)
        for macroblock in range(16):
            writer.ue(25)
            writer.align()
            if emulation_prevention_test and macroblock == 0:
                writer.data.extend((0, 0, 1))
                writer.data.extend(bytes([128]) * 253)
            else:
                writer.data.extend(bytes([128]) * 256)
            writer.data.extend(bytes([128]) * 128)
            writer.bit_count += 384 * 8
        nal = writer.nal(0x21 if non_idr_reference else 0x65)
        return len(nal).to_bytes(4, "big") + nal
