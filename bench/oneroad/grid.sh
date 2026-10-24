#!/usr/bin/env bash
set -uo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TASKS="${TASKS:-20 21 22 23 batch}"
MAX_WAVES="${MAX_WAVES:-3}"
ARMS="${ARMS:-}"

mkdir -p "$ROOT/results"
echo "grid $$ $(date -Is)" >> "$ROOT/results/PIDS"

for task in $TASKS; do
  # Wait for a wave to land before firing the next, so the cap holds without a
  # scheduler: `jobs -r` is this shell's own count of waves still in the air.
  while [ "$(jobs -r | wc -l)" -ge "$MAX_WAVES" ]; do sleep 20; done
  SEED="${SEED:-s1}" bash "$ROOT/wave.sh" "$task" $ARMS > "$ROOT/results/.wave-${WAVE_TAG:-}$task.out" 2>&1 &
  echo "wave-${WAVE_TAG:-}$task $! $(date -Is)" >> "$ROOT/results/PIDS"
  sleep 5
done
wait
python3 "$ROOT/collect.py" >> "$ROOT/results/.collect.out" 2>&1
echo "grid landed $(date -Is)" >> "$ROOT/results/PIDS"
