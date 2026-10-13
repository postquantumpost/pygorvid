#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"

for size in 16 64; do
	if [[ "$size" == 16 ]]; then
		rust_test=a_decoder_smoke_16x16
		go_test=TestADecoderSmoke16x16
		python_test=test_a_decoder_smoke_16x16
	else
		rust_test=b_decoder_smoke_64x64
		go_test=TestBDecoderSmoke64x64
		python_test=test_b_decoder_smoke_64x64
	fi

	printf '\n== %sx%s: Rust ==\n' "$size" "$size"
	cargo test --manifest-path "$ROOT/r/Cargo.toml" --test decoder "$rust_test" -- --exact

	printf '\n== %sx%s: Go ==\n' "$size" "$size"
	go test -C "$ROOT/g" ./vid -run "^${go_test}$" -count=1

	printf '\n== %sx%s: Python ==\n' "$size" "$size"
	PYTHONPATH="$ROOT/p/src${PYTHONPATH:+:$PYTHONPATH}" \
		python3 -m pytest "$ROOT/p/tests/test_decoder.py" -q -k "$python_test"
done