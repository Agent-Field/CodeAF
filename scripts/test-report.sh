#!/usr/bin/env bash
# Run an existing test command through Go's JSON protocol without replacing its
# exit status. The report parser has its own strict failure: empty or malformed
# output is never presented as a successful measurement.
set -uo pipefail

interval="${TEST_REPORT_HEARTBEAT:-30}"

if [ "$#" -lt 2 ]; then
	echo 'usage: scripts/test-report.sh REPORT COMMAND [ARG ...]' >&2
	exit 2
fi
report="$1"
shift
# A failed or cancelled run must not leave an older report at the requested
# path looking like evidence for this invocation.
rm -f -- "$report"

started=$SECONDS
parent=$$
heartbeat() {
	while sleep "$interval"; do
		# The EXIT trap covers every ordinary ending but not SIGKILL. An orphan
		# reporting on a run nobody is watching is worse than a missing heartbeat (#735).
		kill -0 "$parent" 2>/dev/null || exit 0
		printf 'test-report: still running (%ss)\n' "$((SECONDS - started))" >&2
	done
}
heartbeat &
heartbeat_pid=$!
cleanup() {
	kill "$heartbeat_pid" 2>/dev/null || true
	wait "$heartbeat_pid" 2>/dev/null || true
}
trap cleanup EXIT

"$@" | go run ./internal/ci/cmd/testreport -out "$report"
statuses=("${PIPESTATUS[@]}")
if [ "${statuses[0]}" -ne 0 ]; then exit "${statuses[0]}"; fi
exit "${statuses[1]}"
