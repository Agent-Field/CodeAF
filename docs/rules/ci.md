# The checks

## Two gates, and the heavy one is not on your pull request

| Where | Workflow | What runs | Roughly |
| --- | --- | --- | --- |
| pull request into `dev`, and every push to `dev` | `.github/workflows/ci.yml` | build, vet, the packed corpora, the manual law | a few minutes |
| pull request into `staging`, every push to `staging`, and nightly at 09:00 UTC | `.github/workflows/ci-full.yml` | the whole suite, six-platform cross build, the two-machine remote test | tens of minutes |
| a `v*` tag | `.github/workflows/release-binaries.yml` | the release surface, then publish | — |

**The light gate is deliberately light.** Work reaches `dev` many times a day,
much of it written by agents, and a gate that takes fifteen minutes is a gate
people learn to route around. So the pull-request gate asks only the questions
whose answer is the same on every machine and whose failure always means
somebody broke something.

**The price of that trade is that `dev` is not trustworthy on its own.** That is
not a flaw in the arrangement, it is the arrangement: `dev` is where things are
allowed to be briefly wrong, `staging` is where they are not. The full suite is
paid for once, on the way into `staging`, instead of on every pull request.

## What the light gate actually checks

Four things, in `ci.yml`, job name `check`:

- **`go build ./...`** — several sessions work this tree at once and a
  half-finished file breaks the build for everybody. Cheapest possible answer to
  the most blocking possible failure.
- **`go vet ./...`** — the class of bug agents produce most: a `printf` verb that
  does not match, a cancel that is never called, a result that is thrown away.
- **The packed corpora match their folders.** `go generate` on the three packed
  packages, then demand the tree comes back clean. A manual page edited without a
  build ships yesterday's manual.
- **The manual law.** `internal/manual`, plus the `Manual` tests in
  `internal/tui3` and `internal/session` — every slash command and alias, every
  tool on the belt, and every probe question still reaching the page that answers
  it. Six seconds, and it is the law that gets broken most.

Run the same thing before you push:

```sh
go build ./... && go vet ./... && make embed && git diff --exit-code
go test ./internal/manual/ && go test -run Manual ./internal/tui3/ ./internal/session/
```

## What the full gate checks

`ci-full.yml`, four jobs:

- **`full tests`** — every package except `internal/swepro`, minus the ledger
  below.
- **`cross build`** — all six shipped targets compile. The only job here whose
  answer is identical on every machine and every run, which is why it is the one
  that blocks a promotion.
- **`remote`** — `make test-remote`, three containers sharing no path, no home
  and no credential. Skips green where there is no docker.
- **`size (informational)`** — prints the binary's weight next to `SIZE-BUDGET`
  and does not fail. The budget was set on linux/arm64 and the runner is
  linux/amd64, so the two numbers are not comparable; making this block means
  first agreeing which architecture the budget is measured on. `make check` on
  your own machine still enforces it, and `PERF.md` still governs changing it.

## The known-red ledger

`.github/known-red.txt` lists tests that fail on a clean tree. The full run skips
them by name.

**This exists so that red still means something.** A suite with fifteen permanent
failures is a suite nobody reads, and the sixteenth failure — the one somebody
just caused — arrives invisible.

Every line is a fix somebody owes. Adding a line is allowed and sometimes right,
but it is a reviewable line in the same pull request as the reason it was needed,
never something a build quietly acquired. `SIZE-BUDGET` carries the same rule for
bytes and says why at length.

The list was seeded from `CLAUDE.md` and has not yet been trued up against a real
Linux run — `CLAUDE.md` also names four `cmd/harness-design` tests without
spelling them, and at least one entry may pass on Linux. **The first full run
will say. Correct the file from what it shows.**

## What blocks a merge

Required today: **`check`** on `dev`, **`cross build`** on `staging`. Those are
the two whose green is trustworthy right now.

`full tests` and `remote` run and report, and are deliberately not required yet —
neither has been seen green in this repository's CI even once, and a required
check that has never passed blocks all work on its first day. **Promote them by
adding their job names to `required_status_checks` in
`.github/rulesets/promotion-pointers.json` as soon as each has been green twice
in a row.** That is the next piece of work here, not a someday.

## None of it is enforced yet

`Agent-Field` is on the free plan with a private repository, and that combination
has no branch rules — the API answers `403 Upgrade to GitHub Pro`. The checks
above run and show red, but nothing stops a merge on top of red, and nothing
stops a direct push. **Until the org moves to GitHub Team, all of this is
convention.** `.github/rulesets/README.md` has the state of that.
