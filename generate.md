# `--generate` H.264 Feature Plan

This document is both the frame-by-frame plan for `--generate` and the
documentation of the test stream it produces. The goal is a valid, playable
video whose frames introduce encoder features in a deliberate dependency and
complexity order. The same planned frames and pixel patterns must be produced
by the Python, Go, Rust, and C++ implementations.

## Part 1: Frame-to-Feature Association

Frame numbers below are one-based presentation-order numbers. Each row gives
the first frame that introduces a feature; subsequent frames may keep using
that feature. A frame should vary its picture content so that the new coding
path is actually required, while reusing only features introduced in earlier
rows. When possible, introduce one new feature family per frame. A failure at
frame *n* then points first to that row's feature, but is not by itself proof
that the feature is at fault: earlier reference pictures, parameter sets,
container timing, and decoder state remain possible causes.

| Frame | First feature exercised | Planned stimulus and isolation |
| ---: | --- | --- |
| 1 | Stream bootstrap; IDR I-slice with I_PCM macroblocks | Keep the existing simple flat 64x64 8-bit 4:2:0 picture. Exercise SPS/PPS activation, one access unit and slice, IDR reset, NAL/RBSP framing, PCM alignment, and trailing bits. This is the known-good anchor, not a claim that all these prerequisites can be diagnosed independently. |
| 2 | Emulation-prevention insertion and removal | Use another I_PCM IDR picture with its first three luma samples set to `00 00 01`; keep every other luma/chroma sample at 128 and all slice syntax unchanged. Its EBSP must contain `00 00 03 01`, and decoding must restore the exact `00 00 01` samples. |
| 3 | Non-IDR I picture and reference-picture continuity | Encode I_PCM without resetting the decoded-picture buffer. Advance `frame_num` and POC, and mark the picture as a reference. Keep pixels flat so only picture/reference state changes. |
| 4 | Intra_16x16 DC prediction | Use I_16x16 with zero residual and a flat but different luma level; exercise the DC path and unavailable top/left neighbors. |
| 5 | Intra_16x16 directional and chroma prediction | Use vertical, horizontal, and plane luma modes, plus the 4:2:0 chroma DC/horizontal/vertical/plane modes. Place patterns that distinguish each mode. |
| 6 | Intra_4x4 DC prediction and mode syntax | Use I_4x4 blocks with DC prediction and exercise predicted-mode signaling, including the `prev_intra4x4_pred_mode_flag` path. |
| 7 | Intra_4x4 directional prediction and neighbor-derived modes | Exercise the directional modes and top/left/top-right reference availability, including blocks whose mode is derived from already coded neighboring blocks. |
| 8 | First nonzero 4x4 residual and minimal CAVLC path | Add a sparse residual to an intra-predicted picture. Cover coded-block-pattern selection, QP and QP delta, coefficient scan, inverse scaling/transform, and reconstruction without changing the prediction family. Use only the simplest CAVLC coefficient pattern here. |
| 9 | Chroma residual reconstruction | Add 4:2:0 chroma DC and AC residuals, including chroma QP derivation and the chroma DC transform. Keep luma content fixed from the preceding residual test. |
| 10 | Expanded CAVLC coefficient-code paths | Exercise trailing ones, positive and negative levels, total-zero and run-before paths, neighboring `nC` contexts, and a dense block. Preserve the same prediction and residual shapes as earlier frames. |
| 11 | First P slice: P_L0_16x16, zero motion and zero residual | Reference the preceding decoded picture. Use a spatially shifted but exactly representable pattern so the reference-list and inter-prediction path is used without fractional interpolation. |
| 12 | P-skip and skip-run derivation | Include skipped macroblocks and a non-skipped neighbor so inferred reference index and motion vector are distinguishable from an explicit P macroblock. |
| 13 | Motion-vector differences and spatial motion-vector prediction | Exercise zero and nonzero integer-pel vectors, including left/top/upper-right predictor availability and nonzero `mvd_l0`. |
| 14 | P macroblock partitions 16x8 and 8x16 | Use independent motion/content regions to require both partition orientations while retaining integer-pel motion. |
| 15 | P 8x8 and sub-macroblock partitions | Exercise 8x8, 8x4, 4x8, and 4x4 subpartitions and their motion-vector prediction. |
| 16 | Fractional-sample motion compensation | Require quarter-pel luma interpolation and fractional 4:2:0 chroma interpolation. Use distinct edge patterns to expose rounding and filter-tap errors. |
| 17 | Multiple active references and reference indices | Keep at least two distinguishable reference pictures available and select each by `ref_idx_l0`; test list construction and decoded-reference marking. |
| 18 | Reference-list modification and adaptive marking | Reorder the active reference list and signal reference-picture marking operations. Make the selected reference's identity observable in the reconstructed picture. |
| 19 | Explicit weighted P prediction | Signal non-default luma and chroma weights/offsets and verify their effect before residual addition. |
| 20 | B slices and reordered output | Introduce B pictures using L0, L1, and bi-prediction with a simple reference arrangement. Record decode order separately from presentation order. |
| 21 | Spatial direct B prediction | Exercise B_Direct_16x16 and B_Direct_8x8 with spatially derived motion and available neighboring partitions. |
| 22 | Temporal direct B prediction | Exercise temporal direct derivation from a collocated reference picture and nontrivial POC distances. |
| 23 | B macroblock/partition choices | Exercise L0, L1, and bi-predicted partitions at 16x16, 16x8, 8x16, and 8x8 scales, with motion differences. |
| 24 | Explicit weighted B prediction | Exercise explicit weights for L0, L1, and bi-prediction on both luma and chroma. |
| 25 | Implicit weighted bi-prediction | Exercise POC-derived implicit weights with non-equal temporal distances; retain a control region with equal weights. |
| 26 | Inter residual syntax and reconstruction | Add sparse then dense residual blocks to P macroblocks, keeping the already established CAVLC path and motion field stable. |
| 27 | CABAC I-slice syntax | Enable CABAC at the PPS and exercise I-slice macroblock type, prediction, coded-block-pattern, and termination paths. |
| 28 | CABAC P-slice syntax | Add P-slice skip, partition, reference-index, motion-difference, and residual contexts, isolating each context family with controlled macroblocks. |
| 29 | CABAC B-slice and direct-mode syntax | Exercise B-slice macroblock/sub-macroblock types, direct/skip contexts, reference indices, and residual coding. |
| 30 | CABAC residual coefficient coding | Exercise significance/last-significance flags, coefficient levels, sign and bypass bins, including block-end and dense-coefficient cases. |
| 31 | In-loop deblocking strengths | Create controlled intra/inter edges that produce boundary strengths 0 through 4; use pixel comparisons on both sides of each edge. |
| 32 | Deblocking controls and slice boundaries | Exercise `disable_deblocking_filter_idc` and alpha/beta offsets, plus filtering permitted and prohibited across slice boundaries. |
| 33 | Multiple slices in one picture | Use multiple raster-ordered slices with independent slice headers and QP state. Keep slice groups disabled; verify picture assembly and edge-filter behavior. |
| 34 | POC types 1 and 2; wrap and gap handling | Use separate short sequences with valid SPS/IDR setup for each POC type, then exercise `frame_num` wrap and a frame-number gap permitted by the SPS. |
| 35 | 8x8 transform and transform-size selection | In a High-profile sequence, signal `transform_8x8_mode_flag` and `transform_size_8x8_flag`; exercise sparse and dense 8x8 residuals. |
| 36 | Intra_8x8 prediction | Exercise the intra 8x8 mode set, mode derivation, and filtered reference samples with directional patterns. |
| 37 | Scaling matrices | Exercise signaled non-default 4x4 and 8x8 scaling lists, including fallback/default-list behavior, with coefficient patterns that distinguish the matrices. |
| 38 | Transform bypass | In a profile/sequence that permits it, exercise lossless transform-bypass residual coding with values that distinguish bypass from ordinary quantized reconstruction. |
| 39 | VUI, SEI, access-unit delimiters, and sequence end markers | Add valid timing/color/display signaling and representative SEI/AUD/end-of-sequence/end-of-stream NAL units. Keep coded picture content unchanged. |
| 40 | 4:2:2 chroma format | Start a new IDR sequence with profile/parameter sets valid for 4:2:2; use independent chroma patterns and dimensions aligned to macroblocks. |
| 41 | 4:4:4 chroma format and separate colour planes | Start a new IDR sequence using the permitted profile and SPS syntax. Exercise chroma-array and separate-colour-plane paths separately. |
| 42 | High bit depth | Start profile-valid 10-bit, then higher supported bit-depth sequences; use values around rounding, clipping, and transform boundaries. |
| 43 | Field pictures and field-coded macroblocks | Start an interlaced-capable sequence and distinguish top/bottom field order, field references, and field scan behavior. |
| 44 | MBAFF | Exercise frame/field macroblock-pair decisions and neighboring-location derivation in an MBAFF sequence. |
| 45 | Flexible macroblock ordering / slice groups | Exercise the supported slice-group map types one at a time, beginning with the simplest non-raster map. |
| 46 | Arbitrary slice order and redundant pictures | Exercise ordering and redundant-picture-count signaling only in a profile/stream configuration that permits them. |
| 47 | Data-partitioned slices | Exercise partition A/B/C association and missing/invalid partition handling in a compatible stream. |
| 48 | SP and SI slices | Exercise switching and SI prediction/residual paths in an Extended-profile-compatible sequence. |
| 49 | Scalable, multiview, depth, and 3D extensions | Treat SVC, MVC/MVCD, and 3D-AVC as distinct extension families with their own NAL, parameter-set, and view/depth dependencies; do not combine them into one undifferentiated test frame. |

Frames 1–39 are the proposed progressive 8-bit 4:2:0 core sequence. Frames
40–49 require profile, chroma, bit-depth, scan, or extension capabilities
beyond that core. Their frame numbers are reserved for planning; they are not
permission to put incompatible parameter sets into the core stream. See the
Appendix for the profile-transition and broad-playability gate.

## Part 2: Appendix

### A. Scope and stream envelope

The current `--generate` output is documented in the root [README](./README.md):
60 64x64 frames at 60 fps, with 48 kHz stereo silence, in an MP4. Frame 1 is
the flat-gray I_PCM anchor. Frame 2 has luma samples `00 00 01` at the
upper-left and all its other samples remain 128 to force emulation prevention
without changing the coding path. Frame 3 is a reference non-IDR I_PCM picture with `frame_num=1`. Frame 4 is a
reference non-IDR CAVLC I_16x16 DC picture with `frame_num=2` and zero
residual; its flat 8-bit luma prediction is 128 because the first macroblock
has no available intra reference. Frame 5 uses CAVLC I_16x16 with all
vertical, horizontal, DC, and plane luma modes plus all four 4:2:0 chroma
modes. Compact luma and chroma DC residuals seed observable reference edges.
Frame 6 is a reference non-IDR Intra_4x4 DC picture: every macroblock signals
all sixteen `prev_intra4x4_pred_mode_flag` bits and zero coded-block pattern.
Frame 7 is a reference non-IDR Intra_4x4 directional picture. Its first
macroblock uses I_PCM to seed non-flat references; the remaining macroblocks
exercise all directional modes, top-left/top-right reference paths, and both
predicted and explicitly signaled neighbor-derived modes.
Frame 8 is a reference non-IDR Intra_4x4 directional picture with one sparse
4x4 luma residual in the final macroblock. Its CAVLC data uses a luma-only
coded-block pattern, `mb_qp_delta=1`, one positive trailing-one coefficient,
and zero-coefficient tokens for the other blocks in that 8x8 quadrant.
Frame 9 preserves frame 8's luma content and adds CAVLC residuals to both 4:2:0
chroma planes in the final macroblock. Each plane uses a one-coefficient 2x2
DC block and a sparse 4x4 AC block, exercising chroma QP derivation, the chroma
DC inverse transform, AC scan reconstruction, and positive and negative
coefficient signs while leaving all other macroblocks unchanged.
Gray IDR padding follows through frame 60; the padded duration is at least one
second. All current pictures are Constrained Baseline I_PCM, I_16x16, or
Intra_4x4. The plan changes the
eventual core video sequence to Progressive High, 8-bit 4:2:0, progressive,
so CAVLC, CABAC, B pictures, and 8x8 transform tools can be introduced
without changing profile halfway through the core sequence. Frame 1 remains
an IDR/I_PCM anchor. The required Progressive High constraints are in H.264
Annex A, clauses A.2.4 and A.2.4.1.

The generated duration may grow as feature frames are added; keep the existing
dimensions, 60 fps frame rate, silent audio track, and MP4 behavior unless a
later plan revision explicitly changes them. Keep at least 60 frames by padding
with gray pictures after the implemented feature frames. Frame numbering in this table is the
video presentation order. B pictures make coded/decode order different, so
test output must report the planned frame ID, zero-based presentation index,
and decode-order index where available. Feature-target pictures should have
small, deterministic, visually distinct regions so pixel comparisons can
identify which prediction or reconstruction path was exercised.

Profile-specific rows 40–49 need valid SPS/PPS and NAL signaling for each
sequence. Before implementing those rows, verify whether changing profile or
chroma/bit-depth/scan constraints at IDR boundaries remains playable in the
single generated MP4 across the project's target players. If not, keep the
core `--generate` file broadly playable and emit separate companion test
files for those profile families; never make the core output malformed or
silently decoder-dependent just to keep one filename.

### B. Feature isolation and ordering rules

1. Make frame 1 the smallest reproducible, independently decodable anchor.
2. Introduce a feature only after its syntax, prediction, reference, and
   reconstruction prerequisites have appeared earlier in the table.
3. Keep all earlier features valid and deterministic. A new frame may reuse
   prior coding tools, but should not introduce a second unlisted feature.
4. Prefer a picture pattern whose reconstructed samples differ if the target
   feature is omitted or implemented incorrectly.
5. Add combination/interaction frames after individual first-use frames; do
   not use a combination frame as the only test of a feature.
6. Preserve valid access-unit ordering, reference-picture state, parameter-set
   activation, level constraints, and MP4 sample timing. A useful decoder
   failure location is not worth an invalid or unplayable stream.

### C. Cross-language implementation and acceptance notes

- Implement the same feature milestone and deterministic input pattern in
  Python, Go, Rust, and C++; keep frame IDs and output behavior aligned.
- Preserve the existing `--generate` argument, output safety, and MP4/audio
  behavior while replacing the current repeated-grey video in planned stages.
- For each stage, independently parse the emitted NAL/slice syntax, decode all
  generated frames with the project's decoders, and compare reconstructed
  YUV against a trusted reference decoder. Keep external codec tools as
  development/test oracles only; generation must not depend on them.
- Verify that the MP4 remains playable, that reference and presentation
  ordering is correct, and that failures include the planned frame ID.
- Run the same corpus through all four implementations and retain byte parity
  where the output format permits deterministic parity.

### D. H.264 reference

The primary source is the local ITU-T Recommendation H.264, Version 16
(06/2026): [PDF](./docs/T-REC-H.264-202606-I!!PDF-E.pdf) and
[searchable text](./docs/T-REC-H.264-202606-I!!PDF-E.txt).

| Feature family | Main clauses |
| --- | --- |
| NAL, SPS/PPS, slice and macroblock syntax/semantics | 7.3–7.4 |
| POC, reference-list construction/modification and reference marking | 8.2.1, 8.2.4–8.2.5 |
| Intra prediction and I_PCM | 8.3 |
| Motion-vector derivation, inter prediction and weights | 8.4 |
| Residual transforms, scaling and reconstruction | 8.5 |
| Deblocking | 8.7 |
| Exp-Golomb, CAVLC and CABAC | 9.1–9.3 |
| Profiles and levels | Annex A |
| Scalable, multiview, and 3D extension families | Annexes F–I |

Clause numbering refers to this cited edition; check for renumbering if the
source edition changes.
