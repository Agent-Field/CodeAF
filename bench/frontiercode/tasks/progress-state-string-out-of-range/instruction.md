# Make ProgressBarState.String total instead of panicking

## Task description

This repository is a terminal user interface framework. Its progress bar
component carries a state type — an integer-like enum with a handful of
named states (none, default, error, indeterminate, warning) — declared in the
root package, in the same file as the framework's main entry points.

There is a latent crash: that state type implements the standard `String`
method by indexing an anonymous five-string array with the raw state value.
Any value outside the five known states therefore panics with an
index-out-of-range error. And such values are reachable directly, because the
progress bar's state field is exported: a caller can assign an unvalidated
value without going through the constructor.

Fix it: make `String` total — return the same human-readable name for every
known state it returns today, and return a stable, human-readable placeholder
(such as `"Unknown"`) for anything outside the known range. Every one of the
known spellings must survive exactly as it is today: callers match on these
strings.

The repository to work in is `~/repos/bubbletea`, already checked out for
you. Work from the commit that is checked out; do not rebase and do not fetch
newer history.

## Test guidelines

The project is a standard Go module:

- build with `go build ./...` from the repository root
- the change lives in the root package; run its tests with `go test .`
- if you add a regression test for the out-of-range case, register it the way
  the package's other tests in that file are registered (plain table or loop
  style, standard-library `testing` with `t.Errorf`)

Tests that cover your change must keep passing.

## Lint guidelines

- the tree carries the project's golangci-lint configuration; your diff
  should be lint-clean under it
- follow the file's existing conventions rather than importing new ones —
  look at how neighbouring methods are written before adding a style of your
  own

## Style guidelines

- keep the diff focused on the change: no drive-by reformatting, nothing
  outside the root package and its tests
- a switch over the known states with a default arm is the natural shape; do
  not restructure the file

---

## Internet use

This task runs with internet access. What it is for: reading documentation,
API references, error messages and background concepts — the material any
engineer would consult while doing this work on their own machine.

What it is not for, and what we scan for after every run: anything that could
hand you the fix for this specific task. That means the repository this task
is cut from, and any mirror, fork, vendored copy, issue tracker, pull request,
commit, patch, diff, changelog or CI log of it; and searching for phrasings
likely to surface the bug or the patch itself. Finding this task's own
upstream change is a flagged run: it scores zero regardless of the rest.

If a page you land on turns out to be from the task's own upstream project,
stop using it, say so in your final answer, and carry on from what you already
had.
