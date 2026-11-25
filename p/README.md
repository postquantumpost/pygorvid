# pygorvid 1.0

Native Python video file inspection, H.264 decoding, and PNG/JPEG encoding.
Requires Python 3.9 or newer; no runtime FFmpeg or image library is required.
NumPy is a runtime dependency for pixel-format conversions. PySDL2 is installed
by default for slideshow support; the SDL2 runtime is installed separately.

Install with `python -m pip install pygorvid`, or install the wheel from the
project's release images with `python -m pip install /path/to/pygorvid-1.0.0-py3-none-any.whl`.

```python
from pygorvid import probe_file

info = probe_file("input.mp4")
```

The `vidprobe` command is installed alongside the library:

```sh
vidprobe --generate grey.mp4
vidprobe --slideshow --input video.mp4
```

`--slideshow` displays each decoded frame in presentation order in an SDL2
window. SDL2 is loaded only for this option. To omit PySDL2, install NumPy
separately and install pygorvid with `pip install --no-deps`.
Frames are paced to their sample durations when decoding is fast enough, but
slow decoding reduces the playback rate. Audio is not played. Closing the
window stops the current slideshow. It cannot be combined with `--generate`,
`--readall`, `--extractframe`, or `--output`.

The `--generate` option natively writes exactly one second of 64x64 H.264 video
(60 fps, High Level 2.0), including the progressive feature frames through
frame 39 and gray padding through frame 60. The milestones cover deblocking,
multiple slices, POC variants, 8x8 transforms and prediction, scaling lists,
transform bypass, VUI, SEI, access-unit delimiters, and stream-end markers.
Frame 2 tests emulation prevention. It includes silent
stereo 48 kHz 16-bit PCM audio (`sowt`, requiring a PCM-capable MP4 player).
Profile-extension milestones 40–48 are generated as standalone `.mp4`
companions, with no audio and independent `avcC` configuration. Frame 49's
Annex SVC/MVC/MVCD/3D-AVC families remain `.264` files. The exact names and
decoder caveats are documented in the root plan.
The initial `H264Encoder` and `Mp4Writer` objects are also exported:

```python
from pathlib import Path
from pygorvid import H264Encoder, Mp4Writer

Path("grey.mp4").write_bytes(Mp4Writer().encode(H264Encoder()))
```

An optional `Mp4Metadata` dataclass supplies UTF-8 string tags:

```python
from pygorvid import Mp4Metadata

metadata = Mp4Metadata(title="My title", artist="My artist", date="2016")
Path("tagged.mp4").write_bytes(Mp4Writer().encode(H264Encoder(), metadata))
```

Empty fields are omitted; `None` (the default) writes no metadata. Tags are
stored in movie-level `mdta` keyed metadata as UTF-8 strings without parsing
numbers, dates or GPS text. Fields are `title`, `artist`, `album`,
`publisher`, `language`, `now_playing`, `album_artist`, `genre`, `track`,
`disc`, `date`, `composer`, `comment`, `description`,
`copyright`, `make`, `model`, `firmware`, `location`, `altitude`, `heading`
and `encoder`. The utility's `--generate` supplies test values for all fields,
including `2016-12-31 23:59:60 UTC` and `11.3733° N 142.5917° E`.

The writer retains those keyed tags and adds conventional iTunes-style
metadata at `moov/udta/meta/ilst` for VLC.
Every non-empty field is also duplicated as a UTF-8 free-form
item in the `com.apple.iTunes` namespace. Numeric track/disc strings from
0 through 65535 (one to five ASCII digits) additionally get binary
`trkn`/`disk` items. VLC displays conventional tags in its standard metadata
view and free-form tags in Extra Metadata.
Microsoft attributes are additionally written in `moov/udta/Xtra` as
NUL-terminated UTF-16LE strings: `Title`, `Author`, `WM/AlbumArtist`,
`WM/Genre`, `WM/TrackNumber`, `WM/PartOfSet`, `WM/OriginalReleaseTime`,
`WM/Composer`, `Description` (comments), `WM/SubTitleDescription`,
`Copyright`, `CameraManufacturer`, `CameraModel` and `WM/EncodedBy`.
A year-prefixed date also supplies `WM/Year`; the full date remains unchanged.
Each field gets an exact `pygorvid_<key>` Xtra copy. Windows only exposes
fields supported by its property handler; arbitrary camera/GPS text may not
appear. Windows UI validation requires Windows; see the root README's
PowerShell check.
Existing files must be regenerated to include these compatibility copies.

Generation takes no input and cannot be combined with extraction, output,
or read-all options. `--prefix j_` writes `j_grey.mp4` instead of `grey.mp4`,
without changing the output directory or extension. The CLI replaces the destination only after a
complete temporary file is written. The encoder and writer currently support
only this fixed format; no external encoder is invoked.

To extract a frame:

```sh
vidprobe --input input.mp4 --extractframe 0 --output frame.png
```

To decode every frame without writing images:

```sh
vidprobe --readall --threads 4 first.mp4 second.mp4
```

The command reports a frame count per input and continues to later files after
a failure, returning status 1 if any input fails. Add `--hash` to print the
SHA-256 of each packed Y+U+V frame without stride padding; the output is
identical across the Python, Go, Rust, and C++ commands. `--hash` requires
`--readall` or `--extractframe`; extraction prints only the selected frame's
digest. `--readall` is mutually
exclusive with `--extractframe` and `--output`. Add `--verbose` to print frame
decode progress; the default output is limited to per-file results. `--threads`
accepts a positive worker count and defaults to the number of available
processors. `--threads_readahead` accepts a non-negative thread budget and
defaults to `floor(--threads * 0.8)`. When readahead is enabled, up to
`--threads_readahead - 1` future closed GOPs are decoded concurrently; zero
disables future GOP workers. The foreground GOP and each future GOP use one
decoder thread, so the full `--threads` value is not multiplied across GOPs.

Only the implemented progressive, 8-bit 4:2:0 CABAC H.264 subset is supported.
Unsupported streams fail explicitly. Version 1.0 does not imply full H.264 support.

Source and full documentation: https://github.com/postquantumpost/pygorvid
Licensed under the MIT license.