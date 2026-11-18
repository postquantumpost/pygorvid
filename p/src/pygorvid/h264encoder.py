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


class H264Encoder:
    """Initial subset: 64x64, 8-bit YUV420 grey, Baseline I_PCM IDR pictures."""

    def sequence_parameter_set(self) -> bytes:
        writer = _BitWriter()
        writer.bits(66, 8)
        writer.bits(0xC0, 8)
        writer.bits(20, 8)
        for value in (0, 0, 2, 1):
            writer.ue(value)
        writer.bits(0, 1)
        writer.ue(3)
        writer.ue(3)
        writer.bits(0b1100, 4)
        return writer.nal(0x67)

    def picture_parameter_set(self) -> bytes:
        writer = _BitWriter()
        writer.ue(0)
        writer.ue(0)
        writer.bits(0, 2)
        for _ in range(3):
            writer.ue(0)
        writer.bits(0, 3)
        for _ in range(3):
            writer.ue(0)  # Signed Exp-Golomb zero has the same representation.
        writer.bits(0b100, 3)
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
        return self._encode_pcm_frame(frame_index, frame_index == 1)

    def _encode_intra4x4_directional_frame(self) -> bytes:
        writer = _BitWriter()
        for value in (0, 2, 0):
            writer.ue(value)
        writer.bits(5, 4)
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
