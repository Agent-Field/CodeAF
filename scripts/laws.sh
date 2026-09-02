#!/usr/bin/env bash
# THE LAWS: EVERY TEST THAT READS THE TREE ITSELF, RUN AS ONE TARGET.
#
# A law is a test that opens the repository's own source with go/ast or
# go/parser and refuses a shape — a function with too many endings, a goroutine
# outside the guard, a slash command the manual does not know. It decides in
# under a second, the same on every machine, and its red always means somebody
# broke something. That is exactly the kind of test the pull-request gate exists
# to run, and on 2026-09-02 it was running none of them: the gate's session step
# was `go test -run 'Manual'`, a filter nobody remembered, and the endings
# ratchet went red on dev through two merged pull requests (#372).
#
# THE SELECTION IS COMPUTED, NOT KEPT. A list of test names in a workflow file
# is the filter that rots; so the laws are found by what they DO — the import
# is the marker — and the next law written the same way is in the gate the day
# it lands. Every Test function in a file that carries the marker runs, which
# sweeps in a few behavioural neighbours that share a file; they are cheap, and
# a file whose behavioural half is slow should move its laws out rather than
# argue with this script.
#
# One `go test` invocation, so the heavy test binaries link in parallel; a
# per-package loop would pay the link cost of internal/tui3 and internal/session
# one after the other for no gain. The known-red ledger is read the way `make
# test` and ci-full.yml read it, and reading an absent or empty ledger skips
# nothing, so burning the ledger down to zero changes nothing here.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

files="$(git grep -l -e '"go/ast"' -e '"go/parser"' -- '*_test.go')"
pkgs="$(printf '%s\n' "$files" | xargs -n1 dirname | sort -u | sed 's|^|./|')"
names="$(printf '%s\n' "$files" | xargs grep -hoE '^func Test[A-Za-z0-9_]+' | sed 's/^func //' | sort -u | paste -sd'|' -)"

skip=""
if [ -s .github/known-red.txt ]; then
	skip="$(grep -v -e '^#' -e '^[[:space:]]*$' .github/known-red.txt | paste -sd'|' -)"
fi

echo "laws: $(printf '%s\n' "$files" | wc -l | tr -d ' ') files in $(printf '%s\n' "$pkgs" | wc -l | tr -d ' ') packages"
# shellcheck disable=SC2086
exec go test -count=1 -timeout 5m -run "^(${names})$" ${skip:+-skip "^(${skip})$"} $pkgs "$@"
