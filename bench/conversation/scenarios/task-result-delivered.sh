#!/usr/bin/env bash
# task-result-delivered — aforge's own terminal, and the question its shape
# makes possible to get wrong: when work is handed off, does the RESULT come
# back to the person?
#
# aforge can hand a piece of work to a task that runs on after the reply. That
# is a capability the other arms do not have, and it is also a way to lose an
# answer: the task finishes, the file is written, and the person is told nothing
# — or is told a summary that does not match what the task actually produced.
#
# So the cell asks for work whose answer is a number that can be recomputed
# from the fixture, waits for the conversation to settle, and then asks the
# question a person asks: what did that come back with? The check is that the
# number the screen gives afterwards is the number that is really in the file.
#
# NOTHING HERE ASSERTS A SHAPE. Not how many agents ran, not whether a task was
# spawned at all, not what the graph looked like. A build that answers the
# question in one turn with no task at all passes this cell, and should: the
# person asked for a result, not for an org chart.

SCENARIO_WORKLOAD="conversation"
SCENARIO_DOOR="interactive"
# This cell is about aforge's own surface, so it exists for that arm only. On
# any other arm it is recorded `unsupported` — the honest word for "this suite
# has no equivalent to run there".
SCENARIO_ARMS="aforge"
SCENARIO_CAP_S="${SCENARIO_CAP_S:-480}"
SCENARIO_GUARDS="the latest task's real result is what the person is told"

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

  # The delivery: after being asked, the screen carries that number. This is the
  # assertion the cell exists for — a run that writes the right file and tells
  # the person nothing fails here, and a run that tells the person a number the
  # file does not contain fails harder.
  check_grep "the latest result was delivered on screen" "(^|[^0-9])$expected([^0-9]|$)" "$transcript"
  record "asked_for_result_turn" "2"
}
