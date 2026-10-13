#!/usr/bin/env bash
set -uo pipefail

TIME_LIMIT_SECONDS=""

usage() {
	printf 'Usage: %s [--time-limit SECONDS]\n' "${0##*/}"
}

while (($#)); do
	case "$1" in
		--time-limit)
			if (($# < 2)); then
				usage >&2
				exit 2
			fi
			TIME_LIMIT_SECONDS="$2"
			shift 2
			;;
		--time-limit=*)
			TIME_LIMIT_SECONDS="${1#*=}"
			shift
			;;
		-h|--help)
			usage
			exit 0
			;;
		*)
			printf 'Unknown option: %s\n' "$1" >&2
			usage >&2
			exit 2
			;;
	esac
done

if [[ -n "$TIME_LIMIT_SECONDS" ]] && ! [[ "$TIME_LIMIT_SECONDS" =~ ^[1-9][0-9]*$ ]]; then
	printf 'Time limit must be a positive integer number of seconds.\n' >&2
	usage >&2
	exit 2
fi

ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
RUNS_DIR="$ROOT/tmp/test-runs"
mkdir -p "$RUNS_DIR"
RUN_DIR="$(mktemp -d "$RUNS_DIR/python-p-frame-$(date -u +%Y%m%dT%H%M%SZ)-XXXXXX")"
LOG="$RUN_DIR/output.log"
STATUS="$RUN_DIR/status.txt"

printf 'RUNNING pid=%s started_utc=%s\n' "$$" "$(date -u +%FT%TZ)" > "$STATUS"
printf 'Run directory: %s\nLog: %s\nStatus: %s\n' "$RUN_DIR" "$LOG" "$STATUS"

mark_interrupted() {
	printf 'INTERRUPTED pid=%s time_utc=%s\n' "$$" "$(date -u +%FT%TZ)" > "$STATUS"
	exit 130
}
trap mark_interrupted HUP INT TERM

{
	printf 'Run started_utc=%s\n' "$(date -u +%FT%TZ)"
	printf '\n== Ordered cross-language smoke suite ==\n'
	bash "$ROOT/scripts/test_decoder_smoke.sh"
	smoke_status=$?
	if (( smoke_status != 0 )); then
		printf 'Smoke suite failed with exit code %s; full Python test was not started.\n' "$smoke_status"
		exit "$smoke_status"
	fi

	printf '\n== Full Python P-frame plane test ==\n'
	PYGORVID_DEBUG_PROGRESS=1 \
	PYGORVID_TEST_TIME_LIMIT="$TIME_LIMIT_SECONDS" \
	PYTHONPATH="$ROOT/p/src${PYTHONPATH:+:$PYTHONPATH}" \
		python3 -m pytest "$ROOT/p/tests/test_decoder.py" -s -q \
		-k test_h264_decoder_reconstructs_filtered_p_picture_planes
	exit $?
} 2>&1 | tee "$LOG"
pipeline_status=("${PIPESTATUS[@]}")
test_status=${pipeline_status[0]}
tee_status=${pipeline_status[1]}
if (( test_status == 0 && tee_status != 0 )); then
	test_status=$tee_status
fi

if (( test_status == 0 )); then
	result=PASS
else
	result=FAIL
fi
printf '%s exit_code=%s finished_utc=%s\n' \
	"$result" "$test_status" "$(date -u +%FT%TZ)" > "$STATUS"
printf '\n%s (exit %s)\nLog: %s\nStatus: %s\n' \
	"$result" "$test_status" "$LOG" "$STATUS"
printf '\nCopy this evaluation prompt into a new chat:\n\n'
printf 'Evaluate the Python P-frame run in %s. Read %s first, then review %s. Do not rerun the test. Summarize whether it passed; if it failed, diagnose the error and recommend the next smallest check.\n' \
	"$RUN_DIR" "$STATUS" "$LOG"
exit "$test_status"