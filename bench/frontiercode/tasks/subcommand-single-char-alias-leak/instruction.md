# Single-character aliases must not leak between sibling subcommands

## Task description

This repository is a Go command-line parser library. Applications declare
their command structure with struct tags: nested commands, their flags, and
per-flag aliases (alternate spellings accepted on the command line). When the
library builds its model it keeps a registry of the flag spellings each
command node has claimed, so the SAME spelling may be reused by SIBLING
subcommands — two independent subcommands may each declare `--verbose` —
while a genuine duplicate inside one command still errors. When the builder
finishes with a subcommand's flags it "unsees" them, removing their
spellings from the registry so the next sibling can reuse them.

There is a latent bug in that bookkeeping: every alias is unconditionally
unseen under its LONG form only (a `--` prefix plus the alias). A
single-character alias (say `aliases:"f"`) was registered under its SHORT
form (`-f`), but the unsee deletes `--f` — a key that was never registered.
So two sibling subcommands that each declare the same one-character alias
appear to conflict, and parsing the second subcommand's short flag fails with
a duplicate-alias error even though the subcommands are entirely separate.
Long aliases are unaffected only by accident, because they happen to be
registered and unseen under the same form.

Fix it: the unsee must remove an alias under the form it was actually
invoked as — a one-character alias under its short form (`-x`), a longer
alias under its long form (`--x`) — so sibling subcommands may reuse
single-character aliases. Duplicate detection must keep working exactly as
before: a genuine duplicate of a long alias within one command must still be
reported, and no whole record may be cleared to paper over the conflict. Do
not change how flags are registered, parsed, or matched at parse time; the
only change is to the bookkeeping that releases a finished subcommand's
spellings.

The repository to work in is `~/repos/kong`, already checked out for you.
Work from the commit that is checked out; do not rebase and do not fetch
newer history.

## Test guidelines

The project is a standard Go module:

- build with `go build ./...` from the repository root
- the tests live in the package at the module root; run them with `go test .`
- if you add a regression test, register it the way the file's other
  subcommand-alias tests are registered (plain table-free test functions,
  `kong.New` on an anonymous struct, `assert` from `github.com/alecthomas/assert/v2`)

Tests that cover your change must keep passing — including the existing
duplicate-alias tests, which pin the conflict contract this fix must not
weaken.

## Lint guidelines

- the tree carries the project's lint configuration; your diff should be
  lint-clean under it
- follow the file's existing conventions rather than importing new ones —
  look at how the neighbouring unsee lines spell their keys before adding a
  style of your own

## Style guidelines

- keep the diff focused on the change: no drive-by reformatting, nothing
  outside the builder and its tests
- a form check on the alias inside the existing unsee loop is the natural
  shape; do not restructure the function

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
