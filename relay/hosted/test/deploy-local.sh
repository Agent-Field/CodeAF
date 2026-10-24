#!/bin/sh
# C22 exercises the operator's smoke checks locally; C20 exercises the cost brakes on a fresh Worker.
set -eu
cd "$(dirname "$0")/.."
. ./test/proxy-env.sh
proof_root=$(mktemp -d "${TMPDIR:-/tmp}/codeaf-relay-proof.XXXXXX")
proof_pid=
cleanup() {
  proof_status=$?
  if [ -n "$proof_pid" ]; then kill "$proof_pid" 2>/dev/null || true; wait "$proof_pid" 2>/dev/null || true; fi
  if [ "$proof_status" -eq 0 ]; then rm -rf "$proof_root";
  else echo "Local proof failed; evidence kept at $proof_root" >&2; fi
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
port=$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1])')
start() {
  config=$1; name=$2; shift 2
  sh test/local.sh node_modules/.bin/wrangler dev --local -c "$config" --name "$name" \
    --ip 127.0.0.1 --port "$port" --inspector-port 0 --persist-to "$proof_root/$name" "$@" >"$proof_root/$name.log" 2>&1 &
  proof_pid=$!
  for _ in $(seq 90); do
    if curl -s -o /dev/null "http://127.0.0.1:$port/"; then return; fi
    sleep 1
  done
  cat "$proof_root/$name.log"; return 1
}
stop() {
  kill "$proof_pid" 2>/dev/null || true
  wait "$proof_pid" 2>/dev/null || true
  proof_pid=
}

start production.toml "caf-production-local-$port"
page=$(curl -fsS "http://127.0.0.1:$port/p/k7m2q9xd")
printf '%s' "$page" | rg -q '<!doctype html>'
status=$(curl -sS -o "$proof_root/missing.json" -w '%{http_code}' "http://127.0.0.1:$port/fabric/does-not-exist")
test "$status" = 404
python3 -c 'import json,sys; assert json.load(open(sys.argv[1])) == {"err":"not_found"}' "$proof_root/missing.json"
echo 'C22: production routes, pairing page and 404 JSON pass locally'
page_retries=$(grep -Fc 'codeaf local proxy transport retry: Network connection lost.' "$proof_root/caf-production-local-$port.log" || true)
echo "C22: local proxy transport retries: $page_retries"
stop

start staging.toml "caf-brakes-local-$port" --var 'CAF_LIMITS:{"globalNewIdentitiesPerDay":2,"globalStoreBytes":512,"minGraceMs":2000}'
RELAY="http://127.0.0.1:$port" node test/global.e2e.mjs >"$proof_root/global.log" 2>&1 || { cat "$proof_root/global.log"; exit 1; }
cat "$proof_root/global.log"
global_retries=$(grep -Fc 'codeaf local proxy transport retry: Network connection lost.' "$proof_root/caf-brakes-local-$port.log" || true)
echo "C20: local proxy transport retries: $global_retries"
stop

# The canonical runner gives each suite its own report and covers the two-mailbox case too.
(cd ../.. && GOFLAGS='-buildvcs=false -p=2' GOMAXPROCS=4 \
  CONFORMANCE_TRACKS=worker CONFORMANCE_SERIAL=1 CONFORMANCE_WORK="$proof_root/conformance" \
  CONFORMANCE_WORKER_CONFIG="$PWD/relay/hosted/staging.toml" SKIP_E2E=1 bash scripts/relay-conformance.sh)
echo 'C22: staging conformance passes locally, including the capped second relay'
