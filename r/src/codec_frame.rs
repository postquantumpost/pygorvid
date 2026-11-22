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

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct DecodedVideoFrame {
    pub presentation_index: usize,
    pub sample_index: usize,
    pub pts_ticks: i64,
    pub duration_ticks: u32,
    pub timescale: u32,
    pub frame: Yuv420Frame,
}

impl Yuv420Frame {
    fn chroma_height(&self, plane: &[u8], stride: usize) -> usize {
        if stride > 0 && plane.len() >= stride * self.height {
            self.height
        } else {
            self.height / 2 + self.height % 2
        }
    }

    fn chroma_width(&self, plane: &[u8]) -> usize {
        if plane.len() >= self.width * self.height {
            self.width
        } else {
            self.width / 2 + self.width % 2
        }
    }

    pub(crate) fn validate_planes(&self) -> io::Result<()> {
        let chroma_width_u = self.chroma_width(&self.u);
        let chroma_width_v = self.chroma_width(&self.v);
        let chroma_height_u = self.chroma_height(&self.u, self.u_stride);
        let chroma_height_v = self.chroma_height(&self.v, self.v_stride);
        if self.y_stride == self.width
            && self.u_stride == chroma_width_u
            && self.v_stride == chroma_width_v
            && (self.width == 0
                || self.height == 0
                || self
                    .width
                    .checked_mul(self.height)
                    .map_or(false, |required| self.y.len() >= required))
            && (chroma_width_u == 0
                || chroma_height_u == 0
                || chroma_width_u
                    .checked_mul(chroma_height_u)
                    .map_or(false, |required| self.u.len() >= required))
            && (chroma_width_v == 0
                || chroma_height_v == 0
                || chroma_width_v
                    .checked_mul(chroma_height_v)
                    .map_or(false, |required| self.v.len() >= required))
        {
            return Ok(());
        }
        for (plane, width, height, stride) in [
            (&self.y, self.width, self.height, self.y_stride),
            (&self.u, chroma_width_u, chroma_height_u, self.u_stride),
            (&self.v, chroma_width_v, chroma_height_v, self.v_stride),
        ] {
            if width > 0
                && height > 0
                && (stride == 0
                    || stride < width
                    || stride
                        .checked_mul(height)
                        .map_or(true, |required| plane.len() < required))
            {
                return Err(invalid(
                    "reference picture frame layout is invalid or truncated",
                ));
            }
        }
        Ok(())
    }

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
        let chroma_width = self.chroma_width(&self.u);
        let chroma_height = self.chroma_height(&self.u, self.u_stride);
        self.plane_view(&self.u, chroma_width, chroma_height, self.u_stride)
    }

    pub fn v_plane(&self) -> io::Result<std::borrow::Cow<'_, [u8]>> {
        let chroma_width = self.chroma_width(&self.v);
        let chroma_height = self.chroma_height(&self.v, self.v_stride);
        self.plane_view(&self.v, chroma_width, chroma_height, self.v_stride)
    }
}

impl Yuv420Frame {
    pub fn u_plane_bytes(&self) -> io::Result<Vec<u8>> {
        let chroma_width = self.chroma_width(&self.u);
        let chroma_height = self.chroma_height(&self.u, self.u_stride);
        self.plane_bytes(&self.u, chroma_width, chroma_height, self.u_stride)
    }

    pub fn v_plane_bytes(&self) -> io::Result<Vec<u8>> {
        let chroma_width = self.chroma_width(&self.v);
        let chroma_height = self.chroma_height(&self.v, self.v_stride);
        self.plane_bytes(&self.v, chroma_width, chroma_height, self.v_stride)
    }
}
