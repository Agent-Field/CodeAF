#!/bin/sh
# Starts local relays (one with the default limits, one with short ones), runs the end-to-end
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
  last=$!
  pids="$pids $last"
  for _ in $(seq 60); do grep -q 'Ready on' "$work/$port.log" && return 0; sleep 1; done
  cat "$work/$port.log"; return 1
}

TIGHT='{"newIdentitiesPerIpPerDay":3,"requestsPerMinute":40,"requestsPerMinutePerDevice":25,"framesPerDay":6,"storeBytes":3000,"storeObjects":12,"concurrentPuts":1,"pairTtlMs":3000,"pairCreatePerHour":4,"pairWritePerMinute":10,"pairConcurrentPolls":2,"pairMaxBoxes":4}'
# The default relay takes any number of new identities from one address, as the conformance suite
# makes one per case; the tight one keeps the short limits, and the third is for the restart check.
# The default relay is also the conformance target, which names its network in X-Forwarded-For (as
# --trust-proxy allows) and needs a pairing TTL short enough to wait out; the small one is capped at
# two mailboxes for RelayFull.
# Its request rates are lifted too, because the watch suite dials a thousand sockets as one device in seconds.
# It also has the shortest grace and a two-object sweep page, so the deletion case (and the page loop) run in seconds.
OPEN='{"newIdentitiesPerIpPerDay":1000000,"pairTtlMs":4000,"minGraceMs":2000,"sweepPageSize":2,"requestsPerMinute":100000,"requestsPerMinutePerDevice":100000}'
start 18791 --var "CAF_LIMITS:$OPEN" --var TRUST_PROXY:1
start 18792 --var "CAF_LIMITS:$TIGHT"
start 18794 --var 'CAF_LIMITS:{"newIdentitiesPerIpPerDay":1000000,"pairMaxBoxes":2}' --var TRUST_PROXY:1
# The watch cap case needs a relay that lets one identity hold five sockets, not a thousand.
start 18796 --var 'CAF_LIMITS:{"newIdentitiesPerIpPerDay":1000000,"maxWatchers":5}' --var TRUST_PROXY:1
# Link pairing: a 3 s request life, decided requests kept 1.5 s, 40 live requests, 6 creates an hour, 3 misses a minute.
LINK='{"newIdentitiesPerIpPerDay":1000000,"pairTtlMs":3000,"linkDecidedKeepMs":1500,"linkMaxLive":40,"linkCreatePerHour":6,"linkMissPerMinute":3,"linkReadPerMinute":500,"requestsPerMinute":100000,"requestsPerMinutePerDevice":100000}'
start 18797 --var "CAF_LIMITS:$LINK" --var TRUST_PROXY:1

# A relay served under /fabric, as the hosted deployment is: the same wires, beneath one prefix.
start 18798 --var "CAF_LIMITS:$OPEN" --var TRUST_PROXY:1 --var BASE_PATH:/fabric
node test/base.e2e.mjs
RELAY=http://127.0.0.1:18798/fabric node test/api.e2e.mjs

for script in api race flight dedup; do node "test/$script.e2e.mjs"; done
node test/caps.e2e.mjs
node test/pair.e2e.mjs
node test/link.e2e.mjs
node test/watch.e2e.mjs
node test/watch.e2e.mjs lease
WATCH_LOG="$work/18791.log" node test/watch.e2e.mjs idle
IDENTITY_DO_DIR="$work/18791/v3/do/codeaf-hosted-relay-IdentityDO" R2_DIR="$work/18791/v3/r2/miniflare-R2BucketObject" node test/rotation.e2e.mjs
IDENTITY_DO_DIR="$work/18792/v3/do/codeaf-hosted-relay-IdentityDO" node test/newcomers.e2e.mjs

# Counts survive the relay process: count, wait for the flush, stop the relay, start it again over the same port (so the same storage).
PERSIST='{"newIdentitiesPerIpPerDay":1000000,"statsFlushMs":300}'
start 18793 --var "CAF_LIMITS:$PERSIST"
node test/stats.e2e.mjs put "$work/state.json"
kill "$last"; wait "$last" 2>/dev/null || true
start 18793 --var "CAF_LIMITS:$PERSIST"
node test/stats.e2e.mjs check "$work/state.json"

# The directory version survives the relay process the same way: read it, stop the relay, start it over the same storage.
WPERSIST='{"newIdentitiesPerIpPerDay":1000000}'
start 18795 --var "CAF_LIMITS:$WPERSIST"
node test/watch.e2e.mjs put "$work/watch.json"
kill "$last"; wait "$last" 2>/dev/null || true
start 18795 --var "CAF_LIMITS:$WPERSIST"
node test/watch.e2e.mjs check "$work/watch.json"
if [ "${CONF:-}" = 1 ]; then
  (cd ../.. && go test -count=1 -tags relayurl ./internal/relayconf/ -relay-url=http://127.0.0.1:18791 -relay-small-url=http://127.0.0.1:18794)
  (cd ../.. && go test -count=1 -tags relayurl ./internal/relayconf/ -relay-url=http://127.0.0.1:18798/fabric)
fi
