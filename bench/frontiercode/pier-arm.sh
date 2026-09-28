#!/usr/bin/env bash
# The baseline arm (D3): mini-swe-agent through Pier, on the SAME fixture,
# the SAME model, graded by the SAME rubric and grader as the harness arm —
# the comparison the whole rig exists for.
#
#   bench/frontiercode/pier-arm.sh <task-id> <model-id> <seed-tag> [pier-agent]
#
# The arm runs Pier's way: Pier builds the task's environment image, installs
# the agent in it, runs the brief, and (with --no-delete) leaves the trial's
# container for the rig to collect the graded diff from. Egress is PIER'S
# posture — its own squid allowlist for the model endpoint — which is
# STRICTER than FrontierCode's open internet; recorded as a deviation in the
# row, because the official baseline rows are produced by this same harness
# posture.
#
# Env:
#   PIER_AGENT    mini-swe-agent (default) or codex
#   RESULTS       output root (default bench/frontiercode/results)
set -uo pipefail
RIG_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$RIG_DIR/lib.sh"

[ $# -ge 3 ] || { echo "usage: pier-arm.sh <task-id> <model-id> <seed-tag> [pier-agent]" >&2; exit 2; }
TASK="$1"; MODEL="$2"; SEED="$3"; PIER_AGENT="${4:-${PIER_AGENT:-mini-swe-agent}}"
SLUG="$(printf '%s' "$MODEL" | tr '/:' '--')"
OUT="$RESULTS/$TASK-$SLUG-$SEED"
mkdir -p "$OUT" "$RESULTS"

load_key || { echo "pier-arm.sh: no provider key" >&2; exit 2; }
load_task "$TASK" || exit 1

# The linux binary the container needs is Pier's business; ours is the task
# environment, which must exist for Pier to build from.
ensure_env_image || { log "$TASK: environment image build failed"; exit 1; }
# Pier builds the image itself from environment/Dockerfile; the ensure call
# above warms the apt layers so Pier's own build is quick.

meta() { python3 - "$OUT/meta.json" "$@" <<'PY'
import json, os, sys
path = sys.argv[1]
d = json.load(open(path)) if os.path.exists(path) else {}
for kv in sys.argv[2:]:
    k, _, v = kv.partition("=")
    try:
        v = json.loads(v)
    except Exception:
        pass
    d[k] = v
json.dump(d, open(path, "w"), indent=2)
PY
}
meta "task=$TASK" "arm=$PIER_AGENT-pier" "model=$MODEL" "variant=pier-default" "seed=$SEED" \
     "language=$TASK_LANG" "image=$TASK_IMAGE" "base_commit=$TASK_BASE" "reference_commit=$TASK_REFERENCE" \
     "platform=$PLATFORM" "rig_rev=${BENCH_RIG_REV:-unknown}" "egress=pier-squid-allowlist" "stage=start"
python3 "$RIG_DIR/grade/record.py" start --run-dir "$OUT" \
  --json "{\"task\":\"$TASK\",\"arm\":\"$PIER_AGENT-pier\",\"model\":\"$MODEL\",\"variant\":\"pier-default\",\"seed\":\"$SEED\",\"base_commit\":\"$TASK_BASE\"}"

log "$TASK: pier run — agent $PIER_AGENT, model openrouter/$MODEL"
t0=$(date +%s)
export OPENROUTER_API_KEY="$KEY"
pier run \
  -p "$TASK_DIR" \
  -a "$PIER_AGENT" \
  -m "openrouter/$MODEL" \
  -k 1 -n 1 \
  --disable-verification \
  --no-delete \
  -o "$OUT/pier-jobs" \
  --yes > "$OUT/pier-run.log" 2>&1
PIER_CODE=$?
WALL=$(( $(date +%s) - t0 ))
meta "exit_code=$PIER_CODE" "agent_seconds=$WALL" "stage=collect"
log "$TASK: pier exited $PIER_CODE after ${WALL}s (see $OUT/pier-run.log)"

# ── collect the graded diff ──────────────────────────────────────────────────
# Pier keeps the trial's container (--no-delete); the patch is the diff of
# every place the work could have landed, the same discipline the harness
# arm's collect applies. The container name is read out of Pier's own job
# records rather than guessed.
CONTAINER="$(python3 - "$OUT/pier-jobs" <<'PY'
import json, pathlib, sys
root = pathlib.Path(sys.argv[1])
for path in sorted(root.rglob("*.json")):
    try:
        d = json.loads(path.read_text())
    except Exception:
        continue
    for key in ("container_name", "container_id", "container"):
        v = d.get(key) if isinstance(d, dict) else None
        if isinstance(v, str) and v:
            print(v); sys.exit(0)
PY
)"
if [ -n "$CONTAINER" ]; then
  docker exec "$CONTAINER" bash -c 'cd $(git -C /home/agent/repos/jsonschema rev-parse --show-toplevel 2>/dev/null || echo /home/agent/repos/jsonschema) && git add -A && git diff --binary '"$TASK_BASE" > "$OUT/model.patch" 2>> "$OUT/docker.log" || true
fi
if [ ! -s "$OUT/model.patch" ]; then
  # Fall back to every patch-shaped file the job directory holds.
  find "$OUT/pier-jobs" -name '*.patch' -size +0 2>/dev/null | head -1 | xargs -I{} cp {} "$OUT/model.patch" 2>/dev/null || true
fi
if [ -s "$OUT/model.patch" ]; then
  meta "patch_source=$CONTAINER" "patch_bytes=$(wc -c < "$OUT/model.patch" | tr -d ' ')" "stage=grade"
else
  meta "patch_source=none" "stage=collect-failed"
  log "$TASK: no patch collected from the Pier trial — see $OUT/pier-run.log"
fi

# ── grade by the same rubric ─────────────────────────────────────────────────
if [ "${SKIP_GRADE:-0}" = 1 ] || [ ! -s "$OUT/model.patch" ]; then
  python3 "$RIG_DIR/grade/report.py" "$OUT"
  exit 0
fi
EGRESS_SCAN=0 TASK_ID="$TASK" RESULTS="$RESULTS" bash "$RIG_DIR/grade.sh" "$OUT" || log "$TASK: grading failed"
python3 "$RIG_DIR/grade/report.py" "$OUT"