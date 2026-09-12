#!/usr/bin/env bash
# repo-hover — THE REQUEST OF 2026-09-11, word for word in a person's own voice.
#
# This is the paragraph that cost seventeen minutes, sixty-two tool calls and
# forty rounds on `dev` before spilling into a worker task, and it is the whole
# reason the `simplify` branch exists. It is reproduced here rather than
# paraphrased: a benchmark that tidied the request would be measuring a
# different question than the one that went wrong.
#
# It is not sourced as a scenario. The two runnable scenarios beside it —
# repo-hover-print and repo-hover-interactive — are the same ask through the
# two doors, and they share this file so that the words cannot drift apart.

# shellcheck source=../fixtures/repoclone.sh
source "$CONV_ROOT/fixtures/repoclone.sh"

SCENARIO_WORKLOAD="coding"
SCENARIO_ARMS="aforge"
SCENARIO_GUARDS="one paragraph of UI work in a repository the harness has to find"

scenario_fixture() { fixture_repoclone "$1" "$2"; }

# repo_hover_ask is the opening message. It names the project by NICKNAME and
# nothing else: the harness is launched in a directory that is not the
# repository, so working out which folder is meant is part of the turn — which
# is exactly how the turn under study began.
repo_hover_ask() {
  cat <<TXT
In $CONV_REPO_NICKNAME, add a hover effect to the two sortable column labels on
the tasks place. Click-to-sort already works — I just want the label under the
pointer to lift, so you can see it is a thing you can press.
TXT
}

# repo_hover_steer is the one correction a person types while it is working.
# It narrows the scope and settles nothing else: a steer that changed the
# deliverable would change what the judge below is allowed to ask for, and a
# steer that said "do not start a task" would suppress on both arms the very
# behaviour the wave is about.
repo_hover_steer() {
  cat <<'TXT'
To be clear: only the two sortable column labels on the tasks table — not the
rows, and nothing else on that page.
TXT
}

# repo_hover_check judges the CLONE. In order: what changed, whether it still
# compiles, whether it still vets, whether the tasks place's own tests still
# pass, and whether the thing that was asked for is in the source.
repo_hover_check() {
  local work="$1" cell="$3"
  repoclone_measure_changed "$work"
  repoclone_task_worktree "$work"
  repoclone_go "$work" "$cell" build_exit repo-build.log \
    "the workspace still compiles" go build ./...
  repoclone_go "$work" "$cell" vet_exit repo-vet.log \
    "the surface package still vets" go vet ./internal/tui3/
  # The tasks place's own tests are what this request implies: they are the
  # tests a person would run, they cover the table the labels sit on, and a
  # test the arm ADDED for its hover is named after the place too and so runs
  # here as well. The repository's own door is used rather than a bare `go
  # test`, because that door carries the known-red ledger and a benchmark that
  # skipped it would fail an arm for a red it did not cause.
  repoclone_go "$work" "$cell" focus_exit repo-focus.log \
    "the tasks place's own tests are green" \
    make test-focus PKGS=./internal/tui3 RUN='^Test.*Tasks'
  # THE POINTER HAS TO REACH THE LABEL. Both halves have to be in the changed
  # set: something that knows about hovering, and something that knows which
  # column the pointer is over. Either alone is a surface that hovers nothing
  # or a sort that nobody can see is a target.
  repoclone_ask_wired "$work" "the label under the pointer is wired to hover" \
    'hover|Hover' 'tasksSort|tasksControlLabels|sortArrow|taskSheet'
}
