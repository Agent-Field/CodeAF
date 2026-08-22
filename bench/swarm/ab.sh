#!/usr/bin/env bash
# ab.sh — binary-vs-binary A/B over the swarm bench corpus.
#
# One binary runs the whole tier to completion before the other starts, so
# no two cells ever share provider throughput, and every cell repeats SEEDS
# times so a single noisy run cannot decide the winner. The report compares
# per (task, arm): median wall, median cost, mean verdict, plus a wins row.
#
# Usage: ab.sh BASELINE_BIN CURRENT_BIN [RESULTS_DIR]
#   Env: TIER (standard), SEEDS (3), JOBS (1 — timing-clean), MODEL, TASKS.
set -uo pipefail

BASE_BIN="${1:?usage: ab.sh BASELINE_BIN CURRENT_BIN [RESULTS_DIR]}"
CURR_BIN="${2:?usage: ab.sh BASELINE_BIN CURRENT_BIN [RESULTS_DIR]}"
ROOT="${3:-/tmp/swarm-ab-$(date +%s)}"
HERE="$(cd "$(dirname "$0")" && pwd)"

TIER="${TIER:-standard}"
SEEDS="${SEEDS:-3}"
JOBS="${JOBS:-1}"
MODEL="${MODEL:-~deepseek/deepseek-v4-flash-latest}"
TASKS="${TASKS:-$HERE/tasks.txt}"

# run_one LABEL BIN — one full tier, one seed pass, rows into $ROOT/$label/
run_one() {
  local label="$1" bin="$2"
  local dir="$ROOT/$label"
  mkdir -p "$dir"
  (cd "$HERE" && TIER="$TIER" JOBS="$JOBS" TASKS="$TASKS" BENCH_CLEAN=1 \
    RESULTS="$dir" AFORGE_MODEL="$MODEL" AFORGE_BIN="$bin" \
    bash run.sh > "$dir/run.log" 2>&1)
}

echo "A/B — tier=$TIER seeds=$SEEDS jobs=$JOBS root=$ROOT"
echo "  baseline: $BASE_BIN"
echo "  current:  $CURR_BIN"
for seed in $(seq 1 "$SEEDS"); do
  echo "== seed pass $seed/$SEEDS: baseline =="
  run_one "baseline-s$seed" "$BASE_BIN"
  echo "== seed pass $seed/$SEEDS: current =="
  run_one "current-s$seed" "$CURR_BIN"
done

# Merge every seed pass into two flat CSVs for the report.
cat "$ROOT"/baseline-s*/results.csv 2>/dev/null | awk -F, 'NR==1 || $1!="task"' > "$ROOT/baseline.csv"
cat "$ROOT"/current-s*/results.csv  2>/dev/null | awk -F, 'NR==1 || $1!="task"' > "$ROOT/current.csv"

python3 - "$ROOT" <<'PYEOF'
import csv, statistics, sys
from collections import defaultdict

root = sys.argv[1]

def load(path):
    try:
        with open(path) as f:
            return list(csv.DictReader(f))
    except FileNotFoundError:
        return []

base, curr = load(f"{root}/baseline.csv"), load(f"{root}/current.csv")
if not base or not curr:
    print(f"missing data: {len(base)} baseline rows, {len(curr)} current rows", file=sys.stderr)
    sys.exit(1)

def bycell(rows):
    d = defaultdict(list)
    for r in rows:
        d[(r["task"], r["arm"])].append(r)
    return d

B, C = bycell(base), bycell(curr)
cells = sorted(set(B) | set(C))
print(f"\n{'task':<22} {'arm':<10} {'n':>2} {'base_wall':>9} {'curr_wall':>9} {'wallΔ':>7} {'base_cost':>9} {'curr_cost':>9} {'base_v':>6} {'curr_v':>6}  verdict")
print("-" * 120)

def med(rows, key):
    return statistics.median(float(r[key]) for r in rows if r[key]) if rows else 0.0

def meanv(rows):
    vals = []
    for r in rows:
        v = r.get("verdict", "0").split("/")[0]
        try:
            vals.append(float(v))
        except ValueError:
            vals.append(0.0)
    return sum(vals) / len(vals) if vals else 0.0

wins = {"current": 0, "baseline": 0, "tie": 0}
for cell in cells:
    b, c = B.get(cell, []), C.get(cell, [])
    n = f"{len(b)}/{len(c)}"
    bw, cw = med(b, "wall_s"), med(c, "wall_s")
    bc, cc = med(b, "cost_usd"), med(c, "cost_usd")
    bv, cv = meanv(b), meanv(c)
    wd = (cw - bw) / bw * 100 if bw else 0
    if cv > bv:
        w = "current"
    elif cv < bv:
        w = "baseline"
    elif cc < bc * 0.85:
        w = "current"
    elif cc > bc * 1.15:
        w = "baseline"
    else:
        w = "tie"
    wins[w] += 1
    print(f"{cell[0]:<22} {cell[1]:<10} {n:>2} {bw:>8.0f}s {cw:>8.0f}s {wd:>+6.0f}% {bc:>9.4f} {cc:>9.4f} {bv:>6.1f} {cv:>6.1f}  {w}")

print("-" * 120)
print(f"WINS: current={wins['current']} baseline={wins['baseline']} tie={wins['tie']}  ({len(cells)} cells, {len(base)}+{len(curr)} rows)")
PYEOF
echo "== A/B complete: $ROOT =="
