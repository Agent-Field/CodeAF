#!/usr/bin/env bash
# The chat door for the DeepSWE rig: `aforge chat --yolo` in a real terminal,
# INSIDE the task's own container, driven the way a person meets it — the brief
# pasted into the composer, enter, and then waiting.
#
# It is the sibling of the `do` door in run.sh, and the two are kept as alike as
# the surfaces allow: same container, same binary, same home layout under
# /bench/home, same brief byte for byte, same rig wall. What differs is what the
# product does with it, which is the whole measurement.
#
# THE TERMINAL IS ON THE HOST, THE PROCESS IS IN THE CONTAINER. tmux runs on the
# host and its window is `docker exec -it` into the task container, so the
# surface gets a real pty and the host can type at it and read its screen with
# the same send-keys/capture-pane the canary (bench/canary/lib/chat.sh) uses.
# The home stays inside the container rather than being bind-mounted, because a
# session host listens on a unix socket under the home and a socket on a macOS
# bind mount is not a thing every file-sharing backend allows. So the witnesses
# are read through `docker cp` on every poll: the journal under v3/ and the call
# log under logs/, copied out to <out>/home-live and read by journal.py there.
#
# THE WORK IS OVER WHEN THREE WITNESSES AGREE, exactly as the canary has it: the
# journal holds a non-aux `usage` seal (the turn ended), the screen says nothing
# is still being worked (status line idle AND no `N running` on the rail), and
# the call log has been quiet for CHAT_QUIET seconds. The call log's in-flight
# count is deliberately not a witness (see the canary's file for the two ways it
# lies). A quiet call log under a screen that still claims to be working for
# CHAT_STALL seconds is a stall; the rig's wall is the wall.
#
# A QUESTION IS NOT THE END OF THE CELL. `--yolo` opens the tool gate; it does
# not stop the steward from putting a card in front of the person ("needs your
# look", "[a] accept"). A person sitting in front of the chat would answer, and
# this rig is built to have one — a monitor watching the tmux session named in
# <out>/tmux-session.txt. So an asked frame is recorded (asked_s) and the cell
# waits CHAT_ASK_GRACE seconds for the monitor to answer before it ends as
# `asked`. Every answer a monitor gives is the monitor's to write down.
#
# usage: deepswe_chat <container> <out-dir> <prompt-file> <model> <cap-usd> <wall-s> <launch-script>
# needs: EMU_ARGS (lib.sh's emu_args), journal.py beside this file
# writes: <out>/door.json, <out>/screen.txt, <out>/frames.log, <out>/home/ (the
#         whole state root, copied out at the end), <out>/tmux-session.txt

CHAT_LIB="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CHAT_POLL="${CHAT_POLL:-5}"
CHAT_QUIET="${CHAT_QUIET:-20}"
# How far inside the rig's wall aforge's own wall sits (seconds), so the session
# ends on its own law ("hours ran out") and writes its ending before the rig
# stops watching.
CHAT_WALL_MARGIN="${CHAT_WALL_MARGIN:-60}"
CHAT_STALL="${CHAT_STALL:-600}"
# The surface has to draw its first idle frame before it can be typed at, and
# under emulation a first frame is slower than on the host — the catalog is
# fetched, the session host is started, all of it under Rosetta or qemu.
CHAT_FRAME_WAIT="${CHAT_FRAME_WAIT:-300}"
CHAT_ASK_GRACE="${CHAT_ASK_GRACE:-300}"

chat_asked_frame() { # <frame> — a card is waiting on a person
  local frame="$1"
  printf '%s\n' "$frame" | grep -q '\[a\] accept' &&
    printf '%s\n' "$frame" | tail -1 | grep -q ' · idle' &&
    ! printf '%s\n' "$frame" | grep -Eq '[0-9]+ running'
}

chat_blocked_on_line() {
  python3 -c 'import re, sys
for line in sys.stdin:
    if "needs your look" in line:
        line = "".join(ch for ch in line if not "\u2500" <= ch <= "\u257f")
        print(re.sub(r"\s+", " ", line).strip())
        break'
}

chat_snapshot_home() { # <container> <dir> — the two witnesses, copied out
  local c="$1" dir="$2"
  mkdir -p "$dir/logs"
  docker cp "$c:/bench/home/logs/calls.jsonl" "$dir/logs/calls.jsonl" >/dev/null 2>&1 || true
  rm -rf "$dir/v3.new"; mkdir -p "$dir/v3.new"
  if docker cp "$c:/bench/home/v3/projects" "$dir/v3.new/projects" >/dev/null 2>&1; then
    rm -rf "$dir/v3"; mv "$dir/v3.new" "$dir/v3"
  else
    rm -rf "$dir/v3.new"
  fi
}

deepswe_chat() {
  local cname="$1" out="$2" prompt="$3" model="$4" cap="$5" wall="$6" launch="$7"
  local sess="deepswe-chat-$$-$(basename "$out" | cut -c1-40)"
  local live="$out/home-live"
  local ended="" started now seals quiet screen working task_done_s="" asked_s="" asked_since="" blocked_on=""
  local calls_sig="" last_change
  local hours
  hours=$(python3 -c "print(round(max($wall - $CHAT_WALL_MARGIN, 60) / 3600, 4))")
  printf '%s\n' "$sess" > "$out/tmux-session.txt"
  mkdir -p "$live"

  # The launch script is what the tmux window runs: docker exec into the task
  # container with the emulation guard and the home, then the surface itself.
  # It is a file rather than a command string so that nothing here goes through
  # two layers of shell quoting on its way to a pty.
  {
    echo '#!/usr/bin/env bash'
    printf 'exec docker exec -it -w /app'
    printf ' -e AFORGE_HOME=/bench/home -e HOME=/root -e TERM=xterm-256color'
    printf ' -e OPENROUTER_API_KEY=%q' "$KEY"
    local e; for e in "${EMU_ARGS[@]}"; do printf ' %q' "$e"; done
    printf ' %q /usr/local/bin/aforge chat -yolo -one-model -model %q -max-cost %q -max-hours %q\n' \
      "$cname" "$model" "$cap" "$hours"
  } > "$launch"
  chmod 700 "$launch"

  tmux kill-session -t "=$sess" 2>/dev/null
  tmux new-session -d -s "$sess" "sleep $((wall + 900))" 2>>"$out/tmux.err"
  tmux resize-window -t "=$sess" -x 140 -y 45
  tmux respawn-window -k -t "=$sess" "bash '$launch'"

  started=$(date +%s)
  until tmux capture-pane -p -t "=$sess:" 2>/dev/null | grep -q ' · idle'; do
    if [ $(( $(date +%s) - started )) -ge "$CHAT_FRAME_WAIT" ]; then ended="noframe"; break; fi
    if [ "$(tmux list-panes -t "=$sess" -F '#{pane_dead}' 2>/dev/null)" = "1" ]; then ended="crash"; break; fi
    sleep 2
  done
  tmux capture-pane -p -t "=$sess:" > "$out/first-frame.txt" 2>/dev/null

  if [ -z "$ended" ]; then
    # A bracketed paste hands over a multi-line brief without every newline
    # being an enter; the surface reads the whole bracket as one text.
    tmux load-buffer -b "$sess" "$prompt"
    tmux paste-buffer -d -p -b "$sess" -t "=$sess:"
    sleep 2
    tmux send-keys -t "=$sess:" Enter
    started=$(date +%s); last_change=$started
    : > "$out/frames.log"
    while :; do
      sleep "$CHAT_POLL"
      now=$(date +%s)
      screen="$(tmux capture-pane -p -t "=$sess:" 2>/dev/null)"
      printf '[%s] %s\n' "$(date -u +%H:%M:%S)" "$(printf '%s\n' "$screen" | tail -1)" >> "$out/frames.log"
      if [ "$(tmux list-panes -t "=$sess" -F '#{pane_dead}' 2>/dev/null)" = "1" ]; then ended="crash"; break; fi
      chat_snapshot_home "$cname" "$live"
      seals=$(python3 "$CHAT_LIB/journal.py" seals "$live" 2>/dev/null || echo 0)
      # The quiet clock runs on the call log's CONTENT, not its mtime: docker cp
      # rewrites the copy on every poll, so the copy's mtime says nothing.
      local sig; sig="$(wc -c < "$live/logs/calls.jsonl" 2>/dev/null || echo 0)"
      if [ "$sig" != "$calls_sig" ]; then calls_sig="$sig"; last_change=$now; fi
      quiet=$(( now - last_change ))
      if [ $(( now - started )) -ge "$wall" ]; then ended="wall"; break; fi
      if [ -z "$task_done_s" ] && printf '%s\n' "$screen" | grep -Eq '[0-9]+ done' && ! printf '%s\n' "$screen" | grep -Eq '[0-9]+ running'; then
        task_done_s=$(( now - started ))
      fi
      if chat_asked_frame "$screen" && [ "$quiet" -ge "$CHAT_QUIET" ]; then
        [ -n "$asked_s" ] || asked_s=$(( now - started ))
        [ -n "$asked_since" ] || asked_since=$now
        blocked_on="$(printf '%s\n' "$screen" | chat_blocked_on_line)"
        if [ $(( now - asked_since )) -ge "$CHAT_ASK_GRACE" ]; then ended="asked"; break; fi
        continue
      fi
      asked_since=""
      working=0
      printf '%s\n' "$screen" | tail -1 | grep -q ' · idle' || working=1
      printf '%s\n' "$screen" | grep -Eq '[0-9]+ running' && working=1
      if [ "$seals" -ge 1 ] && [ "$working" -eq 0 ] && [ "$quiet" -ge "$CHAT_QUIET" ]; then ended="self"; break; fi
      if [ "$quiet" -ge "$CHAT_STALL" ] && [ "$working" -eq 1 ]; then ended="stall"; break; fi
    done
  fi

  tmux capture-pane -p -t "=$sess:" > "$out/screen.txt" 2>/dev/null
  tmux send-keys -t "=$sess:" C-c; sleep 0.5; tmux send-keys -t "=$sess:" C-c; sleep 2
  tmux kill-session -t "=$sess" 2>/dev/null
  # The whole state root comes out for the record — transcript, call log,
  # session meta — and the key is scrubbed from the copy on the way.
  rm -rf "$out/home"; docker cp "$cname:/bench/home" "$out/home" >/dev/null 2>&1 || true
  python3 - "$out/home/config.json" <<'PY' 2>/dev/null
import json, sys
try:
    p = sys.argv[1]; d = json.load(open(p)); d["api_key"] = "<scrubbed>"; json.dump(d, open(p, "w"), indent=1)
except Exception:
    pass
PY
  chat_snapshot_home "$cname" "$live"

  DS_ENDED="$ended" DS_WALL="$(( $(date +%s) - started ))" DS_TASK_DONE="$task_done_s" \
  DS_ASKED_S="$asked_s" DS_BLOCKED_ON="$blocked_on" DS_HOME="$live" DS_LIB="$CHAT_LIB" \
  DS_OUT="$out/door.json" DS_SESS="$sess" DS_HOURS="$hours" DS_CAP="$cap" python3 - <<'PY'
import json, os, subprocess
lib, home = os.environ["DS_LIB"], os.environ["DS_HOME"]
ended, wall_s = os.environ["DS_ENDED"], int(os.environ["DS_WALL"])
task_done_s = int(os.environ["DS_TASK_DONE"]) if os.environ["DS_TASK_DONE"] else None
asked_s = int(os.environ["DS_ASKED_S"]) if os.environ["DS_ASKED_S"] else None
def ask(word):
    return subprocess.run(["python3", os.path.join(lib, "journal.py"), word, home],
                          capture_output=True, text=True).stdout.strip()
json.dump({
    "door": "chat",
    "ended": ended,
    "wall_s": wall_s,
    "task_done_s": task_done_s,
    "done_to_wall_s": wall_s - task_done_s if ended == "wall" and task_done_s is not None else None,
    "cost_usd": float(ask("cost") or 0),
    "ttft_ms": int(ask("ttft") or 0) or None,
    "calls": int(ask("calls") or 0),
    "turns": int(ask("seals") or 0),
    "max_hours": float(os.environ["DS_HOURS"]),
    "max_cost": float(os.environ["DS_CAP"]),
    "tmux_session": os.environ["DS_SESS"],
    **({"asked_s": asked_s} if asked_s is not None else {}),
    **({"blocked_on": os.environ["DS_BLOCKED_ON"]} if os.environ["DS_BLOCKED_ON"] else {}),
}, open(os.environ["DS_OUT"], "w"), indent=1)
PY
}
