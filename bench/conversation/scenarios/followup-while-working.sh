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
SCENARIO_GUARDS="a second question, asked while the first job is still running"

# shellcheck source=../fixtures/slowwork.sh
source "$CONV_ROOT/fixtures/slowwork.sh"

scenario_fixture() { fixture_slowwork "$1"; }

# scenario_turns writes the driver's script. The `busy` line is the whole cell:
# it is sent only once the screen shows work in flight, and if that window never
# appears the cell records `no-busy-window` and fails rather than quietly
# becoming a two-turn conversation.
scenario_turns() {
  local plan="$2"
  {
    printf 'ready\tRun ./slow-build.sh in this directory. It takes about a minute. Tell me what it writes when it is done.\n'
    printf 'busy\tWhile that is running: what is the checksum word in NOTES.txt?\n'
  } > "$plan"
}

scenario_check() {
  local work="$1" transcript="$2"
  # The followup was answered: CINNABAR is in NOTES.txt and nowhere else.
  check_grep "the followup was answered while the build ran" "CINNABAR" "$transcript"
  # The first job was not dropped on the floor. The file is the witness; the
  # screen is asked separately, because a build that finished after the harness
  # stopped talking about it is still a build the person was never told about.
  check_file "the slow job still finished" "$work/build.log"
  check_grep "the build wrote its marker" "QUARTZLINE" "$work/build.log"
  check_grep "the build's result reached the screen" "(QUARTZLINE|BUILD-OK|build finished)" "$transcript"
}
