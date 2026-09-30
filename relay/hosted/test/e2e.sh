#!/bin/sh
# Starts two local relays (one with the default limits, one with short ones), runs the end-to-end
# scripts against them, and stops exactly the processes it started, by the pids it recorded.
# CONF=1 also runs the Go relayconf suite against the default relay.
set -eu
cd "$(dirname "$0")/.."
work=$(mktemp -d)
pids=""
stop() { for p in $pids; do kill "$p" 2>/dev/null || true; done; rm -rf "$work"; }
trap stop EXIT

start() { # port, extra wrangler args
  port=$1; shift
  : >"$work/$port.log"
  node_modules/.bin/wrangler dev --local --ip 127.0.0.1 --port "$port" --persist-to "$work/$port" "$@" >"$work/$port.log" 2>&1 &
  pids="$pids $!"
  for _ in $(seq 60); do grep -q 'Ready on' "$work/$port.log" && return 0; sleep 1; done
  cat "$work/$port.log"; return 1
}

TIGHT='{"requestsPerMinute":40,"requestsPerMinutePerDevice":25,"framesPerDay":6,"storeBytes":3000,"storeObjects":12,"concurrentPuts":1,"pairTtlMs":3000,"pairCreatePerHour":4,"pairWritePerMinute":10,"pairConcurrentPolls":2,"pairMaxBoxes":4}'
start 18791
start 18792 --var "CAF_LIMITS:$TIGHT"

for script in api race flight; do node "test/$script.e2e.mjs"; done
node test/caps.e2e.mjs
node test/pair.e2e.mjs
if [ "${CONF:-}" = 1 ]; then
  (cd ../.. && go test -count=1 -tags relayurl ./internal/relayconf/ -relay-url=http://127.0.0.1:18791)
fi
