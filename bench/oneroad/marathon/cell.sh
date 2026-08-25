#!/usr/bin/env bash
# marathon/cell.sh — ONE SWE-Marathon cell: one arm, one task, one container.
#
# Usage: marathon/cell.sh <arm> <task>          arms: aforge-crew | aforge-flash | pi | opencode
#
# THE SHAPE IS swe/cell.sh's, AND FOR THE SAME REASONS.
#
# The task only exists inside its own image — a Rust toolchain, a 1,007-file
# Java corpus and an encrypted golden corpus baked in by the dataset's own
# Dockerfile — so the harness has to run IN the container. tmux lives on the
# HOST and drives `docker exec -it`, which means the dataset's image is the one
# under test (nothing is derived, nothing is apt-installed into it) and the pane
# is a real TTY. The profile directory is a bind mount from the cell's own host
# directory, so the journal the settle detector polls and lib/road.py reads is
# on the host at a path the existing readers already understand.
#
# WHAT IS DIFFERENT HERE, AND WHY.
#
# 1. NOTHING FROM tests/ OR solution/ IS MOUNTED WHILE THE AGENT RUNS. On the
#    SWE track /tests is one mount with tests/judge masked, because the verifier
#    imports a library out of it. Here tests/ carries the HOLDOUT corpus, the
#    pristine scorer and the anti-cheat scanner — an agent that can read
#    tests/holdout/golden.jsonl has been handed the hidden test set. So the
#    container is created with no /tests at all, and tests/ is `docker cp`'d in
#    only after the harness is dead. Docker cannot add a bind mount to a running
#    container, and the copy is the honest equivalent: same container, same
#    filesystem, read after the agent can no longer read anything.
#
# 2. THE TIMER IS THE CONTAINER'S OWN UPTIME. environment/timer.sh reports
#    `36000 - $(ps -o etimes= -p 1)`, so `bash /app/timer.sh` is correct for free
#    as long as PID 1 was born when the task began. `--entrypoint sleep … infinity`
#    makes PID 1 the sleep this cell started, exactly as Harbor's own container
#    does, and the container is created immediately before the prompt is sent so
#    the agent's remaining budget matches the cell's remaining wall.
#
# 3. /logs IS SHARED BETWEEN THE TWO PHASES. The agent's own `run_tests.sh`
#    writes /logs/verifier/reward.txt and metrics.json — a score the AGENT
#    produced against the visible corpus, which must never be mistaken for the
#    verifier's verdict. So everything the agent left in /logs is moved aside to
#    /logs/agent-phase/ before /tests/test.sh runs. It is kept, not deleted: what
#    the agent believed about its own progress is evidence.
set -uo pipefail

ARM="${1:?usage: cell.sh <arm> <task>}"
TASK="${2:?usage: cell.sh <arm> <task>}"
SEED="${SEED:-s1}"

MAR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ONEROAD="$(cd "$MAR/.." && pwd)"
MARATHON_REPO="${MARATHON_REPO:-/tmp/claude-1001/-home-santosh/36adab24-74be-4ae9-946a-ab932634b759/scratchpad/marathon/repo}"
TASKDIR="$MARATHON_REPO/tasks/$TASK"
IMAGE="${IMAGE:-swe-marathon/$TASK:v1.1}"

MODEL="${MODEL:-deepseek/deepseek-v4-flash}"
NEW_BIN="${NEW_BIN:-/home/santosh/af-oneroad/bin/aforge}"
# PI IS A NODE BUNDLE, NOT A BINARY, and mounting just the entry point is how
# seven pi cells once died in 0 seconds with ERR_MODULE_NOT_FOUND: cli.js imports
# sibling files from dist/bundle/chunks/. The whole package travels, and it is
# invoked through node — which this image (plain ubuntu:24.04) does not carry, so
# the host's node is mounted alongside. Same architecture, same libc family.
PI_PKG="${PI_PKG:-$(readlink -f "$(command -v pi)" 2>/dev/null | sed 's#/dist/bundle/cli.js##')}"
HOST_NODE="${HOST_NODE:-$(readlink -f "$(command -v node)" 2>/dev/null)}"
OPENCODE_BIN="${OPENCODE_BIN:-$HOME/.opencode/bin/opencode}"

# THE SILENCE WINDOW IS FIFTEEN MINUTES HERE, NOT THREE. The SWE track's three
# minutes suits a wall of forty-five; this task's wall is ten hours and a single
# `cargo build --release` of a tree-sitter grammar runs for minutes with nothing
# written to the store. A settle rule that fires during a build throws the work
# away, and the cost of waiting is bounded by the wall while the cost of killing
# is not bounded by anything.
SILENCE_SECONDS="${SILENCE_SECONDS:-900}"
POLL="${POLL:-15}"
SNAPSHOT_EVERY="${SNAPSHOT_EVERY:-3600}"

# LIVE ARTIFACTS LIVE OUTSIDE THE WORKTREE, and this is not tidiness. A cell's
# bind mounts are written by root inside the container, and a root-owned
# directory anywhere under the repo breaks more than `go build`: internal/config's
# registry test walks the tree and dies on `permission denied`. So artifacts are
# written to $MAR_OUT (default ~/af-bench/marathon) and bench/oneroad/marathon/_results
# is a SYMLINK to it — filepath.Walk does not follow symlinks.
MAR_OUT="${MAR_OUT:-/home/santosh/af-bench/marathon}"
mkdir -p "$MAR_OUT"
CELL="$MAR_OUT/$ARM-$TASK-$SEED"
# A PREVIOUS CELL'S LEAVINGS MAY NOT BELONG TO US: a cell killed before its
# cleanup leaves root-owned files, `rm -rf` then fails, `mkdir -p` succeeds
# anyway, and the run proceeds on a HALF-WIPED directory carrying the previous
# attempt's store. So ownership is reclaimed first, then the directory removed.
HOST_UID="$(id -u)"; HOST_GID="$(id -g)"
if [ -d "$CELL" ] && ! rm -rf "$CELL" 2>/dev/null; then
  docker run --rm -v "$CELL:/c" ubuntu:24.04 chown -R "$HOST_UID:$HOST_GID" /c >/dev/null 2>&1
  rm -rf "$CELL"
fi
mkdir -p "$CELL"/{profile,home,logs,peer,snapshots}
CONTAINER="oneroad-mar-$ARM-$TASK-$SEED"
SESSION_NAME="$CONTAINER"

say() { printf '[%s] %s\n' "$(date +%H:%M:%S)" "$*" | tee -a "$CELL/cell.log"; }

[ -d "$TASKDIR" ] || { say "no such task: $TASKDIR"; exit 2; }
docker image inspect "$IMAGE" >/dev/null 2>&1 || { say "no image $IMAGE — skipped"; echo NOIMAGE > "$CELL/outcome"; exit 3; }

# ── what the task itself says its cell is ───────────────────────────────────
# Every number below comes from task.toml, so this runner works for any marathon
# task whose Dockerfile builds on this machine. Nothing about rust-java-lsp is
# spelled here; the working directory comes from the image's own WORKDIR.
TASK_CFG="$(python3 -c '
import re, sys
raw = open(sys.argv[1]).read()
sect = lambda n: (re.search(r"^\[%s\]\s*$(.*?)(?=^\[|\Z)" % n, raw, re.M | re.S) or [None, ""])[1]
def num(block, key, default):
    m = re.search(r"^%s\s*=\s*([0-9.]+)" % key, block, re.M)
    return m.group(1) if m else default
env = sect("environment")
print(num(env, "cpus", "4"), num(env, "memory_mb", "8192"),
      num(sect("agent"), "timeout_sec", "3600"),
      num(sect("verifier"), "timeout_sec", "3600"))' "$TASKDIR/task.toml")"
read -r CPUS MEM_MB AGENT_TIMEOUT VERIFIER_TIMEOUT <<<"$TASK_CFG"
CPUS="${CPUS%%.*}"; MEM_MB="${MEM_MB%%.*}"
AGENT_TIMEOUT="${AGENT_TIMEOUT%%.*}"; VERIFIER_TIMEOUT="${VERIFIER_TIMEOUT%%.*}"
# THE REAL BUDGET IS THE TASK'S OWN, so the row is comparable to the leaderboard.
CELL_SECONDS="${CELL_SECONDS:-$AGENT_TIMEOUT}"
WORKDIR="$(docker image inspect -f '{{.Config.WorkingDir}}' "$IMAGE")"
[ -n "$WORKDIR" ] || { say "image $IMAGE declares no WORKDIR"; exit 2; }
IMAGE_ID="$(docker image inspect -f '{{.Id}}' "$IMAGE")"
TASK_COMMIT="$(git -C "$MARATHON_REPO" rev-parse HEAD 2>/dev/null || echo unknown)"

# ── the prompt: instruction.md, verbatim ────────────────────────────────────
# There is no `## Task` section to cut out of it as there is on the SWE track —
# instruction.md IS the whole agent-facing instruction, including the sentence
# about `bash /app/timer.sh`, and every arm is handed exactly it.
cp "$TASKDIR/instruction.md" "$CELL/prompt.txt"

say "$ARM/$TASK: image=${IMAGE_ID:7:19} wd=$WORKDIR cpus=$CPUS mem=${MEM_MB}m wall=${CELL_SECONDS}s"

# ── the container ───────────────────────────────────────────────────────────
docker rm -f "$CONTAINER" >/dev/null 2>&1
docker run -d --name "$CONTAINER" --cpus "$CPUS" --memory "${MEM_MB}m" \
  -v "$CELL/logs:/logs" \
  -v "$CELL/profile:/prof" \
  -v "$CELL/home:/chome" \
  -v "$CELL/peer:/peer" \
  -v "$MAR/verify.sh:/oneroad-verify.sh:ro" \
  -v "$ONEROAD/lib:/oneroad-lib:ro" \
  -v "$NEW_BIN:/usr/local/bin/aforge:ro" \
  ${PI_PKG:+-v "$PI_PKG:/opt/pi:ro"} \
  ${HOST_NODE:+-v "$HOST_NODE:/opt/node:ro"} \
  ${OPENCODE_BIN:+-v "$OPENCODE_BIN:/usr/local/bin/opencode:ro"} \
  -e "OPENROUTER_API_KEY=$OPENROUTER_API_KEY" \
  --entrypoint sleep "$IMAGE" infinity >/dev/null || { say "container failed to start"; exit 1; }
STARTED=$(date +%s)
# THE AGENT'S CLOCK IS THE CONTAINER'S, and this proves it rather than assuming
# it: timer.sh reads PID 1's elapsed time, so this is what the agent will see.
docker exec "$CONTAINER" bash /app/timer.sh > "$CELL/timer-at-start.txt" 2>&1
say "$ARM/$TASK: container up — timer says $(tr '\n' ' ' < "$CELL/timer-at-start.txt")"
# Proof for the record that no part of tests/ or solution/ was reachable.
docker exec "$CONTAINER" sh -c 'ls -d /tests /solution 2>&1 | head -5' > "$CELL/oracle-absent.txt" 2>&1

SNAP_PID=""
cleanup() {
  [ -n "$SNAP_PID" ] && kill "$SNAP_PID" 2>/dev/null
  tmux kill-session -t "$SESSION_NAME" 2>/dev/null
  # HAND THE FILES BACK BEFORE THE CONTAINER GOES. Everything under the cell is a
  # bind mount written by root inside the container; left that way it is
  # unreadable to the tools that have to read it, and a root-owned directory
  # inside the Go module makes `go build ./...` fail for every lane on the
  # machine. The chown runs in the container because only root in there can do it.
  docker exec "$CONTAINER" chown -R "$HOST_UID:$HOST_GID" /prof /chome /logs /peer 2>/dev/null
  docker rm -f "$CONTAINER" >/dev/null 2>&1
  if find "$CELL" ! -user "$HOST_UID" -print -quit 2>/dev/null | grep -q .; then
    docker run --rm -v "$CELL:/c" ubuntu:24.04 chown -R "$HOST_UID:$HOST_GID" /c >/dev/null 2>&1
  fi
}
# A SIGNAL TRAP THAT CLEANS UP BUT DOES NOT EXIT LEAVES A CELL HAUNTING ITS OWN
# GRAVE. `trap cleanup EXIT INT TERM` runs cleanup on TERM and then RESUMES the
# script: the container has been removed, but the settle loop keeps polling a
# name that no longer resolves, reads an empty fingerprint as a stable one,
# settles on it, and writes a record over whatever record was already there.
# That very nearly cost the s1 row its verifier numbers. A signal ends the cell.
trap cleanup EXIT
trap 'cleanup; exit 130' INT TERM

# ── the progress curve, alongside the run ───────────────────────────────────
# It scores in its OWN short-lived container so it never steals the running
# agent's CPUs, and it is fired and forgotten: snapshot.sh traps its own failures
# and can never take the cell down with it.
if [ "$SNAPSHOT_EVERY" -gt 0 ]; then
  SNAPSHOT_EVERY="$SNAPSHOT_EVERY" SNAPSHOT_OFFSET="${SNAPSHOT_OFFSET:-0}" \
  bash "$MAR/snapshot.sh" "$CELL" "$CONTAINER" "$IMAGE" "$TASKDIR" "$WORKDIR" "$STARTED" \
    >> "$CELL/snapshot.log" 2>&1 &
  SNAP_PID=$!
  say "$ARM/$TASK: snapshot loop pid $SNAP_PID (every ${SNAPSHOT_EVERY}s)"
fi

# ── the aforge arms: the real TUI, over tmux, into the container ────────────
run_aforge() {
  local all_flash="$1"
  ALL_FLASH="$all_flash" MODEL="$MODEL" PROFILE="$CELL/profile" python3 - <<'PY'
import datetime, json, os
p = os.environ["PROFILE"]
cfg = {"api_key": os.environ["OPENROUTER_API_KEY"],
       "setup_seen_at": datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%S.%fZ")}
# THE CREW ARM PINS NOTHING. Its reader, division and repair roles must resolve
# to their registry tiers exactly as they do in normal use; only the flash arm
# collapses every tier onto the one model.
if os.environ["ALL_FLASH"] == "1":
    for tier in ("reflex", "low", "high", "mastermind"):
        cfg["models.tiers." + tier] = os.environ["MODEL"]
json.dump(cfg, open(os.path.join(p, "config.json"), "w"), indent=2)
PY
  chmod 600 "$CELL/profile/config.json"

  tmux kill-session -t "$SESSION_NAME" 2>/dev/null
  tmux new-session -d -s "$SESSION_NAME" -x 200 -y 50 \
    "docker exec -it -w $WORKDIR \
       -e HOME=/chome -e AFORGE_HOME=/prof -e AFORGE_PROFILE_DIR=/prof \
       -e OPENROUTER_API_KEY=$OPENROUTER_API_KEY -e TERM=xterm-256color \
       $CONTAINER aforge chat --yolo --model '$MODEL'; echo AFORGE-EXITED; sleep 60"

  local waited=0 drew=""
  while [ "$waited" -lt 120 ]; do
    sleep 3; waited=$((waited + 3))
    tmux capture-pane -t "$SESSION_NAME" -p 2>/dev/null | grep -q 'AFORGE-EXITED' && break
    if tmux capture-pane -t "$SESSION_NAME" -p 2>/dev/null | grep -Eq '›|try "what is in this folder"'; then drew=yes; break; fi
  done
  tmux capture-pane -t "$SESSION_NAME" -p > "$CELL/tmux-firstframe.txt"
  [ -n "$drew" ] || { say "no first frame within 120s"; echo INVALID > "$CELL/outcome"; return 91; }

  tmux load-buffer -b "$SESSION_NAME" "$CELL/prompt.txt"
  tmux paste-buffer -p -b "$SESSION_NAME" -t "$SESSION_NAME"
  sleep 3
  tmux capture-pane -t "$SESSION_NAME" -p > "$CELL/tmux-composed.txt"
  tmux send-keys -t "$SESSION_NAME" Enter
  say "$ARM/$TASK: sent ($(wc -c < "$CELL/prompt.txt") bytes), wall ${CELL_SECONDS}s"

  # Settlement, and BOTH READINGS ARE TAKEN INSIDE THE CONTAINER.
  #
  # Reading the bind mount from the host is silently wrong: the harness runs as
  # root and creates its session folder mode 700, so from the host the profile is
  # unreadable, `find` returns nothing with no error, and a fingerprint of
  # nothing is perfectly stable from the very first poll — the cell would settle
  # blind after one window no matter what the agent was doing.
  #
  # The rest is the SWE track's rule, including the correction that cost six 1f
  # cells: a live task VETOES the settle, because a worker running a build or a
  # test suite writes nothing for minutes and silence is not a task finishing.
  local elapsed stable=0 quiet=0 last="" now screen sess live tl stranded_ids
  while :; do
    sleep "$POLL"
    elapsed=$(( $(date +%s) - STARTED ))
    if [ "$elapsed" -ge "$CELL_SECONDS" ]; then
      say "$ARM/$TASK: DNF at ${elapsed}s (wall)"
      tmux capture-pane -t "$SESSION_NAME" -p > "$CELL/tmux-final.txt"
      tmux kill-session -t "$SESSION_NAME" 2>/dev/null
      echo DNF > "$CELL/outcome"; echo wall > "$CELL/settle_reason"; return 124
    fi
    # THE FINGERPRINT MUST NOT SEE THE HEARTBEATS, AND THIS COST s1 ITS ENDING.
    #
    # aforge's standing-work ticker appends one line to /prof/v3/standing/wake.log
    # every 300 seconds for as long as the profile is open. The line it writes
    # when there is nothing to do says so in its own words —
    # `examined=0 checked=0 fired=0 said=0` — but the file's size and mtime change
    # all the same, so a fingerprint over the whole store was reset every five
    # minutes by a record of NOTHING HAPPENING. With SILENCE_SECONDS at 900 the
    # `stable` counter could never reach its threshold, and the 2700-second
    # escape hatch for a pane still showing `working` could never be reached
    # either: the same heartbeat resets `quiet`. The aforge s1 cell therefore sat
    # idle from 12:14 with every task landed and would have run to the ten-hour
    # wall no matter what — its settle rule was arithmetically unreachable.
    #
    # So the two known heartbeats are excluded and nothing else is: presence.json
    # (the pane saying it is still attached) and standing/ (the ticker). Both are
    # the harness reporting that it is ALIVE, which is the opposite of the
    # question being asked. Everything a turn or a worker actually writes —
    # transcript.jsonl, tasks.json, tasks/*.jsonl, logs/jobs/* — is still counted,
    # so a wake that starts real work still shows up, in the files that work
    # writes rather than in the log that says it was considered.
    now="$(docker exec "$CONTAINER" sh -c \
      'find /prof/v3 -type f ! -name presence.json ! -path "*/standing/*" -printf "%s %T@ %p\n" 2>/dev/null | sort | md5sum' 2>/dev/null)"
    screen="$(tmux capture-pane -t "$SESSION_NAME" -p 2>/dev/null)"
    printf '%s\n' "$screen" > "$CELL/tmux-live.txt"
    if [ "$now" = "$last" ]; then quiet=$((quiet + POLL)); else quiet=0; fi

    sess="$(docker exec "$CONTAINER" sh -c \
      'find /prof/v3/projects -mindepth 2 -maxdepth 2 -type d 2>/dev/null | head -1' 2>/dev/null | tr -d '\r')"
    live=0; stranded_ids=""
    if [ -n "$sess" ]; then
      tl="$(docker exec "$CONTAINER" python3 /oneroad-lib/tasklive.py "$sess" 2>/dev/null)"
      live="$(printf '%s' "$tl" | python3 -c 'import json,sys
try:  print(json.load(sys.stdin)["live"])
except Exception: print(0)' 2>/dev/null)"
      # See the wave-1 runner: a live STATE is not a live task.
      stranded_ids="$(printf '%s' "$tl" | python3 -c 'import json,sys
try:  print(",".join(str(i) for i in json.load(sys.stdin).get("stranded_ids") or []))
except Exception: print("")' 2>/dev/null)"
      live="${live:-0}"
    fi

    if [ "$now" = "$last" ] && [ "$live" = "0" ] \
       && { ! printf '%s' "$screen" | grep -qE 'working' || [ "$quiet" -ge $((SILENCE_SECONDS * 3)) ]; }; then
      stable=$((stable + POLL))
      if [ "$stable" -ge "$SILENCE_SECONDS" ]; then
        if [ -n "$stranded_ids" ]; then
          say "$ARM/$TASK: settled at ${elapsed}s — task(s) $stranded_ids STRANDED"
          echo "stranded:$stranded_ids" > "$CELL/settle_reason"
          echo STRANDED > "$CELL/outcome_override"
        else
          say "$ARM/$TASK: settled at ${elapsed}s (idle, and every task landed)"
          echo idle-and-landed > "$CELL/settle_reason"
        fi
        break
      fi
    else
      stable=0
      if [ "$live" != "0" ] && [ $((elapsed % 900)) -lt "$POLL" ]; then
        say "$ARM/$TASK: ${elapsed}s — $live task(s) still running; not settling"
      fi
    fi
    last="$now"
  done
  tmux capture-pane -t "$SESSION_NAME" -p > "$CELL/tmux-final.txt"
  tmux kill-session -t "$SESSION_NAME" 2>/dev/null
  if [ -f "$CELL/outcome_override" ]; then cp "$CELL/outcome_override" "$CELL/outcome"; else echo OK > "$CELL/outcome"; fi
  return 0
}

# ── the peer arms: their own front door, in the same container ─────────────
run_peer() {
  local inner
  case "$ARM" in
    pi)       inner="NODE=\$(command -v node || echo /opt/node); \$NODE /opt/pi/dist/bundle/cli.js -p --provider openrouter --model '$MODEL' \"\$(cat /oneroad-prompt.txt)\"" ;;
    opencode) inner="opencode run --auto -m 'openrouter/$MODEL' \"\$(cat /oneroad-prompt.txt)\"" ;;
  esac
  docker cp "$CELL/prompt.txt" "$CONTAINER:/oneroad-prompt.txt" >/dev/null
  # Their state dirs are bind-mounted out (HOME=/peer) so lib/competitor_cost.py
  # can read the session stores afterwards. /peer is also deliberately not under
  # /tmp, /root, /home or /workspace: tests/test.sh's cached-golden scan walks
  # exactly those four, and a harness store that happened to hold a large .json
  # would otherwise read as a cheat.
  timeout "$CELL_SECONDS" docker exec -w "$WORKDIR" \
    -e HOME=/peer -e "OPENROUTER_API_KEY=$OPENROUTER_API_KEY" \
    "$CONTAINER" bash -lc "$inner"
}

# ── the run ─────────────────────────────────────────────────────────────────
cut -d' ' -f1-3 /proc/loadavg > "$CELL/loadavg-before"
date -Is > "$CELL/started-at"
case "$ARM" in
  aforge-crew)  run_aforge 0; CODE=$? ;;
  aforge-flash) run_aforge 1; CODE=$? ;;
  pi|opencode)  run_peer >"$CELL/harness.log" 2>&1; CODE=$?
                if [ "$CODE" = "124" ]; then echo DNF > "$CELL/outcome"
                elif [ "$CODE" -ge 128 ] 2>/dev/null; then echo KILLED > "$CELL/outcome"
                else echo OK > "$CELL/outcome"; fi ;;
  *) say "unknown arm $ARM"; exit 2 ;;
esac
WALL=$(( $(date +%s) - STARTED ))
date -Is > "$CELL/ended-at"
cut -d' ' -f1-3 /proc/loadavg > "$CELL/loadavg-after"
echo "$CODE" > "$CELL/harness-exit"
[ -n "$SNAP_PID" ] && { kill "$SNAP_PID" 2>/dev/null; SNAP_PID=""; }
touch "$CELL/agent-done"
say "$ARM/$TASK: harness done, exit $CODE, ${WALL}s — verifying"

# THE HARNESS IS KILLED INSIDE THE CONTAINER BEFORE THE VERIFIER RUNS.
# Killing the tmux session only kills the `docker exec` CLIENT; the process it
# started lives on in the container as an orphan, and an orphan still holding a
# cargo build would fight the verifier for the same target directory and the same
# four CPUs. So anything the arms could have started is reaped by name first.
docker exec "$CONTAINER" sh -c \
  'pkill -f aforge; pkill -f "pi/dist/bundle"; pkill -f opencode; sleep 3;
   pkill -9 -f aforge; pkill -9 -f "pi/dist/bundle"; pkill -9 -f opencode; sleep 1;
   pkill -9 cargo; pkill -9 rustc; true' >/dev/null 2>&1
sleep 2

# ── the dataset's own verdict ───────────────────────────────────────────────
# tests/ arrives NOW and not one second earlier — see the header. It is copied
# rather than mounted because docker cannot add a bind mount to a live container,
# and the container has to be the live one: the verifier scores the workspace the
# agent actually left behind, target directory and all.
docker cp "$TASKDIR/tests" "$CONTAINER:/tests" >/dev/null || { say "could not stage /tests"; echo INVALID > "$CELL/outcome"; }
VSTART=$(date +%s)
timeout "$VERIFIER_TIMEOUT" docker exec -e "WORKDIR=$WORKDIR" "$CONTAINER" bash /oneroad-verify.sh \
  > "$CELL/verify.log" 2>&1
VCODE=$?
VWALL=$(( $(date +%s) - VSTART ))
say "$ARM/$TASK: verifier exit $VCODE in ${VWALL}s"

# ── the record ──────────────────────────────────────────────────────────────
# OWNERSHIP IS HANDED BACK BEFORE ANYTHING READS, not only in cleanup(): the
# harness creates its session folder as root mode 700, so every host-side reader
# — record.py's road columns, road.py's timeline — otherwise sees an unreadable
# directory and reports `road=unreadable` about a store sitting right there.
docker exec "$CONTAINER" chown -R "$HOST_UID:$HOST_GID" /prof /chome /logs /peer 2>/dev/null

MODEL="$MODEL" IMAGE_REF="$IMAGE" IMAGE_ID="$IMAGE_ID" TASK_COMMIT="$TASK_COMMIT" \
AFORGE_BUILD_COMMIT="${AFORGE_BUILD_COMMIT:-}" \
WORKDIR="$WORKDIR" NEW_BIN="$NEW_BIN" CELL_SECONDS="$CELL_SECONDS" \
python3 "$MAR/record.py" "$CELL" "$ARM" "$TASK" "$SEED" "$WALL" "$CODE" "$VWALL" "$VCODE"
cleanup
trap - EXIT INT TERM
say "$ARM/$TASK: done — $CELL"
