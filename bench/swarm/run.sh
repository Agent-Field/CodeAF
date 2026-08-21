#!/usr/bin/env bash
# Swarm-mode benchmark: A/B across a task corpus, swarm OFF vs ON.
#
# One cell = one task × one arm. Every cell runs `aforge do` in a fresh
# directory with a private durable store, then a category verdict script
# grades the artifacts — never the run's self-report. The corpus and the
# doctrine live in README.md; this file is only the protocol.
#
# Read bench/README.md for the cost doctrine (self-reported usage only).
# This suite inherits it: the cost column comes from the journal's usage
# table, summed per cell, and from nothing else.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
AFORGE_BIN="${AFORGE_BIN:-aforge}"
RESULTS="${RESULTS:-$HERE/results}"
MODEL="${MODEL:-~deepseek/deepseek-v4-flash-latest}"

# Tier controls power, not coverage. smoke runs one seed per category at
# n=1 (wiring check, minutes). standard runs the full corpus at n=3 (the
# number worth quoting). full adds the large tasks at n=5.
TIER="${TIER:-smoke}"
N_SMALL=1; N_MEDIUM=1; N_LARGE=0
case "$TIER" in
  smoke)    N_SMALL=1; N_MEDIUM=1; N_LARGE=0 ;;
  standard) N_SMALL=3; N_MEDIUM=3; N_LARGE=1 ;;
  full)     N_SMALL=5; N_MEDIUM=5; N_LARGE=3 ;;
esac

CELL_TIMEOUT="${CELL_TIMEOUT:-900}"

# The corpus. category|task-file|verdict-script — three fields, pipe-split,
# read from tasks.txt one per line. Adding a task is adding a row and two
# files; this script never changes.
TASKS="${TASKS:-$HERE/tasks.txt}"

mkdir -p "$RESULTS"
CSV="$RESULTS/results.csv"
# BENCH_CLEAN=1 starts the CSV over; without it smoke-tier rows sit beside the
# timing-baseline rows and a mean over the file mixes the two powers.
if [ "${BENCH_CLEAN:-0}" = "1" ]; then rm -f "$CSV"; fi
[ -f "$CSV" ] || echo "task,category,arm,seed,wall_s,exit,cost_usd,nodes,usage_rows,verdict" > "$CSV"

run_cell() {
  local task="$1" category="$2" verdict="$3" fixture="$4" arm="$5" seed="$6"
  local dir="$RESULTS/${task}-${arm}-s${seed}"
  rm -rf "$dir"; mkdir -p "$dir"
  cp "$HERE/tasks/$task.txt" "$dir/TASK.txt"
  # A fixture builds the inputs the task claims exist (repo, notes, images):
  # without it those cells grade nothing. `-` means the task needs only prose.
  if [ "$fixture" != "-" ]; then
    "$HERE/fixtures/$fixture" "$dir" > "$dir/fixture.log" 2>&1 || {
      echo "$task,$category,$arm,$seed,0,125,0,0,0,fixture-failed" > "$dir/row.csv"
      return
    }
  fi
  local t0 t1 wall ec cost nodes urows verdict_out
  t0=$(date +%s)
  (cd "$dir" && AFORGE_SWARM="$arm" AFORGE_MODEL="$MODEL" timeout "$CELL_TIMEOUT" \
    "$AFORGE_BIN" do "$(cat "$HERE/tasks/$task.txt")" \
      -db "$dir/store.db" -keep -timeout "$CELL_TIMEOUT" --yes-spend --json \
      > "$dir/out.json" 2> "$dir/stderr.log")
  ec=$?
  t1=$(date +%s); wall=$((t1 - t0))
  # Cost and shape from the journal, the honest source. Missing store = the
  # cell died before it could journal; zeros, and the verdict will say why.
  cost=$(sqlite3 "$dir/store.db" "SELECT IFNULL(SUM(cost),0) FROM usage;" 2>/dev/null || echo 0)
  nodes=$(sqlite3 "$dir/store.db" "SELECT COUNT(*) FROM nodes;" 2>/dev/null || echo 0)
  urows=$(sqlite3 "$dir/store.db" "SELECT COUNT(*) FROM usage;" 2>/dev/null || echo 0)
  # Verdict: the category script grades artifacts, never the prose. It prints
  # one line — a number, a count, or a short verdict string. Nonzero exit from
  # the script itself means the grade could not be computed.
  verdict_out=$("$HERE/verdicts/$verdict" "$dir" 2>/dev/null || echo "ungradable")
  # The CSV row goes to a per-cell file, NOT the shared CSV: parallel cells
  # appending one file would interleave bytes within rows. The parent drains
  # the row files, so every CSV line is written whole by one process.
  echo "$task,$category,$arm,$seed,$wall,$ec,$cost,$nodes,$urows,$verdict_out" > "$dir/row.csv"
}

# Concurrent cells, pair-fair: both arms of one task fire in the same wave so
# a provider slowdown or rate-limit wave lands on the OFF and ON arm of the
# same comparison together and cancels out of the delta. JOBS bounds the wave;
# JOBS=1 is the timing-clean mode (no provider throughput sharing).
JOBS="${JOBS:-4}"

echo "swarm bench — tier=$TIER model=$MODEL jobs=$JOBS results=$RESULTS"
running=0
drain() {
  # Wait for one job, then flush any finished cells' rows to the CSV.
  wait -n 2>/dev/null
  for rf in "$RESULTS"/*/row.csv; do
    [ -f "$rf" ] || continue
    cat "$rf" >> "$CSV"; rm -f "$rf"
  done
}
while IFS='|' read -r task category verdict fixture rep; do
  case "$task" in ''|\#*) continue ;; esac
  case "$rep" in
    S) n=$N_SMALL ;; M) n=$N_MEDIUM ;; L) n=$N_LARGE ;;
  esac
  [ "$n" -eq 0 ] && continue
  for seed in $(seq 1 "$n"); do
    run_cell "$task" "$category" "$verdict" "$fixture" 0 "$seed" &
    run_cell "$task" "$category" "$verdict" "$fixture" 1 "$seed" &
    running=$((running+2))
    while [ "$(jobs -rp | wc -l)" -ge "$JOBS" ]; do drain; done
  done
done < "$TASKS"
wait
for rf in "$RESULTS"/*/row.csv; do
  [ -f "$rf" ] || continue
  cat "$rf" >> "$CSV"; rm -f "$rf"
done
echo "== done =="
