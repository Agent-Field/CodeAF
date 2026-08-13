#!/usr/bin/env bash
# compare3 — a three-subject comparison, where the shape is genuinely open.
#
# Guards *coverage*, not structure. Three databases across three axes is nine
# cells of a table, and the regression this catches is a run that answers about
# two subjects, or covers two axes for one subject and one for the others, and
# still reads fluently. Prose hides a missing cell in a way a test suite never
# does, so the anchors are checked one at a time and the failure names which
# subject or axis went missing.
#
# The shape is deliberately NOT asserted. One capable leaf writing all nine
# cells and three leaves each writing a column then merging are both correct
# answers to this ask, and a battery that demands one of them would fail on a
# healthy improvement to the compiler. The route and node count are recorded so
# that a change in shape is visible in the CSV without being a failure.

CELL_GUARDS="comparison covers every subject x axis; shape recorded, not asserted"
CELL_BUDGET=900

CELL_TASK='Write a comparison of three databases — SQLite, PostgreSQL, and DuckDB — across exactly three axes: their concurrency model, their storage layout, and their typical deployment. Cover every one of the three axes for every one of the three databases. Write the finished comparison to comparison.md.'

cell_fixture() { :; }

cell_check() {
  local dir="$1"
  local report="$dir/comparison.md"

  check_file "comparison.md written" "$report"

  # Every subject, named.
  check_grep_all "all three subjects named" "$report" "sqlite" "postgres" "duckdb"

  # Every axis, named. Each anchor is loose enough to survive a synonym in the
  # heading but tight enough that prose about something else will not match.
  check_grep_all "all three axes named" "$report" "concurren" "storage|column|row.?(store|orient)|layout" "deploy|embedded|server"

  # The nine cells, checked per subject: each subject's section has to mention
  # every axis somewhere near it. Approximated by requiring all three axis
  # anchors to co-occur in the file at least as many times as there are
  # subjects — a report covering one subject properly and naming the other two
  # in passing fails this where the whole-file check above would pass it.
  local concurrency_hits storage_hits deploy_hits
  concurrency_hits="$(grep -acioE "concurren" "$report" 2>/dev/null || echo 0)"
  storage_hits="$(grep -acioE "storage|column|layout" "$report" 2>/dev/null || echo 0)"
  deploy_hits="$(grep -acioE "deploy|embedded|server" "$report" 2>/dev/null || echo 0)"
  check_ge "concurrency discussed for each subject" 3 "$concurrency_hits"
  check_ge "storage discussed for each subject" 3 "$storage_hits"
  check_ge "deployment discussed for each subject" 3 "$deploy_hits"
}

cell_shape() {
  local db="$1"

  # Recorded, never asserted: both shapes are correct answers to this ask.
  record "route" "$(scale_route "$db")"
  record "nodes" "$(node_count "$db")"
  record "plan_nodes" "$(plan_graph_nodes "$db")"
  record "max_fan_in" "$(max_fan_in "$db")"
  record "concurrent_starts_2s" "$(concurrent_starts "$db" 2)"
  record "delivery_gate" "$(delivery_pass "$db")"

  # The one shape thing that IS a failure: nodes the engine gave up on.
  check_eq "no failed nodes" 0 "$(failed_count "$db")"
}

CELL_WALL_CEILING=600
