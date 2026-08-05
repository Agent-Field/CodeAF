#!/usr/bin/env bash
# Run one arm of the A/B routing experiment through the real aforge CLI.
#
#   ARM=a bench/ab-routing/run-arm.sh                 # baseline, single model
#   ARM=b bench/ab-routing/run-arm.sh                 # routed panel
#
# One cell is (task, replicate). Each cell gets a fresh workspace seeded from
# the task's seed/ directory, a full plan -> run pipeline, and a deterministic
# grade afterwards. Nothing about a cell depends on another cell except the
# ledger, which is deliberate and explained below.
#
# Read DESIGN.md before quoting any number this produces.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$HERE/../.." && pwd)"

ARM="${ARM:-a}"
# Uppercased for every path this writes: the deliverables are results-armA
# and results-armB, and taking the case from $ARM produced results-armb.jsonl.
ARM_UC="$(printf "%s" "$ARM" | tr "[:lower:]" "[:upper:]")"
TASKS="${TASKS:-t1-logstore t2-synthesis t3-shiftplan}"
REPS="${REPS:-3}"
# START_REP resumes a shared-ledger arm that was interrupted. The ledger is the
# point of that arm, so discarding the cells already run would discard the
# sequence they built; pointing RESULTS at the existing run directory keeps the
# same ledger and continues it.
START_REP="${START_REP:-1}"
RESULTS="${RESULTS:-$HERE/runs/arm$ARM_UC-$(date +%Y%m%d-%H%M%S)}"
JSONL="${JSONL:-$HERE/results-arm$ARM_UC.jsonl}"

# ── the one thing that differs between the arms ─────────────────────────────
#
# Arm A is today's configuration, untouched: one model for every call, which is
# what internal/config/config.go already defaults to. Arm B turns the router on
# over the panel selected in panel.json. If the router lands under different
# names than these, this block is the only edit the experiment needs.
AFORGE_BIN="${AFORGE_BIN:-$ROOT/bin/aforge}"
case "$ARM" in
  a)
    ARM_LABEL="single-model baseline"
    ARM_ENV=()                       # nothing set: the default model, as shipped
    # Independent replicates. Each run starts from an empty profile so run 3 is
    # a repeat of run 1 rather than a continuation of it -- the baseline has to
    # measure the model, not the harness learning about the model.
    LEDGER_MODE="${LEDGER_MODE:-fresh}"
    ;;
  b)
    ARM_LABEL="routed panel"
    # One variable, which is the whole interface: AFORGE_MODELS is either a
    # comma-separated list of slugs or a path to a JSON panel
    # (internal/router/panel.go). panel.json is written in that schema and
    # carries its own justification in keys the router's decoder ignores.
    #
    # This corrects a guess. The harness was written before the router landed
    # and assumed AFORGE_ROUTER=on plus AFORGE_PANEL; there is no AFORGE_ROUTER
    # and the panel variable is named differently. The design said this block
    # would be the only edit an arm-B run needed, and it was.
    ARM_ENV=("AFORGE_MODELS=$HERE/panel.json")
    # Shared, and the runs go in sequence. This is the learning check: the
    # ledger carries what the router learned in run 1 into run 3, and the diff
    # between the routing events of the two is the measurement.
    LEDGER_MODE="${LEDGER_MODE:-shared}"
    ;;
  *) echo "ARM must be a or b" >&2; exit 1 ;;
esac

# ── budgets and backstops ───────────────────────────────────────────────────
# Neither of these is a work limit. Both exist so that one wedged run cannot
# turn a $15 experiment into an unbounded bill, which is the failure mode
# bench/README.md records opencode hitting.
PLAN_TIMEOUT="${PLAN_TIMEOUT:-12m}"
RUN_TIMEOUT="${RUN_TIMEOUT:-30m}"
RUN_TOKEN_BUDGET="${RUN_TOKEN_BUDGET:-2000000}"   # prompt+completion, whole run
LEAF_TOKEN_BUDGET="${LEAF_TOKEN_BUDGET:-300000}"
CONCURRENCY="${CONCURRENCY:-8}"

TIMEOUT_BIN="$(command -v timeout || command -v gtimeout || true)"
[ -n "$TIMEOUT_BIN" ] || { echo "need timeout(1) — brew install coreutils" >&2; exit 1; }
[ -n "${OPENROUTER_API_KEY:-}" ] || { echo "OPENROUTER_API_KEY is required" >&2; exit 1; }

if [ ! -x "$AFORGE_BIN" ]; then
  echo "building $AFORGE_BIN"
  (cd "$ROOT" && make build) || exit 1
fi

mkdir -p "$RESULTS"
SHARED_LEDGER="$RESULTS/ledger-shared"
[ "$LEDGER_MODE" = "shared" ] && mkdir -p "$SHARED_LEDGER"

echo "arm:       $ARM ($ARM_LABEL)"
echo "tasks:     $TASKS"
echo "reps:      $REPS"
echo "ledger:    $LEDGER_MODE"
echo "results:   $RESULTS"
echo "jsonl:     $JSONL"
echo

# ── one cell ────────────────────────────────────────────────────────────────
run_cell() {
  local task="$1" rep="$2"
  local cell="$RESULTS/$task-r$rep"
  local workspace="$cell/workspace"
  mkdir -p "$workspace"

  # Fresh seed every time. Reusing a workspace leaks the previous replicate's
  # repair into the next one's starting state, which flatters whoever runs
  # second -- the same reason bench/run.sh re-clones per cell.
  if [ -d "$HERE/tasks/$task/seed" ]; then
    cp -R "$HERE/tasks/$task/seed/." "$workspace/" 2>/dev/null
  fi
  find "$workspace" -name __pycache__ -type d -exec rm -rf {} + 2>/dev/null

  local ledger
  if [ "$LEDGER_MODE" = "shared" ]; then
    ledger="$SHARED_LEDGER"
  else
    ledger="$cell/ledger"
    mkdir -p "$ledger"
  fi

  local goal
  goal="$(cat "$HERE/tasks/$task/GOAL.md")"

  # The router's event log is append-only for the whole arm, so the rows this
  # cell is responsible for are the ones after this mark. Event.Run cannot
  # substitute: it is the run cache key, derived from the goal, and the goal is
  # byte-identical across the three replicates of a task.
  local events_before=0
  if [ -f "$ledger/router-events.jsonl" ]; then
    events_before=$(wc -l < "$ledger/router-events.jsonl" | tr -d ' ')
  fi

  printf '%-14s r%-2s ' "$task" "$rep"
  local started plan_seconds run_seconds plan_code run_code
  started=$(date +%s)

  env "${ARM_ENV[@]}" AFORGE_PROFILE_DIR="$ledger" \
    "$TIMEOUT_BIN" "$PLAN_TIMEOUT" "$AFORGE_BIN" plan "$goal" --brief \
      -o "$cell/graph.json" >"$cell/plan.log" 2>&1
  plan_code=$?
  plan_seconds=$(( $(date +%s) - started ))

  if [ $plan_code -ne 0 ] || [ ! -s "$cell/graph.json" ]; then
    run_code=-1
    run_seconds=0
    echo "PLAN FAILED (exit $plan_code) in ${plan_seconds}s"
  else
    local run_started
    run_started=$(date +%s)
    env "${ARM_ENV[@]}" AFORGE_PROFILE_DIR="$ledger" \
      "$TIMEOUT_BIN" "$RUN_TIMEOUT" "$AFORGE_BIN" run "$cell/graph.json" \
        -w "$workspace" -o "$cell/done.json" \
        -j "$CONCURRENCY" -budget "$LEAF_TOKEN_BUDGET" \
        -run-budget "$RUN_TOKEN_BUDGET" >"$cell/run.log" 2>&1
    run_code=$?
    run_seconds=$(( $(date +%s) - run_started ))
  fi

  # The ledger as it stands after this cell, kept whole. For arm B this is the
  # raw material of the learning check; for arm A it is the record that each
  # replicate really did start cold.
  cp -R "$ledger" "$cell/ledger-after" 2>/dev/null

  # `aforge models` rendered against the ledger as it stands now. It reads the
  # ledger and makes no API call, so it is free, and it is the only view of the
  # ratings that shows observation counts next to them -- a rating backed by
  # three observations and one backed by three hundred are different claims.
  env "${ARM_ENV[@]}" AFORGE_PROFILE_DIR="$ledger" \
    "$AFORGE_BIN" models > "$cell/models-after.txt" 2>&1

  python3 "$HERE/collect.py" \
    --arm "$ARM" --task "$task" --rep "$rep" --cell "$cell" \
    --workspace "$workspace" --tasks-dir "$HERE/tasks" \
    --plan-seconds "$plan_seconds" --run-seconds "$run_seconds" \
    --plan-exit "$plan_code" --run-exit "$run_code" \
    --ledger-mode "$LEDGER_MODE" --events-before "$events_before" \
    >> "$JSONL" 2>"$cell/collect.err"

  python3 "$HERE/lastrow.py" "$JSONL"
}

# Cells run in sequence. Arm B needs it (the ledger has to carry forward in a
# defined order) and arm A keeps it so wall-clock and provider latency are
# measured under the same contention as arm B rather than under a quieter one.
for rep in $(seq "$START_REP" "$REPS"); do
  for task in $TASKS; do
    run_cell "$task" "$rep"
  done
done

# The ledger and the event log as they finished, at a path that is committed
# rather than under runs/ — the per-cell copies live in the gitignored results
# tree, and the arm's final learned state is a deliverable in its own right.
if [ "$LEDGER_MODE" = "shared" ]; then
  FINAL="$HERE/ledger-arm${ARM_UC}-${LEDGER_MODE}"
  rm -rf "$FINAL"
  mkdir -p "$FINAL"
  cp -R "$SHARED_LEDGER/." "$FINAL/" 2>/dev/null
  env "${ARM_ENV[@]}" AFORGE_PROFILE_DIR="$SHARED_LEDGER" \
    "$AFORGE_BIN" models > "$FINAL/models.txt" 2>&1
  echo "final ledger in $FINAL"
fi

echo
python3 "$HERE/summarize.py" "$JSONL" --arm "$ARM"
echo "raw cells in $RESULTS"
