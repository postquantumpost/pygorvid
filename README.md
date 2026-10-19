# pygorvid
A video file support library for python, go, and rust, to reduce dependency on ffmpeg.

## Version 1.0

Version 1.0.0 provides independent Python, Go, and Rust install images. Run
`make release` to build them into `dist/`, or use `make release-python`,
`make release-go`, or `make release-rust` for one language. Python includes a
pip-installable wheel and source distribution; Go and Rust include library SDKs
and prebuilt Linux commands. See [installation and release build instructions](docs/INSTALL.md)
for Linux Mint setup, toolchain requirements, and use from other projects.

Version 1.0 does not expand the codec subset described below.

`vidprobe` reports basic file information for one or more input files. `--input`
adds a file that is processed before any positional input files. To extract a
zero-based video frame as PNG or JPEG, provide one input file and both options:

```text
vidprobe --input input.mp4 --extractframe 12 --output bob.png --prefix j_
```

This writes `j_bob.png`. The prefix is added to the output filename, not its
directory or extension.

To decode every video frame without writing images, use `--readall`:

```text
vidprobe --readall --threads 4 first.mp4 second.mp4
```

Each successful input reports its decoded frame count. A decode failure reports
the zero-based frame index, continues with subsequent inputs, and produces exit
status 1. `--readall` cannot be combined with `--extractframe` or `--output`.
This uses the same native MP4/H.264 decoder and supported codec subset as frame
extraction; it does not just read compressed samples.

`--threads N` sets the number of decoder threads (default: half the available
processors). Output is identical for every thread count. The Python decoder uses
forked worker processes for this, since CPython threads share one interpreter lock.
`--verbose` enables decoder progress and diagnostic output; without it, the tool
prints only its normal per-file result.

Go, Python, and Rust frame extraction use the native MP4 reader, H.264 decoder,
RGB conversion, and PNG/JPEG encoders. No runtime `ffmpeg` or external image
library is required. Extraction supports the implemented progressive, 8-bit
4:2:0 CABAC H.264 subset; unsupported streams fail explicitly.

The MP4 box readers recognize HEVC `hvc1`/`hev1` sample entries and parse `hvcC`
configuration records, including profile, level, bit depths, NAL length size,
and parameter-set arrays. HEVC decoding is not implemented. In particular,
`samples/PXL_20261014_223017213.mp4` is HEVC Main 10, not H.264, and cannot yet
be decoded by `--readall` or frame extraction.

### Codec Source Isolation

All three implementations use the same source organization:

| Responsibility | Rust | Go | Python |
| --- | --- | --- | --- |
| H.264 decoder/support | `r/src/h264_*.rs` | `g/vid/h264_*.go` | `p/src/pygorvid/h264_*.py` |
| HEVC decoder/support | `r/src/hevc_*.rs` | `g/vid/hevc_*.go` | `p/src/pygorvid/hevc_*.py` |
| Shared selection/errors | `r/src/codec.rs` | `g/vid/codec.go` | `p/src/pygorvid/codec.py` |
| Shared raw frames/framing | `codec_frame.rs`, `codec_sample.rs` | `codec_frame.go`, `codec_sample.go` | `codec_frame.py`, `codec_sample.py` |

MP4 box walking, sample offsets/timing, raw pixel storage, and image output are
codec-neutral. Codec-specific configuration parsing belongs in `h264_mp4` or
`hevc_mp4`; HEVC code must not be called by H.264 implementations. Python's
previous module paths remain compatibility aliases. The CLI selects the decoder
from the video's sample entry rather than assuming H.264.

### HEVC Licensing Assertion

HEVC decoder objects check `HEVC_IS_LICENSED` when constructed. Only the exact,
case-sensitive value `true` asserts licensing:

| Environment | Error Code | Message |
| --- | --- | --- |
| Unset or anything except `true` | `NOT_IMPLEMENTED_AND_NOT_LICENSED` | `HEVC decoding is not implemented and not licensed` |
| `HEVC_IS_LICENSED=true` | `NOT_IMPLEMENTED` | `HEVC decoding is not implemented` |

For testing and development, the assertion can be supplied per invocation:

```sh
HEVC_IS_LICENSED=true r/pygorvid --readall samples/PXL_20261014_223017213.mp4
```

This variable neither implements HEVC decoding nor grants a patent license.
After shipment, the end user is responsible for obtaining and managing any
required licenses and setting the assertion appropriately. HEVC-specific
implementations must remain in HEVC source files, not shared utilities or
other codecs. Source separation is an engineering boundary, not a legal
determination of patent coverage or clearance, including for shared code or
other codecs. MP4 metadata inspection and compressed-sample reading do not
invoke the decoder licensing check.

Remaining HEVC implementation steps, to be kept aligned across Rust, Go, and
Python:
1. Add HEVC NAL/RBSP parsing and VPS/SPS/PPS syntax parsing.
2. Add HEVC in-band parameter-set updates to compressed-sample processing;
	generic compressed-sample reading already uses `hvcC` NAL length sizes.
3. Parse slice headers, reference-picture sets, and decoded-picture buffering.
4. Decode coding-tree units, HEVC CABAC syntax, and transform coefficients.
5. Implement Main 10 intra/inter prediction and inverse transforms.
6. Implement HEVC deblocking and sample-adaptive-offset filtering.
7. Add 10-bit output conversion and wire presentation-order frames into the CLI;
	verify all 106 frames of this sample against reference output.

From the repository root:

```sh
g/pygorvid --input input.mp4 --extractframe 0 --output go.jpg
p/pygorvid --input input.mp4 --extractframe 0 --output python.png
r/pygorvid --input input.mp4 --extractframe 0 --output rust.jpg
```

Frame indices are zero-based presentation indices. Output extensions select the
encoder, and files are replaced only after successful decoding and encoding.
Temporary output directories are cleaned up on success or failure. Python and
Rust decoder checkpoints can be enabled with `PYGORVID_CHECKPOINT_DIR`.

The Python version works as well as the others but is too slow to actually
use.

What Is Supported
Only a subset of mp4, H.264 for decode is supported right now.  It's the
subset needed by the videos I'm working with right now.

What is tested and working:
- Elgato
- OBS
- Linux Video Trimmer

What is not tested but I have sample videos available:
- Android phone video, horizontal
- Android phone video, vertical
- DJI drone video
- Dashcam video, low resolution
- Dashcam video, high resolution

What is not tested:
- full set of ITU sample data
- Everything else
