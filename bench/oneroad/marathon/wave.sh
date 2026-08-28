#!/usr/bin/env bash
# marathon/wave.sh — every arm of ONE marathon task, fired together.
#
# The arm-fair rule: the arms of a task start within seconds of each other so
# provider weather falls on all of them or none. Each cell holds a container with
# the task's own reservation (4 CPUs, 16 GB here) plus a two-CPU scorer once an
# hour, so three arms is already most of a twenty-CPU machine — this fires one
# task's arms and nothing else.
set -uo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TASK="${1:?usage: wave.sh <task> [arms...]}"; shift
ARMS="${*:-aforge-crew pi opencode}"
MAR_OUT="${MAR_OUT:-/home/santosh/af-bench/marathon}"
mkdir -p "$MAR_OUT"
printf 'wave %s fired %s arms=%s loadavg %s\n' "$TASK" "$(date -Is)" "$ARMS" \
  "$(cut -d' ' -f1-3 /proc/loadavg)" >> "$MAR_OUT/waves.log"
pids=()
i=0
for arm in $ARMS; do
  # The cells start together — that is the arm-fair rule — but their hourly
  # snapshot scorers are given different phases so three release builds never
  # start at the same second on a machine the cells have already filled.
  nohup env SNAPSHOT_OFFSET=$(( i * 900 )) \
    bash "$ROOT/cell.sh" "$arm" "$TASK" > "$MAR_OUT/.$arm-$TASK.out" 2>&1 &
  pids+=($!)
  printf '%s %s pid=%s %s\n' "$TASK" "$arm" "$!" "$(date -Is)" >> "$MAR_OUT/PIDS"
  echo "launched $arm (pid $!, snapshot offset $(( i * 900 ))s)"
  i=$((i + 1))
  sleep 5
done
for pid in "${pids[@]}"; do wait "$pid"; done
printf 'wave %s landed %s loadavg %s\n' "$TASK" "$(date -Is)" \
  "$(cut -d' ' -f1-3 /proc/loadavg)" >> "$MAR_OUT/waves.log"
