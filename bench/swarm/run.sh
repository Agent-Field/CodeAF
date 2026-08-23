#!/usr/bin/env bash
# Swarm-mode benchmark: A/B across a task corpus, four arms.
#
# One cell = one task × one arm. Every cell runs `aforge do` in a fresh
# directory with a private durable store, then a category verdict script
# grades the artifacts — never the run's self-report. The corpus and the
# doctrine live in README.md; this file is only the protocol.
#
# Read bench/README.md for the cost doctrine (self-reported usage only).
# This suite inherits it: the cost column comes from the journal's usage
# table, summed per cell, and from nothing else.
#
# Arms (named; AFORGE_MECHANISM selects the inhibition/quorum extension,
# gated behind AFORGE_SWARM=1):
#   baseline    AFORGE_SWARM=0, AFORGE_MECHANISM=baseline
#               refusal-first pipeline — no cooperative decomposition.
#   swarm       AFORGE_SWARM=1, AFORGE_MECHANISM=baseline
#               cooperative claim-time decomposition (the original ON arm).
#   inhibition  AFORGE_SWARM=1, AFORGE_MECHANISM=inhibition
#               swarm + explicit scope ownership injected into split parts.
#   quorum      AFORGE_SWARM=1, AFORGE_MECHANISM=quorum
#               swarm + two validators gate the commit, one revision round.
set -uo pipefail

if [ "${1:-}" = "--help" ] || [ "${1:-}" = "-h" ]; then
  cat <<'EOF'
Usage: bench/swarm/run.sh [--help]   (env: TIER, JOBS, MODEL, TASKS, BENCH_CLEAN)

Runs the swarm benchmark corpus, one cell per task × arm × seed, and appends
one CSV row per cell to results/results.csv.

Arms (the factor this suite varies):
  baseline    AFORGE_SWARM=0, AFORGE_MECHANISM=baseline   (refusal-first)
  swarm       AFORGE_SWARM=1, AFORGE_MECHANISM=baseline   (cooperative decomposition)
  inhibition  AFORGE_SWARM=1, AFORGE_MECHANISM=inhibition  (scope ownership in splits)
  quorum      AFORGE_SWARM=1, AFORGE_MECHANISM=quorum      (validators gate commit)

All arms of one task fire in the same wave (pair-fair → now arm-fair) so a
provider slowdown lands on every arm of the comparison together and cancels
out of the delta. JOBS bounds the wave; JOBS=1 is the timing-clean mode.

Tiers (power, not coverage):
  smoke     one seed per non-L task (wiring check)
  standard  full corpus, n=3
  full      adds the L task, n=5

tasks.txt rows may carry an optional 6th pipe-field: a comma-separated arm
list to run for that task. `all` (the default, also the value when the field
is absent) means baseline,swarm,inhibition,quorum,splitgate.
EOF
  exit 0
fi
# The four arms in fixed order (comma-separated, like the tasks.txt field);
# tasks.txt may select a subset.
ARMS_DEFAULT="baseline,swarm,inhibition,quorum,splitgate"
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

# The corpus. task|category|verdict|fixture|tier[|arms] — pipe-split, read
# from tasks.txt one per line. Adding a task is adding a row and two files;
# this script never changes. The optional 6th field is a comma-separated arm
# list (default `all` = baseline,swarm,inhibition,quorum,splitgate).
TASKS="${TASKS:-$HERE/tasks.txt}"


mkdir -p "$RESULTS"
CSV="$RESULTS/results.csv"
# BENCH_CLEAN=1 starts the CSV over; without it smoke-tier rows sit beside the
# timing-baseline rows and a mean over the file mixes the two powers.
if [ "${BENCH_CLEAN:-0}" = "1" ]; then rm -f "$CSV"; fi
[ -f "$CSV" ] || echo "task,category,arm,mechanism,seed,wall_s,exit,cost_usd,nodes,usage_rows,verdict" > "$CSV"

run_cell() {
  local task="$1" category="$2" verdict="$3" fixture="$4" arm="$5" seed="$6"
  # Translate the named arm into the env the harness reads. swarm=1 gates
  # cooperative decomposition; AFORGE_MECHANISM selects the inhibition/quorum
  # extension (baseline = none). The baseline arm runs swarm off; its
  # mechanism is pinned to baseline for a clean, comparable CSV.
  local swarm mech gate
  case "$arm" in
    baseline)   swarm=0; mech=baseline; gate=0 ;;
    swarm)      swarm=1; mech=baseline; gate=0 ;;
    inhibition) swarm=1; mech=inhibition; gate=0 ;;
    quorum)     swarm=1; mech=quorum; gate=0 ;;
    splitgate)  swarm=1; mech=splitgate; gate=1 ;;
    *) echo "unknown arm: $arm" >&2; return 1 ;;
  esac
  local dir="$RESULTS/${task}-${arm}-s${seed}"
  rm -rf "$dir"; mkdir -p "$dir"
  cp "$HERE/tasks/$task.txt" "$dir/TASK.txt"
  # A fixture builds the inputs the task claims exist (repo, notes, images):
  # without it those cells grade nothing. `-` means the task needs only prose.
  if [ "$fixture" != "-" ]; then
    "$HERE/fixtures/$fixture" "$dir" > "$dir/fixture.log" 2>&1 || {
      echo "$task,$category,$arm,$mech,$seed,0,125,0,0,0,fixture-failed" > "$dir/row.csv"
      return
    }
  fi
  local t0 t1 wall ec cost nodes urows verdict_out
  t0=$(date +%s)
  (cd "$dir" && AFORGE_SWARM="$swarm" AFORGE_MECHANISM="$mech" AFORGE_SPLITGATE="$gate" AFORGE_MODEL="$MODEL" \
    timeout "$CELL_TIMEOUT" \
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
  echo "$task,$category,$arm,$mech,$seed,$wall,$ec,$cost,$nodes,$urows,$verdict_out" > "$dir/row.csv"
}

# Concurrent cells, arm-fair: every arm of one task fires in the same wave so
# a provider slowdown or rate-limit wave lands on all arms of the same
# comparison together and cancels out of the delta. JOBS bounds the wave;
# JOBS=1 is the timing-clean mode (no provider throughput sharing).
JOBS="${JOBS:-4}"

echo "swarm bench — tier=$TIER model=$MODEL jobs=$JOBS results=$RESULTS"
running=0
drain() {
  # Wait for one job, then flush any finished cells' rows to the CSV.
  wait -n 2>/dev/null
  for rf in "$RESULTS"/*/row.csv; do
    [ -f "$rf" ] || continue
    cat "$rf" >> "$CSV" && rm -f "$rf"
  done
}
while IFS='|' read -r task category verdict fixture rep arms; do
  case "$task" in ''|\#*) continue ;; esac
  case "$rep" in
    S) n=$N_SMALL ;; M) n=$N_MEDIUM ;; L) n=$N_LARGE ;;
  esac
  [ "$n" -eq 0 ] && continue
  # Optional 6th field: comma-separated arm list; default all four arms.
  arms="${arms:-all}"
  [ "$arms" = "all" ] && arms="$ARMS_DEFAULT"
  IFS=',' read -ra arm_list <<< "$arms"
  for seed in $(seq 1 "$n"); do
    for arm in "${arm_list[@]}"; do
      run_cell "$task" "$category" "$verdict" "$fixture" "$arm" "$seed" &
      running=$((running+1))
      while [ "$(jobs -rp | wc -l)" -ge "$JOBS" ]; do drain; done
    done
  done
done < "$TASKS"
wait
for rf in "$RESULTS"/*/row.csv; do
  [ -f "$rf" ] || continue
  cat "$rf" >> "$CSV" && rm -f "$rf"
done
echo "== done =="
