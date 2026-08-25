#!/usr/bin/env bash
# swe/wave.sh — every arm of ONE Senior SWE-Bench task, fired together.
#
# Same arm-fair rule as wave 1: the arms of a task start within seconds of each
# other so provider weather falls on all of them or none. The cap is lower here
# than on the live-issue track — each cell holds a container with 4 CPUs and 8 GB
# reserved, so five of them is already most of a machine.
set -uo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TASK="${1:?usage: wave.sh <task> [arms...]}"; shift
ARMS="${*:-aforge-swe-crew pi opencode}"
SWE_OUT="${SWE_OUT:-/home/santosh/af-bench/swe}"
mkdir -p "$SWE_OUT"
printf 'wave %s fired %s loadavg %s\n' "$TASK" "$(date -Is)" "$(cut -d' ' -f1-3 /proc/loadavg)" \
  >> "$SWE_OUT/waves.log"
pids=()
for arm in $ARMS; do
  bash "$ROOT/cell.sh" "$arm" "$TASK" > "$SWE_OUT/.$arm-$TASK.out" 2>&1 &
  pids+=($!); sleep 3
done
for pid in "${pids[@]}"; do wait "$pid"; done
printf 'wave %s landed %s loadavg %s\n' "$TASK" "$(date -Is)" "$(cut -d' ' -f1-3 /proc/loadavg)" \
  >> "$SWE_OUT/waves.log"
