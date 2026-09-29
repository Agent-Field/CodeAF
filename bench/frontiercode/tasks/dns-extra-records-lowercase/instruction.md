# Lowercase DNS extra record names

## Task description

This repository is a coordination server for a mesh VPN. Administrators can
configure extra DNS records (static A/CNAME-style entries) that are handed to
clients as part of the tailcfg DNS configuration. The clients that consume
that configuration match extra records by exact, already-lowercase name, but
nothing on the server lowercases the record names on the way in. A record
configured as `Printer.fritz.box` therefore never resolves against a client
lookup of `printer.fritz.box`.

There is more than one path that writes extra records into the tailcfg DNS
config: the conversion of the configured DNS block into its tailcfg form, and
a runtime setter used when records are updated while the server is up. Both
live in the `hscontrol/types` package's config file.

Fix it: normalize record NAMES to lowercase on EVERY path that produces
extra records for that config. Record types (A, AAAA, CNAME, ...) and record
values must pass through untouched, and an empty record list must be handled
without erroring.

The repository to work in is `~/repos/headscale`, already checked out for
you. Work from the commit that is checked out; do not rebase and do not fetch
newer history.

## Test guidelines

The project is a standard Go module:

- build with `go build ./...` from the repository root
- the package that covers this change is `hscontrol/types`; run its tests
  with `go test ./hscontrol/types/`
- if you add a regression test for the lowercase case, register it the way
  the file's other tests are registered (the file uses standard `testing`
  with `cmp.Diff`)

Tests that cover your change must keep passing.

## Lint guidelines

- the tree carries the project's golangci-lint configuration; your diff
  should be lint-clean under it
- follow the file's existing conventions rather than importing new ones —
  look at how neighbouring functions are documented and structured before
  adding a style of your own

## Style guidelines

- keep the diff focused on the change: no drive-by reformatting, nothing
  outside the `hscontrol/` packages and their tests
- a small helper shared by both paths is the natural shape; do not
  restructure the config file

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
