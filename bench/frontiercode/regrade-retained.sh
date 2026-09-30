#!/usr/bin/env bash
# Regrade one retained run's model.patch with the current grader, leaving the
# original evidence untouched. Phase A (and phase B, when the retained adapted
# patch exists) rerun in the task's verify image; the retained judge.json is
# reused, so no judge call is made and no provider spend is incurred. The
# corrected verdict lands in evidence-corrected/<run>/ beside a provenance file,
# never in evidence/.
#
#   bench/frontiercode/regrade-retained.sh <original-run-dir> [task-id]
#
# DEST=<dir> overrides the output directory. The verify image must already be
# built (it is the image the original grade used).
set -uo pipefail
RIG_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$RIG_DIR/lib.sh"

ORIG="${1:?usage: regrade-retained.sh <original-run-dir> [task-id]}"
ORIG="$(cd "$ORIG" && pwd)"
TASK="${2:-$(python3 -c "import json,sys;print(json.load(open(sys.argv[1])).get('task',''))" "$ORIG/meta.json")}"
load_task "$TASK" || exit 1

DEST="${DEST:-$RIG_DIR/evidence-corrected/$(basename "$ORIG")}"
rm -rf "$DEST"; mkdir -p "$DEST/logs/grade"
cp "$ORIG/model.patch" "$DEST/model.patch"
cp "$ORIG/meta.json" "$DEST/meta.json"
# The retained judge verdicts and adapted patch are the original run's own
# provenance: reused verbatim, never regenerated here.
cp "$ORIG/logs/grade/judge.json" "$DEST/logs/grade/judge.json" 2>/dev/null || true
cp "$ORIG/logs/grade/adapted-tests.patch" "$DEST/logs/grade/adapted-tests.patch" 2>/dev/null || true

cat > "$DEST/PROVENANCE.md" <<EOF
# Corrected grade — $(basename "$ORIG")

Regraded $(date -u +%Y-%m-%dT%H:%M:%SZ) with the grader fixed in this change
(overlay context reduction; phase-B plumbing; phase_a verdict on an
unappliable patch; phaseB.json criteria key; adaptive resolution of a
conflicted classical criterion).

- Original evidence: \`bench/frontiercode/evidence/$(basename "$ORIG")/\`
  (untouched; this directory is the corrected copy).
- model.patch: copied verbatim from the original run.
- judge.json: the ORIGINAL judge verdicts, reused — no judge call was made.
- phase A (and phase B, when the retained adapted patch exists) rerun in the
  task's verifier image.
- Task: $TASK
EOF

REGRADE=1 EGRESS_SCAN=0 TASK_ID="$TASK" RESULTS="$RIG_DIR/evidence-corrected" \
  bash "$RIG_DIR/grade.sh" "$DEST"