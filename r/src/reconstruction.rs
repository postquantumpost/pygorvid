use std::io::{self, ErrorKind};

use crate::slice::RefPicListModification;

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct ReferencePicture {
    pub identifier: u32,
    pub frame_num: u32,
    pub picture_order_cnt: i64,
    pub long_term_frame_idx: Option<u32>,
}

#[derive(Debug, Clone, Copy, Default, PartialEq, Eq)]
pub struct PocType0State {
    pub previous_pic_order_cnt_msb: i64,
    pub previous_pic_order_cnt_lsb: u32,
}

impl PocType0State {
    /// Calculates progressive-frame POC type 0 and advances state for reference pictures.
    pub fn calculate(
        &mut self,
        max_pic_order_cnt_lsb: u32,
        pic_order_cnt_lsb: u32,
        delta_pic_order_bottom: i64,
        nal_ref_idc: u8,
        idr: bool,
    ) -> io::Result<i64> {
        if !(16..=1 << 16).contains(&max_pic_order_cnt_lsb)
            || !max_pic_order_cnt_lsb.is_power_of_two()
            || pic_order_cnt_lsb >= max_pic_order_cnt_lsb
            || (!idr && self.previous_pic_order_cnt_lsb >= max_pic_order_cnt_lsb)
            || nal_ref_idc > 3
            || (idr && nal_ref_idc == 0)
        {
            return Err(invalid(
                "picture order count input or arithmetic is invalid",
            ));
        }

        let (previous_msb, previous_lsb) = if idr {
            (0, 0)
        } else {
            (
                self.previous_pic_order_cnt_msb,
                self.previous_pic_order_cnt_lsb,
            )
        };
        let half_range = max_pic_order_cnt_lsb / 2;
        let current_msb = if pic_order_cnt_lsb < previous_lsb
            && previous_lsb - pic_order_cnt_lsb >= half_range
        {
            previous_msb.checked_add(i64::from(max_pic_order_cnt_lsb))
        } else if pic_order_cnt_lsb > previous_lsb && pic_order_cnt_lsb - previous_lsb > half_range
        {
            previous_msb.checked_sub(i64::from(max_pic_order_cnt_lsb))
        } else {
            Some(previous_msb)
        }
        .ok_or_else(|| invalid("picture order count input or arithmetic is invalid"))?;
        let top_field_order_cnt = current_msb
            .checked_add(i64::from(pic_order_cnt_lsb))
            .ok_or_else(|| invalid("picture order count input or arithmetic is invalid"))?;
        let bottom_field_order_cnt = top_field_order_cnt
            .checked_add(delta_pic_order_bottom)
            .ok_or_else(|| invalid("picture order count input or arithmetic is invalid"))?;
        if nal_ref_idc != 0 {
            self.previous_pic_order_cnt_msb = current_msb;
            self.previous_pic_order_cnt_lsb = pic_order_cnt_lsb;
        }
        Ok(top_field_order_cnt.min(bottom_field_order_cnt))
    }
}

#[derive(Debug, Clone, Copy, Default, PartialEq, Eq)]
pub struct PocType12State {
    pub previous_frame_num: u32,
    pub previous_frame_num_offset: i64,
}

impl PocType12State {
    pub fn calculate_type1(
        &mut self,
        max_frame_num: u32,
        frame_num: u32,
        delta_pic_order_cnt0: i64,
        delta_pic_order_cnt1: i64,
        offset_for_non_ref_pic: i64,
        offset_for_top_to_bottom_field: i64,
        offset_for_ref_frame: &[i64],
        nal_ref_idc: u8,
        idr: bool,
    ) -> io::Result<i64> {
        let frame_num_offset = self.frame_num_offset(max_frame_num, frame_num, nal_ref_idc, idr)?;
        if offset_for_ref_frame.len() > 255 {
            return Err(invalid(
                "picture order count input or arithmetic is invalid",
            ));
        }
        let abs_frame_num = if offset_for_ref_frame.is_empty() {
            0
        } else {
            frame_num_offset
                .checked_add(i64::from(frame_num))
                .ok_or_else(|| invalid("picture order count input or arithmetic is invalid"))?
        };
        if abs_frame_num < 0 {
            return Err(invalid(
                "picture order count input or arithmetic is invalid",
            ));
        }
        let abs_frame_num = if nal_ref_idc == 0 && abs_frame_num > 0 {
            abs_frame_num - 1
        } else {
            abs_frame_num
        };

        let cycle_delta = sum_poc_offsets(offset_for_ref_frame)?;
        let mut expected_poc = 0_i64;
        if abs_frame_num > 0 {
            let cycle_length = offset_for_ref_frame.len() as i64;
            let cycle_count = (abs_frame_num - 1) / cycle_length;
            let frame_in_cycle = ((abs_frame_num - 1) % cycle_length) as usize;
            expected_poc = cycle_count
                .checked_mul(cycle_delta)
                .and_then(|value| {
                    sum_poc_offsets(&offset_for_ref_frame[..=frame_in_cycle])
                        .ok()
                        .and_then(|partial| value.checked_add(partial))
                })
                .ok_or_else(|| invalid("picture order count input or arithmetic is invalid"))?;
        }
        if nal_ref_idc == 0 {
            expected_poc = expected_poc
                .checked_add(offset_for_non_ref_pic)
                .ok_or_else(|| invalid("picture order count input or arithmetic is invalid"))?;
        }
        let top_field_order_cnt = expected_poc
            .checked_add(delta_pic_order_cnt0)
            .ok_or_else(|| invalid("picture order count input or arithmetic is invalid"))?;
        let bottom_field_order_cnt = top_field_order_cnt
            .checked_add(offset_for_top_to_bottom_field)
            .and_then(|value| value.checked_add(delta_pic_order_cnt1))
            .ok_or_else(|| invalid("picture order count input or arithmetic is invalid"))?;
        self.advance(frame_num, frame_num_offset, nal_ref_idc);
        Ok(top_field_order_cnt.min(bottom_field_order_cnt))
    }

    pub fn calculate_type2(
        &mut self,
        max_frame_num: u32,
        frame_num: u32,
        nal_ref_idc: u8,
        idr: bool,
    ) -> io::Result<i64> {
        let frame_num_offset = self.frame_num_offset(max_frame_num, frame_num, nal_ref_idc, idr)?;
        let picture_order_cnt = if idr {
            0
        } else {
            let absolute_frame_num = frame_num_offset
                .checked_add(i64::from(frame_num))
                .ok_or_else(|| invalid("picture order count input or arithmetic is invalid"))?;
            absolute_frame_num
                .checked_mul(2)
                .and_then(|value| value.checked_sub(i64::from(nal_ref_idc == 0)))
                .ok_or_else(|| invalid("picture order count input or arithmetic is invalid"))?
        };
        self.advance(frame_num, frame_num_offset, nal_ref_idc);
        Ok(picture_order_cnt)
    }

    fn frame_num_offset(
        &self,
        max_frame_num: u32,
        frame_num: u32,
        nal_ref_idc: u8,
        idr: bool,
    ) -> io::Result<i64> {
        if !(16..=1 << 16).contains(&max_frame_num)
            || !max_frame_num.is_power_of_two()
            || frame_num >= max_frame_num
            || nal_ref_idc > 3
            || (idr && (nal_ref_idc == 0 || frame_num != 0))
        {
            return Err(invalid(
                "picture order count input or arithmetic is invalid",
            ));
        }
        if idr {
            return Ok(0);
        }
        if self.previous_frame_num >= max_frame_num || self.previous_frame_num_offset < 0 {
            return Err(invalid(
                "picture order count input or arithmetic is invalid",
            ));
        }
        if self.previous_frame_num > frame_num {
            self.previous_frame_num_offset
                .checked_add(i64::from(max_frame_num))
                .ok_or_else(|| invalid("picture order count input or arithmetic is invalid"))
        } else {
            Ok(self.previous_frame_num_offset)
        }
    }

    fn advance(&mut self, frame_num: u32, frame_num_offset: i64, nal_ref_idc: u8) {
        if nal_ref_idc != 0 {
            self.previous_frame_num = frame_num;
            self.previous_frame_num_offset = frame_num_offset;
        }
    }
}

fn sum_poc_offsets(offsets: &[i64]) -> io::Result<i64> {
    offsets.iter().try_fold(0_i64, |sum, offset| {
        sum.checked_add(*offset)
            .ok_or_else(|| invalid("picture order count input or arithmetic is invalid"))
    })
}

#[derive(Debug, Clone, PartialEq, Eq)]
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
    fn plane_bytes(&self, plane: &[u8], width: usize, height: usize, stride: usize) -> io::Result<Vec<u8>> {
        if width == 0 || height == 0 {
            return Ok(Vec::new());
        }
        if stride == 0 || stride < width || plane.len() < stride * height {
            return Err(invalid("reference picture frame layout is invalid or truncated"));
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
}

#[derive(Debug, Clone)]
pub struct Yuv420FrameBuilder {
    picture_width_in_mbs: usize,
    crop_left: usize,
    crop_top: usize,
    frame: Yuv420Frame,
    written: Vec<bool>,
}

impl Yuv420FrameBuilder {
    pub fn new(
        picture_width_in_mbs: usize,
        picture_height_in_mbs: usize,
        crop_left: usize,
        crop_top: usize,
        crop_right: usize,
        crop_bottom: usize,
    ) -> io::Result<Self> {
        let coded_width = picture_width_in_mbs
            .checked_mul(16)
            .filter(|_| picture_width_in_mbs > 0)
            .ok_or_else(|| invalid("YUV 4:2:0 macroblock assembly is invalid"))?;
        let coded_height = picture_height_in_mbs
            .checked_mul(16)
            .filter(|_| picture_height_in_mbs > 0)
            .ok_or_else(|| invalid("YUV 4:2:0 macroblock assembly is invalid"))?;
        let macroblock_count = picture_width_in_mbs
            .checked_mul(picture_height_in_mbs)
            .ok_or_else(|| invalid("YUV 4:2:0 macroblock assembly is invalid"))?;
        if crop_left % 2 != 0
            || crop_top % 2 != 0
            || crop_right % 2 != 0
            || crop_bottom % 2 != 0
        {
            return Err(invalid("YUV 4:2:0 macroblock assembly is invalid"));
        }
        let width = coded_width
            .checked_sub(crop_left)
            .and_then(|value| value.checked_sub(crop_right))
            .filter(|value| *value > 0)
            .ok_or_else(|| invalid("YUV 4:2:0 macroblock assembly is invalid"))?;
        let height = coded_height
            .checked_sub(crop_top)
            .and_then(|value| value.checked_sub(crop_bottom))
            .filter(|value| *value > 0)
            .ok_or_else(|| invalid("YUV 4:2:0 macroblock assembly is invalid"))?;
        let y_size = width
            .checked_mul(height)
            .ok_or_else(|| invalid("YUV 4:2:0 macroblock assembly is invalid"))?;
        let chroma_width = width / 2;
        let chroma_height = height / 2;
        let chroma_size = chroma_width
            .checked_mul(chroma_height)
            .ok_or_else(|| invalid("YUV 4:2:0 macroblock assembly is invalid"))?;
        Ok(Self {
            picture_width_in_mbs,
            crop_left,
            crop_top,
            frame: Yuv420Frame {
                width,
                height,
                y_stride: width,
                u_stride: chroma_width,
                v_stride: chroma_width,
                y: vec![0; y_size],
                u: vec![0; chroma_size],
                v: vec![0; chroma_size],
            },
            written: vec![false; macroblock_count],
        })
    }

    pub fn place_macroblock(
        &mut self,
        address: usize,
        y_block: &[u8; 256],
        u_block: &[u8; 64],
        v_block: &[u8; 64],
    ) -> io::Result<()> {
        if address >= self.written.len() || self.written[address] {
            return Err(invalid("YUV 4:2:0 macroblock assembly is invalid"));
        }
        let mb_x = address % self.picture_width_in_mbs;
        let mb_y = address / self.picture_width_in_mbs;
        for row in 0..16 {
            let dst_y = mb_y * 16 + row;
            if dst_y < self.crop_top || dst_y - self.crop_top >= self.frame.height {
                continue;
            }
            let output_y = dst_y - self.crop_top;
            for column in 0..16 {
                let dst_x = mb_x * 16 + column;
                if dst_x >= self.crop_left && dst_x - self.crop_left < self.frame.width {
                    self.frame.y[output_y * self.frame.y_stride + dst_x - self.crop_left] =
                        y_block[row * 16 + column];
                }
            }
        }
        for row in 0..8 {
            let dst_y = mb_y * 8 + row;
            if dst_y < self.crop_top / 2 || dst_y - self.crop_top / 2 >= self.frame.height / 2 {
                continue;
            }
            let output_y = dst_y - self.crop_top / 2;
            for column in 0..8 {
                let dst_x = mb_x * 8 + column;
                if dst_x >= self.crop_left / 2 && dst_x - self.crop_left / 2 < self.frame.width / 2
                {
                    let output_x = dst_x - self.crop_left / 2;
                    let block_index = row * 8 + column;
                    self.frame.u[output_y * self.frame.u_stride + output_x] = u_block[block_index];
                    self.frame.v[output_y * self.frame.v_stride + output_x] = v_block[block_index];
                }
            }
        }
        self.written[address] = true;
        Ok(())
    }

    pub fn finish(&self) -> io::Result<Yuv420Frame> {
        if self.written.iter().any(|written| !written) {
            return Err(invalid("YUV 4:2:0 macroblock assembly is incomplete"));
        }
        Ok(self.frame.clone())
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

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PresentationPicture {
    pub picture_order_cnt: i64,
    pub frame: Yuv420Frame,
}

#[derive(Debug, Clone, Default)]
pub struct PresentationOrderBuffer {
    max_reorder_pictures: usize,
    pending: Vec<PresentationPicture>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum LumaDeblockingMode {
    AllEdges,
    Disabled,
    AllExceptSliceBoundaries,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct LumaDeblockingParameters {
    pub mode: LumaDeblockingMode,
    pub index_a: u8,
    pub index_b: u8,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct LumaDeblockingThresholds {
    pub alpha: u8,
    pub beta: u8,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct LumaEdgeSamples {
    pub p0: u8,
    pub p1: u8,
    pub p2: u8,
    pub q0: u8,
    pub q1: u8,
    pub q2: u8,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct LumaStrongEdgeSamples {
    pub p0: u8,
    pub p1: u8,
    pub p2: u8,
    pub p3: u8,
    pub q0: u8,
    pub q1: u8,
    pub q2: u8,
    pub q3: u8,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct ChromaEdgeSamples {
    pub p0: u8,
    pub p1: u8,
    pub q0: u8,
    pub q1: u8,
}

pub fn filter_chroma_weak_edge(samples: ChromaEdgeSamples, tc0: u8) -> ChromaEdgeSamples {
    let tc = i32::from(tc0) + 1;
    let delta = (((i32::from(samples.q0) - i32::from(samples.p0)) << 2)
        + (i32::from(samples.p1) - i32::from(samples.q1))
        + 4)
        >> 3;
    let delta = delta.clamp(-tc, tc);
    let p0 = (i32::from(samples.p0) + delta).clamp(0, 255) as u8;
    let q0 = (i32::from(samples.q0) - delta).clamp(0, 255) as u8;
    ChromaEdgeSamples {
        p0,
        p1: samples.p1,
        q0,
        q1: samples.q1,
    }
}

pub fn filter_luma_weak_edge(samples: LumaEdgeSamples, beta: u8, tc0: u8) -> LumaEdgeSamples {
    let ap = samples.p2.abs_diff(samples.p0);
    let aq = samples.q2.abs_diff(samples.q0);
    let ap_smooth = if ap < beta { 1 } else { 0 };
    let aq_smooth = if aq < beta { 1 } else { 0 };
    let tc = i32::from(tc0) + ap_smooth + aq_smooth;
    let delta = (((i32::from(samples.q0) - i32::from(samples.p0)) << 2) + i32::from(samples.p1)
        - i32::from(samples.q1)
        + 4)
        >> 3;
    let delta = delta.clamp(-tc, tc);
    let mut filtered = samples;
    filtered.p0 = clip_luma_sample(i32::from(samples.p0) + delta);
    filtered.q0 = clip_luma_sample(i32::from(samples.q0) - delta);
    if ap < beta {
        let delta_p1 = (i32::from(samples.p2)
            + ((i32::from(samples.p0) + i32::from(samples.q0) + 1) >> 1)
            - (i32::from(samples.p1) << 1))
            >> 1;
        filtered.p1 = clip_luma_sample(
            i32::from(samples.p1) + delta_p1.clamp(-i32::from(tc0), i32::from(tc0)),
        );
    }
    if aq < beta {
        let delta_q1 = (i32::from(samples.q2)
            + ((i32::from(samples.p0) + i32::from(samples.q0) + 1) >> 1)
            - (i32::from(samples.q1) << 1))
            >> 1;
        filtered.q1 = clip_luma_sample(
            i32::from(samples.q1) + delta_q1.clamp(-i32::from(tc0), i32::from(tc0)),
        );
    }
    filtered
}

pub fn filter_luma_strong_edge(
    samples: LumaStrongEdgeSamples,
    alpha: u8,
    beta: u8,
) -> LumaEdgeSamples {
    let mut filtered = LumaEdgeSamples {
        p0: samples.p0,
        p1: samples.p1,
        p2: samples.p2,
        q0: samples.q0,
        q1: samples.q1,
        q2: samples.q2,
    };
    let alpha_passes = samples.p0.abs_diff(samples.q0) < (alpha >> 2) + 2;
    if samples.p2.abs_diff(samples.p0) < beta && alpha_passes {
        filtered.p0 = clip_luma_sample(
            (i32::from(samples.p2)
                + 2 * i32::from(samples.p1)
                + 2 * i32::from(samples.p0)
                + 2 * i32::from(samples.q0)
                + i32::from(samples.q1)
                + 4)
                >> 3,
        );
        filtered.p1 = clip_luma_sample(
            (i32::from(samples.p2)
                + i32::from(samples.p1)
                + i32::from(samples.p0)
                + i32::from(samples.q0)
                + 2)
                >> 2,
        );
        filtered.p2 = clip_luma_sample(
            (2 * i32::from(samples.p3)
                + 3 * i32::from(samples.p2)
                + i32::from(samples.p1)
                + i32::from(samples.p0)
                + i32::from(samples.q0)
                + 4)
                >> 3,
        );
    } else {
        filtered.p0 = clip_luma_sample(
            (2 * i32::from(samples.p1) + i32::from(samples.p0) + i32::from(samples.q1) + 2) >> 2,
        );
    }
    if samples.q2.abs_diff(samples.q0) < beta && alpha_passes {
        filtered.q0 = clip_luma_sample(
            (i32::from(samples.p1)
                + 2 * i32::from(samples.p0)
                + 2 * i32::from(samples.q0)
                + 2 * i32::from(samples.q1)
                + i32::from(samples.q2)
                + 4)
                >> 3,
        );
        filtered.q1 = clip_luma_sample(
            (i32::from(samples.p0)
                + i32::from(samples.q0)
                + i32::from(samples.q1)
                + i32::from(samples.q2)
                + 2)
                >> 2,
        );
        filtered.q2 = clip_luma_sample(
            (2 * i32::from(samples.q3)
                + 3 * i32::from(samples.q2)
                + i32::from(samples.q1)
                + i32::from(samples.q0)
                + i32::from(samples.p0)
                + 4)
                >> 3,
        );
    } else {
        filtered.q0 = clip_luma_sample(
            (2 * i32::from(samples.q1) + i32::from(samples.q0) + i32::from(samples.p1) + 2) >> 2,
        );
    }
    filtered
}

pub fn should_filter_luma_edge(
    p0: u8,
    q0: u8,
    p1: u8,
    q1: u8,
    boundary_strength: u8,
    thresholds: LumaDeblockingThresholds,
) -> io::Result<bool> {
    if boundary_strength > 4 {
        return Err(invalid(
            "luma deblocking boundary strength is outside [0,4]",
        ));
    }
    if boundary_strength == 0 {
        return Ok(false);
    }
    Ok(p0.abs_diff(q0) < thresholds.alpha
        && p1.abs_diff(p0) < thresholds.beta
        && q1.abs_diff(q0) < thresholds.beta)
}

pub fn apply_luma_deblocking_edge(
    samples: LumaStrongEdgeSamples,
    boundary_strength: u8,
    parameters: LumaDeblockingParameters,
    slice_boundary: bool,
) -> io::Result<LumaEdgeSamples> {
    if parameters.index_a > 51 || parameters.index_b > 51 {
        return Err(invalid("luma deblocking edge inputs are invalid"));
    }
    if boundary_strength > 4 {
        return Err(invalid(
            "luma deblocking boundary strength is outside [0,4]",
        ));
    }
    let unchanged = LumaEdgeSamples {
        p0: samples.p0,
        p1: samples.p1,
        p2: samples.p2,
        q0: samples.q0,
        q1: samples.q1,
        q2: samples.q2,
    };
    if parameters.mode == LumaDeblockingMode::Disabled
        || (parameters.mode == LumaDeblockingMode::AllExceptSliceBoundaries && slice_boundary)
        || boundary_strength == 0
    {
        return Ok(unchanged);
    }

    let thresholds = lookup_luma_deblocking_thresholds(parameters.index_a, parameters.index_b)?;
    if !should_filter_luma_edge(
        samples.p0,
        samples.q0,
        samples.p1,
        samples.q1,
        boundary_strength,
        thresholds,
    )? {
        return Ok(unchanged);
    }
    if boundary_strength == 4 {
        return Ok(filter_luma_strong_edge(
            samples,
            thresholds.alpha,
            thresholds.beta,
        ));
    }
    let tc0 = lookup_luma_tc0(parameters.index_a, boundary_strength)?;
    Ok(filter_luma_weak_edge(unchanged, thresholds.beta, tc0))
}

pub fn apply_luma_deblocking_edge_segment(
    samples: [LumaStrongEdgeSamples; 4],
    boundary_strength: u8,
    parameters: LumaDeblockingParameters,
    slice_boundary: bool,
) -> io::Result<[LumaEdgeSamples; 4]> {
    let first =
        apply_luma_deblocking_edge(samples[0], boundary_strength, parameters, slice_boundary)?;
    let mut filtered = [first; 4];
    for (index, edge_samples) in samples.iter().enumerate().skip(1) {
        filtered[index] = apply_luma_deblocking_edge(
            *edge_samples,
            boundary_strength,
            parameters,
            slice_boundary,
        )?;
    }
    Ok(filtered)
}

/// Filters four adjacent plane samples; `(x, y)` is the first `q0` location.
pub fn apply_luma_deblocking_plane_edge_segment(
    plane: &mut [u8],
    width: usize,
    height: usize,
    stride: usize,
    x: usize,
    y: usize,
    vertical: bool,
    boundary_strength: u8,
    parameters: LumaDeblockingParameters,
    slice_boundary: bool,
) -> io::Result<()> {
    let edge_is_out_of_bounds = if vertical {
        width < 8
            || height < 4
            || x < 4
            || x > width.saturating_sub(4)
            || y > height.saturating_sub(4)
    } else {
        width < 4
            || height < 8
            || x > width.saturating_sub(4)
            || y < 4
            || y > height.saturating_sub(4)
    };
    if !valid_yuv_plane(plane, width, height, stride) || edge_is_out_of_bounds {
        return Err(invalid(
            "luma deblocking plane layout or edge coordinates are invalid",
        ));
    }

    let empty = LumaStrongEdgeSamples {
        p0: 0,
        p1: 0,
        p2: 0,
        p3: 0,
        q0: 0,
        q1: 0,
        q2: 0,
        q3: 0,
    };
    let mut samples = [empty; 4];
    for (lane, edge_samples) in samples.iter_mut().enumerate() {
        let q0_index = if vertical {
            (y + lane) * stride + x
        } else {
            y * stride + x + lane
        };
        *edge_samples = if vertical {
            LumaStrongEdgeSamples {
                p0: plane[q0_index - 1],
                p1: plane[q0_index - 2],
                p2: plane[q0_index - 3],
                p3: plane[q0_index - 4],
                q0: plane[q0_index],
                q1: plane[q0_index + 1],
                q2: plane[q0_index + 2],
                q3: plane[q0_index + 3],
            }
        } else {
            LumaStrongEdgeSamples {
                p0: plane[q0_index - stride],
                p1: plane[q0_index - 2 * stride],
                p2: plane[q0_index - 3 * stride],
                p3: plane[q0_index - 4 * stride],
                q0: plane[q0_index],
                q1: plane[q0_index + stride],
                q2: plane[q0_index + 2 * stride],
                q3: plane[q0_index + 3 * stride],
            }
        };
    }

    let filtered =
        apply_luma_deblocking_edge_segment(samples, boundary_strength, parameters, slice_boundary)?;
    for (lane, result) in filtered.iter().enumerate() {
        let q0_index = if vertical {
            (y + lane) * stride + x
        } else {
            y * stride + x + lane
        };
        if vertical {
            plane[q0_index - 1] = result.p0;
            plane[q0_index - 2] = result.p1;
            plane[q0_index - 3] = result.p2;
            plane[q0_index] = result.q0;
            plane[q0_index + 1] = result.q1;
            plane[q0_index + 2] = result.q2;
        } else {
            plane[q0_index - stride] = result.p0;
            plane[q0_index - 2 * stride] = result.p1;
            plane[q0_index - 3 * stride] = result.p2;
            plane[q0_index] = result.q0;
            plane[q0_index + stride] = result.q1;
            plane[q0_index + 2 * stride] = result.q2;
        }
    }
    Ok(())
}

/// Filters a 16-sample macroblock edge as four ordered 4-sample segments.
pub fn apply_luma_deblocking_plane_macroblock_edge(
    plane: &mut [u8],
    width: usize,
    height: usize,
    stride: usize,
    x: usize,
    y: usize,
    vertical: bool,
    boundary_strengths: [u8; 4],
    parameters: LumaDeblockingParameters,
    slice_boundary: bool,
) -> io::Result<()> {
    let edge_is_out_of_bounds = if vertical {
        width < 8
            || height < 16
            || x < 4
            || x > width.saturating_sub(4)
            || y > height.saturating_sub(16)
    } else {
        width < 16
            || height < 8
            || x > width.saturating_sub(16)
            || y < 4
            || y > height.saturating_sub(4)
    };
    if !valid_yuv_plane(plane, width, height, stride) || edge_is_out_of_bounds {
        return Err(invalid(
            "luma deblocking plane layout or edge coordinates are invalid",
        ));
    }
    if parameters.index_a > 51 || parameters.index_b > 51 {
        return Err(invalid("luma deblocking edge inputs are invalid"));
    }
    if boundary_strengths.iter().any(|strength| *strength > 4) {
        return Err(invalid(
            "luma deblocking boundary strength is outside [0,4]",
        ));
    }

    for (segment, boundary_strength) in boundary_strengths.into_iter().enumerate() {
        let segment_x = if vertical { x } else { x + 4 * segment };
        let segment_y = if vertical { y + 4 * segment } else { y };
        apply_luma_deblocking_plane_edge_segment(
            plane,
            width,
            height,
            stride,
            segment_x,
            segment_y,
            vertical,
            boundary_strength,
            parameters,
            slice_boundary,
        )?;
    }
    Ok(())
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct LumaDeblockingMacroblock {
    pub x: usize,
    pub y: usize,
    pub left_neighbor_available: bool,
    pub top_neighbor_available: bool,
    pub transform_size_8x8: bool,
    pub left_slice_boundary: bool,
    pub top_slice_boundary: bool,
    pub left_strengths: [u8; 4],
    pub top_strengths: [u8; 4],
    pub vertical_internal_strengths: [[u8; 4]; 3],
    pub horizontal_internal_strengths: [[u8; 4]; 3],
}

#[derive(Debug, Clone, Copy, Default, PartialEq, Eq)]
pub struct LumaDeblockingEdgeFlags {
    pub filter_left: bool,
    pub filter_top: bool,
    pub filter_internal: bool,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct LumaDeblockingNeighbors {
    pub left_index: Option<usize>,
    pub top_index: Option<usize>,
    pub left_available: bool,
    pub top_available: bool,
    pub left_slice_boundary: bool,
    pub top_slice_boundary: bool,
}

pub fn derive_luma_deblocking_neighbors(
    macroblock_index: usize,
    picture_width_in_macroblocks: usize,
    picture_height_in_macroblocks: usize,
    slice_ids: &[u32],
) -> io::Result<LumaDeblockingNeighbors> {
    let picture_size = picture_width_in_macroblocks
        .checked_mul(picture_height_in_macroblocks)
        .ok_or_else(|| invalid("luma deblocking macroblock address or slice map is invalid"))?;
    if picture_width_in_macroblocks == 0
        || picture_height_in_macroblocks == 0
        || macroblock_index >= picture_size
        || slice_ids.len() != picture_size
    {
        return Err(invalid(
            "luma deblocking macroblock address or slice map is invalid",
        ));
    }
    let left_index =
        (macroblock_index % picture_width_in_macroblocks != 0).then(|| macroblock_index - 1);
    let top_index = (macroblock_index >= picture_width_in_macroblocks)
        .then(|| macroblock_index - picture_width_in_macroblocks);
    Ok(LumaDeblockingNeighbors {
        left_index,
        top_index,
        left_available: left_index.is_some(),
        top_available: top_index.is_some(),
        left_slice_boundary: left_index
            .is_some_and(|index| slice_ids[index] != slice_ids[macroblock_index]),
        top_slice_boundary: top_index
            .is_some_and(|index| slice_ids[index] != slice_ids[macroblock_index]),
    })
}

pub fn resolve_luma_deblocking_macroblock(
    macroblock_index: usize,
    picture_width_in_macroblocks: usize,
    picture_height_in_macroblocks: usize,
    slice_ids: &[u32],
    mut macroblock: LumaDeblockingMacroblock,
) -> io::Result<LumaDeblockingMacroblock> {
    let neighbors = derive_luma_deblocking_neighbors(
        macroblock_index,
        picture_width_in_macroblocks,
        picture_height_in_macroblocks,
        slice_ids,
    )?;
    macroblock.x = (macroblock_index % picture_width_in_macroblocks)
        .checked_mul(16)
        .ok_or_else(|| invalid("luma deblocking macroblock address or slice map is invalid"))?;
    macroblock.y = (macroblock_index / picture_width_in_macroblocks)
        .checked_mul(16)
        .ok_or_else(|| invalid("luma deblocking macroblock address or slice map is invalid"))?;
    macroblock.left_neighbor_available = neighbors.left_available;
    macroblock.top_neighbor_available = neighbors.top_available;
    macroblock.left_slice_boundary = neighbors.left_slice_boundary;
    macroblock.top_slice_boundary = neighbors.top_slice_boundary;
    Ok(macroblock)
}

pub fn derive_luma_deblocking_edge_flags(
    mode: LumaDeblockingMode,
    left_neighbor_available: bool,
    top_neighbor_available: bool,
    left_slice_boundary: bool,
    top_slice_boundary: bool,
) -> LumaDeblockingEdgeFlags {
    if mode == LumaDeblockingMode::Disabled {
        return LumaDeblockingEdgeFlags::default();
    }
    let suppress_slice_boundaries = mode == LumaDeblockingMode::AllExceptSliceBoundaries;
    LumaDeblockingEdgeFlags {
        filter_left: left_neighbor_available && !(suppress_slice_boundaries && left_slice_boundary),
        filter_top: top_neighbor_available && !(suppress_slice_boundaries && top_slice_boundary),
        filter_internal: true,
    }
}

pub fn apply_luma_deblocking_macroblock(
    plane: &mut [u8],
    width: usize,
    height: usize,
    stride: usize,
    macroblock: LumaDeblockingMacroblock,
    parameters: LumaDeblockingParameters,
) -> io::Result<()> {
    let edge_flags = derive_luma_deblocking_edge_flags(
        parameters.mode,
        macroblock.left_neighbor_available,
        macroblock.top_neighbor_available,
        macroblock.left_slice_boundary,
        macroblock.top_slice_boundary,
    );
    if !valid_yuv_plane(plane, width, height, stride)
        || width < 16
        || height < 16
        || macroblock.x > width - 16
        || macroblock.y > height - 16
        || (edge_flags.filter_left && macroblock.x < 4)
        || (edge_flags.filter_top && macroblock.y < 4)
    {
        return Err(invalid(
            "luma deblocking plane layout or edge coordinates are invalid",
        ));
    }
    if parameters.index_a > 51 || parameters.index_b > 51 {
        return Err(invalid("luma deblocking parameters are invalid"));
    }
    let strengths_valid = |strengths: &[u8; 4]| strengths.iter().all(|strength| *strength <= 4);
    if (edge_flags.filter_left && !strengths_valid(&macroblock.left_strengths))
        || (edge_flags.filter_top && !strengths_valid(&macroblock.top_strengths))
        || (edge_flags.filter_internal
            && (macroblock
                .vertical_internal_strengths
                .iter()
                .any(|strengths| !strengths_valid(strengths))
                || macroblock
                    .horizontal_internal_strengths
                    .iter()
                    .any(|strengths| !strengths_valid(strengths))))
    {
        return Err(invalid(
            "luma deblocking boundary strength is outside [0,4]",
        ));
    }

    if parameters.mode == LumaDeblockingMode::Disabled {
        return Ok(());
    }
    if edge_flags.filter_left {
        apply_luma_deblocking_plane_macroblock_edge(
            plane,
            width,
            height,
            stride,
            macroblock.x,
            macroblock.y,
            true,
            macroblock.left_strengths,
            parameters,
            macroblock.left_slice_boundary,
        )?;
    }
    if edge_flags.filter_internal {
        for edge in 0..3 {
            if macroblock.transform_size_8x8 && edge != 1 {
                continue;
            }
            apply_luma_deblocking_plane_macroblock_edge(
                plane,
                width,
                height,
                stride,
                macroblock.x + 4 * (edge + 1),
                macroblock.y,
                true,
                macroblock.vertical_internal_strengths[edge],
                parameters,
                false,
            )?;
        }
    }
    if edge_flags.filter_top {
        apply_luma_deblocking_plane_macroblock_edge(
            plane,
            width,
            height,
            stride,
            macroblock.x,
            macroblock.y,
            false,
            macroblock.top_strengths,
            parameters,
            macroblock.top_slice_boundary,
        )?;
    }
    if edge_flags.filter_internal {
        for edge in 0..3 {
            if macroblock.transform_size_8x8 && edge != 1 {
                continue;
            }
            apply_luma_deblocking_plane_macroblock_edge(
                plane,
                width,
                height,
                stride,
                macroblock.x,
                macroblock.y + 4 * (edge + 1),
                false,
                macroblock.horizontal_internal_strengths[edge],
                parameters,
                false,
            )?;
        }
    }
    Ok(())
}

pub fn derive_luma_boundary_strength(
    macroblock_edge: bool,
    either_intra: bool,
    either_has_nonzero_coefficients: bool,
    inter_prediction_differs: bool,
) -> u8 {
    if either_intra {
        return if macroblock_edge { 4 } else { 3 };
    }
    if either_has_nonzero_coefficients {
        return 2;
    }
    if inter_prediction_differs {
        return 1;
    }
    0
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct LumaPredictionVector {
    pub reference_picture_id: u32,
    pub motion_vector: MotionVector,
}

/// Compares up to two reference/MV pairs per side, ignoring prediction-list order.
pub fn luma_inter_prediction_differs(
    prediction_p: &[LumaPredictionVector],
    prediction_q: &[LumaPredictionVector],
) -> io::Result<bool> {
    if prediction_p.len() > 2 || prediction_q.len() > 2 {
        return Err(invalid(
            "luma inter-prediction supports at most two vectors",
        ));
    }
    if prediction_p.len() != prediction_q.len() {
        return Ok(true);
    }
    if prediction_p.is_empty() {
        return Ok(false);
    }
    if prediction_p.len() == 1 {
        return Ok(!luma_prediction_vectors_match(
            prediction_p[0],
            prediction_q[0],
        ));
    }
    let direct_match = luma_prediction_vectors_match(prediction_p[0], prediction_q[0])
        && luma_prediction_vectors_match(prediction_p[1], prediction_q[1]);
    let swapped_match = luma_prediction_vectors_match(prediction_p[0], prediction_q[1])
        && luma_prediction_vectors_match(prediction_p[1], prediction_q[0]);
    Ok(!direct_match && !swapped_match)
}

pub fn derive_luma_boundary_strength_from_predictions(
    macroblock_edge: bool,
    either_intra: bool,
    either_has_nonzero_coefficients: bool,
    prediction_p: &[LumaPredictionVector],
    prediction_q: &[LumaPredictionVector],
) -> io::Result<u8> {
    if either_intra || either_has_nonzero_coefficients {
        return Ok(derive_luma_boundary_strength(
            macroblock_edge,
            either_intra,
            either_has_nonzero_coefficients,
            false,
        ));
    }
    let inter_prediction_differs = luma_inter_prediction_differs(prediction_p, prediction_q)?;
    Ok(derive_luma_boundary_strength(
        macroblock_edge,
        false,
        false,
        inter_prediction_differs,
    ))
}

fn luma_prediction_vectors_match(p: LumaPredictionVector, q: LumaPredictionVector) -> bool {
    p.reference_picture_id == q.reference_picture_id
        && (i64::from(p.motion_vector.x) - i64::from(q.motion_vector.x)).abs() < 4
        && (i64::from(p.motion_vector.y) - i64::from(q.motion_vector.y)).abs() < 4
}

const LUMA_ALPHA_TABLE: [u8; 52] = [
    0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 4, 4, 5, 6, 7, 8, 9, 10, 12, 13, 15, 17, 20,
    22, 25, 28, 32, 36, 40, 45, 50, 56, 63, 71, 80, 90, 101, 113, 127, 144, 162, 182, 203, 226,
    255, 255,
];

const LUMA_BETA_TABLE: [u8; 52] = [
    0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 2, 2, 2, 3, 3, 3, 3, 4, 4, 4, 6, 6, 7, 7,
    8, 8, 9, 9, 10, 10, 11, 11, 12, 12, 13, 13, 14, 14, 15, 15, 16, 16, 17, 18,
];

const LUMA_TC0_TABLE: [[u8; 52]; 3] = [
    [
        0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 1, 1, 1, 1, 1,
        1, 1, 1, 2, 2, 2, 2, 3, 3, 3, 4, 4, 4, 5, 6, 6, 7, 8, 9, 10, 11, 13,
    ],
    [
        0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 1, 1, 1, 1, 1, 1, 1,
        1, 2, 2, 2, 2, 3, 3, 3, 4, 4, 5, 5, 6, 7, 8, 8, 10, 11, 12, 13, 15, 17,
    ],
    [
        0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 1, 1, 1, 1, 1, 1, 1, 2, 2, 2,
        2, 3, 3, 3, 4, 4, 4, 5, 6, 6, 7, 8, 9, 10, 11, 13, 14, 16, 18, 20, 23, 25,
    ],
];

pub fn lookup_luma_tc0(index_a: u8, boundary_strength: u8) -> io::Result<u8> {
    if index_a > 51 || !(1..=3).contains(&boundary_strength) {
        return Err(invalid("luma tC0 table index is invalid"));
    }
    Ok(LUMA_TC0_TABLE[usize::from(boundary_strength - 1)][usize::from(index_a)])
}

pub fn lookup_luma_deblocking_thresholds(
    index_a: u8,
    index_b: u8,
) -> io::Result<LumaDeblockingThresholds> {
    if index_a > 51 || index_b > 51 {
        return Err(invalid("luma deblocking table index is outside [0,51]"));
    }
    Ok(LumaDeblockingThresholds {
        alpha: LUMA_ALPHA_TABLE[usize::from(index_a)],
        beta: LUMA_BETA_TABLE[usize::from(index_b)],
    })
}

pub fn derive_luma_deblocking_parameters(
    qp_p: i32,
    qp_q: i32,
    disable_deblocking_filter_idc: u8,
    slice_alpha_c0_offset_div2: i64,
    slice_beta_offset_div2: i64,
) -> io::Result<LumaDeblockingParameters> {
    if !(0..=51).contains(&qp_p)
        || !(0..=51).contains(&qp_q)
        || disable_deblocking_filter_idc > 2
        || !(-6..=6).contains(&slice_alpha_c0_offset_div2)
        || !(-6..=6).contains(&slice_beta_offset_div2)
    {
        return Err(invalid("luma deblocking parameters are invalid"));
    }
    let mode = match disable_deblocking_filter_idc {
        0 => LumaDeblockingMode::AllEdges,
        1 => LumaDeblockingMode::Disabled,
        2 => LumaDeblockingMode::AllExceptSliceBoundaries,
        _ => unreachable!(),
    };
    let qp_average = (qp_p + qp_q + 1) >> 1;
    let index_a = (qp_average as i64 + 2 * slice_alpha_c0_offset_div2).clamp(0, 51) as u8;
    let index_b = (qp_average as i64 + 2 * slice_beta_offset_div2).clamp(0, 51) as u8;
    Ok(LumaDeblockingParameters {
        mode,
        index_a,
        index_b,
    })
}

impl PresentationOrderBuffer {
    pub fn new(max_reorder_pictures: usize) -> Self {
        Self {
            max_reorder_pictures,
            pending: Vec::new(),
        }
    }

    pub fn push(
        &mut self,
        picture: &PresentationPicture,
    ) -> io::Result<Option<PresentationPicture>> {
        let chroma_width = picture.frame.width / 2 + picture.frame.width % 2;
        let chroma_height = picture.frame.height / 2 + picture.frame.height % 2;
        if !valid_yuv_plane(
            &picture.frame.y,
            picture.frame.width,
            picture.frame.height,
            picture.frame.y_stride,
        ) || !valid_yuv_plane(
            &picture.frame.u,
            chroma_width,
            chroma_height,
            picture.frame.u_stride,
        ) || !valid_yuv_plane(
            &picture.frame.v,
            chroma_width,
            chroma_height,
            picture.frame.v_stride,
        ) {
            return Err(invalid(
                "reference picture frame layout is invalid or truncated",
            ));
        }
        self.pending.push(picture.clone());
        self.pending.sort_by_key(|item| item.picture_order_cnt);
        if self.pending.len() <= self.max_reorder_pictures {
            return Ok(None);
        }
        Ok(Some(self.pending.remove(0)))
    }

    pub fn drain(&mut self) -> Vec<PresentationPicture> {
        self.pending.sort_by_key(|item| item.picture_order_cnt);
        std::mem::take(&mut self.pending)
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct DecodedReferencePicture {
    pub reference: ReferencePicture,
    pub frame: Yuv420Frame,
}

#[derive(Debug, Clone, Default)]
pub struct ReferencePictureBuffer {
    pictures: Vec<DecodedReferencePicture>,
}

impl ReferencePictureBuffer {
    pub fn store(&mut self, reference: ReferencePicture, frame: &Yuv420Frame) -> io::Result<()> {
        let chroma_width = frame.width / 2 + frame.width % 2;
        let chroma_height = frame.height / 2 + frame.height % 2;
        if !valid_yuv_plane(&frame.y, frame.width, frame.height, frame.y_stride)
            || !valid_yuv_plane(&frame.u, chroma_width, chroma_height, frame.u_stride)
            || !valid_yuv_plane(&frame.v, chroma_width, chroma_height, frame.v_stride)
        {
            return Err(io::Error::new(
                ErrorKind::InvalidInput,
                "reference picture frame layout is invalid or truncated",
            ));
        }
        let stored = DecodedReferencePicture {
            reference,
            frame: frame.clone(),
        };
        if let Some(existing) = self
            .pictures
            .iter_mut()
            .find(|picture| picture.reference.identifier == reference.identifier)
        {
            *existing = stored;
        } else {
            self.pictures.push(stored);
        }
        Ok(())
    }

    pub fn get(&self, identifier: u32) -> Option<DecodedReferencePicture> {
        self.pictures
            .iter()
            .find(|picture| picture.reference.identifier == identifier)
            .cloned()
    }

    pub fn remove(&mut self, identifier: u32) -> bool {
        if let Some(index) = self
            .pictures
            .iter()
            .position(|picture| picture.reference.identifier == identifier)
        {
            self.pictures.remove(index);
            true
        } else {
            false
        }
    }

    pub fn references(&self) -> Vec<ReferencePicture> {
        self.pictures
            .iter()
            .map(|picture| picture.reference)
            .collect()
    }
}

fn valid_yuv_plane(plane: &[u8], width: usize, height: usize, stride: usize) -> bool {
    if width == 0 || height == 0 || stride < width {
        return false;
    }
    (height - 1)
        .checked_mul(stride)
        .and_then(|length| length.checked_add(width))
        .is_some_and(|required| plane.len() >= required)
}

/// Applies the horizontal six-tap filter to six 8-bit luma samples.
pub fn interpolate_luma_half_sample_horizontal(samples: &[u8; 6]) -> u8 {
    interpolate_luma_half_sample(samples)
}

/// Applies the vertical six-tap filter to six 8-bit luma samples.
pub fn interpolate_luma_half_sample_vertical(samples: &[u8; 6]) -> u8 {
    interpolate_luma_half_sample(samples)
}

fn interpolate_luma_half_sample(samples: &[u8; 6]) -> u8 {
    let taps = (*samples).map(i32::from);
    clip_luma_sample((filter_luma_six_tap(taps) + 16) >> 5)
}

/// Applies the unrounded, two-pass six-tap filter at diagonal half-sample position j.
pub fn interpolate_luma_half_sample_diagonal(samples: &[[u8; 6]; 6]) -> u8 {
    let mut vertical = [0_i32; 6];
    for column in 0..6 {
        let mut taps = [0_i32; 6];
        for row in 0..6 {
            taps[row] = i32::from(samples[row][column]);
        }
        vertical[column] = filter_luma_six_tap(taps);
    }
    clip_luma_sample((filter_luma_six_tap(vertical) + 512) >> 10)
}

/// Averages two 8-bit luma samples with upward rounding for quarter-sample positions.
pub fn interpolate_luma_quarter_sample_average(first: u8, second: u8) -> u8 {
    ((u16::from(first) + u16::from(second) + 1) >> 1) as u8
}

/// Derives the quarter-sample positions on either side of a half-sample.
pub fn interpolate_luma_quarter_sample_pair(first: u8, half: u8, second: u8) -> [u8; 2] {
    [
        interpolate_luma_quarter_sample_average(first, half),
        interpolate_luma_quarter_sample_average(half, second),
    ]
}

/// Derives diagonal quarter-sample positions e, g, p, and r from half-samples b, h, m, and s.
pub fn interpolate_luma_quarter_sample_diagonal(b: u8, h: u8, m: u8, s: u8) -> [u8; 4] {
    [
        interpolate_luma_quarter_sample_average(b, h),
        interpolate_luma_quarter_sample_average(b, m),
        interpolate_luma_quarter_sample_average(h, s),
        interpolate_luma_quarter_sample_average(m, s),
    ]
}

/// Derives axial quarter-sample positions a, c, d, and n around an integer sample.
pub fn interpolate_luma_quarter_sample_axial(
    center: u8,
    right: u8,
    lower: u8,
    horizontal_half: u8,
    vertical_half: u8,
) -> [u8; 4] {
    [
        interpolate_luma_quarter_sample_average(center, horizontal_half),
        interpolate_luma_quarter_sample_average(right, horizontal_half),
        interpolate_luma_quarter_sample_average(center, vertical_half),
        interpolate_luma_quarter_sample_average(lower, vertical_half),
    ]
}

/// Derives quarter-sample positions f, i, k, and q around diagonal half-sample j.
pub fn interpolate_luma_quarter_sample_around_j(b: u8, h: u8, j: u8, m: u8, s: u8) -> [u8; 4] {
    [
        interpolate_luma_quarter_sample_average(b, j),
        interpolate_luma_quarter_sample_average(h, j),
        interpolate_luma_quarter_sample_average(j, m),
        interpolate_luma_quarter_sample_average(j, s),
    ]
}

/// Selects a sample from a grid indexed by xFracL then yFracL, as in Table 8-12.
pub fn select_luma_fractional_sample(
    samples: &[[u8; 4]; 4],
    x_frac_l: u8,
    y_frac_l: u8,
) -> io::Result<u8> {
    if x_frac_l > 3 || y_frac_l > 3 {
        return Err(io::Error::new(
            ErrorKind::InvalidInput,
            "luma fractional-sample offset is outside [0,3]",
        ));
    }
    Ok(samples[usize::from(x_frac_l)][usize::from(y_frac_l)])
}

/// Builds the Table 8-12 quarter-sample grid indexed by xFracL then yFracL.
pub fn interpolate_luma_quarter_sample_grid(samples: &[[u8; 6]; 6]) -> [[u8; 4]; 4] {
    let j = interpolate_luma_half_sample_diagonal(samples);
    let b = interpolate_luma_half_sample_horizontal(&samples[2]);
    let center_column = std::array::from_fn(|row| samples[row][2]);
    let right_column = std::array::from_fn(|row| samples[row][3]);
    let h = interpolate_luma_half_sample_vertical(&center_column);
    let m = interpolate_luma_half_sample_vertical(&right_column);
    let s = interpolate_luma_half_sample_horizontal(&samples[3]);

    let axial =
        interpolate_luma_quarter_sample_axial(samples[2][2], samples[2][3], samples[3][2], b, h);
    let diagonal = interpolate_luma_quarter_sample_diagonal(b, h, m, s);
    let around_j = interpolate_luma_quarter_sample_around_j(b, h, j, m, s);
    [
        [samples[2][2], axial[2], h, axial[3]],
        [axial[0], diagonal[0], around_j[1], diagonal[2]],
        [b, around_j[0], j, around_j[3]],
        [axial[1], diagonal[1], around_j[2], diagonal[3]],
    ]
}

/// Gathers the 6x6 integer luma neighborhood, clipping each coordinate to the reference plane.
pub fn gather_luma_quarter_sample_neighborhood(
    plane: &[u8],
    width: usize,
    height: usize,
    stride: usize,
    x_int: isize,
    y_int: isize,
) -> io::Result<[[u8; 6]; 6]> {
    let invalid_layout = || {
        io::Error::new(
            ErrorKind::InvalidInput,
            "luma reference plane layout is invalid or truncated",
        )
    };
    if width == 0
        || height == 0
        || stride < width
        || width > isize::MAX as usize
        || height > isize::MAX as usize
    {
        return Err(invalid_layout());
    }
    let required_length = (height - 1)
        .checked_mul(stride)
        .and_then(|length| length.checked_add(width))
        .ok_or_else(invalid_layout)?;
    if plane.len() < required_length {
        return Err(invalid_layout());
    }

    let mut samples = [[0_u8; 6]; 6];
    for (row, sample_row) in samples.iter_mut().enumerate() {
        let y = clip_luma_reference_coordinate(y_int, row as isize - 2, height);
        for (column, sample) in sample_row.iter_mut().enumerate() {
            let x = clip_luma_reference_coordinate(x_int, column as isize - 2, width);
            *sample = plane[y * stride + x];
        }
    }
    Ok(samples)
}

/// Interpolates and selects one luma sample from a reference plane.
pub fn interpolate_luma_fractional_sample(
    plane: &[u8],
    width: usize,
    height: usize,
    stride: usize,
    x_int: isize,
    y_int: isize,
    x_frac_l: u8,
    y_frac_l: u8,
) -> io::Result<u8> {
    let neighborhood =
        gather_luma_quarter_sample_neighborhood(plane, width, height, stride, x_int, y_int)?;
    let grid = interpolate_luma_quarter_sample_grid(&neighborhood);
    select_luma_fractional_sample(&grid, x_frac_l, y_frac_l)
}

fn clip_luma_reference_coordinate(origin: isize, offset: isize, limit: usize) -> usize {
    origin.saturating_add(offset).clamp(0, (limit - 1) as isize) as usize
}

fn filter_luma_six_tap(samples: [i32; 6]) -> i32 {
    samples[0] - 5 * samples[1] + 20 * samples[2] + 20 * samples[3] - 5 * samples[4] + samples[5]
}

fn clip_luma_sample(value: i32) -> u8 {
    value.clamp(0, 255) as u8
}

/// Builds initial progressive-frame B lists from POC order and long-term indices.
pub fn build_b_reference_lists(
    pictures: &[ReferencePicture],
    current_picture_order_cnt: i64,
) -> (Vec<ReferencePicture>, Vec<ReferencePicture>) {
    let mut before_or_equal = Vec::new();
    let mut after = Vec::new();
    let mut long_term = Vec::new();
    for picture in pictures {
        if picture.long_term_frame_idx.is_some() {
            long_term.push(*picture);
        } else if picture.picture_order_cnt <= current_picture_order_cnt {
            before_or_equal.push(*picture);
        } else {
            after.push(*picture);
        }
    }
    before_or_equal.sort_by(|left, right| right.picture_order_cnt.cmp(&left.picture_order_cnt));
    after.sort_by(|left, right| left.picture_order_cnt.cmp(&right.picture_order_cnt));
    long_term.sort_by_key(|picture| picture.long_term_frame_idx);

    let mut list0 = before_or_equal.clone();
    list0.extend_from_slice(&after);
    list0.extend_from_slice(&long_term);
    let mut list1 = after;
    list1.extend_from_slice(&before_or_equal);
    list1.extend_from_slice(&long_term);
    if list0 == list1 && list1.len() > 1 {
        list1.swap(0, 1);
    }
    (list0, list1)
}

/// Applies parsed frame-coded short- and long-term reference-list commands.
pub fn apply_reference_list_modifications(
    initial: &[ReferencePicture],
    references: &[ReferencePicture],
    current_frame_num: u32,
    maximum_frame_num: u32,
    modifications: &[RefPicListModification],
) -> io::Result<Vec<ReferencePicture>> {
    if maximum_frame_num == 0 || current_frame_num >= maximum_frame_num {
        return Err(io::Error::new(
            std::io::ErrorKind::InvalidInput,
            "current frame number must be within a positive MaxFrameNum",
        ));
    }
    let active_count = initial.len();
    let mut result = initial.to_vec();
    let mut pic_num_pred = u64::from(current_frame_num);
    let max_pic_num = u64::from(maximum_frame_num);
    let mut ref_index = 0;
    for modification in modifications {
        if ref_index >= active_count {
            return Err(io::Error::new(
                std::io::ErrorKind::InvalidInput,
                "reference-list modification exceeds the active reference count",
            ));
        }
        let target = match modification.modification_of_pic_nums_idc {
            0 | 1 => {
                let abs_diff_pic_num = u64::from(modification.value) + 1;
                if abs_diff_pic_num > max_pic_num {
                    return Err(io::Error::new(
                        std::io::ErrorKind::InvalidInput,
                        "absolute picture-number difference exceeds MaxPicNum",
                    ));
                }
                pic_num_pred = if modification.modification_of_pic_nums_idc == 0 {
                    (pic_num_pred + max_pic_num - abs_diff_pic_num) % max_pic_num
                } else {
                    (pic_num_pred + abs_diff_pic_num) % max_pic_num
                };
                let mut frame_num_wrap = i64::try_from(pic_num_pred).map_err(|_| {
                    io::Error::new(std::io::ErrorKind::InvalidInput, "PicNum exceeds i64")
                })?;
                if pic_num_pred > u64::from(current_frame_num) {
                    frame_num_wrap -= i64::from(maximum_frame_num);
                }
                references.iter().find(|picture| {
                    if picture.long_term_frame_idx.is_some()
                        || picture.frame_num >= maximum_frame_num
                    {
                        return false;
                    }
                    let candidate_wrap = if picture.frame_num > current_frame_num {
                        i64::from(picture.frame_num) - i64::from(maximum_frame_num)
                    } else {
                        i64::from(picture.frame_num)
                    };
                    candidate_wrap == frame_num_wrap
                })
            }
            2 => references
                .iter()
                .find(|picture| picture.long_term_frame_idx == Some(modification.value)),
            _ => {
                return Err(io::Error::new(
                    std::io::ErrorKind::InvalidInput,
                    "reference-list modification idc is invalid",
                ));
            }
        }
        .copied()
        .ok_or_else(|| {
            io::Error::new(
                std::io::ErrorKind::NotFound,
                "reference-list modification target is unavailable",
            )
        })?;

        if let Some(duplicate_index) =
            (ref_index..result.len()).find(|&index| result[index].identifier == target.identifier)
        {
            result.remove(duplicate_index);
        }
        result.insert(ref_index, target);
        result.truncate(active_count);
        ref_index += 1;
    }
    Ok(result)
}

/// Builds the initial progressive-frame P list, short-term first then long-term.
pub fn build_p_reference_list(
    pictures: &[ReferencePicture],
    current_frame_num: u32,
    maximum_frame_num: u32,
) -> io::Result<Vec<ReferencePicture>> {
    if maximum_frame_num == 0 || current_frame_num >= maximum_frame_num {
        return Err(io::Error::new(
            std::io::ErrorKind::InvalidInput,
            "current frame number must be within a positive MaxFrameNum",
        ));
    }
    let mut short_term = Vec::new();
    let mut long_term = Vec::new();
    for picture in pictures {
        if picture.long_term_frame_idx.is_some() {
            long_term.push(*picture);
            continue;
        }
        if picture.frame_num >= maximum_frame_num {
            return Err(io::Error::new(
                std::io::ErrorKind::InvalidInput,
                "short-term frame number is outside MaxFrameNum",
            ));
        }
        let mut frame_num_wrap = i64::from(picture.frame_num);
        if picture.frame_num > current_frame_num {
            frame_num_wrap -= i64::from(maximum_frame_num);
        }
        short_term.push((frame_num_wrap, *picture));
    }
    short_term.sort_by(|left, right| right.0.cmp(&left.0));
    long_term.sort_by_key(|picture| picture.long_term_frame_idx);
    Ok(short_term
        .into_iter()
        .map(|(_, picture)| picture)
        .chain(long_term)
        .collect())
}

#[derive(Debug, Clone, Copy, Default, PartialEq, Eq)]
pub struct MotionVector {
    pub x: i32,
    pub y: i32,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct MotionVectorCandidate {
    pub reference_index: Option<i32>,
    pub vector: MotionVector,
}

/// Sums absolute left/top MVD components, treating absent neighbors as zero.
pub fn motion_vector_difference_neighbor_magnitudes(
    left: Option<MotionVector>,
    top: Option<MotionVector>,
) -> (u64, u64) {
    let left = left.unwrap_or_default();
    let top = top.unwrap_or_default();
    (
        u64::from(left.x.unsigned_abs()) + u64::from(top.x.unsigned_abs()),
        u64::from(left.y.unsigned_abs()) + u64::from(top.y.unsigned_abs()),
    )
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum MotionVectorPartitionShape {
    Other,
    SixteenByEight,
    EightBySixteen,
}

/// Applies H.264's partition-specific reference matches before the median rule.
pub fn predict_motion_vector_for_partition(
    shape: MotionVectorPartitionShape,
    current_reference_index: i32,
    left: Option<MotionVectorCandidate>,
    top: Option<MotionVectorCandidate>,
    top_right: Option<MotionVectorCandidate>,
    top_left: Option<MotionVectorCandidate>,
) -> MotionVector {
    let top_right_candidate = top_right.or(top_left);
    if current_reference_index >= 0 {
        let preferred = match shape {
            MotionVectorPartitionShape::SixteenByEight => [left, top, None],
            MotionVectorPartitionShape::EightBySixteen => [left, top_right_candidate, None],
            MotionVectorPartitionShape::Other => [None, None, None],
        };
        for candidate in preferred.into_iter().flatten() {
            if candidate.reference_index == Some(current_reference_index) {
                return candidate.vector;
            }
        }
    }
    predict_motion_vector(current_reference_index, left, top, top_right, top_left)
}

/// Selects the unique matching reference vector, or the component-wise median.
pub fn predict_motion_vector(
    current_reference_index: i32,
    left: Option<MotionVectorCandidate>,
    top: Option<MotionVectorCandidate>,
    top_right: Option<MotionVectorCandidate>,
    top_left: Option<MotionVectorCandidate>,
) -> MotionVector {
    let top_right = top_right.or(top_left);
    let candidates = [left, top, top_right];
    let vectors = candidates.map(|candidate| match candidate {
        Some(candidate) if matches!(candidate.reference_index, Some(index) if index >= 0) => {
            candidate.vector
        }
        _ => MotionVector::default(),
    });
    let mut matching_count = 0;
    let mut matching = None;
    for candidate in candidates.into_iter().flatten() {
        if current_reference_index >= 0
            && candidate.reference_index == Some(current_reference_index)
        {
            matching_count += 1;
            matching = Some(candidate.vector);
        }
    }
    if let (1, Some(vector)) = (matching_count, matching) {
        return vector;
    }

    let mut horizontal = vectors.map(|vector| vector.x);
    let mut vertical = vectors.map(|vector| vector.y);
    horizontal.sort_unstable();
    vertical.sort_unstable();
    MotionVector {
        x: horizontal[1],
        y: vertical[1],
    }
}

/// Adds a decoded motion-vector difference and wraps each component to signed 16-bit range.
pub fn apply_motion_vector_difference(
    predicted: MotionVector,
    difference: MotionVector,
) -> MotionVector {
    let wrap = |value: i64| (value + (1 << 15)).rem_euclid(1 << 16) - (1 << 15);
    MotionVector {
        x: wrap(i64::from(predicted.x) + i64::from(difference.x)) as i32,
        y: wrap(i64::from(predicted.y) + i64::from(difference.y)) as i32,
    }
}

/// Derives a partition vector from neighbor prediction and a decoded difference.
pub fn derive_motion_vector(
    shape: MotionVectorPartitionShape,
    current_reference_index: i32,
    left: Option<MotionVectorCandidate>,
    top: Option<MotionVectorCandidate>,
    top_right: Option<MotionVectorCandidate>,
    top_left: Option<MotionVectorCandidate>,
    difference: MotionVector,
) -> MotionVector {
    let predicted = predict_motion_vector_for_partition(
        shape,
        current_reference_index,
        left,
        top,
        top_right,
        top_left,
    );
    apply_motion_vector_difference(predicted, difference)
}

const INVERSE_SCALE_4X4_FACTORS: [[i64; 3]; 6] = [
    [10, 13, 16],
    [11, 14, 18],
    [13, 16, 20],
    [14, 18, 23],
    [16, 20, 25],
    [18, 23, 29],
];
const CHROMA_QPC_FROM_QPI: [i32; 22] = [
    29, 30, 31, 32, 32, 33, 34, 34, 35, 35, 36, 36, 37, 37, 37, 38, 38, 38, 38, 39, 39, 39,
];
const INVERSE_SCALE_8X8_FACTORS: [[i64; 6]; 6] = [
    [20, 18, 32, 19, 25, 24],
    [22, 19, 35, 21, 28, 26],
    [26, 23, 42, 24, 33, 31],
    [28, 25, 45, 26, 35, 33],
    [32, 28, 51, 30, 40, 38],
    [36, 32, 58, 34, 46, 43],
];
const INVERSE_SCALE_8X8_CLASSES: [usize; 64] = [
    0, 3, 4, 3, 0, 3, 4, 3, 3, 1, 5, 1, 3, 1, 5, 1, 4, 5, 2, 5, 4, 5, 2, 5, 3, 1, 5, 1, 3, 1, 5, 1,
    0, 3, 4, 3, 0, 3, 4, 3, 3, 1, 5, 1, 3, 1, 5, 1, 4, 5, 2, 5, 4, 5, 2, 5, 3, 1, 5, 1, 3, 1, 5, 1,
];

/// Applies H.264 4x4 luma inverse scaling to raster-order coefficient levels.
pub fn inverse_scale_luma4x4(
    levels: &[i32; 16],
    scaling_list: &[u8; 16],
    qpy: i32,
) -> io::Result<[i64; 16]> {
    if !(0..=51).contains(&qpy) {
        return Err(invalid("inverse scaling QPY is outside [0,51]"));
    }
    if scaling_list.contains(&0) {
        return Err(invalid("inverse scaling list contains zero"));
    }

    let mut scaled = [0_i64; 16];
    for index in 0..16 {
        let row = index / 4;
        let column = index % 4;
        let factor_class = row % 2 + column % 2;
        let value = i64::from(levels[index])
            * INVERSE_SCALE_4X4_FACTORS[(qpy % 6) as usize][factor_class]
            * i64::from(scaling_list[index]);
        scaled[index] = if qpy >= 24 {
            value << (qpy / 6 - 4)
        } else {
            let shift = 4 - qpy / 6;
            let rounding = 1_i64 << (shift - 1);
            (value + rounding) >> shift
        };
    }
    Ok(scaled)
}

/// Applies H.264 4:2:0 chroma DC inverse scaling for 8-bit QP-prime C.
pub fn inverse_scale_chroma_dc2x2(transformed: &[i64; 4], qpc: i32) -> io::Result<[i64; 4]> {
    if !(0..=39).contains(&qpc) {
        return Err(invalid("inverse scaling QPC is outside [0,39]"));
    }
    if transformed
        .iter()
        .any(|coefficient| !(-(1_i64 << 15)..=(1_i64 << 15) - 1).contains(coefficient))
    {
        return Err(invalid(
            "chroma DC values must be in the signed 16-bit range",
        ));
    }

    let factor = INVERSE_SCALE_4X4_FACTORS[(qpc % 6) as usize][0];
    let shift = qpc / 6;
    let mut scaled = [0_i64; 4];
    for (index, coefficient) in transformed.iter().enumerate() {
        let value = (coefficient * factor << shift) >> 5;
        if !(-(1_i64 << 15)..=(1_i64 << 15) - 1).contains(&value) {
            return Err(invalid(
                "scaled chroma DC value is outside the signed 16-bit range",
            ));
        }
        scaled[index] = value;
    }
    Ok(scaled)
}

/// Preserves pre-scaled chroma DC and inverse-scales the 4x4 chroma AC levels.
pub fn inverse_scale_chroma4x4(
    levels: &[i32; 16],
    scaling_list: &[u8; 16],
    qpc: i32,
) -> io::Result<[i64; 16]> {
    if !(0..=39).contains(&qpc) {
        return Err(invalid("inverse scaling QPC is outside [0,39]"));
    }
    if levels
        .iter()
        .any(|level| !(-(1_i32 << 15)..=(1_i32 << 15) - 1).contains(level))
    {
        return Err(invalid(
            "chroma block levels must be in the signed 16-bit range",
        ));
    }
    if scaling_list.contains(&0) {
        return Err(invalid("inverse scaling list contains zero"));
    }

    let mut scaled = [0_i64; 16];
    scaled[0] = i64::from(levels[0]);
    for index in 1..16 {
        let row = index / 4;
        let column = index % 4;
        let factor_class = row % 2 + column % 2;
        let value = i64::from(levels[index])
            * INVERSE_SCALE_4X4_FACTORS[(qpc % 6) as usize][factor_class]
            * i64::from(scaling_list[index]);
        scaled[index] = if qpc >= 24 {
            value << (qpc / 6 - 4)
        } else {
            let shift = 4 - qpc / 6;
            let rounding = 1_i64 << (shift - 1);
            (value + rounding) >> shift
        };
        if !(-(1_i64 << 15)..=(1_i64 << 15) - 1).contains(&scaled[index]) {
            return Err(invalid(
                "scaled chroma block value is outside the signed 16-bit range",
            ));
        }
    }
    Ok(scaled)
}

/// Assembles, scales, and inverse-transforms one 4x4 chroma residual block.
pub fn reconstruct_chroma4x4_residual(
    dc_c: i64,
    ac_scan_levels: &[i32; 15],
    scaling_list: &[u8; 16],
    qpc: i32,
) -> io::Result<[i64; 16]> {
    if !(0..=39).contains(&qpc) {
        return Err(invalid("inverse scaling QPC is outside [0,39]"));
    }
    if !(-(1_i64 << 15)..=(1_i64 << 15) - 1).contains(&dc_c) {
        return Err(invalid(
            "chroma block DC must be in the signed 16-bit range",
        ));
    }

    let dc_level = i32::try_from(dc_c)
        .map_err(|_| invalid("chroma block DC must be in the signed 16-bit range"))?;
    let levels = crate::place_chroma4x4_scan_levels(dc_level, ac_scan_levels);
    let scaled = inverse_scale_chroma4x4(&levels, scaling_list, qpc)?;
    Ok(inverse_transform_luma4x4(&scaled))
}

/// Places four raster-ordered 4x4 residual blocks into an 8x8 4:2:0 chroma macroblock.
pub fn assemble_chroma420_residual_macroblock(blocks: &[[i64; 16]; 4]) -> [i64; 64] {
    let mut macroblock = [0_i64; 64];
    for (block_index, block) in blocks.iter().enumerate() {
        let x_offset = block_index % 2 * 4;
        let y_offset = block_index / 2 * 4;
        for row in 0..4 {
            let destination = (y_offset + row) * 8 + x_offset;
            macroblock[destination..destination + 4].copy_from_slice(&block[row * 4..row * 4 + 4]);
        }
    }
    macroblock
}

/// Adds chroma residual samples to prediction and applies 8-bit Clip1C.
pub fn reconstruct_chroma420_macroblock(prediction: &[u8; 64], residual: &[i64; 64]) -> [u8; 64] {
    let mut samples = [0_u8; 64];
    for index in 0..64 {
        samples[index] = i64::from(prediction[index])
            .saturating_add(residual[index])
            .clamp(0, 255) as u8;
    }
    samples
}

/// Derives 8-bit chroma QPC from luma QPY and a PPS component offset.
pub fn derive_chroma_qpc(qpy: i32, qp_offset: i32) -> io::Result<i32> {
    if !(0..=51).contains(&qpy) {
        return Err(invalid("chroma QPY is outside [0,51]"));
    }
    if !(-12..=12).contains(&qp_offset) {
        return Err(invalid("chroma QP index offset is outside [-12,12]"));
    }

    let qpi = (qpy + qp_offset).clamp(0, 51);
    if qpi < 30 {
        return Ok(qpi);
    }
    Ok(CHROMA_QPC_FROM_QPI[(qpi - 30) as usize])
}

/// Applies H.264 8x8 luma inverse scaling to raster-order coefficient levels.
pub fn inverse_scale_luma8x8(
    levels: &[i32; 64],
    scaling_list: &[u8; 64],
    qpy: i32,
) -> io::Result<[i64; 64]> {
    if !(0..=51).contains(&qpy) {
        return Err(invalid("inverse scaling QPY is outside [0,51]"));
    }
    if scaling_list.contains(&0) {
        return Err(invalid("inverse scaling list contains zero"));
    }

    let mut scaled = [0_i64; 64];
    for index in 0..64 {
        let factor_class = INVERSE_SCALE_8X8_CLASSES[index];
        let value = i64::from(levels[index])
            * INVERSE_SCALE_8X8_FACTORS[(qpy % 6) as usize][factor_class]
            * i64::from(scaling_list[index]);
        scaled[index] = if qpy >= 24 {
            value << (qpy / 6 - 4)
        } else {
            let shift = 4 - qpy / 6;
            let rounding = 1_i64 << (shift - 1);
            (value + rounding) >> shift
        };
    }
    Ok(scaled)
}

/// Transforms dequantized raster-order coefficients into luma residual samples.
pub fn inverse_transform_luma4x4(coefficients: &[i64; 16]) -> [i64; 16] {
    let mut horizontal = [0_i64; 16];
    for row in 0..4 {
        let offset = row * 4;
        let transformed = inverse_transform_4x4_line([
            coefficients[offset],
            coefficients[offset + 1],
            coefficients[offset + 2],
            coefficients[offset + 3],
        ]);
        horizontal[offset..offset + 4].copy_from_slice(&transformed);
    }

    let mut residual = [0_i64; 16];
    for column in 0..4 {
        let transformed = inverse_transform_4x4_line([
            horizontal[column],
            horizontal[4 + column],
            horizontal[8 + column],
            horizontal[12 + column],
        ]);
        for (row, value) in transformed.into_iter().enumerate() {
            residual[row * 4 + column] = (value + 32) >> 6;
        }
    }
    residual
}

/// Applies the 4:2:0 2x2 inverse Hadamard transform to chroma DC levels.
pub fn inverse_transform_chroma_dc2x2(coefficients: &[i32; 4]) -> [i64; 4] {
    let [c00, c01, c10, c11] = coefficients.map(i64::from);
    [
        c00 + c01 + c10 + c11,
        c00 - c01 + c10 - c11,
        c00 + c01 - c10 - c11,
        c00 - c01 - c10 + c11,
    ]
}

/// Transforms 8x8 dequantized raster-order coefficients into residual samples.
pub fn inverse_transform_luma8x8(coefficients: &[i64; 64]) -> [i64; 64] {
    let mut horizontal = [0_i64; 64];
    for row in 0..8 {
        let offset = row * 8;
        let transformed = inverse_transform_8x8_line([
            coefficients[offset],
            coefficients[offset + 1],
            coefficients[offset + 2],
            coefficients[offset + 3],
            coefficients[offset + 4],
            coefficients[offset + 5],
            coefficients[offset + 6],
            coefficients[offset + 7],
        ]);
        horizontal[offset..offset + 8].copy_from_slice(&transformed);
    }

    let mut residual = [0_i64; 64];
    for column in 0..8 {
        let transformed = inverse_transform_8x8_line([
            horizontal[column],
            horizontal[8 + column],
            horizontal[16 + column],
            horizontal[24 + column],
            horizontal[32 + column],
            horizontal[40 + column],
            horizontal[48 + column],
            horizontal[56 + column],
        ]);
        for (row, value) in transformed.into_iter().enumerate() {
            residual[row * 8 + column] = (value + 32) >> 6;
        }
    }
    residual
}

/// Repeats eight filtered top reference samples across an 8x8 luma block.
pub fn predict_luma_intra8x8_vertical(top: &[u8; 8]) -> [u8; 64] {
    let mut prediction = [0_u8; 64];
    for row in 0..8 {
        prediction[row * 8..row * 8 + 8].copy_from_slice(top);
    }
    prediction
}

/// Repeats 16 filtered top samples across a 16x16 luma block.
pub fn predict_luma_intra16x16_vertical(top: &[u8; 16]) -> [u8; 256] {
    let mut prediction = [0_u8; 256];
    for row in 0..16 {
        prediction[row * 16..row * 16 + 16].copy_from_slice(top);
    }
    prediction
}

/// Repeats each filtered left sample across one row of a 16x16 luma block.
pub fn predict_luma_intra16x16_horizontal(left: &[u8; 16]) -> [u8; 256] {
    let mut prediction = [0_u8; 256];
    for (row, sample) in left.iter().enumerate() {
        prediction[row * 16..row * 16 + 16].fill(*sample);
    }
    prediction
}

/// Predicts a 16x16 luma block using whichever filtered edges are available.
pub fn predict_luma_intra16x16_dc(top: Option<&[u8; 16]>, left: Option<&[u8; 16]>) -> [u8; 256] {
    let dc_value = match (top, left) {
        (Some(top), Some(left)) => {
            let sum = top
                .iter()
                .chain(left.iter())
                .map(|sample| u32::from(*sample))
                .sum::<u32>();
            (sum + 16) >> 5
        }
        (Some(top), None) => {
            let sum = top.iter().map(|sample| u32::from(*sample)).sum::<u32>();
            (sum + 8) >> 4
        }
        (None, Some(left)) => {
            let sum = left.iter().map(|sample| u32::from(*sample)).sum::<u32>();
            (sum + 8) >> 4
        }
        (None, None) => 128,
    } as u8;
    [dc_value; 256]
}

/// Predicts a 16x16 luma block from filtered top, left, and top-left references.
pub fn predict_luma_intra16x16_plane(top: &[u8; 16], left: &[u8; 16], top_left: u8) -> [u8; 256] {
    let mut horizontal_gradient = 0_i32;
    let mut vertical_gradient = 0_i32;
    for index in 1..=7 {
        horizontal_gradient +=
            index as i32 * (i32::from(top[7 + index]) - i32::from(top[7 - index]));
        vertical_gradient +=
            index as i32 * (i32::from(left[7 + index]) - i32::from(left[7 - index]));
    }
    horizontal_gradient += 8 * (i32::from(top[15]) - i32::from(top_left));
    vertical_gradient += 8 * (i32::from(left[15]) - i32::from(top_left));
    let a = 16 * (i32::from(top[15]) + i32::from(left[15]));
    let b = (5 * horizontal_gradient + 32) >> 6;
    let c = (5 * vertical_gradient + 32) >> 6;

    let mut prediction = [0_u8; 256];
    for row in 0..16 {
        for column in 0..16 {
            let value = (a + b * (column as i32 - 7) + c * (row as i32 - 7) + 16) >> 5;
            prediction[row * 16 + column] = value.clamp(0, 255) as u8;
        }
    }
    prediction
}

/// Repeats each filtered left reference sample across one row of an 8x8 luma block.
pub fn predict_luma_intra8x8_horizontal(left: &[u8; 8]) -> [u8; 64] {
    let mut prediction = [0_u8; 64];
    for (row, sample) in left.iter().enumerate() {
        prediction[row * 8..row * 8 + 8].fill(*sample);
    }
    prediction
}

/// Predicts an 8x8 luma block using whichever filtered edges are available.
pub fn predict_luma_intra8x8_dc(top: Option<&[u8; 8]>, left: Option<&[u8; 8]>) -> [u8; 64] {
    let dc_value = match (top, left) {
        (Some(top), Some(left)) => {
            let sum = top
                .iter()
                .chain(left.iter())
                .map(|sample| u32::from(*sample))
                .sum::<u32>();
            (sum + 8) >> 4
        }
        (Some(top), None) => {
            let sum = top.iter().map(|sample| u32::from(*sample)).sum::<u32>();
            (sum + 4) >> 3
        }
        (None, Some(left)) => {
            let sum = left.iter().map(|sample| u32::from(*sample)).sum::<u32>();
            (sum + 4) >> 3
        }
        (None, None) => 128,
    } as u8;
    [dc_value; 64]
}

/// Predicts 4:2:0 chroma DC using quadrant-specific edge fallbacks.
pub fn predict_chroma_intra8x8_dc(top: Option<&[u8; 8]>, left: Option<&[u8; 8]>) -> [u8; 64] {
    let average_edge = |samples: &[u8; 8], offset: usize| -> u32 {
        (samples[offset..offset + 4]
            .iter()
            .map(|sample| u32::from(*sample))
            .sum::<u32>()
            + 2)
            >> 2
    };
    let mut prediction = [0_u8; 64];
    for block_row in 0..2 {
        for block_column in 0..2 {
            let top_offset = block_column * 4;
            let left_offset = block_row * 4;
            let dc_value = match (block_row, block_column) {
                (0, 1) => top
                    .map(|samples| average_edge(samples, top_offset))
                    .or_else(|| left.map(|samples| average_edge(samples, left_offset)))
                    .unwrap_or(128),
                (1, 0) => left
                    .map(|samples| average_edge(samples, left_offset))
                    .or_else(|| top.map(|samples| average_edge(samples, top_offset)))
                    .unwrap_or(128),
                _ => match (top, left) {
                    (Some(top), Some(left)) => {
                        let sum = top[top_offset..top_offset + 4]
                            .iter()
                            .chain(&left[left_offset..left_offset + 4])
                            .map(|sample| u32::from(*sample))
                            .sum::<u32>();
                        (sum + 4) >> 3
                    }
                    (Some(top), None) => average_edge(top, top_offset),
                    (None, Some(left)) => average_edge(left, left_offset),
                    (None, None) => 128,
                },
            } as u8;
            for row in block_row * 4..block_row * 4 + 4 {
                prediction[row * 8 + block_column * 4..row * 8 + block_column * 4 + 4]
                    .fill(dc_value);
            }
        }
    }
    prediction
}

/// Predicts 4:2:0 chroma horizontally by repeating each left sample across one row.
pub fn predict_chroma_intra8x8_horizontal(left: &[u8; 8]) -> [u8; 64] {
    let mut prediction = [0_u8; 64];
    for (row, sample) in left.iter().enumerate() {
        prediction[row * 8..row * 8 + 8].fill(*sample);
    }
    prediction
}

/// Predicts 4:2:0 chroma vertically by repeating each top sample down one column.
pub fn predict_chroma_intra8x8_vertical(top: &[u8; 8]) -> [u8; 64] {
    predict_luma_intra8x8_vertical(top)
}

/// Predicts 4:2:0 chroma with the normative 8x8 plane gradients and clipping.
pub fn predict_chroma_intra8x8_plane(top: &[u8; 8], left: &[u8; 8], top_left: u8) -> [u8; 64] {
    let mut horizontal_gradient = 0_i32;
    let mut vertical_gradient = 0_i32;
    for index in 0..4 {
        let weight = (index + 1) as i32;
        let top_reference = if index == 3 {
            i32::from(top_left)
        } else {
            i32::from(top[2 - index])
        };
        let left_reference = if index == 3 {
            i32::from(top_left)
        } else {
            i32::from(left[2 - index])
        };
        horizontal_gradient += weight * (i32::from(top[4 + index]) - top_reference);
        vertical_gradient += weight * (i32::from(left[4 + index]) - left_reference);
    }

    let a = 16 * (i32::from(top[7]) + i32::from(left[7]));
    let b = (34 * horizontal_gradient + 32) >> 6;
    let c = (34 * vertical_gradient + 32) >> 6;
    let mut prediction = [0_u8; 64];
    for row in 0..8 {
        for column in 0..8 {
            let value = (a + b * (column as i32 - 3) + c * (row as i32 - 3) + 16) >> 5;
            prediction[row * 8 + column] = value.clamp(0, 255) as u8;
        }
    }
    prediction
}

/// Dispatches 4:2:0 chroma intra prediction modes 0=DC, 1=Horizontal, 2=Vertical, and 3=Plane.
pub fn predict_chroma_intra8x8(
    mode: u8,
    top: Option<&[u8; 8]>,
    left: Option<&[u8; 8]>,
    top_left: Option<u8>,
) -> io::Result<[u8; 64]> {
    match mode {
        0 => Ok(predict_chroma_intra8x8_dc(top, left)),
        1 => left
            .map(predict_chroma_intra8x8_horizontal)
            .ok_or_else(|| invalid("left edge is required for chroma horizontal prediction")),
        2 => top
            .map(predict_chroma_intra8x8_vertical)
            .ok_or_else(|| invalid("top edge is required for chroma vertical prediction")),
        3 => match (top, left, top_left) {
            (Some(top), Some(left), Some(top_left)) => {
                Ok(predict_chroma_intra8x8_plane(top, left, top_left))
            }
            _ => Err(invalid(
                "top, left, and top-left edges are required for chroma plane prediction",
            )),
        },
        _ => Err(invalid("chroma intra prediction mode is outside [0,3]")),
    }
}

/// Interpolates an 8x8 luma block from 16 filtered top references.
pub fn predict_luma_intra8x8_diagonal_down_left(top: &[u8; 16]) -> [u8; 64] {
    let mut prediction = [0_u8; 64];
    for row in 0..8 {
        for column in 0..8 {
            let index = row + column;
            let last_index = (index + 2).min(15);
            let value = (u16::from(top[index])
                + 2 * u16::from(top[index + 1])
                + u16::from(top[last_index])
                + 2)
                >> 2;
            prediction[row * 8 + column] = value as u8;
        }
    }
    prediction
}

/// Interpolates an 8x8 luma block from filtered top, left, and top-left references.
pub fn predict_luma_intra8x8_diagonal_down_right(
    top: &[u8; 16],
    left: &[u8; 8],
    top_left: u8,
) -> [u8; 64] {
    let reference_at = |position: i32| -> u16 {
        match position {
            -1 => u16::from(top_left),
            value if value < -1 => u16::from(left[(-value - 2) as usize]),
            value => u16::from(top[value as usize]),
        }
    };

    let mut prediction = [0_u8; 64];
    for row in 0..8 {
        for column in 0..8 {
            let position = column as i32 - row as i32;
            let value = (reference_at(position - 1)
                + 2 * reference_at(position)
                + reference_at(position + 1)
                + 2)
                >> 2;
            prediction[row * 8 + column] = value as u8;
        }
    }
    prediction
}

/// Interpolates an 8x8 luma block across vertical-right reference phases.
pub fn predict_luma_intra8x8_vertical_right(
    top: &[u8; 16],
    left: &[u8; 8],
    top_left: u8,
) -> [u8; 64] {
    let top_reference_at = |position: i32| -> u16 {
        if position == -1 {
            u16::from(top_left)
        } else {
            u16::from(top[position as usize])
        }
    };

    let mut prediction = [0_u8; 64];
    for row in 0..8 {
        for column in 0..8 {
            let phase = 2 * column as i32 - row as i32;
            let value = if phase >= 0 && phase % 2 == 0 {
                let center = phase / 2;
                (top_reference_at(center - 1)
                    + 2 * top_reference_at(center)
                    + top_reference_at(center + 1)
                    + 2)
                    >> 2
            } else if phase >= 0 {
                let center = (phase - 1) / 2;
                (top_reference_at(center - 1) + top_reference_at(center) + 1) >> 1
            } else if phase == -1 {
                (u16::from(left[0]) + u16::from(top_left) + 1) >> 1
            } else if phase == -2 {
                (u16::from(left[0]) + 2 * u16::from(top_left) + u16::from(top[0]) + 2) >> 2
            } else if (-phase) % 2 == 1 {
                let center = ((-phase - 3) / 2) as usize;
                (u16::from(left[center]) + u16::from(left[center + 1]) + 1) >> 1
            } else {
                let center = ((-phase - 4) / 2) as usize;
                (u16::from(left[center])
                    + 2 * u16::from(left[center + 1])
                    + u16::from(left[center + 2])
                    + 2)
                    >> 2
            };
            prediction[row * 8 + column] = value as u8;
        }
    }
    prediction
}

/// Predicts an 8x8 luma block by transposing vertical-right prediction.
pub fn predict_luma_intra8x8_horizontal_down(
    top: &[u8; 8],
    left: &[u8; 16],
    top_left: u8,
) -> [u8; 64] {
    let vertical_prediction = predict_luma_intra8x8_vertical_right(left, top, top_left);
    let mut prediction = [0_u8; 64];
    for row in 0..8 {
        for column in 0..8 {
            prediction[row * 8 + column] = vertical_prediction[column * 8 + row];
        }
    }
    prediction
}

/// Interpolates an 8x8 luma block across vertical-left top-reference phases.
pub fn predict_luma_intra8x8_vertical_left(top: &[u8; 16]) -> [u8; 64] {
    let mut prediction = [0_u8; 64];
    for row in 0..8 {
        for column in 0..8 {
            let phase = 2 * column + row;
            let value = if phase % 2 == 0 {
                let center = phase / 2;
                (u16::from(top[center]) + u16::from(top[center + 1]) + 1) >> 1
            } else {
                let center = (phase - 1) / 2;
                (u16::from(top[center])
                    + 2 * u16::from(top[center + 1])
                    + u16::from(top[center + 2])
                    + 2)
                    >> 2
            };
            prediction[row * 8 + column] = value as u8;
        }
    }
    prediction
}

/// Predicts an 8x8 luma block by transposing vertical-left prediction.
pub fn predict_luma_intra8x8_horizontal_up(left: &[u8; 16]) -> [u8; 64] {
    let vertical_prediction = predict_luma_intra8x8_vertical_left(left);
    let mut prediction = [0_u8; 64];
    for row in 0..8 {
        for column in 0..8 {
            prediction[row * 8 + column] = vertical_prediction[column * 8 + row];
        }
    }
    prediction
}

/// Predicts an 8x8 luma block from filtered top and left references.
pub fn predict_luma_intra8x8_plane(top: &[u8; 16], left: &[u8; 16]) -> [u8; 64] {
    let mut horizontal_gradient = 0_i32;
    let mut vertical_gradient = 0_i32;
    for index in 1..=4 {
        horizontal_gradient +=
            index as i32 * (i32::from(top[4 + index]) - i32::from(top[4 - index]));
        vertical_gradient +=
            index as i32 * (i32::from(left[4 + index]) - i32::from(left[4 - index]));
    }
    let a = 16 * (i32::from(top[8]) + i32::from(left[8]));
    let b = (17 * horizontal_gradient + 16) >> 5;
    let c = (17 * vertical_gradient + 16) >> 5;

    let mut prediction = [0_u8; 64];
    for row in 0..8 {
        for column in 0..8 {
            let value = (a + b * (column as i32 - 3) + c * (row as i32 - 3) + 16) >> 5;
            prediction[row * 8 + column] = value.clamp(0, 255) as u8;
        }
    }
    prediction
}

/// Repeats four available top reference samples across a 4x4 luma block.
pub fn predict_luma_intra4x4_vertical(top: &[u8; 4]) -> [u8; 16] {
    let mut prediction = [0_u8; 16];
    for row in 0..4 {
        prediction[row * 4..row * 4 + 4].copy_from_slice(top);
    }
    prediction
}

/// Repeats each of four available left reference samples across one block row.
pub fn predict_luma_intra4x4_horizontal(left: &[u8; 4]) -> [u8; 16] {
    let mut prediction = [0_u8; 16];
    for (row, sample) in left.iter().enumerate() {
        prediction[row * 4..row * 4 + 4].fill(*sample);
    }
    prediction
}

/// Predicts a 4x4 luma block using whichever top and left references are available.
pub fn predict_luma_intra4x4_dc(top: Option<&[u8; 4]>, left: Option<&[u8; 4]>) -> [u8; 16] {
    let dc_value = match (top, left) {
        (Some(top), Some(left)) => {
            let sum = top
                .iter()
                .chain(left.iter())
                .map(|sample| u32::from(*sample))
                .sum::<u32>();
            (sum + 4) >> 3
        }
        (Some(top), None) => {
            let sum = top.iter().map(|sample| u32::from(*sample)).sum::<u32>();
            (sum + 2) >> 2
        }
        (None, Some(left)) => {
            let sum = left.iter().map(|sample| u32::from(*sample)).sum::<u32>();
            (sum + 2) >> 2
        }
        (None, None) => 128,
    } as u8;
    [dc_value; 16]
}

/// Interpolates a 4x4 luma block from top and top-right references.
pub fn predict_luma_intra4x4_diagonal_down_left(top: &[u8; 8]) -> [u8; 16] {
    let mut prediction = [0_u8; 16];
    for row in 0..4 {
        for column in 0..4 {
            let index = row + column;
            let last_index = (index + 2).min(7);
            let value = (u16::from(top[index])
                + 2 * u16::from(top[index + 1])
                + u16::from(top[last_index])
                + 2)
                >> 2;
            prediction[row * 4 + column] = value as u8;
        }
    }
    prediction
}

/// Interpolates a 4x4 luma block from top, left, and top-left references.
pub fn predict_luma_intra4x4_diagonal_down_right(
    top: &[u8; 8],
    left: &[u8; 4],
    top_left: u8,
) -> [u8; 16] {
    let reference_at = |position: i32| -> u16 {
        match position {
            -1 => u16::from(top_left),
            value if value < -1 => u16::from(left[(-value - 2) as usize]),
            value => u16::from(top[value as usize]),
        }
    };

    let mut prediction = [0_u8; 16];
    for row in 0..4 {
        for column in 0..4 {
            let position = column as i32 - row as i32;
            let value = (reference_at(position - 1)
                + 2 * reference_at(position)
                + reference_at(position + 1)
                + 2)
                >> 2;
            prediction[row * 4 + column] = value as u8;
        }
    }
    prediction
}

/// Interpolates a 4x4 luma block from vertical-right reference phases.
pub fn predict_luma_intra4x4_vertical_right(
    top: &[u8; 8],
    left: &[u8; 4],
    top_left: u8,
) -> [u8; 16] {
    let top_reference_at = |position: i32| -> u16 {
        if position == -1 {
            u16::from(top_left)
        } else {
            u16::from(top[position as usize])
        }
    };

    let mut prediction = [0_u8; 16];
    for row in 0..4 {
        for column in 0..4 {
            let phase = 2 * column as i32 - row as i32;
            let value = if phase >= 0 && phase % 2 == 0 {
                let center = phase / 2;
                (top_reference_at(center - 1)
                    + 2 * top_reference_at(center)
                    + top_reference_at(center + 1)
                    + 2)
                    >> 2
            } else if phase >= 0 {
                let center = (phase - 1) / 2;
                (top_reference_at(center - 1) + top_reference_at(center) + 1) >> 1
            } else if phase == -1 {
                (u16::from(left[0]) + u16::from(top_left) + 1) >> 1
            } else if phase == -2 {
                (u16::from(left[0]) + 2 * u16::from(top_left) + u16::from(top[0]) + 2) >> 2
            } else {
                (u16::from(left[0]) + u16::from(left[1]) + 1) >> 1
            };
            prediction[row * 4 + column] = value as u8;
        }
    }
    prediction
}

/// Predicts Horizontal_Down by transposing Vertical_Right and extending the left edge.
pub fn predict_luma_intra4x4_horizontal_down(
    top: &[u8; 4],
    left: &[u8; 4],
    top_left: u8,
) -> [u8; 16] {
    let vertical_top = [
        left[0], left[1], left[2], left[3], left[3], left[3], left[3], left[3],
    ];
    let vertical_prediction = predict_luma_intra4x4_vertical_right(&vertical_top, top, top_left);
    let mut prediction = [0_u8; 16];
    for row in 0..4 {
        for column in 0..4 {
            prediction[row * 4 + column] = vertical_prediction[column * 4 + row];
        }
    }
    prediction
}

/// Interpolates a 4x4 luma block using alternating top-reference phases.
pub fn predict_luma_intra4x4_vertical_left(top: &[u8; 8]) -> [u8; 16] {
    let mut prediction = [0_u8; 16];
    for row in 0..4 {
        for column in 0..4 {
            let phase = 2 * column + row;
            let value = if phase % 2 == 0 {
                let center = phase / 2;
                (u16::from(top[center]) + u16::from(top[center + 1]) + 1) >> 1
            } else {
                let center = (phase - 1) / 2;
                (u16::from(top[center])
                    + 2 * u16::from(top[center + 1])
                    + u16::from(top[center + 2])
                    + 2)
                    >> 2
            };
            prediction[row * 4 + column] = value as u8;
        }
    }
    prediction
}

/// Interpolates a 4x4 luma block using alternating left-reference phases.
pub fn predict_luma_intra4x4_horizontal_up(left: &[u8; 8]) -> [u8; 16] {
    let mut prediction = [0_u8; 16];
    for row in 0..4 {
        for column in 0..4 {
            let phase = 2 * row + column;
            let value = if phase % 2 == 0 {
                let center = phase / 2;
                (u16::from(left[center]) + u16::from(left[center + 1]) + 1) >> 1
            } else {
                let center = (phase - 1) / 2;
                (u16::from(left[center])
                    + 2 * u16::from(left[center + 1])
                    + u16::from(left[center + 2])
                    + 2)
                    >> 2
            };
            prediction[row * 4 + column] = value as u8;
        }
    }
    prediction
}

fn inverse_transform_4x4_line(coefficients: [i64; 4]) -> [i64; 4] {
    let even_sum = coefficients[0] + coefficients[2];
    let even_difference = coefficients[0] - coefficients[2];
    let odd_difference = (coefficients[1] >> 1) - coefficients[3];
    let odd_sum = coefficients[1] + (coefficients[3] >> 1);
    [
        even_sum + odd_sum,
        even_difference + odd_difference,
        even_difference - odd_difference,
        even_sum - odd_sum,
    ]
}

fn inverse_transform_8x8_line(coefficients: [i64; 8]) -> [i64; 8] {
    let a0 = coefficients[0] + coefficients[4];
    let a2 = coefficients[0] - coefficients[4];
    let a4 = (coefficients[2] >> 1) - coefficients[6];
    let a6 = coefficients[2] + (coefficients[6] >> 1);
    let b0 = a0 + a6;
    let b2 = a2 + a4;
    let b4 = a2 - a4;
    let b6 = a0 - a6;

    let a1 = -coefficients[3] + coefficients[5] - coefficients[7] - (coefficients[7] >> 1);
    let a3 = coefficients[1] + coefficients[7] - coefficients[3] - (coefficients[3] >> 1);
    let a5 = -coefficients[1] + coefficients[7] + coefficients[5] + (coefficients[5] >> 1);
    let a7 = coefficients[3] + coefficients[5] + coefficients[1] + (coefficients[1] >> 1);
    let b1 = a1 + (a7 >> 2);
    let b3 = a3 + (a5 >> 2);
    let b5 = a5 - (a3 >> 2);
    let b7 = a7 - (a1 >> 2);

    [
        b0 + b7,
        b2 + b5,
        b4 + b3,
        b6 + b1,
        b6 - b1,
        b4 - b3,
        b2 - b5,
        b0 - b7,
    ]
}

fn invalid(message: &'static str) -> io::Error {
    io::Error::new(ErrorKind::InvalidData, message)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn poc_type0_wraps_lsb_and_updates_reference_state() {
        for (state, lsb, delta, idr, expected, next_msb, next_lsb) in [
            (PocType0State::default(), 3, 0, false, 3, 0, 3),
            (
                PocType0State {
                    previous_pic_order_cnt_msb: 0,
                    previous_pic_order_cnt_lsb: 15,
                },
                7,
                0,
                false,
                23,
                16,
                7,
            ),
            (
                PocType0State {
                    previous_pic_order_cnt_msb: 16,
                    previous_pic_order_cnt_lsb: 1,
                },
                10,
                0,
                false,
                10,
                0,
                10,
            ),
            (
                PocType0State {
                    previous_pic_order_cnt_msb: 0,
                    previous_pic_order_cnt_lsb: 1,
                },
                9,
                0,
                false,
                9,
                0,
                9,
            ),
            (PocType0State::default(), 5, -8, false, -3, 0, 5),
            (
                PocType0State {
                    previous_pic_order_cnt_msb: 64,
                    previous_pic_order_cnt_lsb: 15,
                },
                2,
                0,
                true,
                2,
                0,
                2,
            ),
            (
                PocType0State {
                    previous_pic_order_cnt_msb: 0,
                    previous_pic_order_cnt_lsb: u32::MAX,
                },
                2,
                0,
                true,
                2,
                0,
                2,
            ),
        ] {
            let mut state = state;
            assert_eq!(state.calculate(16, lsb, delta, 1, idr).unwrap(), expected);
            assert_eq!(state.previous_pic_order_cnt_msb, next_msb);
            assert_eq!(state.previous_pic_order_cnt_lsb, next_lsb);
        }
    }

    #[test]
    fn poc_type0_non_reference_does_not_advance_state() {
        let mut state = PocType0State {
            previous_pic_order_cnt_msb: 16,
            previous_pic_order_cnt_lsb: 14,
        };
        assert_eq!(state.calculate(16, 1, 0, 0, false).unwrap(), 33);
        assert_eq!(state.previous_pic_order_cnt_msb, 16);
        assert_eq!(state.previous_pic_order_cnt_lsb, 14);
    }

    #[test]
    fn poc_type0_rejects_invalid_input_and_overflow_without_changing_state() {
        for (maximum, lsb, nal_ref_idc, idr) in [
            (15, 0, 1, false),
            (24, 0, 1, false),
            (16, 16, 1, false),
            (16, 0, 4, false),
            (16, 0, 0, true),
        ] {
            let mut state = PocType0State {
                previous_pic_order_cnt_msb: 7,
                previous_pic_order_cnt_lsb: 2,
            };
            let previous = state;
            assert!(state.calculate(maximum, lsb, 0, nal_ref_idc, idr).is_err());
            assert_eq!(state, previous);
        }
        let mut state = PocType0State {
            previous_pic_order_cnt_msb: i64::MAX,
            previous_pic_order_cnt_lsb: 15,
        };
        let previous = state;
        assert!(state.calculate(16, 0, 0, 1, false).is_err());
        assert_eq!(state, previous);
    }

    #[test]
    fn poc_type1_uses_frame_number_cycles_and_reference_state() {
        let offsets = [2, 3];
        let mut state = PocType12State::default();
        assert_eq!(
            state
                .calculate_type1(16, 0, 0, 0, -1, 1, &offsets, 1, true)
                .unwrap(),
            0
        );
        assert_eq!(
            state
                .calculate_type1(16, 1, 0, 0, -1, 1, &offsets, 1, false)
                .unwrap(),
            2
        );
        let previous = state;
        assert_eq!(
            state
                .calculate_type1(16, 2, 0, 0, -1, 1, &offsets, 0, false)
                .unwrap(),
            1
        );
        assert_eq!(state, previous);
        assert_eq!(
            state
                .calculate_type1(16, 0, 0, 0, -1, 1, &offsets, 1, false)
                .unwrap(),
            40
        );
    }

    #[test]
    fn poc_type2_uses_frame_number_offset_and_non_reference_adjustment() {
        let mut state = PocType12State::default();
        assert_eq!(state.calculate_type2(16, 0, 1, true).unwrap(), 0);
        assert_eq!(state.calculate_type2(16, 1, 1, false).unwrap(), 2);
        let previous = state;
        assert_eq!(state.calculate_type2(16, 2, 0, false).unwrap(), 3);
        assert_eq!(state, previous);
        let mut wrap_state = PocType12State {
            previous_frame_num: 15,
            previous_frame_num_offset: 0,
        };
        assert_eq!(wrap_state.calculate_type2(16, 0, 1, false).unwrap(), 32);
    }

    #[test]
    fn poc_type12_rejects_invalid_inputs_and_overflow_transactionally() {
        let mut state = PocType12State {
            previous_frame_num: 15,
            previous_frame_num_offset: i64::MAX,
        };
        let previous = state;
        assert!(state.calculate_type2(16, 0, 1, false).is_err());
        assert_eq!(state, previous);

        let mut type1_state = PocType12State::default();
        assert!(type1_state
            .calculate_type1(16, 0, i64::MAX, 1, 0, 0, &[1], 1, false)
            .is_err());
        assert_eq!(type1_state, PocType12State::default());
    }

    #[test]
    fn presentation_order_buffer_holds_and_releases_pictures_by_poc() {
        let frame = Yuv420Frame {
            width: 2,
            height: 2,
            y_stride: 2,
            u_stride: 1,
            v_stride: 1,
            y: vec![1, 2, 3, 4],
            u: vec![5],
            v: vec![6],
        };
        let mut buffer = PresentationOrderBuffer::new(2);
        let mut released = Vec::new();
        for poc in [0, 6, 2, 4] {
            if let Some(picture) = buffer
                .push(&PresentationPicture {
                    picture_order_cnt: poc,
                    frame: frame.clone(),
                })
                .unwrap()
            {
                released.push(picture.picture_order_cnt);
            }
        }
        assert_eq!(released, vec![0, 2]);
        assert_eq!(
            buffer
                .drain()
                .iter()
                .map(|picture| picture.picture_order_cnt)
                .collect::<Vec<_>>(),
            vec![4, 6]
        );
        assert!(buffer.drain().is_empty());
    }

    #[test]
    fn derives_luma_deblocking_indices_and_disable_mode() {
        for (qp_p, qp_q, disable_idc, alpha_offset, beta_offset, expected) in [
            (
                20,
                21,
                0,
                -3,
                2,
                LumaDeblockingParameters {
                    mode: LumaDeblockingMode::AllEdges,
                    index_a: 15,
                    index_b: 25,
                },
            ),
            (
                0,
                1,
                2,
                -6,
                -1,
                LumaDeblockingParameters {
                    mode: LumaDeblockingMode::AllExceptSliceBoundaries,
                    index_a: 0,
                    index_b: 0,
                },
            ),
            (
                51,
                50,
                1,
                6,
                2,
                LumaDeblockingParameters {
                    mode: LumaDeblockingMode::Disabled,
                    index_a: 51,
                    index_b: 51,
                },
            ),
        ] {
            assert_eq!(
                derive_luma_deblocking_parameters(
                    qp_p,
                    qp_q,
                    disable_idc,
                    alpha_offset,
                    beta_offset
                )
                .unwrap(),
                expected
            );
        }
    }

    #[test]
    fn looks_up_luma_deblocking_threshold_table_vectors() {
        for (index_a, index_b, expected) in [
            (0, 0, LumaDeblockingThresholds { alpha: 0, beta: 0 }),
            (16, 17, LumaDeblockingThresholds { alpha: 4, beta: 0 }),
            (18, 18, LumaDeblockingThresholds { alpha: 5, beta: 2 }),
            (26, 26, LumaDeblockingThresholds { alpha: 15, beta: 4 }),
            (
                40,
                40,
                LumaDeblockingThresholds {
                    alpha: 80,
                    beta: 12,
                },
            ),
            (
                51,
                51,
                LumaDeblockingThresholds {
                    alpha: 255,
                    beta: 18,
                },
            ),
        ] {
            assert_eq!(
                lookup_luma_deblocking_thresholds(index_a, index_b).unwrap(),
                expected
            );
        }
        assert!(lookup_luma_deblocking_thresholds(52, 0).is_err());
    }

    #[test]
    fn looks_up_luma_tc0_table_vectors() {
        for (index_a, boundary_strength, expected) in [
            (0, 1, 0),
            (23, 1, 1),
            (21, 2, 1),
            (18, 3, 1),
            (33, 1, 2),
            (51, 1, 13),
            (51, 2, 17),
            (51, 3, 25),
        ] {
            assert_eq!(
                lookup_luma_tc0(index_a, boundary_strength).unwrap(),
                expected
            );
        }
        assert!(lookup_luma_tc0(52, 1).is_err());
        assert!(lookup_luma_tc0(0, 0).is_err());
        assert!(lookup_luma_tc0(0, 4).is_err());
    }

    #[test]
    fn should_filter_luma_edge_applies_strict_thresholds() {
        let vectors = [
            (1, LumaDeblockingThresholds { alpha: 5, beta: 11 }, true),
            (4, LumaDeblockingThresholds { alpha: 5, beta: 11 }, true),
            (
                0,
                LumaDeblockingThresholds {
                    alpha: 255,
                    beta: 255,
                },
                false,
            ),
            (1, LumaDeblockingThresholds { alpha: 4, beta: 11 }, false),
            (1, LumaDeblockingThresholds { alpha: 5, beta: 10 }, false),
            (1, LumaDeblockingThresholds { alpha: 5, beta: 9 }, false),
        ];
        for (boundary_strength, thresholds, expected) in vectors {
            assert_eq!(
                should_filter_luma_edge(100, 104, 90, 113, boundary_strength, thresholds).unwrap(),
                expected
            );
        }
        assert!(should_filter_luma_edge(
            100,
            104,
            90,
            113,
            5,
            LumaDeblockingThresholds { alpha: 5, beta: 11 }
        )
        .is_err());
    }

    #[test]
    fn filters_luma_weak_edges_with_tc_and_conditional_side_adjustments() {
        let vectors = [
            (
                LumaEdgeSamples {
                    p0: 100,
                    p1: 98,
                    p2: 96,
                    q0: 104,
                    q1: 105,
                    q2: 106,
                },
                10,
                2,
                LumaEdgeSamples {
                    p0: 101,
                    p1: 99,
                    p2: 96,
                    q0: 103,
                    q1: 104,
                    q2: 106,
                },
            ),
            (
                LumaEdgeSamples {
                    p0: 110,
                    p1: 110,
                    p2: 120,
                    q0: 100,
                    q1: 100,
                    q2: 90,
                },
                5,
                1,
                LumaEdgeSamples {
                    p0: 109,
                    p1: 110,
                    p2: 120,
                    q0: 101,
                    q1: 100,
                    q2: 90,
                },
            ),
        ];
        for (samples, beta, tc0, expected) in vectors {
            assert_eq!(filter_luma_weak_edge(samples, beta, tc0), expected);
        }
    }

    #[test]
    fn filters_chroma_weak_edges_with_chroma_style_clipping_and_preserves_side_samples() {
        let vectors = [
            (
                ChromaEdgeSamples {
                    p0: 100,
                    p1: 98,
                    q0: 104,
                    q1: 105,
                },
                0,
                ChromaEdgeSamples {
                    p0: 101,
                    p1: 98,
                    q0: 103,
                    q1: 105,
                },
            ),
            (
                ChromaEdgeSamples {
                    p0: 110,
                    p1: 110,
                    q0: 100,
                    q1: 100,
                },
                2,
                ChromaEdgeSamples {
                    p0: 107,
                    p1: 110,
                    q0: 103,
                    q1: 100,
                },
            ),
        ];
        for (samples, tc0, expected) in vectors {
            assert_eq!(filter_chroma_weak_edge(samples, tc0), expected);
        }
    }

    #[test]
    fn filters_luma_strong_edges_per_side_with_fallbacks() {
        let vectors = [
            (
                LumaStrongEdgeSamples {
                    p0: 100,
                    p1: 99,
                    p2: 98,
                    p3: 97,
                    q0: 102,
                    q1: 103,
                    q2: 104,
                    q3: 105,
                },
                16,
                5,
                LumaEdgeSamples {
                    p0: 100,
                    p1: 100,
                    p2: 99,
                    q0: 102,
                    q1: 102,
                    q2: 103,
                },
            ),
            (
                LumaStrongEdgeSamples {
                    p0: 100,
                    p1: 99,
                    p2: 98,
                    p3: 97,
                    q0: 102,
                    q1: 103,
                    q2: 130,
                    q3: 120,
                },
                16,
                5,
                LumaEdgeSamples {
                    p0: 100,
                    p1: 100,
                    p2: 99,
                    q0: 102,
                    q1: 103,
                    q2: 130,
                },
            ),
            (
                LumaStrongEdgeSamples {
                    p0: 100,
                    p1: 99,
                    p2: 130,
                    p3: 120,
                    q0: 102,
                    q1: 103,
                    q2: 104,
                    q3: 105,
                },
                16,
                5,
                LumaEdgeSamples {
                    p0: 100,
                    p1: 99,
                    p2: 130,
                    q0: 102,
                    q1: 102,
                    q2: 103,
                },
            ),
            (
                LumaStrongEdgeSamples {
                    p0: 100,
                    p1: 99,
                    p2: 98,
                    p3: 97,
                    q0: 102,
                    q1: 103,
                    q2: 104,
                    q3: 105,
                },
                0,
                5,
                LumaEdgeSamples {
                    p0: 100,
                    p1: 99,
                    p2: 98,
                    q0: 102,
                    q1: 103,
                    q2: 104,
                },
            ),
        ];
        for (samples, alpha, beta, expected) in vectors {
            assert_eq!(filter_luma_strong_edge(samples, alpha, beta), expected);
        }
    }

    #[test]
    fn applies_luma_deblocking_edge_and_honors_mode() {
        let samples = LumaStrongEdgeSamples {
            p0: 100,
            p1: 98,
            p2: 96,
            p3: 95,
            q0: 104,
            q1: 105,
            q2: 106,
            q3: 107,
        };
        let unchanged = LumaEdgeSamples {
            p0: 100,
            p1: 98,
            p2: 96,
            q0: 104,
            q1: 105,
            q2: 106,
        };
        let vectors = [
            (
                2,
                LumaDeblockingParameters {
                    mode: LumaDeblockingMode::AllEdges,
                    index_a: 33,
                    index_b: 40,
                },
                false,
                LumaEdgeSamples {
                    p0: 101,
                    p1: 99,
                    p2: 96,
                    q0: 103,
                    q1: 104,
                    q2: 106,
                },
            ),
            (
                4,
                LumaDeblockingParameters {
                    mode: LumaDeblockingMode::AllEdges,
                    index_a: 26,
                    index_b: 30,
                },
                false,
                LumaEdgeSamples {
                    p0: 101,
                    p1: 100,
                    p2: 98,
                    q0: 103,
                    q1: 104,
                    q2: 105,
                },
            ),
            (
                1,
                LumaDeblockingParameters {
                    mode: LumaDeblockingMode::AllEdges,
                    index_a: 0,
                    index_b: 0,
                },
                false,
                unchanged,
            ),
            (
                4,
                LumaDeblockingParameters {
                    mode: LumaDeblockingMode::Disabled,
                    index_a: 26,
                    index_b: 30,
                },
                false,
                unchanged,
            ),
            (
                4,
                LumaDeblockingParameters {
                    mode: LumaDeblockingMode::AllExceptSliceBoundaries,
                    index_a: 26,
                    index_b: 30,
                },
                true,
                unchanged,
            ),
            (
                4,
                LumaDeblockingParameters {
                    mode: LumaDeblockingMode::AllExceptSliceBoundaries,
                    index_a: 26,
                    index_b: 30,
                },
                false,
                LumaEdgeSamples {
                    p0: 101,
                    p1: 100,
                    p2: 98,
                    q0: 103,
                    q1: 104,
                    q2: 105,
                },
            ),
            (
                4,
                LumaDeblockingParameters {
                    mode: LumaDeblockingMode::AllEdges,
                    index_a: 26,
                    index_b: 30,
                },
                true,
                LumaEdgeSamples {
                    p0: 101,
                    p1: 100,
                    p2: 98,
                    q0: 103,
                    q1: 104,
                    q2: 105,
                },
            ),
            (
                0,
                LumaDeblockingParameters {
                    mode: LumaDeblockingMode::AllEdges,
                    index_a: 26,
                    index_b: 30,
                },
                false,
                unchanged,
            ),
        ];
        for (boundary_strength, parameters, slice_boundary, expected) in vectors {
            assert_eq!(
                apply_luma_deblocking_edge(samples, boundary_strength, parameters, slice_boundary)
                    .unwrap(),
                expected
            );
        }
        assert!(apply_luma_deblocking_edge(
            samples,
            5,
            LumaDeblockingParameters {
                mode: LumaDeblockingMode::AllEdges,
                index_a: 0,
                index_b: 0,
            },
            false,
        )
        .is_err());
    }

    #[test]
    fn applies_luma_deblocking_edge_segment_in_order_and_honors_mode() {
        let samples = [
            LumaStrongEdgeSamples {
                p0: 100,
                p1: 98,
                p2: 96,
                p3: 95,
                q0: 104,
                q1: 105,
                q2: 106,
                q3: 107,
            },
            LumaStrongEdgeSamples {
                p0: 100,
                p1: 99,
                p2: 98,
                p3: 97,
                q0: 102,
                q1: 103,
                q2: 130,
                q3: 120,
            },
            LumaStrongEdgeSamples {
                p0: 100,
                p1: 98,
                p2: 96,
                p3: 95,
                q0: 120,
                q1: 121,
                q2: 122,
                q3: 123,
            },
            LumaStrongEdgeSamples {
                p0: 100,
                p1: 98,
                p2: 96,
                p3: 95,
                q0: 104,
                q1: 105,
                q2: 106,
                q3: 107,
            },
        ];
        let parameters = LumaDeblockingParameters {
            mode: LumaDeblockingMode::AllExceptSliceBoundaries,
            index_a: 26,
            index_b: 30,
        };
        let expected = [
            LumaEdgeSamples {
                p0: 101,
                p1: 100,
                p2: 98,
                q0: 103,
                q1: 104,
                q2: 105,
            },
            LumaEdgeSamples {
                p0: 100,
                p1: 100,
                p2: 99,
                q0: 102,
                q1: 103,
                q2: 130,
            },
            LumaEdgeSamples {
                p0: 100,
                p1: 98,
                p2: 96,
                q0: 120,
                q1: 121,
                q2: 122,
            },
            LumaEdgeSamples {
                p0: 101,
                p1: 100,
                p2: 98,
                q0: 103,
                q1: 104,
                q2: 105,
            },
        ];
        assert_eq!(
            apply_luma_deblocking_edge_segment(samples, 4, parameters, false).unwrap(),
            expected
        );
        let skipped = apply_luma_deblocking_edge_segment(samples, 4, parameters, true).unwrap();
        for (actual, original) in skipped.iter().zip(samples) {
            assert_eq!(
                *actual,
                LumaEdgeSamples {
                    p0: original.p0,
                    p1: original.p1,
                    p2: original.p2,
                    q0: original.q0,
                    q1: original.q1,
                    q2: original.q2,
                }
            );
        }
    }

    #[test]
    fn applies_luma_deblocking_plane_edge_segment_with_stride_and_bounds() {
        let inputs = [
            LumaStrongEdgeSamples {
                p0: 100,
                p1: 98,
                p2: 96,
                p3: 95,
                q0: 104,
                q1: 105,
                q2: 106,
                q3: 107,
            },
            LumaStrongEdgeSamples {
                p0: 100,
                p1: 99,
                p2: 98,
                p3: 97,
                q0: 102,
                q1: 103,
                q2: 130,
                q3: 120,
            },
            LumaStrongEdgeSamples {
                p0: 100,
                p1: 98,
                p2: 96,
                p3: 95,
                q0: 120,
                q1: 121,
                q2: 122,
                q3: 123,
            },
            LumaStrongEdgeSamples {
                p0: 100,
                p1: 98,
                p2: 96,
                p3: 95,
                q0: 104,
                q1: 105,
                q2: 106,
                q3: 107,
            },
        ];
        let expected = [
            LumaEdgeSamples {
                p0: 101,
                p1: 100,
                p2: 98,
                q0: 103,
                q1: 104,
                q2: 105,
            },
            LumaEdgeSamples {
                p0: 100,
                p1: 100,
                p2: 99,
                q0: 102,
                q1: 103,
                q2: 130,
            },
            LumaEdgeSamples {
                p0: 100,
                p1: 98,
                p2: 96,
                q0: 120,
                q1: 121,
                q2: 122,
            },
            LumaEdgeSamples {
                p0: 101,
                p1: 100,
                p2: 98,
                q0: 103,
                q1: 104,
                q2: 105,
            },
        ];
        let parameters = LumaDeblockingParameters {
            mode: LumaDeblockingMode::AllEdges,
            index_a: 26,
            index_b: 30,
        };

        for vertical in [true, false] {
            let (width, height, stride) = (12, 12, 15);
            let (x, y) = if vertical { (6, 2) } else { (2, 6) };
            let mut plane = vec![250; stride * height];
            for (lane, samples) in inputs.iter().enumerate() {
                let q0_index = if vertical {
                    (y + lane) * stride + x
                } else {
                    y * stride + x + lane
                };
                if vertical {
                    plane[q0_index - 4..=q0_index + 3].copy_from_slice(&[
                        samples.p3, samples.p2, samples.p1, samples.p0, samples.q0, samples.q1,
                        samples.q2, samples.q3,
                    ]);
                } else {
                    plane[q0_index - 4 * stride] = samples.p3;
                    plane[q0_index - 3 * stride] = samples.p2;
                    plane[q0_index - 2 * stride] = samples.p1;
                    plane[q0_index - stride] = samples.p0;
                    plane[q0_index] = samples.q0;
                    plane[q0_index + stride] = samples.q1;
                    plane[q0_index + 2 * stride] = samples.q2;
                    plane[q0_index + 3 * stride] = samples.q3;
                }
            }

            apply_luma_deblocking_plane_edge_segment(
                &mut plane, width, height, stride, x, y, vertical, 4, parameters, false,
            )
            .unwrap();
            for (lane, expected_edge) in expected.iter().enumerate() {
                let q0_index = if vertical {
                    (y + lane) * stride + x
                } else {
                    y * stride + x + lane
                };
                let actual = if vertical {
                    LumaEdgeSamples {
                        p0: plane[q0_index - 1],
                        p1: plane[q0_index - 2],
                        p2: plane[q0_index - 3],
                        q0: plane[q0_index],
                        q1: plane[q0_index + 1],
                        q2: plane[q0_index + 2],
                    }
                } else {
                    LumaEdgeSamples {
                        p0: plane[q0_index - stride],
                        p1: plane[q0_index - 2 * stride],
                        p2: plane[q0_index - 3 * stride],
                        q0: plane[q0_index],
                        q1: plane[q0_index + stride],
                        q2: plane[q0_index + 2 * stride],
                    }
                };
                assert_eq!(actual, *expected_edge);
            }
            for row in 0..height {
                assert!(plane[row * stride + width..(row + 1) * stride]
                    .iter()
                    .all(|sample| *sample == 250));
            }
            let before = plane.clone();
            assert!(apply_luma_deblocking_plane_edge_segment(
                &mut plane, width, height, stride, 3, 2, true, 4, parameters, false,
            )
            .is_err());
            assert_eq!(plane, before);
        }
    }

    #[test]
    fn applies_luma_deblocking_plane_macroblock_edge_with_segment_strengths() {
        let samples = LumaStrongEdgeSamples {
            p0: 100,
            p1: 98,
            p2: 96,
            p3: 95,
            q0: 104,
            q1: 105,
            q2: 106,
            q3: 107,
        };
        let parameters = LumaDeblockingParameters {
            mode: LumaDeblockingMode::AllEdges,
            index_a: 33,
            index_b: 40,
        };
        let strengths = [4, 0, 2, 0];
        let expected_by_strength = [
            LumaEdgeSamples {
                p0: 101,
                p1: 100,
                p2: 98,
                q0: 103,
                q1: 104,
                q2: 105,
            },
            LumaEdgeSamples {
                p0: 100,
                p1: 98,
                p2: 96,
                q0: 104,
                q1: 105,
                q2: 106,
            },
            LumaEdgeSamples {
                p0: 101,
                p1: 99,
                p2: 96,
                q0: 103,
                q1: 104,
                q2: 106,
            },
        ];

        for vertical in [true, false] {
            let (width, height, stride) = (24, 24, 27);
            let (x, y) = if vertical { (8, 2) } else { (2, 8) };
            let mut plane = vec![250; stride * height];
            for lane in 0..16 {
                let q0_index = if vertical {
                    (y + lane) * stride + x
                } else {
                    y * stride + x + lane
                };
                if vertical {
                    plane[q0_index - 4..=q0_index + 3].copy_from_slice(&[
                        samples.p3, samples.p2, samples.p1, samples.p0, samples.q0, samples.q1,
                        samples.q2, samples.q3,
                    ]);
                } else {
                    plane[q0_index - 4 * stride] = samples.p3;
                    plane[q0_index - 3 * stride] = samples.p2;
                    plane[q0_index - 2 * stride] = samples.p1;
                    plane[q0_index - stride] = samples.p0;
                    plane[q0_index] = samples.q0;
                    plane[q0_index + stride] = samples.q1;
                    plane[q0_index + 2 * stride] = samples.q2;
                    plane[q0_index + 3 * stride] = samples.q3;
                }
            }
            apply_luma_deblocking_plane_macroblock_edge(
                &mut plane, width, height, stride, x, y, vertical, strengths, parameters, false,
            )
            .unwrap();
            for lane in 0..16 {
                let q0_index = if vertical {
                    (y + lane) * stride + x
                } else {
                    y * stride + x + lane
                };
                let actual = if vertical {
                    LumaEdgeSamples {
                        p0: plane[q0_index - 1],
                        p1: plane[q0_index - 2],
                        p2: plane[q0_index - 3],
                        q0: plane[q0_index],
                        q1: plane[q0_index + 1],
                        q2: plane[q0_index + 2],
                    }
                } else {
                    LumaEdgeSamples {
                        p0: plane[q0_index - stride],
                        p1: plane[q0_index - 2 * stride],
                        p2: plane[q0_index - 3 * stride],
                        q0: plane[q0_index],
                        q1: plane[q0_index + stride],
                        q2: plane[q0_index + 2 * stride],
                    }
                };
                let expected_index = match strengths[lane / 4] {
                    4 => 0,
                    0 => 1,
                    2 => 2,
                    _ => unreachable!(),
                };
                assert_eq!(actual, expected_by_strength[expected_index]);
            }

            let before = plane.clone();
            assert!(apply_luma_deblocking_plane_macroblock_edge(
                &mut plane,
                width,
                height,
                stride,
                x,
                y,
                vertical,
                [4, 0, 5, 0],
                parameters,
                false,
            )
            .is_err());
            assert_eq!(plane, before);
        }
    }

    #[test]
    fn applies_luma_deblocking_macroblock_in_normative_order() {
        let (width, height, stride) = (32, 32, 35);
        let parameters = LumaDeblockingParameters {
            mode: LumaDeblockingMode::AllEdges,
            index_a: 40,
            index_b: 40,
        };
        let macroblock = LumaDeblockingMacroblock {
            x: 8,
            y: 8,
            left_neighbor_available: true,
            top_neighbor_available: true,
            transform_size_8x8: false,
            left_slice_boundary: false,
            top_slice_boundary: false,
            left_strengths: [4, 3, 2, 1],
            top_strengths: [1, 2, 3, 4],
            vertical_internal_strengths: [[1, 2, 3, 4], [4, 3, 2, 1], [2, 1, 4, 3]],
            horizontal_internal_strengths: [[4, 1, 3, 2], [2, 4, 1, 3], [3, 2, 4, 1]],
        };
        let make_plane = || {
            let mut plane = vec![251; stride * height];
            for row in 0..height {
                for column in 0..width {
                    plane[row * stride + column] =
                        ((column * 7 + row * 11 + column * row) % 180 + 30) as u8;
                }
            }
            plane
        };

        for transform_size_8x8 in [false, true] {
            let macroblock = LumaDeblockingMacroblock {
                transform_size_8x8,
                ..macroblock
            };
            let mut actual = make_plane();
            let mut expected = actual.clone();
            apply_luma_deblocking_plane_macroblock_edge(
                &mut expected,
                width,
                height,
                stride,
                macroblock.x,
                macroblock.y,
                true,
                macroblock.left_strengths,
                parameters,
                false,
            )
            .unwrap();
            for edge in 0..3 {
                if !transform_size_8x8 || edge == 1 {
                    apply_luma_deblocking_plane_macroblock_edge(
                        &mut expected,
                        width,
                        height,
                        stride,
                        macroblock.x + 4 * (edge + 1),
                        macroblock.y,
                        true,
                        macroblock.vertical_internal_strengths[edge],
                        parameters,
                        false,
                    )
                    .unwrap();
                }
            }
            apply_luma_deblocking_plane_macroblock_edge(
                &mut expected,
                width,
                height,
                stride,
                macroblock.x,
                macroblock.y,
                false,
                macroblock.top_strengths,
                parameters,
                false,
            )
            .unwrap();
            for edge in 0..3 {
                if !transform_size_8x8 || edge == 1 {
                    apply_luma_deblocking_plane_macroblock_edge(
                        &mut expected,
                        width,
                        height,
                        stride,
                        macroblock.x,
                        macroblock.y + 4 * (edge + 1),
                        false,
                        macroblock.horizontal_internal_strengths[edge],
                        parameters,
                        false,
                    )
                    .unwrap();
                }
            }
            apply_luma_deblocking_macroblock(
                &mut actual,
                width,
                height,
                stride,
                macroblock,
                parameters,
            )
            .unwrap();
            assert_eq!(actual, expected);

            let before = actual.clone();
            let mut invalid = macroblock;
            invalid.horizontal_internal_strengths[2][3] = 5;
            assert!(apply_luma_deblocking_macroblock(
                &mut actual,
                width,
                height,
                stride,
                invalid,
                parameters,
            )
            .is_err());
            assert_eq!(actual, before);
        }
    }

    #[test]
    fn derives_luma_deblocking_neighbors_from_raster_address_and_slice_ids() {
        let slice_ids = [1, 1, 2, 3];
        let expected = [
            LumaDeblockingNeighbors {
                left_index: None,
                top_index: None,
                left_available: false,
                top_available: false,
                left_slice_boundary: false,
                top_slice_boundary: false,
            },
            LumaDeblockingNeighbors {
                left_index: Some(0),
                top_index: None,
                left_available: true,
                top_available: false,
                left_slice_boundary: false,
                top_slice_boundary: false,
            },
            LumaDeblockingNeighbors {
                left_index: None,
                top_index: Some(0),
                left_available: false,
                top_available: true,
                left_slice_boundary: false,
                top_slice_boundary: true,
            },
            LumaDeblockingNeighbors {
                left_index: Some(2),
                top_index: Some(1),
                left_available: true,
                top_available: true,
                left_slice_boundary: true,
                top_slice_boundary: true,
            },
        ];
        for (index, expected_neighbors) in expected.into_iter().enumerate() {
            assert_eq!(
                derive_luma_deblocking_neighbors(index, 2, 2, &slice_ids).unwrap(),
                expected_neighbors
            );
        }
        assert!(derive_luma_deblocking_neighbors(4, 2, 2, &slice_ids).is_err());
        assert!(derive_luma_deblocking_neighbors(0, 2, 2, &slice_ids[..3]).is_err());
    }

    #[test]
    fn resolves_luma_deblocking_macroblock_position_and_neighbors() {
        let template = LumaDeblockingMacroblock {
            x: 99,
            y: 99,
            left_neighbor_available: false,
            top_neighbor_available: false,
            transform_size_8x8: true,
            left_slice_boundary: false,
            top_slice_boundary: false,
            left_strengths: [1, 2, 3, 4],
            top_strengths: [0; 4],
            vertical_internal_strengths: [[0; 4]; 3],
            horizontal_internal_strengths: [[0; 4]; 3],
        };
        let slice_ids = [1, 1, 2, 3];
        let origin = resolve_luma_deblocking_macroblock(0, 2, 2, &slice_ids, template).unwrap();
        assert_eq!((origin.x, origin.y), (0, 0));
        assert!(!origin.left_neighbor_available && !origin.top_neighbor_available);
        let last = resolve_luma_deblocking_macroblock(3, 2, 2, &slice_ids, template).unwrap();
        assert_eq!((last.x, last.y), (16, 16));
        assert!(last.left_neighbor_available && last.top_neighbor_available);
        assert!(last.left_slice_boundary && last.top_slice_boundary);
        assert_eq!(last.left_strengths, template.left_strengths);
    }

    #[test]
    fn derives_luma_macroblock_edge_flags_from_mode_and_neighbor_availability() {
        let vectors = [
            (
                LumaDeblockingMode::AllEdges,
                true,
                true,
                true,
                true,
                LumaDeblockingEdgeFlags {
                    filter_left: true,
                    filter_top: true,
                    filter_internal: true,
                },
            ),
            (
                LumaDeblockingMode::Disabled,
                true,
                true,
                false,
                false,
                LumaDeblockingEdgeFlags::default(),
            ),
            (
                LumaDeblockingMode::AllExceptSliceBoundaries,
                true,
                true,
                false,
                false,
                LumaDeblockingEdgeFlags {
                    filter_left: true,
                    filter_top: true,
                    filter_internal: true,
                },
            ),
            (
                LumaDeblockingMode::AllExceptSliceBoundaries,
                true,
                true,
                true,
                false,
                LumaDeblockingEdgeFlags {
                    filter_left: false,
                    filter_top: true,
                    filter_internal: true,
                },
            ),
            (
                LumaDeblockingMode::AllEdges,
                false,
                false,
                false,
                false,
                LumaDeblockingEdgeFlags {
                    filter_left: false,
                    filter_top: false,
                    filter_internal: true,
                },
            ),
        ];
        for (mode, left_available, top_available, left_slice, top_slice, expected) in vectors {
            assert_eq!(
                derive_luma_deblocking_edge_flags(
                    mode,
                    left_available,
                    top_available,
                    left_slice,
                    top_slice,
                ),
                expected
            );
        }
    }

    #[test]
    fn derives_luma_boundary_strength_with_normative_priority() {
        let vectors = [
            (true, true, true, true, 4),
            (false, true, false, false, 3),
            (false, false, true, true, 2),
            (false, false, false, true, 1),
            (false, false, false, false, 0),
        ];
        for (macroblock_edge, either_intra, coefficients, inter_differs, expected) in vectors {
            assert_eq!(
                derive_luma_boundary_strength(
                    macroblock_edge,
                    either_intra,
                    coefficients,
                    inter_differs,
                ),
                expected
            );
        }
    }

    #[test]
    fn compares_luma_inter_prediction_references_and_motion_vectors() {
        let vector = |reference_picture_id, x, y| LumaPredictionVector {
            reference_picture_id,
            motion_vector: MotionVector { x, y },
        };
        let cases = [
            (vec![], vec![], false),
            (vec![vector(7, -3, 3)], vec![vector(7, 0, 0)], false),
            (vec![vector(7, 0, 0)], vec![vector(8, 0, 0)], true),
            (vec![vector(7, 0, 0)], vec![vector(7, 4, 0)], true),
            (vec![vector(7, 0, 0)], vec![vector(7, 0, -4)], true),
            (
                vec![vector(7, 12, 0), vector(8, 0, 20)],
                vec![vector(8, 0, 22), vector(7, 10, 0)],
                false,
            ),
            (
                vec![vector(7, 0, 0), vector(8, 0, 0)],
                vec![vector(8, 0, 0), vector(7, 0, 4)],
                true,
            ),
            (vec![vector(7, 0, 0)], vec![], true),
        ];
        for (prediction_p, prediction_q, expected) in cases {
            assert_eq!(
                luma_inter_prediction_differs(&prediction_p, &prediction_q).unwrap(),
                expected
            );
        }
        let too_many = [vector(1, 0, 0), vector(2, 0, 0), vector(3, 0, 0)];
        assert!(luma_inter_prediction_differs(&too_many, &[]).is_err());
    }

    #[test]
    fn derives_luma_boundary_strength_from_predictions() {
        let prediction = |reference_picture_id, x, y| LumaPredictionVector {
            reference_picture_id,
            motion_vector: MotionVector { x, y },
        };
        let cases = [
            (
                true,
                true,
                false,
                vec![prediction(1, 0, 0)],
                vec![prediction(2, 4, 0)],
                4,
            ),
            (
                false,
                false,
                true,
                vec![prediction(1, 0, 0)],
                vec![prediction(2, 4, 0)],
                2,
            ),
            (
                false,
                false,
                false,
                vec![prediction(1, 0, 0)],
                vec![prediction(2, 0, 0)],
                1,
            ),
            (
                false,
                false,
                false,
                vec![prediction(1, 0, 0)],
                vec![prediction(1, 3, -3)],
                0,
            ),
        ];
        for (macroblock_edge, either_intra, coefficients, prediction_p, prediction_q, expected) in
            cases
        {
            assert_eq!(
                derive_luma_boundary_strength_from_predictions(
                    macroblock_edge,
                    either_intra,
                    coefficients,
                    &prediction_p,
                    &prediction_q,
                )
                .unwrap(),
                expected
            );
        }
        let too_many = vec![
            prediction(1, 0, 0),
            prediction(2, 0, 0),
            prediction(3, 0, 0),
        ];
        assert!(derive_luma_boundary_strength_from_predictions(
            false,
            false,
            false,
            &too_many,
            &[]
        )
        .is_err());
    }

    #[test]
    fn rejects_invalid_luma_deblocking_parameters() {
        for (qp_p, qp_q, disable_idc, alpha_offset, beta_offset) in [
            (-1, 26, 0, 0, 0),
            (26, 52, 0, 0, 0),
            (26, 26, 3, 0, 0),
            (26, 26, 0, -7, 0),
            (26, 26, 0, 7, 0),
            (26, 26, 0, 0, -7),
            (26, 26, 0, 0, 7),
        ] {
            assert!(derive_luma_deblocking_parameters(
                qp_p,
                qp_q,
                disable_idc,
                alpha_offset,
                beta_offset
            )
            .is_err());
        }
    }

    #[test]
    fn presentation_order_buffer_is_stable_owns_frames_and_rejects_invalid() {
        let frame = Yuv420Frame {
            width: 2,
            height: 2,
            y_stride: 2,
            u_stride: 1,
            v_stride: 1,
            y: vec![1, 2, 3, 4],
            u: vec![5],
            v: vec![6],
        };
        let mut buffer = PresentationOrderBuffer::new(1);
        let first = buffer
            .push(&PresentationPicture {
                picture_order_cnt: 3,
                frame: frame.clone(),
            })
            .unwrap();
        assert!(first.is_none());
        let mut changed_frame = frame.clone();
        changed_frame.y[0] = 99;
        let second = buffer
            .push(&PresentationPicture {
                picture_order_cnt: 3,
                frame: changed_frame,
            })
            .unwrap()
            .unwrap();
        assert_eq!(second.frame.y[0], 1);
        assert_eq!(buffer.drain()[0].frame.y[0], 99);

        let mut buffer = PresentationOrderBuffer::new(0);
        let invalid_frame = Yuv420Frame {
            y: Vec::new(),
            ..frame
        };
        assert!(buffer
            .push(&PresentationPicture {
                picture_order_cnt: 0,
                frame: invalid_frame,
            })
            .is_err());
    }

    #[test]
    fn reference_picture_buffer_owns_frames_and_preserves_insertion_order() {
        let mut buffer = ReferencePictureBuffer::default();
        let frame = Yuv420Frame {
            width: 3,
            height: 3,
            y_stride: 4,
            u_stride: 3,
            v_stride: 3,
            y: vec![1, 2, 3, 99, 4, 5, 6, 99, 7, 8, 9],
            u: vec![10, 11, 99, 12, 13],
            v: vec![20, 21, 99, 22, 23],
        };
        let first = ReferencePicture {
            identifier: 1,
            frame_num: 4,
            picture_order_cnt: 0,
            long_term_frame_idx: None,
        };
        let second = ReferencePicture {
            identifier: 2,
            frame_num: 5,
            picture_order_cnt: 1,
            long_term_frame_idx: None,
        };
        buffer.store(first, &frame).unwrap();
        buffer.store(second, &frame).unwrap();

        let mut returned = buffer.get(first.identifier).unwrap();
        returned.frame.y[0] = 50;
        assert_eq!(buffer.get(first.identifier).unwrap().frame.y[0], 1);
        assert_eq!(buffer.references(), vec![first, second]);
        assert!(buffer.remove(first.identifier));
        assert!(!buffer.remove(first.identifier));
    }

    #[test]
    fn reference_picture_buffer_replaces_and_rejects_invalid_frames() {
        let mut buffer = ReferencePictureBuffer::default();
        let reference = ReferencePicture {
            identifier: 1,
            frame_num: 1,
            picture_order_cnt: 0,
            long_term_frame_idx: None,
        };
        let frame = Yuv420Frame {
            width: 2,
            height: 2,
            y_stride: 2,
            u_stride: 1,
            v_stride: 1,
            y: vec![1, 2, 3, 4],
            u: vec![5],
            v: vec![6],
        };
        buffer.store(reference, &frame).unwrap();
        let replacement = ReferencePicture {
            frame_num: 2,
            ..reference
        };
        let replacement_frame = Yuv420Frame {
            y: vec![7, 8, 9, 10],
            ..frame.clone()
        };
        buffer.store(replacement, &replacement_frame).unwrap();
        let stored = buffer.get(reference.identifier).unwrap();
        assert_eq!(stored.reference, replacement);
        assert_eq!(stored.frame.y[0], 7);
        assert_eq!(buffer.references(), vec![replacement]);

        let invalid_frame = Yuv420Frame {
            y: vec![1],
            ..frame
        };
        assert_eq!(
            buffer
                .store(replacement, &invalid_frame)
                .unwrap_err()
                .kind(),
            ErrorKind::InvalidInput
        );
    }

    #[test]
    fn interpolates_horizontal_luma_half_sample_with_rounding_and_clipping() {
        for (samples, expected) in [
            ([10, 40, 80, 120, 200, 240], 95),
            ([0, 255, 0, 0, 0, 0], 0),
            ([255, 255, 255, 255, 0, 0], 255),
        ] {
            assert_eq!(interpolate_luma_half_sample_horizontal(&samples), expected);
        }
    }

    #[test]
    fn interpolates_vertical_luma_half_sample_with_rounding_and_clipping() {
        for (samples, expected) in [
            ([10, 40, 80, 120, 200, 240], 95),
            ([0, 255, 0, 0, 0, 0], 0),
            ([255, 255, 255, 255, 0, 0], 255),
        ] {
            assert_eq!(interpolate_luma_half_sample_vertical(&samples), expected);
        }
    }

    #[test]
    fn interpolates_diagonal_luma_half_sample_without_intermediate_rounding() {
        let mut centered_impulse = [[0_u8; 6]; 6];
        centered_impulse[2][2] = 255;
        let mut negative_impulse = [[0_u8; 6]; 6];
        negative_impulse[2][1] = 255;
        let mut high_overshoot = [[0_u8; 6]; 6];
        for row in [0, 2, 3, 5] {
            high_overshoot[row].fill(255);
        }

        for (samples, expected) in [
            (centered_impulse, 100),
            (negative_impulse, 0),
            (high_overshoot, 255),
        ] {
            assert_eq!(interpolate_luma_half_sample_diagonal(&samples), expected);
        }
    }

    #[test]
    fn averages_luma_quarter_sample_with_upward_rounding() {
        for (first, second, expected) in
            [(10, 20, 15), (10, 21, 16), (0, 255, 128), (255, 255, 255)]
        {
            assert_eq!(
                interpolate_luma_quarter_sample_average(first, second),
                expected
            );
        }
    }

    #[test]
    fn interpolates_quarter_sample_pair_from_adjacent_integer_samples() {
        for (first, half, second, expected) in [(10, 21, 30, [16, 26]), (0, 255, 0, [128, 128])] {
            assert_eq!(
                interpolate_luma_quarter_sample_pair(first, half, second),
                expected
            );
        }
    }

    #[test]
    fn interpolates_diagonal_quarter_sample_positions_from_half_samples() {
        assert_eq!(
            interpolate_luma_quarter_sample_diagonal(10, 21, 30, 41),
            [16, 20, 31, 36]
        );
    }

    #[test]
    fn interpolates_axial_quarter_sample_positions_from_integer_and_half_samples() {
        assert_eq!(
            interpolate_luma_quarter_sample_axial(10, 30, 40, 21, 25),
            [16, 26, 18, 33]
        );
    }

    #[test]
    fn interpolates_quarter_sample_positions_around_j() {
        assert_eq!(
            interpolate_luma_quarter_sample_around_j(10, 21, 30, 40, 50),
            [20, 26, 35, 40]
        );
    }

    #[test]
    fn selects_luma_fractional_sample_by_x_then_y_offset() {
        let samples = [
            [10, 11, 12, 13],
            [20, 21, 22, 23],
            [30, 31, 32, 33],
            [40, 41, 42, 43],
        ];
        for (x_frac_l, row) in samples.iter().enumerate() {
            for (y_frac_l, expected) in row.iter().enumerate() {
                assert_eq!(
                    select_luma_fractional_sample(&samples, x_frac_l as u8, y_frac_l as u8)
                        .unwrap(),
                    *expected
                );
            }
        }
    }

    #[test]
    fn rejects_luma_fractional_sample_offsets_outside_table() {
        let samples = [[0_u8; 4]; 4];
        for (x_frac_l, y_frac_l) in [(4, 0), (0, 4)] {
            let error = select_luma_fractional_sample(&samples, x_frac_l, y_frac_l).unwrap_err();
            assert_eq!(error.kind(), std::io::ErrorKind::InvalidInput);
            assert_eq!(
                error.to_string(),
                "luma fractional-sample offset is outside [0,3]"
            );
        }
    }

    #[test]
    fn interpolates_quarter_sample_grid_for_linear_plane() {
        let mut samples = [[0_u8; 6]; 6];
        for (row, row_samples) in samples.iter_mut().enumerate() {
            for (column, sample) in row_samples.iter_mut().enumerate() {
                *sample = (10 * row + 5 * column) as u8;
            }
        }
        assert_eq!(
            interpolate_luma_quarter_sample_grid(&samples),
            [
                [30, 33, 35, 38],
                [32, 34, 37, 39],
                [33, 36, 38, 41],
                [34, 37, 39, 42],
            ]
        );
    }

    #[test]
    fn gathers_luma_neighborhood_with_stride_and_padding() {
        let (width, height, stride) = (8, 8, 10);
        let mut plane = vec![250_u8; stride * height];
        for row in 0..height {
            for column in 0..width {
                plane[row * stride + column] = (row * width + column) as u8;
            }
        }
        let neighborhood =
            gather_luma_quarter_sample_neighborhood(&plane, width, height, stride, 3, 3).unwrap();
        for (row, sample_row) in neighborhood.iter().enumerate() {
            for (column, sample) in sample_row.iter().enumerate() {
                assert_eq!(*sample, ((row + 1) * width + column + 1) as u8);
            }
        }
    }

    #[test]
    fn gathers_luma_neighborhood_with_edge_clipping_and_ignores_padding() {
        let plane = [1, 2, 99, 3, 4, 99];
        let neighborhood = gather_luma_quarter_sample_neighborhood(&plane, 2, 2, 3, 0, 0).unwrap();
        for row in 0..6 {
            let expected = if row < 3 {
                [1, 1, 1, 2, 2, 2]
            } else {
                [3, 3, 3, 4, 4, 4]
            };
            assert_eq!(neighborhood[row], expected);
        }
        let far_edge =
            gather_luma_quarter_sample_neighborhood(&plane, 2, 2, 3, isize::MAX, isize::MIN)
                .unwrap();
        assert_eq!(far_edge, [[2; 6]; 6]);
    }

    #[test]
    fn rejects_invalid_luma_reference_plane_layouts() {
        for (plane, width, height, stride) in [
            (vec![1], 0, 1, 1),
            (vec![1], 2, 1, 1),
            (vec![1, 2, 3], 2, 2, 2),
            (vec![1], usize::MAX, 2, usize::MAX),
        ] {
            let error =
                gather_luma_quarter_sample_neighborhood(&plane, width, height, stride, 0, 0)
                    .unwrap_err();
            assert_eq!(error.kind(), ErrorKind::InvalidInput);
            assert_eq!(
                error.to_string(),
                "luma reference plane layout is invalid or truncated"
            );
        }
    }

    #[test]
    fn interpolates_fractional_luma_sample_from_reference_plane() {
        let (width, height, stride) = (6, 6, 6);
        let mut plane = vec![0_u8; stride * height];
        for row in 0..height {
            for column in 0..width {
                plane[row * stride + column] = (10 * row + 5 * column) as u8;
            }
        }
        let expected = [
            [30, 33, 35, 38],
            [32, 34, 37, 39],
            [33, 36, 38, 41],
            [34, 37, 39, 42],
        ];
        for (x_frac_l, row) in expected.iter().enumerate() {
            for (y_frac_l, sample) in row.iter().enumerate() {
                assert_eq!(
                    interpolate_luma_fractional_sample(
                        &plane,
                        width,
                        height,
                        stride,
                        2,
                        2,
                        x_frac_l as u8,
                        y_frac_l as u8,
                    )
                    .unwrap(),
                    *sample
                );
            }
        }
    }

    #[test]
    fn propagates_fractional_luma_offset_and_plane_layout_errors() {
        let plane = [0_u8; 36];
        let offset_error =
            interpolate_luma_fractional_sample(&plane, 6, 6, 6, 2, 2, 4, 0).unwrap_err();
        assert_eq!(offset_error.kind(), ErrorKind::InvalidInput);
        assert_eq!(
            offset_error.to_string(),
            "luma fractional-sample offset is outside [0,3]"
        );

        let layout_error =
            interpolate_luma_fractional_sample(&[], 6, 6, 6, 2, 2, 0, 0).unwrap_err();
        assert_eq!(layout_error.kind(), ErrorKind::InvalidInput);
        assert_eq!(
            layout_error.to_string(),
            "luma reference plane layout is invalid or truncated"
        );
    }

    #[test]
    fn applies_reference_list_modifications_with_pic_num_wrap_and_long_term_index() {
        let references = [
            ReferencePicture {
                identifier: 1,
                frame_num: 1,
                picture_order_cnt: 0,
                long_term_frame_idx: None,
            },
            ReferencePicture {
                identifier: 2,
                frame_num: 3,
                picture_order_cnt: 0,
                long_term_frame_idx: None,
            },
            ReferencePicture {
                identifier: 3,
                frame_num: 5,
                picture_order_cnt: 0,
                long_term_frame_idx: None,
            },
            ReferencePicture {
                identifier: 4,
                frame_num: 0,
                picture_order_cnt: 0,
                long_term_frame_idx: None,
            },
            ReferencePicture {
                identifier: 5,
                frame_num: 0,
                picture_order_cnt: 0,
                long_term_frame_idx: Some(4),
            },
            ReferencePicture {
                identifier: 6,
                frame_num: 0,
                picture_order_cnt: 0,
                long_term_frame_idx: Some(1),
            },
        ];
        let initial = build_p_reference_list(&references, 2, 16).unwrap();
        let modifications = [
            RefPicListModification {
                modification_of_pic_nums_idc: 0,
                value: 1,
            },
            RefPicListModification {
                modification_of_pic_nums_idc: 1,
                value: 0,
            },
            RefPicListModification {
                modification_of_pic_nums_idc: 2,
                value: 1,
            },
        ];
        let ordered =
            apply_reference_list_modifications(&initial, &references, 2, 16, &modifications)
                .unwrap();
        assert_eq!(
            ordered
                .iter()
                .map(|picture| picture.identifier)
                .collect::<Vec<_>>(),
            [4, 1, 6, 3, 2, 5]
        );

        for modification in [
            RefPicListModification {
                modification_of_pic_nums_idc: 0,
                value: 16,
            },
            RefPicListModification {
                modification_of_pic_nums_idc: 2,
                value: 9,
            },
            RefPicListModification {
                modification_of_pic_nums_idc: 3,
                value: 0,
            },
        ] {
            assert!(apply_reference_list_modifications(
                &initial,
                &references,
                2,
                16,
                &[modification],
            )
            .is_err());
        }
    }

    #[test]
    fn builds_p_reference_list_with_wrapped_short_term_and_ordered_long_term_pictures() {
        let pictures = [
            ReferencePicture {
                identifier: 4,
                frame_num: 5,
                picture_order_cnt: 0,
                long_term_frame_idx: None,
            },
            ReferencePicture {
                identifier: 6,
                frame_num: 0,
                picture_order_cnt: 0,
                long_term_frame_idx: Some(1),
            },
            ReferencePicture {
                identifier: 2,
                frame_num: 1,
                picture_order_cnt: 0,
                long_term_frame_idx: None,
            },
            ReferencePicture {
                identifier: 5,
                frame_num: 0,
                picture_order_cnt: 0,
                long_term_frame_idx: Some(4),
            },
            ReferencePicture {
                identifier: 3,
                frame_num: 15,
                picture_order_cnt: 0,
                long_term_frame_idx: None,
            },
            ReferencePicture {
                identifier: 1,
                frame_num: 3,
                picture_order_cnt: 0,
                long_term_frame_idx: None,
            },
        ];
        let ordered = build_p_reference_list(&pictures, 2, 16).unwrap();
        assert_eq!(
            ordered
                .iter()
                .map(|picture| picture.identifier)
                .collect::<Vec<_>>(),
            [2, 3, 4, 1, 6, 5]
        );
        assert_eq!(
            build_p_reference_list(&[], 16, 16).unwrap_err().kind(),
            std::io::ErrorKind::InvalidInput
        );
        assert_eq!(
            build_p_reference_list(
                &[ReferencePicture {
                    identifier: 1,
                    frame_num: 16,
                    picture_order_cnt: 0,
                    long_term_frame_idx: None
                }],
                0,
                16,
            )
            .unwrap_err()
            .kind(),
            std::io::ErrorKind::InvalidInput
        );
    }

    #[test]
    fn builds_b_reference_lists_by_poc_and_swaps_identical_lists() {
        let pictures = [
            ReferencePicture {
                identifier: 1,
                frame_num: 0,
                picture_order_cnt: 6,
                long_term_frame_idx: None,
            },
            ReferencePicture {
                identifier: 2,
                frame_num: 0,
                picture_order_cnt: 14,
                long_term_frame_idx: None,
            },
            ReferencePicture {
                identifier: 3,
                frame_num: 0,
                picture_order_cnt: 2,
                long_term_frame_idx: None,
            },
            ReferencePicture {
                identifier: 4,
                frame_num: 0,
                picture_order_cnt: 20,
                long_term_frame_idx: None,
            },
            ReferencePicture {
                identifier: 5,
                frame_num: 0,
                picture_order_cnt: 10,
                long_term_frame_idx: None,
            },
            ReferencePicture {
                identifier: 6,
                frame_num: 0,
                picture_order_cnt: 0,
                long_term_frame_idx: Some(1),
            },
            ReferencePicture {
                identifier: 7,
                frame_num: 0,
                picture_order_cnt: 0,
                long_term_frame_idx: Some(4),
            },
        ];
        let (list0, list1) = build_b_reference_lists(&pictures, 10);
        assert_eq!(
            list0
                .iter()
                .map(|picture| picture.identifier)
                .collect::<Vec<_>>(),
            [5, 1, 3, 2, 4, 6, 7]
        );
        assert_eq!(
            list1
                .iter()
                .map(|picture| picture.identifier)
                .collect::<Vec<_>>(),
            [2, 4, 5, 1, 3, 6, 7]
        );

        let tied = [
            ReferencePicture {
                identifier: 1,
                frame_num: 0,
                picture_order_cnt: 8,
                long_term_frame_idx: None,
            },
            ReferencePicture {
                identifier: 2,
                frame_num: 0,
                picture_order_cnt: 5,
                long_term_frame_idx: None,
            },
        ];
        let (list0, list1) = build_b_reference_lists(&tied, 10);
        assert_eq!(
            list0
                .iter()
                .map(|picture| picture.identifier)
                .collect::<Vec<_>>(),
            [1, 2]
        );
        assert_eq!(
            list1
                .iter()
                .map(|picture| picture.identifier)
                .collect::<Vec<_>>(),
            [2, 1]
        );
    }

    #[test]
    fn mvd_neighbor_magnitudes_sum_absolute_components_and_default_missing_neighbors() {
        let vectors = [
            (None, None, (0, 0)),
            (Some(MotionVector { x: -2, y: 3 }), None, (2, 3)),
            (
                Some(MotionVector { x: -2, y: 3 }),
                Some(MotionVector { x: 4, y: -5 }),
                (6, 8),
            ),
            (
                Some(MotionVector {
                    x: i32::MIN,
                    y: i32::MIN,
                }),
                Some(MotionVector {
                    x: i32::MIN,
                    y: i32::MIN,
                }),
                (1_u64 << 32, 1_u64 << 32),
            ),
        ];
        for (left, top, expected) in vectors {
            assert_eq!(
                motion_vector_difference_neighbor_magnitudes(left, top),
                expected
            );
        }
    }

    #[test]
    fn derives_motion_vector_from_partition_prediction_and_difference() {
        let left = Some(MotionVectorCandidate {
            reference_index: Some(1),
            vector: MotionVector { x: 12, y: 20 },
        });
        let top_left = Some(MotionVectorCandidate {
            reference_index: Some(0),
            vector: MotionVector {
                x: 32760,
                y: -32760,
            },
        });
        assert_eq!(
            derive_motion_vector(
                MotionVectorPartitionShape::EightBySixteen,
                0,
                left,
                None,
                None,
                top_left,
                MotionVector { x: 20, y: -20 },
            ),
            MotionVector {
                x: -32756,
                y: 32756,
            }
        );
    }

    #[test]
    fn motion_vector_partition_shapes_use_their_preferred_matching_neighbors() {
        let candidate = |reference_index, x, y| {
            Some(MotionVectorCandidate {
                reference_index: Some(reference_index),
                vector: MotionVector { x, y },
            })
        };
        let tests = [
            (
                MotionVectorPartitionShape::SixteenByEight,
                candidate(0, 10, 1),
                candidate(0, 20, 2),
                candidate(1, 30, 3),
                None,
                MotionVector { x: 10, y: 1 },
            ),
            (
                MotionVectorPartitionShape::SixteenByEight,
                candidate(1, 10, 1),
                candidate(0, 20, 2),
                candidate(1, 30, 3),
                None,
                MotionVector { x: 20, y: 2 },
            ),
            (
                MotionVectorPartitionShape::EightBySixteen,
                candidate(0, 10, 1),
                candidate(1, 20, 2),
                candidate(0, 30, 3),
                None,
                MotionVector { x: 10, y: 1 },
            ),
            (
                MotionVectorPartitionShape::EightBySixteen,
                candidate(1, 10, 1),
                candidate(1, 20, 2),
                candidate(0, 30, 3),
                None,
                MotionVector { x: 30, y: 3 },
            ),
            (
                MotionVectorPartitionShape::EightBySixteen,
                candidate(1, 10, 1),
                candidate(1, 20, 2),
                None,
                candidate(0, 40, 4),
                MotionVector { x: 40, y: 4 },
            ),
        ];
        for (shape, left, top, top_right, top_left, expected) in tests {
            assert_eq!(
                predict_motion_vector_for_partition(shape, 0, left, top, top_right, top_left),
                expected
            );
        }
    }

    #[test]
    fn applies_motion_vector_difference_with_signed_component_wrapping() {
        let vectors = [
            (
                MotionVector { x: 120, y: -240 },
                MotionVector { x: 8, y: 15 },
                MotionVector { x: 128, y: -225 },
            ),
            (
                MotionVector {
                    x: 32760,
                    y: -32760,
                },
                MotionVector { x: 20, y: -20 },
                MotionVector {
                    x: -32756,
                    y: 32756,
                },
            ),
            (
                MotionVector {
                    x: -32760,
                    y: 32760,
                },
                MotionVector { x: -20, y: 20 },
                MotionVector {
                    x: 32756,
                    y: -32756,
                },
            ),
        ];
        for (predicted, difference, expected) in vectors {
            assert_eq!(
                apply_motion_vector_difference(predicted, difference),
                expected
            );
        }
    }

    #[test]
    fn motion_vector_prediction_matches_reference_selection_and_median_vectors() {
        let tests = [
            (
                2,
                Some(MotionVectorCandidate {
                    reference_index: Some(1),
                    vector: MotionVector { x: 20, y: 0 },
                }),
                Some(MotionVectorCandidate {
                    reference_index: Some(2),
                    vector: MotionVector { x: 7, y: -5 },
                }),
                Some(MotionVectorCandidate {
                    reference_index: Some(0),
                    vector: MotionVector { x: 0, y: 30 },
                }),
                None,
                MotionVector { x: 7, y: -5 },
            ),
            (
                0,
                Some(MotionVectorCandidate {
                    reference_index: Some(1),
                    vector: MotionVector { x: 20, y: 0 },
                }),
                Some(MotionVectorCandidate {
                    reference_index: Some(0),
                    vector: MotionVector { x: 8, y: 10 },
                }),
                Some(MotionVectorCandidate {
                    reference_index: Some(0),
                    vector: MotionVector { x: 2, y: 4 },
                }),
                None,
                MotionVector { x: 8, y: 4 },
            ),
            (
                0,
                Some(MotionVectorCandidate {
                    reference_index: Some(1),
                    vector: MotionVector { x: 3, y: 9 },
                }),
                Some(MotionVectorCandidate {
                    reference_index: Some(2),
                    vector: MotionVector { x: 8, y: 2 },
                }),
                Some(MotionVectorCandidate {
                    reference_index: Some(3),
                    vector: MotionVector { x: 5, y: 6 },
                }),
                None,
                MotionVector { x: 5, y: 6 },
            ),
            (
                3,
                Some(MotionVectorCandidate {
                    reference_index: Some(1),
                    vector: MotionVector { x: 20, y: 0 },
                }),
                Some(MotionVectorCandidate {
                    reference_index: Some(2),
                    vector: MotionVector { x: 30, y: 0 },
                }),
                None,
                Some(MotionVectorCandidate {
                    reference_index: Some(3),
                    vector: MotionVector { x: 11, y: -7 },
                }),
                MotionVector { x: 11, y: -7 },
            ),
            (
                0,
                None,
                Some(MotionVectorCandidate {
                    reference_index: Some(-1),
                    vector: MotionVector { x: 99, y: 99 },
                }),
                Some(MotionVectorCandidate {
                    reference_index: Some(1),
                    vector: MotionVector { x: 9, y: 6 },
                }),
                None,
                MotionVector::default(),
            ),
        ];

        for (current_ref, left, top, top_right, top_left, expected) in tests {
            assert_eq!(
                predict_motion_vector(current_ref, left, top, top_right, top_left),
                expected
            );
        }
    }

    #[test]
    fn qp_vectors_match_other_languages() {
        let levels = [1_i32; 16];
        let scaling_list = [16_u8; 16];
        let vectors = [
            (
                0,
                [
                    10, 13, 10, 13, 13, 16, 13, 16, 10, 13, 10, 13, 13, 16, 13, 16,
                ],
            ),
            (
                24,
                [
                    160, 208, 160, 208, 208, 256, 208, 256, 160, 208, 160, 208, 208, 256, 208, 256,
                ],
            ),
            (
                51,
                [
                    3584, 4608, 3584, 4608, 4608, 5888, 4608, 5888, 3584, 4608, 3584, 4608, 4608,
                    5888, 4608, 5888,
                ],
            ),
        ];

        for (qpy, expected) in vectors {
            assert_eq!(
                inverse_scale_luma4x4(&levels, &scaling_list, qpy).unwrap(),
                expected
            );
        }
    }

    #[test]
    fn inverse_scale_chroma_dc2x2_matches_qpc_vectors() {
        let vectors = [
            ([32, -32, 1, -1], 0, [10, -10, 0, -1]),
            ([1, 2, -1, -2], 5, [0, 1, -1, -2]),
            ([1, 2, -1, -2], 6, [0, 1, -1, -2]),
            ([1, 2, -1, -2], 39, [28, 56, -28, -56]),
            (
                [-(1_i64 << 15), (1_i64 << 15) - 1, 0, 0],
                0,
                [-10240, 10239, 0, 0],
            ),
        ];
        for (transformed, qpc, expected) in vectors {
            assert_eq!(
                inverse_scale_chroma_dc2x2(&transformed, qpc).unwrap(),
                expected
            );
        }
    }

    #[test]
    fn inverse_scale_chroma_dc2x2_rejects_out_of_range_values() {
        for qpc in [-1, 40, 51] {
            assert_eq!(
                inverse_scale_chroma_dc2x2(&[0; 4], qpc).unwrap_err().kind(),
                ErrorKind::InvalidData
            );
        }
        for coefficient in [-(1_i64 << 15) - 1, 1_i64 << 15] {
            assert_eq!(
                inverse_scale_chroma_dc2x2(&[coefficient, 0, 0, 0], 0)
                    .unwrap_err()
                    .kind(),
                ErrorKind::InvalidData
            );
        }
        assert_eq!(
            inverse_scale_chroma_dc2x2(&[(1_i64 << 15) - 1, 0, 0, 0], 39)
                .unwrap_err()
                .kind(),
            ErrorKind::InvalidData
        );
    }

    #[test]
    fn inverse_scale_chroma4x4_preserves_dc_and_scales_ac() {
        let mut levels = [1_i32; 16];
        levels[0] = 7;
        let scaling_list = [16_u8; 16];
        for qpc in [0, 23, 24, 39] {
            let mut luma_levels = levels;
            luma_levels[0] = 1;
            let mut expected = inverse_scale_luma4x4(&luma_levels, &scaling_list, qpc).unwrap();
            expected[0] = 7;
            assert_eq!(
                inverse_scale_chroma4x4(&levels, &scaling_list, qpc).unwrap(),
                expected
            );
        }
    }

    #[test]
    fn inverse_scale_chroma4x4_applies_custom_ac_weights() {
        let mut levels = [0_i32; 16];
        levels[0] = 5;
        levels[1] = -1;
        levels[2] = 1;
        let mut scaling_list = [16_u8; 16];
        scaling_list[1] = 8;
        let mut expected = inverse_scale_luma4x4(&levels, &scaling_list, 0).unwrap();
        expected[0] = 5;
        assert_eq!(
            inverse_scale_chroma4x4(&levels, &scaling_list, 0).unwrap(),
            expected
        );
    }

    #[test]
    fn inverse_scale_chroma4x4_rejects_out_of_range_values() {
        let levels = [0_i32; 16];
        let scaling_list = [16_u8; 16];
        for qpc in [-1, 40, 51] {
            assert_eq!(
                inverse_scale_chroma4x4(&levels, &scaling_list, qpc)
                    .unwrap_err()
                    .kind(),
                ErrorKind::InvalidData
            );
        }
        for level in [-(1_i32 << 15) - 1, 1_i32 << 15] {
            let mut invalid_levels = levels;
            invalid_levels[1] = level;
            assert_eq!(
                inverse_scale_chroma4x4(&invalid_levels, &scaling_list, 0)
                    .unwrap_err()
                    .kind(),
                ErrorKind::InvalidData
            );
        }
        let mut excessive_output = levels;
        excessive_output[1] = (1_i32 << 15) - 1;
        assert_eq!(
            inverse_scale_chroma4x4(&excessive_output, &scaling_list, 39)
                .unwrap_err()
                .kind(),
            ErrorKind::InvalidData
        );
        let mut zero_weight = scaling_list;
        zero_weight[1] = 0;
        assert_eq!(
            inverse_scale_chroma4x4(&levels, &zero_weight, 0)
                .unwrap_err()
                .kind(),
            ErrorKind::InvalidData
        );
    }

    #[test]
    fn reconstruct_chroma4x4_residual_composes_scan_scaling_and_transform() {
        let mut ac_scan_levels = [0_i32; 15];
        ac_scan_levels[0] = 1;
        let scaling_list = [16_u8; 16];
        assert_eq!(
            reconstruct_chroma4x4_residual(0, &ac_scan_levels, &scaling_list, 24).unwrap(),
            [3, 2, -2, -3, 3, 2, -2, -3, 3, 2, -2, -3, 3, 2, -2, -3]
        );
        assert_eq!(
            reconstruct_chroma4x4_residual(64, &[0; 15], &scaling_list, 0).unwrap(),
            [1_i64; 16]
        );
    }

    #[test]
    fn reconstruct_chroma4x4_residual_rejects_invalid_dc_and_qpc() {
        let scaling_list = [16_u8; 16];
        assert_eq!(
            reconstruct_chroma4x4_residual(1_i64 << 15, &[0; 15], &scaling_list, 0)
                .unwrap_err()
                .kind(),
            ErrorKind::InvalidData
        );
        assert_eq!(
            reconstruct_chroma4x4_residual(0, &[0; 15], &scaling_list, 40)
                .unwrap_err()
                .kind(),
            ErrorKind::InvalidData
        );
    }

    #[test]
    fn assembles_chroma420_residual_macroblock_in_block_raster_order() {
        let mut blocks = [[0_i64; 16]; 4];
        for (block_index, block) in blocks.iter_mut().enumerate() {
            for (sample_index, sample) in block.iter_mut().enumerate() {
                *sample = (block_index * 16 + sample_index) as i64;
            }
        }
        assert_eq!(
            assemble_chroma420_residual_macroblock(&blocks),
            [
                0, 1, 2, 3, 16, 17, 18, 19, 4, 5, 6, 7, 20, 21, 22, 23, 8, 9, 10, 11, 24, 25, 26,
                27, 12, 13, 14, 15, 28, 29, 30, 31, 32, 33, 34, 35, 48, 49, 50, 51, 36, 37, 38, 39,
                52, 53, 54, 55, 40, 41, 42, 43, 56, 57, 58, 59, 44, 45, 46, 47, 60, 61, 62, 63,
            ]
        );
    }

    #[test]
    fn reconstructs_chroma420_prediction_plus_residual_with_clip1c() {
        let mut prediction = [0_u8; 64];
        prediction[..4].copy_from_slice(&[10, 250, 100, 100]);
        let mut residual = [0_i64; 64];
        residual[..4].copy_from_slice(&[-20, 20, -25, 25]);
        residual[4] = i64::MAX;
        residual[5] = i64::MIN;
        let mut expected = [0_u8; 64];
        expected[..5].copy_from_slice(&[0, 255, 75, 125, 255]);
        assert_eq!(
            reconstruct_chroma420_macroblock(&prediction, &residual),
            expected
        );
    }

    #[test]
    fn assembles_cropped_yuv420_frame_from_raster_macroblocks() {
        let mut builder = Yuv420FrameBuilder::new(2, 2, 2, 2, 2, 2).unwrap();
        assert!(builder.finish().is_err());
        assert!(builder
            .place_macroblock(4, &[0; 256], &[0; 64], &[0; 64])
            .is_err());
        for (address, values) in [[1, 2, 3], [4, 5, 6], [7, 8, 9], [10, 11, 12]]
            .into_iter()
            .enumerate()
        {
            builder
                .place_macroblock(
                    address,
                    &[values[0]; 256],
                    &[values[1]; 64],
                    &[values[2]; 64],
                )
                .unwrap();
        }
        assert!(builder
            .place_macroblock(0, &[0; 256], &[0; 64], &[0; 64])
            .is_err());
        let frame = builder.finish().unwrap();
        assert_eq!((frame.width, frame.height), (28, 28));
        assert_eq!((frame.y_stride, frame.u_stride, frame.v_stride), (28, 14, 14));
        for (plane, stride, x, y, expected) in [
            (&frame.y, frame.y_stride, 0, 0, 1),
            (&frame.y, frame.y_stride, 14, 0, 4),
            (&frame.y, frame.y_stride, 0, 14, 7),
            (&frame.y, frame.y_stride, 27, 27, 10),
            (&frame.u, frame.u_stride, 0, 0, 2),
            (&frame.u, frame.u_stride, 7, 0, 5),
            (&frame.v, frame.v_stride, 0, 7, 9),
            (&frame.v, frame.v_stride, 13, 13, 12),
        ] {
            assert_eq!(plane[y * stride + x], expected);
        }
        assert!(Yuv420FrameBuilder::new(1, 1, 1, 0, 0, 0).is_err());
    }

    #[test]
    fn derives_chroma_qpc_from_table_8_15() {
        let qpc_from_qpi = [
            29, 30, 31, 32, 32, 33, 34, 34, 35, 35, 36, 36, 37, 37, 37, 38, 38, 38, 38, 39, 39, 39,
        ];
        for qpy in 0..=51 {
            let expected = if qpy < 30 {
                qpy
            } else {
                qpc_from_qpi[(qpy - 30) as usize]
            };
            assert_eq!(derive_chroma_qpc(qpy, 0).unwrap(), expected);
        }
    }

    #[test]
    fn derives_chroma_qpc_with_clipping_and_rejects_invalid_inputs() {
        let vectors = [
            (0, -12, 0),
            (0, 12, 12),
            (26, -12, 14),
            (26, 12, 35),
            (51, 12, 39),
        ];
        for (qpy, offset, expected) in vectors {
            assert_eq!(derive_chroma_qpc(qpy, offset).unwrap(), expected);
        }
        for qpy in [-1, 52] {
            assert_eq!(
                derive_chroma_qpc(qpy, 0).unwrap_err().kind(),
                ErrorKind::InvalidData
            );
        }
        for offset in [-13, 13] {
            assert_eq!(
                derive_chroma_qpc(26, offset).unwrap_err().kind(),
                ErrorKind::InvalidData
            );
        }
    }

    #[test]
    fn inverse_scale_8x8_matches_scaling_class_and_qp_vectors() {
        let levels = [1_i32; 64];
        let scaling_list = [16_u8; 64];
        let vectors = [
            (
                0,
                [
                    20, 19, 25, 19, 20, 19, 25, 19, 19, 18, 24, 18, 19, 18, 24, 18, 25, 24, 32, 24,
                    25, 24, 32, 24, 19, 18, 24, 18, 19, 18, 24, 18, 20, 19, 25, 19, 20, 19, 25, 19,
                    19, 18, 24, 18, 19, 18, 24, 18, 25, 24, 32, 24, 25, 24, 32, 24, 19, 18, 24, 18,
                    19, 18, 24, 18,
                ],
            ),
            (
                24,
                [
                    320, 304, 400, 304, 320, 304, 400, 304, 304, 288, 384, 288, 304, 288, 384, 288,
                    400, 384, 512, 384, 400, 384, 512, 384, 304, 288, 384, 288, 304, 288, 384, 288,
                    320, 304, 400, 304, 320, 304, 400, 304, 304, 288, 384, 288, 304, 288, 384, 288,
                    400, 384, 512, 384, 400, 384, 512, 384, 304, 288, 384, 288, 304, 288, 384, 288,
                ],
            ),
            (
                51,
                [
                    7168, 6656, 8960, 6656, 7168, 6656, 8960, 6656, 6656, 6400, 8448, 6400, 6656,
                    6400, 8448, 6400, 8960, 8448, 11520, 8448, 8960, 8448, 11520, 8448, 6656, 6400,
                    8448, 6400, 6656, 6400, 8448, 6400, 7168, 6656, 8960, 6656, 7168, 6656, 8960,
                    6656, 6656, 6400, 8448, 6400, 6656, 6400, 8448, 6400, 8960, 8448, 11520, 8448,
                    8960, 8448, 11520, 8448, 6656, 6400, 8448, 6400, 6656, 6400, 8448, 6400,
                ],
            ),
        ];

        for (qpy, expected) in vectors {
            assert_eq!(
                inverse_scale_luma8x8(&levels, &scaling_list, qpy).unwrap(),
                expected
            );
        }
    }

    #[test]
    fn inverse_scale_8x8_matches_negative_and_custom_weight_vectors() {
        let mut levels = [0_i32; 64];
        levels[0] = -1;
        levels[1] = 1;
        let mut scaling_list = [16_u8; 64];
        scaling_list[1] = 8;

        let scaled = inverse_scale_luma8x8(&levels, &scaling_list, 0).unwrap();

        assert_eq!((scaled[0], scaled[1]), (-20, 10));
    }

    #[test]
    fn inverse_scale_8x8_rejects_out_of_range_qpy_and_zero_weight() {
        let levels = [0_i32; 64];
        let scaling_list = [16_u8; 64];
        for qpy in [-1, 52] {
            assert_eq!(
                inverse_scale_luma8x8(&levels, &scaling_list, qpy)
                    .unwrap_err()
                    .kind(),
                ErrorKind::InvalidData
            );
        }

        let mut zero_weight = scaling_list;
        zero_weight[42] = 0;
        assert_eq!(
            inverse_scale_luma8x8(&levels, &zero_weight, 0)
                .unwrap_err()
                .kind(),
            ErrorKind::InvalidData
        );
    }

    #[test]
    fn negative_level_and_custom_weight_match_other_languages() {
        let mut levels = [0_i32; 16];
        levels[0] = -1;
        levels[1] = 1;
        let mut scaling_list = [16_u8; 16];
        scaling_list[1] = 8;

        let scaled = inverse_scale_luma4x4(&levels, &scaling_list, 0).unwrap();

        assert_eq!((scaled[0], scaled[1]), (-10, 7));
    }

    #[test]
    fn rejects_out_of_range_qpy_and_zero_scaling_weight() {
        let levels = [0_i32; 16];
        let scaling_list = [16_u8; 16];
        for qpy in [-1, 52] {
            assert_eq!(
                inverse_scale_luma4x4(&levels, &scaling_list, qpy)
                    .unwrap_err()
                    .kind(),
                ErrorKind::InvalidData
            );
        }

        let mut zero_weight = scaling_list;
        zero_weight[7] = 0;
        assert_eq!(
            inverse_scale_luma4x4(&levels, &zero_weight, 0)
                .unwrap_err()
                .kind(),
            ErrorKind::InvalidData
        );
    }

    #[test]
    fn inverse_transform_matches_dc_and_frequency_impulse_vectors() {
        let mut dc = [0_i64; 16];
        dc[0] = 64;
        assert_eq!(inverse_transform_luma4x4(&dc), [1_i64; 16]);

        let mut horizontal_frequency = [0_i64; 16];
        horizontal_frequency[1] = 64;
        assert_eq!(
            inverse_transform_luma4x4(&horizontal_frequency),
            [1, 1, 0, -1, 1, 1, 0, -1, 1, 1, 0, -1, 1, 1, 0, -1]
        );
    }

    #[test]
    fn inverse_transform_matches_signed_rounding_vectors() {
        for (dc_level, expected) in [(31, 0), (32, 1), (-33, -1)] {
            let mut coefficients = [0_i64; 16];
            coefficients[0] = dc_level;
            assert_eq!(inverse_transform_luma4x4(&coefficients), [expected; 16]);
        }
    }

    #[test]
    fn inverse_transform_chroma_dc2x2_matches_hadamard_vectors() {
        let vectors = [
            ([7, 7, 7, 7], [28, 0, 0, 0]),
            ([1, 0, 0, 0], [1, 1, 1, 1]),
            ([1, 2, 3, 4], [10, -2, -4, 0]),
            ([-1, 2, -3, 4], [2, -10, 0, 4]),
        ];
        for (coefficients, expected) in vectors {
            assert_eq!(inverse_transform_chroma_dc2x2(&coefficients), expected);
        }
    }

    #[test]
    fn inverse_transform_8x8_matches_dc_and_frequency_impulse_vectors() {
        let mut dc = [0_i64; 64];
        dc[0] = 64;
        assert_eq!(inverse_transform_luma8x8(&dc), [1_i64; 64]);

        let frequency = [2, -1, 1, 0, 0, -1, 1, -1];
        let mut horizontal_frequency = [0_i64; 64];
        horizontal_frequency[1] = 64;
        let mut expected_horizontal = [0_i64; 64];
        for row in 0..8 {
            expected_horizontal[row * 8..row * 8 + 8].copy_from_slice(&frequency);
        }
        assert_eq!(
            inverse_transform_luma8x8(&horizontal_frequency),
            expected_horizontal
        );

        let mut vertical_frequency = [0_i64; 64];
        vertical_frequency[8] = 64;
        let mut expected_vertical = [0_i64; 64];
        for (row, value) in frequency.into_iter().enumerate() {
            expected_vertical[row * 8..row * 8 + 8].fill(value);
        }
        assert_eq!(
            inverse_transform_luma8x8(&vertical_frequency),
            expected_vertical
        );
    }

    #[test]
    fn inverse_transform_8x8_matches_signed_rounding_vectors() {
        for (dc_level, expected) in [(31, 0), (32, 1), (-33, -1)] {
            let mut coefficients = [0_i64; 64];
            coefficients[0] = dc_level;
            assert_eq!(inverse_transform_luma8x8(&coefficients), [expected; 64]);
        }
    }

    #[test]
    fn predicts_vertical_intra8x8_from_filtered_top_reference_samples() {
        let top = [0, 17, 63, 129, 190, 220, 254, 255];
        let mut expected = [0_u8; 64];
        for row in 0..8 {
            expected[row * 8..row * 8 + 8].copy_from_slice(&top);
        }
        assert_eq!(predict_luma_intra8x8_vertical(&top), expected);
    }

    #[test]
    fn predicts_vertical_intra16x16_from_filtered_top_reference_samples() {
        let top = [
            0, 17, 33, 51, 68, 85, 102, 119, 136, 153, 170, 187, 204, 221, 238, 255,
        ];
        let prediction = predict_luma_intra16x16_vertical(&top);
        for row in 0..16 {
            assert_eq!(&prediction[row * 16..row * 16 + 16], &top);
        }
    }

    #[test]
    fn predicts_horizontal_intra16x16_from_filtered_left_reference_samples() {
        let left = [
            0, 17, 33, 51, 68, 85, 102, 119, 136, 153, 170, 187, 204, 221, 238, 255,
        ];
        let prediction = predict_luma_intra16x16_horizontal(&left);
        for (row, sample) in left.iter().enumerate() {
            assert_eq!(&prediction[row * 16..row * 16 + 16], &[*sample; 16]);
        }
    }

    #[test]
    fn predicts_dc_intra16x16_for_reference_availability_and_rounding_cases() {
        let top = std::array::from_fn::<_, 16, _>(|index| index as u8);
        let left = std::array::from_fn::<_, 16, _>(|index| (index + 16) as u8);
        for (top_reference, left_reference, expected) in [
            (Some(&top), Some(&left), 16),
            (Some(&top), None, 8),
            (None, Some(&left), 24),
            (None, None, 128),
        ] {
            assert_eq!(
                predict_luma_intra16x16_dc(top_reference, left_reference),
                [expected; 256]
            );
        }
    }

    #[test]
    fn predicts_plane_intra16x16_gradient_and_clipping() {
        let top = std::array::from_fn::<_, 16, _>(|index| (80 + 2 * (index + 1)) as u8);
        let left = std::array::from_fn::<_, 16, _>(|index| (80 + 3 * (index + 1)) as u8);
        let mut expected = [0_u8; 256];
        for row in 0..16 {
            for column in 0..16 {
                expected[row * 16 + column] = (85 + 2 * column + 3 * row) as u8;
            }
        }
        assert_eq!(predict_luma_intra16x16_plane(&top, &left, 80), expected);

        let mut high_top = [0_u8; 16];
        let mut high_left = [0_u8; 16];
        high_top[15] = 255;
        high_left[15] = 255;
        assert_eq!(
            predict_luma_intra16x16_plane(&high_top, &high_left, 0)[255],
            255
        );

        let mut low_top = [0_u8; 16];
        let mut low_left = [0_u8; 16];
        low_top[0] = 255;
        low_left[0] = 255;
        assert_eq!(
            predict_luma_intra16x16_plane(&low_top, &low_left, 0)[255],
            0
        );
    }

    #[test]
    fn predicts_chroma_plane_intra8x8_gradient_and_clipping() {
        let top = [82, 84, 86, 88, 90, 92, 94, 96];
        let left = [83, 86, 89, 92, 95, 98, 101, 104];
        let mut expected = [0_u8; 64];
        for row in 0..8 {
            for column in 0..8 {
                expected[row * 8 + column] = (85 + 2 * column + 3 * row) as u8;
            }
        }
        assert_eq!(predict_chroma_intra8x8_plane(&top, &left, 80), expected);

        assert_eq!(
            predict_chroma_intra8x8_plane(&[255; 8], &[255; 8], 0)[63],
            255
        );
        assert_eq!(predict_chroma_intra8x8_plane(&[0; 8], &[0; 8], 255)[63], 0);
    }

    #[test]
    fn dispatches_chroma_intra8x8_modes_and_dc_fallback() {
        let top = [82, 84, 86, 88, 90, 92, 94, 96];
        let left = [83, 86, 89, 92, 95, 98, 101, 104];
        let top_left = 80;
        let expected = [
            predict_chroma_intra8x8_dc(Some(&top), Some(&left)),
            predict_chroma_intra8x8_horizontal(&left),
            predict_chroma_intra8x8_vertical(&top),
            predict_chroma_intra8x8_plane(&top, &left, top_left),
        ];
        for (mode, prediction) in expected.iter().enumerate() {
            assert_eq!(
                predict_chroma_intra8x8(mode as u8, Some(&top), Some(&left), Some(top_left))
                    .unwrap(),
                *prediction
            );
        }
        assert_eq!(
            predict_chroma_intra8x8(0, None, None, None).unwrap(),
            [128_u8; 64]
        );
    }

    #[test]
    fn rejects_invalid_chroma_intra8x8_modes_and_missing_references() {
        let top = [0_u8; 8];
        let left = [0_u8; 8];
        for (mode, top_reference, left_reference, top_left) in [
            (1, Some(&top), None, None),
            (2, None, Some(&left), None),
            (3, None, Some(&left), Some(0)),
            (3, Some(&top), None, Some(0)),
            (3, Some(&top), Some(&left), None),
            (4, None, None, None),
        ] {
            assert_eq!(
                predict_chroma_intra8x8(mode, top_reference, left_reference, top_left)
                    .unwrap_err()
                    .kind(),
                ErrorKind::InvalidData
            );
        }
    }

    #[test]
    fn predicts_horizontal_intra8x8_from_filtered_left_reference_samples() {
        let left = [0, 17, 63, 129, 190, 220, 254, 255];
        let mut expected = [0_u8; 64];
        for (row, sample) in left.iter().enumerate() {
            expected[row * 8..row * 8 + 8].fill(*sample);
        }
        assert_eq!(predict_luma_intra8x8_horizontal(&left), expected);
    }

    #[test]
    fn predicts_dc_intra8x8_for_reference_availability_and_rounding_cases() {
        let top = [10, 40, 90, 160, 20, 50, 80, 110];
        let left = [20, 60, 100, 140, 30, 70, 110, 150];
        for (top_reference, left_reference, expected) in [
            (Some(&top), Some(&left), 78),
            (Some(&top), None, 70),
            (None, Some(&left), 85),
            (None, None, 128),
        ] {
            assert_eq!(
                predict_luma_intra8x8_dc(top_reference, left_reference),
                [expected; 64]
            );
        }
    }

    #[test]
    fn predicts_chroma_dc_intra8x8_by_quadrant_and_reference_fallback() {
        let top = [10, 20, 30, 40, 50, 60, 70, 80];
        let left = [1, 3, 5, 7, 9, 11, 13, 15];
        for (top_reference, left_reference, quadrants) in [
            (Some(&top), Some(&left), [15, 65, 12, 39]),
            (Some(&top), None, [25, 65, 25, 65]),
            (None, Some(&left), [4, 4, 12, 12]),
            (None, None, [128, 128, 128, 128]),
        ] {
            let mut expected = [0_u8; 64];
            for row in 0..8 {
                for column in 0..8 {
                    expected[row * 8 + column] = quadrants[(row / 4) * 2 + column / 4];
                }
            }
            assert_eq!(
                predict_chroma_intra8x8_dc(top_reference, left_reference),
                expected
            );
        }
    }

    #[test]
    fn predicts_chroma_horizontal_intra8x8_from_available_left_edge() {
        let left = [0, 17, 63, 129, 190, 220, 254, 255];
        let mut expected = [0_u8; 64];
        for (row, sample) in left.iter().enumerate() {
            expected[row * 8..row * 8 + 8].fill(*sample);
        }
        assert_eq!(predict_chroma_intra8x8_horizontal(&left), expected);
    }

    #[test]
    fn predicts_chroma_vertical_intra8x8_from_available_top_edge() {
        let top = [0, 17, 63, 129, 190, 220, 254, 255];
        let mut expected = [0_u8; 64];
        for row in 0..8 {
            expected[row * 8..row * 8 + 8].copy_from_slice(&top);
        }
        assert_eq!(predict_chroma_intra8x8_vertical(&top), expected);
    }

    #[test]
    fn predicts_diagonal_down_left_intra8x8_with_final_reference_extension() {
        let top = [
            10, 20, 30, 40, 50, 60, 70, 80, 90, 100, 110, 120, 130, 140, 150, 160,
        ];
        assert_eq!(
            predict_luma_intra8x8_diagonal_down_left(&top),
            [
                20, 30, 40, 50, 60, 70, 80, 90, 30, 40, 50, 60, 70, 80, 90, 100, 40, 50, 60, 70,
                80, 90, 100, 110, 50, 60, 70, 80, 90, 100, 110, 120, 60, 70, 80, 90, 100, 110, 120,
                130, 70, 80, 90, 100, 110, 120, 130, 140, 80, 90, 100, 110, 120, 130, 140, 150, 90,
                100, 110, 120, 130, 140, 150, 158,
            ]
        );
    }

    #[test]
    fn predicts_diagonal_down_right_intra8x8_from_all_reference_regions() {
        let top = [
            20, 40, 60, 80, 100, 120, 140, 160, 180, 200, 220, 240, 250, 230, 210, 190,
        ];
        let left = [60, 100, 140, 180, 220, 240, 200, 160];
        assert_eq!(
            predict_luma_intra8x8_diagonal_down_right(&top, &left, 30),
            [
                28, 40, 60, 80, 100, 120, 140, 160, 35, 28, 40, 60, 80, 100, 120, 140, 63, 35, 28,
                40, 60, 80, 100, 120, 100, 63, 35, 28, 40, 60, 80, 100, 140, 100, 63, 35, 28, 40,
                60, 80, 180, 140, 100, 63, 35, 28, 40, 60, 215, 180, 140, 100, 63, 35, 28, 40, 225,
                215, 180, 140, 100, 63, 35, 28,
            ]
        );
    }

    #[test]
    fn predicts_vertical_right_intra8x8_from_all_reference_regions() {
        let top = [
            20, 40, 60, 80, 100, 120, 140, 160, 180, 200, 220, 240, 250, 230, 210, 190,
        ];
        let left = [60, 100, 140, 180, 220, 240, 200, 160];
        assert_eq!(
            predict_luma_intra8x8_vertical_right(&top, &left, 30),
            [
                28, 40, 60, 80, 100, 120, 140, 160, 45, 25, 30, 50, 70, 90, 110, 130, 35, 28, 40,
                60, 80, 100, 120, 140, 80, 45, 25, 30, 50, 70, 90, 110, 100, 35, 28, 40, 60, 80,
                100, 120, 120, 80, 45, 25, 30, 50, 70, 90, 140, 100, 35, 28, 40, 60, 80, 100, 160,
                120, 80, 45, 25, 30, 50, 70,
            ]
        );
    }

    #[test]
    fn predicts_horizontal_down_intra8x8_as_vertical_right_transpose() {
        let top = [60, 100, 140, 180, 220, 240, 200, 160];
        let left = [
            20, 40, 60, 80, 100, 120, 140, 160, 180, 200, 220, 240, 250, 230, 210, 190,
        ];
        assert_eq!(
            predict_luma_intra8x8_horizontal_down(&top, &left, 30),
            [
                28, 45, 35, 80, 100, 120, 140, 160, 40, 25, 28, 45, 35, 80, 100, 120, 60, 30, 40,
                25, 28, 45, 35, 80, 80, 50, 60, 30, 40, 25, 28, 45, 100, 70, 80, 50, 60, 30, 40,
                25, 120, 90, 100, 70, 80, 50, 60, 30, 140, 110, 120, 90, 100, 70, 80, 50, 160, 130,
                140, 110, 120, 90, 100, 70,
            ]
        );
    }

    #[test]
    fn predicts_vertical_left_intra8x8_across_top_reference_phases() {
        let top = [
            10, 20, 30, 40, 50, 60, 70, 80, 90, 100, 110, 120, 130, 140, 150, 160,
        ];
        assert_eq!(
            predict_luma_intra8x8_vertical_left(&top),
            [
                15, 25, 35, 45, 55, 65, 75, 85, 20, 30, 40, 50, 60, 70, 80, 90, 25, 35, 45, 55, 65,
                75, 85, 95, 30, 40, 50, 60, 70, 80, 90, 100, 35, 45, 55, 65, 75, 85, 95, 105, 40,
                50, 60, 70, 80, 90, 100, 110, 45, 55, 65, 75, 85, 95, 105, 115, 50, 60, 70, 80, 90,
                100, 110, 120,
            ]
        );
    }

    #[test]
    fn predicts_horizontal_up_intra8x8_as_vertical_left_transpose() {
        let left = [
            10, 20, 30, 40, 50, 60, 70, 80, 90, 100, 110, 120, 130, 140, 150, 160,
        ];
        assert_eq!(
            predict_luma_intra8x8_horizontal_up(&left),
            [
                15, 20, 25, 30, 35, 40, 45, 50, 25, 30, 35, 40, 45, 50, 55, 60, 35, 40, 45, 50, 55,
                60, 65, 70, 45, 50, 55, 60, 65, 70, 75, 80, 55, 60, 65, 70, 75, 80, 85, 90, 65, 70,
                75, 80, 85, 90, 95, 100, 75, 80, 85, 90, 95, 100, 105, 110, 85, 90, 95, 100, 105,
                110, 115, 120,
            ]
        );
    }

    #[test]
    fn predicts_plane_intra8x8_gradients_and_clips() {
        let mut top = [0_u8; 16];
        let mut left = [0_u8; 16];
        for index in 0..16 {
            top[index] = (80 + 2 * index) as u8;
            left[index] = (80 + 3 * index) as u8;
        }
        assert_eq!(
            predict_luma_intra8x8_plane(&top, &left),
            [
                85, 87, 89, 91, 93, 95, 97, 99, 88, 90, 92, 94, 96, 98, 100, 102, 91, 93, 95, 97,
                99, 101, 103, 105, 94, 96, 98, 100, 102, 104, 106, 108, 97, 99, 101, 103, 105, 107,
                109, 111, 100, 102, 104, 106, 108, 110, 112, 114, 103, 105, 107, 109, 111, 113,
                115, 117, 106, 108, 110, 112, 114, 116, 118, 120,
            ]
        );

        let mut high_top = [0_u8; 16];
        let mut high_left = [0_u8; 16];
        high_top[8] = 255;
        high_left[8] = 255;
        assert_eq!(predict_luma_intra8x8_plane(&high_top, &high_left)[63], 255);

        let mut low_top = [0_u8; 16];
        let mut low_left = [0_u8; 16];
        low_top[0] = 255;
        low_left[0] = 255;
        assert_eq!(predict_luma_intra8x8_plane(&low_top, &low_left)[63], 0);
    }

    #[test]
    fn predicts_vertical_intra4x4_from_top_reference_samples() {
        for (top, expected) in [
            (
                [10, 40, 90, 160],
                [
                    10, 40, 90, 160, 10, 40, 90, 160, 10, 40, 90, 160, 10, 40, 90, 160,
                ],
            ),
            (
                [0, 255, 0, 255],
                [
                    0, 255, 0, 255, 0, 255, 0, 255, 0, 255, 0, 255, 0, 255, 0, 255,
                ],
            ),
        ] {
            assert_eq!(predict_luma_intra4x4_vertical(&top), expected);
        }
    }

    #[test]
    fn predicts_horizontal_intra4x4_from_left_reference_samples() {
        for (left, expected) in [
            (
                [10, 40, 90, 160],
                [
                    10, 10, 10, 10, 40, 40, 40, 40, 90, 90, 90, 90, 160, 160, 160, 160,
                ],
            ),
            (
                [0, 255, 0, 255],
                [
                    0, 0, 0, 0, 255, 255, 255, 255, 0, 0, 0, 0, 255, 255, 255, 255,
                ],
            ),
        ] {
            assert_eq!(predict_luma_intra4x4_horizontal(&left), expected);
        }
    }

    #[test]
    fn predicts_dc_intra4x4_for_reference_availability_cases() {
        let top = [10, 40, 90, 161];
        let left = [20, 60, 100, 141];
        for (top_reference, left_reference, expected) in [
            (Some(&top), Some(&left), 78),
            (Some(&top), None, 75),
            (None, Some(&left), 80),
            (None, None, 128),
        ] {
            assert_eq!(
                predict_luma_intra4x4_dc(top_reference, left_reference),
                [expected; 16]
            );
        }
    }

    #[test]
    fn predicts_diagonal_down_left_intra4x4_with_final_reference_extension() {
        let top = [10, 20, 30, 40, 50, 60, 70, 80];
        assert_eq!(
            predict_luma_intra4x4_diagonal_down_left(&top),
            [20, 30, 40, 50, 30, 40, 50, 60, 40, 50, 60, 70, 50, 60, 70, 78]
        );
    }

    #[test]
    fn predicts_diagonal_down_right_intra4x4_from_all_reference_regions() {
        let top = [20, 40, 80, 120, 160, 200, 220, 240];
        let left = [60, 100, 140, 180];
        assert_eq!(
            predict_luma_intra4x4_diagonal_down_right(&top, &left, 30),
            [28, 45, 80, 120, 35, 28, 45, 80, 63, 35, 28, 45, 100, 63, 35, 28]
        );
    }

    #[test]
    fn predicts_vertical_right_intra4x4_from_all_reference_regions() {
        let top = [20, 40, 80, 120, 160, 200, 220, 240];
        let left = [60, 100, 140, 180];
        assert_eq!(
            predict_luma_intra4x4_vertical_right(&top, &left, 30),
            [28, 45, 80, 120, 45, 25, 30, 60, 35, 28, 45, 80, 80, 45, 25, 30]
        );
    }

    #[test]
    fn predicts_horizontal_down_intra4x4_as_vertical_right_transpose() {
        let top = [20, 40, 80, 120];
        let left = [60, 100, 140, 180];
        assert_eq!(
            predict_luma_intra4x4_horizontal_down(&top, &left, 30),
            [63, 25, 35, 30, 100, 45, 63, 25, 140, 80, 100, 45, 170, 120, 140, 80]
        );
    }

    #[test]
    fn predicts_vertical_left_intra4x4_with_alternating_top_phases() {
        let top = [10, 20, 30, 40, 50, 60, 70, 80];
        assert_eq!(
            predict_luma_intra4x4_vertical_left(&top),
            [15, 25, 35, 45, 20, 30, 40, 50, 25, 35, 45, 55, 30, 40, 50, 60]
        );
    }

    #[test]
    fn predicts_horizontal_up_intra4x4_with_alternating_left_phases() {
        let left = [10, 20, 30, 40, 50, 60, 70, 80];
        assert_eq!(
            predict_luma_intra4x4_horizontal_up(&left),
            [15, 20, 25, 30, 25, 30, 35, 40, 35, 40, 45, 50, 45, 50, 55, 60]
        );
    }
}
