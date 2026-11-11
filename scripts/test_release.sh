#!/usr/bin/env bash
set -euo pipefail
shopt -s nullglob

root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
version=$(<"$root/VERSION")
python=${PYTHON:-python3}
cxx=${CXX:-c++}
mkdir -p "$root/tmp"
work=$(mktemp -d "$root/tmp/release-test.XXXXXX")
trap 'rm -rf -- "$work"' EXIT
export TMPDIR="$work"
unset PYTHONPATH
export PYTHONNOUSERSITE=1
cd "$work"

(cd "$root/dist" && sha256sum --check SHA256SUMS)
for artifact in "$root/dist/pygorvid-$version-py3-none-any.whl" "$root/dist/pygorvid-$version.tar.gz"; do
    "$python" -m venv "$work/venv"
    "$work/venv/bin/python" -m pip install --no-deps "$artifact"
    "$work/venv/bin/python" -I -c "from importlib.metadata import version; from pygorvid import BasicInfo, probe_file; assert version('pygorvid') == '$version'; assert callable(probe_file); print(BasicInfo())"
    "$work/venv/bin/vidprobe" --help
    rm -rf "$work/venv"
done

go_images=("$root/dist/pygorvid-go-$version-linux-"*.tar.gz)
rust_images=("$root/dist/pygorvid-rust-$version-"*.tar.gz)
cpp_images=("$root/dist/pygorvid-cpp-$version-linux-"*.tar.gz)
[[ ${#go_images[@]} == 1 && ${#rust_images[@]} == 1 && ${#cpp_images[@]} == 1 ]] || {
    printf 'Expected one Go, Rust, and C++ image for this host in dist/.\n' >&2
    exit 1
}
tar -xzf "${go_images[0]}" -C "$work"
tar -xzf "${rust_images[0]}" -C "$work"
tar -xzf "${cpp_images[0]}" -C "$work"
go_image="$work/$(basename "${go_images[0]}" .tar.gz)"
rust_image="$work/$(basename "${rust_images[0]}" .tar.gz)"
cpp_image="$work/$(basename "${cpp_images[0]}" .tar.gz)"
for executable in "$go_image/bin/vidprobe-go" "$rust_image/bin/vidprobe-rust" "$cpp_image/bin/vidprobe-cpp"; do
    status=0
    "$executable" > "$work/usage.txt" 2>&1 || status=$?
    [[ $status == 2 ]]
    grep -qi usage "$work/usage.txt"
done

mkdir "$work/go-consumer"
cd "$work/go-consumer"
go mod init release-consumer
go mod edit -require="pygorvid@v$version" -replace="pygorvid=$go_image/sdk"
cat > main.go <<'GO'
package main

import (
    "fmt"
    "pygorvid/vid"
)

func main() {
    fmt.Println(vid.BasicInfo{})
}
GO
go mod tidy
GOWORK=off go run .

mkdir -p "$work/rust-consumer/src"
cd "$work/rust-consumer"
cat > Cargo.toml <<TOML
[package]
name = "release-consumer"
version = "0.0.0"
edition = "2021"

[dependencies]
pygorvid = { version = "=$version", path = "$rust_image/sdk" }
TOML
cat > src/main.rs <<'RUST'
fn main() {
    let _ = std::mem::size_of::<pygorvid::BasicInfo>();
    println!("Rust library imported successfully");
}
RUST
CARGO_TARGET_DIR="$work/rust-target" cargo run --quiet

mkdir "$work/cpp-consumer"
cat > "$work/cpp-consumer/main.cpp" <<'CPP'
#include <pygorvid/cavlc.hpp>
#include <pygorvid/h264.hpp>
#include <pygorvid/png.hpp>
#include <pygorvid/video_samples.hpp>
#include <iostream>
#include <stdexcept>

int main() {
    const auto header = pygorvid::parse_nal_header({0x65});
    const std::vector<std::uint8_t> residual_bits{0xa0};
    pygorvid::BitReader reader(residual_bits);
    pygorvid::CavlcResidualBlock block;
    try {
        block = pygorvid::decode_cavlc_residual_block(reader, -1, 4);
    } catch (const std::exception& error) {
        std::cerr << "C++ release consumer CAVLC decode failed: "
                  << error.what() << '\n';
        return 5;
    }
    pygorvid::VideoSampleReader samples;
    pygorvid::Yuv420Frame frame;
    frame.width = 2;
    frame.height = 2;
    frame.luma.assign(4, 16);
    frame.chroma_u.assign(1, 128);
    frame.chroma_v.assign(1, 128);
    const auto png = pygorvid::encode_png(frame);
    bool empty_sample_rejected = false;
    try {
        (void)pygorvid::decode_idr_i_pcm_sample({}, 4, {}, {});
    } catch (const std::runtime_error&) {
        empty_sample_rejected = true;
    }
    const auto opened_missing_file = samples.open("missing-release-test.mp4");
    if (header.unit_type != 5) {
        std::cerr << "C++ release consumer parsed the wrong NAL type\n";
        return 1;
    }
    if (block.coefficients[0] != 1) {
        std::cerr << "C++ release consumer decoded the wrong CAVLC coefficient\n";
        return 2;
    }
    if (png.size() < 8 || png[0] != 0x89 || png[1] != 'P' ||
        png[2] != 'N' || png[3] != 'G') {
        std::cerr << "C++ release consumer encoded an invalid PNG signature\n";
        return 6;
    }
    if (!empty_sample_rejected) {
        std::cerr << "C++ release consumer accepted an empty sample\n";
        return 3;
    }
    if (opened_missing_file || samples.error_info().empty()) {
        std::cerr << "C++ release consumer mishandled a missing MP4 file\n";
        return 4;
    }
    return 0;
}
CPP
"$cxx" -std=c++17 -Wall -Wextra -Wpedantic \
    -I"$cpp_image/sdk/include" "$work/cpp-consumer/main.cpp" \
    "$cpp_image/sdk/lib/libpygorvid.a" -o "$work/cpp-consumer/consumer"
"$work/cpp-consumer/consumer"
printf 'All four language images passed isolated installation checks.\n'