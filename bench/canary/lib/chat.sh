#!/usr/bin/env bash
# canary_chat runs one cell through the conversation surface the way a person
# meets it: the binary in a real terminal, the issue pasted into the composer,
# enter, and then waiting.
#
# THE WORK IS OVER WHEN THREE WITNESSES AGREE. The journal says the turn ended
# (a non-aux `usage` row, internal/session's seal); the screen says nothing is
# still being worked — the status line reads idle AND the rail shows no task
# running, because a reply may end by handing the issue to a task that works on
# after the seal; and the call log has been quiet for a moment. The call log's
# own in-flight count is deliberately NOT a witness, because it is wrong in
# both directions: a start row can be left with no end row by a logging fault
# alone (#334 dropped the row on a +Inf figure, fixed by #357), and a handoff
# refused before the wire writes no row at all (#443: no model for the role
# under --one-model, so nothing was ever sent). A rule that waited on that
# count would wait until the wall. A call log quiet for STALL seconds while
# something on the screen still claims to be working is a stall, and the wall
# is the wall.
#
# Every rig is named by its own pid and cell so that several cells, and several
# sessions of this rig on one box, never reach each other's tmux sessions — and
# every target is spelled `=name`, because a bare -t prefix-matches, with a
# trailing colon added wherever tmux wants a pane rather than a session.
#
# usage: canary_chat <bin> <home> <work> <prompt-file> <model> <cap-usd> <wall-s> <out-dir>
# writes: <out-dir>/door.json, <out-dir>/screen.txt, <out-dir>/frames.log

# Resolve this when the file is sourced, because otherwise the witness reads
# the wrong directory and the cell waits until the wall.
CHAT_LIB="${CANARY_LIB:-$(cd "$(dirname "${BASH_SOURCE[0]:-}")" && pwd)}"
if [ ! -r "$CHAT_LIB/journal.py" ]; then
  echo "lib/chat.sh must be sourced under bash from a script or with CANARY_LIB set to the lib directory" >&2
  return 1
fi

CHAT_POLL="${CHAT_POLL:-3}"
CHAT_QUIET="${CHAT_QUIET:-20}"
CHAT_STALL="${CHAT_STALL:-420}"
CHAT_FRAME_WAIT="${CHAT_FRAME_WAIT:-60}"

canary_chat() {
  local bin="$1" home="$2" work="$3" prompt="$4" model="$5" cap="$6" wall="$7" out="$8"
  local here="$CHAT_LIB"
  local name="canary-$$-$(basename "$out")"
  local calls="$home/logs/calls.jsonl"
  local ended="" started now seals quiet screen working

  tmux kill-session -t "=$name" 2>/dev/null
  # Its stderr is kept: a session that never comes up is a cell that says
  # `noframe` and nothing else, and the reason is here or nowhere.
  tmux new-session -d -s "$name" -c "$work" "sleep $((wall + 300))" 2>>"$out/tmux.err"
  tmux resize-window -t "=$name" -x 140 -y 45
  tmux respawn-window -k -t "=$name" -c "$work" \
    "env AFORGE_HOME='$home' PATH='$PATH' TERM=xterm-256color '$bin' chat -yolo -one-model -model '$model' -max-cost '$cap'"

  # The composer has to be up before it can be typed at; the status line's
  # idle word is the frame that says so.
  started=$(date +%s)
  # A pane-level command refuses a bare session target — `capture-pane -t
  # "=name"` answers `can't find pane` on tmux 3.4 — so every capture-pane,
  # send-keys and paste-buffer below spells the pane out as `=name:`: this exact
  # session, its current window, its active pane. The session-level commands
  # (kill-session, resize-window, respawn-window, list-panes) keep `=name`.
  until tmux capture-pane -p -t "=$name:" 2>/dev/null | grep -q ' · idle'; do
    if [ $(( $(date +%s) - started )) -ge "$CHAT_FRAME_WAIT" ]; then ended="noframe"; break; fi
    if [ "$(tmux list-panes -t "=$name" -F '#{pane_dead}' 2>/dev/null)" = "1" ]; then ended="crash"; break; fi
    sleep 1
  done

  if [ -z "$ended" ]; then
    # A bracketed paste is how a terminal hands over a multi-line issue without
    # every newline being an enter.
    tmux load-buffer -b "$name" "$prompt"
    tmux paste-buffer -d -p -b "$name" -t "=$name:"
    sleep 1
    tmux send-keys -t "=$name:" Enter
    started=$(date +%s)
    : > "$out/frames.log"
    while :; do
      sleep "$CHAT_POLL"
      now=$(date +%s)
      tmux capture-pane -p -t "=$name:" 2>/dev/null | tail -1 >> "$out/frames.log"
      if [ "$(tmux list-panes -t "=$name" -F '#{pane_dead}' 2>/dev/null)" = "1" ]; then ended="crash"; break; fi
      if [ $(( now - started )) -ge "$wall" ]; then ended="wall"; break; fi
      seals=$(python3 "$here/journal.py" seals "$home")
      quiet=$(( now - $(stat -c %Y "$calls" 2>/dev/null || echo "$now") ))
      screen="$(tmux capture-pane -p -t "=$name:" 2>/dev/null)"
      working=0
      echo "$screen" | tail -1 | grep -q ' · idle' || working=1
      echo "$screen" | grep -Eq '[0-9]+ running' && working=1
      if [ "$seals" -ge 1 ] && [ "$working" -eq 0 ] && [ "$quiet" -ge "$CHAT_QUIET" ]; then ended="self"; break; fi
      if [ "$quiet" -ge "$CHAT_STALL" ] && [ "$working" -eq 1 ]; then ended="stall"; break; fi
    done
  fi

  tmux capture-pane -p -t "=$name:" > "$out/screen.txt" 2>/dev/null
  tmux send-keys -t "=$name:" C-c; sleep 0.5; tmux send-keys -t "=$name:" C-c; sleep 1
  tmux kill-session -t "=$name" 2>/dev/null
  # THE RIG SIGNALS ONLY WHAT IT LAUNCHED. Killing the tmux session ends the
  # process it spawned; a session host that detached from it is recorded by
  # the socket it listens on under this cell's home, never hunted by pattern —
  # on a shared box a pattern kill is how somebody else's run dies.
  ss -xlp 2>/dev/null | grep -F "$home" > "$out/listeners.txt" || true

  CANARY_ENDED="$ended" CANARY_WALL="$(( $(date +%s) - started ))" CANARY_HOME="$home" \
  CANARY_LIB="$here" CANARY_OUT="$out/door.json" python3 - <<'PY'
import json, os, subprocess
lib, home = os.environ["CANARY_LIB"], os.environ["CANARY_HOME"]
def ask(word):
    return subprocess.run(["python3", os.path.join(lib, "journal.py"), word, home],
                          capture_output=True, text=True).stdout.strip()
json.dump({
    "door": "chat",
    "ended": os.environ["CANARY_ENDED"],
    "wall_s": int(os.environ["CANARY_WALL"]),
    "cost_usd": float(ask("cost") or 0),
    "ttft_ms": int(ask("ttft") or 0) or None,
    "calls": int(ask("calls") or 0),
    "turns": int(ask("seals") or 0),
}, open(os.environ["CANARY_OUT"], "w"), indent=1)
PY
}
