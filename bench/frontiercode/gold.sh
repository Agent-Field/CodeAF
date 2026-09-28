#!/usr/bin/env bash
# Positive control for the rig: grade the task's OWN reference solution. It
# must clear every blocker and score 1.00 by construction — the reference
# patch is the change the rubric was written against. A 0 means the grading
# path is broken rather than the model, which is worth knowing BEFORE spending
# a single provider token. Costs nothing but CPU.
#
#   bench/frontiercode/gold.sh <task-id>
#
# The reference patch is fetched from the verifier image (the only place it
# exists — the environment image the agent runs in never carries it, and the
# rig commits no upstream source bytes).
set -uo pipefail
RIG_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$RIG_DIR/lib.sh"

[ $# -ge 1 ] || { echo "usage: gold.sh <task-id> [<task-id> ...]" >&2; exit 2; }

mkdir -p "$RESULTS"
ok=1
for id in "$@"; do
  load_task "$id" || { ok=0; continue; }
  ensure_env_image && ensure_verify_image || { ok=0; log "$id: image build failed"; continue; }
  out="$RESULTS/$id-gold"
  rm -rf "$out"; mkdir -p "$out"
  python3 - "$out/meta.json" "$id" "$TASK_BASE" "$TASK_REFERENCE" "$TASK_IMAGE" "$PLATFORM" <<'PY'
import json, sys
path, task, base, ref, image, platform = sys.argv[1:]
json.dump({"task": task, "arm": "gold-control", "base_commit": base,
           "reference_commit": ref, "image": image, "platform": platform,
           "patch_source": "reference"}, open(path, "w"), indent=2)
PY
  # The reference patch comes out of the verifier image, which fetched it from
  # the pinned upstream commit at build time.
  docker run --rm --platform "$PLATFORM" "$TASK_VERIFY_IMAGE" \
    cat /solution/upstream-reference.patch > "$out/model.patch" 2>> "$out/docker.log" || \
    { ok=0; log "$id: could not read the reference patch from the verifier image"; continue; }
  log "$id: gold patch is $(wc -c < "$out/model.patch" | tr -d ' ') bytes"
  t0=$(date +%s)
  TASK_ID="$id" EGRESS_SCAN=0 bash "$RIG_DIR/grade.sh" "$out" || ok=0
  wall=$(( $(date +%s) - t0 ))
  grade="$(python3 -c "import json;print(json.load(open('$out/logs/grade/grade.json')).get('score'))" 2>/dev/null || echo rig)"
  [ "$grade" = "1.0" ] || [ "$grade" = "1" ] || ok=0
  printf '%-40s gold score=%-6s %4ds  %s\n' "$id" "$grade" "$wall" "$out"
done

if [ "$ok" = 1 ]; then
  echo; echo "grading path OK — the rig scores the reference solution 1.00"
else
  echo; echo "GRADING PATH SUSPECT — fix the rig before spending model tokens"
  exit 1
fi