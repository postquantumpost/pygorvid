# pygorvid 1.0

Native Python video file inspection, H.264 decoding, and PNG/JPEG encoding.
Requires Python 3.9 or newer; no runtime FFmpeg or image library is required.

Install with `python -m pip install pygorvid`, or install the wheel from the
project's release images with `python -m pip install /path/to/pygorvid-1.0.0-py3-none-any.whl`.

```python
from pygorvid import probe_file

info = probe_file("input.mp4")
```

The `vidprobe` command is installed alongside the library:

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