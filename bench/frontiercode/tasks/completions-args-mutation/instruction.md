# Stop completion lookup from mutating the caller's arguments

## Task description

This repository is a Go library for building command-line programs. One of
its features generates shell completion scripts and answers live completion
requests: when the user's shell asks the program for completions, the
program re-invokes its own top-level command object with the arguments its
shell passed it, and the machinery walks the command tree to find which
command and which arguments the user is partway through typing.

There is a latent bug in how that machinery handles its input. The arguments
arrive as a slice that is, in real programs, a subslice of the operating
system's own argument vector for the process. The completion machinery
trims the last element off that slice — the word the user is still typing —
and keeps only a subslice of the original. Later, while parsing flags, it
appends a sentinel element to a slice derived from that subslice. An append
to a slice with spare capacity writes into the SAME backing array in place,
so the sentinel ends up inside the caller's argument vector: the program's
own view of its arguments is silently corrupted, for any program that walks
the tree from the top of the argument list (the library's
`TraverseChildren` mode).

Fix it: the arguments handed through completion lookup must be a copy the
machinery owns, so that no later append anywhere in the path can write into
the caller's array. A copy at the point where the trimmed list is produced
is the natural shape; do not restructure the function, and do not
special-case the single append site — the shape (an owned copy at the
boundary) is the fix, not one call site's behaviour.

Every other behaviour must stay exactly as it is: completions for commands,
flags and `ValidArgsFunction` must return the same results as before.

The repository to work in is `~/repos/cobra`, already checked out for you.
Work from the commit that is checked out; do not rebase and do not fetch
newer history.

## Test guidelines

The project is a standard Go module; the completion code and its tests live
in the module root package:

- build with `go build ./...` from the repository root
- the tests that cover this area run with `go test . -run 'TestCompletion'`
- a regression test for this bug overrides the process argument vector
  directly (saves and restores `os.Args`), runs a top-level execute of the
  no-descriptions completion command, and asserts the vector is
  byte-identical afterwards; if you add such a test, follow the table-driven
  and helper conventions the file already uses

Tests that cover your change must keep passing.

## Lint guidelines

- the tree carries the project's golangci-lint configuration; your diff
  should be lint-clean under it
- follow the file's existing conventions rather than importing new ones

## Style guidelines

- keep the diff focused on the change: no drive-by reformatting, nothing
  outside the library's own sources and tests
- a copy where the trimmed list is first produced is a few lines; anything
  larger than that is a sign you are restructuring — stop and re-read the
  brief above

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
