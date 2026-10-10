# Silkroad Active Feature Matrix

## Scope and Method

The initial working corpus contains the 10 MP4 samples at or below 15,000,000 bytes (15 MB decimal). The 12 larger source samples remain untouched and are listed in `silkroad1-streams.json`; they are excluded from the initial implementation and test workload.

Stream metadata and AVC configuration are inventoried by `scripts/inventory_streams.py` using development-time `ffprobe`. Slice/NAL summaries use FFmpeg's `trace_headers` bitstream filter with video stream copy; this does not decode frame pixels. `slice_type_counts` counts slice headers, not pictures, so a multi-slice picture may contribute more than one count.

## Observed Features

| Feature | Observation in active corpus | Initial decoder target |
| --- | --- | --- |
| Container/sample entry | MP4, `avc1`, AVC configuration record (`avcC`) | Support these entries only |
| Video codec/profile | H.264 High Profile; levels 4.2 and 5.2 | Support observed High Profile tools after syntax audit |
| Pixel format | 8-bit `yuv420p` (4:2:0) | Support 8-bit 4:2:0 only |
| Dimensions | 1920x1080 and 3840x2160 | Support both observed dimensions |
| Scan/color signaling | Progressive; limited range, BT.709 matrix/transfer/primaries, left chroma siting | Support observed progressive BT.709 signaling |
| AVC NAL framing | Four-byte length prefixes in all AVC configurations | Support four-byte lengths first |
| Slice types | I, P, and B | Support all three; preserve presentation order |
| NAL unit types | 1 (non-IDR coded slice), 5 (IDR), 6 (SEI), 7 (SPS), 8 (PPS), 9 (AUD) | Parse required types; ignore or safely skip SEI/AUD when not needed |
| Entropy coding | CABAC in every inventoried PPS | CABAC required; CAVLC is not observed |
| Weighted prediction | `weighted_pred_flag=0` and `weighted_bipred_idc=0` in every active PPS | Not needed for the active corpus |
| Reference/reorder metadata | `refs=1`; `has_b_frames=2` in every active video stream | Support observed reference use and up to two B-frame reordering |
| Audio accompanying video | AAC-LC, 48 kHz, stereo in the active samples | Not decoded by frame extraction |

## Explicit Initial Exclusions

The following are not observed in the active corpus and are out of scope for the first decoder release unless a later inventory finds them:

- H.264 profiles other than High.
- Bit depths other than 8-bit and chroma formats other than 4:2:0.
- Interlaced/field-coded video.
- AVC NAL length-prefix sizes other than four bytes.
- CAVLC entropy-coded PPSs.
- SP/SI slice types and coded-slice data partitions (NAL types 2-4).
- NAL unit types other than 1, 5, 6, 7, 8, and 9.

## Still Unassessed

This inventory does not yet parse every SPS/PPS/slice tool. Features such as transform-size flags, scaling matrices, FMO, redundant pictures, unusual POC modes, and gaps in frame numbering remain unsupported until their presence is explicitly checked against the active bitstreams. Unknown syntax must produce a clear unsupported-feature error rather than being assumed absent.

## Reproducibility

- Active input hashes: `silkroad1.sha256`.
- Full stream/configuration inventory and oversized exclusion list: `silkroad1-streams.json`.
- Regenerate the inventory with `python3 scripts/inventory_streams.py`; the script verifies the manifest and filters by the 15,000,000-byte limit before hashing or probing.