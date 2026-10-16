# pygorvid
A video file support library for python, go, and rust, to reduce dependency on ffmpeg.

`vidprobe` reports basic file information for one or more input files. `--input`
adds a file that is processed before any positional input files. To extract a
zero-based video frame as PNG or JPEG, provide one input file and both options:

```text
vidprobe --input input.mp4 --extractframe 12 --output bob.png --prefix j_
```

This writes `j_bob.png`. The prefix is added to the output filename, not its
directory or extension.

`--threads N` sets the number of decoder threads (default: half the available
processors). Output is identical for every thread count. The Python decoder uses
forked worker processes for this, since CPython threads share one interpreter lock.

Go, Python, and Rust frame extraction use the native MP4 reader, H.264 decoder,
RGB conversion, and PNG/JPEG encoders. No runtime `ffmpeg` or external image
library is required. Extraction supports the implemented progressive, 8-bit
4:2:0 CABAC H.264 subset; unsupported streams fail explicitly.

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
