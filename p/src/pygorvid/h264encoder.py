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
        if not 0 <= idr_pic_id <= 65535:
            raise ValueError("H.264 IDR picture ID must be between 0 and 65535")
        writer = _BitWriter()
        for value in (0, 2, 0):
            writer.ue(value)
        writer.bits(0, 4)
        writer.ue(idr_pic_id)
        writer.bits(0, 2)
        writer.ue(0)
        writer.ue(1)
        for _ in range(16):
            writer.ue(25)
            writer.align()
            writer.data.extend(bytes([128]) * 384)
            writer.bit_count += 384 * 8
        nal = writer.nal(0x65)
        return len(nal).to_bytes(4, "big") + nal
