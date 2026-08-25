#!/usr/bin/env bash
# marathon/finish.sh — end a cell that is still up, by the same road cell.sh ends one.
#
# Usage: finish.sh <arm> <task> <seed> <settle-reason>
#
# WHY THIS EXISTS SEPARATELY. A cell whose settle rule failed to fire is sitting
# on a live container with a live verdict inside it, and the two obvious ways to
# rescue it are both wrong:
#
#   editing cell.sh in place — bash reads a script INCREMENTALLY, from a byte
#   offset it keeps as it goes, so rewriting the file under a running cell makes
#   it resume parsing at an offset that no longer means what it meant. The
#   verify-and-record tail is exactly the part that would be misparsed;
#
#   killing cell.sh — its EXIT trap tears the container down, and the workspace
#   the verifier has to score goes with it.
#
# So the ending is performed from outside, against the live container, in the
# same order and with the same steps as cell.sh's own tail — reap the harness,
# stage tests/ (never before the harness is dead), run the verifier, hand the
# files back, write the record — and only THEN is cell.sh killed, so that its own
# cleanup() performs the teardown it was always going to perform.
#
# It does not touch /workspace. A worker that made its own arrangement in there —
# the `test-files -> java` symlink this task's scorer needs, a build tree, a
# scratch file — is part of the container state the benchmark scores, and a
# runner that tidies before judging is scoring something the agent did not leave.
set -uo pipefail

ARM="${1:?usage: finish.sh <arm> <task> <seed> <settle-reason>}"
TASK="${2:?}"; SEED="${3:?}"; REASON="${4:-finished out of band}"

MAR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MARATHON_REPO="${MARATHON_REPO:-/tmp/claude-1001/-home-santosh/36adab24-74be-4ae9-946a-ab932634b759/scratchpad/marathon/repo}"
TASKDIR="$MARATHON_REPO/tasks/$TASK"
MAR_OUT="${MAR_OUT:-/home/santosh/af-bench/marathon}"
CELL="$MAR_OUT/$ARM-$TASK-$SEED"
CONTAINER="oneroad-mar-$ARM-$TASK-$SEED"
SESSION_NAME="$CONTAINER"
MODEL="${MODEL:-deepseek/deepseek-v4-flash}"
NEW_BIN="${NEW_BIN:-/home/santosh/af-oneroad/bin/aforge}"
HOST_UID="$(id -u)"; HOST_GID="$(id -g)"

say() { printf '[%s] %s\n' "$(date +%H:%M:%S)" "$*" | tee -a "$CELL/cell.log"; }

[ -d "$CELL" ] || { echo "no such cell: $CELL"; exit 2; }
docker inspect -f '{{.State.Running}}' "$CONTAINER" 2>/dev/null | grep -q true \
  || { echo "container $CONTAINER is not running — nothing to finish"; exit 2; }

IMAGE="$(docker inspect -f '{{.Config.Image}}' "$CONTAINER")"
IMAGE_ID="$(docker image inspect -f '{{.Id}}' "$IMAGE")"
WORKDIR="$(docker inspect -f '{{.Config.WorkingDir}}' "$CONTAINER")"
[ -n "$WORKDIR" ] || WORKDIR="$(docker image inspect -f '{{.Config.WorkingDir}}' "$IMAGE")"
TASK_COMMIT="$(git -C "$MARATHON_REPO" rev-parse HEAD 2>/dev/null || echo unknown)"
VERIFIER_TIMEOUT="${VERIFIER_TIMEOUT:-3600}"

# The snapshot loop is stopped first: it is about to take two more CPUs and half
# an hour for a curve point about a cell that is ending anyway, and it would take
# them from the verifier.
pkill -f "marathon/snapshot.sh $CELL " 2>/dev/null
say "$ARM/$TASK: finishing out of band — $REASON"

STARTED_EPOCH="$(date -d "$(cat "$CELL/started-at")" +%s 2>/dev/null || echo 0)"
WALL=$(( $(date +%s) - STARTED_EPOCH ))
tmux capture-pane -t "$SESSION_NAME" -p > "$CELL/tmux-final.txt" 2>/dev/null
tmux kill-session -t "$SESSION_NAME" 2>/dev/null
echo "$REASON" > "$CELL/settle_reason"
echo OK > "$CELL/outcome"
date -Is > "$CELL/ended-at"
cut -d' ' -f1-3 /proc/loadavg > "$CELL/loadavg-after"
[ -f "$CELL/loadavg-before" ] || cut -d' ' -f1-3 /proc/loadavg > "$CELL/loadavg-before"
touch "$CELL/agent-done"

# Killing the tmux session only kills the `docker exec` CLIENT; the process it
# started lives on in the container as an orphan, and an orphan holding a cargo
# build would fight the verifier for the same target directory and CPUs.
docker exec "$CONTAINER" sh -c \
  'pkill -f aforge; pkill -f "pi/dist/bundle"; pkill -f opencode; sleep 3;
   pkill -9 -f aforge; pkill -9 -f "pi/dist/bundle"; pkill -9 -f opencode; sleep 1;
   pkill -9 cargo; pkill -9 rustc; true' >/dev/null 2>&1
sleep 2

docker cp "$TASKDIR/tests" "$CONTAINER:/tests" >/dev/null || { say "could not stage /tests"; exit 1; }
VSTART=$(date +%s)
timeout "$VERIFIER_TIMEOUT" docker exec -e "WORKDIR=$WORKDIR" "$CONTAINER" bash /oneroad-verify.sh \
  > "$CELL/verify.log" 2>&1
VCODE=$?
VWALL=$(( $(date +%s) - VSTART ))
say "$ARM/$TASK: verifier exit $VCODE in ${VWALL}s"

docker exec "$CONTAINER" chown -R "$HOST_UID:$HOST_GID" /prof /chome /logs /peer 2>/dev/null

MODEL="$MODEL" IMAGE_REF="$IMAGE" IMAGE_ID="$IMAGE_ID" TASK_COMMIT="$TASK_COMMIT" \
AFORGE_BUILD_COMMIT="${AFORGE_BUILD_COMMIT:-}" \
WORKDIR="$WORKDIR" NEW_BIN="$NEW_BIN" CELL_SECONDS="${CELL_SECONDS:-36000}" \
python3 "$MAR/record.py" "$CELL" "$ARM" "$TASK" "$SEED" "$WALL" 0 "$VWALL" "$VCODE"

# NOW the cell's own process may go: its EXIT trap is the teardown — kill the
# tmux session, hand the mounts back, remove the container — and letting it run
# is how this ending stays the same ending cell.sh would have performed.
# Matched WITHOUT a directory prefix, because a cell launched from inside the
# runner directory has argv `bash cell.sh <arm> <task>` and one launched by
# wave.sh has the absolute path. A pattern that assumed the second would silently
# fail to release the first, leaving it polling a container that no longer exists.
CELLPID="$(pgrep -f "cell\.sh $ARM $TASK" | head -1)"
if [ -n "$CELLPID" ]; then
  say "$ARM/$TASK: releasing cell.sh pid $CELLPID (its EXIT trap tears the cell down)"
  kill "$CELLPID" 2>/dev/null
  for _ in $(seq 30); do
    docker inspect -f '{{.State.Running}}' "$CONTAINER" >/dev/null 2>&1 || break
    sleep 1
  done
fi
# Belt and braces: if the cell's own process was already gone, its trap cannot
# have run, so the same teardown is performed here.
if docker inspect -f '{{.State.Running}}' "$CONTAINER" >/dev/null 2>&1; then
  docker exec "$CONTAINER" chown -R "$HOST_UID:$HOST_GID" /prof /chome /logs /peer 2>/dev/null
  docker rm -f "$CONTAINER" >/dev/null 2>&1
fi
if find "$CELL" ! -user "$HOST_UID" -print -quit 2>/dev/null | grep -q .; then
  docker run --rm -v "$CELL:/c" ubuntu:24.04 chown -R "$HOST_UID:$HOST_GID" /c >/dev/null 2>&1
fi
say "$ARM/$TASK: done — $CELL"
