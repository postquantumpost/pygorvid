# CABAC intra regression fixtures

These videos were generated from FFmpeg's `testsrc2` source at 128x96 and
5 fps with libx264's `veryslow` preset, `keyint=60`, `scenecut=0`, `weightp=0`,
and `ref=1`. The P fixture has five frames, `bframes=0`, and `no-deblock=1`.
It contains P-slice Intra_4x4 and Intra_8x8 macroblocks alongside inter and
skip macroblocks. The spatial B fixture has eight frames, `bframes=2`,
`weightb=0`, `direct=spatial`, and `no-deblock=1`; it includes a B-slice
Intra_4x4 macroblock. Their decoded RGB frames were checked against the Go
decoder before recording the PNG hashes in the CMake test.

The temporal B fixture has eight frames, `bframes=2` and `weightb=0`, with
default direct prediction and deblocking. It verifies that the C++ decoder
rejects temporal-direct prediction instead of silently using spatial direct.
