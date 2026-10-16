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

Only the implemented progressive, 8-bit 4:2:0 CABAC H.264 subset is supported.
Unsupported streams fail explicitly. Version 1.0 does not imply full H.264 support.

Source and full documentation: https://github.com/postquantumpost/pygorvid
Licensed under the MIT license.