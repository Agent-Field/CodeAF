#!/usr/bin/env bash
# Positive control for the rig: grade each task's OWN reference solution.
# It must score 1 by construction — the reference patch is the fix the hidden
# tests were written against. A 0 means the grading path is broken rather than
# the model, which is worth knowing BEFORE spending a single provider token.
# Costs nothing but CPU.
#
#   bench/deepswe/gold.sh <task-id> [<task-id> ...]
set -uo pipefail
source "$(cd "$(dirname "$0")" && pwd)/lib.sh"

[ $# -ge 1 ] || { echo "usage: gold.sh <task-id> [<task-id> ...]" >&2; exit 2; }

ok=1
for id in "$@"; do
  load_task "$id" || { ok=0; continue; }
  out="$RESULTS/$id-gold"
  mkdir -p "$out"
  gold="$TASK_DIR/solution/solution.patch"
  [ -f "$gold" ] || { log "$id: no reference solution — skipped"; continue; }
  cp "$gold" "$out/model.patch"
  t0=$(date +%s)
  if grade_patch "$out"; then
    reward=$(python3 -c "import json,sys;print(json.load(open(sys.argv[1]))['reward'])" "$out/reward.json")
  else
    reward="RIG-ERROR"
  fi
  wall=$(( $(date +%s) - t0 ))
  [ "$reward" = "1" ] || ok=0
  printf '%-52s gold reward=%-9s %4ds  %s\n' "$id" "$reward" "$wall" "$out"
done

if [ "$ok" = 1 ]; then
  echo; echo "grading path OK — the rig scores the reference solutions correctly"
else
  echo; echo "GRADING PATH SUSPECT — fix the rig before spending model tokens"
  exit 1
fi
