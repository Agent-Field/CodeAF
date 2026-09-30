#!/usr/bin/env bash
# One heavy user against the staging relay, then what Cloudflare billed for it.
#
#   scripts/relay-bill.sh before [idle-minutes]   # the baseline, before the push lanes
#   scripts/relay-bill.sh after  [idle-minutes]   # the same load again; needs a "before"
#
# It drives the real client (internal/relaybill through the home screen's own
# pace in internal/tui3), waits for the analytics to settle, reads the billed
# usage of each phase, and rewrites section 9 of the decision document with the
# before and, once there is one, the after column. A run takes about idle-minutes
# plus ten, so start it detached:
#   setsid nohup scripts/relay-bill.sh after > after.log 2>&1 &
# Use the same idle length for both runs: the extrapolation is per second.
set -euo pipefail

label=${1:?usage: relay-bill.sh before|after [idle-minutes]}
idle=${2:-20}
url=${RELAY_URL:-https://caf-relay-staging.instrument-santosh.workers.dev}
runs=${BILL_DIR:-$HOME/relay-bill-runs}
doc=${BILL_DOC:-/home/santosh/codeaf-prototype/docs/STAGE-1H-DECISION.md}
root=$(cd "$(dirname "$0")/.." && pwd)
cost=$root/relay/hosted/cost
mkdir -p "$runs"

if [[ $label == after && ! -s $runs/before.billed.json ]]; then
	echo "relay-bill: no before run in $runs; run 'relay-bill.sh before' first" >&2
	exit 2
fi

cd "$root"
make furrow FURROW_FROM=source >/dev/null
go test -tags relaybill ./internal/tui3 -run TestRelayBill -count=1 -timeout 120m -v \
	-args -bill-url "$url" -bill-out "$runs/$label.run.json" -bill-idle "${idle}m"
python3 "$cost/billed.py" --manifest "$runs/$label.run.json" --out "$runs/$label.billed.json"

args=(--manifest "$runs/before.run.json" --billed "$runs/before.billed.json" --doc "$doc")
if [[ -s $runs/after.billed.json ]]; then
	args+=(--after-manifest "$runs/after.run.json" --after-billed "$runs/after.billed.json")
fi
python3 "$cost/bill.py" "${args[@]}"
echo "EXIT=0"
