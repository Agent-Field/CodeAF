#!/usr/bin/env bash
# tui-fake — a terminal program that behaves like a chat TUI, so the tmux door
# can be tested without a model.
#
# It draws the markers the real pi TUI draws — a status line carrying
# "(openrouter)" and a "⠙ Working..." line with an "Elapsed Ns" counter while
# something is in flight — because the driver is then exercised against the SAME
# regexes it uses in production rather than against ones written for the test.
#
# It reads what is typed at it, which is the whole point: a followup sent while
# it is working has to arrive, and the test proves it arrived by what the fake
# prints in response. Two modes are failures the driver has to notice:
#
#   neverbusy   it answers instantly and never shows a busy window, so a
#               scenario that depends on interrupting work did not happen and
#               must not be recorded as though it did
#   deaf        it shows a busy window and ignores anything typed during it —
#               the dropped-followup failure, which must fail the cell
set -uo pipefail

MODE="${FAKE_TUI_MODE:-ok}"
BUSY_SECONDS="${FAKE_TUI_BUSY:-8}"

# The rig asks every binary its version and asks its catalog about the pin
# before it opens any door. Both questions are answered and exited here: a fake
# that fell through to the read loop for `--version` would hang the run that was
# only trying to record which build it measured.
while [ $# -gt 0 ]; do
  case "$1" in
    --version|-v) echo "0.0.0-fake-tui"; exit 0 ;;
    --list-models)
      printf 'provider     model                                  context\n'
      printf 'openrouter   %s   1M\n' "${FAKE_CATALOG_ID:-deepseek/deepseek-v4-flash-0731}"
      exit 0
      ;;
  esac
  shift
done

status() { printf '\n(openrouter) fake/model • low\n'; }

# clean strips the bracketed-paste wrapper tmux puts around a pasted message,
# and any stray escape bytes, so the text can be matched.
clean() { printf '%s' "$1" | tr -d '\033' | sed 's/\[200~//g; s/\[201~//g'; }

[ -n "${FAKE_MARKER:-}" ] && date +%s >> "$FAKE_MARKER"

# A real TUI repaints rather than appends, so a finished turn's spinner is gone
# from the screen. The fake repaints too: leaving "Working..." on the pane after
# the work ended would make the driver wait for an idle that has already
# happened, which is a property of this fake and not of any harness.
repaint() { printf '\033[2J\033[H'; }

echo "fake TUI ready"
status

first_done=0
while :; do
  line=""
  if ! IFS= read -r -t 300 line; then break; fi
  line="$(clean "$line")"
  [ -n "$line" ] || continue
  printf 'you said: %s\n' "$line"

  if [ "$MODE" = "neverbusy" ]; then
    printf 'answered without doing any work\n'
    status
    continue
  fi

  if [ "$first_done" = "0" ]; then
    first_done=1
    # The busy window. The build runs for real in the background so that the
    # scenario's own file assertions have something true to check.
    if [ -x ./slow-build.sh ]; then ./slow-build.sh > slow-build.out 2>&1 & fi
    started="$(date +%s)"
    followup=""
    while [ $(( $(date +%s) - started )) -lt "$BUSY_SECONDS" ]; do
      repaint
      printf '⠙ Working...\nElapsed %ss\n' "$(( $(date +%s) - started ))"
      # Anything typed during the busy window is picked up here — this is the
      # behaviour under test.
      if IFS= read -r -t 1 typed; then
        typed="$(clean "$typed")"
        [ -n "$typed" ] && followup="$typed"
      fi
    done
    wait 2>/dev/null || true
    repaint
    if [ -n "$followup" ] && [ "$MODE" != "deaf" ]; then
      printf 'while working, you asked: %s\n' "$followup"
      # Answer it from the fixture, the way a harness would have to.
      if printf '%s' "$followup" | grep -qi 'checksum'; then
        printf 'the checksum word is %s\n' "$(grep -oE '[A-Z]{6,}' NOTES.txt 2>/dev/null | head -1)"
      fi
      if printf '%s' "$followup" | grep -qi 'csv'; then
        printf 'service,port\n' > report.csv
        sed 's/ /,/' services.txt >> report.csv 2>/dev/null
        rm -f report.md
        printf 'rewrote it as CSV\n'
      fi
    fi
    [ -f build.log ] && printf 'the build finished: %s\n' "$(cat build.log)"
    status
    continue
  fi

  # A later turn, asked once things have settled.
  if printf '%s' "$line" | grep -qi 'total'; then
    awk '{ sum += $2 } END { print sum }' inventory.txt > total.txt 2>/dev/null
    printf 'the result was %s\n' "$(cat total.txt 2>/dev/null)"
  fi
  status
done
