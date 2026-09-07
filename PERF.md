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
platform difference crossed the former cap. The budget was 53,954,000, two
percent above the larger measured binary; the exact before-and-after source cost
is not disguised as the whole reset.

It was reset a third time on 2026-08-31, when furrow moved inside the binary
(`internal/furrowbin`, the places wave's P0). This one is a decision and not a
drift: the owner's ruling was **no variance — every aforge is an aforge with
furrow, and if the only blocker is size, the limit rises**, so the four
workspace verbs stop being a capability some machines have. The measurement on
darwin/arm64, Go 1.26.5:

| | bytes |
| --- | --- |
| the branch without the embed | 55,143,970 |
| the same tree with furrow embedded | 58,347,314 |
| what the embed itself cost | 3,203,344 |

Two figures, deliberately, because they are two different bills. **3,203,344 is
this change** — the gzipped furrow-darwin-arm64 artifact at 3,185,397 bytes,
plus the package that carries it out to disk. The other 1,189,970 is accumulated
branch growth that had already crossed the old 53,954,000 cap before this lane
touched anything, and it is named here rather than folded quietly into a bigger
number. The budget is 59,515,000, two percent above the larger measured binary,
which is the headroom every figure in this section was given.

Two things that do NOT grow with it. The artifact is fetched at build time
against the sha256 in `internal/furrowbin/pin.json` and never committed, so a
clone stays the size it was. And only the platform being built for is staged, so
the binary carries one furrow rather than four.

It was reset a fourth time on 2026-09-01, and this is the first reset that moves
the number DOWN. #227 left one worker: the imported engine and the second leaf
engine beside it are gone, and the dependencies they alone pulled in went with
them. A removal nobody weighs is a removal that quietly leaves the budget where
it was — the ratchet is only worth what it measures, so the figure follows the
code in both directions. The measurement:

| | bytes |
| --- | --- |
| the branch before #227 | 55,574,793 |
| the same tree with one worker | 48,627,977 |
| what the removal gave back | 6,946,816 |

That pair is linux/arm64, Go 1.26.5, the machine the removal was built on.
The budget is set on the LARGEST platform, not the one at hand, because the
ratchet has to hold wherever `make size` runs — so the same tree was built for
all four, each with its own furrow artifact staged:

| platform | bytes |
| --- | --- |
| linux/arm64 | 48,627,977 |
| darwin/arm64 | 49,539,282 |
| linux/amd64 | 52,707,490 |
| darwin/amd64 | 53,530,544 |

The budget is 54,600,000, two percent above darwin/amd64, the same headroom
every figure in this section was given, now over a smaller binary.

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

## The workspace snapshot's budget

What a run left behind is answered by reading the tree, not by asking the clock.
`Workspace.Snapshot` (`internal/exec/workspace.go`) photographs every path a
deliverable could be and `diffTrees` compares two photographs; that ONE pair
answers both spans — the whole leaf (`WatchTree` at the top, `RecordChanges` at
landing) and the single tool call (`RecordProducedSince`, `internal/exec/produced.go`).
It replaces a mtime-versus-a-wall-clock-mark test that misfiled three ordinary
cases: a write landing inside the filesystem's own timestamp granularity, a tool
that preserves the timestamp it copied (`cp -p`, `git checkout`, `tar`), and a
rewrite whose bytes are identical. Content and mode decide; the clock is a cache
key and never an answer.

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
way it would be for a walk sized by somebody's whole checkout.

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
after — is the most expensive thing on a leaf's path. A baseline `go test` on a
repository this size was measured at seven minutes, and it was invisible enough
in the headless stream that an operator read it as a hang and killed the run. So this measurement is bounded three ways, and the bound is
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
| `verify.ReplacementsNamed` | **4** | `internal/verify/subject.go` |

`verify.ReplacementsNamed` bounds the heirs one replacement record names — a row
is read to learn what SHAPE a replacement had, one stub into one check or one
stub into nine, and four settles that as well as forty. The subtraction it comes
out of costs one pass over each roster and one map of them, which is what the
subtraction already cost.

The two share constants live in `internal/verify` (`verify.ReadingBudget`),
where every reader can reach them. Two things read them — the belt that
photographs before the work (`internal/exec/photograph.go`), and the delivery
gate, which takes the reading of the tree it is about to judge when nobody else
did — and two copies of one cap is how a number in this repository drifts.

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
the job's own account of what it has changed is what decides it. So the
quarter-of-the-wall worst case is paid only by a long leaf, in a project that
says how it is checked, in a job that actually wrote something.

**A reading is retaken only when the tree changed, and never over the scope that
was just killed.** The baseline is remembered against the state of the tree it is
a reading of — `verify.TreeState`, a digest of the job's own artifact record at
the moment it was taken — and a leaf or a gate standing in a tree that carries the
same state inherits the reading rather than running the suite again over identical
bytes (`verify.TreeUnchangedSince`, `Reading.OnAnUnchangedTree`). What used to
stand in for that question was "did this leaf inherit a baseline", which every
leaf after the first does whatever it touched, so every one of them bought a whole
second reading. `Reading.Retakeable` carries the other half: a reading killed at
its budget is taken again only where the measured pace affords a **strictly
smaller** selection, because a second identical attempt cannot finish where the
first did not. A whole-suite reading has no narrower scope to fall to and stands
as it is. The measured cost of neither rule existing (#429): nine readings of `go
test -json ./...` over 4,587 tests on one errand, each killed at its two-minute
budget, 82% of an 11m40s wall, for a request that named one package.

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
the wall, and none of them move between rounds. Measured: textual s6's leaf
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

**The run's own checks are always in scope, and they lead the selection.** A
scope is decided before the work exists, so it can hold no test the work goes on
to WRITE. igel s8 scoped all four of its readings to `touched (1 file)` — the
one source file the request named — and every one of them named the same **2**
checks, while the graded patch had put about **40** into a new file under
`tests/`. Nothing ever read one of them: the coverage mapping is handed the
ROSTER of a reading, so a checklist point whose only exercise is a check that
round wrote stays unexercised however good the check is, and the before/after
cannot move. So the after reading's file set is the structural adjacency UNION
every check file in the record of what the run left behind — `verify.OwnChecks`
over `Outcome.Artifacts`/`Evidence.Artifacts`, filtered by the runner's own
test-file convention and by `os.Stat`, joined on by `Strategy.WithOwnChecks` at
both seams that take a second reading (`exec.PhotographAfter` in
`internal/exec/photograph.go`, and `revision.measureFinalTree`). It is read from the WORLD's record of the tree and
never from the worker's account of what it tested, which is the claim this whole
gate exists not to weigh. It costs no extra reading: the same command, a longer
file list. Two invariants make it safe. The comparison is by COVERING rather
than matching — `Strategy.covers` reads a superset of the before selection, over
the same base command in the same workdir, as comparable — and `Reading.Regressed`
weighs a new failure against `Strategy.Widened`, the paths the widening joined
on, so a check that did not exist before cannot be convicted as a regression: a
failure that names one of those files, or that the before roster kept and never
named, is not one. A reading that was NOT widened subtracts its two failing
lists exactly as it always did — a runner that prints its failures and nothing
else says nothing about its passes, and reading that silence as "no such check"
would excuse every regression in every such project. And the run's own checks lead the selection, ahead of
both adjacency ranks, so neither the eighth-of-suite cut nor a pace-driven
retake can take them: igel s8's new file sorts after every check the repository
already had, and a selection ordered by name alone hands exactly the run's own
work to the cut.

**The gate takes the job's reading when nobody else did.** The photograph used
to sit beside one belt, where only that belt could reach it, and textual s7 ran
every node under the generalist and reached its gates with no reading in the
store at all. It is `internal/exec/photograph.go` now — every belt takes it —
but a gate can still arrive at a job nobody photographed: an old store, a
workspace the gate does not hold.
`revision.jobReading` reads the job's remembered baseline first (free), and takes
one itself on `ReadingBudget(time until the gate's own deadline)` only when
nothing anywhere has looked, remembering it against the job so it costs one
reading per job rather than one per round.

**And whatever the gate does about a reading, it journals.** The photograph used
to be journaled by exactly one belt, so the record of a run's own verification
was a fact about WHICH BELT A NODE HAPPENED TO RUN ON rather than about the
project. igel s9 and ink s9 put every node on the generalist and
finished with **zero** `verification` events between them: not a reading, not a
refusal, not a row, while the gate had in fact taken a reading of the tree it was
judging. ofetch s9, on the identical binary, journaled **four**, because one of
its nodes happened to run on the belt that had the reading. `revision.journalGateReading` writes the
same event the belt writes, with `when: on the tree the gate is judging`, on
every path: the reading it took, the job baseline it inherited, and each refusal
— no workspace, and no deadline to size a budget against. It costs one row and no
reading: the reading was already being taken or already being declined.
`CheckEvidence` was split so the settlement reads ONE photograph and writes ONE
row rather than asking the memoised reader twice.

**And the reading is taken on every gate, checklist or no checklist.** It used to
sit below the checklist, so a job that stated none returned before the reading —
`Judgment.Unreadable` was unreachable on that path and a delivery settled
`Whole()` over a verification nothing had looked at, exit 0. The two are
different questions: a checklist governs whether COVERAGE can be settled, and
whether the WORLD was read is a fact about the project and the run that is true
or false whether or not anyone wrote a checklist. `revision.settleUnmeasured` is
that half, split out and asked of every verdict. **The cost is one reading per
JOB on jobs that state no checklist and whose leaves never photographed** — not
per round: `jobReading` remembers against `verify.JobKey`, so later rounds
inherit it, and it is bounded by the same `ReadingBudget` share of the gate's own
remaining wall as every other reading here.

**A regression is a check named green at the baseline, and the run's own red
checks are a different finding.** Subtracting the two failing lists is exact only
while both readings run the same set of checks, and a run writes checks:
happy-dom's nemotron n1 run rewrote the one file its reading was scoped to, from
**4** checks to **33**, and the gate failed the delivery for breaking eighteen it
had written that hour — on a tree the grader scored 9/9. The baseline's roster is
the authority now (`Reading.Regressed`) — a roster that names at least one GREEN
check, because a runner that prints only its failures has a reported list that IS
its failure list and can tell nothing apart; there the old subtraction stands.
What such a roster never named is `Reading.OwnFailing` → `revision.OwnChecksFailing`, Sourced, buying the same
round, with `DeliveryGate.OwnFailing` and a stream line of its own. It costs
nothing: the same two rosters, read once. The old `Strategy.Widened` heuristic
this replaces is gone.

**A suite that failed to collect is not a suite that went red.** A runner told to
print a machine-readable report prints one whenever it ran its tests at all, so
its absence is a fact rather than an empty roster: `Result.Uncollected` records
it, `Result.Error` quotes what the runner said instead, `Reading.comparable`
refuses the subtraction from either side, and the second reading is reported as
*its suite failed to collect* with the runner's own words. It costs nothing — the
same output, read once — and it closes FAILSAFE's tenth failure, where a check
named `to`, scraped out of *Failed to load url ./circuit-breaker*, was subtracted
against a baseline of 28.

**A cut second reading keeps what it named, and a derived rung retakes on the
baseline's.** The before half has kept a partial roster since ink s7; the after
half discarded it, so ink's second reading came back `read: false, named: 0` on
EVERY leaf of two whole sweeps (s10 and s11) while the baseline of the same run,
on the same `npx ava --tap` at the same workdir, named **44** checks — 156 on one
node. Nothing was wrong with the reader, the scope or the room: the runner
streamed its checks in TAP, the ceiling fired, and the after path threw away what
it had already read. It is kept now, marked `Partial` so the subtraction is
refused (`Reading.comparable` already reads it that way) while the roster the
coverage settlement spends survives. Two further corrections came with it: the
failure path journals the result the runner actually left — its exit status and
whatever it printed — rather than a zero value, which is how a command that ran
and exited 1 was written down as `exit: 0`; and `exec.readFinishedTree` retakes
on the BASELINE'S OWN RUNG before reporting a tree unreadable, because the two
ways an after reading may differ from a before one (widened by the run's own
work, aimed at the diff) both choose a command the baseline never proved could
run, and that is the one thing here a retake fixes. One retake, only when the
command actually differs, and whichever attempt said more is the one kept.

**A name resolves to every file that carries it, and a compound the request also
spells short names two things.** textual's nemotron n1 run took **14** readings
and every one the request asked for was
`pytest tests/test_concurrency.py tests/test_textlog.py` — 3 checks — while the
change touched `widgets/_log.py` and `widgets/_rich_log.py`. Two structural
faults, neither about vocabulary. `Locate` kept the FIRST file the walk tripped
over, so a repository that documents itself shadows its own source:
`docs/examples/widgets/rich_log.py` sorts before
`src/textual/widgets/_rich_log.py`, and every reading was aimed at the
documentation copy of the widget being changed. It keeps all of them now, capped
at `locateLimit` **4** per name — which of two files of one name is the
implementation is not a question this can answer by looking at either, so it
answers none of it and lets `Adjacent`'s ranking decide. And a single capitalised
word is not a subject on sight, so `Log` was dropped and `_log.py` was never
located at all; `standaloneSegments` admits a segment of an already-accepted
compound when the SAME TEXT also uses it as a whole word on its own, which is a
fact about the request rather than a judgement about the word. `locateKey`'s floor
moves from 4 to 3 for that caller alone. The selection stays bounded by the
eighth-of-suite cap it always was.

**The second reading is aimed at the change as well as at the request.** The
first reading's scope has to come from the request — there is no diff yet — and
textual s10 is what that costs on its own: the request said *Log and RichLog*,
`RichLog` resolved to `_rich_log.py`, `Log` resolved to nothing, and both readings
ran `tests/test_concurrency.py tests/test_textlog.py` while `tests/test_log.py`
sat unread beside a file the change had touched. `verify.ChangedSources` reads the
non-check half of the artifact record and `Strategy.WithChangedWork` puts it back
through `Adjacent`, so the second reading's selection is the first's ∪ the diff's
structural adjacency ∪ the run's own checks. Same command, longer file list, no
extra reading. And where the request resolved to nothing — ofetch s10 took six
readings, all `whole` — a whole rung killed at its ceiling has proved the suite
is bigger than the wall, so `verify.ChangedWorkStrategy` aims the second reading
at the diff rather than spending another eighth on the same ceiling. That pair is
a subset rather than a superset, so `covers` refuses it and `Regressed` answers
nothing; what it buys is the roster.

**What a person would see if this were wrong.** Too generous, and short leaves
stop doing work — a `do` run whose nodes each sit for minutes with nothing in
the stream but the suite they are running, which is exactly the failure that got
a run killed by hand once. Too mean, and `Outcome.Regressed` is nil on every
leaf that should have carried a name, which reads downstream as *no claim* and
lets a patch that deleted an attribute the repository already had ship as whole
— the measured failure in `docs/design/gate/SETTLEMENT.md` §4. Neither is a test
going red; both are read off a run, which is why the numbers are written down
here.

## What the change set's own text costs

The leaf's account of what it changed carries a PATCH — the change's own text,
written to a file under the harness's own directory and handed on as a path
(`Account.Patch`, filled by `AccountFor` in `internal/exec/accountfor.go`). It is
what nothing downstream ever had: a judge asked whether a deliverable's account
of the change is true can read the change, and a method writer handed the goal of
describing it can read it instead of inferring it.

It is derived on the landing seam, from git, on the leaf's own context: `git diff`
over the committed range, `git diff HEAD` for what nobody committed, and one
`git diff --no-index` per untracked file the leaf created. Every one of them runs
with **`--no-ext-diff`**, which is a law and not a flag: a repository may
configure an external diff driver per-repository or per-path, and a helper this
program never named is not a helper that cancelling a context reliably reaps — a
measurement taken from a defer on the landing path must not be able to outlive the
leaf.

| budget | value | what it bounds |
| --- | --- | --- |
| `accountPatchBytes` | **1 MiB** | the whole patch file. Past it the text is cut at a line boundary and the file SAYS where it was cut — a clipped patch that reads whole is a false account of the change set rather than a smaller one. |
| `accountPathspecBytes` | **96 KiB** | the paths one `git diff` is handed at once. `git diff` reads no pathspec from a file the way `git add` does (git 2.43), so a change set large enough to pass the kernel's argv limit is sent in runs; they concatenate to exactly the patch one invocation would have written, because the paths are disjoint. |

The paths ARE the bound that matters, and they are the account's own: the diffs
are read over the shared tree, where a sibling leaf that landed in between is in
the same history, and a patch that swept that up would hand every reader another
node's work as this node's.

**A source that could not be read abandons the patch entirely.** git's exit 0 and
1 are answers — a diff exits 1 having found something — and anything from 2 up is
git refusing the command; a refusal read as an empty diff is how an oversized
argv came back as "this leaf changed nothing". A patch missing its untracked half
is not a smaller patch, it is one that is silent about the files the leaf
created, so the account claims none. `TestAPatchTooLargeToWriteOutSaysWhereItWasCut`
pins the clip and its sentence.

## What the symbol-level photograph costs

The check-level reading answers *what does this project's suite say*. It cannot
answer *what did this work delete*, because a project only owns checks for what
somebody wrote checks for. igel s11 removed eight public class attributes off
`Igel` and its own reading of the finished tree came back BETTER — named **2 →
14**, red **2 → 0** — while all **24** hidden tests failed at setup on
`Igel.results_path`.

`verify.PublicSurface` reads the tree's public names once per photograph, beside
the check-level reading and on every path that reading can refuse on — a project
that declares no verification, a wall too short for a suite, a suite killed at
its ceiling all still get this half. `verify.SurfaceOf` re-reads only the files
the run's own record says it changed.

| number | value | what it bounds |
| --- | --- | --- |
| `surfaceFileLimit` | 2000 | source files one baseline reads; textual is ~700 python files, ofetch ~90 typescript ones |
| `surfaceReadBudget` | 8MB | bytes one baseline reads — four times the scope walk's, spent once per JOB rather than once per reading |
| `surfaceFileBytes` | 512KB | one file, so a generated module cannot spend the budget alone |
| `surfaceNamesReported` | 8 | names one finding spells out; `store.VerificationSample`'s sibling |

Past either bound the walk stops and the surface is PARTIAL, which degrades in
the safe direction: a file with no baseline entry can never be reported as having
lost a name. No model call and no second suite run — it is two file reads and a
set difference.

**What the readers deliberately do not read.** Go goes through the standard
library's own parser, and a file that does not parse contributes NOTHING rather
than a partial reading — a syntax error mid-edit would otherwise read as half a
package disappearing. Python, TypeScript/JavaScript and Rust go through
line-and-indent readers that only ever read a DECLARATION AT THE START OF A LINE,
which is the one thing each language's own formatter guarantees. A name assigned
inside a conditional, a class built by a decorator, an export re-exported through
a barrel file, a symbol behind a macro: none of those is read, on purpose. The
cost of a name invented here is a false blocker on real work; the cost of a name
missed is the silence this was written in.

**What the readers deliberately do not read, one correction.** A module-level
binding — python's `configs = {...}`, the singleton or table a package hands out
— IS a name the module publishes, by the same underscore rule everything else in
that reader uses, and it is read now. It was the one shape python's reader missed
while Go reported an exported `var`, TypeScript an exported `const` and Rust a
`pub static`, and it is the shape igel s12 moved (FAILSAFE.md's twentieth
chapter). It costs nothing extra: the same walk, the same line, one more regex
that was already compiled.

## What the changed-definition reading costs

The presence photograph answers *is the name still there*. It cannot answer *does
the thing behind the name still work the way it is used*, and igel s12 is the
run that turned on the difference: `configs` was rebound from a dict to an
instance of a class the run wrote, `surface` read `compared: 8, lost: 0`, and all
**24** hidden tests failed on `'Configs' object does not support item
assignment`.

`verify.ChangedDefinitions` is a subtraction of the two photographs the job
already takes — a name in both readings whose declaration DIGEST differs — and
costs nothing beyond the digest itself, which is an FNV hash over the
declaration's own non-blank lines on the same walk that decides the name is
public. It needs no diff, which is what makes it exist at all: nothing on the
belt a headless run carries records one.
`verify.Consumers` then walks the project ONCE for every name at the same time,
because the walk is what this costs and a settlement weighing eight definitions
must not read the tree eight times.

| number | value | what it bounds |
| --- | --- | --- |
| `consumerScanLimit` | 6000 | directory entries one walk visits; `scopeScanLimit`'s sibling |
| `consumerReadBudget` | 2MB | bytes one settlement reads looking for sites; `scopeReadBudget`'s figure |
| `consumerSites` | 200 | sites one name keeps |
| `consumerNamesWeighed` | 8 | changed definitions one settlement carries; `surfaceNamesReported`'s sibling |
| `revision.consumerSamples` | 3 | sites one shape spells out for a reader |
| `revision.consumerQuotes` | 24 | consumer lines that travel as the verdict schema's enum |
| `revision.gateConsumersShare` | **6 of `gateShareTotal`**, 3 KiB unknown | the block's room in the judge's prompt |

Per file it reuses `surfaceFileBytes` **512KB**, and it skips every file the run
CHANGED — those lines are the work, not a consumer of it. No model call, no
second suite run, no toolchain: two readings the job already has, one walk, and
`strings.Index` with an identifier-boundary test.

## What the unbound-reference reading costs

One question further back than either of those: a name the run's own sources READ
that no file in the tree binds. It needs no baseline and no suite — one reading
of the finished tree settles it — which makes it the only measurement a project
with no verification at all still gets. igel s14 imported
`temp_post_req_data_path` from a module that had just stopped binding it, all 24
hidden tests failed on `ImportError`, and the gate could say only that a suite
was red (FAILSAFE.md's twenty-fifth chapter).

`verify.UnboundReferences` walks the tree ONCE for bindings — every attribute,
method, class, field, slot and module name the repository spells, with no privacy
rule applied, because a leading underscore says a name is internal and never that
it is absent — and then reads only the run's own changed sources for references.
Import targets are read on demand and memoised.

| number | value | what it bounds |
| --- | --- | --- |
| `unboundScanLimit` | 2000 | source files the binding index reads; `surfaceFileLimit`'s sibling |
| `unboundReadBudget` | 8MB | bytes that walk reads; `surfaceReadBudget`'s figure, once per settlement |
| `unboundModuleBudget` | 1MB | bytes resolving imports reads on top of it |
| `unboundSitesKept` | 24 | unbound references one settlement carries |
| `unboundNamesReported` | 8 | references one finding spells out; `surfaceNamesReported`'s sibling |

Per file it reuses `surfaceFileBytes` **512KB**. Past either walk bound the index
is PARTIAL, and a partial index **silences every attribute check** rather than
narrowing it — a binding the walk never reached is a binding that reads as
absent, which is the one direction this reading may not be wrong in. Measured on
the bench trees: textual's 988 python files in **0.8s**, happy-dom's 615 readable
sources in **0.2s**, igel's 44 in **0.03s**. No model call, no second suite run,
no toolchain, and no type checker in any of it.

**The baseline surface now carries a digest per declaration.** A public name is a
string plus eight bytes, so a repository with twenty thousand of them costs about
160KB per remembered baseline, and `rememberedTrees` **16** bounds how many are
held at once — kilobytes, once per process.

Past any bound the reading is PARTIAL, which degrades in the safe direction every
time: fewer sites can only UNDERSTATE a count, never invent one, and a definition
with no consumer found is not carried at all. **The block has a share of its
own**, never a slice of the tree block's, because the tree block IS the
deliverable — a consumer list must never be the reason a changed file went
unprinted — and inside that share a definition's sites are printed whole or the
definition appears with its counts and no sites, which is the tree block's
whole-or-named rule read one level down.

## What grounding the acceptance mapping costs

`revision.GroundMapping` spends no model call. It reads the file each mapped
check names, once, lowercased and cached, bounded twice: `mappingBodyBytes`
**512KB** for one file so a generated fixture cannot spend the whole allowance,
and `mappingBodyBudget` **2MB** for one settlement — `verify`'s `scopeReadBudget`
at the same size and for the same reason. It degrades in the safe direction: a
body it could not read grounds nothing, so the point stays unexercised.

## What weighing the acceptance mapping's assertions costs

`revision.WeighAssertions` spends no model call and reads no file the names door
above has not already paid for: it shares that door's `checkBodies`, so
`mappingBodyBytes` **512KB** per file and `mappingBodyBudget` **2MB** per
settlement bound both readers together. Each file is parsed for its assertions
once per settlement and cached, and one check's captured assertion text is bound
by `verify.assertionTextBytes` **16KB** — past it the reader stops appending and
the check keeps what it has, which can only leave a point OPEN and never close
one. `revision.observablesNamed` **4** bounds the observables one finding line
names.

The tree's vocabulary (`verify.IndexSurface`) is built from the surface the
JOB'S BASELINE already holds — `verify.BaselineFor`, one walk per job under
`surfaceReadBudget` — so no gate re-walks the tree, and a job with no baseline
gets an empty index and behaves as it did before this existed. Matching is
bounded by `verify.spokenWindow` **4**: a run of more than four of a sentence's
words is a clause, not a name.

## What the acceptance checklist costs

The gate's acceptance settlement (`docs/design/gate/ACCEPTANCE.md`) adds no suite
run to the common path and one bounded model call to two seams.

| number | value | where |
| --- | --- | --- |
| the checklist's length | **one point per clause of the request** | `plan.NormalizeAcceptance` |
| the acceptance finding's size | **one entry per REQUEST LINE, 8 named then a count** | `revision.groupUnexercised`, `revision.groupUnasserted` |
| observables one finding line names | **4** | `revision.observablesNamed` |
| one check's captured assertion text | **16KB** | `verify.assertionTextBytes` |
| words of a sentence read as one name | **4** | `verify.spokenWindow` |
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

**And three questions are now never asked, which is where the cost actually
fell.** The mapping call is not bought at all for a checklist of ACTIONS
(`plan.Behaviours`), for a run that changed no code (`revision.codeChanged`), or
against a roster that could not be read (`revision.rosterSpeaks`). The errand
that measured this spent two mapping calls carrying ~127k prompt tokens each —
$0.022 — on a request whose whole instruction was to change no files, and then
bought the rounds that ran it into a 700-second wall. See
docs/design/gate/ACCEPTANCE.md, "What #428 proved".

## What the request-met question costs

**One model call, at the moment a round would otherwise be bought, and nowhere
else.**

| number | value | where |
| --- | --- | --- |
| when it is asked | **once per gate that is about to buy a repair or a remainder** | `cmd/aforge/chat.go requestSettled`, `revision.ExtendForGap` |
| how many times per gate | **one** | `revision.Judgment.RequestAsked` |
| the deliverable it carries | **24KB** | `revision.requestMetDeliverableBytes` |
| the record it carries | **the gate's own evidence block** | `revision.Evidence.block` |

THE BOUND IS THE ROUND IT REPLACES. A repair round is a whole leaf — a worker,
its tools, its wall — and this is one structured call against a prompt whose
static half and whose request half are fixed for the job's lifetime, so two
rounds of one job share the entire prefix. It is asked at no other seam: not per
turn, not per node, and never on a gate that passed. A run that would have bought
no round pays nothing, and a run that would have bought four pays for at most as
many as it reached. `Judgment.RequestAsked` is what stops the two doors paying
twice for one answer on the way to one conclusion.

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

## What the gate reads of the tree it judges

The delivery gate's fence holds the CHANGE where a run made one — the changed
sources and checks, with what is in them — and the worker's message where it did
not (`internal/revision/subject.go`, FAILSAFE.md's seventeenth chapter). That
block is the deliverable, so it is bounded like every other block in that prompt:
a share of the judge's own room, with a literal for a build whose catalog cannot
place the model.

| number | value | where |
| --- | --- | --- |
| the change's share of a known window | **25 of `gateShareTotal`** | `gateTreeShare` |
| and the bound when the window is unknown | **8 KiB** | `gateTreeBytes` |
| the record that travels as a schema enum | at most **64 paths** | `treeEnumFiles` |
| the stated behaviours a refusal may quote | **6 of `gateShareTotal`**, 4 KiB unknown | `gateAcceptShare`, `gateAcceptBytes` |
| the consumer lines a refusal may quote | at most **24**, from what was shown | `consumerQuotes` |
| the files a refusal may name | the record, the consumers, and everything the plan or request PROMISED | `Evidence.PromisedFiles` |

**NO FILE IS EVER PRINTED IN PART.** A file's contents appear entire or the file
appears by name and size only, and the block says which. This is the law, not the
budget: two graded runs refused a correct delivery for what the READING did —
*"the fenced text shows only a truncated excerpt ending mid-sentence"* (ofetch
v4-flash s12, 4/47) and *"The file is truncated — it cuts off before the
implementation of write(expand=True)"* (textual v4-flash s12, 15/20). Annotating
the excerpt was tried first and is not enough; a model reading source that stops
mid-function concludes the source stops mid-function. The percept is removed
rather than argued with. The diff still travels underneath (`Evidence.Patch`,
12%), which is where a change too large for this block is read.

**Files are taken in the record's own order, never weighted.** Which of six
changed files carries the behaviour a request asked for is precisely the question
the judge is being paid to answer; a record that decided it in advance would be
answering it with an arithmetic nobody could see.

**No file is read for its meaning and no file is read twice.** A path that is not
text — a compiled model, an image — stays a name and a size; the read is one
`ReadFile` of a file already known to fit; and the artifact list the record block
used to print is dropped where the fence carries it, so the prompt got one block
wider and not two.

**Both enums are guards, never the decision.** Past 64 paths the `file` field
keeps its meaning and the Go-side check against the same record admits the
verdict. The behaviour list is all-or-nothing — a checklist too long for its
share sends none, and sending none turns the quote requirement off — because part
of a checklist would refuse every refusal built on the behaviours that fell off
it, which is the same defect as a list nobody was shown.

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

What actually stops a job is what the growth journal measured: two rounds in a
row that moved nothing the job is about (standstill, weighed at the JOB), or the
same remainder handed to one lineage twice (fixed point). The spent-citation
ledger now defers to the same evidence — words whose round positively moved the
tree may buy another, and **everything unknown stays spent**, which keeps the
bound's direction wherever the journal is missing.

**What "moved" means costs one workspace walk per growth decision**, and it is
`verify`'s own, not a second one: `verify.Locate` resolves what the request names
against the tree, bounded at `scopeScanLimit` (**6000** entries) like every other
walk in that package, and `verify.OwnChecks` / `verify.ChangedSources` are two
passes over the round's own artifact list. The paths the request SPELLS join that
focus for **one `os.Stat` each** and no walk at all — a file the request names is
never scratch, and settling a name a person wrote out in full against the disk is
a single stat rather than a search. It runs a handful of times in a job's
life — once per growth ask, and the exact-ceiling recheck deliberately reuses the
first reading rather than taking a second, because a recheck that could answer
differently is not a recheck. See `internal/resident/progress.go`.

One clock enters, as a floor rather than a ceiling, in `outOfWall`
(`internal/revision/judge.go`): **a repair is bought only while the run's own
deadline still holds as long as the attempt that produced the finding took.** It
is derived from two things that already exist — the errand's deadline, which
`aforge do --timeout` sets, and the node's start, which the store stamps — so it
is not a knob and not a typed duration. A repair the wall will kill mid-flight
spends money to deliver nothing, and a run that stopped for want of time is
**partial** (exit 2), never whole. An unknown deadline or an untimed attempt
answers empty and buys the round: this makes runs longer, not shorter.

## The leaf's own closing, whose room is what it did not spend

A finding a leaf's own after-photograph raises against that leaf — a public name
it deleted, a name it reads that nothing binds, its own checks red, a check it
turned red — is put to the leaf before it lands (`internal/exec/selfclose.go`).
The alternative was the only one there was: land, gate, and a repair ROUND, which
is a cold leaf with a fresh brief and none of the context that made the mistake.
igel v4-flash s14 bought three of those and each deleted what the last relied on.

**There is no number here and there must never be one.** The room a close may
spend is what the leaf has left of the meter it was already granted, and nothing
else:

| meter | what is left | where the grant comes from |
| --- | --- | --- |
| turns | `turnCap - outcome.Turns` | `Linear.maxTurns`, clamped by `FoldTurns` for a fold |
| billed tokens | `maxTokens - spent(outcome)` | `Linear.maxTokens` |
| wall | `time.Until(deadline) - landingReserve` | the leaf's own lease, less the reserve it already keeps back to land in |

A meter a belt does not have is not a meter that ran out: a belt with no turn
cap and no token ceiling passes zero for both, and `exec.NoWall` when it has no
deadline, and a non-positive grant reads as *this belt does not bound that*. A
leaf whose figures are all fine but whose `Exhausted` is set — a deadline
reserve it entered — has no room either, because it was told to land.

**What a close costs is one extra reading of the finished tree**, on the same
budget as every other (`verify.ReadingBudget`, an eighth of the wall). It is
bounded by the FINDING KINDS a leaf can raise against itself — four — asked once
each, so a leaf pays at most one extra reading per kind it actually raised and a
leaf that photographs clean pays nothing at all. The second reading is the one
that lands, and `exec.PhotographAfter` clears the four fields it owns on entry so
a finding the first reading raised and the second does not stops being a finding.

**The governor sees a close as part of the leaf and never as a round.** It is
inside `Executor.Run`, so `MaxOverrunRounds`, the standstill reading and the
fixed-point reading above are all untouched, and `store.EventLeafSelfClose` is
inert — nothing reads it to decide anything.

## What a round of a job costs, measured on that job

A job still growing when its wall arrives is a job that never settles, so no gate
is cut and the person is handed a partial with nothing judged: ink s10 and
happy-dom s10 both ended that way, 5401 seconds each, `settled: false`, **zero
gate events between them**. So a job stops growing while there is still time to
finish what is running and be judged — and the bound is derived, never typed:

| term | value | where |
| --- | --- | --- |
| the round | the **median** interval between two admitted rounds of THIS job | `jobPace`, `internal/resident/grow.go` |
| the reading | the **longest** reading this job has been observed taking | `readingPace`, `store.VerificationReading.Elapsed` |
| the pace | the round plus the reading | `JobPace` |
| the clock | the run context's deadline, which is the errand's own `--timeout` | `chatBrain.wall`, `cmd/aforge/chat.go` |
| the rule | refuse when `time.Until(deadline) < pace`, and stop the job's queued work | `CauseOutOfWall`, `Runner.CloseOut` |
| the floor | the settlement watch forces a verdict at the same distance | `settlementWatch.forceJudgement`, `cmd/aforge/do.go` |

The median and no longer the longest. Rounds are long-tailed: one round of ofetch
v4-flash s13 took twenty minutes while the median of its five was under four, so
the longest taught the governor to refuse everything after the first outlier
rather than at the wall. What makes it safe to relax is that the estimate is no
longer the only thing holding — the settlement watch forces a verdict at this
same distance from the wall whether or not any rule here noticed
(`FAILSAFE.md`, the twenty-second chapter).

A round is not the whole of what another round costs: what follows it is a
reading of the tree and a judgement on it. The reading is the ONE THING A RUN
MEASURES ABOUT THE MACHINE IT IS ON — the benchmarks run amd64 containers under
qemu, where a suite that takes eight seconds natively takes forty-five, and a
reading killed at its ceiling journals how long it ran before that happened. So
the pace is the median round plus that reading, and one derivation serves both
readers: a run that stops growing at one estimate and is judged against another
is a run whose two clocks disagree about the same wall.

Fewer than two admitted rounds is a job that has not shown its pace, and it
answers zero, which refuses nothing and forces nothing — the same direction
`outOfWall` takes above, and for the same reason (`SETTLEMENT.md` §3).

**The clock reaches the machinery.** `aforge do` used to build its timeout
context for the settlement watcher alone while the brain ran on
`context.Background()`, so every deadline-reading rule in the program — this one
and `outOfWall` both — was told there was no limit. The errand's wall is now the
run context's deadline. It changes no timing on a run that finishes inside its
wall; what it changes is that a run approaching one can act on it.

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
| how many | the ask's own figure — `fanOutWidth` (**5**) for a fan-out, `sequenceDepth` (**4**) for the stage question a sequence is divided by, the node count for the per-node passes, **1** everywhere else | `Ask.Answers` |
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
if they ever become two. The stage question that divides a node no worker can
carry to an end is the same arrangement one constant along (`sequenceDepth`,
`internal/plan/sequence.go`), pinned by
`TestTheStagePromptAsksTheSizePromptsSecondQuestion`.

`internal/shaped/shaped_test.go` pins the derivation, the operator's reserve
never being outrun, and the three repairs.

## What a gated task inherits from the work before it

`TaskGraph.inheritedLocked` is the seam every gated node’s brief passes through:
its own contract, then the reports of the work it waited on. The reports used to
arrive whole, with no bound, so a sink gated on six long leaves could have its
own brief pushed off the window — and a 4 KiB pot once fed that sink four of
six sections and it wrote a confident four-row table.

| what | bound | the rule |
| --- | --- | --- |
| the task's own brief | never cut | the contract takes priority absolutely |
| the prerequisite reports | what remains of `taskShapeBriefLimit` (**6000**) after that brief and the heading | equal first shares; unused remainder goes to whoever is still clipped |
| one report's floor | `inheritedReportFloor` (**512**) | when N times the floor will not fit, the bound yields — the list gets thinner, never shorter |
| a cut | marked with `…` (`clip`) | one mark or none; a second fit on the division road (`familyOf`) moves a trailing mark rather than stacking one |
| the count | printed on the heading when any reports are present | `What the work before you learned — N reports:` — the same idiom as `openFindingsLimit` |

Pinned by `internal/session/task_inherit_test.go`. The number on
`taskShapeBriefLimit` is the brief bound every brief on this road is already
held to; this section is the first time the inherited half is held to it.

## What a landed task keeps of its own answer

The report is the card — three lines of 300 characters (`taskReportLines`,
`taskReportLineLimit`). What the work produced is kept beside it, and two
numbers bound it (`internal/session/task_result.go`):

| what | bound | the rule |
| --- | --- | --- |
| the record's own body | `taskResultLimit` (**16,000**) | the checkpoint is rewritten on every transition, so an unbounded body is a file written hundreds of times. Past it the text is cut with `…` and the whole is written beside the transcript. |
| what one reader is handed | `taskResultCarry` (**4,000**) | a landing note, a continuation's finding and one notice each carry this much — a parent folding five pieces back reads five of them — with the address of the whole beside it. |
| the file it overflows to | `<transcript stem>-result.txt` | written whenever the answer is longer than one reader gets, through a temporary file and a rename, so a reader following the pointer never opens a half-written answer. A write that fails leaves the transcript as the source and never a path to a file that is not there. |

What travels is decided by the node's **state and ending**, never by the
report's wording, which an accept or a late verdict may rewrite: a landing whose
ending is `refused` names its output and its address instead of handing the body
on, and every other settled landing hands it over. The only question asked of
the report is whether it already contains the answer word for word, which can
only omit what the reader is already holding. On the inherited-brief road the
address rides the prerequisite's HEADER, which the shared pot above does not
clip. Pinned by `internal/session/task_result_e2e_test.go`.

## Specialist tool discovery

Chat starts with core tools and one local `load_capability` registry operation
for available media, settings and saved-procedure groups. Loading appends the
original schemas on the next request within the same turn, preserving the
existing order, execution path and permissions. Workers retain their direct
belts. No new inference call selects or constructs a group, although first use
needs an additional model request to call the loaded tool.

The standard prefix fixture measured **47,606 → 44,347 bytes**, including the
heavier wording that explains discovery; `fixedPrefixBudget` stays **48,000**.
The fully enabled tool block measured **40,595 → 26,740 bytes**, including its
**708-byte** loader. `TestShelvingTakesMoreOffTheToolBlockThanItPutsOn` compares
complete encoded blocks and requires net savings at least **four times** the
loader's encoded size. Byte savings are not measured provider tokens, cache
hits, latency or bills. Loading changes the prefix once; repeat loading leaves
it unchanged. Reopening restores load calls still in saved history; a load
compacted away may be needed again.

## Following through on a completion claim

A turn may decline handoff once per request when its own continuation says no
work remains and the other reader names no independent parts. Agreement does
not grant another decline. The meter asks again after **10 additional rounds
of real work**, using `checkpointPrice`, and does not grant the same request
another completion decline. Watching existing work does not advance this count;
a new direction changes the request. This adds no classifier or model call to
an ordinary tool round. `internal/session/completion_stale_test.go` pins the
bound, revised direction, and survival of commands already owned by the turn.

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

## The frozen tool history, rebuilt per request

Every request of every tool round re-sends the whole conversation, and old tool
results are the bulk of it. The snapshot on its way to the provider therefore
carries the results of earlier turns as a REDUCED VIEW, while the live
transcript and the journal keep every byte (`internal/session/toolcompact.go`).

| bound | value | why |
| --- | --- | --- |
| what one reduced result keeps | head **200** (`checkpointResultBytes/2`) + tail **400** (`checkpointResultBytes`) | the tail is the verdict the checkpoint reader already proved is enough; the head is what ran, and where `no such file` and a compiler's banner land. Both are derived from the one bound rather than written twice. |
| left verbatim below | **600 bytes** (`compactViewBytes`) | a view of a result that small repeats most of it and then charges a header for having done so. |
| all consumed results together | **5,000 tokens** (`checkpointDigestBytes`) | the same account the checkpoint digest is held to. Over it, the oldest shrink to stub.go's one-line account, oldest first. It is a ceiling to walk towards: several hundred calls weigh more than it even as single lines. |
| the walk itself | one pass, running total | re-adding every old result on every iteration is quadratic in the call count, on the hot path of every request. The call-id→tool-name index is built once for the same reason. |

Every reduction names where the whole result can be read, and **one resolver
answers for all three passes** — this view, the end-of-turn stub and the
current-turn fold (`Agent.fullResultPointer`). It answers with the result's own
bytes filed under `logs/stubs/` (`writeStub`), which `read` opens and pages at
any size; where there is nowhere to file them, with the session journal and the
call id to grep for; and otherwise with `not retrievable`.

**A `store:` ref is not a pointer**, and it was the first answer all three passes
used to give. Nothing on the belt fetches a store message by id —
`search_conversations` searches words and clips every hit to one line
(`tools_conversations.go`) — so with memory on, every stub in the session pointed
at a handle only this process could resolve.

The memo behind it has three bounds, because it sits on the request path:

| bound | value | why |
| --- | --- | --- |
| the place a pointer may name | one reading per request (`Agent.resultPlaceNow`) | the workspace moves under `a.mu` (`AnchorWorkspace`); a resolver reading the field per result would race it and could answer two ways inside one request. The stub pass and the turn fold pass the same reading, already holding the lock. |
| the memo's key | workspace + the result's fingerprint | a stub path is relative to the workspace it was filed in, so an entry kept across an anchor names a file `read` now resolves elsewhere. |
| the memo's size | `filedCap` = `defaultContextWindow × bytesPerToken ÷ stubMinBytes` (≈ 341) | the most results one request could carry. Past it the memo is dropped whole rather than evicted one at a time; a miss costs one write. |

A filing that FAILED is remembered as the absence it is, so the fallback is the
journal and the request path does not retry the write per result per request. The
retry happens when the workspace changes or the memo is dropped. The write itself
lands through a rename (`writeStub`), because the file may be read by a model
following a pointer that is already in flight.

Pinned by `internal/session/toolcompact_test.go`. Retrieval is proved with the
belt's OWN read tool rather than with a string assertion:
`TestAReducedResultsPointerFetchesTheElidedMiddleWithTheBeltsOwnRead` runs a turn
with a store AND a journal behind it, takes the pointer out of the request the
provider was sent, pages the file it names and finds the sentinel the view
elided; `TestStubbingWithAStoreOnPointsAtSomethingTheBeltCanOpen` does the same
for the end-of-turn stub. The fallbacks are pinned by
`TestThePointerFallsBackToTheJournalAndThenToNothing`, the anchor and the race by
`TestAPointerStillOpensTheSameBytesAfterTheWorkspaceMoves` and
`TestResolvingAPointerRacesNothingWithAnAnchor` (the latter is a `-race`
assertion), the bounds by `TestThePointerMemoStaysBounded` and
`TestAFailedFilingFallsToTheJournalWithoutSpinning`, the repeat by
`TestCompactToolHistoryRepeatsItselfExactly`, the pairing by
`TestCompactToolHistoryKeepsEveryCallPairedWithItsResult`, and
`BenchmarkCompactToolHistory` reports the constant at 100, 400 and 1,600 rounds.

## The compaction threshold, and the ceiling that is no longer a constant

Compaction fires at `window − max(15% of window, 16,384)`
(`internal/session/loop.go`'s `CompactThreshold`), and the WINDOW it is taken of
is the model card's own figure.

It used to be clamped first, to twice `defaultContextWindow` — 256,000 — for
every model alike. That ceiling was put in for a real failure: a catalog row
claiming 1,310,720 tokens put the trigger at 1,114,112, a conversation grew to
386,309 tokens without folding once, and what came back at that size was the
model's own template turned inside out. **The bill for it was paid by every model
that was telling the truth.** Measured on 2026-08-31: a two-and-a-half-hour run
on a model advertising 1.3M compacted nineteen times, each pass at around a
hundred thousand tokens, each one throwing away the prefix cache the run was
otherwise getting 57–61% of its prompt back from.

So the ceiling is a MEASUREMENT now and not a constant: the narrowest prompt this
model has actually been refused for being too long, learned from the overflow
refusal itself and remembered across processes
(`internal/provider`'s `NoteServedWindow` / `ServedWindow`, applied by
`session.TrustedWindowFor`). A model nobody has refused is believed; one that has
refused is capped at what it refused, for good. The 386k incident now costs one
turn per model per machine instead of every model for ever.

Two guards stand behind that trade and neither is new: `guardOversizeRequest`
still shrinks a transcript that has grown past the trusted window before it goes
out, and the reply guard still cuts an answer that has stopped being language.

The other half of the same law is that the window has to REACH the agent doing
the folding. A child agent — a task node's worker, an adaptive run's worker, a
forked hand, an auditor — running on a model other than the conversation's used
to be handed zero outright, and so folded against the 128,000-token default
whatever its own model claimed. It is handed that model's card figure now
(`Agent.childWindow`, and `Config.ContextWindowFor` inherited by every child).

**And a person's own line outranks the derivation.** The fill percentage
(`ctxbudget.DefaultFillPercent`, **60**) reaches the process from three places
that are one setting — the `context fill` row, `AFORGE_CONTEXT_FILL_PCT`, and
`--context-fill N`, which sets that variable for one run. It governs the
conversation's fold line **only when somebody set it**: `ctxbudget.Limits`
carries zero for a row nobody has written down, `ctxbudget.PinnedFillPercent`
reports that as unpinned, and `compactThresholdOf` then draws the derived line
above. Honouring an untouched sixty would have folded every conversation at
sixty percent of its window — on the 1,310,720-token card, 786,432 instead of
1,114,112, which is the regression the derivation exists to prevent.

Pinned, the line is `fill × window` held between two bounds and nothing else is
free to move:

| bound | value | why |
| --- | --- | --- |
| the fill itself | **10–90** (`ctxbudget.clampFill`) | a typo may neither starve nor overrun a window |
| ceiling | `max(window − ctxbudget.CompletionReserve(), derivedThreshold(window))` | the room every call keeps for its answer (**65,536**, `--completion-reserve`) is still kept — and on a window under ~437,000 tokens that reserve is larger than the derived one, so the derived line is the ceiling instead. Without the second half, a pin of ninety on a 128,000-token window would land BELOW what an unpinned session gets. |
| floor | `2 × keepRecent(window)` | the verbatim tail is never folded, so a line at or under it fires every step and finds nothing to take. Twice it leaves a pass something to fold and somewhere to fold to. |

The chain `threshold > compactTarget > keepRecent` holds under both laws, because
`compactTarget` derives from whichever threshold governs rather than from the
derivation alone.

**And the fold inside a pass is asked for against the target, not the trigger.**
A pass stubs first, and the stub pass alone routinely lands the estimate just
under the trigger and thousands of tokens above the target — below the line that
fired the pass, with no headroom bought. Gated on the trigger, the fold then did
not run at all and the next step fired another pass, which is the once-a-step
thrash `compactTarget` exists to end. Pinned by
`TestAPassThatOnlyStubbedStillFoldsToTheTarget`, whose fixture SEARCHES for a
history that stubs to between the two lines rather than hard-coding today's
arithmetic.

Pinned by `internal/session/window_policy_test.go`,
`internal/session/window_guard_test.go` and
`internal/session/context_fill_test.go`.

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

## The home laws, which are about the machine in front of you

The connection laws below were written about a far machine; these are the same
laws with the latency at THIS end. Home is the screen a person walks with the
arrow keys and sweeps with the pointer, and it is in `AllMotion` (view.go), so a
pointer crossing it is answered up to sixty times a second (coalesce.go). Every
one of those answers runs on the update loop, which is the one goroutine that
also decodes the next key.

**What they were written after.** The owner reported: *"when I hover or go up and
down in home it's very laggy, I think it has something to do with git"*. Three
separate things were true, and the guess was the smallest of them.

| Law | Where it is pinned |
| --- | --- |
| **The deliverables index is read ONCE for the whole screen**, on `open` and on the beat, and re-parsed only when the file changed. | `internal/tui3/homeband_deliverables_test.go` |
| **A key that moves the cursor on home runs no command.** It ASKS for one. | `internal/tui3/homesnappy_test.go` |
| **A pointer motion on home runs no command.** | `internal/tui3/homesnappy_test.go` |
| **A pointer motion on home builds ONE home frame**, and the one it builds is the paint's. | `internal/tui3/homesnappy_test.go` |
| **A key or a motion on home walks no directory.** | `internal/tui3/homesnappy_test.go` |
| **A home beat walks the world exactly once.** | `internal/tui3/homesnappy_test.go` |
| **The card's own two readings — the conversation's journal and its inbox — are ASKED FOR and not taken.** | `internal/tui3/homesnappy_test.go` |

**There is no millisecond in that table, and there must not be**, for the reason
this file's doctrine gives: a count is a fact about the code and a stopwatch is a
fact about the weather. The numbers below are what the counts were derived FROM,
written down so the caps above can be read as a decision somebody signed for.

**The three costs, measured on this machine.** A card ARRIVES once per arrow key
and once per hover that moves, so a cost "cached per card" is a cost paid per
keystroke:

| what | cost per arrival | why |
| --- | --- | --- |
| the deliverables index | **7–11 ms** | 900 KB of JSON decoded to keep one conversation's four rows |
| the conversation's journal | **1–33 ms** | `session.Peek` scans the whole transcript |
| the repository | **7.7 ms** | `git status --porcelain=v2 --branch` on aforge's own worktree, warm, with a **1 s** ceiling on it |

The first is one file about the WHOLE MACHINE, so it is read with the world and
filed by the conversation that made each row. The other two are about the row
itself and cannot be read in advance for every row on the machine, so they are
asked for as `tea.Cmd`s and answered as messages (`internal/tui3/homecardread.go`,
`homeband_repo.go`). The band draws the last answer it was given; a row nobody
has read for yet draws no band, which is the emptiness law rather than a blank.

**And the pointer built two frames.** `app.homeHover` built a whole home frame of
its own to hit-test against and then asked for the repaint, so an answered motion
drew the screen twice. It resolves against `homeView.painted` now — the frame
that is ACTUALLY ON THE SCREEN, which is also the more honest of the two answers,
because a frame built inside a hover is a frame nobody has ever seen.

**And the beat walked the world twice.** `refreshHome` asked `readWorld` for the
reading and then `worldKnown` whether the reading was an answer, and each of
those is a full walk of the places root — every project's index, every session's
`meta.json`. `app.readWorldKnown` takes both from one walk.

Measured end to end, on a lab of eight repositories answering `git status` in ten
milliseconds each — a tenth of what a cold or network-mounted worktree costs:

|  | before | after |
| --- | --- | --- |
| twenty arrivals (key, readings, frame) | 1.14 ms each, **worst 10.4 ms** | 0.048 ms each, **worst 0.10 ms** |
| sixty answered motions | 0.57 ms each, **worst 10.8 ms** | 0.116 ms each, **worst 0.79 ms** |
| commands run on the update loop | 3 | **0** |
| home frames built per motion | 2 | **1** |

And on this machine's own home, whose repositories answer in microseconds because
the same three workspaces are cached, the two halves that are not git still show:

|  | before | after |
| --- | --- | --- |
| sixty answered motions | 0.25 ms each | 0.127 ms each |
| a resting beat | 6.8 ms | 2.9 ms |

**What a person would see if this were wrong.** Too eager, and the screen is
where it was: a keystroke behind a `git status` on a repository big enough to
need its whole second, with every key typed behind it queued. Too lazy, and a
card never learns its branch, its files or where the conversation got to — which
is why every law above is a pair, *the loop runs nothing* AND *the reading still
lands*, and why `TestTheCardsReadingsAreAskedForAndNotTaken` follows the same
gesture all the way to the state it leaves behind.

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
| **Prose whose first token is not path-shaped arms no timer and asks the disk nothing.** A sentence of ordinary prose costs two integer comparisons a character and nothing else; a sentence that begins with a real path pays the bounded per-reading cost the next row states. | `internal/tui3/dropkeys_test.go` |
| **Typing a slash command costs the same.** A dropped path is told from a command by a SEPARATOR INSIDE IT — `/var/folders` has one, `/help` does not — which is string work on runes already in memory. | `internal/tui3/dropkeys_test.go` |
| **A burst arms ONE wakeup**, however many characters it holds, and the one in flight re-arms itself while characters are still arriving rather than a second one being asked for. It is `pointerFold.settling`'s shape exactly. | `internal/tui3/dropkeys_test.go` |
| **A settled burst asks the disk at most once per candidate in at most four readings of the run**, and only after the string gate above has passed. | `internal/tui3/dropkeys_test.go` |
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
| **A stream that has gone quiet is cut**, with keepalives buying bounded patience and no more. | `midStreamGapBound`, 45s, is what a lane nothing is known about gets — half the first bound, because a model that has started writing has finished deciding. A lane whose RATE this process has measured is cut at `gapFor(rate)` instead: the time that lane takes to write `streamGapLumpTokens` (3,500 — the fourteen-kilobyte server-side lump of 2026-08-24, at the estimator's four bytes to the token), clamped to `LagGap`…`midStreamGapBound`. So 250 tok/s waits 15s where a stranger waits 45. The keepalive extension window stays the FLAT bound at every rate, and `bufferedQuietBound`, 150s, still caps the total quiet. | `internal/provider/streamguard_test.go`, `internal/provider/patience_measured_test.go` |
| **A reply that never ends is cut at a wall derived from the LANE'S OWN history** — the longest reply that endpoint has actually finished for this process, times `streamWallFactor`, clamped to `streamWallMeasuredFloor`…`streamWallCeiling`. A lane with NO history gets `streamWallFloor`, 5m, and that figure is now the outer bound for a stranger rather than the floor under everybody: it used to outrank the derivation, so a lane whose longest finished reply was twenty-four seconds still waited out five whole minutes, and two streams in the dogfood run of 2026-08-31 did exactly that on endpoints sustaining 83–270 tok/s. `streamWallMeasuredFloor` is `bufferedQuietBound` rather than a number of its own: the shortest honest wall is the longest honest silence, or the wall would cut a stream the silence bounds are still being patient with. | `internal/provider/velocity.go`'s `runs` ledger. A model-size table is a claim this process cannot check; a completed reply is a measurement. | `internal/provider/streamguard_test.go`, `internal/provider/patience_measured_test.go` |
| **An endpoint whose ANSWERS cannot be used loses standing, and wins it back by serving.** A guard cut — silence, stall, overrun, soup, unparsed tool grammar — and an answer with nothing in it are reported to the lane belief as outcomes that were not accepted; every answer that survives every guard is reported as one that was. The belief decays toward the lane's prior over `lane.QualityHalfLife` and the frontier gate reads it against the role's own `QualityNeed`. | No new number: `lane.Outcome` and `Ledger.NoteOutcome` have existed since the routing wave and had no production caller until this. The decay, the recovery and the gate are all `internal/lane`'s own. | `internal/provider/lanequality_test.go`, `internal/lane/garbage_test.go` |
| **An endpoint that STALLS is treated exactly like one that REFUSES**: its lane is struck, memoized for `ignoreCooldown`, and every request encoded afterwards routes around it. | `velocityLedger.pace`, per model, sourced from the endpoint the wire itself named. | `internal/provider/unwatched_test.go` |
| **A cut retries the CALL, never the leaf**, and says so on the stream a person is reading. | `cutBudget` — 2 attempts when the ledger routed around the endpoint, 1 when it could not. | `internal/session/loop.go` |

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

## A leaf's bounds, and the one that is allowed to land it

Written against ink s9 of 2026-08-29, where three leaves were landed early by a
bound the record could not name.

A leaf carried **five** ceilings. Three of them could land it and all three set
`Exhausted = StopBudget`, so the journal said *"it was still working when it ran
out of its tokens"* and the surface printed the grant — which in that run was
150,000 against three leaves landed at 240,000 by a different meter. The first
three readings of the store each blamed a different ceiling, and none of them
could be checked.

| bound | what it counts | may it land the leaf |
| --- | --- | --- |
| `spent()` vs the grant | uncached prompt + cached at `cachedTokenWeightPercent` (10, the provider's own discount) + completion — **what the job pays** | **yes** |
| `maxTurnBackstop` (400) | iterations | **yes** |
| the no-progress guard | repeated calls, a stagnant window, `noProgressTurnFloor` (60) | **yes** |
| `rawCeiling` (3 × grant) | Σ over turns of prompt + completion, undiscounted | no — wrap-up warning only |
| `reuseCeiling` (working set × fill × reuse = 240,000) | Σ over turns of prompt sent | no — wrap-up warning only |

**The two cumulative bounds were retired as stops, and the arithmetic is why.**
Σ over turns of the prompt is `turns × mean-context` wearing a token name: a
transcript that only grows re-sends its whole prefix every turn, so the sum
climbs at the same rate for a leaf doing hard work as for one circling. Two
measurements settle it:

- **No separation.** ink s9's leaf stopped at a duplication factor of 8.9×; the
  audited runaway `reuseCeiling` was written for ran 11.2×. No detector lives in
  a gap of 1.26×.
- **It rewards carrying more.** The ceiling is stated against the *window* and
  consumed against the *transcript*, so a leaf holding 30k of a 96k fill is
  landed at a third of the turns a leaf holding 96k gets, for the same work.

ink s9's first attempt was landed at turn 13 of a 200-turn grant having spent
104,064 of 150,000 tokens of billed work and 372,941 of its 450,000 raw ceiling
— 69% of the money, 31% unspent, and thrown away. All three of its leaves were
landed at exactly `reuseCeiling` plus the four landing turns: crossed at turns
13, 12 and 9, landed at 17, 16 and 13.

The runaway both were written for — a warm loop re-reading files it has already
read — is the no-progress guard's case, and `noprogress.go` opens by saying in
terms that *a magnitude bound cannot separate "many turns because the work is
hard" from "many turns because it is stuck"*, and that a signal which can was
what was missing. That signal exists, it fires at turn 60, and it is a fifth of
where the raw ceiling would have reached. Both numbers survive as pressure on
`budgetUsed`, where firing early on an honest leaf costs a sentence telling it
to wrap up rather than the leaf's work.

**And the bound that fires names itself.** `exec.Meter` carries the bound's name,
its two numbers and their unit; `store.LeafExhausted` journals them beside the
`StopReason`; the exhaustion sentence quotes them. A record that cannot say which
of five ceilings stopped a leaf is FAILSAFE.md's fourth clause exactly — an
absence that means five things at once. Pinned by
`internal/exec/inks9_test.go`.

## The generalist leaves a record

`exec.TranscriptFrom` had **one reader in the tree**, and it was not the leaf
belt — so the worker every unrouted node gets recorded nothing durable at all. ink
s9 ran three leaves on it and left a store with zero transcript rows, which is
why `resident.BankedRun` found nothing, why every continuation started cold, and
why the resumption the lease lane built could never fire on the belt that
actually runs.

It is wired at the **flight recorder** rather than in each loop, because every
turn of the generalist and every note the harness writes about itself already
passes through that one object with the response, the calls and the results in
hand (`tracer.sink`, `newRecordingTracer`). One seam, and the coding pipeline
gets it in the same change because it builds a tracer too. The cost is the batch
the store already imposes — `store.MaxTranscriptBatch` — and not one extra write
per tool result, which is the line `internal/exec/liveness.go` draws.

## What a growing job hands its next piece

`resident.replanOverrun` is the one seam every growing job passes through — the
overrun round, the delivery gate's gap round, the cooperative split and the
deferred resumption all reach the graph through it. Two things are read THERE,
from the lineage, rather than passed in by whichever caller asked:

| what | where it comes from | bound |
| --- | --- | --- |
| the recorded runs | `LineageBank` over `store.LineageNodes`, newest first, sink skipped | `BankedTranscriptBytes` **once for the whole composition**, not per node |
| the open findings | `ReadOpenFindings` over `store.DeliveryGateLineage` and the newest `VerificationReading` | `openFindingsLimit` (12) per list, and the count is printed when a list is cut |

Both are lineage reads because **a growing job does not keep its id**: a repair
round is spliced beside the work it repairs, under the root, so a node-shaped
read finds a fresh row with nothing on it. `store.LineageNodes` is an id-range
read on the `-x` namespace, the same law `DeliveryGateLineage` already ran on,
rather than a second copy of it built from parents and edges.

`ReadOpenFindings` runs on **every leaf claim** (it composes the brief), so its
walk is bounded by construction: it reads one gate query, then walks the lineage
newest-first and **stops at the first node that took a reading at all**. A node
that read the tree after another node read it holds the newer photograph, and the
older one is history rather than a finding — so keeping the last answer over the
whole lineage would cost O(nodes) queries per claim to arrive at the same string.

Measured, textual s9: three gap rounds, fourteen briefed nodes, the same two
unexercised behaviours reported on every gate, and thirteen of the fourteen
briefs naming neither. Pinned by `internal/resident/lineage_test.go` and
`cmd/aforge/openfindings_test.go`.

## Conversation trees on Home and Tasks

Tasks builds its conversation and parent index once per reading and reuses it
for layout. Search retains the full ancestor chain; valid hierarchy depth is
not capped. The visible indentation uses at most four two-cell levels and
shrinks further on narrow terminals (`tasksKinLevels`, `tasksKinRoom`). This is
a display allowance, not a limit on delegated work. Home preserves at least
sixteen cells for a task name by shortening ancestor prefixes
(`homeWorkNameFloor`), and its desktop preview still shows three task names.
`BenchmarkTasksConversationHistory` exercises forty conversations holding 5,120
tasks; the fixture is built outside the measurement.
