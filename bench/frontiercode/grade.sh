#!/usr/bin/env bash
# Grade one run directory: whatever model.patch sits in it, under the task's
# rubric, the whole grader in a no-network verifier container with the LLM
# judge called from the host — the only exception to --network none.
#
#   bench/frontiercode/grade.sh <result-dir>
#
# Env: TASK_ID (default: read from the result dir's meta.json), EGRESS_SCAN=1
# for runs that had a live agent (the controls pass --no-egress), FORCE_IMAGE=1
# to rebuild the verifier image.
#
# The phases, in order — each one's output written even when it fails, because
# the question a failed grade has to answer is which layer broke:
#   logs/grade/phaseA.json   in-container verdicts (command, classical,
#                            reverse-classical, scope) + judge evidence
#   logs/grade/judge.json    the host judge's verdicts (prompt criteria)
#   logs/grade/phaseB.json   the adaptive path's rerun, when it ran
#   grade.json               the combined grade: score, blockers, per criterion
#   scan.json                the egress scan (agent runs and sealed fixtures)
#   record.jsonl final row + DONE + artifacts.sha256
set -uo pipefail
RIG_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$RIG_DIR/lib.sh"

OUT="$1"
[ -d "$OUT" ] || { log "no such result dir: $OUT"; exit 2; }
OUT="$(cd "$OUT" && pwd)"

META_JSON="${TASK_ID:-$(python3 -c "import json,sys;print(json.load(open(sys.argv[1])).get('task',''))" "$OUT/meta.json" 2>/dev/null)}"
[ -n "$META_JSON" ] || { log "grade.sh: no task in meta.json and no TASK_ID"; exit 2; }
load_task "$META_JSON" || exit 1

[ -s "$OUT/model.patch" ] || {
  log "$TASK_ID: empty or missing model.patch — a rig failure, recorded as rig, never 0"
  python3 "$RIG_DIR/grade/record.py" row --run-dir "$OUT" \
    --json "{\"event\":\"grade\",\"task\":\"$TASK_ID\",\"grade_status\":\"rig\",\"notes\":\"no model.patch to grade\"}"
  exit 1
}

EGRESS_SCAN="${EGRESS_SCAN:-0}"
mkdir -p "$OUT/logs/artifacts" "$OUT/logs/grade"
cp "$OUT/model.patch" "$OUT/logs/artifacts/model.patch"
echo "$TASK_DIR" > "$OUT/logs/grade/task_dir.txt"

python3 "$RIG_DIR/grade/record.py" row --run-dir "$OUT" \
  --json "{\"event\":\"grade\",\"task\":\"$TASK_ID\",\"stage\":\"phase-a\"}"

ensure_verify_image || {
  log "$TASK_ID: verifier image build failed — see $RESULTS/.build-$TASK_ID-verify.log"
  exit 1
}


# ── phase helper: bind-mount run, docker-cp fallback ────────────────────────
# The verifier image needs /rig (the grader), /task (the task dir) and
# /logs/artifacts/model.patch. On a normal workstation those are bind mounts.
# On a checkout the Docker VM cannot read through (a synthesized/lazy tree —
# observed on this Mac's session copies: mounts resolve names but reads fail
# with I/O errors), fall back to docker cp: the phases' inputs are copied into
# a stopped container and the outputs copied back out. Same image, same
# --network none, same verdicts.
run_in_verify() { # <phase> <script...>
  local phase="$1"; shift
  local cid="fc-grade-$$-$phase"
  docker rm -f "$cid" >/dev/null 2>&1
  docker create --platform "$PLATFORM" --network none --name "$cid" \
    --cpus "$TASK_CPUS" --memory "${TASK_MEM}m" "${EMU_ARGS[@]+${EMU_ARGS[@]}}" \
    -e FC_PATCH=/logs/artifacts/model.patch \
    "$TASK_VERIFY_IMAGE" sleep 3600 >/dev/null || return 99
  # Stream the inputs as a tar over stdin: the docker daemon's own reads of
  # this tree can hit the same lazy-materialization I/O errors that broke the
  # bind mounts, but a host-side tar cannot.
  docker start "$cid" >/dev/null || { docker rm -f "$cid" >/dev/null; return 99; }
  docker exec "$cid" mkdir -p /rig /task /logs/artifacts /logs/grade
  tar --no-xattrs -C "$RIG_DIR" -cf - . \
    | docker exec -i "$cid" tar -xf - -C /rig \
    || { docker rm -f "$cid" >/dev/null; return 99; }
  tar --no-xattrs -C "$TASK_DIR" -cf - . \
    | docker exec -i "$cid" tar -xf - -C /task \
    || { docker rm -f "$cid" >/dev/null; return 99; }
  tar --no-xattrs -C "$OUT/logs/artifacts" -cf - model.patch \
    | docker exec -i "$cid" tar -xf - -C /logs/artifacts \
    || { docker rm -f "$cid" >/dev/null; return 99; }
  # The host's grade directory travels in: phase A writes phaseA.json into it,
  # and phase B needs the judge's adapted-tests.patch, which lives there. A
  # fresh container otherwise has no /logs/grade at all, which is what made
  # phase B die with FileNotFoundError instead of reporting a verdict.
  if [ -d "$OUT/logs/grade" ]; then
    tar --no-xattrs -C "$OUT/logs/grade" -cf - . \
      | docker exec -i "$cid" tar -xf - -C /logs/grade \
      || { docker rm -f "$cid" >/dev/null; return 99; }
  fi
  timeout "$((TASK_VSECS + 120))" docker exec "$cid" "$@"
  local code=$?
  docker exec "$cid" tar -cf - -C /logs grade 2>/dev/null \
    | tar -xf - -C "$OUT/logs" 2>/dev/null
  docker rm -f "$cid" >/dev/null 2>&1
  return $code
}

# ── phase A: in-container verdicts ──────────────────────────────────────────
emu_args
log "$TASK_ID: grading phase A (limit ${TASK_VSECS}s, network none)"
run_in_verify phase-a \
  python3 /rig/grade/rubric.py phase-a --task /task \
    --repo "/root/repos/$TASK_AGENT_REPO" --base "$TASK_BASE" --out /logs/grade \
  > "$OUT/logs/grade/phase-a.out" 2>&1
PHASE_A_CODE=$?
if [ ! -f "$OUT/logs/grade/phaseA.json" ]; then
  log "$TASK_ID: phase A produced no verdicts (exit $PHASE_A_CODE) — rig"
  python3 "$RIG_DIR/grade/record.py" row --run-dir "$OUT" \
    --json "{\"event\":\"grade\",\"task\":\"$TASK_ID\",\"grade_status\":\"rig\",\"notes\":\"phase A produced no phaseA.json (exit $PHASE_A_CODE)\"}"
  exit 1
fi

# ── the judge: prompt criteria, from the host ───────────────────────────────
# REGRADE=1 reuses the run's retained judge.json (and adapted-tests.patch) and
# rewrites only the deterministic phases and the combined grade: no judge call,
# no scan, no final row. It is how a retained patch is regraded under a fixed
# grader without spending on the model or touching the original evidence.
REGRADE="${REGRADE:-0}"
if [ "$REGRADE" != 1 ]; then
  log "$TASK_ID: judge — prompt criteria"
  python3 "$RIG_DIR/grade/judge.py" review --task "$TASK_DIR" --grade-dir "$OUT/logs/grade" \
    > "$OUT/logs/grade/judge-review.out" 2>&1 || log "$TASK_ID: judge review failed — see judge-review.out"
fi

# ── the adaptive path: only when the verbatim tests did not fit ─────────────
# Adapt when the verbatim reference tests did not fit: an overlay conflict,
# or a failed criterion that is classical or adaptive-classical in the task's
# own rubric (the fixture pinned the id; candidates name their own).
NEEDS_ADAPT=$(python3 - "$OUT/logs/grade/phaseA.json" "$TASK_DIR/rubric.toml" <<'PY'
import json, sys, tomllib
pa = json.load(open(sys.argv[1]))
# A patch that did not apply is a legitimate 0: the tests never ran, so there
# is nothing for the judge to adapt. Adapting here would spend a judge call on
# a run the grader already answered.
if not pa.get("apply_ok", True):
    print("0"); raise SystemExit
for e in pa["criteria"].values():
    if e.get("note", "").startswith("test overlay conflict"):
        print("1"); break
else:
    rubric = tomllib.load(open(sys.argv[2], "rb"))
    testable = {c["id"] for c in rubric.get("criterion", [])
                if c.get("kind") in ("classical", "adaptive-classical")}
    adapt = any(e.get("status") == "fail" and cid in testable
                for cid, e in pa["criteria"].items())
    print("1" if adapt else "0")
PY
)
if [ "$NEEDS_ADAPT" = 1 ]; then
  log "$TASK_ID: judge — adaptive test adaptation"
  python3 "$RIG_DIR/grade/judge.py" adapt --task "$TASK_DIR" --grade-dir "$OUT/logs/grade" \
    > "$OUT/logs/grade/judge-adapt.out" 2>&1 || log "$TASK_ID: judge adapt failed — see judge-adapt.out"
  if [ -s "$OUT/logs/grade/adapted-tests.patch" ]; then
    log "$TASK_ID: phase B — rerunning adapted tests"
    run_in_verify phase-b \
      python3 /rig/grade/rubric.py phase-b --task /task \
        --repo "/root/repos/$TASK_AGENT_REPO" --base "$TASK_BASE" --out /logs/grade \
      > "$OUT/logs/grade/phase-b.out" 2>&1 || log "$TASK_ID: phase B failed — see phase-b.out"
  fi
fi

# ── combine, scan, record ───────────────────────────────────────────────────
python3 "$RIG_DIR/grade/rubric.py" combine --task "$TASK_DIR" --grade-dir "$OUT/logs/grade" \
  > "$OUT/logs/grade/combine.out" 2>&1 || log "$TASK_ID: combine failed — see combine.out"

if [ "$EGRESS_SCAN" = 1 ]; then
  python3 "$RIG_DIR/grade/scanner.py" --run-dir "$OUT" \
    --repository-url "$TASK_REPO_URL" > "$OUT/logs/grade/scan.out" 2>&1 || \
    log "$TASK_ID: scanner failed — see scan.out"
else
  python3 - "$OUT" <<'PY'
import json, sys, pathlib
out = sys.argv[1]
pathlib.Path(out, "scan.json").write_text(json.dumps({
    "flagged": None, "reasons": [], "soft": [],
    "counts": {}, "note": "control: no agent egress to scan"}, indent=2))
PY
fi

python3 "$RIG_DIR/grade/finalize.py" --run-dir "$OUT" --task-dir "$TASK_DIR" \
  > "$OUT/logs/grade/finalize.out" 2>&1 || log "$TASK_ID: finalize failed — see finalize.out"

SCORE=$(python3 -c "import json;g=json.load(open('$OUT/logs/grade/grade.json'));print(g.get('score'))" 2>/dev/null || echo rig)
log "$TASK_ID: graded — score=$SCORE (see $OUT/logs/grade/grade.json)"