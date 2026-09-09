#!/usr/bin/env bash
# work-result-recalled — after work has been done, is the person told what it
# actually produced when they ask?
#
# The narrow question, stated exactly. A piece of work is asked for, the
# conversation settles, and then the person asks what came back. The check is
# that the number the screen gives afterwards is the number really in the file.
# That is a RECALL check: it catches a session that writes the right file and
# then tells the person a different figure, which is a real way to lose an
# answer.
#
# WHAT THIS CELL DOES NOT ESTABLISH, and must not be quoted for:
#
#   * that any task was spawned, that a checkpoint or a task node existed, or
#     that anything ran on after the reply. Nothing here asserts a shape, and a
#     build that answers in one turn with no task at all passes — correctly, on
#     this question.
#   * that a result is delivered AUTOMATICALLY. The person has to ask. A
#     session that finishes work and says nothing until prompted passes this
#     cell, so it cannot be evidence of automatic delivery. That is a separate
#     live check, run against the product's own task surface, and it is not in
#     this suite.
#
# It runs on aforge alone because it is aforge's terminal being examined, not
# because a claim is made about what the other arms can or cannot hand off.

SCENARIO_WORKLOAD="conversation"
SCENARIO_DOOR="interactive"
# On any other arm it is recorded `unsupported` — the honest word for "this
# suite has no equivalent calibrated to run there".
SCENARIO_ARMS="aforge"
SCENARIO_CAP_S="${SCENARIO_CAP_S:-480}"
SCENARIO_GUARDS="what the work really produced is what the person is told when they ask"

# shellcheck source=../fixtures/slowwork.sh
source "$CONV_ROOT/fixtures/slowwork.sh"

scenario_fixture() { fixture_slowwork "$1"; }

scenario_turns() {
  local plan="$2"
  {
    printf 'ready\tTake this on as a piece of work: add up every quantity in inventory.txt and write the single total number, digits only, into total.txt in this directory.\n'
    printf 'idle\tWhat was the result of that work — what number ended up in total.txt?\n'
  } > "$plan"
}

scenario_check() {
  local work="$1" transcript="$2"
  check_file "the work produced its file (total.txt)" "$work/total.txt"

  # The truth is computed from the fixture, not taken from the run.
  local expected got
  expected="$(awk '{ sum += $2 } END { print sum }' "$work/inventory.txt")"
  got="$(tr -cd '0-9' < "$work/total.txt" 2>/dev/null)"
  check_eq "the number in total.txt is right" "$expected" "$got"

  # The recall: having been ASKED, the screen carries that number. A run that
  # writes the right file and then answers with a different figure fails here,
  # which is the thing this cell is for. It says nothing about whether the
  # answer would have arrived unasked.
  check_grep "the result was recalled correctly when asked" "(^|[^0-9])$expected([^0-9]|$)" "$transcript"
  record "delivery" "on request (turn 2) — automatic delivery is not tested here"
}
