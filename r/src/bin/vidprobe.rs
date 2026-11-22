use std::fs;
use std::path::{Path, PathBuf};
use std::process::exit;
use std::sync::mpsc::{self, Receiver};
use std::thread;
use std::time::{SystemTime, UNIX_EPOCH};

#[cfg(feature = "slideshow")]
#[path = "vidprobe/slideshow.rs"]
mod slideshow;

fn main() {
    exit(run(std::env::args().skip(1).collect()));
}

fn generation_metadata() -> pygorvid::Mp4Metadata {
    pygorvid::Mp4Metadata {
        title: "Title".into(),
        artist: "Artist".into(),
        album: "Album".into(),
        publisher: "Publisher".into(),
        language: "Language".into(),
        now_playing: "Now Playing".into(),
        album_artist: "Album Artist".into(),
        genre: "Genre".into(),
        track: "1".into(),
        disc: "1".into(),
        date: "2016-12-31 23:59:60 UTC".into(),
        composer: "Composer".into(),
        comment: "Comments".into(),
        description: "Description".into(),
        copyright: "Copyright".into(),
        make: "Camera make".into(),
        model: "Camera model".into(),
        firmware: "firmware version".into(),
        location: "11.3733\u{b0} N 142.5917\u{b0} E".into(),
        altitude: "-10925.0 meters".into(),
        heading: "180.0".into(),
        encoder: "pygorvid".into(),
    }
}

fn run(args: Vec<String>) -> i32 {
    let (
        input,
        output,
        frame,
        prefix,
        threads,
        read_all,
        hash_frames,
        slideshow,
        readahead,
        verbose,
        mut files,
        generate,
    ) = match parse_args(&args) {
        Ok(parsed) => parsed,
        Err(error) => {
            eprintln!("{error}");
            print_usage();
            return 2;
        }
    };
    if let Some(input) = input {
        files.insert(0, input);
    }
    if let Some(output_path) = generate {
        if !files.is_empty()
            || read_all
            || hash_frames
            || slideshow
            || frame.is_some()
            || output.is_some()
        {
            eprintln!(
                "--generate cannot be combined with inputs, --readall, --hash, --extractframe or --output"
            );
            return 2;
        }
        let output = Path::new(&output_path);
        if !output
            .extension()
            .is_some_and(|value| value.eq_ignore_ascii_case("mp4"))
        {
            eprintln!("--generate must end in .mp4");
            return 2;
        }
        let output_path = prefixed_output(&output_path, prefix.as_deref());
        let output = Path::new(&output_path);
        let payload = match pygorvid::Mp4Writer::new()
            .encode_with_metadata(&pygorvid::H264Encoder::new(), Some(&generation_metadata()))
        {
            Ok(payload) => payload,
            Err(error) => {
                eprintln!("{output_path}: {error}");
                return 1;
            }
        };
        if let Err(error) = write_generated_outputs(
            output,
            &payload,
            &pygorvid::generate_h264_companion_corpus(),
        ) {
            eprintln!("{output_path}: {error}");
            return 1;
        }
        println!("{output_path}: generated one second of 64x64 H.264 feature-test video with stereo silence");
        return 0;
    }

    if files.is_empty() {
        print_usage();
        return 2;
    }
    if (read_all && slideshow) || ((read_all || slideshow) && (frame.is_some() || output.is_some()))
    {
        eprintln!("--slideshow, --readall and extraction options cannot be combined");
        return 2;
    }
    if hash_frames && !read_all && frame.is_none() {
        eprintln!("--hash requires --readall or --extractframe");
        return 2;
    }
    if frame.is_some() != output.is_some() {
        eprintln!("--extractframe and --output must be used together");
        return 2;
    }
    if let (Some(frame), Some(output)) = (frame, output) {
        if files.len() != 1 {
            eprintln!("frame extraction requires exactly one input file");
            return 2;
        }
        let output = prefixed_output(&output, prefix.as_deref());
        if let Err(error) = extract_frame(&files[0], frame, &output, threads, verbose, hash_frames)
        {
            eprintln!("{}: {error}", files[0]);
            return 1;
        }
        println!("{}: extracted frame {frame} to {output}", files[0]);
        return 0;
    }
    let mut status = 0;
    for p in &files {
        if slideshow {
            match show_slideshow(p, threads, verbose, readahead) {
                Ok(count) => println!("{p}: displayed {count} frames"),
                Err(error) => {
                    eprintln!("{p}: {error}");
                    status = 1;
                }
            }
            continue;
        }
        if read_all {
            match read_all_frames_with_readahead(
                p,
                threads,
                verbose,
                hash_frames,
                readahead,
            ) {
                Ok(count) => println!("{p}: read {count} frames"),
                Err(error) => {
                    eprintln!("{p}: {error}");
                    status = 1;
                }
            }
            continue;
        }
        let mut v = pygorvid::open_file(p);
        if v.isopen() {
            let info = v.getbasicinfo();
            let err = v.errorinfo().0;
            if err.is_empty() {
                let durations = info
                    .videostreams
                    .iter()
                    .map(|stream| {
                        format!(
                            "{} seconds ({})",
                            stream.duration_seconds,
                            format_duration_hms(stream.duration_seconds)
                        )
                    })
                    .collect::<Vec<_>>();
                let suffix = if durations.is_empty() {
                    String::new()
                } else {
                    format!(" video_durations=[{}]", durations.join(", "))
                };
                println!("{p}: {} {info:?}{suffix}", v.format());
            } else {
                eprintln!("{p}: {}: {err}", v.format());
                status = 1;
            }
        } else {
            eprintln!("{p}: {}", v.errorinfo().0);
            status = 1;
        }
    }
    status
}

fn format_duration_hms(seconds: f64) -> String {
    let total = seconds.ceil() as u64;
    let hours = total / 3600;
    let minutes = (total / 60) % 60;
    let seconds_part = total % 60;
    if hours > 0 {
        format!("{hours}:{minutes:02}:{seconds_part:02}")
    } else if minutes > 0 {
        format!("{minutes:02}:{seconds_part:02}")
    } else {
        format!("{seconds_part:02}")
    }
}

fn companion_output_path(mp4: &Path, suffix: &str) -> PathBuf {
    let stem = mp4
        .file_stem()
        .and_then(|value| value.to_str())
        .unwrap_or("output");
    mp4.with_file_name(format!("{stem}-{suffix}"))
}

fn write_generated_outputs(
    output: &Path,
    payload: &[u8],
    companions: &[pygorvid::H264Companion],
) -> std::io::Result<()> {
    let parent = output
        .parent()
        .filter(|path| !path.as_os_str().is_empty())
        .unwrap_or(Path::new("."));
    let nonce = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map_err(std::io::Error::other)?
        .as_nanos();
    let mut temporary = None;
    for attempt in 0..100 {
        let directory = parent.join(format!(
            ".pygorvid-{}-{nonce}-{attempt}",
            std::process::id()
        ));
        match fs::create_dir(&directory) {
            Ok(()) => {
                temporary = Some(TemporaryOutputDirectory(directory));
                break;
            }
            Err(error) if error.kind() == std::io::ErrorKind::AlreadyExists => continue,
            Err(error) => return Err(error),
        }
    }
    let temporary = temporary.ok_or_else(|| {
        std::io::Error::new(
            std::io::ErrorKind::AlreadyExists,
            "cannot create temporary output directory",
        )
    })?;
    if output.is_dir() {
        return Err(std::io::Error::other("output path is a directory"));
    }
    let mut staged = Vec::new();
    for companion in companions {
        let target = companion_output_path(output, companion.suffix);
        if target.is_dir() {
            return Err(std::io::Error::other(format!(
                "{} is a directory",
                target.display()
            )));
        }
        let source = temporary.0.join(companion.suffix);
        staged.push((source, target, &companion.data));
    }
    for (source, _, data) in &staged {
        fs::write(source, data)?;
    }
    let video = temporary.0.join("video");
    fs::write(&video, payload)?;
    for (source, target, _) in staged {
        fs::rename(source, target)?;
    }
    fs::rename(video, output)
}

fn parse_args(
    args: &[String],
) -> Result<
    (
        Option<String>,
        Option<String>,
        Option<usize>,
        Option<String>,
        usize,
        bool,
        bool,
        bool,
        bool,
        bool,
        Vec<String>,
        Option<String>,
    ),
    String,
> {
    let mut input = None;
    let mut generate = None;
    let mut output = None;
    let mut frame = None;
    let mut prefix = None;
    let mut threads = pygorvid::default_decoder_threads();
    let mut read_all = false;
    let mut hash_frames = false;
    let mut slideshow = false;
    let mut readahead = false;
    let mut verbose = false;
    let parse_threads = |value: &str| {
        value
            .parse::<usize>()
            .ok()
            .filter(|threads| *threads > 0)
            .ok_or_else(|| "--threads must be a positive integer".to_string())
    };
    let mut files = Vec::new();
    let mut index = 0;
    while index < args.len() {
        let arg = &args[index];
        if arg == "--" {
            files.extend(args[index + 1..].iter().cloned());
            break;
        }
        if arg == "--input"
            || arg == "--generate"
            || arg == "--output"
            || arg == "--extractframe"
            || arg == "--prefix"
            || arg == "--threads"
        {
            index += 1;
            let value = args
                .get(index)
                .ok_or_else(|| format!("{arg} requires a value"))?;
            match arg.as_str() {
                "--input" => input = Some(value.clone()),
                "--generate" => generate = Some(value.clone()),
                "--output" => output = Some(value.clone()),
                "--prefix" => prefix = Some(value.clone()),
                "--threads" => threads = parse_threads(value)?,
                _ => {
                    frame = Some(value.parse::<usize>().map_err(|_| {
                        "--extractframe must be a non-negative integer".to_string()
                    })?);
                }
            }
        } else if arg == "--readall" {
            read_all = true;
        } else if arg == "--hash" {
            hash_frames = true;
        } else if arg == "--slideshow" {
            slideshow = true;
        } else if arg == "--readahead" {
            readahead = true;
        } else if arg == "--verbose" {
            verbose = true;
        } else if let Some(value) = arg.strip_prefix("--input=") {
            input = Some(value.to_string());
        } else if let Some(value) = arg.strip_prefix("--generate=") {
            generate = Some(value.to_string());
        } else if let Some(value) = arg.strip_prefix("--output=") {
            output = Some(value.to_string());
        } else if let Some(value) = arg.strip_prefix("--extractframe=") {
            frame = Some(
                value
                    .parse::<usize>()
                    .map_err(|_| "--extractframe must be a non-negative integer".to_string())?,
            );
        } else if let Some(value) = arg.strip_prefix("--prefix=") {
            prefix = Some(value.to_string());
        } else if let Some(value) = arg.strip_prefix("--threads=") {
            threads = parse_threads(value)?;
        } else if arg.starts_with('-') {
            return Err(format!("unknown option: {arg}"));
        } else {
            files.push(arg.clone());
        }
        index += 1;
    }
    Ok((
        input,
        output,
        frame,
        prefix,
        threads,
        read_all,
        hash_frames,
        slideshow,
        readahead,
        verbose,
        files,
        generate,
    ))
}

fn print_usage() {
    eprintln!("       vidprobe --generate OUTPUT.mp4");
    eprintln!("usage: vidprobe [--input FILE] [--threads N] [--readahead] [--verbose] [--hash] [--slideshow | --readall | --extractframe INDEX --output IMAGE [--prefix TEXT]] FILE...");
}

#[cfg(not(feature = "slideshow"))]
fn show_slideshow(_: &str, _: usize, _: bool, _: bool) -> Result<usize, String> {
    Err(
        "optional slideshow build feature was not included; rebuild with --features slideshow"
            .into(),
    )
}

#[cfg(feature = "slideshow")]
fn show_slideshow(
    input: &str,
    threads: usize,
    verbose: bool,
    readahead: bool,
) -> Result<usize, String> {
    slideshow::show(input, threads, verbose, readahead)
}

fn closed_gops(input: &str) -> Result<Option<Vec<Vec<usize>>>, String> {
    let mut reader =
        pygorvid::VideoSampleReader::open(input).map_err(|error| error.to_string())?;
    let order = reader.presentation_sample_indices();
    let mut positions = vec![0; order.len()];
    for (presentation, sample) in order.into_iter().enumerate() {
        positions[sample] = presentation;
    }
    let configuration = reader.configuration();
    let length_size = configuration.nal_length_size;
    let count = reader.sample_count();
    let mut starts = Vec::new();
    for sample_index in 0..count {
        let sample = reader
            .sample_at(sample_index)
            .map_err(|error| error.to_string())?;
        if !sample.is_sync {
            continue;
        }
        let closed = pygorvid::split_h264_sample_nals(
            &sample.data,
            usize::from(length_size),
        )
            .map_err(|error| error.to_string())?
            .iter()
            .any(|nal| {
                pygorvid::parse_nal_header(nal)
                    .is_ok_and(|header| header.unit_type == 5)
            });
        if closed {
            starts.push(sample_index);
        }
    }
    if starts.len() < 2 {
        return Ok(None);
    }
    if starts[0] != 0 {
        starts.insert(0, 0);
    }
    let mut groups = Vec::with_capacity(starts.len());
    for (group_index, start) in starts.iter().copied().enumerate() {
        let end = starts.get(group_index + 1).copied().unwrap_or(count);
        let mut frames: Vec<usize> = (start..end).map(|sample| positions[sample]).collect();
        frames.sort_unstable();
        groups.push(frames);
    }
    Ok(Some(groups))
}

type DecodedFrames = Result<Vec<(usize, pygorvid::Yuv420Frame)>, String>;

fn available_memory_bytes() -> Option<usize> {
    std::fs::read_to_string("/proc/meminfo")
        .ok()?
        .lines()
        .find_map(|line| {
            let mut fields = line.split_whitespace();
            if fields.next()? != "MemAvailable:" {
                return None;
            }
            fields.next()?.parse::<usize>().ok()?.checked_mul(1024)
        })
}

fn memory_worker_limit(
    input: &str,
    groups: &[Vec<usize>],
    requested: usize,
) -> Result<usize, String> {
    let Some(available) = available_memory_bytes() else {
        return Ok(requested);
    };
    let reader =
        pygorvid::VideoSampleReader::open(input).map_err(|error| error.to_string())?;
    let Some(sequence_set) = reader.configuration().sequence_parameter_sets.first() else {
        return Ok(requested);
    };
    let sps = pygorvid::parse_sps(sequence_set).map_err(|error| error.to_string())?;
    let frame_bytes = (sps.width as usize)
        .checked_mul(sps.height as usize)
        .and_then(|pixels| pixels.checked_mul(3))
        .and_then(|bytes| bytes.checked_div(2))
        .unwrap_or(0);
    let maximum_frames = groups.iter().map(Vec::len).max().unwrap_or(0);
    if frame_bytes == 0 || maximum_frames == 0 {
        return Ok(requested);
    }
    Ok(requested.min((available / 2) / (frame_bytes * maximum_frames)))
}

fn spawn_gop(
    input: &str,
    frames: &[usize],
    threads: usize,
    verbose: bool,
) -> Receiver<DecodedFrames> {
    let input = input.to_string();
    let frames = frames.to_vec();
    let (sender, receiver) = mpsc::channel();
    thread::spawn(move || {
        let decoded = (|| {
            let reader =
                pygorvid::VideoSampleReader::open(&input).map_err(|error| error.to_string())?;
            let mut decoder =
                pygorvid::VideoDecoder::new(reader).map_err(|error| error.to_string())?;
            decoder.set_threads(threads);
            decoder.set_verbose(verbose);
            frames
                .into_iter()
                .map(|index| {
                    decoder
                        .decode_frame(index as u64)
                        .map(|frame| (index, frame))
                        .map_err(|error| format!("decode frame {index}: {error}"))
                })
                .collect()
        })();
        let _ = sender.send(decoded);
    });
    receiver
}

fn decode_sequential_frames(
    input: &str,
    threads: usize,
    verbose: bool,
    consume: &mut dyn FnMut(usize, pygorvid::Yuv420Frame) -> Result<(), String>,
) -> Result<usize, String> {
    let Some(groups) = closed_gops(input)? else {
        return read_all_frames_internal(input, threads, verbose, consume);
    };
    if threads < 2 {
        return read_all_frames_internal(input, threads, verbose, consume);
    }
    let worker_limit = memory_worker_limit(
        input,
        &groups,
        (threads - 1).min(groups.len() - 1),
    )?;
    if worker_limit == 0 {
        return read_all_frames_internal(input, threads, verbose, consume);
    }
    let mut jobs = std::collections::HashMap::new();
    for group_index in 1..=worker_limit {
        jobs.insert(
            group_index,
            spawn_gop(input, &groups[group_index], threads, verbose),
        );
    }
    let mut next_group = worker_limit + 1;
    let reader = pygorvid::VideoSampleReader::open(input).map_err(|error| error.to_string())?;
    let mut decoder =
        pygorvid::VideoDecoder::new(reader).map_err(|error| error.to_string())?;
    decoder.set_threads(threads);
    decoder.set_verbose(verbose);
    let mut consumed = 0;
    for (group_index, frames) in groups.iter().enumerate() {
        let decoded = if group_index == 0 {
            let mut decoded = Vec::with_capacity(frames.len());
            for &index in frames {
                let frame = decoder
                    .decode_frame(index as u64)
                    .map_err(|error| format!("decode frame {index}: {error}"))?;
                decoded.push((index, frame));
            }
            decoded
        } else {
            jobs.remove(&group_index)
                .ok_or_else(|| "readahead worker is unavailable".to_string())?
                .recv()
                .map_err(|_| "readahead worker ended unexpectedly".to_string())??
        };
        for (index, frame) in decoded {
            consume(index, frame)?;
            consumed += 1;
        }
        if next_group < groups.len() {
            jobs.insert(
                next_group,
                spawn_gop(input, &groups[next_group], threads, verbose),
            );
            next_group += 1;
        }
    }
    Ok(consumed)
}

fn read_all_frames_internal(
    input: &str,
    threads: usize,
    verbose: bool,
    consume: &mut dyn FnMut(usize, pygorvid::Yuv420Frame) -> Result<(), String>,
) -> Result<usize, String> {
    let mut reader = pygorvid::VideoSampleReader::open(input).map_err(|error| error.to_string())?;
    let count = reader.sample_count();
    let mut decoder = pygorvid::VideoDecoder::new(
        pygorvid::VideoSampleReader::open(input).map_err(|error| error.to_string())?,
    )
    .map_err(|error| error.to_string())?;
    decoder.set_threads(threads);
    decoder.set_verbose(verbose);
    for index in 0..count {
        if index > 0
            && reader
                .sample_at(index)
                .map_err(|error| format!("decode frame {index}: read sample: {error}"))?
                .is_sync
        {
            decoder = pygorvid::VideoDecoder::new(
                pygorvid::VideoSampleReader::open(input).map_err(|error| error.to_string())?,
            )
            .map_err(|error| error.to_string())?;
            decoder.set_threads(threads);
            decoder.set_verbose(verbose);
        }
        let frame = decoder
            .decode_frame(index as u64)
            .map_err(|error| format!("decode frame {index}: {error}"))?;
        consume(index, frame)?;
    }
    Ok(count)
}

fn read_all_frames_with_readahead(
    input: &str,
    threads: usize,
    verbose: bool,
    hash_frames: bool,
    readahead: bool,
) -> Result<usize, String> {
    let mut consume = |index: usize, frame: pygorvid::Yuv420Frame| {
        if hash_frames {
            let y = frame.luma_plane().map_err(|error| format!("decode frame {index}: {error}"))?;
            let u = frame.u_plane().map_err(|error| format!("decode frame {index}: {error}"))?;
            let v = frame.v_plane().map_err(|error| format!("decode frame {index}: {error}"))?;
            println!("{input}: frame {index} sha256={}", sha256_planes(&[&y, &u, &v]));
        }
        Ok(())
    };
    if readahead {
        decode_sequential_frames(input, threads, verbose, &mut consume)
    } else {
        read_all_frames_internal(input, threads, verbose, &mut consume)
    }
}

#[cfg(test)]
fn read_all_frames(
    input: &str,
    threads: usize,
    verbose: bool,
    hash_frames: bool,
) -> Result<usize, String> {
    read_all_frames_with_readahead(input, threads, verbose, hash_frames, false)
}

fn sha256_planes(planes: &[&[u8]]) -> String {
    const CONSTANTS: [u32; 64] = [
        0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1, 0x923f82a4,
        0xab1c5ed5, 0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3, 0x72be5d74, 0x80deb1fe,
        0x9bdc06a7, 0xc19bf174, 0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc, 0x2de92c6f,
        0x4a7484aa, 0x5cb0a9dc, 0x76f988da, 0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7,
        0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967, 0x27b70a85, 0x2e1b2138, 0x4d2c6dfc,
        0x53380d13, 0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85, 0xa2bfe8a1, 0xa81a664b,
        0xc24b8b70, 0xc76c51a3, 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070, 0x19a4c116,
        0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
        0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208, 0x90befffa, 0xa4506ceb, 0xbef9a3f7,
        0xc67178f2,
    ];
    let mut state = [
        0x6a09e667_u32,
        0xbb67ae85,
        0x3c6ef372,
        0xa54ff53a,
        0x510e527f,
        0x9b05688c,
        0x1f83d9ab,
        0x5be0cd19,
    ];
    let mut block = [0_u8; 64];
    let mut block_len = 0;
    let mut bit_length = 0_u64;
    let transform = |block: &[u8; 64], state: &mut [u32; 8]| {
        let mut schedule = [0_u32; 64];
        for (index, word) in schedule[..16].iter_mut().enumerate() {
            let offset = index * 4;
            *word = u32::from_be_bytes(block[offset..offset + 4].try_into().unwrap());
        }
        for index in 16..64 {
            let previous_15 = schedule[index - 15];
            let previous_2 = schedule[index - 2];
            let sigma_0 =
                previous_15.rotate_right(7) ^ previous_15.rotate_right(18) ^ (previous_15 >> 3);
            let sigma_1 =
                previous_2.rotate_right(17) ^ previous_2.rotate_right(19) ^ (previous_2 >> 10);
            schedule[index] = schedule[index - 16]
                .wrapping_add(sigma_0)
                .wrapping_add(schedule[index - 7])
                .wrapping_add(sigma_1);
        }
        let [mut a, mut b, mut c, mut d, mut e, mut f, mut g, mut h] = *state;
        for index in 0..64 {
            let big_sigma_1 = e.rotate_right(6) ^ e.rotate_right(11) ^ e.rotate_right(25);
            let choose = (e & f) ^ (!e & g);
            let temporary_1 = h
                .wrapping_add(big_sigma_1)
                .wrapping_add(choose)
                .wrapping_add(CONSTANTS[index])
                .wrapping_add(schedule[index]);
            let big_sigma_0 = a.rotate_right(2) ^ a.rotate_right(13) ^ a.rotate_right(22);
            let majority = (a & b) ^ (a & c) ^ (b & c);
            let temporary_2 = big_sigma_0.wrapping_add(majority);
            h = g;
            g = f;
            f = e;
            e = d.wrapping_add(temporary_1);
            d = c;
            c = b;
            b = a;
            a = temporary_1.wrapping_add(temporary_2);
        }
        for (word, addition) in state.iter_mut().zip([a, b, c, d, e, f, g, h]) {
            *word = word.wrapping_add(addition);
        }
    };
    for plane in planes {
        let mut offset = 0;
        while offset < plane.len() {
            let count = (64 - block_len).min(plane.len() - offset);
            block[block_len..block_len + count].copy_from_slice(&plane[offset..offset + count]);
            block_len += count;
            offset += count;
            bit_length += (count as u64) * 8;
            if block_len == 64 {
                transform(&block, &mut state);
                block_len = 0;
            }
        }
    }
    block[block_len] = 0x80;
    block_len += 1;
    if block_len > 56 {
        block[block_len..].fill(0);
        transform(&block, &mut state);
        block.fill(0);
    } else {
        block[block_len..56].fill(0);
    }
    block[56..].copy_from_slice(&bit_length.to_be_bytes());
    transform(&block, &mut state);
    let mut digest = String::with_capacity(64);
    for word in state {
        use std::fmt::Write;
        write!(&mut digest, "{word:08x}").unwrap();
    }
    digest
}

fn prefixed_output(output: &str, prefix: Option<&str>) -> String {
    let Some(prefix) = prefix else {
        return output.to_string();
    };
    let path = Path::new(output);
    let name = path.file_name().unwrap_or_default().to_string_lossy();
    path.with_file_name(format!("{prefix}{name}"))
        .display()
        .to_string()
}

fn extract_frame(
    input: &str,
    frame: usize,
    output: &str,
    threads: usize,
    verbose: bool,
    hash_frame: bool,
) -> Result<(), String> {
    let extension = Path::new(output)
        .extension()
        .and_then(|value| value.to_str())
        .map(str::to_ascii_lowercase)
        .filter(|value| value == "png" || value == "jpg")
        .ok_or_else(|| "--output must end in .png or .jpg".to_string())?;

    let reader = pygorvid::VideoSampleReader::open(input).map_err(|error| error.to_string())?;
    let mut decoder = pygorvid::VideoDecoder::new_with_environment_checkpoint(reader)
        .map_err(|error| error.to_string())?;
    decoder.set_threads(threads);
    decoder.set_verbose(verbose);
    let decoded = decoder
        .decode_frame(frame as u64)
        .map_err(|error| error.to_string())?;
    if hash_frame {
        let y = decoded.luma_plane().map_err(|error| error.to_string())?;
        let u = decoded.u_plane().map_err(|error| error.to_string())?;
        let v = decoded.v_plane().map_err(|error| error.to_string())?;
        println!(
            "{input}: frame {frame} sha256={}",
            sha256_planes(&[&y, &u, &v])
        );
    }
    let payload = if extension == "png" {
        pygorvid::encode_png_frame(&decoded)
    } else {
        pygorvid::encode_jpeg_frame(&decoded)
    }
    .map_err(|error| error.to_string())?;
    write_output(Path::new(output), &payload).map_err(|error| error.to_string())
}

struct TemporaryOutputDirectory(PathBuf);

impl Drop for TemporaryOutputDirectory {
    fn drop(&mut self) {
        let _ = fs::remove_dir_all(&self.0);
    }
}

fn write_output(output: &Path, payload: &[u8]) -> std::io::Result<()> {
    let parent = output
        .parent()
        .filter(|path| !path.as_os_str().is_empty())
        .unwrap_or(Path::new("."));
    let nonce = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map_err(std::io::Error::other)?
        .as_nanos();
    let mut temporary = None;
    for attempt in 0..100 {
        let directory = parent.join(format!(
            ".pygorvid-{}-{nonce}-{attempt}",
            std::process::id()
        ));
        match fs::create_dir(&directory) {
            Ok(()) => {
                temporary = Some(TemporaryOutputDirectory(directory));
                break;
            }
            Err(error) if error.kind() == std::io::ErrorKind::AlreadyExists => continue,
            Err(error) => return Err(error),
        }
    }
    let temporary = temporary.ok_or_else(|| {
        std::io::Error::new(
            std::io::ErrorKind::AlreadyExists,
            "cannot create temporary output directory",
        )
    })?;
    let image = temporary.0.join("image");
    fs::write(&image, payload)?;
    fs::rename(image, output)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn slideshow_rejects_conflicting_options() {
        for options in [
            vec!["--slideshow", "--readall", "input.mp4"],
            vec!["--slideshow", "--extractframe", "0", "input.mp4"],
            vec!["--slideshow", "--output", "", "input.mp4"],
            vec!["--slideshow", "--generate", "output.mp4"],
        ] {
            assert_eq!(
                run(options.iter().map(|value| value.to_string()).collect()),
                2
            );
        }
    }

    #[cfg(not(feature = "slideshow"))]
    #[test]
    fn slideshow_disabled_reports_missing_feature() {
        assert!(show_slideshow("unused", 1, false)
            .unwrap_err()
            .contains("optional slideshow build feature was not included"));
    }

    #[cfg(feature = "slideshow")]
    #[test]
    fn slideshow_displays_sample() {
        std::env::set_var("SDL_VIDEODRIVER", "dummy");
        let input =
            PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("../samples/decoder-smoke-16x16.mp4");
        assert_eq!(
            show_slideshow(input.to_str().unwrap(), 1, false, false).unwrap(),
            3
        );
    }

    #[test]
    fn generates_native_grey_video() {
        let directory = PathBuf::from(env!("CARGO_MANIFEST_DIR"))
            .join(format!("../tmp/generate-rust-{}", std::process::id()));
        fs::create_dir(&directory).unwrap();
        let cleanup = TemporaryOutputDirectory(directory.clone());
        let output = directory.join("grey video.MP4");
        let output = output.to_str().unwrap();
        let args = |values: &[&str]| values.iter().map(|v| v.to_string()).collect::<Vec<_>>();
        assert_eq!(run(args(&["--generate", output])), 0);
        assert_eq!(run(vec![format!("--generate={output}")]), 0);
        assert_eq!(
            fs::read(output).unwrap(),
            pygorvid::Mp4Writer::new()
                .encode_with_metadata(&pygorvid::H264Encoder::new(), Some(&generation_metadata()))
                .unwrap()
        );
        let reader = pygorvid::VideoSampleReader::open(output).unwrap();
        assert_eq!(reader.sample_count(), 60);
        let configuration = reader.configuration();
        assert_eq!(
            configuration
                .sequence_parameter_sets
                .iter()
                .map(|nal| pygorvid::parse_sps(nal).unwrap().sps_id)
                .collect::<Vec<_>>(),
            [0, 1, 3, 4]
        );
        assert_eq!(configuration.picture_parameter_sets.len(), 10);
        let pps: Vec<_> = configuration
            .picture_parameter_sets
            .iter()
            .map(|nal| pygorvid::parse_pps(nal).unwrap())
            .collect();
        assert_eq!(
            pps.iter()
                .map(|entry| entry.picture_parameter_set_id)
                .collect::<Vec<_>>(),
            (0..10).collect::<Vec<_>>()
        );
        assert_eq!(pps[4].deblocking_filter_control_present, true);
        assert_eq!(pps[5].transform_8x8_mode, true);
        assert_eq!(pps[6].entropy_coding_mode, false);
        assert_eq!(pps[7].transform_8x8_mode, true);
        assert_eq!(pps[8].transform_8x8_mode, true);
        assert_eq!(pps[9].entropy_coding_mode, false);
        let mut reader = reader;
        for index in [0, 1, 2, 59] {
            let sample = reader.sample_at(index).unwrap();
            assert_eq!(sample.dts_ticks, index as i64);
            assert_eq!(sample.pts_ticks, index as i64);
            assert_eq!(sample.duration_ticks, 1);
            assert_eq!(sample.is_sync, index != 2);
            if index == 1 {
                assert!(sample.data.windows(4).any(|bytes| bytes == [0, 0, 3, 1]));
            }
        }
        for (index, expected_nals) in [1_u8, 2, 2, 1, 1, 1, 1, 1, 3].into_iter().enumerate() {
            let sample = reader.sample_at(index + 31).unwrap();
            let nals = pygorvid::split_h264_sample_nals(&sample.data, 4).unwrap();
            assert_eq!(nals.len(), expected_nals as usize, "frame {}", index + 31);
            if index == 8 {
                assert_eq!(
                    nals.iter().map(|nal| nal[0] & 0x1f).collect::<Vec<_>>(),
                    [9, 6, 5]
                );
                assert_eq!(&nals[1][1..3], [5, 0x12]);
            }
        }
        let final_sample = reader.sample_at(59).unwrap();
        let final_nals = pygorvid::split_h264_sample_nals(&final_sample.data, 4).unwrap();
        assert_eq!(
            final_nals
                .iter()
                .map(|nal| nal[0] & 0x1f)
                .collect::<Vec<_>>(),
            [5, 10, 11]
        );
        let mut decoder = pygorvid::H264Decoder::new(reader);
        decoder.set_threads(1);
        for index in 0..60 {
            let frame = decoder.decode_frame(index).unwrap();
            assert_eq!((frame.width, frame.height), (64, 64));
            if ![0, 1, 59].contains(&index) {
                continue;
            }
            let mut expected_luma = vec![128; 4096];
            if index == 1 {
                expected_luma[..3].copy_from_slice(&[0, 0, 1]);
            }
            assert_eq!(frame.y, expected_luma);
            assert_eq!(frame.u, vec![128; 1024]);
            assert_eq!(frame.v, vec![128; 1024]);
        }
        for options in [
            vec!["input.mp4"],
            vec!["--input", "input.mp4"],
            vec!["--readall"],
            vec!["--extractframe", "0"],
            vec!["--output", ""],
        ] {
            let mut values = vec!["--generate", output];
            values.extend(options);
            assert_eq!(run(args(&values)), 2);
        }
        assert_eq!(run(args(&["--generate"])), 2);
        assert_eq!(run(args(&["--generate", "bad.avi"])), 2);
        let blocked = directory.join("directory.mp4");
        fs::create_dir(&blocked).unwrap();
        assert_eq!(run(args(&["--generate", blocked.to_str().unwrap()])), 1);
        assert_eq!(fs::read_dir(&directory).unwrap().count(), 17);
        let sample_counts = [1, 1, 1, 1, 1, 2, 1, 1, 3, 2, 3];
        for (suffix, sample_count) in pygorvid::H264_COMPANION_SUFFIXES[..11]
            .iter()
            .zip(sample_counts)
        {
            let reader =
                pygorvid::VideoSampleReader::open(companion_output_path(Path::new(output), suffix))
                    .unwrap();
            assert_eq!(reader.sample_count(), sample_count, "{suffix}");
            assert_eq!(reader.configuration().sequence_parameter_sets.len(), 1);
            assert_eq!(reader.configuration().picture_parameter_sets.len(), 1);
        }
        for prefix in ["native_", ""] {
            let requested = directory.join("prefixed video.MP4");
            let written = directory.join(format!("{prefix}prefixed video.MP4"));
            assert_eq!(
                run(vec![
                    format!("--generate={}", requested.display()),
                    "--prefix".into(),
                    prefix.into(),
                ]),
                0
            );
            assert_eq!(
                fs::read(&written).unwrap(),
                pygorvid::Mp4Writer::new()
                    .encode_with_metadata(
                        &pygorvid::H264Encoder::new(),
                        Some(&generation_metadata())
                    )
                    .unwrap()
            );
            fs::remove_file(&written).unwrap();
            for suffix in pygorvid::H264_COMPANION_SUFFIXES {
                fs::remove_file(companion_output_path(&written, suffix)).unwrap();
            }
            fs::create_dir(&written).unwrap();
            assert_eq!(
                run(vec![
                    "--generate".into(),
                    requested.to_str().unwrap().into(),
                    format!("--prefix={prefix}"),
                ]),
                1
            );
            fs::remove_dir(&written).unwrap();
            assert_eq!(fs::read_dir(&directory).unwrap().count(), 17);
        }
        drop(cleanup);
    }

    #[test]
    fn generation_preserves_outputs_when_a_companion_target_is_a_directory() {
        let directory = PathBuf::from(env!("CARGO_MANIFEST_DIR")).join(format!(
            "../tmp/generate-rust-blocked-{}",
            std::process::id()
        ));
        fs::create_dir(&directory).unwrap();
        let cleanup = TemporaryOutputDirectory(directory.clone());
        let output = directory.join("grey.mp4");
        fs::write(&output, b"existing output").unwrap();
        let blocked = companion_output_path(&output, pygorvid::H264_COMPANION_SUFFIXES[5]);
        fs::create_dir(&blocked).unwrap();

        assert_eq!(
            run(vec!["--generate".into(), output.display().to_string()]),
            1
        );
        assert_eq!(fs::read(&output).unwrap(), b"existing output");
        assert!(blocked.is_dir());
        assert_eq!(fs::read_dir(&directory).unwrap().count(), 2);
        drop(cleanup);
    }

    #[test]
    fn reads_all_frames_and_accepts_multiple_files() {
        let input =
            PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("../samples/decoder-smoke-16x16.mp4");
        let input = input.to_str().unwrap();
        for threads in [1, 2] {
            assert_eq!(read_all_frames(input, threads, false, false).unwrap(), 3);
        }
        let args = |values: &[&str]| values.iter().map(|value| value.to_string()).collect();
        assert_eq!(
            run(args(&[
                input,
                "--readall",
                "--input",
                input,
                "--threads",
                "2"
            ])),
            0
        );
        assert_eq!(run(args(&["--readall", "missing.mp4", input])), 1);
    }

    #[test]
    fn readahead_matches_sequential_frame_count() {
        let directory = PathBuf::from(env!("CARGO_MANIFEST_DIR")).join(format!(
            "../tmp/readahead-rust-{}",
            std::process::id()
        ));
        fs::create_dir_all(&directory).unwrap();
        let cleanup = TemporaryOutputDirectory(directory.clone());
        let input = directory.join("video.mp4");
        let payload = pygorvid::Mp4Writer::new()
            .encode(&pygorvid::H264Encoder::new());
        fs::write(&input, payload).unwrap();
        let sequential =
            read_all_frames(input.to_str().unwrap(), 2, false, false).unwrap();
        let readahead = read_all_frames_with_readahead(
            input.to_str().unwrap(),
            2,
            false,
            false,
            true,
        )
        .unwrap();
        assert_eq!(readahead, sequential);
        drop(cleanup);
    }

    #[test]
    fn read_all_rejects_extraction_options() {
        for options in [
            vec!["--extractframe", "0"],
            vec!["--output", "out.png"],
            vec!["--extractframe", "0", "--output", "out.png"],
        ] {
            let arguments = [vec!["--readall", "input.mp4"], options].concat();
            assert_eq!(run(arguments.into_iter().map(str::to_string).collect()), 2);
        }
    }

    #[test]
    fn hashes_packed_frame_bytes() {
        assert_eq!(
            sha256_planes(&[b"abc"]),
            "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
        );
        assert_eq!(
            sha256_planes(&[b"", b"abc", b""]),
            "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
        );
    }

    #[test]
    fn hash_requires_decoding_mode() {
        assert_eq!(run(vec!["--hash".into(), "input.mp4".into()]), 2);
    }

    #[test]
    fn read_all_reports_failing_frame() {
        let input =
            PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("../samples/decoder-smoke-16x16.mp4");
        let mut reader = pygorvid::VideoSampleReader::open(&input).unwrap();
        let sample = reader.sample_at(1).unwrap();
        let mut data = fs::read(&input).unwrap();
        let offset = data
            .windows(sample.data.len())
            .position(|bytes| bytes == sample.data)
            .unwrap();
        data[offset..offset + sample.data.len()].fill(0);
        let directory = PathBuf::from(env!("CARGO_MANIFEST_DIR"))
            .join(format!("../tmp/readall-cli-rust-{}", std::process::id()));
        fs::create_dir_all(&directory).unwrap();
        let cleanup = TemporaryOutputDirectory(directory.clone());
        let invalid = directory.join("invalid.mp4");
        fs::write(&invalid, data).unwrap();
        assert!(read_all_frames(invalid.to_str().unwrap(), 1, false, false)
            .unwrap_err()
            .contains("decode frame 1:"));
        drop(cleanup);
    }

    #[test]
    fn parses_threads_option() {
        let args = |values: &[&str]| values.iter().map(|v| v.to_string()).collect::<Vec<_>>();
        let parsed = parse_args(&args(&["--threads", "3", "a.mp4"])).unwrap();
        assert_eq!(parsed.4, 3);
        assert_eq!(parse_args(&args(&["--threads=5", "a.mp4"])).unwrap().4, 5);
        assert_eq!(
            parse_args(&args(&["a.mp4"])).unwrap().4,
            pygorvid::default_decoder_threads()
        );
        assert!(parse_args(&args(&["--threads", "0", "a.mp4"])).is_err());
        assert!(parse_args(&args(&["--threads=x", "a.mp4"])).is_err());
        assert!(!parse_args(&args(&["a.mp4"])).unwrap().9);
        assert!(parse_args(&args(&["--verbose", "a.mp4"])).unwrap().9);
        assert!(parse_args(&args(&["--hash", "a.mp4"])).unwrap().6);
        assert!(parse_args(&args(&["--readahead", "a.mp4"])).unwrap().8);
    }

    #[test]
    fn extracts_native_images_and_preserves_output_on_failure() {
        let directory = PathBuf::from(env!("CARGO_MANIFEST_DIR"))
            .join(format!("../tmp/native-cli-rust-{}", std::process::id()));
        fs::create_dir_all(&directory).unwrap();
        let cleanup = TemporaryOutputDirectory(directory.clone());
        let input =
            PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("../samples/decoder-smoke-16x16.mp4");
        for extension in ["PNG", "JPG"] {
            let output = directory.join(format!("image with spaces.{extension}"));
            extract_frame(
                input.to_str().unwrap(),
                1,
                output.to_str().unwrap(),
                2,
                false,
                true,
            )
            .unwrap();
            let payload = fs::read(&output).unwrap();
            if extension == "PNG" {
                assert_eq!(&payload[..8], b"\x89PNG\r\n\x1a\n");
                assert_eq!(&payload[16..24], &[0, 0, 0, 16, 0, 0, 0, 16]);
            } else {
                assert_eq!(&payload[..2], &[0xff, 0xd8]);
                assert_eq!(&payload[payload.len() - 2..], &[0xff, 0xd9]);
            }
            assert!(extract_frame(
                input.to_str().unwrap(),
                999,
                output.to_str().unwrap(),
                1,
                false,
                false,
            )
            .is_err());
            assert_eq!(fs::read(&output).unwrap(), payload);
        }
        assert_eq!(fs::read_dir(&directory).unwrap().count(), 2);
        drop(cleanup);
    }
}
