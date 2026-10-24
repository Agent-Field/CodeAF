#!/usr/bin/env bash
# One command that holds both relays to every contract and prints one table.
#
#   scripts/relay-conformance.sh            # the whole matrix
#   SKIP_E2E=1 scripts/relay-conformance.sh # without the Worker's end-to-end script (about 10 minutes)
#
# Two relays, the Go one and the Worker, are run through the same suites: in process for the Go relay,
# as a live process for both. The Worker runs under `wrangler dev --local` on free ports and never
# against staging, so a billing window stays clean. Tracks run sequentially on the shared box;
# CONFORMANCE_PARALLEL=1 opts into separate concurrent tracks. Their suites run one after another, because the watch suite's thousand
# sockets would otherwise stall a Worker that a lease case is waiting on. Cases default to two
# at a time (watch cases run alone), and the local Worker transport bounds requests, retrying only the
# proxy's connection-lost error twice and reporting every retry in the table. The exit status is 0 only
# when no row failed and no suite ran zero cases.
set -uo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
hosted=$root/relay/hosted
. "$hosted/test/proxy-env.sh"
work=${CONFORMANCE_WORK:-$root/.lane/conformance}
rm -rf "$work"; mkdir -p "$work/rows"
cd "$root"

# The suites of relayconf, in the order a table reads them: name and the test pattern that runs it.
suites=(
	"store:^TestStoreConformance$"
	"directory:^TestDirectoryConformance$"
	"isolation:^(TestNamespaceIsolation|TestSkewIsRefused)$"
	"pairing:^TestPairingConformance$"
	"rotation:^TestRotationConformance$"
	"bigtake:^TestBigTakeBack$"
	"watch:^TestWatchConformance$"
	"lease:^TestLeaseConformance$"
	"fabric:^TestFabricConformance$"
)

pids=()
stop() { for p in "${pids[@]:-}"; do [[ -n $p ]] && kill "$p" 2>/dev/null; done; }
trap stop EXIT

# free_port answers a port nobody listens on and nobody else is about to take.
free_port() {
	python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1])'
}

# row appends one table line to the file of the running track (ROWFILE), so tracks never share a writer
# and a track's rows read in the order its suites ran.
row() { printf '%s\n' "$*" >>"$work/rows/$ROWFILE"; } # relay suite pass skip fail secs note

# tally reads `go test -json` and prints "pass skip fail" over the leaf cases of the named tests: a
# test that has subtests counts through them, so one skipped case among forty is one skip.
tally() {
	python3 - "$1" <<'PY'
import json, sys
res, names = {}, set()
for line in open(sys.argv[1]):
    try: e = json.loads(line)
    except ValueError: continue
    t = e.get("Test")
    if t and e["Action"] in ("pass", "skip", "fail"):
        res[t] = e["Action"]; names.add(t)
leaf = [t for t in names if not any(o.startswith(t + "/") for o in names)]
print(*(sum(res[t] == a for t in leaf) for a in ("pass", "skip", "fail")))
PY
}

# Proxy exceptions are counted at their source, before HTTP can erase the
# transport reason and turn an application failure into an unsafe retry.
proxy_retries() {
	python3 - "$work" <<'PY'
import pathlib, sys
marker = "codeaf local proxy transport retry: Network connection lost."
print(sum(marker in line for path in pathlib.Path(sys.argv[1]).glob("worker-*.log") for line in path.open(errors="replace")))
PY
}

# gotest runs one suite and records its row: relay, suite, package, test pattern, extra go test args.
gotest() {
	local relay=$1 suite=$2 pkg=$3 pat=$4; shift 4
	local log=$work/$relay.$suite.json t0=$SECONDS
	local test_parallel=$CONFORMANCE_TEST_PARALLEL
	# The thousand-socket watch case needs the proxy to itself, while lease
	# cases spend most of their time waiting on distinct identities' clocks.
	if [[ $suite == watch ]]; then test_parallel=1; fi
	local worker_concurrency=
	local retries_before=0
	if [[ $relay == worker ]]; then worker_concurrency=${CONFORMANCE_HTTP_CONCURRENCY:-$CODEAF_RELAYCONF_CONCURRENCY}; fi
	if [[ $relay == worker ]]; then retries_before=$(proxy_retries); fi
	CODEAF_RELAYCONF_CONCURRENCY=$worker_concurrency \
		go test -p=2 -parallel="$test_parallel" -json -count=1 -timeout 15m "$pkg" -run "$pat" "$@" >"$log" 2>"$log.err"
	local code=$? counts note=""
	counts=$(tally "$log")
	read -r p s f <<<"$counts"
	if [[ $code -ne 0 && $f -eq 0 ]]; then f=1; note="go test exit $code, see $log.err"; fi
	if [[ $((p + s + f)) -eq 0 ]]; then f=1; note="ran zero cases"; fi
	# A local expiry fixture must exercise the canonical assertion, rather
	# than silently gaining a skip because another fixture's TTL grew.
	if [[ $relay == worker && $suite == pairing ]] && ! python3 - "$log" <<'PY'
import json, sys
events = []
for line in open(sys.argv[1]):
    try: events.append(json.loads(line))
    except ValueError: pass
sys.exit(not any(e.get("Test") == "TestPairingConformance/ExpiryDeletes" and e.get("Action") == "pass" for e in events))
PY
	then f=$((f + 1)); note="${note:+$note; }canonical expiry case did not pass"; fi
	if [[ $relay == worker ]]; then
		local retries
		retries=$(( $(proxy_retries) - retries_before ))
		note="${note:+$note; }transport retries: $retries"
	fi
	row "$relay" "$suite" "$p" "$s" "$f" "$((SECONDS - t0))" "$note"
}

# start_relay launches a long-lived relay in the background and waits until it answers. It sets
# $started to the pid, which is recorded so that only this run's own processes are ever stopped.
start_relay() { # ready-probe url, logfile, command...
	local url=$1 log=$2; shift 2
	"$@" >"$log" 2>&1 &
	started=$!; pids+=("$started")
	for _ in $(seq 90); do [[ $(curl -s -o /dev/null -w "%{http_code}" "$url") != 000 ]] && return 0; sleep 1; done
	echo "relay did not come up, see $log" >&2; return 1
}

# The relayconf suites against one live relay; $1 names the relay in the table.
conf_live() { # relay, url, small-url or "", expiry-url or ""
	local relay=$1 url=$2 small=${3:-} expiry=${4:-}
	for s in "${suites[@]}"; do
		local cap_url= cap_pid=
		# The cap case still opens all thousand sockets. Its proxy gets a
		# private runtime so teardown cannot stall the next case's upgrade.
		if [[ $relay == worker && ${s%%:*} == watch ]]; then
			local cap_port; cap_port=$(free_port)
			wrangler "$cap_port" "$work/persist-$cap_port" "$OPEN" || { row worker watch 0 0 1 0 "watch-cap wrangler did not start"; continue; }
			cap_url=http://127.0.0.1:$cap_port; cap_pid=$started
		fi
		gotest "$relay" "${s%%:*}" ./internal/relayconf "${s#*:}" -tags relayurl -args -relay-url="$url" ${small:+-relay-small-url="$small"} ${expiry:+-relay-expiry-url="$expiry"} ${cap_url:+-relay-watch-cap-url="$cap_url"}
		if [[ -n $cap_pid ]]; then kill "$cap_pid" 2>/dev/null; fi
	done
}

# Track 1: the Go relay in process, then as a live process.
track_go() {
	if [[ -z ${CONFORMANCE_LIVE_ONLY:-} ]]; then
	gotest go-inproc store ./internal/blobstore '.'
	gotest go-inproc directory ./internal/relayserve '^TestRelayPassesDirectoryConformance$'
	gotest go-inproc pairing ./internal/relayserve '^TestPairing'
	gotest go-inproc rotation ./internal/relayserve 'Rotated|Frozen|Retire|Sweep'
	gotest go-inproc watch ./internal/relayserve '^TestWatch'
	gotest go-inproc lease ./internal/relayserve '^TestLease'
	gotest go-inproc fabric ./internal/relayserve '^TestFabricConformance$'
	fi
	local relay_bin=${CONFORMANCE_CODEAF:-$root/bin/codeaf}
	# An explicit candidate avoids rebuilding a binary the caller has already prepared with make build.
	if [[ -z ${CONFORMANCE_CODEAF:-} ]]; then
		make build ${FURROW_ARTIFACT:+FURROW_ARTIFACT="$FURROW_ARTIFACT"} >"$work/build.log" 2>&1 || { row go-live build 0 0 1 0 "codeaf did not build; see $work/build.log"; return; }
	fi
	[[ -x $relay_bin ]] || { row go-live build 0 0 1 0 "candidate is not executable: $relay_bin"; return; }
	local port; port=$(free_port)
	# Two seconds of shortest grace and a one-second sweep let the deletion case run in seconds.
	start_relay "http://127.0.0.1:$port/status" "$work/go-live.log" \
		"$relay_bin" relay --listen "127.0.0.1:$port" --store "$work/go-store" --quiet --trust-proxy --min-grace 2s --sweep-every 1s || { row go-live start 0 0 1 0 "relay did not start"; return; }
	conf_live go-live "http://127.0.0.1:$port" ""
	kill "$started" 2>/dev/null
}

# Authorization cases keep the normal mailbox lifetime. Only ExpiryDeletes uses
# a short-lived service; RelayFull keeps its separate two-mailbox service.
OPEN='{"newIdentitiesPerIpPerDay":1000000,"minGraceMs":2000,"sweepPageSize":2,"requestsPerMinute":100000,"requestsPerMinutePerDevice":100000}'
SMALL='{"newIdentitiesPerIpPerDay":1000000,"pairMaxBoxes":2}'
EXPIRY='{"newIdentitiesPerIpPerDay":1000000,"pairTtlMs":4000}'
wrangler() { # port, persist dir, limits json
	start_relay "http://127.0.0.1:$1/" "$work/worker-$1.log" \
		sh "$hosted/test/local.sh" "$hosted/node_modules/.bin/wrangler" dev --local -c "${CONFORMANCE_WORKER_CONFIG:-$hosted/wrangler.toml}" --name "codeaf-conformance-$1" --inspector-port 0 --ip 127.0.0.1 --port "$1" --persist-to "$2" \
		--var "CAF_LIMITS:$3" --var TRUST_PROXY:1
}

# Track 2: relayconf through the Worker.
track_worker() {
	cd "$hosted" || return
	local a b c; a=$(free_port); b=$(free_port); c=$(free_port)
	wrangler "$a" "$work/persist-$a" "$OPEN" || { row worker start 0 0 1 0 "wrangler did not start"; return; }
	wrangler "$b" "$work/persist-$b" "$SMALL" || { row worker start 0 0 1 0 "wrangler did not start"; return; }
	wrangler "$c" "$work/persist-$c" "$EXPIRY" || { row worker start 0 0 1 0 "wrangler did not start"; return; }
	cd "$root" && conf_live worker "http://127.0.0.1:$a" "http://127.0.0.1:$b" "http://127.0.0.1:$c"
}

# Track 3: the Worker's own unit tests and end-to-end script. e2e.sh owns ports 18791 to 18796, so
# when another run holds one this waits for it, and never stops a process it did not start.
track_hosted() {
	cd "$hosted" || return
	local t0=$SECONDS out=$work/npm-test.log
	npm test >"$out" 2>&1; local code=$?
	local p s f
	# node's reporter prefixes its totals with "ℹ " on a terminal-less run and "# " in tap mode.
	p=$(sed -n 's/^[#ℹ] pass //p' "$out"); s=$(sed -n 's/^[#ℹ] skipped //p' "$out"); f=$(sed -n 's/^[#ℹ] fail //p' "$out")
	local note=""
	[[ $code -ne 0 && ${f:-0} -eq 0 ]] && f=1 note="npm test exit $code, see $out"
	[[ ${p:-0} -eq 0 ]] && f=1 note="ran zero cases"
	row worker npm-test "${p:-0}" "${s:-0}" "${f:-0}" "$((SECONDS - t0))" "$note"
	[[ -n ${SKIP_E2E:-} ]] && return
	while ss -ltn | grep -qE ':1879[1-6] '; do echo "e2e ports busy, waiting" >&2; sleep 30; done
	t0=$SECONDS
	sh test/e2e.sh >"$work/e2e.log" 2>&1; code=$?
	# e2e.sh stops on the first failing script, so it is one case that passed or failed.
	row worker e2e "$((code == 0))" 0 "$((code != 0))" "$((SECONDS - t0))" "$([[ $code -ne 0 ]] && echo "see $work/e2e.log")"
}

# Each track is a subshell with its own exit trap, so an interrupted run stops the relays it started.
tracks=()
for t in ${CONFORMANCE_TRACKS:-go worker hosted}; do
	if [[ ${CONFORMANCE_PARALLEL:-0} != 1 || ${CONFORMANCE_SERIAL:-0} == 1 ]]; then
		( ROWFILE=$t; pids=(); trap stop EXIT; "track_$t" )
		continue
	fi
	( ROWFILE=$t; pids=(); trap stop EXIT; "track_$t" ) &
	tracks+=($!)
done
wait "${tracks[@]}"

# The table: every row file in the order the suites read, failures counted for the exit status.
printf '%-10s %-10s %5s %5s %5s %6s  %s\n' relay suite pass skip fail secs note
bad=0
for f in "$work"/rows/go "$work"/rows/worker "$work"/rows/hosted; do
	[[ -f $f ]] || continue
	while read -r relay suite p s fl secs note; do
		printf '%-10s %-10s %5s %5s %5s %6s  %s\n' "$relay" "$suite" "$p" "$s" "$fl" "$secs" "$note"
		[[ $fl -gt 0 ]] && bad=1
	done <"$f"
done
printf 'local proxy transport retries (including startup): %s\n' "$(proxy_retries)"
exit $bad
