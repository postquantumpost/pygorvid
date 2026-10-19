"""Codec-neutral raw 8-bit YUV frame storage and plane access."""

from dataclasses import dataclass


@dataclass(frozen=True)
class Yuv420Frame:
    width: int
    height: int
    y_stride: int
    u_stride: int
    v_stride: int
    y: bytes
    u: bytes
    v: bytes

    def _plane_bytes(self, plane: bytes, width: int, height: int, stride: int) -> bytes:
        if any(not isinstance(value, int) or isinstance(value, bool) for value in (width, height, stride)):
            raise ValueError("reference picture frame layout is invalid or truncated")
        if stride <= 0 or stride < width:
            raise ValueError("reference picture frame layout is invalid or truncated")
        if len(plane) < stride * height:
            raise ValueError("reference picture frame layout is invalid or truncated")
        if stride == width and isinstance(plane, bytes) and len(plane) == width * height:
            return plane
        return b"".join(plane[row * stride : row * stride + width] for row in range(height))

    def luma_plane_bytes(self) -> bytes:
        return self._plane_bytes(self.y, self.width, self.height, self.y_stride)

    def u_plane_bytes(self) -> bytes:
        chroma_width = self.width // 2 + self.width % 2
        chroma_height = self.height // 2 + self.height % 2
        return self._plane_bytes(self.u, chroma_width, chroma_height, self.u_stride)

    def v_plane_bytes(self) -> bytes:
        chroma_width = self.width // 2 + self.width % 2
        chroma_height = self.height // 2 + self.height % 2
        return self._plane_bytes(self.v, chroma_width, chroma_height, self.v_stride)

    def luma_plane_matches(self, reference: bytes) -> bool:
        return self.luma_plane_bytes() == bytes(reference)


