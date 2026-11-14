# Installing pygorvid 1.0

Each language can be installed independently. No FFmpeg installation is needed.
The release images contain only the selected implementation, not sample videos,
tests, build caches, or the other language implementations.

## Python

On Linux Mint, install Python's venv support with `sudo apt install python3-venv`.
Use a virtual environment rather than modifying the distribution's Python:

```sh
python3 -m venv ~/.venvs/pygorvid
~/.venvs/pygorvid/bin/python -m pip install /path/to/pygorvid-1.0.0-py3-none-any.whl
~/.venvs/pygorvid/bin/vidprobe --help
```

Other Python projects can install the same wheel into their own virtual
environment and `from pygorvid import probe_file`. Python 3.9 or newer is required.
The wheel is pure Python and is not restricted to one CPU architecture.
The source distribution is also pip-installable; building it requires setuptools.
After the maintainer publishes version 1.0.0 to PyPI, the wheel path can be replaced
with `pygorvid==1.0.0`. Building release images does not publish them.

## Go

Extract `pygorvid-go-1.0.0-linux-ARCH.tar.gz` to a permanent location, for example
`$HOME/.local/lib/pygorvid-go-1.0.0`. The archive has a single top-level directory;
its `sdk` directory is a complete Go module. In a consuming Go project:

```sh
go mod edit -require=pygorvid@v1.0.0
go mod edit -replace=pygorvid=/absolute/path/to/extracted-image/sdk
go mod tidy
```

Import `vid "pygorvid/vid"`. Go 1.22 or newer is required to compile projects
using the library. The module name is intentionally unchanged; these local SDK
images use `replace`, not a public `go get` module path.
No Go compiler is needed to run the prebuilt, statically linked `bin/vidprobe-go`.

## Rust

Extract `pygorvid-rust-1.0.0-TARGET.tar.gz` to a permanent location. Add this to
the consuming project's `Cargo.toml`, using the extracted SDK's absolute path:

```toml
[dependencies]
pygorvid = { version = "1.0.0", path = "/absolute/path/to/extracted-image/sdk" }
```

The crate's public API is available as `pygorvid`, for example
`pygorvid::BasicInfo`. A current stable Rust toolchain is recommended (the
included lockfile uses format 4). Cargo downloads the Rust dependencies on first
build; this is not an offline dependency bundle. Rust libraries are distributed
as source because compiled Rust libraries do not have a stable cross-toolchain
ABI. The prebuilt `bin/vidprobe-rust` does not need a Rust toolchain.

## C++

Extract `pygorvid-cpp-1.0.0-linux-ARCH.tar.gz`. The SDK includes
`include/pygorvid/pygorvid.hpp`, C++ source, and `lib/libpygorvid.a`. Compile a
consumer with C++17 and link the static library:

```sh
c++ -std=c++17 -I/path/to/extracted-image/sdk/include main.cpp \
  /path/to/extracted-image/sdk/lib/libpygorvid.a -o my-app
```

The C++ SDK provides container detection, MP4 sample reading
(`video_samples.hpp`), H.264 parameter-set and slice parsing, CAVLC residual
decoding, and PNG (`png.hpp`) and JPEG (`jpeg.hpp`) encoding. `decode_idr_i_pcm_sample()` can
reconstruct progressive 8-bit 4:2:0 IDR pictures when every macroblock is
I_PCM. `decode_idr_i_sample()` also supports I16x16 macroblocks with luma and
chroma DC/AC CAVLC residual reconstruction and I4x4 macroblocks with all nine
prediction modes and luma plus 4:2:0 chroma DC/AC CAVLC residuals using default scaling lists. `decode_p_frame()`
supports complete P pictures containing P_SKIP and P_L0_16x16 macroblocks,
with one reference fixed to the supplied previous frame. It derives motion
vector predictors from neighboring macroblocks and supports quarter-pel luma
motion with bilinear fractional 4:2:0 chroma motion. These reconstruction paths
also reconstruct CAVLC luma and chroma DC/AC residuals for P_L0_16x16
macroblocks using default scaling lists. IDR deblocking is supported for
decoded intra macroblocks; P-picture reconstruction requires deblocking to be
disabled. I8x8 prediction, weighted/reordered references,
adaptive reference marking, custom scaling lists, and CABAC are not supported.
Sample DTS, PTS, and durations are in `VideoSampleReader::timescale()`.

The `vidprobe-cpp` command extracts one frame by MP4 sample index. An output
ending in `.jpg` (case-insensitive) selects JPEG; other names produce PNG:

```sh
vidprobe-cpp --extractframe 0 --output frame.png input.mp4
vidprobe-cpp --extractframe 0 --output frame.jpg input.mp4
```

It decodes earlier supported samples to establish the reference frame. Only
IDR pictures followed by supported P pictures with P-picture deblocking
disabled can currently be extracted; unsupported slices fail with an error.
PNG conversion uses limited-range BT.601 coefficients. Metadata probing remains
available for one or more files.

## Command Installation

Native commands have distinct filenames so multiple versions can be installed:

```sh
mkdir -p ~/.local/bin
install -m 755 /path/to/go-image/bin/vidprobe-go ~/.local/bin/
install -m 755 /path/to/rust-image/bin/vidprobe-rust ~/.local/bin/
install -m 755 /path/to/cpp-image/bin/vidprobe-cpp ~/.local/bin/
```

Ensure `~/.local/bin` is on `PATH`. Python's `vidprobe` remains in its virtual
environment. Running a native command without arguments prints usage and exits
with status 2; Python's command also supports `--help` with a successful exit.

All four installed commands accept `--generate grey.mp4` without any input
file. This writes one second of 64x64 grey H.264 video at 25 fps with silent
stereo 48 kHz, 16-bit PCM audio. The initial encoder uses Constrained Baseline
Level 2.0 I_PCM IDR pictures; the MP4 writer uses the QuickTime `sowt` audio
sample entry, so playback requires PCM-in-MP4 support. Generation is native
and does not require ffmpeg. It cannot be combined with input, extraction,
output, prefix or read-all options. The SDKs expose separate `H264Encoder`
and `Mp4Writer` objects for this initial fixed format.

## Building Release Images

From the repository root, with GNU Make, tar, Go 1.22+, a current stable Rust
toolchain, a C++17 compiler and `ar`, and Python 3.9+ installed:

```sh
python3 -m venv tmp/release-tools
tmp/release-tools/bin/python -m pip install build
make release PYTHON="$PWD/tmp/release-tools/bin/python"
make test-release PYTHON="$PWD/tmp/release-tools/bin/python"
```

`make release-python`, `make release-go`, `make release-rust`, and
`make release-cpp` build only the chosen language and require only its toolchain
(plus Python's `build` package for Python). `CXX` selects the C++ compiler.
Artifacts and SHA-256 checksums are written to `dist/`. Temporary build
directories are created under the repository's `tmp/` and removed on exit.
The smoke test installs both Python distributions and builds independent Go,
Rust, and C++ consumer projects using only the extracted images.
To run C++ tests from a source checkout, configure with
`-DPYGORVID_BUILD_TESTS=ON`.

Build on the oldest supported Linux Mint release, or a compatible older Linux
userspace, and on each desired CPU architecture. Rust's dynamically linked
executable requires a glibc no newer than the target system provides; building
on a newer distribution does not guarantee compatibility with older Mint.
Go's executable is built without cgo. The automated release build uses Ubuntu
22.04 as a glibc 2.35 baseline for Linux Mint 21 and newer on x86-64.

To publish Python to PyPI, first run the release smoke test, then use `twine`
from a maintainer environment:

```sh
python -m twine check dist/*.whl dist/pygorvid-1.0.0.tar.gz
python -m twine upload dist/*.whl dist/pygorvid-1.0.0.tar.gz
```

Publishing requires control of the PyPI project and authentication. No PyPI
upload, Git tag, or GitHub release is performed by the local build.