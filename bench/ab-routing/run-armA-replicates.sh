#!/usr/bin/env bash
# Arm A only: run the three replicates as three concurrent streams.
#
# Why this exists. run-arm.sh runs every cell in sequence, which arm B needs
# because its ledger has to carry forward in a defined order. Arm A does not:
# DESIGN.md §5 gives every arm-A cell its own fresh profile directory precisely
# so the replicates are independent, and independent cells can run at the same
# time. Each stream still does its three tasks in order, so nothing inside a
# replicate is reordered.
#
# What this costs, stated plainly: wall clock is then measured under three-way
# self-contention and is NOT comparable to a sequential arm B. The uncontended
# per-cell timings come from the calibration round, which ran one cell at a
# time, and those are the figures BASELINE.md quotes for latency. Success rate,
# score, cost and token counts are unaffected by contention and are what this
# script is for.
#
#   bench/ab-routing/run-armA-replicates.sh
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
STAMP="$(date +%Y%m%d-%H%M%S)"
REPS="${REPS:-3}"
TASKS="${TASKS:-t1-logstore t2-synthesis t3-shiftplan}"

pids=()
for rep in $(seq 1 "$REPS"); do
  # Each stream writes its own jsonl so two streams cannot interleave a line;
  # they are concatenated once everything has landed.
  RESULTS="$HERE/runs/armA-$STAMP-r$rep" \
  JSONL="$HERE/runs/armA-$STAMP-r$rep.jsonl" \
  ARM=a REPS=1 TASKS="$TASKS" REP_LABEL="$rep" \
    bash "$HERE/run-arm.sh" > "$HERE/runs/armA-$STAMP-r$rep.log" 2>&1 &
  pids+=($!)
  echo "stream $rep started (pid ${pids[-1]})"
  # A short stagger so three planners do not hit the provider in the same
  # instant; it costs nothing and keeps the first minute from looking like a
  # burst to the rate limiter.
  sleep 10
done

status=0
for pid in "${pids[@]}"; do
  wait "$pid" || status=1
done

# Stamp the replicate number onto each stream's rows and merge. Every stream
# ran with REPS=1 and so labelled itself rep 1; the stream index is the real
# replicate.
python3 - "$HERE" "$STAMP" "$REPS" <<'PY'
import json, sys, os
here, stamp, reps = sys.argv[1], sys.argv[2], int(sys.argv[3])
out = os.path.join(here, "results-armA.jsonl")
rows = []
for rep in range(1, reps + 1):
    path = os.path.join(here, "runs", f"armA-{stamp}-r{rep}.jsonl")
    if not os.path.exists(path):
        print(f"warning: stream {rep} produced no rows", file=sys.stderr)
        continue
    for line in open(path):
        line = line.strip()
        if not line:
            continue
        row = json.loads(line)
        row["rep"] = rep
        row["concurrent_streams"] = reps
        rows.append(row)
with open(out, "a") as f:
    for row in sorted(rows, key=lambda r: (r["rep"], r["task"])):
        f.write(json.dumps(row) + "\n")
print(f"merged {len(rows)} rows into {out}")
PY

python3 "$HERE/summarize.py" "$HERE/results-armA.jsonl" --arm a
exit $status
