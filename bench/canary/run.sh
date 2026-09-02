#!/usr/bin/env bash
# The canary: a fixed pool of real GitHub issues, run through both doors on one
# build, graded by the fix pull request's own tests, and written down.
#
# It exists to answer one question on a schedule — can this build still finish
# an ordinary issue a person would paste — so the pool is stable, the model is
# pinned, and a change in the answer is a change in the product. It spends real
# money and is ON DEMAND; nothing in `make check` reaches it.
#
# Usage:
#   bench/canary/run.sh --bin ~/af-dev/bin/aforge                 anchors, both doors
#   bench/canary/run.sh --bin BIN --fresh 2                       plus two fresh picks
#   bench/canary/run.sh --bin BIN --doors do                      one door
#   bench/canary/run.sh --bin BIN --baseline RUN/rows.csv --post 380
#   bench/canary/run.sh --dry-run                                 list the cells, run none
set -uo pipefail

CANARY_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$CANARY_ROOT/../.." && pwd)"

BIN="${AFORGE_BIN:-$REPO_ROOT/bin/aforge}"
POOL="$CANARY_ROOT/pool.json"
DOORS="do,chat"
FRESH=0
JOBS=6
# The pin. Both doors, every cell, one concrete model id — never an alias that
# can float, because a pool that is stable under a model that is not measures
# the model's drift and calls it the product's.
MODEL="${CANARY_MODEL:-deepseek/deepseek-v4-flash}"
CAP="1.00"
WALL=900
RESULTS="${CANARY_RESULTS:-$REPO_ROOT/bench-results/canary}"
CACHE="${CANARY_CACHE:-$HOME/.cache/aforge-canary}"
BASELINE=""
POST=""
DRY=0
LABEL=""

while [ $# -gt 0 ]; do
  case "$1" in
    --bin)      BIN="$2"; shift 2 ;;
    --pool)     POOL="$2"; shift 2 ;;
    --doors)    DOORS="$2"; shift 2 ;;
    --fresh)    FRESH="$2"; shift 2 ;;
    --jobs)     JOBS="$2"; shift 2 ;;
    --model)    MODEL="$2"; shift 2 ;;
    --cap)      CAP="$2"; shift 2 ;;
    --wall)     WALL="$2"; shift 2 ;;
    --results)  RESULTS="$2"; shift 2 ;;
    --baseline) BASELINE="$2"; shift 2 ;;
    --post)     POST="$2"; shift 2 ;;
    --label)    LABEL="$2"; shift 2 ;;
    --dry-run)  DRY=1; shift ;;
    -h|--help)  sed -n '2,17p' "$0"; exit 0 ;;
    *) echo "unknown argument: $1" >&2; exit 1 ;;
  esac
done

for tool in tmux git python3 timeout flock gh; do
  command -v "$tool" >/dev/null || { echo "need $tool" >&2; exit 1; }
done
[ -x "$BIN" ] || { echo "no aforge binary at $BIN — build one or pass --bin" >&2; exit 1; }
[ -f "$POOL" ] || { echo "no pool at $POOL — bench/canary/pick.py --anchors 3 writes one" >&2; exit 1; }

SHA="$("$BIN" version 2>/dev/null | awk '{print $2}')"
RUN="$(date -u +%Y%m%dT%H%M%SZ)-${SHA:-unknown}${LABEL:+-$LABEL}"
RUN_DIR="$RESULTS/$RUN"
mkdir -p "$RUN_DIR/cells" "$CACHE"

# Fresh picks are drawn before anything spends, and a picker that cannot
# deliver is a note on the run rather than a reason not to run the anchors.
FRESH_FILE=""
if [ "$FRESH" -gt 0 ] && [ "$DRY" = 0 ]; then
  FRESH_FILE="$RUN_DIR/fresh.json"
  python3 "$CANARY_ROOT/pick.py" --fresh "$FRESH" --exclude "$POOL" --out "$FRESH_FILE" >"$RUN_DIR/pick.log" 2>&1 \
    || { echo "picker delivered nothing fresh — see $RUN_DIR/pick.log" >&2; FRESH_FILE=""; }
fi

# One entry file per cell: the pool entry with the door written in.
CANARY_POOL="$POOL" CANARY_FRESH="$FRESH_FILE" CANARY_DOORS="$DOORS" CANARY_CELLS="$RUN_DIR/cells" python3 - <<'PY'
import json, os
pool = json.load(open(os.environ["CANARY_POOL"]))
entries = [dict(e, anchor=True) for e in pool["anchors"]]
fresh = os.environ["CANARY_FRESH"]
if fresh:
    entries += [dict(e, anchor=False) for e in json.load(open(fresh)).get("picks", [])]
for entry in entries:
    for door in os.environ["CANARY_DOORS"].split(","):
        name = "%s-%s" % (entry["id"], door)
        os.makedirs(os.path.join(os.environ["CANARY_CELLS"], name), exist_ok=True)
        json.dump(dict(entry, door=door), open(os.path.join(os.environ["CANARY_CELLS"], name, "entry.json"), "w"), indent=1)
    print("  %-40s %s" % (entry["id"], "anchor" if entry["anchor"] else "fresh"))
PY

cat > "$RUN_DIR/run.json" <<JSON
{"run": "$RUN", "sha": "$SHA", "bin": "$BIN", "model": "$MODEL", "doors": "$DOORS", "wall": $WALL, "cap": $CAP,
 "pool": "$POOL", "fresh": ${FRESH}, "started": "$(date -u +%Y-%m-%dT%H:%M:%SZ)"}
JSON

echo "canary:  $RUN"
echo "binary:  $BIN ($SHA)"
echo "model:   $MODEL · wall ${WALL}s · cap \$$CAP · $JOBS at a time"
echo "cells:   $RUN_DIR/cells"
[ "$DRY" = 1 ] && { echo "dry run — nothing launched, nothing spent"; exit 0; }

# THE RUN EXECUTES A FROZEN COPY OF THE RIG. bash reads a script as it goes,
# so a lib file edited while a cell is in flight is read mid-script at a moved
# offset — a cell lost its grade to exactly that. The copy also makes the run
# its own evidence: the code that produced these rows is beside them.
cp -R "$CANARY_ROOT/lib" "$RUN_DIR/rig"
export CANARY_BIN="$BIN" CANARY_MODEL="$MODEL" CANARY_CAP="$CAP" CANARY_WALL="$WALL" CANARY_CACHE="$CACHE"
ls "$RUN_DIR/cells" | xargs -P "$JOBS" -I{} sh -c \
  '"$0" "$1/{}/entry.json" "$1/{}" > "$1/{}/cell.log" 2>&1; cat "$1/{}/cell.log" | tail -1' \
  "$RUN_DIR/rig/cell.sh" "$RUN_DIR/cells"

echo
python3 "$CANARY_ROOT/lib/report.py" "$RUN_DIR" ${BASELINE:+--baseline "$BASELINE"} ${POST:+--post "$POST"}
echo "evidence: $RUN_DIR"
