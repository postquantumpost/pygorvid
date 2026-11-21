import argparse
import math
import sys
import tempfile
import time
from pathlib import Path

from .colorconv import PixelConverter
from .codec import DecoderError, create_decoder
from .jpegencoder import JPEGEncoder
from .h264encoder import H264Encoder
from .mp4writer import Mp4Metadata, Mp4Writer
from .mp4boxes import Mp4FormatError
from .pixelbuffer import PixelColorRange
from .pngencoder import PNGEncoder
from .videosamplereader import VideoSampleReader
from .vidfile import openfile


def _generation_metadata() -> Mp4Metadata:
    return Mp4Metadata(
        title="Title", artist="Artist", album="Album", publisher="Publisher",
        language="Language", now_playing="Now Playing", album_artist="Album Artist",
        genre="Genre",
        track="1", disc="1", date="2016-12-31 23:59:60 UTC", composer="Composer",
        comment="Comments", description="Description", copyright="Copyright",
        make="Camera make", model="Camera model", firmware="firmware version",
        location="11.3733\u00b0 N 142.5917\u00b0 E", altitude="-10925.0 meters",
        heading="180.0", encoder="pygorvid",
    )


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


def _positive_int(value: str) -> int:
    try:
        count = int(value)
    except ValueError as error:
        raise argparse.ArgumentTypeError("must be an integer") from error
    if count < 1:
        raise argparse.ArgumentTypeError("must be a positive integer")
    return count


def _extract_frame(
    input_path: str, frame: int, output_path: str,
    threads: int = 1, verbose: bool = False,
) -> None:
    output = Path(output_path)
    extension = output.suffix.lower()
    if extension not in {".png", ".jpg"}:
        raise ValueError("--output must end in .png or .jpg")
    reader = VideoSampleReader(input_path)
    try:
        decoded = create_decoder(
            reader, threads=threads, debug_progress=verbose
        ).decode_frame(frame)
    finally:
        reader.close()
    rgb = PixelConverter(PixelColorRange.LIMITED).convert_yuv420_to_rgb(
        decoded.width, decoded.height, decoded.y, decoded.u, decoded.v,
        decoded.y_stride, decoded.u_stride, decoded.v_stride,
    )
    payload = PNGEncoder().encode(rgb) if extension == ".png" else JPEGEncoder().encode(rgb)
    with tempfile.TemporaryDirectory(prefix=".pygorvid-", dir=output.parent) as directory:
        temporary = Path(directory) / "image"
        temporary.write_bytes(payload)
        temporary.replace(output)


def _read_all_frames(
    input_path: str, threads: int = 1, verbose: bool = False,
) -> int:
    reader = VideoSampleReader(input_path)
    try:
        decoder = create_decoder(reader, threads=threads, debug_progress=verbose)
        decoder.frame_cache_size = reader.sample_count
        for index in range(reader.sample_count):
            try:
                if index > 0 and reader.sample_at(index).is_sync:
                    decoder = create_decoder(
                        reader, threads=threads, debug_progress=verbose
                    )
                decoder.decode_frame(index)
            except (OSError, ValueError, DecoderError, Mp4FormatError) as error:
                raise DecoderError(f"decode frame {index}: {error}") from error
        return reader.sample_count
    finally:
        reader.close()


def _slideshow(
    input_path: str, threads: int = 1, verbose: bool = False,
) -> int:
    from .slideshow import SlideshowWindow

    window = SlideshowWindow(input_path)
    try:
        reader = VideoSampleReader(input_path)
        try:
            decoder = create_decoder(
                reader, threads=threads, debug_progress=verbose
            )
            if reader.configuration is None:
                raise Mp4FormatError("video configuration is unavailable")
            timescale = reader.configuration.timescale
            presentation_order = reader.presentation_sample_indices()
            decoder.frame_cache_size = len(presentation_order)
            deadline_seconds = time.monotonic()
            count = 0
            for index, sample_index in enumerate(presentation_order):
                if not window.is_open():
                    break
                try:
                    sample = reader.sample_at(sample_index)
                    frame = decoder.decode_frame(index)
                except (OSError, ValueError, DecoderError, Mp4FormatError) as error:
                    raise DecoderError(f"decode frame {index}: {error}") from error
                if not window.wait_until(deadline_seconds):
                    break
                window.show(frame)
                count += 1
                deadline_seconds = time.monotonic() + sample.duration_ticks / timescale
            if count:
                window.wait_until(deadline_seconds)
            return count
        finally:
            reader.close()
    finally:
        window.close()


def main(argv=None) -> int:
    parser = argparse.ArgumentParser(prog="vidprobe")
    parser.add_argument("--input", help="first input video file")
    parser.add_argument("--generate", help="generate 64x64 H.264 feature-test video with stereo silence (.mp4)")
    parser.add_argument("--output", help="output image path (.png or .jpg)")
    parser.add_argument("--prefix", help="prefix prepended to the output filename")
    parser.add_argument("--readall", action="store_true", help="decode all video frames without writing images")
    parser.add_argument("--slideshow", action="store_true", help="display decoded frames in an SDL2 window")
    parser.add_argument("--verbose", action="store_true", help="print decoder progress and debug information")
    parser.add_argument(
        "--extractframe",
        type=_nonnegative_int,
        help="zero-based frame index to extract",
    )
    parser.add_argument(
        "--threads",
        type=_positive_int,
        default=1,
        help="number of decoder threads (currently reserved for future parallel decoding)",
    )
    parser.add_argument("files", nargs="*")
    args = parser.parse_args(sys.argv[1:] if argv is None else argv)
    files = ([args.input] if args.input is not None else []) + args.files
    if args.generate is not None:
        if files or args.readall or args.slideshow or args.extractframe is not None or args.output is not None:
            parser.error("--generate cannot be combined with inputs, --readall, --slideshow, --extractframe or --output")
        output = Path(args.generate)
        if output.suffix.lower() != ".mp4":
            parser.error("--generate must end in .mp4")
        try:
            if args.prefix is not None:
                output = output.with_name(args.prefix + output.name)
            payload = Mp4Writer().encode(H264Encoder(), _generation_metadata())
            companions = H264Encoder().encode_companion_streams()
            if output.is_dir():
                raise IsADirectoryError(f"{output} is a directory")
            with tempfile.TemporaryDirectory(prefix=".pygorvid-", dir=output.parent) as directory:
                staged = {}
                for suffix, companion in companions.items():
                    companion_output = output.with_name(
                        f"{output.stem}-{suffix}"
                    )
                    if companion_output.is_dir():
                        raise IsADirectoryError(
                            f"{companion_output} is a directory"
                        )
                    companion_temporary = Path(directory) / suffix
                    companion_temporary.write_bytes(companion)
                    staged[companion_temporary] = companion_output
                temporary = Path(directory) / "video"
                temporary.write_bytes(payload)
                for companion_temporary, companion_output in staged.items():
                    companion_temporary.replace(companion_output)
                temporary.replace(output)
        except (OSError, ValueError) as error:
            print(f"{output}: {error}", file=sys.stderr)
            return 1
        print(f"{output}: generated one second of 64x64 H.264 feature-test video with stereo silence")
        return 0
    if not files:
        parser.print_usage(sys.stderr)
        return 2
    if args.readall and args.slideshow:
        parser.error("--readall cannot be combined with --slideshow")
    if (args.readall or args.slideshow) and (args.extractframe is not None or args.output is not None):
        parser.error("--readall and --slideshow cannot be used with --extractframe or --output")
    if (args.extractframe is None) != (args.output is None):
        parser.error("--extractframe and --output must be used together")
    if args.extractframe is not None:
        if len(files) != 1:
            parser.error("frame extraction requires exactly one input file")
        output = Path(args.output)
        if args.prefix is not None:
            output = output.with_name(args.prefix + output.name)
        try:
            _extract_frame(
                files[0], args.extractframe, str(output), args.threads, args.verbose
            )
        except (OSError, ValueError, DecoderError, Mp4FormatError) as e:
            print(f"{files[0]}: {e}", file=sys.stderr)
            return 1
        print(f"{files[0]}: extracted frame {args.extractframe} to {output}")
        return 0
    status = 0
    for path in files:
        if args.slideshow:
            from .slideshow import SlideshowError
            try:
                count = _slideshow(path, args.threads, args.verbose)
            except (OSError, ValueError, DecoderError, Mp4FormatError, SlideshowError) as error:
                print(f"{path}: {error}", file=sys.stderr)
                status = 1
            else:
                print(f"{path}: displayed {count} frames")
            continue
        if args.readall:
            try:
                count = _read_all_frames(path, args.threads, args.verbose)
            except (OSError, ValueError, DecoderError, Mp4FormatError) as error:
                print(f"{path}: {error}", file=sys.stderr)
                status = 1
            else:
                print(f"{path}: read {count} frames")
            continue
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
