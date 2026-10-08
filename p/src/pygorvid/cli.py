import argparse
import math
import os
import subprocess
import sys
import tempfile
from pathlib import Path

from .vidfile import openfile


def _format_duration_hms(seconds: float) -> str:
    total = math.ceil(seconds)
    hours, remainder = divmod(total, 3600)
    minutes, seconds_part = divmod(remainder, 60)
    if hours:
        return f"{hours}:{minutes:02d}:{seconds_part:02d}"
    if minutes:
        return f"{minutes:02d}:{seconds_part:02d}"
    return f"{seconds_part:02d}"


def _nonnegative_int(value: str) -> int:
    try:
        frame = int(value)
    except ValueError as e:
        raise argparse.ArgumentTypeError("must be an integer") from e
    if frame < 0:
        raise argparse.ArgumentTypeError("must be non-negative")
    return frame


def _extract_frame(input_path: str, frame: int, output_path: str) -> None:
    extension = Path(output_path).suffix.lower()
    if extension not in {".png", ".jpg"}:
        raise ValueError("--output must end in .png or .jpg")
    with openfile(input_path) as video:
        if not video.isopen():
            raise ValueError(video.errorinfo()[0])
        if video.format != "mp4":
            raise ValueError("frame extraction requires an MP4 file")
        video.getbasicinfo()
        error = video.errorinfo()[0]
        if error:
            raise ValueError(error)

    output = Path(output_path)
    with tempfile.TemporaryDirectory(prefix=".vidprobe-", dir=output.parent or ".") as temp_dir:
        temp_output = Path(temp_dir) / f"frame{extension}"
        command = [
            "ffmpeg",
            "-v",
            "error",
            "-i",
            input_path,
            "-map",
            "0:v:0",
            "-vf",
            f"select=eq(n\\,{frame})",
            "-fps_mode",
            "passthrough",
            "-frames:v",
            "1",
            "-y",
            str(temp_output),
        ]
        try:
            result = subprocess.run(command, capture_output=True, text=True)
        except OSError as e:
            raise ValueError(f"could not run ffmpeg: {e}") from e
        if result.returncode:
            raise ValueError(result.stderr.strip() or "ffmpeg failed")
        if not temp_output.is_file() or temp_output.stat().st_size == 0:
            raise ValueError("requested frame does not exist or produced an empty image")
        os.replace(temp_output, output)


def main(argv=None) -> int:
    parser = argparse.ArgumentParser(prog="vidprobe")
    parser.add_argument("--input", help="first input video file")
    parser.add_argument("--output", help="output image path (.png or .jpg)")
    parser.add_argument("--prefix", help="prefix prepended to the output filename")
    parser.add_argument(
        "--extractframe",
        type=_nonnegative_int,
        help="zero-based frame index to extract",
    )
    parser.add_argument("files", nargs="*")
    args = parser.parse_args(sys.argv[1:] if argv is None else argv)
    files = ([args.input] if args.input is not None else []) + args.files
    if not files:
        parser.print_usage(sys.stderr)
        return 2
    if (args.extractframe is None) != (args.output is None):
        parser.error("--extractframe and --output must be used together")
    if args.extractframe is not None:
        if len(files) != 1:
            parser.error("frame extraction requires exactly one input file")
        output = Path(args.output)
        if args.prefix is not None:
            output = output.with_name(args.prefix + output.name)
        try:
            _extract_frame(files[0], args.extractframe, str(output))
        except (OSError, ValueError) as e:
            print(f"{files[0]}: {e}", file=sys.stderr)
            return 1
        print(f"{files[0]}: extracted frame {args.extractframe} to {output}")
        return 0
    status = 0
    for path in files:
        with openfile(path) as v:
            if v.isopen():
                info = v.getbasicinfo()
                err = v.errorinfo()[0]
                if err:
                    print(f"{path}: {v.format}: {err}", file=sys.stderr)
                    status = 1
                else:
                    duration_text = ", ".join(
                        f"{stream.duration_seconds:g} seconds ({_format_duration_hms(stream.duration_seconds)})"
                        for stream in info.videostreams
                    )
                    suffix = f" video_durations=[{duration_text}]" if duration_text else ""
                    print(f"{path}: {v.format} {info}{suffix}")
            else:
                print(f"{path}: {v.errorinfo()[0]}", file=sys.stderr)
                status = 1
    return status


if __name__ == "__main__":
    sys.exit(main())
