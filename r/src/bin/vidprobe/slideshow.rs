use std::time::{Duration, Instant};

use sdl2::event::{Event, WindowEvent};
use sdl2::pixels::PixelFormatEnum;

pub fn show(input: &str, threads: usize, verbose: bool) -> Result<usize, String> {
    let mut reader = pygorvid::VideoSampleReader::open(input).map_err(|e| e.to_string())?;
    let presentation_order = reader.presentation_sample_indices();
    let timescale = reader.timescale();
    let decoder_reader = pygorvid::VideoSampleReader::open(input).map_err(|e| e.to_string())?;
    let mut decoder = pygorvid::VideoDecoder::new(decoder_reader).map_err(|e| e.to_string())?;
    decoder.set_threads(threads);
    decoder.set_verbose(verbose);
    let sdl = sdl2::init().map_err(|e| format!("initialize SDL2: {e}"))?;
    let video = sdl
        .video()
        .map_err(|e| format!("initialize SDL2 video: {e}"))?;
    let mut events = sdl
        .event_pump()
        .map_err(|e| format!("initialize SDL2 events: {e}"))?;
    let mut canvas = None;
    let mut deadline = Instant::now();
    let mut displayed = 0;
    for (index, &sample_index) in presentation_order.iter().enumerate() {
        if !wait(&mut events, Instant::now()) {
            return Ok(displayed);
        }
        let sample = reader
            .sample_at(sample_index)
            .map_err(|e| format!("read frame {index}: {e}"))?;
        let frame = decoder
            .decode_frame(index as u64)
            .map_err(|e| format!("decode frame {index}: {e}"))?;
        if !wait(&mut events, deadline) {
            return Ok(displayed);
        }
        if canvas.is_none() {
            let scale = 1.0_f64
                .min(1024.0 / frame.width as f64)
                .min(768.0 / frame.height as f64);
            let width = (frame.width as f64 * scale).max(1.0) as u32;
            let height = (frame.height as f64 * scale).max(1.0) as u32;
            let window = video
                .window(input, width, height)
                .position_centered()
                .resizable()
                .build()
                .map_err(|e| format!("create slideshow window: {e}"))?;
            canvas = Some(
                window
                    .into_canvas()
                    .build()
                    .map_err(|e| format!("create slideshow renderer: {e}"))?,
            );
        }
        let canvas = canvas.as_mut().expect("canvas was initialized");
        let creator = canvas.texture_creator();
        let mut texture = creator
            .create_texture_streaming(
                PixelFormatEnum::IYUV,
                frame.width as u32,
                frame.height as u32,
            )
            .map_err(|e| format!("create slideshow texture: {e}"))?;
        let chroma_full_height = frame.u.len() >= frame.u_stride * frame.height;
        let (display_u, display_v, display_stride) = if chroma_full_height {
            let downsample = |plane: &[u8], stride: usize| {
                let output_stride = frame.width.div_ceil(2);
                let chroma_444 = stride >= frame.width;
                let mut output = vec![0; output_stride * frame.height.div_ceil(2)];
                for row in 0..frame.height.div_ceil(2) {
                    for column in 0..output_stride {
                        let sample = |source_row: usize| {
                            if chroma_444 {
                                let x = column * 2;
                                (u16::from(plane[source_row * stride + x.min(frame.width - 1)])
                                    + u16::from(
                                        plane[source_row * stride + (x + 1).min(frame.width - 1)],
                                    )
                                    + 1)
                                    / 2
                            } else {
                                u16::from(plane[source_row * stride + column])
                            }
                        };
                        output[row * output_stride + column] =
                            ((sample(row * 2) + sample((row * 2 + 1).min(frame.height - 1)) + 1)
                                / 2) as u8;
                    }
                }
                output
            };
            (
                downsample(&frame.u, frame.u_stride),
                downsample(&frame.v, frame.v_stride),
                frame.width.div_ceil(2),
            )
        } else {
            (frame.u.clone(), frame.v.clone(), frame.u_stride)
        };
        texture
            .update_yuv(
                None,
                &frame.y,
                frame.y_stride,
                &display_u,
                display_stride,
                &display_v,
                display_stride,
            )
            .map_err(|e| format!("update slideshow texture: {e}"))?;
        canvas.clear();
        canvas
            .copy(&texture, None, None)
            .map_err(|e| format!("render slideshow frame: {e}"))?;
        canvas.present();
        displayed += 1;
        deadline = Instant::now()
            + Duration::from_secs_f64(f64::from(sample.duration_ticks) / f64::from(timescale));
    }
    if displayed > 0 {
        wait(&mut events, deadline);
    }
    Ok(displayed)
}

fn wait(events: &mut sdl2::EventPump, deadline: Instant) -> bool {
    loop {
        let remaining = deadline.saturating_duration_since(Instant::now());
        if remaining.is_zero() {
            return true;
        }
        let timeout = remaining.as_millis().min(u32::MAX as u128).max(1) as u32;
        if let Some(event) = events.wait_event_timeout(timeout) {
            if matches!(
                event,
                Event::Quit { .. }
                    | Event::Window {
                        win_event: WindowEvent::Close,
                        ..
                    }
            ) {
                return false;
            }
        }
    }
}
