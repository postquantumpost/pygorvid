#!/usr/bin/env bash
set -euo pipefail

root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
version=$(<"$root/VERSION")
language=${1:-all}
python=${PYTHON:-python3}
dist="$root/dist"

case "$language" in
    all|python|go|rust) ;;
    *) printf 'Usage: %s [all|python|go|rust]\n' "$0" >&2; exit 2 ;;
esac
[[ $(uname -s) == Linux ]] || { printf 'Build Linux release images on Linux.\n' >&2; exit 1; }
mkdir -p "$root/tmp" "$dist"
work=$(mktemp -d "$root/tmp/release.XXXXXX")
trap 'rm -rf -- "$work"' EXIT
export TMPDIR="$work"

grep -qx "version = \"$version\"" "$root/p/pyproject.toml"
grep -qx "version = \"$version\"" "$root/r/Cargo.toml"
cmp "$root/LICENSE" "$root/p/LICENSE"

if [[ $language == all || $language == python ]]; then
    mkdir "$work/python"
    cp "$root/p/pyproject.toml" "$root/p/README.md" "$root/p/LICENSE" "$work/python/"
    cp -R "$root/p/src" "$work/python/src"
    find "$work/python" -type d \( -name __pycache__ -o -name '*.egg-info' \) -prune -exec rm -rf {} +
    "$python" -m build --outdir "$work/python-dist" "$work/python"
    cp "$work/python-dist/pygorvid-$version.tar.gz" "$work/python-dist/pygorvid-$version-py3-none-any.whl" "$dist/"
fi

if [[ $language == all || $language == go ]]; then
    name="pygorvid-go-$version-linux-$(go env GOARCH)"
    image="$work/$name"
    mkdir -p "$image/bin" "$image/sdk"
    cp "$root/g/go.mod" "$image/sdk/"
    cp -R "$root/g/vid" "$root/g/cmd" "$image/sdk/"
    find "$image/sdk" -name '*_test.go' -delete
    cp "$root/LICENSE" "$root/VERSION" "$image/"
    cp "$root/docs/INSTALL.md" "$image/README.md"
    (cd "$image/sdk" && CGO_ENABLED=0 go build -trimpath -o "$image/bin/vidprobe-go" ./cmd/vidprobe)
    tar -C "$work" -czf "$work/$name.tar.gz" "$name"
    mv "$work/$name.tar.gz" "$dist/"
fi

if [[ $language == all || $language == rust ]]; then
    target=$(rustc -vV | sed -n 's/^host: //p')
    name="pygorvid-rust-$version-$target"
    image="$work/$name"
    mkdir -p "$image/bin" "$image/sdk"
    cp "$root/r/Cargo.toml" "$root/r/Cargo.lock" "$image/sdk/"
    cp -R "$root/r/src" "$image/sdk/"
    cp "$root/LICENSE" "$root/VERSION" "$image/"
    cp "$root/LICENSE" "$image/sdk/"
    cp "$root/docs/INSTALL.md" "$image/README.md"
    CARGO_TARGET_DIR="$work/rust-target" cargo build --locked --release --manifest-path "$image/sdk/Cargo.toml" --bin vidprobe
    cp "$work/rust-target/release/vidprobe" "$image/bin/vidprobe-rust"
    tar -C "$work" -czf "$work/$name.tar.gz" "$name"
    mv "$work/$name.tar.gz" "$dist/"
fi

(cd "$dist" && sha256sum pygorvid-* > SHA256SUMS)
printf 'Release images: %s\n' "$dist"