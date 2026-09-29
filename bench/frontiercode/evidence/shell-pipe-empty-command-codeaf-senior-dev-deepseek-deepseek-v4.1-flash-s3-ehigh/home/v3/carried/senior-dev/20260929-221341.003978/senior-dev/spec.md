# Skip empty shell-pipe commands instead of panicking

## Task description

This repository is a release-automation CLI. Its pipe feature lets a
configuration run arbitrary shell commands at points of the release pipeline
(pre-build, post-build, hooks, and so on). The code that actually executes a
pipe entry's command lives in the internal shell package, in a single `Run`
function that receives the command as a slice of strings (program plus
arguments) and hands it to the OS.

There is a latent crash: the runner indexes the first element of that command
slice without checking that the slice is non-empty. A configuration whose
pipe entry carries no command at all therefore panics the whole release run,
instead of the entry being skipped.

Fix it: when a pipe entry's command list is EMPTY, the runner must skip the
entry without crashing and without returning an error — silently, or with a
warning log if the project already has a logging convention for that. Every
other behaviour of the runner must stay exactly as it is: a command that is
present but fails must still produce its error, and environment and output
handling must be untouched.

The repository to work in is `~/repos/goreleaser`, already checked out for
you. Work from the commit that is checked out; do not rebase and do not fetch
newer history.

## Test guidelines

The project is a standard Go module:

- build with `go build ./...` from the repository root
- the package that covers this change is `internal/shell`; run its tests with
  `go test ./internal/shell/`
- if you add a regression test for the empty case, register it the way the
  package's other table/subtests are registered (the file uses testify
  `require` and the project's own test-context helper)

Tests that cover your change must keep passing.

## Lint guidelines

- the tree carries the project's golangci-lint configuration; your diff
  should be lint-clean under it
- follow the file's existing conventions rather than importing new ones —
  look at how the function already logs and returns errors before adding a
  style of your own

## Style guidelines

- keep the diff focused on the change: no drive-by reformatting, nothing
  outside the internal packages and their tests
- a length guard at the top of the run path is the natural shape; do not
  restructure the function

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