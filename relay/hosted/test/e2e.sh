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

# All local workers consume the same reactor. Multiple custom-build watchers
# otherwise keep rewriting it and restarting each other during requests.
sh ../../scripts/raft-hosted-build.sh
node --input-type=module - "$work/wrangler.toml" <<'JS'
import { readFileSync, writeFileSync } from 'node:fs';
import { resolve } from 'node:path';
const config = readFileSync('wrangler.toml', 'utf8')
  .replace(/^\[build\][\s\S]*?(?=^\[|$(?![\s\S]))/m, '')
  .replace(/^main = "([^"]+)"/m, (_, path) => `main = ${JSON.stringify(resolve(path))}`);
writeFileSync(process.argv[2], config);
JS

start() { # port, extra wrangler args
  port=$1; shift
  # Existing storage scenarios use an open service; targeted storage tests prove the closed default.
  remaining=$#
  while [ "$remaining" -gt 0 ]; do
    argument=$1; shift
    case "$argument" in
      CAF_LIMITS:*) argument="CAF_LIMITS:$(node -e 'console.log(JSON.stringify({...JSON.parse(process.argv[1]), storageOpen:true}))' "${argument#CAF_LIMITS:}")" ;;
    esac
    set -- "$@" "$argument"
    remaining=$((remaining - 1))
  done
  persist="$work/$port"
  if [ "$port" = "${scenario_port:-}" ]; then persist=$scenario_state; fi
  : >"$work/$port.log"
  # Reuse the opt-in local proxy transport policy; retries remain off by default.
  sh test/local.sh node_modules/.bin/wrangler dev --config "$work/wrangler.toml" --local --ip 127.0.0.1 --port "$port" --persist-to "$persist" "$@" >"$work/$port.log" 2>&1 &
  last=$!
  pids="$pids $last"
  for _ in $(seq 60); do grep -q 'Ready on' "$work/$port.log" && return 0; sleep 1; done
  cat "$work/$port.log"; return 1
}

scenario_count=0
restartScenario() { # port, previous pid, limits, label, extra wrangler args
  scenario_port=$1; previous_pid=$2; scenario_limits=$3; scenario_label=$4; shift 4
  kill "$previous_pid"; wait "$previous_pid" 2>/dev/null || true
  # Prior scenarios' durable alarms must not reactivate groups in this isolate.
  scenario_count=$((scenario_count + 1))
  scenario_state="$work/scenario-$scenario_count-$scenario_port-$scenario_label"
  start "$scenario_port" --var "CAF_LIMITS:$scenario_limits" "$@"
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
default_pid=$last
restartDefault() {
  restartScenario 18791 "$default_pid" "$OPEN" "$1" --var TRUST_PROXY:1
  default_pid=$last
  default_state=$scenario_state
}
scenario() {
  script=$1; shift
  restartDefault "$script"
  IDENTITY_DO_DIR="$default_state/v3/do/codeaf-hosted-relay-IdentityDO" R2_DIR="$default_state/v3/r2/miniflare-R2BucketObject" node "test/$script.e2e.mjs" "$@"
}
start 18792 --var "CAF_LIMITS:$TIGHT"
tight_pid=$last
restartTight() {
  restartScenario 18792 "$tight_pid" "$TIGHT" "$1"
  tight_pid=$last
  tight_state=$scenario_state
}
start 18794 --var 'CAF_LIMITS:{"newIdentitiesPerIpPerDay":1000000,"pairMaxBoxes":2}' --var TRUST_PROXY:1
# The watch cap case needs a relay that lets one identity hold five sockets, not a thousand.
start 18796 --var 'CAF_LIMITS:{"newIdentitiesPerIpPerDay":1000000,"maxWatchers":5}' --var TRUST_PROXY:1
# Link pairing: a 3 s request life, decided requests kept 1.5 s, 40 live requests, 6 creates an hour, 3 misses a minute.
LINK='{"newIdentitiesPerIpPerDay":1000000,"pairTtlMs":3000,"linkDecidedKeepMs":1500,"linkMaxLive":40,"linkCreatePerHour":6,"linkMissPerMinute":3,"linkReadPerMinute":500,"requestsPerMinute":100000,"requestsPerMinutePerDevice":100000}'
# Capacity needs forty simultaneously live requests. Use the production TTL
# only there; expiry and every other link case retain the short test TTL.
LINK_CAPACITY=$(node -e 'const limits=JSON.parse(process.argv[1]);delete limits.pairTtlMs;console.log(JSON.stringify(limits));' "$LINK")
start 18797 --var "CAF_LIMITS:$LINK" --var TRUST_PROXY:1
link_pid=$last
restartLink() {
  link_limits=$LINK
  if [ "$1" = RelayFull ]; then link_limits=$LINK_CAPACITY; fi
  restartScenario 18797 "$link_pid" "$link_limits" "$1" --var TRUST_PROXY:1
  link_pid=$last
}

# A relay served under /fabric, as the hosted deployment is: the same wires, beneath one prefix.
start 18798 --var "CAF_LIMITS:$OPEN" --var TRUST_PROXY:1 --var BASE_PATH:/fabric
node test/base.e2e.mjs
RELAY=http://127.0.0.1:18798/fabric node test/api.e2e.mjs

for script in api race flight dedup; do scenario "$script"; done
for cap in $(node test/caps.e2e.mjs --list); do
  restartTight "$cap"
  node test/caps.e2e.mjs "$cap"
done
restartTight pair
node test/pair.e2e.mjs
for link_case in $(node test/link.e2e.mjs --list); do
  restartLink "$link_case"
  node test/link.e2e.mjs "$link_case"
done
for watch_case in $(node test/watch.e2e.mjs --list); do scenario watch "$watch_case"; done
scenario watch lease
WATCH_LOG="$work/18791.log" scenario watch idle
scenario rotation
restartTight newcomers
# The runtime creates this namespace lazily; the initial stored count is zero.
mkdir -p "$tight_state/v3/do/codeaf-hosted-relay-IdentityDO"
IDENTITY_DO_DIR="$tight_state/v3/do/codeaf-hosted-relay-IdentityDO" node test/newcomers.e2e.mjs

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
