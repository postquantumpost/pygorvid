use std::fs;
use std::path::{Path, PathBuf};
use std::process::exit;
use std::time::{SystemTime, UNIX_EPOCH};

fn main() {
    exit(run(std::env::args().skip(1).collect()));
}

fn run(args: Vec<String>) -> i32 {
    let (input, output, frame, prefix, threads, read_all, verbose, mut files) =
        match parse_args(&args) {
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
    if files.is_empty() {
        print_usage();
        return 2;
    }
    if read_all && (frame.is_some() || output.is_some()) {
        eprintln!("--readall cannot be used with --extractframe or --output");
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
        if let Err(error) = extract_frame(&files[0], frame, &output, threads, verbose) {
            eprintln!("{}: {error}", files[0]);
            return 1;
        }
        println!("{}: extracted frame {frame} to {output}", files[0]);
        return 0;
    }
    let mut status = 0;
    for p in &files {
        if read_all {
            match read_all_frames(p, threads, verbose) {
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
        Vec<String>,
    ),
    String,
> {
    let mut input = None;
    let mut output = None;
    let mut frame = None;
    let mut prefix = None;
    let mut threads = pygorvid::default_decoder_threads();
    let mut read_all = false;
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
        } else if arg == "--verbose" {
            verbose = true;
        } else if let Some(value) = arg.strip_prefix("--input=") {
            input = Some(value.to_string());
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
        input, output, frame, prefix, threads, read_all, verbose, files,
    ))
}

fn print_usage() {
    eprintln!("usage: vidprobe [--input FILE] [--threads N] [--verbose] [--readall | --extractframe INDEX --output IMAGE [--prefix TEXT]] FILE...");
}

fn read_all_frames(input: &str, threads: usize, verbose: bool) -> Result<usize, String> {
    let mut reader = pygorvid::VideoSampleReader::open(input).map_err(|error| error.to_string())?;
    let count = reader.sample_count();
    let mut decoder = pygorvid::VideoDecoder::new(
        pygorvid::VideoSampleReader::open(input).map_err(|error| error.to_string())?,
    ).map_err(|error| error.to_string())?;
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
            ).map_err(|error| error.to_string())?;
            decoder.set_threads(threads);
            decoder.set_verbose(verbose);
        }
        decoder
            .decode_frame(index as u64)
            .map_err(|error| format!("decode frame {index}: {error}"))?;
    }
    Ok(count)
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
    fn reads_all_frames_and_accepts_multiple_files() {
        let input =
            PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("../samples/decoder-smoke-16x16.mp4");
        let input = input.to_str().unwrap();
        for threads in [1, 2] {
            assert_eq!(read_all_frames(input, threads, false).unwrap(), 3);
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
        assert!(read_all_frames(invalid.to_str().unwrap(), 1, false)
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
        assert!(!parse_args(&args(&["a.mp4"])).unwrap().6);
        assert!(parse_args(&args(&["--verbose", "a.mp4"])).unwrap().6);
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
                false
            )
            .is_err());
            assert_eq!(fs::read(&output).unwrap(), payload);
        }
        assert_eq!(fs::read_dir(&directory).unwrap().count(), 2);
        drop(cleanup);
    }
}
