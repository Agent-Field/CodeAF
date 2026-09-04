#!/usr/bin/env bash
# tui-fake — a terminal program that behaves like a chat TUI, so the interactive
# door can be tested without a model.
#
# It draws the markers the real pi TUI draws, so the driver is exercised against
# the SAME regexes it uses in production. What it does with a message typed
# while work is running is the point, and FAKE_TUI_MODE chooses:
#
#   ok          answers the followup WHILE the build runs, and lets the build
#               finish. This is the behaviour the cell is meant to pass.
#   blocking    reads nothing until the build has finished, then answers
#               correctly and reports the build. A transcript-only check calls
#               this a pass; it is the false green root found, and this cell
#               must FAIL it.
#   neverbusy   answers instantly and never runs the build at all, so no
#               mid-work window ever exists — unexercised, which is a failure
#               and not a success.
#   deaf        runs the build and ignores anything typed during it.
#   smudged     answers with the right word carrying an extra leading letter
#               ("RRABANNIC"), which a live omp pane really produced. A
#               substring match calls it correct; the cell must not.
#   noready     draws a screen the driver's ready marker does not match, and
#               stays alive. This is a calibration gap in the rig, not a crash.
set -uo pipefail

MODE="${FAKE_TUI_MODE:-ok}"
# How long the fake holds the turn. It must outlast the build, so that the
# blocking mode is genuinely still holding the turn when the work ends.
BUSY_SECONDS="${FAKE_TUI_BUSY:-12}"

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

# answer_followup replies the way the scenarios ask: the checksum word reversed
# (which an echo of the file cannot produce), or the CSV the revision wants.
answer_followup() {
  local asked="$1"
  printf 'you asked while working: %s\n' "$asked"
  if printf '%s' "$asked" | grep -qiE 'backwards|reversed|checksum'; then
    local word answer
    word="$(grep -oE '[A-Z]{6,}' NOTES.txt 2>/dev/null | head -1)"
    answer="$(printf '%s' "$word" | rev)"
    # The near miss: one duplicated leading character, everything else right.
    [ "$MODE" = "smudged" ] && answer="${answer:0:1}$answer"
    printf 'the reversed checksum word is %s\n' "$answer"
  fi
  if printf '%s' "$asked" | grep -qi 'csv'; then
    printf 'service,port\n' > report.csv
    sed 's/ /,/' services.txt >> report.csv 2>/dev/null
    rm -f report.md
    printf 'rewrote it as report.csv\n'
  fi
}

if [ "$MODE" = "noready" ]; then
  # A perfectly healthy TUI whose composer this rig has never been calibrated
  # against. It draws, it waits, and it never dies.
  printf 'a different harness, drawing a screen nobody taught this rig to read\n'
  while :; do
    if ! IFS= read -r -t 300 line; then break; fi
    printf 'ignored: %s\n' "$(clean "$line")"
  done
  exit 0
fi

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
      if [ "$MODE" = "blocking" ]; then
        # A foreground agent: nothing typed is even read until the work ends.
        sleep 1
        continue
      fi
      # Anything typed during the busy window is picked up here — this is the
      # behaviour under test.
      if IFS= read -r -t 1 typed; then
        typed="$(clean "$typed")"
        if [ -n "$typed" ]; then
          followup="$typed"
          # Answered WHILE the build is still running, which is what separates
          # this from the blocking mode below.
          [ "$MODE" != "deaf" ] && answer_followup "$followup"
        fi
      fi
    done
    wait 2>/dev/null || true
    repaint
    if [ "$MODE" = "blocking" ]; then
      # Now that the work is over, read what was queued and answer it. Correct
      # word, correct build marker, and far too late.
      if IFS= read -r -t 2 typed; then
        typed="$(clean "$typed")"
        [ -n "$typed" ] && answer_followup "$typed"
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
