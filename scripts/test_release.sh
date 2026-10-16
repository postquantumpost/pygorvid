#!/usr/bin/env bash
set -euo pipefail
shopt -s nullglob

root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
version=$(<"$root/VERSION")
python=${PYTHON:-python3}
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
[[ ${#go_images[@]} == 1 && ${#rust_images[@]} == 1 ]] || {
    printf 'Expected one Go and one Rust image for this host in dist/.\n' >&2
    exit 1
}
tar -xzf "${go_images[0]}" -C "$work"
tar -xzf "${rust_images[0]}" -C "$work"
go_image="$work/$(basename "${go_images[0]}" .tar.gz)"
rust_image="$work/$(basename "${rust_images[0]}" .tar.gz)"
for executable in "$go_image/bin/vidprobe-go" "$rust_image/bin/vidprobe-rust"; do
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
printf 'All three language images passed isolated installation checks.\n'