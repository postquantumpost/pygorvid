# Silkroad Cross-Language Contract

This document defines behavior that Go, Python, and Rust implementations must share. Language-specific names and error types may differ, but observable results, frame selection, pixel layout, and failure semantics must agree.

## Input and Corpus

- The initial working corpus is the set in `silkroad1.sha256`: MP4 files no larger than 15,000,000 bytes (15 MB decimal).
- Files above the limit remain untouched and are listed as excluded in `silkroad1-streams.json`.
- The first decoder target is the observed active subset: MP4 `avc1` H.264 High Profile, 8-bit `yuv420p`, progressive scan, four-byte NAL lengths, CABAC, and I/P/B slices at the inventoried levels and dimensions.
- Any H.264 tool not explicitly included in the feature matrix is unsupported until its syntax and behavior are audited. Never guess that an unknown tool is absent; return an unsupported-feature error.
- Frame extraction uses the first video stream only. Audio and other streams are ignored by extraction.

## CLI Semantics

- `--input FILE` adds the first input; positional files follow it in processing order.
- `--extractframe INDEX` uses a nonnegative, zero-based frame index in presentation order.
- Extraction requires `--extractframe` and `--output` together and exactly one input file.
- `--output` must end in `.png` or `.jpg`, case-insensitively. The suffix selects the encoder.
- `--prefix TEXT` prepends to the output basename, preserving its directory and extension. If no image is emitted, the prefix is accepted but has no effect.
- Metadata-only invocation continues to process all inputs and print each complete `BasicInfo` value.

## Native Object Boundaries

Each implementation follows the same pipeline and owns state explicitly:

1. `VideoSampleReader` opens one MP4 track and supplies AVC configuration, compressed access units, timestamps, and sync information.
2. `H264Decoder` owns SPS/PPS state, reference pictures, and decode/presentation reordering. `decode_frame(index)` returns the requested presentation-order frame or a typed error.
3. `Yuv420Frame` represents decoded planes before color conversion.
4. `PixelConverter` converts supported colorimetry to packed RGB24.
5. `PNGEncoder` or `JPEGEncoder` writes the selected image format.

Reader/decoder state is per input stream, not global. The returned frame is independent of decoder-owned reference buffers; callers may retain it after the decoder advances or closes. Encoders do not mutate their input frame.

## VideoSampleReader Contract

Each language provides a `VideoSampleReader` with equivalent behavior:

- `open(path)` opens one MP4 and selects its first `vide` track. It fails if the container is malformed, there is no video track, or the selected sample description is outside the active feature matrix.
- `configuration()` returns immutable/caller-owned AVC configuration: NAL length-prefix size, SPS/PPS byte arrays, and the track media timescale.
- `next_sample()` returns one compressed MP4 video sample, or a distinct end-of-stream result. For the active corpus, each MP4 sample must be validated as one access unit before the decoder consumes it.
- The sample's `data` is exactly the MP4 sample payload with its original length-prefixed NAL units; it is not Annex B and excludes box headers.
- Each sample carries a zero-based decode-order `index`, signed `dts_ticks` and `pts_ticks`, nonnegative `duration_ticks`, and `is_sync`. Timestamps use the track's media timescale; PTS is DTS plus the `ctts` composition offset, or DTS when `ctts` is absent.
- `is_sync` comes from `stss`; when `stss` is absent, samples are treated as sync samples as defined by the container format.
- `close()` is idempotent. Calls after close return a closed-reader error. File data and returned sample buffers have explicit ownership; a returned sample remains valid after the next read and after close.

Idiomatic method shapes may differ: Go may return `(sample, ok, error)`, Python may return a sample or `None` at EOF, and Rust may return `Result<Option<Sample>, Error>`. Error categories remain equivalent: I/O, malformed/truncated MP4 tables or sample data, unsupported codec/sample entry, no video track, and closed reader. Partial samples are never returned as success.

## Frame Data

### Decoded YUV

- Format: 8-bit planar YUV 4:2:0 (`yuv420p`).
- Luma dimensions: `width` by `height`; chroma dimensions are `ceil(width/2)` by `ceil(height/2)` for U and V.
- Each plane has an explicit stride and owned byte storage; padding bytes are not pixel data.
- Frame metadata includes width, height, presentation timestamp, and picture type when known.

### Converted RGB

- Format: packed 8-bit RGB24, channel order R then G then B.
- Pixel origin is top-left; rows are emitted top-to-bottom.
- Stride is exactly `3 * width`; there is no row padding.
- Conversion honors signaled range, matrix, transfer, primaries, and chroma siting for supported inputs. Unsupported or missing required color metadata yields a clear error instead of guessed color output.
- PNG preserves these RGB values losslessly. JPEG uses the documented baseline encoder quality and is validated with pixel-error bounds, not byte equality: at quality 75, smooth-gradient vectors require maximum per-channel error <=24 and mean <=8; the high-frequency stress vector requires maximum <=64 and mean <=20.
- JPEG quality defaults to 75 and accepts explicit values from 1 through 100. Quantization scales the baseline luminance/chrominance tables by `5000 / quality` below 50, or `200 - 2 * quality` otherwise, rounds each table value to nearest, and clips entries to 1 through 255. Its initial RGB conversion uses full-range JFIF YCbCr with 4:4:4 sampling: Q16 coefficients are rounded to nearest with an added 32768 before shifting, and output samples are clamped to 8 bits. Chroma subsampling is not performed in this initial layout.
- JPEG applies an orthonormal 8x8 forward DCT after subtracting 128 from each sample. Quantization divides natural-order coefficients by their nonzero table entries and rounds to nearest with ties away from zero; the resulting coefficients are ordered by the standard 64-position zigzag scan.
- JPEG entropy coding differences each block's DC coefficient from the previous block in that component. Signed amplitudes use the category-width JPEG mapping; AC coefficients use zero-run/category symbols, with `0xF0` for each 16-zero run and `0x00` for trailing zeros. Canonical Huffman codewords are emitted most-significant bit first; byte packing and stuffing are handled by the marker/scan writer.

## Errors and Output Safety

Failures are classified consistently as usage/options, I/O, malformed MP4, unsupported codec/bitstream feature, frame out of range, decode, color-conversion, or encode errors. Error messages include the input path and stage when available.

An out-of-range frame index is an error; it must not silently return the nearest frame or an empty successful image. Unsupported syntax must not yield partially decoded pixels. Write encoded bytes to a temporary file in the destination directory, publish the final path only after encoding and validation succeeds, and remove temporary files on failure.

## Cross-Language Acceptance

- Given the same fixture and frame index, Go, Python, and Rust select the same presentation-order frame and report matching dimensions and timestamps.
- Their RGB24 output must match exactly for PNG-path conversion vectors.
- PNG outputs must independently validate chunk boundaries, CRCs, decompression, dimensions, and RGB pixels.
- JPEG outputs must validate baseline markers, dimensions, entropy scan byte stuffing, and EOI, with decoded pixel error within the agreed tolerance.
- The supported fixture set must extract successfully with FFmpeg absent from `PATH`; excluded files must remain excluded and unmodified.