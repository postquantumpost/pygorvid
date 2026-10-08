use std::fs;
use std::io;
use std::path::{Path, PathBuf};
use std::process::{exit, Command};

fn main() {
    exit(run(std::env::args().skip(1).collect()));
}

fn run(args: Vec<String>) -> i32 {
    let (input, output, frame, prefix, mut files) = match parse_args(&args) {
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
        if let Err(error) = extract_frame(&files[0], frame, &output) {
            eprintln!("{}: {error}", files[0]);
            return 1;
        }
        println!("{}: extracted frame {frame} to {output}", files[0]);
        return 0;
    }
    let mut status = 0;
    for p in &files {
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
        Vec<String>,
    ),
    String,
> {
    let mut input = None;
    let mut output = None;
    let mut frame = None;
    let mut prefix = None;
    let mut files = Vec::new();
    let mut index = 0;
    while index < args.len() {
        let arg = &args[index];
        if arg == "--" {
            files.extend(args[index + 1..].iter().cloned());
            break;
        }
        if arg == "--input" || arg == "--output" || arg == "--extractframe" || arg == "--prefix" {
            index += 1;
            let value = args
                .get(index)
                .ok_or_else(|| format!("{arg} requires a value"))?;
            match arg.as_str() {
                "--input" => input = Some(value.clone()),
                "--output" => output = Some(value.clone()),
                "--prefix" => prefix = Some(value.clone()),
                _ => {
                    frame = Some(value.parse::<usize>().map_err(|_| {
                        "--extractframe must be a non-negative integer".to_string()
                    })?);
                }
            }
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
        } else if arg.starts_with('-') {
            return Err(format!("unknown option: {arg}"));
        } else {
            files.push(arg.clone());
        }
        index += 1;
    }
    Ok((input, output, frame, prefix, files))
}

fn print_usage() {
    eprintln!("usage: vidprobe [--input FILE] [--extractframe INDEX --output IMAGE [--prefix TEXT]] FILE...");
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

struct TempDir(PathBuf);

impl Drop for TempDir {
    fn drop(&mut self) {
        let _ = fs::remove_dir_all(&self.0);
    }
}

fn create_temp_dir(parent: &Path) -> Result<TempDir, String> {
    for suffix in 0..100 {
        let path = parent.join(format!(".vidprobe-{}-{suffix}", std::process::id()));
        match fs::create_dir(&path) {
            Ok(()) => return Ok(TempDir(path)),
            Err(error) if error.kind() == io::ErrorKind::AlreadyExists => continue,
            Err(error) => return Err(error.to_string()),
        }
    }
    Err("could not create temporary output directory".to_string())
}

fn extract_frame(input: &str, frame: usize, output: &str) -> Result<(), String> {
    let extension = Path::new(output)
        .extension()
        .and_then(|value| value.to_str())
        .map(str::to_ascii_lowercase)
        .filter(|value| value == "png" || value == "jpg")
        .ok_or_else(|| "--output must end in .png or .jpg".to_string())?;

    let mut video = pygorvid::open_file(input);
    if !video.isopen() {
        return Err(video.errorinfo().0);
    }
    if video.format() != "mp4" {
        return Err("frame extraction requires an MP4 file".to_string());
    }
    video.getbasicinfo();
    let error = video.errorinfo().0;
    if !error.is_empty() {
        return Err(error);
    }

    let output_path = Path::new(output);
    let parent = output_path
        .parent()
        .filter(|path| !path.as_os_str().is_empty())
        .unwrap_or_else(|| Path::new("."));
    let temp_dir = create_temp_dir(parent)?;
    let temp_output = temp_dir.0.join(format!("frame.{extension}"));
    let filter = format!("select=eq(n\\,{frame})");
    let result = Command::new("ffmpeg")
        .args([
            "-v",
            "error",
            "-i",
            input,
            "-map",
            "0:v:0",
            "-vf",
            &filter,
            "-fps_mode",
            "passthrough",
            "-frames:v",
            "1",
            "-y",
        ])
        .arg(&temp_output)
        .output()
        .map_err(|error| format!("could not run ffmpeg: {error}"))?;
    if !result.status.success() {
        let message = String::from_utf8_lossy(&result.stderr).trim().to_string();
        return Err(if message.is_empty() {
            format!("ffmpeg exited with {}", result.status)
        } else {
            message
        });
    }
    let metadata = fs::metadata(&temp_output)
        .map_err(|_| "requested frame does not exist or produced no image".to_string())?;
    if metadata.len() == 0 {
        return Err("requested frame does not exist or produced an empty image".to_string());
    }
    fs::rename(temp_output, output_path).map_err(|error| error.to_string())
}
