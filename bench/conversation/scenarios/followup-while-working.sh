#!/usr/bin/env bash
# followup-while-working — the interactive door, and the thing print mode cannot
# do at all.
#
# A person starts something slow and then, while it runs, asks a second
# question. Two outcomes are worth having and this cell wants both:
#
#   the followup is answered      — the second question got a real answer, from
#                                   a file only the fixture knows about, so the
#                                   answer had to be looked up rather than
#                                   guessed
#   the first job is not lost     — the slow work still finished, and its result
#                                   is still on the screen
#
# A harness that answers the followup by abandoning the build has not done what
# was asked, and neither has one that ignores the person until the build ends.
# Both failures are visible here and neither can be seen through --print.

SCENARIO_WORKLOAD="conversation"
SCENARIO_DOOR="interactive"
# opencode is absent because this suite has no calibrated screen markers for its
# TUI; its cells are recorded `unsupported` rather than run through another door.
SCENARIO_ARMS="aforge omp pi"
SCENARIO_CAP_S="${SCENARIO_CAP_S:-420}"
SCENARIO_GUARDS="a second question answered while the first job is still running"

# shellcheck source=../fixtures/slowwork.sh
source "$CONV_ROOT/fixtures/slowwork.sh"

scenario_fixture() { fixture_slowwork "$1"; }

# The witnesses this scenario gives the driver: the build's own phase markers,
# and the pattern that means the followup was answered.
#
# THE ANSWER IS THE CHECKSUM WORD REVERSED. That is deliberate. CINNABAR appears
# in NOTES.txt, so a tool call that cats the file puts it on the screen without
# anybody having answered anything; RABANNIC cannot come from an echo. It is
# still a screen witness rather than a transcript event — recorded in door.json
# as such — but it cannot be produced by the file passing through the terminal.
scenario_turns() {
  local work="$1" plan="$2"
  SCENARIO_WORK_START="$work/.phases/build-started"
  SCENARIO_WORK_DONE="$work/.phases/build-finished"
  SCENARIO_ANSWER_RE="RABANNIC"
  {
    printf 'ready\tRun ./slow-build.sh in this directory. It takes about a minute. Tell me what it writes when it is done.\n'
    printf 'midwork\tWhile that runs: read NOTES.txt, take the checksum word, and reply with that word spelled backwards in capitals. Just the reversed word.\n'
  } > "$plan"
}

scenario_check() {
  local work="$1" transcript="$2" cell="$3"
  local door="$cell/door.json"
  local sent answered started finished

  sent="$(door_field "$door" midwork_sent_at)"
  answered="$(door_field "$door" answer_first_seen_at)"
  started="$(door_field "$door" work_started_at)"
  finished="$(door_field "$door" work_finished_at)"
  record "witness" "$(door_field "$door" witness)"
  record "work_window_s" "$(stamp_gap "$started" "$finished")"

  # The window has to have existed at all, and the message has to have gone into
  # it. Without this the rest is a two-turn conversation wearing a mid-work label.
  check "the build actually started" "$([ -n "$started" ] && echo 1 || echo 0)"
  check "the followup was sent after the build started" \
    "$(stamps_ordered "$started" "$sent")"
  check "and before the build finished" \
    "$([ -z "$finished" ] && echo 1 || stamps_ordered "$sent" "$finished")"

  # The answer has to have arrived while the build was still running. A harness
  # that queues the question, finishes the build, and then answers has not been
  # steered mid-work — and the old version of this cell passed it.
  check "the followup was answered" "$([ -n "$answered" ] && echo 1 || echo 0)"
  check "and answered BEFORE the build finished" \
    "$(stamps_ordered "$answered" "$finished")"
  check_grep "the answer is the derived token, not an echo of the file" "RABANNIC" "$transcript"

  # The first job was not dropped on the floor to serve the interruption.
  check_file "the slow job still finished" "$work/build.log"
  check_grep "the build wrote its marker" "QUARTZLINE" "$work/build.log"
  check_grep "the build's result reached the screen" "(QUARTZLINE|BUILD-OK|build finished)" "$transcript"
}
