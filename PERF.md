# The performance laws

A wave of work in August 2026 took a millisecond and half a megabyte off every
provider call, stopped the frame redoing what nothing asked it to redo, and took
two thirds off the embedded corpora. This file is what keeps it. Every win below
is defended by something that goes red locally, in `go test` or in `make check`,
with a message that says what happened.

## The doctrine: gate on work, never on time

**No gate in this repository is allowed a wall-clock threshold.** Every one of
them counts something exact — allocations, blocking calls, decompressions,
bytes — because a count is a fact about the code and the same fact on a loaded
laptop, on idle CI and on a machine three years faster. A stopwatch is a fact
about the weather. A suite whose red means "the box was busy" is a suite people
learn to re-run instead of read, and one flaky perf test costs more trust than
the regression it was guarding against.

**Any change to a number below must change this file in the same commit.** The
caps are allowed to move — a feature is sometimes worth its bytes, a fix
sometimes pays a residual off — but moving one is a decision somebody signs for
in a diff, never a drift nobody saw.

## The size ratchet

`SIZE-BUDGET` holds one number: the bytes the stripped binary may weigh. `make
check` builds and weighs; over budget is a hard failure naming the size, the
budget, and the overage. `make build` alone never gates, because a developer
rebuilding twenty times an hour must not be stopped by a byte count.

Over budget, there are exactly two honest moves: shrink what you added — pack a
corpus (`internal/packed`), drop a dependency, stop embedding what can be
fetched — or raise `SIZE-BUDGET` in the same commit as the thing that spent it.

The budget was set on linux/arm64 at 48,890,121 bytes with about two percent of
headroom. A different platform or a new Go release moves the figure on its own;
that too is a reason to reset it deliberately, never to ignore red.

It was reset once since, on linux/arm64, when the perf wave merged into
`chat-v3-task`: the remote-access wave that landed on that branch in the meantime
— the relay, the pairing door, the engine host, the far-disk file surface, the
media notes — weighs 201,769 bytes more than the budget the perf wave was
weighed against, and none of it is embedded data a packer could take back. The
binary measured 50,069,769 and the budget was set to 51,071,000, keeping the
same two percent of headroom the first figure was given.

It was reset again on 2026-08-27 after the current branch, built with Go
1.26.5, measured 52,895,586 bytes on darwin/arm64 and 51,380,386 bytes on
linux/arm64. The build-identity package and its packed manual page landed at the
same boundary where accumulated branch growth, the newer toolchain and the
platform difference crossed the former cap. The budget is 53,954,000, two
percent above the larger measured binary; the exact before-and-after source cost
is not disguised as the whole reset.

## The flush ceiling

`FlushUsage` waits at most **2 seconds** (`usageFlushLimit`, `internal/session/usage_ledger.go`)
for every ledger it is flushing, together rather than each. It is the exit path's
cap, not a turn's: `v3Process.closeAll` calls it so the last turn's spending is on
disk before the terminal comes back, and a writer parked inside `openUsageLedger`
on a stalled mount never drains its queue again. Unbounded, that flush never
returned and the terminal never came back — the one failure the rest of that file
is written to prevent, moved off the turn path and onto the exit. The bargain is
the file's own: a spending record is worth less than the turn that earned it, and
less than the exit as well. Pinned by `TestFlushingUsageGivesUpOnAStalledLedger`.

## The worktree fingerprint's budget

The coding engine's post-audit gate compares two observations of the worktree to
decide whether the phases that run after an audit changed the work it passed.
That measurement has exactly **one** budget, and it is a deadline:
**2 seconds** (`worktreeFingerprintTimeout`, `internal/swepro/codeaf/pipeline.go`).

It is derived from what it guards. A fingerprint is taken at most three times
around one full project verification, whose own ceiling is
`fullVerificationTimeoutMS` — ten minutes. Two seconds is a three-hundredth of
that, so the whole measurement costs under one percent of the cheapest single
thing it measures.

**There is no file-count budget and no byte budget, and adding one back is a
regression.** There used to be two — 4,096 files and 8 MiB — spent hashing every
tracked or unignored file in the repository. aforge's own tree is 3,746 files and
75 MB, nine times that allowance, and one tracked file in it exceeds the byte
half on its own. So on this repository every fingerprint came back over budget,
and over budget answered with a **fresh nonce**: no two observations of an
untouched tree could agree, the stabilisation loop could never converge, and five
leaves across two measured runs were failed for "post-audit verification is
self-mutating or exceeded the fingerprint budget" having mutated nothing.

Two rules replace those numbers, and they are the reason no size budget is
needed:

- **The fingerprint photographs the change, not the repository.** git is asked
  what differs from HEAD (`git status --porcelain=v1 -z -uall`) and only those
  paths are hashed, so the cost is the size of the leaf's own change set rather
  than the size of somebody's checkout.
- **A measurement that cannot be taken is not a measurement that came back
  different.** The snapshot answers with a third state, and the gate resolves it
  from the world instead — it re-runs the project's own verification and keeps
  the pass when that is green.

The digest is taken over content and mode. Modification times are the cache key
only: a formatter that rewrites a file with byte-identical bytes has changed the
clock and not the tree. `TestAFingerprintOfATreeTooBigForTheOldBudgetIsStableRatherThanAlwaysChanged`
and `TestAFingerprintReadsContentRatherThanTheClock` pin both.

`maxStableReverifications` (**2**) survives and is not a detector. The detector
is the comparison across a verification: the leaf is finished, nothing but the
verification is running, so a tree that differs across it was changed by it. Two
is how many chances a settling tree gets to settle — one verification that writes
a file and then reuses it settles on the second, and a tree still moving on the
third moves every time.

## The workspace snapshot's budget

What a run left behind is answered by reading the tree, not by asking the clock.
`Workspace.Snapshot` (`internal/exec/workspace.go`) photographs every path a
deliverable could be and `diffTrees` compares two photographs; that ONE pair
answers both spans — the whole leaf (`WatchTree` at the top, `RecordChanges` at
landing) and the single tool call (`RecordProducedSince`, `internal/exec/produced.go`).
It replaces a mtime-versus-a-wall-clock-mark test that misfiled three ordinary
cases: a write landing inside the filesystem's own timestamp granularity, a tool
that preserves the timestamp it copied (`cp -p`, `git checkout`, `tar`), and a
rewrite whose bytes are identical. It is the same conclusion the worktree
fingerprint above reached from the other end.

The cost of that honesty is a second walk: **two bounded snapshots per tool call**
rather than one sweep afterwards. Three budgets bound it, and all three are in
`internal/exec/produced.go`:

| budget | value | what it bounds |
| --- | --- | --- |
| `producedScanLimit` | **6000** entries | one walk. Past it the snapshot is PARTIAL, and a partial snapshot claims no deletions and falls back on the file's own write time before calling anything created. |
| `snapshotDigestFileLimit` | **1 MiB** | the largest file whose bytes are read. Above it the stamp is size, mode and write time — the pre-existing, narrower answer. |
| `snapshotDigestBudget` | **8 MiB** | the total bytes ONE snapshot reads. Past it the remaining stamps are digestless and fall back the same way. |

The two digest budgets exist because the walk's own bound does not bound reading:
six thousand files just under the per-file limit is six gigabytes on the leaf's
critical path, twice per tool call. Against the real case — a workspace holding a
report, a chart and a script — the whole snapshot is a handful of stats and a few
kilobytes of reading, which is why no third budget (a deadline) is needed here the
way it is for a repository-sized fingerprint.

**What degrades past the budget is only the rewrite case.** Created and deleted
files are decided by whether the tree holds the path at all, which reads no bytes
and is the half that was broken; a tree big enough to exhaust the budget gets the
older size-and-clock answer for its tail. The walk is lexical, so both snapshots
spend the budget on the same files and compare like with like.

**A digestless stamp is a narrower answer and never a different one.** Size and
mode still decide first; only when neither sighting could be read does the write
time get consulted at all, and that is the one place a clock is still trusted.
`TestAFileTheClockCallsOldIsStillProduced`,
`TestARewriteWithIdenticalBytesIsNotProduced` and
`TestARewriteAtTheSameLengthAndClockIsStillProduced` pin all three cases, and
`TestProducedFilesAreBounded` pins the per-call cap of **24** paths
(`producedPerCall`) that one command may claim.

## The verification photograph's budget

Running a project's own test suite twice — once before a leaf works and once
after — is the most expensive thing on the bare worker's path. The swe worker's
equivalent baseline `go test` was measured at seven minutes, and it was
invisible enough in the headless stream that an operator read it as a hang and
killed the run. So this measurement is bounded three ways, and the bound is
**derived from the leaf's own wall** rather than typed as a duration.

| number | value | where |
| --- | --- | --- |
| `verify.WallShare` | **8** | `internal/verify/reading.go` |
| `verify.ShortestUsefulReading` | **1 minute** | `internal/verify/reading.go` |
| `capturedOutputLimit` | **4 MiB** | `internal/verify/run.go` |
| `verify.scriptExpansions` | **4** | `internal/verify/runner.go` |
| `verify.rememberedTrees` | **16** | `internal/verify/baseline.go` |
| `verify.memberScanLimit` | **3** | `internal/verify/workspaces.go` |
| `verify.memberLimit` | **4** | `internal/verify/workspaces.go` |
| `verify.scopeScanLimit` | **6000** | `internal/verify/scope.go` |
| `verify.scopeReadBudget` | **2 MiB** | `internal/verify/scope.go` |
| `verify.scopeSelectionLimit` | **40** | `internal/verify/scope.go` |
| `verify.namedSubjectLimit` | **64** | `internal/verify/scope.go` |
| a scope's share of the suite | **`WallShare`, an eighth** | `internal/verify/scope.go` |
| `revision.rememberedJobs` | **16** | `internal/revision/acceptance.go` |
| `revision.producedSweepLimit` | **6000** | `internal/revision/produces.go` |
| `store.VerificationSample` | **8** | `internal/store/verification.go` |

The two share constants moved out of `internal/exec/bare` on 2026-08-29. Two
things read them now — the worker that photographs before the work, and the
delivery gate, which takes the reading of the tree it is about to judge when
nobody else did — and two copies of one cap is how a number in this repository
drifts.

The arithmetic is one line: **one reading may spend `deadline / 8`, and a
reading worth less than a minute is not taken at all.** A ninety-minute leaf
affords 11m15s a reading, which is a real suite — codeaf's own project
verification ceiling is ten minutes, so this is the same order and arrived at
from the other end. A sixty-second leaf affords 7.5s, which is under the floor,
so **it photographs nothing and runs no command**: a leaf too short to afford
the measurement does not take it rather than spending its whole life measuring.
The shortest wall that photographs at all is therefore eight minutes.

The floor is derived too. One minute is the fastest whole project suite measured
in the 2026-08-28 sweep — igel's two passing project tests, "2 passed in 27.86s"
— doubled to leave room for an interpreter, an import graph and a compile.
Below it the reading is killed before the runner says anything, so it costs an
eighth of a wall for a result that names nothing.

Two more conditions keep the worst case off the common path, and neither is a
clock. A reading is taken only when the project **declares** a test entrypoint
(`verify.Discover`), and the second reading is taken only when the tree actually
moved — this leaf's own before-and-after comparison of the workspace, or an
earlier round of the same job having already changed it. A first leaf that
changed nothing cannot have regressed anything; a continuation that changed
nothing still hands over a tree an earlier round may have broken, which is why
the inherited baseline is reason enough on its own. So the quarter-of-the-wall
worst case is paid only by a long leaf, in a project that says how it is checked,
in a job that actually wrote something.

`capturedOutputLimit` bounds the memory rather than the time: it keeps the last
4 MiB of a reading's output, ten times the largest suite output in that sweep
(textual's 391,519-byte log for twenty failing tests with full tracebacks). It
is a tail because every runner prints its failure summary last.

**What is run is the runner, not the lifecycle script, and that is a saving as
well as a repair.** `verify.ReadingStrategies` follows the project's declared
entrypoint into its own body — a package script, a make or just recipe, through
that file's own variable table and past the ecosystem's launcher (`npx`, `pnpm
exec`, `poetry run`) — and takes the reading from the test runner it finds there,
asked for its own machine-readable output. `scriptExpansions` bounds that walk at
**4**, which is the deepest a chain can usefully be (`test` runs `test:unit` runs
`vitest`, plus one) and is what stops a reader following a cycle; the same bound
settles a variable that names itself. A lifecycle script that lints and
typechecks before it tests no longer spends the reading's budget on the lint, and
a formatting complaint no longer exits 1 in front of a suite that was never run —
which is exactly what happened on ofetch s5, where the gate read "`pnpm test`
exited 1 and named 0 checks" of a suite that was green.

**The ladder costs nothing when the first rung reads.** `ReadingStrategies`
returns up to three rungs — the runner as the project invokes it, the runner as
it invokes itself, and the project's declared entrypoint read as plain text —
and `verify.Photograph` walks them inside ONE `ReadingBudget`, stopping at the
first that names anything. A rung is only paid for when the one above it named
nothing, which is a rung that told us nothing about the suite: textual's own
`make test` passes `-n 16 --dist=loadgroup` and its task image has no
pytest-xdist, so it exits in eight seconds on `unrecognized arguments: -n`. The
LAST rung's answer is taken whatever it named, because a runner that reports no
identities is a real reading with an empty roster.

**A repair round takes no baseline reading at all, and a job that cannot be read
pays for finding that out once.** The baseline is the job's, taken once before
the job's first change and inherited by every continuation
(`verify.BaselineFor`), so the second and later rounds of a job spend their
eighth of the wall only on the after reading. The REFUSAL is remembered the same
way: why a reading could not be taken is a fact about the tree, the project and
the wall, and none of them move between rounds. Measured: textual s6's bare leaf
spent 5m27s on a suite killed at its ceiling — its whole suite is 3,422 tests and
takes 793s in that image, measured — and without this every
continuation of that job would spend the same 5m27s to learn the same thing. `verify.rememberedTrees` is **16**,
a bound on memory rather than on behaviour: a reading is two rosters and an
entrypoint, sixteen of them is kilobytes, and sixteen is more concurrent working
directories than any surface here opens. Past it the oldest is dropped, and
dropping a baseline costs a re-photograph rather than a wrong answer.

**And a reading that could not be taken is written down.** There are four ways
to have none — a project that declares no verification, a wall that cannot afford
one, a shell the preamble cannot be trusted in, and a command killed at its
ceiling — and they cost a run nothing, nothing, nothing and an eighth of its wall
respectively. All four used to return one silent zero value, so an autopsy could
not tell the free ones from the expensive one: textual s6 held no row saying its
five and a half minutes had been spent. Each now carries its own sentence on
`verify.Reading.Unread` and is journaled as a `verification` event with
`read: false`. It costs one row.

**A READING IS SCOPED BEFORE IT IS BOUNDED, AND THAT IS WHERE THE SAVING IS.**
The budget above was doing exactly what it was written to do and it still
produced no reading, because nothing had decided WHAT to measure before deciding
how long to measure it. textual's whole-repository `pytest` collects 3,422 tests
and takes **793s** in its own task image, against the **5m30s** its wall
afforded: the only rung the ladder had was one that could not finish. So
`verify.ReadingStrategies` now puts a SCOPED rung in front of the whole ones —
the runner handed the checks adjacent to the change — and the whole suite is what
is below it.

The scope is derived from the job's own `verify.Focus`: the names the person's
request uses and the paths the record shows the work touched. **A request usually
spells no path at all** — happy-dom's says "Implement `observe()`, `unobserve()`,
`disconnect()` and `takeRecords()`" and names `IntersectionObserver`, and
contains no path — so `verify.NamedSubjects` reads the identifiers too (CamelCase
or snake_case with at least two segments, capped at `namedSubjectLimit` **64**)
and `verify.Locate` resolves each of them, WHOLE, against a source file the
workspace holds. A name that matches nothing costs nothing; a name that matches
is a fact about where the work is. Without it happy-dom s8 had an empty focus, no
touched package, and both its readings taken at the repository root and killed at
their ceiling.

**Adjacency is a relationship and never a substring**, which is the second half
of the same repair. `verify.Adjacent` selects by structure only: rank 1 is the
test file NAMED AFTER a touched file under the runner's own convention
(`test_<stem>.py`, `<stem>.test.ts`, `<stem>_test.go`) plus the test files beside
it; rank 2 is the test files whose IMPORT STATEMENTS resolve to the touched
module — through the package entry point's own re-exports for Python, and by
resolving relative specifiers for JavaScript. Only import lines are read and only
whole identifiers match, so `Log` never matches inside `Logger` and `log` never
matches inside `dialog`. textual s8 flattened every name to its letters and asked
whether a test file's TEXT contained one: the selection came back as **40 of 251
files** — a third of the suite, spanning animations, command_palette, css,
directory_tree, document, footer and input — and the reading was killed at 1m53s
naming nothing. The structural answer for that same job is **3 files**.

**And a selection larger than an eighth of the suite is not a scope.** Past that
it is a sample of the same order as the whole thing and the whole rung below it
is a better reading for the same money, so the selection is cut back to its rank-1
core (or, where nothing is named after the change, to the eighth itself). The
eighth is `WallShare` spent on the other axis, for the reason it is spent on the
first: an eighth is the share of a thing a measurement may take before it has
become the thing. It applies only to a suite larger than `scopeSelectionLimit`,
because below that the whole rung costs about the same and trimming loses roster
for nothing.

The selection is bounded three ways — `scopeScanLimit` **6000** entries walked (`internal/exec`'s
`producedScanLimit` at the other end of the same question), `scopeReadBudget`
**2 MiB** read to find the checks that IMPORT a touched module, and
`scopeSelectionLimit` **40** paths on one command line. Past any of them the
answer is "no adjacent checks", the ladder falls to the whole rung, and nothing
is wrong except that a very large repository paid for a scoped reading it did not
get. A job that named nothing gets `scope: whole`, which is what every reading
here was before this existed.

**The scope is part of the reading's identity, so the comparison compares like
with like.** `Strategy.comparable` is command AND workdir AND scope, and
`Reading.Regressed`/`Vanished` refuse to subtract when the two halves disagree
about any of them — a before reading of a whole suite minus an after reading of
three files is every unselected check reported as one that stopped existing. The
strategy is pinned on the first reading and re-used verbatim for the second, so
in practice they always agree; the test is the fail-safe, not the mechanism.

**A rung is entitled to its share of the budget or it is not started.**
`verify.photograph` walks the ladder inside ONE `ReadingBudget` and refuses any
rung past the first with less than `budget / len(ladder)` left. It is
`ShortestUsefulReading`'s rule one level in — the thing being divided is the
reading's budget rather than the wall — and it stops a scoped rung that spent
most of the budget handing the whole-suite rung a scrap and a certain timeout.

**A monorepo is read in the package the work touched, not at its root.**
`verify.Members` reads what the repository declares itself to be made of —
`workspaces` in package.json, `packages:` in pnpm-workspace.yaml, `packages` in
lerna.json, `[workspace] members` in Cargo.toml, `use` in go.work, and (for a
turbo/nx/rush repository that declares no member list) a bounded walk for nested
manifests at `memberScanLimit` **3** levels. `verify.MemberFor` files a touched
path under the nearest manifest above it, and `TouchedMembers` orders the
packages most-touched first and cuts at `memberLimit` **4**, which is the
ladder's own shape. Measured in happy-dom's task image at its base commit on
2026-08-29: the root's declared `npm test` (`turbo run test`) exits 1 in **5.3s**
with 0 of 4 tasks successful and names **no check of any package**; `npx vitest
run --reporter=json` at the ROOT is killed at a 180s ceiling naming **nothing**;
and the reading this change takes — vitest inside `packages/happy-dom`, scoped to
the touched test file — exits 0 and names **173 checks**.

**A cut reading is retaken at the size its own pace affords.** Every other
refusal is a fact about the tree, the project or the wall and is inherited by
every round; a scoped reading killed at its ceiling is a fact about a size THIS
PROGRAM CHOSE, and `verify.Reading.Retakeable` is what stops a job inheriting it.
The arithmetic is deliberately modest about what a cut proves: it proves the
selection cost MORE than `CutAfter`, so `CutAfter / files` is a lower bound on
the per-file cost and the count it yields is a CEILING, never a target — taken as
a target it says a reading killed at 1m53s over forty files can be retaken over
thirty-six, which is the same reading again. So `Pace.Affords` is the smaller of
that ceiling and a halving, and `Strategy.retakeSize` puts the rank-1 core under
it as a floor: textual s8's forty files narrow to the three named after what it
touched, not to the twenty a halving alone would reach.

**A cut reading keeps what it named, and the run learns the pace.** A command
killed at its ceiling used to return nothing at all; ink s7's `npx ava --tap` was
cut at **1m53s** having already streamed part of its 922 checks, the whole
reading was discarded, and the next round's gate passed a deliverable at 13 of 25
hidden checks with no roster to weigh. What the runner named before the cut is
now kept as `Reading.Partial` — a partial roster answers "does a check for this
exist" and answers "did this work break something" not at all, so it is legible
to the acceptance settlement and refused by the comparison. `Reading.CutAfter`
records how long it ran, journaled as `elapsed` on the `verification` event; it
is the only thing a run ever measures about the PACE of the machine it is on,
which matters because these readings are taken in amd64 containers under qemu
where everything is five to ten times slower than the wall-derived arithmetic
assumes. The reading and its reason are remembered against the job, so **the same
blind ceiling is never spent twice**: the tightening that follows a cut is the
scoped rung, and it happens before the ceiling rather than after it.

**The gate takes the job's reading when no worker did.** Not every worker
photographs — only `internal/exec/bare` does — and textual s7 ran every node
under the generalist and reached its gates with no reading in the store at all.
`revision.jobReading` reads the job's remembered baseline first (free), and takes
one itself on `ReadingBudget(time until the gate's own deadline)` only when
nothing anywhere has looked, remembering it against the job so it costs one
reading per job rather than one per round.

**What a person would see if this were wrong.** Too generous, and short leaves
stop doing work — a `do` run whose nodes each sit for minutes with nothing in
the stream but the suite they are running, which is exactly the failure that got
a run killed by hand once. Too mean, and `Outcome.Regressed` is nil on every
leaf that should have carried a name, which reads downstream as *no claim* and
lets a patch that deleted an attribute the repository already had ship as whole
— the measured failure in `docs/design/gate/SETTLEMENT.md` §4. Neither is a test
going red; both are read off a run, which is why the numbers are written down
here.

## What the acceptance checklist costs

The gate's acceptance settlement (`docs/design/gate/ACCEPTANCE.md`) adds no suite
run to the common path and one bounded model call to two seams.

| number | value | where |
| --- | --- | --- |
| the checklist's length | **one point per clause of the request** | `plan.NormalizeAcceptance` |
| the acceptance finding's size | **one entry per REQUEST LINE, 8 named then a count** | `revision.groupUnexercised` |
| behaviours a finding names | **8, then a count** | `revision.regressionsNamed` |
| the gate's own reading | **`verify.ReadingBudget`, above** | `revision.Evidence.measureFinalTree` |

**The checklist's length is derived from the request and not typed.** A request
cannot state more behaviours than it has clauses, so a request with forty
statement-ending marks affords about forty points and a one-clause request
affords one. There is no constant here for a later wave to tune wrongly, and past
that ceiling a model has stopped describing the request and started describing
the domain.

It counted LINES until 2026-08-29, and that was the wrong unit measured twice. A
person writes "defaults are `threshold = 5`, `cooldown = 30000`,
`halfOpenMaxRequests = 1`" on one line and has stated three behaviours a check
either exercises or does not; the s5 sweep held four points against igel's
twenty-four hidden checks and five against textual's twenty. A clause — text
either side of a full stop, semicolon, colon, question or exclamation mark with
whitespace after it — is the smallest unit a person writes one behaviour in, so
it is the unit the ceiling counts. A comma is deliberately not a boundary: it
sits inside names and numbers as often as between statements, and counting it
would raise a ceiling the request never earned.

**The finding's size is bounded by the request's own lines, not by the
checklist's length.** Points are derived per clause, which is right for the
mapping — four defaults are four things a check either exercises or does not —
and wrong for the finding, because a repair round aimed at four halves of one
sentence is four rounds aimed at one sentence. So the finding groups the
unexercised points by the LINE of the request they were read from, and one
grouped entry is one thing to go and check. The number of things the finding can
ask for is therefore bounded by the number of things the person wrote, and by
nothing this program chose; past eight entries it names eight and counts the
rest, which is `revision.regressionsNamed`, the same bound its sibling findings
spell.

**One call at plan time**, on the request alone, beside the compile that already
read it. **One call at the gate, on every verdict.** It used to be asked only of
a delivery the model judge was about to pass, on the reasoning that a failing
gate buys its repair round anyway — and the s5 sweep's ten delivery gates all
failed, so the question was asked at none of them and no run in the sweep holds a
mapping at all. The reasoning was wrong twice over: a repair round is aimed at
the gap the gate NAMED, so a round bought for a missing branch name leaves every
unexercised behaviour where it was; and a mechanism that runs only on the happy
path is a mechanism nothing exercises. The call is still bounded by the same
context budget, still skipped entirely when there is no checklist, and it costs
nothing at all when the roster is empty — `MapChecks` answers "nothing mapped"
without a wire call, which is the true answer and the finding this whole
mechanism exists to raise.

**No second suite run.** The reading the gate weighs is the one the leaf's own
verification photograph already took of the final tree. It runs `verify.RunTests`
itself only where that photograph has no after half — a repair that rewrote the
account rather than the code, or a worker whose wall could not afford the second
reading — and it runs it on the same `verify.ReadingBudget` share as everything
else. So the quarter-of-the-wall worst case above is unchanged.

**The checklist and its finding are the JOB'S, and that is what keeps the cost at
one call a round.** Both used to live on one node's spec. A continuation is
planned afresh and its spec carries none, so ofetch s7 mapped fifty-four points
in round one, named eighteen behaviours nothing exercised, bought a repair — and
rounds two, three and four hold ZERO mapping rows and raised prose gaps about the
deliverable's wording instead. The run ended at 41 of 47 with the same defaults
untested that round one had named out loud. So the checklist is remembered
against the job (`revision.RememberChecklist`, keyed by `verify.JobKey` — the
identical key `verify.BaselineFor` uses, for the identical reason), and so is
what the last measurement found nothing exercising. `rememberedJobs` is **16**,
`rememberedTrees`' sibling and a bound on memory rather than on behaviour.

Two rules follow, and neither costs a call. **A finding measured once stands
until a measurement closes it** — a round whose worker took no reading inherits
the open set rather than passing over it. And **the set can only shrink by
world evidence**: a round that wrote the missing checks grows the roster with
names the mapping then matches, and the next mapping is what notices. The
finding's last line is the score, `N of the M behaviours this request states are
still exercised by nothing`, so a repair brief says what REMAINS rather than
restating the list.

**A pass over a suite nobody could read is not whole.** `store.DeliveryGate.
Unreadable` says the project DECLARED a way of checking itself and this run could
not read it, and `Whole()` spends it: the run settles partial, exit 2, with
`partial — nothing in this project's verification could be read: <why>` as its
last line. ink s7 journaled its cut `npx ava --tap` correctly and then passed the
next gate over an empty roster at 13 of 25 hidden checks. A project that declares
NO verification is deliberately not charged for it — the question is unanswerable
rather than unanswered — and which of the two it was is journaled either way.

**What a person would see if this were wrong.** Too generous a checklist and
every delivery fails on behaviours the person never asked for, which is the
invented-scope failure the grounding invariant exists to stop — that is why the
checklist goes through the same door a review's finding does. Too mean and the
gate is back where s4 left it: passing "All 56 tests pass" at 41 of 47.

## The repair round's room, which is not a clock and not a count

A run that holds a finding it agrees with, with wall and money left, buys the
work that closes it. The DeepSWE sweep of 2026-08-28 measured the opposite: a
ninety-minute wall, eight cents spent, and eight of eight graded runs stopping at
a tenth of it by choice. **Raising the wall buys nothing and raising a retry
count buys a retry of the same blind decision** — see
`docs/design/gate/SETTLEMENT.md` §3.

So the bound on repair is evidence, and there is exactly one number in it and it
was already there:

| number | value | where |
| --- | --- | --- |
| `MaxOverrunRounds` | **3** | `internal/resident/grow.go` — the BACKSTOP, unchanged |

What actually stops a lineage is what the growth journal measured: two rounds in
a row that left nothing on disk (standstill), or the same remainder handed over
twice (fixed point). The spent-citation ledger now defers to the same evidence —
words whose round positively moved the tree may buy another, and **everything
unknown stays spent**, which keeps the bound's direction wherever the journal is
missing.

One clock enters, as a floor rather than a ceiling, in `outOfWall`
(`internal/revision/judge.go`): **a repair is bought only while the run's own
deadline still holds as long as the attempt that produced the finding took.** It
is derived from two things that already exist — the errand's deadline, which
`aforge do --timeout` sets, and the node's start, which the store stamps — so it
is not a knob and not a typed duration. A repair the wall will kill mid-flight
spends money to deliver nothing, and a run that stopped for want of time is
**partial** (exit 2), never whole. An unknown deadline or an untimed attempt
answers empty and buys the round: this makes runs longer, not shorter.

## The shaped answer's room, which is derived and never named

Every call that asks a model for a JSON object goes through `internal/shaped`,
and the ceiling it goes out with is **derived from the ask**. Before the seam
existed each pass named its own: 8192 in the planner, a reserve-eighth in the
delivery gate, eight thousand plus an echo in the intent compiler. Each was
right about the call in front of its author and wrong about the next one — and
wrong in the direction that costs a whole run, because a fan-out asking for five
parts was given the room for one verdict, cut off mid-part, and the run exited
with zero nodes (`bench/deepswe/AUTOPSY.md`, s4, `textual-richlog-follow-state`).

```
room = one object × how many objects the ask asks for + what the answer echoes
```

| term | value | where |
| --- | --- | --- |
| one object | `CompletionReserve() / 8`, floored at **4096** | `objectShare`, `objectFloor`, `internal/shaped/ceiling.go` |
| how many | the ask's own figure — `fanOutWidth` (**5**) for a fan-out, the node count for the per-node passes, **1** everywhere else | `Ask.Answers` |
| the echo | `2 × len(material) / 3` — tokens ≈ bytes/3, twice, because the compiled goal restates the request and then quotes it | `Ask.Echo` |
| the memo | the widest cut this model has been watched taking on this lane, **doubled** | `provider.WidestAnswerCut`, `model-quirks.json` |
| the bound | never past `CompletionReserve()` | `reserve()` |

Two properties are the whole reason this is safe to adopt everywhere.

**Every one-object ask comes out with exactly the ceiling it already had.** The
share and the floor are the delivery gate's own measured numbers, moved rather
than changed, so nothing anybody measured moves. Only an ask for MORE than one
object gets more room, which is the finding.

**Nothing here is a clock and nothing here is a retry count.** A repair
continues while the last round added text and the answer is still an unterminated
object, bounded by the reserve — both quantities move one way, so the loop ends
on what happened rather than on a number somebody guessed. A reply that never
began an object is asked again exactly once, at doubled room, which is the same
arithmetic `plan.retryTokenBudget` and `revision.retryVerdictTokens` each wrote
separately and which is now written here alone.

The width the fan-out prompt states and the width its ceiling is derived from
are one constant, interpolated into the prompt (`fanOutWidth`,
`internal/plan/fanout.go`). `TestAFanOutIsSizedForTheWidthItsPromptPermits` fails
if they ever become two.

`internal/shaped/shaped_test.go` pins the derivation, the operator's reserve
never being outrun, and the three repairs.

## The in-turn working-set ceiling

A single tool-heavy turn starts folding already-seen tool results at **64,000
tokens**, or half the trusted context window when that is smaller. It preserves
the recent **20,000-token** tail (already the compaction tail law) and folds to
the midpoint between that tail and the trigger. The lower target is part of the
performance contract: rewriting one result makes the provider cache cold from
that byte onward, so a pass that stopped just under the trigger would repay the
whole cold prefix one tool round later.

The pass changes only tool-result messages, in whole oldest-first batches. The
person's message, assistant text and the newest batch the model has not seen are
never candidates; every replaced result remains readable through its stub path.
`TestALongTurnsToolWorkingSetStaysBounded` pins the 60-round request ceiling and
the readable bytes, while the other `turnfold_test.go` cases pin the no-op below
the line and the unseen-result horizon.

## The allocation laws

| Law | Where it is pinned |
| --- | --- |
| A warm tool-schema encode allocates **nothing**. The belt is append-only, so the memo hands back the slice it holds. | `internal/provider/alloclaws_test.go` |
| A warm transcript encode costs the **same** at 81 turns as at 8 — 1 allocation automatic, 8 with breakpoints. Encode runs once per call, so a per-call cost linear in the transcript is a per-run cost quadratic in the run. | `internal/provider/alloclaws_test.go` |
| The babble guard builds **one** zlib writer per stream and Resets it per window; a window costs at most 4 allocations. A writer per window is a hundred kilobytes of deflate state per five hundred bytes of reply. | `internal/provider/alloclaws_test.go` |
| The hub's backlog fold is **amortized constant per delta**: ten times the deltas for less than twice the allocations. `Text += delta` is quadratic — 1.6 GB of copying over one long reply, under the hub's lock. | `internal/session/alloclaws_test.go` |
| A frame with a four-thousand-line draft costs what a frame with a twelve-line draft costs. | `internal/tui3/inputsmooth_test.go` |
| Scrolling a **4,000-line transcript** by one screen allocates at most **220** times and re-renders **zero unchanged entries**. The residual is composing the visible frame, not wrapping history. | `internal/tui3/inputsmooth_test.go` |
| Message-part reads and the v2 token formatters allocate nothing. | `internal/store/message_parts_test.go`, `internal/tui2/tokens/format_test.go` |

Correctness is pinned separately and deliberately so: `memo_test.go` proves the
memo's bytes are the direct path's bytes, `attach_test.go` proves the fold spells
the whole answer. Those tests say the fast path is *right*; the ones above say it
is still *fast*.

## The connection laws

Over `--host` the surface runs on the laptop and only the engine is far away
(docs/REMOTE.md), so every question the surface asks its agent is a round trip
down an ssh pipe with a ten-second deadline on it (internal/remote's
`callDeadline`) — and every one of them is made from the update loop, which is
the one goroutine that also decodes keys, resolves clicks and paints. A question
asked while DRAWING is therefore a question asked thirty times a second, and one
asked while resolving a POINTER is asked once per cell the pointer crosses.

| Law | Where it is pinned |
| --- | --- |
| **A frame over a connection asks the far machine nothing.** | `internal/tui3/hostlatency_test.go` |
| **A pointer motion over a connection asks the far machine nothing** — including one below the conversation, which rebuilds the chrome to find its row. | `internal/tui3/hostlatency_test.go` |
| **The model picker draws its whole list for nothing**, however many rows it is showing. | `internal/tui3/hostlatency_test.go` |
| **The frame clock's beat makes no call on the update loop.** What it reads it reads as a `tea.Cmd`. | `internal/tui3/hostlatency_test.go` |
| **A key over a connection asks the far machine nothing** — thirty-six of them, typing and moving. | `internal/tui3/hostlatency_test.go` |
| **A submit over a connection is exactly one call.** The sentence goes up and nothing else does; the update that echoes the line on screen is zero, because the call happens on the command. | `internal/tui3/hostlatency_test.go` |
| **A turn ending is zero.** The settle reads the spending, the weight and the effort table off the replica, which the engine has already refreshed ahead of the turn's own ending. | `internal/tui3/hostlatency_test.go` |
| The five facts a frame draws — the model, the name, the spending, the weight, the effort rung — answer from the replica and never from the wire, and the effort table is whole from the first frame. | `internal/remote/replica_test.go`, `internal/tui3/hostlatency_test.go` |
| **A frame over a connection walks none of THIS disk for the far machine's paths.** Home keys every row by its transcript path, and resolving a far path's symlinks here is a stat of a file that was never on this machine — on macOS `/home` is an automounter's mount point, so each one waited on autofs. A hosted key is the cleaned spelling (`app.convKey`). | `internal/tui3/home_test.go` |

`remote.Client.CallsMade` exists for these pins and for nothing else — one
atomic add inside the one door every call already goes through. It counts calls
and never stream frames, because a turn's events are the work a person asked for
and a getter is work nobody did.

They were written after the owner reported that over `--host` "even hover seems
to slow everything down", and that clicks and keys felt dead. It was one defect
in three places: the status row asked the agent what the model was dialled to
while it was drawing, a hover below the conversation rebuilds the chrome, and
the frame clock read the session's cost on the loop. At the twenty-millisecond
round trip a real link has, a pointer swept across the foot of the window put
about thirty-six milliseconds of network in front of the update loop per cell —
so a two-hundred-cell sweep left seven seconds of keystrokes and clicks queued
behind it, and a six-hundred-cell sweep twenty-two. Driven through tmux over a
pipe with that delay, a typed character took 7.4s to appear after two hundred
motions and 21.8s after six hundred; after the fix, 0.014s, which is what the
same script measures on a local session. internal/tui3's reasoninglevel.go holds
the fix.

The disk is the other far machine. The day after the wire was taken out of the
frame, typing on a hosted home took 250ms to 1.6s per key with the wire silent
and the CPU idle: every row's transcript path was being resolved with
`filepath.EvalSymlinks` on the laptop, and `/home/...` on a Mac is autofs
territory, where an `Lstat` waits on the automounter. A goroutine dump taken
mid-stall found it (`homeTrue → convKey → EvalSymlinks → Lstat`); a CPU profile
had not, because waiting is not computing. So the law is about any syscall on
a path that belongs to the other machine, and not only about the wire.

The number that is NOT pinned here is the boot: opening a hosted conversation
costs six calls, one of them the current model's dial. Six is a launch cost paid
once with a person watching a connection open, which is the moment waiting is
correct; the laws above are about the moments it never is.
## The storm laws

The section above took the far machine out of the pointer's way. What is left is
the one cost every input message pays whether or not anything is far away: a
pointer swept across the window sends **one message per cell it crosses** — six
hundred for a fast diagonal — and Bubble Tea builds a frame after every one of
them. It writes one per sixtieth of a second, so nearly all of those frames are
built and thrown away, and the keystroke behind the sweep waits for all of them.

Measured on the loopback client through a pipe with a 20 ms round trip, with the
connection laws above already in place, delivering the whole sweep in **one
write** — which is what a terminal actually does, and what `tmux send-keys` in a
loop cannot reproduce because it paces itself at about 7 ms an event:

| A typed character appears after… | before | after |
| --- | --- | --- |
| no motion at all | 0.015 s | 0.015 s |
| 600 motions in one write | 0.045 s | 0.013 s |
| 3000 motions in one write | 0.204 s | 0.016 s |
| 600 wheel notches in one write | 0.047 s | 0.013 s |

The before column is linear in the burst — 0.07 ms a message, all of it frame
building — and the after column is flat. That is the point: **the cost of a
storm no longer depends on how big the storm is**, so a per-message cost added
back tomorrow cannot resurrect the stall.

`internal/tui3/coalesce.go` is the fold. The positions between the ends of a
sweep are not information; they are the same claim made six hundred times, and
every one but the last was already false when it was read. So the newest is
kept, the rest are dropped, and the surface answers **once per frame** —
`pointerEvery`, which is half of `frameInterval` because Bubble Tea writes at
60 Hz while the surface animates at 30 Hz. A folded message also declares the
frame before it rather than building one, because it
provably changed nothing `app.View` reads; that half is the larger one, and it
is only reachable because the fold is what knows.

| Law | Where it is pinned |
| --- | --- |
| **Six hundred motions cost one answer**, and 598 of them are folded away. | `internal/tui3/coalesce_test.go` |
| **A key never waits behind a sweep.** A hundred keys behind a six-hundred-motion storm are handled without the router answering a single motion on the way, and each character is in the draft when its own `Update` returns. | `internal/tui3/coalesce_test.go` |
| **Keys are never folded and never reordered** — not against each other and not against the motions around them. | `internal/tui3/coalesce_test.go` |
| **A sweep is answered where it ended**, never at a position it crossed. | `internal/tui3/coalesce_test.go` |
| **A folded wheel run scrolls exactly as far as an unfolded one.** A scroll is a distance, so a folded run owes its whole length and spends every notch of it at the frame. | `internal/tui3/coalesce_test.go` |
| **A folded message builds no frame**, at a handful of allocations against a frame's tens of thousands of bytes. | `internal/tui3/coalesce_test.go` |
| A pointer ARRIVING somewhere is still answered on the spot, with no clock in between: only the SECOND motion in a row is a sweep. | `internal/tui3/coalesce_test.go` |
| A sweep whose previous answer is already one pointer interval old answers its newest position immediately, with no second clock. Dense bursts keep the one-answer-per-frame ceiling; an overdue arrival does not begin another wait. | `internal/tui3/coalesce_test.go` |
| A pointer crossing a row it is already on still leaves no stale entry, no dirty flag and no frame. | `internal/tui3/inputsmooth_test.go` |

**Why a fold and not a drain.** `internal/session`'s stream is coalesced by
taking events off a channel until it would block (`waitEvent` in `app.go`) —
the honest way, because the backlog is in hand. A Bubble Tea program has no such
channel to reach: `Program.msgs` is unbuffered and private, the input reader
hands over one message at a time and blocks until the model has taken it, and
the rest of a storm is unparsed bytes in the terminal's own pipe. There is
nothing queued to drain and no way to look ahead, so the fold is made forward
instead — keep the newest position, answer it on a clock of its own.

**What Bubble Tea already rate-limits, and what it does not.** Measured against
v2.0.8, not assumed: the renderer writes to the terminal on a 60 Hz ticker
(`startRenderer`), and `render(view)` only stores the view under a lock. But
`model.View()` is called after **every** message. A clean frame over a
twenty-turn conversation measures 31 µs and 19 KB, independent of transcript
length — the row list is cached (`app.visible`) and the chrome around it is not.
Thirty-one microseconds times six hundred is the middle row of the table above.

**INTENT UP, FACTS DOWN** is the shape that keeps all of these true as the
surface grows, and it is the second half of the same fix. The first half stopped
the draw path ASKING; this half stops there being anything to ask. The engine
STATES its fact set — the model, the session's name, what has been spent, what
the conversation weighs, and the effort rung held for every model anybody has
dialled (`session.Facts`) — in the welcome and again on a `facts` frame whenever
one of them moves: a turn ending, a name settling, a compaction landing, somebody
turning the model or the rung. The surface keeps a replica of it
(`internal/remote/replica.go`) and every getter on the frame path is a memory
read of that.

So the surface-side tables the laws above are drawn from are FED rather than
filled: `internal/tui3`'s reasoninglevel.go seeds itself from the whole map the
welcome carries and is complete at boot rather than a level behind, and
`app.settle`'s two reads at a turn end are the replica's, taken after the engine
has already restated them.

The rule for the next thing anybody adds: **a fact a frame reads belongs in
`session.Facts` and is stated; a question a person opened a door for may be
asked.** The transcript, the rewind points and a file fetched from that machine
are all the second kind, and all of them are off the frame path.

The number that is NOT pinned here is the boot. Opening a hosted conversation
costs five calls, read off the engine's own side of a real ssh pipe: the
transcript, the earlier history, the recent sessions, this workspace's standing
items and the questions held for somebody to come back to. Every one is a launch
cost paid once with a person watching a connection open, which is the moment
waiting is correct, and NOT ONE OF THEM IS A FACT A FRAME READS — those arrive
in the welcome. The laws above are about the moments waiting is never correct.

The other thing that shows on a real link and is not pinned here is the standing
band's own beat, which asks the engine for this workspace's items every few
seconds. It is a poll of the far machine's DISK rather than a fact a frame reads,
so it is a different lane's question; it is written down because anybody counting
frames on a real connection will see it and should know what it is.

## The drop laws

The storm laws are about a pointer. This one is about the keyboard, and it is the
same bargain in the other direction: `internal/tui3/dropkeys.go` sits on the ONE
line every typed character in the program passes through, because some terminals
deliver a dragged file as KEYSTROKES rather than as the bracketed paste
imagepaste.go was written for. A cost added there is a cost paid per character
typed, forever, by everybody — so what it is allowed is counted rather than
described.

| Law | Where it is pinned |
| --- | --- |
| **Ordinary typing arms no timer and asks the disk nothing.** A sentence of prose costs two integer comparisons a character and nothing else. | `internal/tui3/dropkeys_test.go` |
| **Typing a slash command costs the same.** A dropped path is told from a command by a SEPARATOR INSIDE IT — `/var/folders` has one, `/help` does not — which is string work on runes already in memory. | `internal/tui3/dropkeys_test.go` |
| **A burst arms ONE wakeup**, however many characters it holds, and the one in flight re-arms itself while characters are still arriving rather than a second one being asked for. It is `pointerFold.settling`'s shape exactly. | `internal/tui3/dropkeys_test.go` |
| **A settled burst asks the disk at most once per word it holds**, and only after the string gate above has passed. | `internal/tui3/dropkeys_test.go` |
| **A burst that names nothing builds no frame.** It provably mutated nothing `app.View` reads — the characters were already in the draft, put there by the keys that carried them — so it declares the frame before it, exactly as a folded motion does. | `internal/tui3/dropkeys_test.go` |

`dropQuiet` is two `frameInterval`s and it is a QUIET WINDOW rather than a
deadline: the fold settles when the sender has STOPPED, which is the only moment
the run in hand is the whole path. A fixed deadline would convert `/a/b.png`
while `/a/b.png.orig` was still arriving. Like every other clock in this file it
is a mechanism and not a gate — no test here waits on it, they move a seam clock
and deliver the wakeup by hand.

## The launch-path pins

Two costs can hold a terminal dark before anything is drawn in it, and neither
looks slow in review: a question put to the model catalog through the door that
waits (a `GET /models` with a fifteen-second ceiling on a cold cache), and a
packed corpus decompressed. Both are counted, in `cmd/aforge/launchlaws_test.go`,
against a catalog endpoint that refuses immediately.

- **Nothing on the way to the first frame unpacks a corpus.** Zero, for
  `--version` and for the whole chat launch. `packed.Unpacks()` is the reading.
- **Wiring a conversation's subharnesses asks the catalog one blocking
  question**, and it is not this surface's: `subharness.go`'s linear
  constructor, shared with the headless doors where waiting is correct. What
  `chatv3_subharness.go` asks for itself is zero — it reads the window through
  `catalog.Catalog.ModelsNow`, which answers nil while the catalog warms.
- **The whole launch asks sixteen**, and that figure is a ratchet, not a law.
  Fifteen of them come from `v3RunHarness` building the harness tool bridge
  eagerly, which arms the media hands, each of which asks which model would
  draw, see, speak or sing. Nothing in the first frame reads any of those
  answers. It is written down so it is a known debt rather than a discovery, and
  the only direction it may move without a conversation is down.

`catalog.Catalog.BlockingReads` and `packed.Unpacks` exist for these pins and
for nothing else. Each is one atomic counter behind a door that already existed,
because "the launch does not wait for the catalog" is a claim about the shape of
the code and a stopwatch would test the network instead.

## The liveness laws

The laws above are about work a machine does. These four are about work it is
WAITING FOR, and they were written after a headless run spent ninety minutes and
sixty-three cents producing nothing (`bench/deepswe/AUTOPSY.md`, happy-dom s1).
The shape was one hung provider call, three times over: fifteen minutes on a
socket, a twenty-minute claim reaper taking the node away, and a leaf that began
again from nothing beside a workspace holding its own work.

**Every bound here is derived from something the process already measured.** Not
one of them is a new figure, and that is the point: the detectors already existed
and the run met none of them.

| Law | The bound, and where it comes from | Where it is pinned |
| --- | --- | --- |
| **A completion is asked for as a STREAM whether or not anybody is watching it.** The observer decides who is TOLD; it never decided whether the call is guarded, and until this it silently did. | No bound of its own. It is what arms the three below on a headless call. | `internal/provider/unwatched_test.go` |
| **A request that has produced no token is cut.** | `firstDeltaBound`, 90s — past every healthy first token this adapter has measured, and under the streaming transport's `responseHeaderTimeout` so a stall is named rather than surfacing as a torn connection. | `internal/provider/streamguard_test.go` |
| **A stream that has gone quiet is cut**, with keepalives buying bounded patience and no more. | `midStreamGapBound`, 45s, half the first bound because a model that has started writing has finished deciding; `bufferedQuietBound`, 150s, which covers every buffered delivery measured on 2026-08-24. | `internal/provider/streamguard_test.go` |
| **A reply that never ends is cut at a wall derived from the LANE'S OWN history** — the longest reply that endpoint has actually finished for this process, times `streamWallFactor`, clamped to `streamWallFloor`…`streamWallCeiling`. | `internal/provider/velocity.go`'s `runs` ledger. A model-size table is a claim this process cannot check; a completed reply is a measurement. | `internal/provider/streamguard_test.go` |
| **An endpoint that STALLS is treated exactly like one that REFUSES**: its lane is struck, memoized for `ignoreCooldown`, and every request encoded afterwards routes around it. | `velocityLedger.pace`, per model, sourced from the endpoint the wire itself named. | `internal/provider/unwatched_test.go` |
| **A cut retries the CALL, never the leaf**, and says so on the stream a person is reading. | `cutBudget` — 2 attempts when the ledger routed around the endpoint, 1 when it could not. | `internal/exec/bare/loop.go` |

### The claim reaper is the backstop, not the detector

`internal/resident`'s `staleClaimAge` is the generalist's own deadline floor
plus `claimReaperPad` — fifteen minutes plus five — and it reads that floor from
`exec.SubharnessInfo.Deadline`, WHICH IS THE ONLY PLACE IN THE PROCESS THAT
SIZES A LEAF'S ROOM. It was a fifteen-minute constant of its own until
2026-08-29, one of five copies of the same arithmetic and seven of the watchdog
pad above it; a floor raised in one of them would have put this backstop below
the deadline it exists to sit above. `internal/exec`'s
`TestOnlyTheSubharnessTableSizesALeafsRoom` fails the build on a sixth copy.
It is **raised** by `Runner.RaiseStaleAge` to
`that leaf's own watchdog + claimReaperPad` whenever a surface grants a longer
deadline, because a leaf's deadline scales with its budget and a reaper firing
below a worker's own deadline is not a backstop, it is the thing that fires
first.

It is deliberately the slowest thing in this section. The detectors above act in
ninety seconds, at the layer where the failure is, with everything the leaf had
still in hand. By the time a claim is old enough to interest the reaper, all of
them have been tried and the claim is genuinely held by nobody — which is the
only case a reaper can be right about. On 2026-08-28 it was the FIRST thing to
react, which is why it looked like the problem: its only available reaction is
the bluntest one there is.

**And the window bounds SILENCE, not work.** `store.SilentClaims` measures it
from the node's last durable sign of life — a billed `usage` row, a `usage_turns`
row, a `transcript` flush — floored at the claim's own start, and never from
`started_at` alone. Measured from the start it asked how long the WORKER had
been alive, and one claim legitimately carries the executor's deadline plus the
retry a spent deadline earns: on 2026-08-29 that fired at 22m10s, 22m10s, 22m11s
and 22m06s at a leaf that was calling the model throughout (15m deadline + 2m
watchdog + 5m pad = 22m, plus one `runnerQuietCeiling` of dispatch latency).
Every release now carries its reason on the journal.

**And a call in flight counts, without a flush timer.** The three durable marks
are all written when something finishes, and the transcript's flush is batched at
`store.MaxTranscriptBatch` on purpose — so a leaf inside one long `go test`
writes nothing while it runs. `exec.Working(ctx)` opens a span around each tool
call and each model request; `resident.leafHold` counts the open spans and the
scheduler's sweep skips a claim whose worker is waiting on something. It costs
two atomics per call and not one write, it adds no constant, and a worker that
has marked nothing is judged on the journal exactly as before.

**And it cancels rather than confiscates.** `store.SilentClaims` only reads;
`resident.Runner.reapSilentClaims` decides. A claim this process is behind is
never taken back — its worker's context is cancelled and the node stays Running
until that worker's own landing releases it, which is the only release that
cannot overtake a live goroutine. `store.EventLeafStopped` carries the token and
is journaled immediately before that release, so the ordering is checkable in any
store. A worker that ignores its cancellation for `claimReaperPad` — the same
five minutes a landing leaf is already given, not a new figure — is gone, and the
claim is taken without it and said to be. A claim owned by a process that is no
longer here has no worker to stop and is released at once, which is what
`ReleaseOrphans` does at startup for the same reason.

### And a reaped node resumes

A node the reaper returns to the queue is claimed again with its attempt counter
raised, and `cmd/aforge`'s `leafBank` hands the new attempt what the old one
left: its partial, its progress lines, the files the workspace was SEEN to change
under this leaf's key, and — this is the part that was missing — **its own
recorded turns**, read back from the store through `resident.BankedRun`.

`BankedRun` reads ONE run and not the whole table. A node's transcript is every
attempt ever made under it, appended, each numbering its turns from one, so a
turn-number cutoff read across all of them is a window on nothing; the run
boundary is where the counter goes backwards. What comes back is two blocks:

| block | bound | why |
| --- | --- | --- |
| the **outline** — every turn of the run, one line each: what it ran and what it said | `BankedTranscriptBytes`, clipped from the MIDDLE | an outline that keeps only its end has thrown away what the attempt set out to do |
| the **tail** — the end of the run verbatim | `BankedTranscriptTurns` (12) and `BankedTranscriptBytes`, trimmed from the FRONT | a continuation needs where the work got to |

`BankedTranscriptBytes` is `8 × store.MaxTranscriptTextBytes` — this package's
own answer to "enough of one step to tell what happened", times a handful of
steps — and one outline line is bounded at `store.MaxTranscriptTextBytes / 16`,
which is about a sentence. Twelve turns alone was not enough and the shortfall is
measurable: the ink leaf of 2026-08-29 created `src/grid-layout.ts` on turn 25
and was interrupted on turn 45, so the file it had just written was outside the
window its successor was handed.

Whether a claim resumed is journaled (`store.EventLeafResumed`, with the turn
count and the files) rather than assumed, because this section asserted it for a
day while it was not true.

### A tool call's room, which is measured and never picked

`internal/exec/bare`'s belt had **no per-command bound at all**: pi's `bash`
schema says "no default timeout", so a command the model did not think to bound
inherited the leaf's whole envelope. The ink run of 2026-08-29 (s8) ended on
`npx ava test/grid.tsx` with fifteen minutes of leaf to spend, the command was
SIGKILLed when the leaf's context expired, and three separate things went wrong
in the same instant: the cut was tested for `context.Canceled` and a deadline is
`DeadlineExceeded`, so a killed command reported a clean success with truncated
output; the loop's next turn-boundary check found a dead context and stopped, so
the output never reached the model that had asked for it; and the node watchdog
two minutes above was already counting, so a leaf that had been working for
seventeen minutes was recorded as one that never came back.

**There is no per-command timeout constant, and adding one back is a
regression.** A figure picked here is wrong on every machine it was not picked
on — these leaves run in amd64 containers under qemu where everything is five to
ten times slower than the wall-derived arithmetic assumes — and "does this
command fit a number" is not the question anyway. The question is whether the
leaf can afford it and still land.

| what | the bound, and where it comes from |
| --- | --- |
| one tool batch | `time.Until(deadline) − pace.reserve()`, in `loopState.toolRoom` |
| **the landing reserve** | `pace.reserve()` = the slowest model call THIS leaf has completed **plus** the slowest transcript flush THIS leaf has taken |
| the landing itself | one call with no tools on the wire, which is the same shape the reserve was measured as |

Both halves are read rather than chosen. Landing is exactly those two things
happening once more — one call in which the leaf says where it got to, one write
that puts it on disk — so a leaf on a slow machine measures a slow machine and
reserves accordingly, with nothing to configure. The reserve holds the WORST
observation of each rather than a mean, because the honest answer to "will there
be enough time" is the worst this leaf has seen; a mean would under-reserve
exactly on the run where the machine is getting slower. It is zero until the
first call and the first flush have happened, which is correct: a tool call can
only be asked for by a model that has already answered once.

A cut command returns `cut after 9m12s; output so far: …` with everything the
accumulator had — the output is streamed as the command writes it
(`newOutputAccumulator`, `StreamingShell`), so a runner cut at nine minutes still
names every check it reached. The duration leads because it is the fact the model
acts on: "this command is too big for this leaf on this machine" is an
instruction to scope it, where "aborted" invites the same command again. The
process GROUP is killed, so a cut leaves no runaway child.

Pinned by `internal/exec/bare/room_test.go`.

### A node watchdog that reads evidence of life

`cmd/aforge`'s `runLeafWithWatchdog` fired on a flat `deadline + 2m` timer and,
when it fired, **returned without cancelling the leaf** — so the goroutine went
on spending, went on writing to the workspace, and kept whatever children its
last command had started, for as long as its own deadline had left.

It now asks the same question the claim reaper asks, one level down. The reaper
asks whether anybody is working the node; this asks whether THIS worker still is,
and it reads the same evidence from the same seam: `exec.Working` spans, composed
onto the context with `exec.AlsoWithLiveness` so the reaper's own listener is not
displaced. **The window bounds silence.** A worker inside a call, or one that
closed a span inside the window, re-arms it over the silence that is actually
left; a worker that has marked nothing is judged exactly as before.

When it does give up, the worker is STOPPED rather than left running — the same
law the reaper obeys — and then given the longest span it was ever observed
inside to land, which is measured rather than chosen and is zero for a worker
that never marked anything. What comes back is then a leaf that landed after
being stopped, with its spend banked and its record flushed, instead of a nil
outcome and a sentence about abandonment.

### Money is banked per call, not per landing

A leaf's spend used to reach the journal exactly once, from
`exec.Outcome.Usage`, on the way out of the run (`resident.recordSpend`) — so
every ending that returns no outcome returned no money either. Ink s8 made 109
billed calls in seventeen minutes and left a `usage` table holding one row, the
planner's; `cost.json` read $0.000228 over a 26 KB patch. The transcript had been
hardened against exactly those endings a wave earlier, with a flush on each side
of the abandonment; the money had no equivalent.

`provider.WithBilling` reports each decoded response at the adapter's own door —
the one seam every outbound call in this process passes through, and the same
place the call log's per-call row is already written. `cmd/aforge`'s `leafBanker`
writes one `usage` row per call under the leaf's node as the answers arrive.
**This adds one durable write per MODEL CALL and not per tool result**, which is
the line `internal/exec/liveness.go` draws and stays the right side of: a leaf's
calls are tens, its tool results are hundreds.

The summed row is not written twice. What `resident.recordSpend` journals is the
REMAINDER — the leaf's total less what the banker already put on disk — so a
fully banked leaf writes nothing and a leaf that escalated to a worker driving
another process, whose calls this adapter never sees, still journals that
worker's spend. It is clamped at zero per field, because two totals summed from
the same responses in different places must never disagree into a negative row.
`usage_turns` is written either way, because one row per turn is a different kind
of record and nothing else carries it. `cost.json` and the settlement's money line both read
the `usage` table, so both now see an interrupted leaf's spend. Pinned by
`cmd/aforge/leafbank_test.go`.
