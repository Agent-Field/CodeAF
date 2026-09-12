#!/usr/bin/env bash
# repo-wording — the small point on the same curve.
#
# One request is one point, and one point is not a shape. This is the second:
# the same repository, the same clone, the same door, and a job of the size the
# change entries in docs/changes/unreleased/ are mostly made of — a
# person-facing word changes in one file, and the test that quotes that word
# changes with it. It is deliberately a job with a right answer that fits on a
# line, so that a difference between two arms on THIS ask cannot be a
# difference of opinion about what was wanted.
#
# It is also the ask that says what the fixed cost of a turn is. A build that
# spends four minutes before writing one word spends them here too, where there
# is nothing to think about.

# shellcheck source=../fixtures/repoclone.sh
source "$CONV_ROOT/fixtures/repoclone.sh"

SCENARIO_WORKLOAD="coding"
SCENARIO_ARMS="aforge"
SCENARIO_GUARDS="one word, one file, and the test that quotes it"

scenario_fixture() { fixture_repoclone "$1" "$2"; }

repo_wording_ask() {
  cat <<TXT
In $CONV_REPO_NICKNAME, the foot of the tasks place reads "alt+s sort". Make it
say "alt+s sorts" instead, and keep the test that quotes that line green.
TXT
}

repo_wording_steer() {
  cat <<'TXT'
Only that one clause — the rest of the foot stays exactly as it reads now.
TXT
}

repo_wording_check() {
  local work="$1" cell="$3"
  repoclone_measure_changed "$work"
  repoclone_task_worktree "$work"
  repoclone_go "$work" "$cell" build_exit repo-build.log \
    "the workspace still compiles" go build ./...
  repoclone_go "$work" "$cell" vet_exit repo-vet.log \
    "the surface package still vets" go vet ./internal/tui3/
  # The named test IS the request: it is the one that quotes the foot word for
  # word, so it is red the moment the word changes and green only once both
  # halves of the job are done.
  repoclone_go "$work" "$cell" focus_exit repo-focus.log \
    "the test that quotes the foot is green" \
    make test-focus PKGS=./internal/tui3 RUN='^TestTheTasksFootIsScreenOneEWordForWord$'
  repoclone_ask_wired "$work" "the foot says the new word" 'alt\+s sorts|" sorts"'
}
