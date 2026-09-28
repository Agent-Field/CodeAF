#!/usr/bin/env bash
# Negative control for the jsonschema task: the labelled example's shape. A
# hand-derived patch that does every non-blocker thing right but converts each
# multi-line warning only down to its first line must fail BOTH blockers and
# score 0.0 — the same result FrontierCode reports for the equivalent patch.
#
#   bench/frontiercode/negative.sh <task-id>
#
# The patch is generated inside the verifier image from the reference commit
# by the task's own make-negative.py, so the rig commits no upstream bytes.
set -uo pipefail
RIG_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$RIG_DIR/lib.sh"

[ $# -ge 1 ] || { echo "usage: negative.sh <task-id>" >&2; exit 2; }

mkdir -p "$RESULTS"
ok=1
for id in "$@"; do
  load_task "$id" || { ok=0; continue; }
  ensure_env_image && ensure_verify_image || { ok=0; log "$id: image build failed"; continue; }
  out="$RESULTS/$id-negative"
  rm -rf "$out"; mkdir -p "$out"
  python3 - "$out/meta.json" "$id" "$TASK_BASE" "$TASK_REFERENCE" "$TASK_IMAGE" "$PLATFORM" <<'PY'
import json, sys
path, task, base, ref, image, platform = sys.argv[1:]
json.dump({"task": task, "arm": "negative-control", "base_commit": base,
           "reference_commit": ref, "image": image, "platform": platform,
           "patch_source": "first-line-only"}, open(path, "w"), indent=2)
PY
  docker run --rm --platform "$PLATFORM" "$TASK_VERIFY_IMAGE" \
    cat /solution/first-line-only.patch > "$out/model.patch" 2>> "$out/docker.log" || \
    { ok=0; log "$id: could not read the negative patch from the verifier image"; continue; }
  log "$id: negative patch is $(wc -c < "$out/model.patch" | tr -d ' ') bytes"
  t0=$(date +%s)
  TASK_ID="$id" EGRESS_SCAN=0 bash "$RIG_DIR/grade.sh" "$out" || ok=0
  wall=$(( $(date +%s) - t0 ))
  read_result() {
    python3 - "$out/logs/grade/grade.json" <<'PY'
import json, sys
g = json.load(open(sys.argv[1]))
print(g.get("score"), len(g.get("failed_blockers") or []))
PY
  }
  result="$(read_result 2>/dev/null || echo 'rig -')"
  score="$(echo "$result" | cut -d' ' -f1)"
  blockers="$(echo "$result" | cut -d' ' -f2)"
  # The labelled example: 0.0, with both blockers failed.
  if [ "$score" != "0.0" ] || [ "$blockers" != "2" ]; then ok=0; fi
  printf '%-40s negative score=%-6s blockers_failed=%s %4ds  %s\n' "$id" "$score" "$blockers" "$wall" "$out"
done

if [ "$ok" = 1 ]; then
  echo; echo "negative control OK — the first-line-only patch scores 0.0 with both blockers failed"
else
  echo; echo "NEGATIVE CONTROL SUSPECT — the grader did not reproduce the labelled example"
  exit 1
fi