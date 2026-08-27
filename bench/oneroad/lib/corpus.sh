#!/usr/bin/env bash
# corpus.sh — the live-issue corpus, and the clone/venv/suite verdict.
#
# Every function here is bench/run.sh's, copied rather than sourced: bench/run.sh
# runs its own grid at the bottom of the file, so sourcing it would run it. The
# bodies are byte-equivalent where they can be, and where they differ the comment
# says why. ATTRIBUTION: bench/run.sh (issue_prompt, fresh_clone, setup_python,
# run_suite, changed_files).

REPO="${REPO:-https://github.com/MALIBA-AI/bambara-text-normalization}"
OWNER_REPO="$(echo "$REPO" | sed -E 's#^.*github.com[:/]##; s#\.git$##')"
SLUG="$(basename "$REPO" .git)"

# The pin bench/run.sh records: 2026-08-02, merge of PR #18, the last commit
# with all four issues open. A HEAD clone passes the suite before any harness
# arrives and the row would measure nothing.
BASE_COMMIT="${BASE_COMMIT:-6c978ffa1c49ba600c85eb893958409e37dbedd2}"

# issue_prompt — bench/run.sh's, unchanged. All five arms receive this text; the
# only difference between cells is who executes it.
issue_prompt() {
  local number="$1"
  gh issue view "$number" --repo "$OWNER_REPO" --json title,body \
    --template 'Implement issue #'"$number"': {{.title}}

{{.body}}

Work in this repository. Implement the change and make the existing test suite pass. Do not weaken or delete tests to make them pass.'
}

# batch_prompt is the width probe: the same four issues, in one message, in one
# clone. It is the ONLY prompt this benchmark writes that bench/run.sh does not
# have, and it says nothing about how to do the work — no "task", no "split", no
# "divide". Whether four independent issues is one road or four is the thing
# being measured, and a prompt that hinted would answer its own question.
batch_prompt() {
  local number body
  printf 'Here are four issues to fix in this repository.\n\n'
  for number in $ISSUES; do
    body="$(gh issue view "$number" --repo "$OWNER_REPO" --json title,body \
      --template '## Issue #'"$number"': {{.title}}

{{.body}}')" || return 1
    printf '%s\n\n' "$body"
  done
  printf 'Work in this repository. Implement the changes and make the existing test suite pass. Do not weaken or delete tests to make them pass.\n'
}

# fresh_clone — bench/run.sh's. A clone of its own per cell, at the same starting
# commit: reusing one checkout leaks the previous harness's diff into the next
# one's starting state.
fresh_clone() {
  local dir="$1"
  rm -rf "$dir"
  if [ -n "$BASE_COMMIT" ]; then
    git clone --quiet "$REPO" "$dir" || return 1
    (cd "$dir" && git checkout --quiet "$BASE_COMMIT") || return 1
  else
    git clone --depth 1 --quiet "$REPO" "$dir" || return 1
  fi
  (cd "$dir" && git rev-parse HEAD)
}

# setup_python — bench/run.sh's. The suite is the judge, so it is installed
# before the harness runs and never touched afterwards.
setup_python() {
  local dir="$1"
  (
    cd "$dir" || exit 1
    python3 -m venv .venv >/dev/null 2>&1 || exit 1
    .venv/bin/pip install -q --upgrade pip >/dev/null 2>&1
    .venv/bin/pip install -q -e ".[dev]" >/dev/null 2>&1 ||
      .venv/bin/pip install -q -e . >/dev/null 2>&1 ||
      .venv/bin/pip install -q -r requirements.txt >/dev/null 2>&1
    .venv/bin/pip install -q pytest >/dev/null 2>&1
  )
}

# run_suite — bench/run.sh's verdict, run TWICE here where bench/run.sh runs it
# once. bench/oneroad/README.md's table wants tests_before as well as
# tests_after, and "before" on a pinned commit is a fact worth having written
# down rather than assumed from the pin.
run_suite() {
  local dir="$1" log="$2"
  (cd "$dir" && ./.venv/bin/python -m pytest -q) >"$log" 2>&1
  local line passed failed
  line="$(grep -E '[0-9]+ (passed|failed)' "$log" | tail -1)"
  passed="$(echo "$line" | grep -oE '[0-9]+ passed' | grep -oE '[0-9]+')"
  failed="$(echo "$line" | grep -oE '[0-9]+ failed' | grep -oE '[0-9]+')"
  echo "${passed:-0}/${failed:-0}"
}

# changed_files — bench/run.sh's. What the harness actually did to the working
# tree. __pycache__ joins .venv in the exclusion because a suite run creates it
# and a cell that changed nothing would otherwise read as having changed one.
changed_files() {
  (cd "$1" && git status --porcelain 2>/dev/null | grep -vE '\.venv|__pycache__' | grep -c . || true)
}
