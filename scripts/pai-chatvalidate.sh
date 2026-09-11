#!/usr/bin/env bash
# Person-like chat validation runner: one live scenario run per call.
#
#   scripts/pai-chatvalidate.sh <scenario> <run-number>
#
# The driver is internal/e2e/paichat_e2e_test.go (build tag e2e); build its
# test binary first, into the scratch root:
#
#   go test -tags e2e -c -o "$PAI_ROOT/e2e.test" ./internal/e2e/
#
# PAI_REPO is the checkout whose bin/aforge the runs drive; it defaults to the
# checkout this script lives in. PAI_ROOT is the scratch root that holds the
# test binary, every run's home and project, and the logs; it has no default,
# because two lanes sharing one root would share one budget and one set of
# logs. PAI_BUDGET_STOP is the spend, summed over every usage ledger under
# PAI_ROOT, at which the runner refuses to start another run (default 1.35).
# Needs OPENROUTER_API_KEY. The last log line is the driver's CHATRESULT.
set -uo pipefail
if [ $# -ne 2 ]; then
  echo "usage: $0 <scenario> <run-number>" >&2; exit 2
fi
scen=$1; n=$(printf '%02d' "$2")
repo=${PAI_REPO:-$(cd "$(dirname "$0")/.." && pwd)}
root=${PAI_ROOT:?set PAI_ROOT to a scratch directory of your own}
stop=${PAI_BUDGET_STOP:-1.35}
spent=$(python3 - "$root" <<'PY'
import glob, json, sys
usd = 0.0
for path in glob.glob(sys.argv[1] + "/*/run*/home/v3/usage.jsonl"):
    for line in open(path):
        try:
            usd += json.loads(line).get("usd", 0) or 0
        except ValueError:
            pass
print(f"{usd:.6f}")
PY
)
if python3 -c "import sys; sys.exit(0 if float('$spent') >= float('$stop') else 1)"; then
  echo "BUDGET STOP: \$$spent spent under $root (stop at \$$stop) — not starting $scen run $n"; exit 3
fi
keep=$root/$scen/run$n
log=$root/logs/$scen-run$n.log
mkdir -p "$keep" "$root/logs"
cd "$repo/internal/e2e" || exit 2
echo "START $scen run$n at $(date -Is) in $repo with \$$spent already spent" | tee "$log"
PAI_SCEN=$scen AFORGE_LOCALWORK_KEEP=$keep timeout 45m "$root/e2e.test" -test.run '^TestPAIChat$' -test.v -test.count=1 -test.timeout 40m >> "$log" 2>&1
code=$?
echo "EXIT $code at $(date -Is)" >> "$log"
grep -o 'CHATRESULT .*' "$log" | tail -1 || echo "RESULT $scen run$n: no CHATRESULT (exit $code)"
exit $code
