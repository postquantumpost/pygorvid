#!/usr/bin/env bash
set -euo pipefail

root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
version=$(<"$root/VERSION")
language=${1:-all}
python=${PYTHON:-python3}
cxx=${CXX:-c++}
dist="$root/dist"

case "$language" in
    all|python|go|rust|cpp) ;;
    *) printf 'Usage: %s [all|python|go|rust|cpp]\n' "$0" >&2; exit 2 ;;
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

if [[ $language == all || $language == cpp ]]; then
    architecture=$(uname -m)
    name="pygorvid-cpp-$version-linux-$architecture"
    image="$work/$name"
    mkdir -p "$image/bin" "$image/sdk/include" "$image/sdk/src" "$image/sdk/lib"
    cp "$root/c/CMakeLists.txt" "$image/sdk/"
    cp -R "$root/c/include/pygorvid" "$image/sdk/include/"
    cp "$root/c/src/pygorvid.cpp" "$root/c/src/h264.cpp" "$root/c/src/h264_pcm.cpp" "$root/c/src/h264_encoder.cpp" "$root/c/src/mp4_writer.cpp" "$root/c/src/cabac.cpp" "$root/c/src/cavlc.cpp" "$root/c/src/jpeg.cpp" "$root/c/src/png.cpp" "$root/c/src/vidprobe.cpp" "$root/c/src/cabac_context_init.hpp" "$image/sdk/src/"
    cp "$root/LICENSE" "$root/VERSION" "$image/"
    cp "$root/docs/INSTALL.md" "$image/README.md"
    "$cxx" -std=c++17 -O2 -Wall -Wextra -Wpedantic \
        -I"$image/sdk/include" -c "$image/sdk/src/pygorvid.cpp" \
        -o "$work/pygorvid.o"
    "$cxx" -std=c++17 -O2 -Wall -Wextra -Wpedantic \
        -I"$image/sdk/include" -c "$image/sdk/src/h264.cpp" \
        -o "$work/h264.o"
    "$cxx" -std=c++17 -O2 -Wall -Wextra -Wpedantic \
        -I"$image/sdk/include" -c "$image/sdk/src/h264_pcm.cpp" \
        -o "$work/h264_pcm.o"
    "$cxx" -std=c++17 -O2 -Wall -Wextra -Wpedantic \
        -I"$image/sdk/include" -c "$image/sdk/src/h264_encoder.cpp" \
        -o "$work/h264_encoder.o"
    "$cxx" -std=c++17 -O2 -Wall -Wextra -Wpedantic \
        -I"$image/sdk/include" -c "$image/sdk/src/mp4_writer.cpp" \
        -o "$work/mp4_writer.o"
    "$cxx" -std=c++17 -O2 -Wall -Wextra -Wpedantic \
        -I"$image/sdk/include" -c "$image/sdk/src/cabac.cpp" \
        -o "$work/cabac.o"
    "$cxx" -std=c++17 -O2 -Wall -Wextra -Wpedantic \
        -I"$image/sdk/include" -c "$image/sdk/src/cavlc.cpp" \
        -o "$work/cavlc.o"
    "$cxx" -std=c++17 -O2 -Wall -Wextra -Wpedantic \
        -I"$image/sdk/include" -c "$image/sdk/src/jpeg.cpp" \
        -o "$work/jpeg.o"
    "$cxx" -std=c++17 -O2 -Wall -Wextra -Wpedantic \
        -I"$image/sdk/include" -c "$image/sdk/src/png.cpp" \
        -o "$work/png.o"
    ar rcs "$image/sdk/lib/libpygorvid.a" "$work/pygorvid.o" "$work/h264.o" "$work/h264_pcm.o" "$work/h264_encoder.o" "$work/mp4_writer.o" "$work/cabac.o" "$work/cavlc.o" "$work/jpeg.o" "$work/png.o"
    "$cxx" -std=c++17 -O2 -Wall -Wextra -Wpedantic \
        -I"$image/sdk/include" "$image/sdk/src/vidprobe.cpp" \
        "$image/sdk/lib/libpygorvid.a" -o "$image/bin/vidprobe-cpp"
    tar -C "$work" -czf "$work/$name.tar.gz" "$name"
    mv "$work/$name.tar.gz" "$dist/"
fi

(cd "$dist" && sha256sum pygorvid-* > SHA256SUMS)
printf 'Release images: %s\n' "$dist"