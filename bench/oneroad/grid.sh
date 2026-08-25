#!/usr/bin/env bash
# grid.sh — the whole 25-cell wave-1 grid, driven by ONE detached process.
#
# IT IS DETACHED ON PURPOSE. The first attempt at this grid was launched from an
# interactive harness, and when that harness restarted it took every cell with
# it: fifteen cells mid-flight, no rows. A benchmark that a terminal closing can
# kill is a benchmark that has to be re-run every time somebody's editor
# crashes, so this is started under setsid and writes its own pid where a person
# who lost it can find it (results/PIDS).
#
# THE CAP AND THE WAVES. Five arms of one task fire together (wave.sh says why),
# and no more than MAX_WAVES tasks are in the air at once — five cells a wave, so
# three waves is fifteen cells, which is the honest ceiling for one machine whose
# cells spend their middles waiting on a provider and their ends running pytest.
set -uo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TASKS="${TASKS:-20 21 22 23 batch}"
MAX_WAVES="${MAX_WAVES:-3}"
# ARMS lets one grid run a SUBSET of the arms over the same tasks, which is what
# a follow-up wave is: wave 1b re-asks the identical five tasks of two new arms
# and must not re-run the eight rows that already landed. Empty is every arm,
# which is what wave 1 wanted.
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
