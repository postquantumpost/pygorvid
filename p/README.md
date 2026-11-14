# pygorvid 1.0

Native Python video file inspection, H.264 decoding, and PNG/JPEG encoding.
Requires Python 3.9 or newer; no runtime FFmpeg or image library is required.
NumPy is a runtime dependency for pixel-format conversions.

Install with `python -m pip install pygorvid`, or install the wheel from the
project's release images with `python -m pip install /path/to/pygorvid-1.0.0-py3-none-any.whl`.

```python
from pygorvid import probe_file

info = probe_file("input.mp4")
```

The `vidprobe` command is installed alongside the library:

```sh
vidprobe --generate grey.mp4
```

This natively writes exactly one second of 64x64 grey H.264 video (25 fps,
Constrained Baseline Level 2.0 I_PCM IDR pictures), with silent stereo 48 kHz
16-bit PCM audio (`sowt`, requiring a PCM-capable MP4 player).
The initial `H264Encoder` and `Mp4Writer` objects are also exported:

```python
from pathlib import Path
from pygorvid import H264Encoder, Mp4Writer

Path("grey.mp4").write_bytes(Mp4Writer().encode(H264Encoder()))
```

Generation takes no input and cannot be combined with extraction, output,
prefix or read-all options. The CLI replaces the destination only after a
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
a failure, returning status 1 if any input fails. `--readall` is mutually
exclusive with `--extractframe` and `--output`. Add `--verbose` to print frame
decode progress; the default output is limited to per-file results.

Only the implemented progressive, 8-bit 4:2:0 CABAC H.264 subset is supported.
Unsupported streams fail explicitly. Version 1.0 does not imply full H.264 support.

Source and full documentation: https://github.com/postquantumpost/pygorvid
Licensed under the MIT license.