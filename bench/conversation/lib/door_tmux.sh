#!/usr/bin/env bash
# door_tmux.sh — the interactive door: a real terminal, real keystrokes.
#
# This is the only door in this suite that may be described as conversation. A
# `--print` invocation is one message in and one reply out; it cannot be
# interrupted, cannot be revised halfway, and cannot be typed at while it works.
# Those are the three things people actually do, so they are measured here or
# not claimed at all.
#
# The driver is deliberately dumb about what a harness means and precise about
# what it sees. It knows two regexes per arm (adapters.sh): one that says a
# composer is up, and one that says something is in flight. Everything else is
# timing. An arm without calibrated regexes has no interactive door here and its
# cells are recorded `unsupported` — never run through the print door instead,
# because a print row wearing an interactive label is the exact lie this suite
# is built to avoid.
#
# THE TURN PLAN IS THE SCENARIO'S SCRIPT. One line per message:
#
#   ready<TAB>text    send once the composer is up (the opening message)
#   busy<TAB>text     send WHILE the harness is working — the followup case.
#                     If no busy window is ever observed, the cell records
#                     `no-busy-window` and fails: the thing under test did not
#                     happen, so there is nothing to pass.
#   idle<TAB>text     send after the harness has settled (the next-turn case)
#
# Evidence written to <out>:
#   screen.txt        the final pane
#   scrollback.txt    the whole conversation, which is what assertions read
#   frames.log        one status line per poll, and a full frame per turn
#   door.json         what the driver observed, in numbers

CONV_POLL="${CONV_POLL:-2}"
# How long the busy marker must have been absent before a turn counts as over.
CONV_QUIET="${CONV_QUIET:-12}"
# How long to wait for a composer before giving up on the session entirely.
CONV_READY_WAIT="${CONV_READY_WAIT:-90}"
# How long to wait for the harness to start working after a message is sent.
CONV_BUSY_WAIT="${CONV_BUSY_WAIT:-120}"

DOOR_ENDED=""
DOOR_WALL_S=0
DOOR_TURNS_SENT=0
DOOR_BUSY_OBSERVED=0
DOOR_ASK_OBSERVED=0

# pane_text prints what is on the screen right now.
pane_text() { tmux capture-pane -p -t "=$1:" 2>/dev/null; }

# pane_scrollback prints the whole session, not only the last page. Assertions
# read this: a reply that scrolled off the top is still a reply that was given.
pane_scrollback() { tmux capture-pane -p -S -5000 -t "=$1:" 2>/dev/null; }

pane_dead() { [ "$(tmux list-panes -t "=$1" -F '#{pane_dead}' 2>/dev/null)" = "1" ]; }

screen_matches() { printf '%s\n' "$2" | grep -aqE -- "$1"; }

# door_is_busy and door_is_ready are the two questions the driver can ask the
# screen. Both are per-arm regexes stated, with their provenance, in adapters.sh.
door_is_busy() { [ -n "$ARM_BUSY_RE" ] && screen_matches "$ARM_BUSY_RE" "$1"; }
door_is_ready() { [ -z "$ARM_READY_RE" ] || screen_matches "$ARM_READY_RE" "$1"; }

# door_send types one message the way a person does: a bracketed paste so that
# the newlines inside a paragraph are not each an Enter, then Enter once.
door_send() {
  local name="$1" text="$2" buffer
  buffer="$(mktemp)"
  printf '%s' "$text" > "$buffer"
  tmux load-buffer -b "$name" "$buffer"
  tmux paste-buffer -d -p -b "$name" -t "=$name:"
  rm -f "$buffer"
  sleep 1
  tmux send-keys -t "=$name:" Enter
  DOOR_TURNS_SENT=$((DOOR_TURNS_SENT + 1))
}

# door_wait_busy waits for the harness to start working. Returns 1 when it never
# does — which is a finding about the cell, not a reason to wait until the cap.
door_wait_busy() {
  local name="$1" deadline=$(( $(now_s) + CONV_BUSY_WAIT )) screen
  while [ "$(now_s)" -lt "$deadline" ]; do
    screen="$(pane_text "$name")"
    if door_is_busy "$screen"; then DOOR_BUSY_OBSERVED=1; return 0; fi
    pane_dead "$name" && return 1
    sleep "$CONV_POLL"
  done
  return 1
}

# door_wait_idle waits until the busy marker has been gone for CONV_QUIET
# seconds and a composer is back. A single quiet poll is not enough: every one
# of these TUIs has gaps between a model call and the tool call it asked for.
door_wait_idle() {
  local name="$1" cap_at="$2" quiet_since="" screen now
  while :; do
    now="$(now_s)"
    [ "$now" -ge "$cap_at" ] && { DOOR_ENDED="cap"; return 1; }
    pane_dead "$name" && { DOOR_ENDED="crash"; return 1; }
    screen="$(pane_text "$name")"
    printf '%s\n' "$screen" | tail -1 >> "$DOOR_OUT/frames.log"
    if [ -n "$ARM_ASK_RE" ] && screen_matches "$ARM_ASK_RE" "$screen"; then
      DOOR_ASK_OBSERVED=1
    fi
    if door_is_busy "$screen"; then
      quiet_since=""
    else
      if door_is_ready "$screen"; then
        [ -n "$quiet_since" ] || quiet_since="$now"
        if [ $(( now - quiet_since )) -ge "$CONV_QUIET" ]; then return 0; fi
      else
        quiet_since=""
      fi
    fi
    sleep "$CONV_POLL"
  done
}

# tmux_door_run drives one cell. ARGV, ARM_ENV and the marker regexes must be
# set by the caller (adapters.sh) before it is called.
#
#   tmux_door_run <arm> <work-dir> <cap-seconds> <out-dir> <turn-plan-file>
tmux_door_run() {
  local arm="$1" work="$2" cap="$3" out="$4" plan="$5"
  DOOR_OUT="$out"
  DOOR_ENDED=""
  DOOR_TURNS_SENT=0
  DOOR_BUSY_OBSERVED=0
  DOOR_ASK_OBSERVED=0
  mkdir -p "$out"
  : > "$out/frames.log"

  local name="afconv-$$-$arm-$(basename "$out")"
  # Every target is spelled `=name` so that a bare prefix match can never reach
  # another run's session, and pane-level commands add the colon: tmux answers
  # `can't find pane` for a bare session target on capture-pane.
  tmux kill-session -t "=$name" 2>/dev/null

  # tmux takes one shell string, so the command is built with printf %q — that
  # is what keeps a prompt containing quotes from becoming three arguments. The
  # environment is the built one (common.sh: child_env), not this shell's.
  local command part
  # TERM is appended last so it wins over whatever this shell had: a TUI opened
  # under TERM=dumb draws nothing and the cell reports `noframe` for a reason
  # that has nothing to do with the harness.
  command="$(child_env "${ARM_ENV[@]}" TERM=xterm-256color)"
  for part in "${ARGV[@]}"; do command="$command $(printf '%q' "$part")"; done
  printf '%s\n' "$command" > "$out/door-command.txt"

  local started; started="$(now_s)"
  local cap_at=$(( started + cap ))
  tmux new-session -d -s "$name" -c "$work" -x 140 -y 45 \
    "$command" 2>>"$out/tmux.err"

  # A session that never draws a composer is a cell with nothing in it. The
  # reason is in tmux.err or nowhere.
  local screen
  while :; do
    screen="$(pane_text "$name")"
    door_is_ready "$screen" && break
    if [ "$(now_s)" -ge $(( started + CONV_READY_WAIT )) ]; then DOOR_ENDED="noframe"; break; fi
    if pane_dead "$name"; then DOOR_ENDED="crash"; break; fi
    sleep 1
  done

  if [ -z "$DOOR_ENDED" ]; then
    local when text
    while IFS="$(printf '\t')" read -r when text; do
      [ -n "$when" ] || continue
      case "$when" in
        \#*) continue ;;
        ready)
          door_send "$name" "$text"
          ;;
        busy)
          # The followup case: wait until it is demonstrably working, then type.
          if door_wait_busy "$name"; then
            printf '=== busy frame ===\n%s\n' "$(pane_text "$name")" >> "$out/frames.log"
            door_send "$name" "$text"
          else
            # Nothing was in flight to interrupt, so the scenario did not
            # happen. Send nothing, and say so.
            DOOR_ENDED="no-busy-window"
            break
          fi
          ;;
        idle)
          door_wait_idle "$name" "$cap_at" || break
          printf '=== idle frame ===\n%s\n' "$(pane_text "$name")" >> "$out/frames.log"
          door_send "$name" "$text"
          ;;
        *)
          conv_warn "turn plan: unknown timing '$when'"
          ;;
      esac
    done < "$plan"
  fi

  if [ -z "$DOOR_ENDED" ]; then
    if door_wait_idle "$name" "$cap_at"; then DOOR_ENDED="idle"; fi
  fi

  pane_text "$name" > "$out/screen.txt" 2>/dev/null
  pane_scrollback "$name" > "$out/scrollback.txt" 2>/dev/null
  # Leaving the session the way a person would, then ending the session that
  # was started here — and only that one. On a shared box a pattern kill is how
  # somebody else's run dies.
  tmux send-keys -t "=$name:" C-c 2>/dev/null; sleep 0.5
  tmux send-keys -t "=$name:" C-c 2>/dev/null; sleep 1
  tmux kill-session -t "=$name" 2>/dev/null

  DOOR_WALL_S=$(( $(now_s) - started ))
  cat > "$out/door.json" <<JSON
{
 "door": "tui-tmux",
 "arm": $(json_str "$arm"),
 "ended": $(json_str "$DOOR_ENDED"),
 "wall_s": $DOOR_WALL_S,
 "turns_sent": $DOOR_TURNS_SENT,
 "busy_observed": $DOOR_BUSY_OBSERVED,
 "ask_observed": $DOOR_ASK_OBSERVED,
 "ready_re": $(json_str "$ARM_READY_RE"),
 "busy_re": $(json_str "$ARM_BUSY_RE"),
 "marker_provenance": $(json_str "$ARM_DOOR_NOTE")
}
JSON
}
