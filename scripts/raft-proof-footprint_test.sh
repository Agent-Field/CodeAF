#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
gate="$script_dir/raft-proof-footprint.sh"

tmp="$(mktemp -d "${TMPDIR:-/tmp}/raft-proof-footprint-test.XXXXXX")"
trap 'rm -rf "$tmp"' EXIT

fail() {
	echo "FAIL: $*" >&2
	exit 1
}

# With no pressure file (macOS, or Linux without PSI) the command runs at once
# and its own exit status comes back.
start=$(date +%s)
CODEAF_RAFT_PROOF_PSI="$tmp/missing" sh "$gate" sh -c 'echo ran > "$1"; exit 3' gate "$tmp/out-missing" && status=0 || status=$?
[ "$status" = 3 ] || fail "expected the command's status 3 with no pressure file, got $status"
[ "$(cat "$tmp/out-missing")" = ran ] || fail "the command did not run with no pressure file"
[ $(( $(date +%s) - start )) -lt 30 ] || fail "a missing pressure file made the gate wait"

# A calm pressure reading lets the command run.
printf 'some avg10=5.00 avg60=4.00 avg300=3.00 total=1\nfull avg10=0.00 avg60=0.00 avg300=0.00 total=0\n' > "$tmp/calm"
CODEAF_RAFT_PROOF_PSI="$tmp/calm" sh "$gate" sh -c 'echo ran > "$1"' gate "$tmp/out-calm" >/dev/null
[ "$(cat "$tmp/out-calm")" = ran ] || fail "a calm reading did not run the command"

# A busy reading still holds the command back, exactly as before.
printf 'some avg10=90.00 avg60=80.00 avg300=70.00 total=1\n' > "$tmp/busy"
if timeout 3 env CODEAF_RAFT_PROOF_PSI="$tmp/busy" sh "$gate" sh -c 'echo ran > "$1"' gate "$tmp/out-busy" >/dev/null; then
	fail "a busy reading did not hold the command back"
fi
[ ! -e "$tmp/out-busy" ] || fail "the command ran while the reading was busy"

echo "raft-proof-footprint: ok"
