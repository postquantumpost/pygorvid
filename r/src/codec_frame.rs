use std::io;

fn invalid(message: &str) -> io::Error {
    io::Error::new(io::ErrorKind::InvalidData, message)
}

#[derive(Debug, Clone, PartialEq, Eq, serde::Serialize, serde::Deserialize)]
pub struct Yuv420Frame {
    pub width: usize,
    pub height: usize,
    pub y_stride: usize,
    pub u_stride: usize,
    pub v_stride: usize,
    pub y: Vec<u8>,
    pub u: Vec<u8>,
    pub v: Vec<u8>,
}

impl Yuv420Frame {
    pub(crate) fn plane_bytes(
        &self,
        plane: &[u8],
        width: usize,
        height: usize,
        stride: usize,
    ) -> io::Result<Vec<u8>> {
        if width == 0 || height == 0 {
            return Ok(Vec::new());
        }
        if stride == 0 || stride < width || plane.len() < stride * height {
            return Err(invalid(
                "reference picture frame layout is invalid or truncated",
            ));
        }
        let mut out = Vec::with_capacity(width * height);
        for row in 0..height {
            let start = row * stride;
            let end = start + width;
            out.extend_from_slice(&plane[start..end]);
        }
        Ok(out)
    }

    pub fn luma_plane_bytes(&self) -> io::Result<Vec<u8>> {
        self.plane_bytes(&self.y, self.width, self.height, self.y_stride)
    }

    /// Like `plane_bytes`, but borrows the plane when it is already tightly packed.
    fn plane_view<'a>(
        &self,
        plane: &'a [u8],
        width: usize,
        height: usize,
        stride: usize,
    ) -> io::Result<std::borrow::Cow<'a, [u8]>> {
        if width > 0 && height > 0 && stride == width && plane.len() >= stride * height {
            return Ok(std::borrow::Cow::Borrowed(&plane[..width * height]));
        }
        self.plane_bytes(plane, width, height, stride)
            .map(std::borrow::Cow::Owned)
    }

    pub fn luma_plane(&self) -> io::Result<std::borrow::Cow<'_, [u8]>> {
        self.plane_view(&self.y, self.width, self.height, self.y_stride)
    }

    pub fn u_plane(&self) -> io::Result<std::borrow::Cow<'_, [u8]>> {
        let chroma_width = self.width / 2 + self.width % 2;
        let chroma_height = self.height / 2 + self.height % 2;
        self.plane_view(&self.u, chroma_width, chroma_height, self.u_stride)
    }

    pub fn v_plane(&self) -> io::Result<std::borrow::Cow<'_, [u8]>> {
        let chroma_width = self.width / 2 + self.width % 2;
        let chroma_height = self.height / 2 + self.height % 2;
        self.plane_view(&self.v, chroma_width, chroma_height, self.v_stride)
    }
}

impl Yuv420Frame {
    pub fn u_plane_bytes(&self) -> io::Result<Vec<u8>> {
        let chroma_width = self.width / 2 + self.width % 2;
        let chroma_height = self.height / 2 + self.height % 2;
        self.plane_bytes(&self.u, chroma_width, chroma_height, self.u_stride)
    }

    pub fn v_plane_bytes(&self) -> io::Result<Vec<u8>> {
        let chroma_width = self.width / 2 + self.width % 2;
        let chroma_height = self.height / 2 + self.height % 2;
        self.plane_bytes(&self.v, chroma_width, chroma_height, self.v_stride)
    }
}
