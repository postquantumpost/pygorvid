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

Frame extraction requires the `ffmpeg` executable on `PATH`.
