#!/usr/bin/env bash
# One heavy user against the staging relay, then what Cloudflare billed for it.
#
#   BILL_TITLE="before (10c125f4e)" scripts/relay-bill.sh before [idle-minutes]
#   BILL_TITLE="after push (845e5061d)" scripts/relay-bill.sh after [idle-minutes]
# The label names the run's files; every labelled run so far is a column of the table, oldest first.
#
# It drives the real client (internal/relaybill through the home screen's own
# pace in internal/tui3), waits for the analytics to settle, reads the billed
# usage of each phase, and rewrites section 9 of the decision document with the
# one column per run. A run takes about idle-minutes
# plus ten, so start it detached:
#   setsid nohup scripts/relay-bill.sh after > after.log 2>&1 &
# Use the same idle length for every run: the extrapolation is per second.
set -euo pipefail

label=${1:?usage: relay-bill.sh label [idle-minutes]}
idle=${2:-20}
url=${RELAY_URL:-https://caf-relay-staging.instrument-santosh.workers.dev}
runs=${BILL_DIR:-$HOME/relay-bill-runs}
doc=${BILL_DOC:-/home/santosh/codeaf-prototype/docs/STAGE-1H-DECISION.md}
root=$(cd "$(dirname "$0")/.." && pwd)
cost=$root/relay/hosted/cost
mkdir -p "$runs"
# The column's heading in section 9, for example BILL_TITLE="after push (845e5061d)".
echo "${BILL_TITLE:-$label}" >"$runs/$label.title"

cd "$root"
make furrow FURROW_FROM=source >/dev/null
go test -tags relaybill ./internal/tui3 -run TestRelayBill -count=1 -timeout 120m -v \
	-args -bill-url "$url" -bill-out "$runs/$label.run.json" -bill-idle "${idle}m"
python3 "$cost/billed.py" --manifest "$runs/$label.run.json" --out "$runs/$label.billed.json"

# Every run so far is a column, oldest first.
args=()
for f in $(ls -tr "$runs"/*.run.json); do
	name=$(basename "$f" .run.json)
	[[ -s $runs/$name.billed.json ]] && args+=(--column "$(cat "$runs/$name.title")" "$f" "$runs/$name.billed.json")
done
python3 "$cost/bill.py" "${args[@]}" --doc "$doc"
echo "EXIT=0"
