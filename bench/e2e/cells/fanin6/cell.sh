#!/usr/bin/env bash
# fanin6 — six parts into one sink. The dependency-pot regression sentinel.
#
# This is the cell that exists because of a specific bug. The pot of finished
# work handed to a gated node used to be capped at 4 KiB; six sections of prose
# do not fit in 4 KiB, so the sink was fed four of them and never told about the
# other two. It then wrote a confident, well-formed, four-row table. Nothing
# failed. No node errored. The delivery gate passed it. The only way to see the
# loss was to count the rows.
#
# So this cell asserts three separate things, and they fail in different places:
#
#   1. six leaves actually started together  — the fan-out happened at all
#   2. one node is gated on all six          — the sink waits for every part
#   3. the final TABLE names all six         — nothing was dropped in transit
#
# The third is the one that caught the pot bug, and it is checked against the
# table's rows rather than the whole document on purpose: the six sections were
# all present in the file. It was the table, built from the pot, that had four
# rows. Checking the document as a whole would have passed the broken run.
#
# The six subjects are all distinctive strings — no "Go", which would match the
# word "go" anywhere in the prose and turn the anchor into a coin flip.

CELL_GUARDS="6 parallel leaves, a sink gated on all 6, and all 6 present in the final table"
CELL_BUDGET=1500

CELL_TASK='For each of these six languages — Rust, Zig, Elixir, Haskell, OCaml, and Erlang — write a short section describing its concurrency story. Then, after all six sections, produce a single ranked markdown table that ranks all six languages by their suitability for building a high-throughput network service. The table must have exactly one row per language, so six rows, each naming the language and giving a one-sentence justification. Every one of the six languages must appear in the table. Write the sections and the table to languages.md.'

cell_fixture() { :; }

# The six anchors, in one place, used by both the section check and the far more
# important table check.
FANIN6_SUBJECTS=(rust zig elixir haskell ocaml erlang)

cell_check() {
  local dir="$1"
  local report="$dir/languages.md"

  check_file "languages.md written" "$report"

  # All six discussed somewhere in the document.
  check_grep_all "all six languages have a section" "$report" "${FANIN6_SUBJECTS[@]}"

  # The sentinel. Pull out the markdown table rows and require all six there.
  # A document with six sections and a four-row table is the exact shape of the
  # pot bug, and it passes every check except this one.
  local rows="$dir/.e2e-table-rows"
  grep -a '^[[:space:]]*|' "$report" 2>/dev/null > "$rows" || true

  if [ ! -s "$rows" ]; then
    fail "final answer contains a markdown table — none found in languages.md"
  else
    check_grep_all "TABLE names all six languages (pot sentinel)" "$rows" "${FANIN6_SUBJECTS[@]}"

    # Six data rows plus a header and a separator. Counted loosely — extra rows
    # are fine, missing ones are the failure.
    local row_count
    row_count="$(grep -acE '^[[:space:]]*\|' "$rows" 2>/dev/null || echo 0)"
    check_ge "table has at least six rows" 6 "$row_count"
    record "table_rows" "$row_count"
  fi
}

cell_shape() {
  local db="$1"

  local concurrent
  concurrent="$(concurrent_starts "$db" 2)"

  # The fan-out itself: six leaves inside a two-second window.
  check_ge "six leaves started within 2s of each other" 6 "$concurrent"

  # The gate: one node waiting on all six parts. This is the structural half of
  # the pot sentinel — the sink can only be fed six parts if six edges reach it.
  check_ge "a sink is gated on at least six edges" 6 "$(max_fan_in "$db")"

  check_ge "route planned the work" 6 "$(plan_graph_nodes "$db")"
  check_eq "no failed nodes" 0 "$(failed_count "$db")"

  record "route" "$(scale_route "$db")"
  record "nodes" "$(node_count "$db")"
  record "concurrent_starts_2s" "$concurrent"
  record "max_fan_in" "$(max_fan_in "$db")"
  record "edges" "$(edge_count "$db")"
  record "sink" "$(sink_id "$db")"
  record "start_spread_s" "$(start_spread_seconds "$db")"
  record "delivery_gate" "$(delivery_pass "$db")"
}

CELL_WALL_CEILING=1500
