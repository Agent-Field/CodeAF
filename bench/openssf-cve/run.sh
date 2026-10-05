#!/usr/bin/env bash
# Run one tool over the row set, judge every cell, and score the run.
#
#   bench/openssf-cve/run.sh <tool> <model> <seed-tag> [<cve> ...]
#
#   tool    a driver under tools/: gold, null, codeaf, sec-af, replay
#   model   the model the tool runs on (recorded; what codeaf is pinned to;
#           `-` for a tool that has no model, such as gold or replay)
#   seed    any tag that keeps two runs of the same thing apart
#   cve...  restrict the run to these CVEs (both variants); default is the SET
#
# Env:
#   MODE        diff (default) or repo — "Two shapes" in README.md
#   SET         deepsource (default), all, or a row file — lib.sh
#   SAMPLE      run only N rows, chosen by a shuffle seeded from the seed tag
#   JOBS        cells running at once (default 2)
#   TIMEOUT     seconds a tool gets per cell (default 900)
#   RESUME=1    keep an existing result directory and skip every cell that is
#               already past its tool stage — a campaign stopped by a wall or a
#               closed lid finishes from where it was
#   NO_JUDGE=1  stop after the tools have run; judge later with judge.py
#   JUDGE_MODEL the judge (default anthropic/claude-opus-4.5, DeepSource's)
#   CODEAF_BIN  the binary the codeaf driver runs (default bin/codeaf)
#   BENCH_ROOT  where work/ and results/ live (default ~/bench-openssf-cve;
#               never inside the checkout — lib.sh says why)
#
# Writes $RESULTS/<tool>-<mode>-<model>-<seed>/{meta.json,rows.tsv,rig/,
# <CVE>/<variant>/…,summary.json}. The rig is copied into the result directory
# first and the run re-executes from the copy, so editing a script while a run
# is in flight cannot change the run (bash reads scripts incrementally; the
# DeepSWE rig lost two runs to that before it adopted the same snapshot).
set -uo pipefail

__RIG_SRC="$(cd "$(dirname "$0")" && pwd)"
if [ -z "${RIG_SNAPSHOT:-}" ]; then
  [ $# -ge 3 ] || { echo "usage: run.sh <tool> <model> <seed-tag> [<cve> ...]" >&2; exit 2; }
  __mode="${MODE:-diff}"
  __slug="$(printf '%s' "$2" | tr '/:' '--')"; [ "$__slug" = "-" ] && __slug=nomodel
  export RESULTS="${RESULTS:-${BENCH_ROOT:-$HOME/bench-openssf-cve}/results}"
  __out="$RESULTS/$1-$__mode-$__slug-$3"
  if [ "${RESUME:-0}" = 1 ] && [ -d "$__out" ]; then
    rm -rf "$__out/rig"
  else
    rm -rf "$__out"
  fi
  mkdir -p "$__out/rig/tools"
  cp "$__RIG_SRC"/*.sh "$__RIG_SRC"/*.py "$__out/rig/"
  cp "$__RIG_SRC"/tools/*.sh "$__out/rig/tools/"
  cp -R "$__RIG_SRC/sets" "$__RIG_SRC/comparison" "$__out/rig/"
  export RIG_SNAPSHOT="$__out/rig" RIG_SRC="$__RIG_SRC"
  export RIG_REV="$(git -C "$__RIG_SRC" rev-parse --short HEAD 2>/dev/null || echo unknown)"
  export WORK="${WORK:-${BENCH_ROOT:-$HOME/bench-openssf-cve}/work}"
  exec bash "$__out/rig/run.sh" "$@"
fi

source "$RIG_SNAPSHOT/lib.sh"
TOOL="$1"; MODEL="$2"; SEED="$3"; shift 3
MODE="${MODE:-diff}"
case "$MODE" in diff|repo) ;; *) echo "run.sh: MODE must be diff or repo, not $MODE" >&2; exit 2 ;; esac
[ -f "$RIG_SNAPSHOT/tools/$TOOL.sh" ] || { echo "run.sh: no driver tools/$TOOL.sh" >&2; exit 2; }
TIMEOUT="${TIMEOUT:-900}"; JOBS="${JOBS:-2}"
SLUG="$(printf '%s' "$MODEL" | tr '/:' '--')"; [ "$SLUG" = "-" ] && SLUG=nomodel
OUT="$RESULTS/$TOOL-$MODE-$SLUG-$SEED"
[ -d "$DATASET/CVEs" ] || { log "no dataset at $DATASET — run fetch.sh first"; exit 1; }

# The judge and the codeaf driver both talk to OpenRouter; the oracle, the
# floor and the replay do not, and a run of those must work on a box with no
# key at all when NO_JUDGE is set.
NEEDS_KEY=0
case "$TOOL" in codeaf|sec-af) NEEDS_KEY=1 ;; esac
[ "${NO_JUDGE:-0}" = 1 ] || NEEDS_KEY=1
if [ "$NEEDS_KEY" = 1 ]; then resolve_key || exit 2; fi

# --- the rows ------------------------------------------------------------------
if [ $# -gt 0 ]; then
  for cve in "$@"; do printf '%s\tunfixed\n%s\tfixed\n' "$cve" "$cve"; done > "$OUT/rows.tsv"
else
  set_rows > "$OUT/rows.tsv" || exit 2
fi
if [ -n "${SAMPLE:-}" ]; then
  python3 - "$OUT/rows.tsv" "$SAMPLE" "$SEED" <<'PY'
import random, sys
path, n, seed = sys.argv[1], int(sys.argv[2]), sys.argv[3]
rows = [l for l in open(path) if l.strip()]
random.Random(seed).shuffle(rows)
open(path, "w").write("".join(sorted(rows[:n])))
PY
fi
N="$(wc -l < "$OUT/rows.tsv" | tr -d ' ')"

BIN_SHA=""
if [ "$TOOL" = codeaf ]; then
  CODEAF_BIN="${CODEAF_BIN:-$(cd "${RIG_SRC:-$RIG_DIR}/../.." && pwd)/bin/codeaf}"
  [ -x "$CODEAF_BIN" ] || { log "no codeaf binary at $CODEAF_BIN — make build, or set CODEAF_BIN"; exit 2; }
  export CODEAF_BIN
  BIN_SHA="$(shasum -a 256 "$CODEAF_BIN" | cut -c1-16)"
fi
meta "$OUT/meta.json" "tool=$TOOL" "model=$MODEL" "seed=$SEED" "mode=$MODE" "set=${SET:-deepsource}" \
  "rows=$N" "timeout_seconds=$TIMEOUT" "jobs=$JOBS" "judge_model=$JUDGE_MODEL" \
  "dataset_rev=$(cat "$WORK/dataset.rev" 2>/dev/null || echo unknown)" "rig_rev=${RIG_REV:-unknown}" \
  "bin_sha256_16=$BIN_SHA" "host=$(uname -sm)" "started=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  "replay_tool=${REPLAY_TOOL:-}" "work=$WORK" "resumed=${RESUME:-0}" "stage=prepare"

# --- checkouts -----------------------------------------------------------------
cut -f1 "$OUT/rows.tsv" | sort -u | while read -r cve; do
  [ -s "$WORK/repos/$cve/unfixed/info.json" ] && [ -s "$WORK/repos/$cve/fixed/info.json" ] && continue
  [ -f "$WORK/repos/$cve/unavailable.txt" ] && continue
  bash "$RIG_SNAPSHOT/prepare.sh" "$cve"
done

# --- the cells -----------------------------------------------------------------
export OUT TOOL MODEL MODE TIMEOUT WORK RESULTS
log "$TOOL: $N rows, mode $MODE, $JOBS at a time, ${TIMEOUT}s each"
meta "$OUT/meta.json" "stage=tool"
t0=$(date +%s)
xargs -P "$JOBS" -L 1 bash "$RIG_SNAPSHOT/cell.sh" < "$OUT/rows.tsv"
meta "$OUT/meta.json" "tool_wall_seconds=$(( $(date +%s) - t0 ))"

# Without the judge, the cells that need no verdict call — the empty reports —
# are still decided, so a model-free run such as null scores completely and a
# real run shows how much of it is waiting on the judge.
if [ "${NO_JUDGE:-0}" = 1 ]; then
  python3 "$RIG_SNAPSHOT/judge.py" "$OUT" --offline
  meta "$OUT/meta.json" "stage=unjudged"; log "$TOOL: NO_JUDGE set — judge later with judge.py $OUT"
  python3 "$RIG_SNAPSHOT/score.py" "$OUT"
  exit 0
fi

# --- judge and score ------------------------------------------------------------
meta "$OUT/meta.json" "stage=judge"
python3 "$RIG_SNAPSHOT/judge.py" "$OUT" --model "$JUDGE_MODEL" --jobs "$JOBS" || meta "$OUT/meta.json" "judge_error=true"
meta "$OUT/meta.json" "stage=done" "finished=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
python3 "$RIG_SNAPSHOT/score.py" "$OUT"
