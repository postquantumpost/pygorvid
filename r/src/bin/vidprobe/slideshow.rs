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
            let width = u32::try_from(frame.width).map_err(|_| "frame width is too large")?;
            let height = u32::try_from(frame.height).map_err(|_| "frame height is too large")?;
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
        texture
            .update_yuv(
                None,
                &frame.y,
                frame.y_stride,
                &frame.u,
                frame.u_stride,
                &frame.v,
                frame.v_stride,
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
        for event in events.poll_iter() {
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
        let remaining = deadline.saturating_duration_since(Instant::now());
        if remaining.is_zero() {
            return true;
        }
        std::thread::sleep(remaining.min(Duration::from_millis(20)));
    }
}
