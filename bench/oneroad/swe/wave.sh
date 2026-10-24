#!/usr/bin/env bash
set -uo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TASK="${1:?usage: wave.sh <task> [arms...]}"; shift
ARMS="${*:-codeaf-swe-crew pi opencode}"
SWE_OUT="${SWE_OUT:-$HOME/af-bench/swe}"
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
