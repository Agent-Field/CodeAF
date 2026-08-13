#!/usr/bin/env bash
# lookup — the smallest possible errand, and the collapse it must produce.
#
# Guards the floor of the scale gate. A one-paragraph factual question needs one
# leaf, no planner, and no fan-out, and the failure this cell exists to catch is
# the tasker deciding otherwise: a lookup that arrives as a planned project
# costs several times what it should and takes minutes instead of seconds, with
# nothing in the output to say anything went wrong. Only the journal shows it.
#
# This is also the cheapest cell by a wide margin (~$0.002, ~12s measured), which
# makes it the one to run alone when all you want to know is whether the
# machinery still works.

CELL_GUARDS="scale gate collapses a lookup to one leaf, no planning, under a minute"
CELL_BUDGET=300

CELL_TASK='In one paragraph, explain what the Nyquist-Shannon sampling theorem states and why it matters for digital audio. Write the paragraph to answer.md and also print it.'

# No fixture: the ask carries everything it needs.
cell_fixture() { :; }

cell_check() {
  local dir="$1" stdout="$2"
  check_file "answer.md written" "$dir/answer.md"
  check_grep "answer names the theorem" "nyquist" "$dir/answer.md"
  check_grep "answer covers sampling rate" "sampl" "$dir/answer.md"
  check_grep "the paragraph also reached stdout" "nyquist" "$stdout"
}

cell_shape() {
  local db="$1"

  # Exactly one work node. The spine root is not counted — see node_count.
  check_eq "exactly one work node" 1 "$(node_count "$db")"

  # The route the gate must take.
  check_eq "scale_gate route is single_leaf" "single_leaf" "$(scale_route "$db")"

  # A plan_graph event is emitted on every route, including this one, carrying a
  # one-node graph. So the sentinel for "did not plan" is the graph's node count,
  # not the event's absence: asserting the event never appears would fail on a
  # perfectly healthy single-leaf run. Measured on a real run: the event is
  # present, with exactly one node in it.
  check_eq "plan graph describes one node (no fan-out)" 1 "$(plan_graph_nodes "$db")"

  check_eq "no dependency edges" 0 "$(edge_count "$db")"
  check_eq "no growth rounds" 0 "$(growth_rounds "$db")"

  record "scale" "$(scale_field "$db" scale)"
  record "delivery_gate" "$(delivery_pass "$db")"
}

# cell_wall_ceiling is the wall this cell asserts, separate from CELL_BUDGET,
# which is only the spend backstop handed to --timeout.
CELL_WALL_CEILING=60
