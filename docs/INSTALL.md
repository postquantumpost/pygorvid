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

## Command Installation

The Go and Rust commands have distinct filenames so both can be installed:

```sh
mkdir -p ~/.local/bin
install -m 755 /path/to/go-image/bin/vidprobe-go ~/.local/bin/
install -m 755 /path/to/rust-image/bin/vidprobe-rust ~/.local/bin/
```

Ensure `~/.local/bin` is on `PATH`. Python's `vidprobe` remains in its virtual
environment. Running a native command without arguments prints usage and exits
with status 2; Python's command also supports `--help` with a successful exit.

## Building Release Images

From the repository root, with GNU Make, tar, Go 1.22+, a current stable Rust
toolchain, and Python 3.9+ installed:

```sh
python3 -m venv tmp/release-tools
tmp/release-tools/bin/python -m pip install build
make release PYTHON="$PWD/tmp/release-tools/bin/python"
make test-release PYTHON="$PWD/tmp/release-tools/bin/python"
```

`make release-python`, `make release-go`, and `make release-rust` build only the
chosen language and require only its toolchain (plus Python's `build` package
for Python). Artifacts and SHA-256 checksums are written to `dist/`. Temporary
build directories are created under the repository's `tmp/` and removed on exit.
The smoke test installs both Python distributions and builds independent Go and
Rust consumer projects using only the extracted images.

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