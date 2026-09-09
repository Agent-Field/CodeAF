#!/usr/bin/env bash
# research-brief — the research workload, through the print door.
#
# Research here is "read what is written down and join it", not "recall what you
# were trained on". The facts are invented tokens that exist nowhere but the
# fixture, so an answer carrying them was read rather than remembered — and one
# of the four files is a superseded note that contradicts the current one, so an
# answer that stops at the first hit gets the ownership question wrong.

SCENARIO_WORKLOAD="research"
SCENARIO_DOOR="print"
SCENARIO_ARMS="aforge omp pi opencode"
SCENARIO_CAP_S="${SCENARIO_CAP_S:-420}"
SCENARIO_GUARDS="facts joined across files, with a stale note present"

# shellcheck source=../fixtures/corpus.sh
source "$CONV_ROOT/fixtures/corpus.sh"

scenario_fixture() { fixture_corpus "$1"; }

scenario_prompt() {
  cat <<'TXT'
The notes/ directory in this folder holds a service's operational notes. Read
them and answer, in a few sentences:

1. Which team currently owns the kestrel service, and how many services does
   that team own?
2. Which port does kestrel listen on, and what is its health endpoint?
3. What caused incident INC-4471, and what was the peak p99 in milliseconds?

Some notes are out of date; say which answer you had to reconcile. Do not write
any files.
TXT
}

scenario_check() {
  local reply="$2"
  # The current owner, not the superseded one. TINDERBOX appears in the corpus
  # as kestrel's owner in a file that says at the top that it is superseded, so
  # this pair of checks is the whole point of the cell.
  check_grep "names the current kestrel owner (BRACKISH)" "BRACKISH" "$reply"
  # The COUNT is the assertion; the decoration is not. Live replies wrote this
  # as "owns 2 services total", "owns **2 services**" and "owns **two**
  # services", and only the last failed — for its asterisks. The count itself is
  # still required exactly: "three services" fails here, as it should.
  check_grep_plain "says BRACKISH owns two services" \
    "(two|2)[ -]services" "$reply"
  check_grep_all "carries the facts that are only in the notes" "$reply" \
    "8431" "/healthz" "2310" "retry budget"
  # A brief that quotes the stale owner as current is wrong even if it also
  # names the right one; the reconciliation has to be visible.
  if grep -aqiE "TINDERBOX" "$reply"; then
    check_grep "if the stale note is quoted, it is marked as superseded/old" \
      "(supersed|out of date|outdated|older|no longer|previously|used to)" "$reply"
  else
    record "stale_note_mentioned" "no"
  fi
  record "reply_chars" "$(wc -c < "$reply" | tr -d ' ')"
}
