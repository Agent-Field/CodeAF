#!/usr/bin/env bash
# swe/cell.sh — ONE Senior SWE-Bench cell: one arm, one task, one container.
#
# Usage: swe/cell.sh <arm> <task>
#
# THE SHAPE, AND WHY IT IS THIS SHAPE.
#
# The task's repository only exists inside its Docker image, with the toolchain
# the repo needs already built (better-auth's image carries a full pnpm build).
# So the harness has to run IN the container. Two ways to get a real TUI in
# there, and this takes the second:
#
#   a thin derived image per task with tmux apt-installed into it — seven builds,
#   seven images to keep in step with the dataset's own, and a benchmark that
#   measures a container it built rather than the one the dataset ships;
#
#   TMUX ON THE HOST, driving `docker exec -it` — no image is modified, the
#   dataset's container is the one under test, and the pane is a real TTY on a
#   real terminal exactly as wave 1's was.
#
# The second also buys the thing that makes the road columns work: the profile
# directory is a BIND MOUNT from the cell's own host directory, so the journal
# the settle detector polls and lib/road.py reads is on the host, at a path the
# existing readers already understand. Nothing about the autopsy changes.
#
# ORACLE SAFETY. solution/oracle.patch, tests/judge/ and task.toml (whose
# [metadata] carries the canonical fix IN PROSE) must never reach a harness or a
# judge. Only tests/ is mounted, at /tests, and tests/judge is masked with an
# empty read-only directory over it. The prompt is instruction.md's `## Task`
# section alone — not the file, which also carries the general instructions, and
# never task.toml.
set -uo pipefail

ARM="${1:?usage: cell.sh <arm> <task>}"
TASK="${2:?usage: cell.sh <arm> <task>}"
SEED="${SEED:-s1}"

SWE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ONEROAD="$(cd "$SWE/.." && pwd)"
DATASET="${DATASET:-/home/santosh/src/senior-swe-bench-v2026.06}"
TASKDIR="$DATASET/tasks/$TASK"

MODEL="${MODEL:-deepseek/deepseek-v4-flash}"
NEW_BIN="${NEW_BIN:-/home/santosh/af-oneroad/bin/aforge}"
PI_BIN="${PI_BIN:-$(command -v pi)}"
OPENCODE_BIN="${OPENCODE_BIN:-$HOME/.opencode/bin/opencode}"

# Forty-five minutes: these repositories are large and a build alone can take
# several minutes before the harness has read anything.
CELL_SECONDS="${CELL_SECONDS:-2700}"
SILENCE_SECONDS="${SILENCE_SECONDS:-180}"
POLL="${POLL:-10}"

# LIVE ARTIFACTS LIVE OUTSIDE THE WORKTREE, and this is not tidiness.
# A cell's bind mounts are written by root inside the container, and while the
# cell runs they are root-owned. A root-owned directory anywhere under the repo
# breaks more than `go build`: internal/config's registry test walks the tree and
# died on `permission denied` reading a live cell's profile. Go's `_` prefix
# hides a directory from the BUILD, not from filepath.Walk.
# So the artifacts are written to $SWE_OUT (default ~/af-bench/swe) and
# bench/oneroad/swe/_results is a SYMLINK to it — filepath.Walk does not follow
# symlinks, so no walker in the repository can descend into a running cell.
SWE_OUT="${SWE_OUT:-/home/santosh/af-bench/swe}"
mkdir -p "$SWE_OUT"
CELL="$SWE_OUT/$ARM-$TASK-$SEED"
rm -rf "$CELL"; mkdir -p "$CELL"/{profile,home,logs,peer}
CONTAINER="oneroad-swe-$ARM-$TASK-$SEED"
SESSION_NAME="$CONTAINER"

say() { printf '[%s] %s\n' "$(date +%H:%M:%S)" "$*" | tee -a "$CELL/cell.log"; }

[ -d "$TASKDIR" ] || { say "no such task: $TASKDIR"; exit 2; }
docker image inspect "$TASK:latest" >/dev/null 2>&1 || { say "no image $TASK:latest — skipped"; echo NOIMAGE > "$CELL/outcome"; exit 3; }

# ── the prompt: the Task section, and nothing else ──────────────────────────
# instruction.md is two sections. `## Task` is the person's own words — the
# thing every arm is handed. `## General instructions` is harness scaffolding
# from the dataset ("you are inside a Docker container", "dependencies are not
# pre-installed") and it IS given too, because it is what the dataset hands its
# own baselines and removing it would change the task rather than isolate it.
python3 - "$TASKDIR/instruction.md" "$CELL/prompt.txt" "$CELL/issue.txt" <<'PY'
import re, sys
raw = open(sys.argv[1]).read()
# The judge's ISSUE is the Task section alone: the general instructions describe
# the container, not the work, and a judge grading "did it do what was asked"
# must not count them among the asks.
task = re.split(r"^## +General instructions", raw, flags=re.M)[0]
task = re.sub(r"^## +Task\s*", "", task, flags=re.M).strip()
open(sys.argv[2], "w").write(raw.strip() + "\n")
open(sys.argv[3], "w").write(task + "\n")
PY
REPO_NAME="$(python3 -c "
import re,sys
raw=open('$TASKDIR/task.toml').read()
m=re.search(r'REPO_NAME\s*=\s*\"([^\"]+)\"',raw)
print(m.group(1) if m else '')")"
[ -n "$REPO_NAME" ] || REPO_NAME="$(docker run --rm --entrypoint sh "$TASK:latest" -c 'ls /repo | head -1')"
say "$ARM/$TASK: repo=/repo/$REPO_NAME"
echo "$REPO_NAME" > "$CELL/repo-name"

# ── the container ───────────────────────────────────────────────────────────
CPUS="$(python3 -c "
import re;raw=open('$TASKDIR/task.toml').read()
m=re.search(r'^cpus\s*=\s*(\d+)',raw,re.M);print(m.group(1) if m else 4)")"
MEM="$(python3 -c "
import re;raw=open('$TASKDIR/task.toml').read()
m=re.search(r'^memory\s*=\s*\"([^\"]+)\"',raw,re.M);print(m.group(1) if m else '8G')")"

docker rm -f "$CONTAINER" >/dev/null 2>&1
mkdir -p "$CELL/empty"
docker run -d --name "$CONTAINER" --cpus "$CPUS" --memory "$MEM" \
  -v "$TASKDIR/tests:/tests:ro" \
  -v "$CELL/empty:/tests/judge:ro" \
  -v "$CELL/logs:/logs" \
  -v "$CELL/profile:/prof" \
  -v "$CELL/home:/chome" \
  -v "$CELL/peer:/peer" \
  -v "$SWE/verify.sh:/oneroad-verify.sh:ro" \
  -v "$NEW_BIN:/usr/local/bin/aforge:ro" \
  ${PI_BIN:+-v "$PI_BIN:/usr/local/bin/pi:ro"} \
  ${OPENCODE_BIN:+-v "$OPENCODE_BIN:/usr/local/bin/opencode:ro"} \
  -e "OPENROUTER_API_KEY=$OPENROUTER_API_KEY" \
  -e "REPO_NAME=$REPO_NAME" \
  --entrypoint sleep "$TASK:latest" infinity >/dev/null || { say "container failed to start"; exit 1; }
say "$ARM/$TASK: container $CONTAINER up (cpus=$CPUS mem=$MEM)"

# tests/judge is masked rather than left out, because /tests must be one mount:
# the verifier imports ssb_lib from it. An empty read-only directory over
# tests/judge means oracle.patch and rubric.json are simply not there to read.
docker exec "$CONTAINER" sh -c 'ls /tests/judge 2>/dev/null | wc -l' > "$CELL/judge-dir-masked.txt"

cleanup() {
  tmux kill-session -t "$SESSION_NAME" 2>/dev/null
  # HAND THE FILES BACK BEFORE THE CONTAINER GOES. Everything under the cell is a
  # bind mount written by root inside the container; left that way it is
  # unreadable to the tools that have to read it and — the failure that taught
  # this — a root-owned directory inside the Go module makes `go build ./...`
  # fail with permission denied for every lane on the machine. The chown runs in
  # the container because only root in there can perform it.
  docker exec "$CONTAINER" chown -R "$HOST_UID:$HOST_GID" /prof /chome /logs /peer 2>/dev/null
  docker rm -f "$CONTAINER" >/dev/null 2>&1
  # A container that died before cleanup cannot chown its own mounts, so the
  # same reclaim is attempted once more from a throwaway one.
  if find "$CELL" ! -user "$HOST_UID" -print -quit 2>/dev/null | grep -q .; then
    docker run --rm -v "$CELL:/c" ubuntu:24.04 chown -R "$HOST_UID:$HOST_GID" /c >/dev/null 2>&1
  fi
}
HOST_UID="$(id -u)"; HOST_GID="$(id -g)"
trap cleanup EXIT INT TERM

# ── the aforge arms: the real TUI, over tmux, into the container ────────────
run_aforge() {
  local all_flash="$1"
  ALL_FLASH="$all_flash" MODEL="$MODEL" PROFILE="$CELL/profile" python3 - <<'PY'
import datetime, json, os
p = os.environ["PROFILE"]
cfg = {"api_key": os.environ["OPENROUTER_API_KEY"],
       "setup_seen_at": datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%S.%fZ")}
if os.environ["ALL_FLASH"] == "1":
    for tier in ("reflex", "low", "high", "mastermind"):
        cfg["models.tiers." + tier] = os.environ["MODEL"]
json.dump(cfg, open(os.path.join(p, "config.json"), "w"), indent=2)
PY
  chmod 600 "$CELL/profile/config.json"

  tmux kill-session -t "$SESSION_NAME" 2>/dev/null
  # tmux is the HOST's; the TTY it allocates belongs to `docker exec -it`, so the
  # surface drawing into this pane is the one running beside the repository.
  tmux new-session -d -s "$SESSION_NAME" -x 200 -y 50 \
    "docker exec -it -w /repo/$REPO_NAME \
       -e HOME=/chome -e AFORGE_HOME=/prof -e AFORGE_PROFILE_DIR=/prof \
       -e OPENROUTER_API_KEY=$OPENROUTER_API_KEY -e TERM=xterm-256color \
       $CONTAINER aforge chat --yolo --model '$MODEL'; echo AFORGE-EXITED; sleep 60"

  local waited=0 drew=""
  while [ "$waited" -lt 90 ]; do
    sleep 3; waited=$((waited + 3))
    tmux capture-pane -t "$SESSION_NAME" -p 2>/dev/null | grep -q 'AFORGE-EXITED' && break
    if tmux capture-pane -t "$SESSION_NAME" -p 2>/dev/null | grep -Eq '›|try "what is in this folder"'; then drew=yes; break; fi
  done
  tmux capture-pane -t "$SESSION_NAME" -p > "$CELL/tmux-firstframe.txt"
  [ -n "$drew" ] || { say "no first frame within 90s"; return 91; }

  tmux load-buffer -b "$SESSION_NAME" "$CELL/prompt.txt"
  tmux paste-buffer -p -b "$SESSION_NAME" -t "$SESSION_NAME"
  sleep 3
  tmux capture-pane -t "$SESSION_NAME" -p > "$CELL/tmux-composed.txt"
  tmux send-keys -t "$SESSION_NAME" Enter
  say "$ARM/$TASK: sent ($(wc -c < "$CELL/prompt.txt") bytes), wall ${CELL_SECONDS}s"

  # Settlement, wave 1's rules exactly: the store is the signal (presence.json is
  # a heartbeat and is excluded), the screen may delay a settle but past 3x the
  # window it may not prevent one. The store is a bind mount, so this is a
  # host-side read of the same bytes the container is writing.
  local started elapsed stable=0 quiet=0 last="" now screen
  started=$(date +%s)
  while :; do
    sleep "$POLL"
    elapsed=$(( $(date +%s) - started ))
    if [ "$elapsed" -ge "$CELL_SECONDS" ]; then
      say "$ARM/$TASK: DNF at ${elapsed}s"
      tmux capture-pane -t "$SESSION_NAME" -p > "$CELL/tmux-final.txt"
      tmux kill-session -t "$SESSION_NAME" 2>/dev/null
      echo DNF > "$CELL/outcome"; return 124
    fi
    now="$(find "$CELL/profile/v3" -type f ! -name presence.json -printf '%s %T@ %p\n' 2>/dev/null | sort | md5sum)"
    screen="$(tmux capture-pane -t "$SESSION_NAME" -p 2>/dev/null)"
    printf '%s\n' "$screen" > "$CELL/tmux-live.txt"
    if [ "$now" = "$last" ]; then quiet=$((quiet + POLL)); else quiet=0; fi
    if [ "$now" = "$last" ] && { ! printf '%s' "$screen" | grep -qE 'working' || [ "$quiet" -ge $((SILENCE_SECONDS * 3)) ]; }; then
      stable=$((stable + POLL))
      [ "$stable" -ge "$SILENCE_SECONDS" ] && { say "$ARM/$TASK: settled at ${elapsed}s"; break; }
    else stable=0; fi
    last="$now"
  done
  tmux capture-pane -t "$SESSION_NAME" -p > "$CELL/tmux-final.txt"
  tmux kill-session -t "$SESSION_NAME" 2>/dev/null
  echo OK > "$CELL/outcome"; return 0
}

# ── the peer arms: their own front door, in the same container ─────────────
run_peer() {
  local inner
  case "$ARM" in
    pi)       inner="pi -p --provider openrouter --model '$MODEL' \"\$(cat /oneroad-prompt.txt)\"" ;;
    opencode) inner="opencode run --auto -m 'openrouter/$MODEL' \"\$(cat /oneroad-prompt.txt)\"" ;;
  esac
  docker cp "$CELL/prompt.txt" "$CONTAINER:/oneroad-prompt.txt" >/dev/null
  # Their state dirs are bind-mounted out so competitor_cost.py can read the
  # session stores afterwards, exactly as it does on the host in wave 1.
  timeout "$CELL_SECONDS" docker exec -w "/repo/$REPO_NAME" \
    -e HOME=/peer -e "OPENROUTER_API_KEY=$OPENROUTER_API_KEY" \
    "$CONTAINER" bash -lc "$inner"
}

# ── the run ─────────────────────────────────────────────────────────────────
STARTED=$(date +%s)
cut -d' ' -f1-3 /proc/loadavg > "$CELL/loadavg-before"
case "$ARM" in
  aforge-swe-flash) run_aforge 1; CODE=$? ;;
  aforge-swe-crew)  run_aforge 0; CODE=$? ;;
  pi|opencode)      run_peer >"$CELL/harness.log" 2>&1; CODE=$?
                    [ "$CODE" = "124" ] && echo DNF > "$CELL/outcome" || echo OK > "$CELL/outcome" ;;
  *) say "unknown arm $ARM"; cleanup; exit 2 ;;
esac
WALL=$(( $(date +%s) - STARTED ))
cut -d' ' -f1-3 /proc/loadavg > "$CELL/loadavg-after"
say "$ARM/$TASK: harness done, exit $CODE, ${WALL}s — verifying"

# ── the dataset's own verdict ───────────────────────────────────────────────
VSTART=$(date +%s)
timeout 1800 docker exec -e "REPO_NAME=$REPO_NAME" "$CONTAINER" bash /oneroad-verify.sh \
  > "$CELL/verify.log" 2>&1
VCODE=$?
VWALL=$(( $(date +%s) - VSTART ))
say "$ARM/$TASK: verifier exit $VCODE in ${VWALL}s"

# ── the record ──────────────────────────────────────────────────────────────
python3 "$SWE/record.py" "$CELL" "$ARM" "$TASK" "$SEED" "$WALL" "$CODE" "$VWALL" "$MODEL" "$TASKDIR"
# The SWE bundle, not lib/judge_bundle.py: this track's ISSUE is issue.txt (the
# Task section alone) and its PATCH is the dataset's agent.patch, neither of
# which the wave-1 builder knows about.
python3 "$SWE/judge_bundle_swe.py" "$CELL"
cleanup
say "$ARM/$TASK: done — $CELL"
