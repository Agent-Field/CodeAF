#!/usr/bin/env bash
# swe/grid.sh — the Senior SWE-Bench wave, fired arm-fair, under a GLOBAL cap.
#
# THE CAP IS GLOBAL AND NOT THIS TRACK'S OWN. The live-issue wave is running on
# the same machine and each of its cells holds a tmux session, an aforge process
# and a clone; each of these holds a container with 4 CPUs and 8 GB reserved on
# top. Counting only our own cells would let the two tracks add up to twice the
# machine. So capacity is measured across BOTH runners' cells and a wave waits
# for room rather than taking it.
set -uo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SWE_OUT="${SWE_OUT:-/home/santosh/af-bench/swe}"
mkdir -p "$SWE_OUT"

ARMS="${ARMS:-aforge-swe-crew pi opencode}"
# The tasks whose reward this benchmark can actually produce. The four that are
# validation-primary are left out by name, with the reason, rather than run and
# reported as failures: their reward needs an LLM validation agent we have no
# key for, and run_aggregate.py refuses verifier-only scoring for them.
TASKS="${TASKS:-turborepo-fix-prune-missing-sources better-auth-fix-api-return-response \
immich-fix-server-live-photo prefect-fix-resolve-race-condition \
gitea-fix-force-push-timeline posthog-fix-replay-buffering gitea-fix-codeql-code-scanning}"
SKIPPED="harbor-add-agent-file-retention paperless-ngx-feat-saved-view-sharing \
gitea-refactor-auth-middleware paperless-ngx-refactor-task-system"

CAP="${CAP:-15}"
echo "swe-grid $$ $(date -Is)" >> "$SWE_OUT/PIDS"
printf 'skipped (validation-primary, no deterministic reward without judge keys): %s\n' "$SKIPPED" \
  >> "$SWE_OUT/waves.log"

running() { ps -eo args= | grep -cE 'oneroad/(swe/)?cell\.sh [a-z0-9-]+ ' || true; }

for task in $TASKS; do
  need=$(echo "$ARMS" | wc -w)
  # Wait for room for a WHOLE wave: firing half the arms of a task would break
  # the arm-fair rule that keeps provider weather comparable across them.
  while [ "$(( $(running) + need ))" -gt "$CAP" ]; do sleep 30; done
  bash "$ROOT/wave.sh" "$task" $ARMS > "$SWE_OUT/.wave-$task.out" 2>&1 &
  echo "swe-wave-$task $! $(date -Is)" >> "$SWE_OUT/PIDS"
  sleep 5
done
wait
echo "swe-grid landed $(date -Is)" >> "$SWE_OUT/PIDS"
