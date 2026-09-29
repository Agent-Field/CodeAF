# Hide hidden positionals from the man-page SYNOPSIS

## Task description

This repository is a command-line argument parser whose workspace carries a
man-page generator crate. The generator renders a command's man page,
including a SYNOPSIS section that lists the command's arguments.

There is an inconsistency: the generator filters hidden OPTIONS out of the
SYNOPSIS, but the loop that renders POSITIONALS does no such filtering — it
renders every positional, hidden or not. An application's internal
positional, one marked hidden because it is not meant for users, therefore
leaks into the application's published man page.

Fix it: hidden positionals must be filtered from the SYNOPSIS exactly as
hidden options already are. The SYNOPSIS of a command with NO hidden
arguments must remain byte-identical to what it is today, and visible
positionals must keep rendering as they do.

The repository to work in is `~/repos/clap`, already checked out for you.
Work from the commit that is checked out; do not rebase and do not fetch
newer history.

## Test guidelines

The project is a standard Cargo workspace:

- build the crate with `cargo build -p clap_mangen` from the repository root
- the crate that covers this change is `clap_mangen`; run its tests with
  `cargo test -p clap_mangen`
- the snapshot tests use `snapbox`; a test registers its expected snapshot
  with `snapbox::file!["../snapshots/<name>.roff"]`, and the shared command
  builders live in `clap_mangen/tests/testsuite/common.rs`

Tests that cover your change must keep passing.

## Lint guidelines

- the workspace carries its own lint configuration; your diff should be
  lint-clean under it
- follow the file's existing conventions rather than importing new ones —
  look at how the neighbouring rendering loops filter before adding a style
  of your own

## Style guidelines

- keep the diff focused on the change: no drive-by reformatting, nothing
  outside the man-page crate and its tests
- a filter on the existing positional iterator is the natural shape; do not
  restructure the rendering function

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
