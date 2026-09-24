#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
fixture="$root/scripts/testdata/shardfixture"
runner="$root/scripts/shard-test.sh"
tmp="$(mktemp -d /tmp/codeaf-shard-test-test.XXXXXX)"
trap 'rm -rf -- "$tmp"' EXIT

run_fixture() {
	(cd "$fixture" && "$runner" "$@" ./)
}

trace="$tmp/pass.trace"
output="$(SHARDS=3 SHARD_FIXTURE_TRACE="$trace" run_fixture -timeout 5s -p 1 -count=4 -v)"
[ "$(wc -l <"$trace" | tr -d ' ')" -eq 4 ]
[ "$(cut -d' ' -f1 "$trace" | sort -u | wc -l | tr -d ' ')" -eq 4 ]
case "$output" in
	*'shard 1/3'*'shard 2/3'*'shard 3/3'*'3 shards'*) ;;
	*) printf 'passing run did not report every shard and its summary:\n%s\n' "$output" >&2; exit 1 ;;
esac

trace="$tmp/fail.trace"
set +e
output="$(SHARDS=2 SHARD_FIXTURE_FAIL=1 SHARD_FIXTURE_TRACE="$trace" run_fixture -timeout 5s 2>&1)"
status=$?
set -e
[ "$status" -ne 0 ]
case "$output" in
	*'fixture failure requested'*'--- FAIL: TestCharlieCanFail'*'shard 2/2'*'failing: TestCharlieCanFail'*) ;;
	*) printf 'failing run did not surface the failure after every shard:\n%s\n' "$output" >&2; exit 1 ;;
esac
grep -q '^TestDeltaCanSleep done$' "$trace"

for count in 1 3; do
	SHARDS="$count" run_fixture -timeout 5s >/dev/null
	set +e
	SHARDS="$count" SHARD_FIXTURE_FAIL=1 run_fixture -timeout 5s >/dev/null 2>&1
	status=$?
	set -e
	[ "$status" -ne 0 ]
done

set +e
output="$(SHARDS=2 SHARD_FIXTURE_SLEEP=2s run_fixture -timeout 100ms 2>&1)"
status=$?
set -e
[ "$status" -ne 0 ]
case "$output" in
	*'panic: test timed out after 100ms'*'TestDeltaCanSleep'*) ;;
	*) printf 'timeout run swallowed the panic or its running test:\n%s\n' "$output" >&2; exit 1 ;;
esac

report="$tmp/report.json"
SHARDS=3 "$root/scripts/test-report.sh" "$report" bash -c 'cd "$1" && exec "$2" -json -timeout 5s ./' bash "$fixture" "$runner"
serial_report="$tmp/serial-report.json"
"$root/scripts/test-report.sh" "$serial_report" bash -c 'cd "$1" && exec go test -json -count=1 ./' bash "$fixture"
python3 - "$report" "$serial_report" <<'PY'
import json
import sys

with open(sys.argv[1], encoding="utf-8") as source:
    report = json.load(source)
with open(sys.argv[2], encoding="utf-8") as source:
    serial = json.load(source)
top = [test for test in report["tests"] if "/" not in test["name"]]
assert len(top) == 4, report
assert len({test["name"] for test in top}) == 4, report
assert len(report["packages"]) == 1, report
assert report["tests_passed"] == serial["tests_passed"], (report, serial)
assert report["tests_failed"] == serial["tests_failed"], (report, serial)
PY

set +e
output="$(SHARDS=2 run_fixture -definitely-not-a-go-test-flag 2>&1)"
status=$?
set -e
[ "$status" -eq 2 ]
case "$output" in *'-definitely-not-a-go-test-flag'*) ;; *) printf 'unsupported flag was not named: %s\n' "$output" >&2; exit 1 ;; esac

printf 'shard-test acceptance: ok\n'
