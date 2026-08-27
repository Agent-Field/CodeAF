#!/usr/bin/env bash
# wave.sh — every arm of ONE task, fired together.
#
# THE SAME-WAVE RULE, which bench/swarm/run.sh's arm-fair waves already state:
# all five arms of a task start within seconds of each other, so provider
# weather — a slow minute at OpenRouter, a rate limit, a bad route — falls on
# every arm of that task or on none of it. A grid run arm-by-arm would give the
# arm that ran at a quiet hour a bonus nothing in the CSV could show.
#
# Usage: wave.sh <task> [arm ...]
set -uo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TASK="${1:?usage: wave.sh <task> [arms...]}"; shift
ARMS="${*:-aforge-new-flash aforge-new-crew aforge-old-flash pi opencode}"
# SEED rides through so a wave can be repeated: wave 1f runs the same arm twice
# over the same five tasks, and the two runs must land in different cells.
SEED="${SEED:-s1}"

mkdir -p "$ROOT/results"
# The load at the instant the wave fired. Five cells on one box contend for CPU
# during clone, venv and pytest even though the middle of every cell is waiting
# on a model, and the discount is only arguable afterwards if the number is here.
printf 'wave %s fired %s loadavg %s\n' "$TASK" "$(date -Is)" "$(cut -d' ' -f1-3 /proc/loadavg)" \
  >> "$ROOT/results/waves.log"

pids=()
for arm in $ARMS; do
  SEED="$SEED" bash "$ROOT/cell.sh" "$arm" "$TASK" > "$ROOT/results/.$arm-$TASK-$SEED.out" 2>&1 &
  pids+=($!)
  sleep 2
done
for pid in "${pids[@]}"; do wait "$pid"; done
printf 'wave %s landed %s loadavg %s\n' "$TASK" "$(date -Is)" "$(cut -d' ' -f1-3 /proc/loadavg)" \
  >> "$ROOT/results/waves.log"
