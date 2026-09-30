#!/usr/bin/env bash
# Smoke reproduction for the adaptive (phase B) grading path, on the retained
# conflicted-files-refname-crash s1 patch. It forces a genuine overlay conflict
# in phase A and a retained adapted-tests.patch, then proves the fixed chain:
# phase B runs in a fresh no-network container, applies the adapted patch, runs
# the tests, and combine answers the conflicted classical criterion from the
# phase-B verdict.
#
# It uses the retained evidence and the task's verifier image; it makes no
# judge call and no provider spend, and it leaves evidence/ untouched.
#
#   bash bench/frontiercode/tests/phase-b-smoke.sh
#
# SKIPS (exit 0) when docker or the verifier image is absent, so a machine that
# has not built the task image is not failed for it.
set -euo pipefail
RIG_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TASK=conflicted-files-refname-crash
IMAGE="frontiercode-$TASK:verify"
SRC="$RIG_DIR/evidence/$TASK-codeaf-senior-dev-deepseek-deepseek-v4.1-flash-s1-ehigh"

command -v docker >/dev/null 2>&1 || { echo "SKIP: docker is not installed"; exit 0; }
docker image inspect "$IMAGE" >/dev/null 2>&1 || { echo "SKIP: $IMAGE is not built"; exit 0; }
[ -s "$SRC/model.patch" ] || { echo "SKIP: no retained model.patch at $SRC"; exit 0; }

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
mkdir -p "$TMP/tasks" "$TMP/run/logs/grade"
cp -R "$RIG_DIR/tasks/$TASK" "$TMP/tasks/"
cp "$SRC/model.patch" "$TMP/run/model.patch"
cp "$SRC/meta.json" "$TMP/run/meta.json"
cp "$SRC/logs/grade/judge.json" "$TMP/run/logs/grade/judge.json"
cp "$SRC/logs/grade/adapted-tests.patch" "$TMP/run/logs/grade/adapted-tests.patch"

# A patch that cannot apply, so the classical criterion conflicts and the
# adaptive path is taken; it lives under /task so the container can read it.
cat > "$TMP/tasks/$TASK/conflict.patch" <<'PATCH'
diff --git a/tests/git_test.py b/tests/git_test.py
--- a/tests/git_test.py
+++ b/tests/git_test.py
@@ -99999,1 +99999,2 @@
+def impossible():
+    assert True
PATCH
python3 - "$TMP/tasks/$TASK/rubric.toml" <<'PY'
import sys, pathlib
p = pathlib.Path(sys.argv[1])
t = p.read_text().replace('overlay = "/solution/test-overlay.patch"',
                          'overlay = "/task/conflict.patch"', 1)
p.write_text(t)
PY

TASKS="$TMP/tasks" REGRADE=1 EGRESS_SCAN=0 TASK_ID="$TASK" RESULTS="$TMP/results" \
  bash "$RIG_DIR/grade.sh" "$TMP/run" >/dev/null 2>&1

python3 - "$TMP/run/logs/grade" <<'PY'
import json, pathlib, sys
g = pathlib.Path(sys.argv[1])
pb = json.loads((g / "phaseB.json").read_text())["criteria"]["adapted-tests-pass"]
grade = json.loads((g / "grade.json").read_text())
cls = grade["criteria"]["refname-collision-no-crash"]
assert pb["status"] == "pass", f"phase B did not pass: {pb}"
assert cls["status"] == "pass", f"classical criterion not resolved: {cls}"
assert cls.get("source") == "phase-b", f"classical criterion not resolved from phase B: {cls}"
assert grade["score"] is not None, "score was not computed"
print("phase-B smoke: PASS",
      f"(phase B {pb['status']}, classical {cls['status']} from {cls.get('source')}, score {grade['score']})")
PY