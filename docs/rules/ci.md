# The checks

## Two gates, and the heavy one is not on your pull request

| Where | Workflow | What runs | Roughly |
| --- | --- | --- | --- |
| pull request into `dev`, and every push to `dev` | `.github/workflows/ci.yml` | `light gate`: build, gofmt, vet, the packed corpora, the change entry, the manual law, the laws. `touched packages`: the full suite of every package the change touched. `check`: green only when both are | `light gate` a few minutes; `touched packages` as long as the slowest touched package; `check` when both are in |
| pull request into `staging` or `main`, every push to either, and nightly at 09:00 UTC | `.github/workflows/ci-full.yml` | the whole suite, six-platform cross build, the two-machine remote test | tens of minutes |
| Friday after the 17:00 Toronto cutoff, or a manual dispatch | `.github/workflows/promote-staging.yml` | plan the cutoff commit, call Full check on that commit, fast-forward staging if it passes, and report the release and production signal | Full check plus the release build |
| every push to `dev`, `staging` or `main`; a manual stable or channel dispatch | `.github/workflows/release.yml` | resolve and guard the tag, test the release surface except on dev, build six binaries with furrow, publish | — |

**The light gate is deliberately light.** Work reaches `dev` many times a day,
much of it written by agents, and a gate that takes fifteen minutes is a gate
people learn to route around. So the pull-request gate asks only the questions
whose answer is the same on every machine and whose failure always means
somebody broke something.

**The price of that trade is that `dev` is not trustworthy on its own.** That is
not a flaw in the arrangement, it is the arrangement: `dev` is where things are
allowed to be briefly wrong, `staging` is where they are not. The full suite is
paid for once, on the way into `staging`, instead of on every pull request.
`Full check` is also called by the weekly promotion with the chosen commit as
its `ref`; that run is the check the promotion uses before moving staging.

**But "light" never meant "runs a filter nobody remembers".** Until 2026-09-02
the gate's one test step over the engine was `go test -run 'Manual'`, and the
endings ratchet in `internal/session` went red on `dev` through two merged pull
requests with every check green (#372). A structural test — one that reads the
tree and refuses a shape — decides in under a second and the same on every
machine, which is the light gate's own definition of what belongs on it. So the
gate now runs every one of them, found by what they do rather than by a list,
and runs the packages a change touched in full beside them — on the pull
request, where a red can still be read before it is on the trunk. `check`, the
one required name, is green only when both halves are.

## What the light gate actually checks

Seven things in `ci.yml`, job name `light gate`, then the touched packages, then `check`:

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
change touched, through `make test` with `-count=1`, so it reads the same ledger
and the same timeout as a laptop and never a cached pass. It runs on every pull
request and every push to `dev`, beside the light gate rather than after it. A
change with no Go file and no module file runs nothing here and is green in a
minute; a change to `go.mod` or `go.sum` runs the whole tree. **It blocks.** It
was neutral for a day, then off pull requests for a day (#499), and in that day
#523 merged red on `cmd/codeaf` with `check` green, as #437, #439 and #483 had
before the job existed. The owner's ruling is that it runs on the pull request
and `check` needs it.

When the touched set contains `internal/tui3` or `internal/session`, `make test`
compiles that heavy package once and runs its sorted top-level tests as
round-robin concurrent shards. `SHARDS` defaults to at most eight and can be set
to `1` to reproduce the serial outcome; the package's whole sharded run still
holds the one-suite-per-box lock.

**`check`** is the third job and the only required name: it needs the other two
and is green only when both are, the same one-spellable-name shape `full tests`
and `cross build` use in `ci-full.yml`. There is no branch protection on this
repository, so this is as blocking as a check can be here: a red `check` is what
every landing script and every person reads, and nothing merges over it by
convention.

Run the same thing before you push:

```sh
make pr-ready
```

`make pr-ready` is the local spelling of `check`: it runs the light-gate pieces
above, then `make test-touched`. The touched target compares `BASE..HEAD`, maps
changed `.go` files to their surviving package directories, treats `go.mod` or
`go.sum` as a whole-tree change, and runs the result through `make test` with
`-count=1 -p 1`. It refuses when a Go or module file is staged, unstaged or
untracked: commit the candidate first so the proof sees exactly what CI will
see, without absorbing another session's work. Pass `BASE=<commit>` to
reproduce a pull request's exact base. A docs-only change has no touched package
and still runs the light half.

This is pull-request parity, not the full-tree ritual. `make check` remains for
Spark, staging, or an intentional full laptop run; it runs the whole test tree,
builds the shipped binary and enforces its size budget.

## What the full gate checks

`ci-full.yml`, seven jobs (three test shards and their one name, the six-target cross build and its one name, `remote`, `size`, and the page):

- **`full tests`** — every package, minus the ledger below, split round-robin
  across three runners. Within each runner, `make test` also runs
  `internal/tui3` and `internal/session` as concurrent test-binary shards from
  one compile; the outer three-way split still assigns each package to only one
  runner. Not for speed first: the
  free-plan runner has seven gigabytes, this repository's test binaries are
  heavy, and the suite's first run was killed under the link load of its last
  eight packages. Three machines carrying a third each stay inside their memory.
  It runs through `make test`, so the ledger it skips and the per-shard timeout
  it carries are the Makefile's and the same as a laptop's. The timeout is
  measured, not guessed: `internal/tui3` once took about 485 seconds serial on
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

## The race detector, which no gate runs

No gate in this repository runs `go test -race`. Not the light gate, not
`touched packages` (so not `check`), not `full tests` on the nightly or on the
way into `staging`, not the release surface tests. The greps say so plainly:
`grep -rn race .github/workflows/ Makefile scripts/laws.sh` finds the word only
inside "traces", in a comment about panic output — there is no `-race` flag in
CI to find, and no gate to name.

**So a green gate proves nothing about data races, and this page is where that
is said rather than assumed.** The detector makes a test run two to twenty
times slower and five to ten times fatter, by Go's own estimate, and `full
tests` is already split three ways to stay inside the free-plan runner's seven
gigabytes. It is also not the light gate's kind of question: a race is a window
between two goroutines, so its answer is not the same on every machine — #957
needed `-count=5` to be sure its quiet first run was quiet. What that trade
cost is on the record there: the detector went red on clean `dev`, on a fixture
that moved the goal owner's clock past its lock while the wall clock read it
under the lock, through a merge whose every check was green. That race was
test-only, and no gate output anywhere said so or said otherwise.

So the detector is run by hand, over the package and the tests a report names:

```sh
go test -race -count=5 -run '^TestADoneEndingNamesTheCheckItCouldNotRun$' ./internal/session/
```

It answers for that package at that moment and nothing else. The known-red
ledger below does not cover this either: it holds tests that fail, not checks
that never run.

## The known-red ledger — burned to zero

There is no ledger any more. `.github/known-red.txt` listed tests that failed on
a clean tree so `make test` could skip them by name and red could still mean
this change did it. Ruled 2026-09-02 that it only shrank, in its own wave, every
entry fixed for real or deleted with a written ruling, and no new entry allowed
after; #408 took the first five out, and the burn finished on 2026-09-12 when
the last entry — the lockdefer scan — was fixed and the file and its
`internal/ci` ratchet test were deleted together (#1012).

**Red means this change did it, now with nothing skipped.** Every run — the
nightly, `touched packages`, the laws — goes through `make test` or reads the
file's old location the same way (`Makefile`, `scripts/laws.sh`, both workflows),
and an absent ledger skips nothing, so "green locally" and "green in CI" are one
fact with no debt subtracted. A test that fails on a clean tree today is a bug
report, not a line to add back — the ledger does not return.

## A test that fails only beside another suite

It is a bug report, and it never goes on the ledger above. Reproduce it under
load — a focused `-count=50` at `GOMAXPROCS=2` with a few `yes > /dev/null`
beside it, **on the bench host and never on a work machine**, since the point is to starve
the machine — and fix the CAUSE, which is almost always that the test measured
the SCHEDULER and called it the road:

- **Wait on the fact, never on a figure.** A fixed number of scripted rounds, a
  margin between two wall-clock numbers, slack "to one short of the next mark" —
  each of those is green on a quiet machine and red beside another suite, and
  widening one is not a fix.
- **A reading beside the work is waited on through the door that owns it.** A
  scripted conversation answers in no time and never blocks, so a reading the
  turn has just started may not have been scheduled at all; `internal/session`'s
  `besideWatch` (sidecar.go) is told by `readBeside` itself when each reading
  under a turn's context starts and lands, and a fixture that opts in
  (`watchReadings`) answers only once none is in flight. The product carries no
  watch, and a law test fails the build if it ever does.
- **A scripted arm orders itself by a signal.** `internal/lane/lanestub`'s
  `StallUntil` holds a stalled answer on a channel and `FirstTokenUntil` holds
  the first word the same way, both after the scripted wait is spent — so a
  primary that must answer *after* the caller has acted says exactly that,
  rather than being scripted a few milliseconds past a bound a busy machine
  eats.
- **Never `t.Skip`, never a retry, never a wider timeout.** A bound is for
  failing honestly when the fact never arrives, not for passing.

## What blocks a merge

Required today: **`check`** on `dev`, **`cross build`** on `staging` and `main`.
Those are the two check names whose green is trustworthy right now.

`full tests` and `remote` run and report, and are deliberately not required yet —
neither has been seen green in this repository's CI twice in a row, and a
required check that has never passed blocks all work on its first day.
`touched packages` is required through `check` since 2026-09-03. **Promote them by
adding their job names to `required_status_checks` in
`.github/rulesets/promotion-pointers.json` as soon as each has been green twice
in a row.** That is the next piece of work here, not a someday.

## When GitHub enforces it

While `Agent-Field` is on the free plan and the repository is private, that
combination has no branch rules — the API answers `403 Upgrade to GitHub Pro`;
the checks above run and show red, but nothing stops a merge on top of red, and
nothing stops a direct push. **Until the org moves to GitHub Team or the
repository is public, all of this is convention.** `.github/rulesets/README.md`
has the state of that.
