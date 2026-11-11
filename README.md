# pygorvid
A video file support library for Python, Go, Rust, and C++, to reduce dependency on ffmpeg.

Much of this code was developed with AI tools including code generation.

## Version 1.0

Version 1.0.0 provides independent Python, Go, Rust, and C++ install images. Run
`make release` to build them into `dist/`, or use `make release-python`,
`make release-go`, `make release-rust`, or `make release-cpp` for one language. Python includes a
pip-installable wheel and source distribution; Go and Rust include library SDKs
and prebuilt Linux commands. C++ includes a C++17 SDK and metadata probe
command, compressed H.264 sample reading, progressive frame reconstruction,
PNG encoding, and single-frame extraction. See
[installation and release build instructions](docs/INSTALL.md)
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

For a late H.264 framing or slice-header error, inspect only the selected
presentation frame without decoding its predecessors:

```sh
timeout 60s cargo run --release --quiet --manifest-path r/Cargo.toml \
  --example check_h264_sample -- samples/rannirune1d.mp4 943
```

The diagnostic reads MP4 metadata, maps the presentation index to its decode-order
sample, seeks directly to that sample, and checks its NAL framing, EBSP and slice
headers. It reports compressed byte counts and NAL tails; it does not reconstruct
pixels or validate reference pictures. Frame 943 in this file maps to sample 944
and requires only 14,981 compressed bytes, not the preceding 943 frames.

All four H.264 decoders recover a narrowly recognized malformed MP4 encoding:
a three- or four-byte Annex B start code followed by an empty end-of-sequence
(`0a`) or end-of-stream (`0b`) NAL appended inside the sample's final VCL NAL
(type 1 or 5). The H.264 sample splitter separates this marker before strict
EBSP conversion. Generic MP4 sample bytes and length-prefix splitting are
unchanged, as are HEVC framing and rejection of other malformed EBSP sequences.
This is compatibility handling, not conformant MP4 NAL encapsulation.

The compact `testdata/h264/rannirune-eos.hex` regression contains the original
SPS, PPS and failing 1,092-byte final NAL from that sample (one hex record per
line). `weighted-eos-32x32.mp4` is derived from `weighted-32x32.mp4` by appending
the same five-byte marker to its final slice and adjusting the NAL length,
sample size and `mdat` size. It must decode to the original reference YUV in
all four languages with one and four threads.

`--threads N` sets the number of decoder threads (default: half the available
processors). Output is identical for every thread count. The Python decoder uses
forked worker processes for this, since CPython threads share one interpreter lock.
`--verbose` enables decoder progress and diagnostic output; without it, the tool
prints only its normal per-file result.

Go, Python, and Rust frame extraction use the native MP4 reader, H.264 decoder,
RGB conversion, and PNG/JPEG encoders. No runtime `ffmpeg` or external image
library is required. Extraction supports the implemented progressive, 8-bit
4:2:0 H.264 subset with CABAC or CAVLC entropy coding; unsupported streams
fail explicitly.

All four implementations use the same default JPEG profile: quality 75,
JFIF 1.01, baseline 8-bit YCbCr 4:4:4, matching quantization and Huffman
tables, and edge-replicated 8x8 blocks. RGB-to-YCbCr conversion uses the same
integer coefficients. The DCT uses a shared Q20 integer cosine basis and
64-bit accumulation, with a final scale of `8 * 2^40` and quantization ties
rounded away from zero. This avoids platform math-library differences at
half-integer coefficients. Identical decoded RGB pixels produce identical
JPEG bytes; older versions may produce different, but valid, JPEG encodings.
The shared `testdata/jpeg-parity-18x10.hex` regression contains width, height,
hex Y/U/V planes, and expected JPEG bytes. It covers positive and negative
quantization ties, color clipping, DC prediction, and partial edge blocks.

Weighted prediction is supported for this subset: explicit P/B slice weight
tables and implicit B-slice POC-derived weights apply to luma and both chroma
planes, including skip/direct partitions, before residual reconstruction.
B-direct neighbors are excluded from CABAC reference-index and MVD context
increments, while their motion vectors remain available for prediction.
The eight-frame fading test pattern in `testdata/h264/weighted-32x32.mp4`
exercises explicit P and implicit B weighting and is checked against reference
YUV with one and four decoder threads. It was encoded with x264 parameters
`keyint=30:scenecut=0:bframes=2:b-adapt=0:ref=1:weightp=2:weightb=1`.
Reference-list reordering uses the signaled active count, allowing the same
stored picture to occupy multiple entries with different prediction weights.

CAVLC support includes slice/macroblock syntax, skip runs, coefficient decoding,
neighboring coefficient-count contexts, and reconstruction for I/P/B pictures.
The implementations live in codec-specific `h264_cavlc*` files and reuse the
existing H.264 prediction, inverse transforms, and filtering. A compact eight-frame
32x32 I/P/B fixture in `testdata/h264/cavlc-32x32.mp4` is compared against raw
reference YUV output in all four languages, including multithreaded decoding.

The MP4 box readers recognize HEVC `hvc1`/`hev1` sample entries and parse `hvcC`
configuration records, including profile, level, bit depths, NAL length size,
and parameter-set arrays. HEVC decoding is not implemented. In particular,
`samples/PXL_20261014_223017213.mp4` is HEVC Main 10, not H.264, and cannot yet
be decoded by `--readall` or frame extraction.

### Codec Source Isolation

The Rust, Go, and Python codec implementations use the same source organization.
The C++ port currently contains MP4 compressed-sample reading, shared bit
reading, H.264 parameter-set and slice parsing, CABAC and CAVLC syntax and
residual decoding, frame reconstruction, and PNG encoding. It supports a
restricted progressive 8-bit 4:2:0 subset of IDR, P, and B pictures, including
I_PCM, I16x16, I4x4, skipped, and inter-partition macroblocks. Inter prediction
supports multiple active references, parsed reference-list modifications, and
a subset of adaptive reference marking; luma motion compensation supports
quarter-pel positions and chroma supports fractional 4:2:0 positions. Default
scaling lists and deblocking are implemented for the supported syntax paths.
This is not a general-purpose H.264 decoder: field/MBAFF pictures, full
long-term reference management, weighted B prediction, custom scaling lists,
I8x8 prediction, and other unimplemented syntax remain unsupported.

The C++ CLI extracts a presentation frame by mapping its PTS-ordered index to
the corresponding decode-order sample and decoding preceding supported
pictures:

```sh
vidprobe-cpp --extractframe 0 --output frame.png input.mp4
```

`--extractframe` uses a zero-based presentation-frame index. Extraction
requires one input, both options, and an IDR followed only by pictures in the
supported subset.
The PNG writer converts 8-bit 4:2:0 samples to RGB using limited-range BT.601
coefficients.
Unsupported streams fail explicitly:

| Responsibility | Rust | Go | Python | C++ |
| --- | --- | --- | --- | --- |
| H.264 decoder/support | `r/src/h264_*.rs` | `g/vid/h264_*.go` | `p/src/pygorvid/h264_*.py` | MP4 samples, restricted progressive IDR/P/B reconstruction with CABAC/CAVLC, short-term multiple references, and fractional motion |
| HEVC decoder/support | `r/src/hevc_*.rs` | `g/vid/hevc_*.go` | `p/src/pygorvid/hevc_*.py` | Not yet ported |
| Shared selection/errors | `r/src/codec.rs` | `g/vid/codec.go` | `p/src/pygorvid/codec.py` | Not yet ported |
| Raw frames/framing | `codec_frame.rs`, `codec_sample.rs` | `codec_frame.go`, `codec_sample.go` | `codec_frame.py`, `codec_sample.py` | Not yet ported |

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
- Dashcam video, low resolution
- Dashcam video, high resolution

What is not supported
- Android phone video, horizontal, it uses HEVC
- Android phone video, vertical, it uses HEVC
- DJI drone video, it uses HEVC

What is not tested but I have sample videos available:

What is not tested:
- full set of ITU sample data
- Everything else
