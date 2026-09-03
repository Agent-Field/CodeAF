# The checks

## Two gates, and the heavy one is not on your pull request

| Where | Workflow | What runs | Roughly |
| --- | --- | --- | --- |
| pull request into `dev`, and every push to `dev` | `.github/workflows/ci.yml` | `check`: build, gofmt, vet, the packed corpora, the change entry, the manual law, the laws. `touched packages`: the full suite of every package the change touched — pushes to `dev` (and manual dispatch) only, not pull requests | `check` a few minutes; `touched packages` as long as the slowest touched package |
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

**But "light" never meant "runs a filter nobody remembers".** Until 2026-09-02
the gate's one test step over the engine was `go test -run 'Manual'`, and the
endings ratchet in `internal/session` went red on `dev` through two merged pull
requests with every check green (#372). A structural test — one that reads the
tree and refuses a shape — decides in under a second and the same on every
machine, which is the light gate's own definition of what belongs on it. So the
gate now runs every one of them, found by what they do rather than by a list.
The packages a change touched run in full on the push to `dev`, not on the
pull request.

## What the light gate actually checks

Seven things in `ci.yml`, job name `check`, and then one more job:

- **`go build ./...`** — several sessions work this tree at once and a
  half-finished file breaks the build for everybody. Cheapest possible answer to
  the most blocking possible failure.
- **`make fmt-check`** — a file gofmt would rewrite is a file the next editor's
  save rewrites, and that diff lands in somebody else's pull request.
- **`go vet ./...`** — the class of bug agents produce most: a `printf` verb that
  does not match, a cancel that is never called, a result that is thrown away.
- **The packed corpora build from their folders.** `go generate` on the packed
  package (`internal/manual`), then compile and test the manual's packed release path. Its
  archives are ignored build products, avoiding one binary merge hotspot; the
  other packed corpora remain tracked, so the tree must still come back clean.
- **The change is written down.** A new file in `docs/changes/unreleased/`,
  well formed. Two seconds. It carries the one thing a diff cannot — which of
  the things somebody believes about this repository stopped being true — and it
  is asked for here because it is worth nothing written later. A one-line
  `kind: internal` entry is a legitimate answer and the `no-changelog` label is
  the way out; [changelog.md](changelog.md) is the rule.
- **The manual law.** `internal/manual`, plus the `Manual` tests in
  `internal/tui3` and `internal/session` — every slash command and alias, every
  tool on the belt, and every probe question still reaching the page that answers
  it. Six seconds, and it is the law that gets broken most.
- **The laws hold.** `make test-laws`: every test in the tree that opens the
  repository's own source with `go/ast` or `go/parser` — the endings ratchet,
  the guard, the taxonomy, the known-red ratchet, the words the e2e suite waits
  for. `scripts/laws.sh` finds them by that import, so a new law is on the gate
  the day it is written and there is no list to forget. About twenty seconds
  after the link. Every test in a file that carries the marker runs, so a file
  whose behavioural half is slow should move its laws out rather than argue with
  the script.

**`touched packages`** is the second job: the full suite of every package the
change touched, through `make test`, so it reads the same ledger and the same
timeout as a laptop. It runs on pushes to `dev` and on manual dispatch, not on
pull requests — it never blocked a merge, and its per-PR red signal was not
being acted on, so the pull request keeps only the light answer, which still
arrives in its few minutes. It is neutral today: its test step may
fail without the job failing, and a failure is a warning on the run and a line in
its summary, never silence. A red job on a merged pull request reads as a red
`dev` to everyone after it, and the day this landed `internal/tui3` carried three
runner-only reds (#417) no change caused. Once it has been green twice in a row,
delete `continue-on-error` and the warning step in `ci.yml`, and it blocks like
`check`.

Run the same thing before you push:

```sh
go build ./... && make fmt-check && go vet ./... && make test-packed-manual && git diff --exit-code
make changelog-check
go test ./internal/manual/ && go test -run Manual ./internal/tui3/ ./internal/session/
make test-laws
make test PKGS='./internal/whatever/you/touched'
```

## What the full gate checks

`ci-full.yml`, seven jobs (three test shards and their one name, the six-target cross build and its one name, `remote`, `size`, and the page):

- **`full tests`** — every package, minus the ledger below, split round-robin
  across three runners. Not for speed first: the
  free-plan runner has seven gigabytes, this repository's test binaries are
  heavy, and the suite's first run was killed under the link load of its last
  eight packages. Three machines carrying a third each stay inside their memory.
  It runs through `make test`, so the ledger it skips and the per-package
  timeout it carries are the Makefile's and the same as a laptop's. The
  timeout is measured, not guessed: `internal/tui3` takes about 485 seconds on
  this runner, and the old `8m` cut it off at the finish line and reported a
  test that had been running for two seconds as a hang (#372).
- **`page on a red nightly`** — a scheduled run reports to nobody, and every
  nightly before 2026-09-02 had been red unseen. So a red night opens one issue,
  or adds the night's run to the one already open, and that issue is what
  somebody sees in the morning. Whoever makes the nightly green closes it.
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

`.github/known-red.txt` lists tests that fail on a clean tree. `make test` skips
them by name, and every run — the nightly, `touched packages`, the laws — goes
through `make test` or reads the file the same way, so "green locally" and
"green in CI" are one fact. `make check` could not pass on a clean tree before
2026-09-02, because its `test` target skipped nothing while the nightly did; that
is over.

**This exists so that red still means something.** A suite with fifteen permanent
failures is a suite nobody reads, and the sixteenth failure — the one somebody
just caused — arrives invisible.

**And it only shrinks.** Ruled 2026-09-02: the ledger burns to zero, in its own
wave, every entry fixed for real or deleted with a written ruling, and no new
entry is allowed after. `internal/ci` holds the ratchet — one constant,
`knownRedEntries`, that a change may not push up and that a change which fixed
a test lowers in the same commit — and checks that every name on the file is a
test the tree still declares. The commit that deletes the file deletes that
test with it; nothing else reads the ledger, and an absent ledger skips nothing.

The list was seeded from `CLAUDE.md` and trued up against the first real Linux
run on 2026-08-31; #408 took the first five out. What it holds now, and which
entries are Linux-only, is written in the file's own comments and nowhere else —
`CLAUDE.md` no longer copies it, and neither does this page.

## What blocks a merge

Required today: **`check`** on `dev`, **`cross build`** on `staging`. Those are
the two whose green is trustworthy right now.

`full tests`, `remote` and the light gate's `touched packages` run and report,
and are deliberately not required yet — none has been seen green in this
repository's CI twice in a row, and a required check that has never passed
blocks all work on its first day. **Promote them by
adding their job names to `required_status_checks` in
`.github/rulesets/promotion-pointers.json` as soon as each has been green twice
in a row.** That is the next piece of work here, not a someday.

## None of it is enforced yet

`Agent-Field` is on the free plan with a private repository, and that combination
has no branch rules — the API answers `403 Upgrade to GitHub Pro`. The checks
above run and show red, but nothing stops a merge on top of red, and nothing
stops a direct push. **Until the org moves to GitHub Team, all of this is
convention.** `.github/rulesets/README.md` has the state of that.
