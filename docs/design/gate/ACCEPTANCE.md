# Acceptance: why the gate passed two deliverables that were wrong

*Written 2026-08-29 against the s4 section of `bench/deepswe/AUTOPSY.md` and the
five stores under `bench/deepswe/results/*-s4/`. It is the sibling of
[SETTLEMENT.md](SETTLEMENT.md), which fixed the REFUSAL path, and it is about the
hole that fix uncovered: the ACCEPTANCE path.*

## The shape of the failure

The settlement lane fixed what happens when the gate says no. s4 proved it: igel
ended at exit 2 with the finding named, which is the first honest failure in
twenty runs. And then two runs of five ended at exit 0 with a reward of 0,
because the gate had said **yes**.

ofetch s4, `gate: pass` at ten minutes, on a deliverable whose first sentence is:

> All 56 tests pass (28 existing + 28 new circuit breaker tests).

The claim is true. The suite the leaf ran was green. The hidden fail-to-pass set
is 41 of 47, and the six failures are one family the request states in so many
words:

| the request's own line | what the leaf's test file exercises |
| --- | --- |
| "Rejected non-listed statuses … must not close half-open state" | only the failure-streak half of that sentence |
| "A half-open probe keeps its slot for the full logical request, including internal retries" | nothing |
| "Parse/hook failures are not retried by status-based retry logic" | nothing |
| "`half-open` -> `open` on failed probe, restarting cooldown from that failure time" | the transition, never the restart time |
| "Circuit state is keyed by URL origin" — per-origin independence | keying by origin, never two origins at once |

happy-dom s4 is the same shape with a repair round in front of it. The first gate
was right — "The deliverable is a list of file paths, not the implementation
itself" — the run repaired, and the second gate passed a deliverable claiming
"All tests pass (31/31)" at 13 of 14 hidden tests.

Four seeds of ofetch sit at 37–42 of 47 with the same family failing. The leaf
tests narrowly around **what it built**, never around **what was asked**, and
then reports the count.

> **In both runs the gate accepted the WORKER'S CLAIM about tests the worker
> wrote itself.**

That is FAILSAFE clause 2 — *source evidence from the world, not from the
component you are checking* — broken in the one place the whole system's verdict
is decided. A leaf's own new tests are structurally incapable of finding what the
leaf did not think of; SETTLEMENT §4 already said that about regressions, and it
is exactly as true about coverage. The count of them proves nothing: igel s2 ran
its suite thirty-three times and scored 0.

## What was missing, stated as a lack

Three things the run never held:

1. **Nothing anywhere was a list of what the request asked for.** The compiled
   plan holds `Done` — a criterion of at most six conditions, written by the
   planner in its own words, aimed at what the work *produces*. The request's
   forty bullets never became structure. So at the moment of judgement there was
   no checklist to be short against.
2. **Nothing asked whether a check existed for each thing asked.** The gate reads
   the deliverable, the file list, the run tail and the diff. Every one of those
   is a record of what the work DID. None is a record of what the work was FOR.
3. **The prose claim was load-bearing.** "All 56 tests pass" arrived in the
   deliverable, in the same block as the answer, and the judge weighed it as
   evidence — because nothing beside it said what the world had actually
   returned.

## The law

> **ACCEPTANCE IS A CHECKLIST DERIVED FROM THE REQUEST BEFORE THE WORK, HELD BY
> THE GATE, AND SETTLED AGAINST THE WORLD. A behaviour the request states with no
> check that exercises it is a finding, and no claim about a test the worker
> wrote is evidence of anything.**

Four mechanisms, each stated at the line it lives on.

### 1. The checklist exists, and it is grounded

`internal/plan/accept.go`. At plan time — the same call shape as `Ground`,
`Contracts` and `Briefs`, and for the same reason: it reads the request once,
before anything has been produced, so nothing it says can have moved in response
to the work.

A `plan.Point` is one behaviour the request states, carrying two fields: the
`Behaviour` in the request's own vocabulary, and the `Quote` it is a reading of.
It is stored on `plan.Spec` beside `Done`, which is the one object in this system
that travels verbatim through a retry (`RetargetSpec`), so a repair round is
judged against the same checklist its predecessor was.

**Every point must be grounded in the request**, through the same three doors
`revision.Grounds` opens for a review's finding — a quotation with its elisions,
a file name, or a set of symbols the request also names. A point that is not
grounded is DROPPED. That is what stops the harness inventing scope for itself,
and it is the identical invariant, read by the identical code, that already
decides whether a review's finding may buy work: `revision.Held` calls
`citationGrounded` and nothing else. Grounding is applied where the checklist is
USED rather than where it is written, so a list that reached the gate by some
other route is still weighed against the person's own words before it can
convict anything.

**The count is derived, not typed.** A request cannot state more behaviours than
it has lines, so `plan.NormalizeAcceptance` keeps at most one point per non-empty
line of the request. Past that the model is not describing the request any more.
The gate holds however many survive that and its own budget share; the finding it
raises names eight and counts the rest, which is the rule
`revision.regressionsNamed` already spells for the sibling finding.

The worker is never shown the checklist. That is deliberate and it is the whole
point: a worker handed the list of things it will be checked on writes checks for
the list, which is the failure being fixed one level up. The checklist is the
gate's, and the record's.

### 2. The gate checks each point against the world

`internal/revision/acceptance.go`, reached from `JudgeDeliverable` at the moment
the model judge says **pass** — and only then. A gate that is already failing the
work buys the repair round anyway; the acceptance settlement exists for the run
that was about to be called whole.

Evidence, in order of what it costs:

- **The checks the change itself declares.** `verify.PatchChecks` reads the
  worker's own diff and names every check declaration on an added line and every
  one on a removed line. It recognises a check by SHAPE — `it(`, `test(`,
  `def test_`, `func Test`, `@Test`, `#[test]`, `- name:` — never by a list of
  frameworks, and it costs nothing but a scan of a string the gate already holds.
- **The checks the project's own verification named.** `verify.ReportedTests`
  reads a runner's output for every identity it printed, passing or failing —
  the roster, where `FailingTests` reads only the red half. The reading is the
  one the verification photograph already took of the final tree
  (`verify.Reading`); where the photograph has no after half — a composed repair,
  a worker that took no second reading — the gate runs `verify.RunTests` itself,
  on the same budget, because the reading is the gate's to hold.
- **The mapping**, which is one judge call and the only new model round on this
  path: given the points and the checks, which check exercises which point. It is
  recorded on the gate event, so the mapping is auditable rather than implied.

A point that no check exercises is a finding:

> `no check exercises: <the point, in the request's words>`

It is `Sourced` — a measurement rather than a reading of the request — so it
carries no citation to weigh and the admission rules step aside for it, exactly
as they do for a regression. That is what buys the repair round through the
settle2 re-drive machinery, whose brief is to write the check and make it pass.

**And the claim carries no weight.** The deliverable's "All 56 tests pass" is
weighed as prose, never as evidence: what the judge is shown beside it is the
world's own reading — the command that ran, its exit status, how many checks it
named and how many were red. Where the two disagree the disagreement is itself a
finding (`revision.ClaimContradicted`), because a deliverable that misreports its
own verification has misreported the one thing the person cannot check for
themselves.

### 3. A check that disappeared is a finding

`revision.WeakenedChecks`. The verification photograph SETTLEMENT §4 built sees a
check that turned red. It could not see a check that stopped existing — deleted,
renamed, or skipped — and "delete the failing test" is the cheapest way there is
to make a suite green.

Two sources, and either convicts: a check declaration on a REMOVED line of the
worker's own diff, and a name in the before roster that is absent from the after
roster. The second is asked only when both readings are real and both named
something, because two empty rosters subtract to nothing and an empty one proves
nothing at all — the same fail-safe direction `verify.NewFailures` is written in.

### 4. What it costs, and where the number comes from

No new suite run on the common path. The reading the gate weighs is the one the
leaf's own verification photograph already took, on the budget PERF.md states —
`verify.ReadingBudget`, a share of the leaf's own wall, moved out of
`internal/exec/bare` into `internal/verify` so the gate and the worker read one
arithmetic rather than two copies of it. The gate takes a reading of its own only
where none exists, on that same budget.

One model call at plan time, one at the gate, and the second is spent only on a
delivery that was about to pass. Both are bounded by the same context budget the
rest of the gate's prompt is, and both are absent — not broken — when the
request states nothing checkable or the project declares no verification at all.

## What is deliberately not here

No count of tests, ever, as a measure of anything. No knowledge anywhere in the
harness of what the hidden tests are or that hidden tests exist. No phrase list:
a check is recognised by shape and a point by grounding. No new retry, no new
clock, and no cap that is not derived from something the request or the wall
already fixed.

---

## What s5 proved, and the three things it changed

*Added 2026-08-29 against the five stores under `bench/deepswe/results/*-s5/`,
taken on `b910ccf8` with every mechanism above already wired.*

Every mechanism above fired zero times in five graded runs. Each of the three
reasons is FAILSAFE's own rule broken again, one clause each.

### 1. A reading is of the RUNNER, not of the script around it

ofetch's gate said, in its own words:

> The project's own verification (`pnpm test`) exited 1 and named 0 checks.

`pnpm test` is `pnpm lint && vitest run --coverage`. A prettier complaint exits
1 in front of a suite that never ran, so the sentence is true of the script and
says nothing whatever about the tests. And even where the suite does run, the
roster was file-level: vitest's default reporter prints `✓ test/index.test.ts
(28 tests)` for the green half and names the red half only in a banner the
vocabulary did not know. pytest is worse — measured in textual's own image,
forty-seven green checks print as forty-seven dots and name nothing at all.

Measured across all five task images at their base commits, plain output against
the runner's own reporter — and the rule the numbers show is that **the amount of
identity in a default reading is a function of how many checks FAILED, not of how
many ran**, which is the worst possible property for a reader whose question is
what exists:

| project | runner | ran | named by the declared command | named by the runner |
| --- | --- | --- | --- | --- |
| ofetch | vitest | 28 | 2 (all green) | 28 |
| textual | pytest | 3422 | 448 | 3422 |
| ink | ava | 923 | 0 (dies in lint) | 922 |
| igel | pytest | 2 | 2 | 2 |
| happy-dom | vitest | 7260 | 192 | 7260 |

So the reading is taken from the runner underneath, asked for its own
machine-readable output, and the runner is found in what the project itself
declares: the body of its own test script or make recipe, its manifest's
dependencies, its runner's config file, its lockfile. `verify.ReadingStrategy`.
`--reporter=json` for vitest, `--json` for jest, `-json` for go test, `-rA` for
pytest (a flag, not a parser — its summary lines are already in the shared
vocabulary), TAP for mocha. Every strategy falls back to that shared vocabulary
over the same bytes when its own reader recognises nothing, so a runner nobody
here has met is read exactly as well as it was before, and the strategy that was
used is recorded on the reading.

### 2. A reading that named nothing is still a reading

With an empty roster `CheckEvidence` returned nothing and `settleAcceptance`
recorded `Unmeasured` — a note, on a pass. Fifty-two grounded points went
unasked. **A point with no check exercising it is a finding whenever the reading
was TAKEN, even when it named nothing.** Only a project that declares no
verification and a worker that derived no diff leave the question unanswerable,
and that is `Unmeasured`, which reaches the verdict and the stream as such.

And the settlement runs on EVERY verdict. It used to be asked only of a delivery
the model judge was about to pass; all ten gates in the sweep failed, so it was
asked at none of them. A repair round is aimed at the gap the gate NAMED, so a
round bought for a missing branch name leaves every unexercised behaviour where
it was. On a failing verdict the acceptance gap joins the gap already named — as
text only, never as citations, because a sourced finding is admitted with no
citation weighed and letting the judge's own prose ride in on that exemption is
the laundering the admission rules exist to prevent.

### 3. The checklist is per clause; the finding is per line

igel held four points against twenty-four hidden checks, textual five against
twenty. The ceiling and the prompt agreed that a LINE was a behaviour, and a
person writes three defaults on one line. Points are now derived per clause,
which is the unit a check is matched to.

The finding is grouped the other way, back onto the request's own lines, because
four halves of one sentence must not become four repair rounds. Both derivations
are in PERF.md, "What the acceptance checklist costs".

---

## What s6 and s7 proved: the checklist and its finding belong to the JOB

*Added 2026-08-29 against the s6 and s7 stores under `bench/deepswe/results/*`.*

Every mechanism above is one node's. A job is not one node.

**ofetch s7.** Round one mapped 54 points, named 18 behaviours nothing
exercises, and bought a repair. Rounds two, three and four hold ZERO mapping
rows. The continuation nodes are planned afresh so their specs carry no
`Accept`; the continuation worker (`linear`) takes no photograph, so there was
no roster to map against either. The gates that judged them raised prose gaps
about the deliverable's wording, and the run ended at 41 of 47 with the same
four defaults untested that round one had named out loud.

**textual s7.** The planner fell back to one worker (`the planner could not lay
this out — running it as one piece of work`), and that path dropped
`compiled.Accept` on the floor. The store holds no `acceptance` event and no
`verification` event at all: no checklist, no reading, nothing for the gate to
weigh but prose.

The rule both of them state:

> **THE CHECKLIST IS A READING OF THE REQUEST, AND EVERY ROUND OF A JOB HAS THE
> SAME REQUEST. So is the photograph, and so is what the job is still short of.
> All three are remembered against the JOB — the identical key
> `verify.BaselineFor` already uses — and every round inherits them.**

Four mechanisms follow, each in `internal/revision/acceptance.go`:

1. **The checklist is inherited.** `RememberChecklist`/`ChecklistFor`, keyed by
   `verify.JobKey(request)`. A round whose own spec carries none uses the job's,
   weighed through `Held` against the request exactly as before. And the planner
   fallback carries `compiled.Accept` onto its one leaf's spec, which is where
   textual s7 lost it.
2. **The reading is the job's.** `jobReading` reads this round's photograph
   first, the job's remembered baseline second, and takes one itself — on
   `ReadingBudget` of the gate's own remaining wall, remembered against the job —
   only when nothing anywhere has looked. That is what a run whose workers do not
   photograph gets instead of silence.
3. **The mapping is settled on every verdict of every round**, which it already
   was; what changed is that there is now a checklist and a roster to settle it
   with. It is one model call against a reading the job has already paid for, and
   it is the ONLY thing that can shrink the set: a round that wrote the missing
   checks grows the roster with names that map, and the next mapping notices.
4. **A finding measured once stands until a measurement closes it.**
   `RememberUnexercised`/`UnexercisedFor`. A round whose worker took no reading
   inherits the open set rather than passing over it, and the finding's last line
   is the score — `3 of the 17 behaviours this request states are still exercised
   by nothing` — so each round's brief says what REMAINS.

**And a pass over a suite nobody could read is not whole.** See FAILSAFE's
seventh failure, clause 5: `store.DeliveryGate.Unreadable`, exit 2, and the last
line `partial — nothing in this project's verification could be read: <why>`.

## What igel s8 proved: the run's own checks are always in scope

*Added 2026-08-29 against
`bench/deepswe/results/igel-persist-feature-schema-deepseek-deepseek-v4-flash-s8/graph.db`.*

All four `verification` events in that store read
`python3 -m pytest -rA tests/test_igel/test_igel.py`, `scope: touched packages
(1 file)`, `named: 2`, `red: 2`. The graded patch creates
`tests/test_igel/test_feature_schema.py` and
`tests/test_igel/test_integration.py`, holding about forty checks between them.
No reading of that job ever saw one of them.

The scope was not wrong. It was decided from the request, before the work
existed, which is what makes it the same scope every round inherits (§ "A
reading is scoped before it is bounded") — and it is exactly why it can never
contain a test the work itself goes on to write.

What that costs is the whole settlement. The mapping in § 2 is asked *which
checklist point does each name in this roster exercise*, and it is handed the
roster of a reading. A point whose only exercise is a check this round wrote is
therefore unexercised however good the check is; the finding is re-raised, the
repair round is bought again for work already done, and the before/after cannot
move because the after reading is running the same two checks the before one
ran.

> **THE SCOPED READING'S FILE SET IS THE STRUCTURAL ADJACENCY ∪ EVERY CHECK FILE
> IN THE JOB'S ARTIFACT RECORD. The run's own checks are in scope on every
> round, taken from the world's record of what the tree gained — never from the
> worker's account of what it tested.**

`verify.OwnChecks(root, record)` is the whole of the reading: every path in the
record that is a check file by the runner's own naming convention
(`testFileName`) and that the tree still holds. `Strategy.WithOwnChecks` joins
them to the pinned before-strategy's selection at the two seams that take a
second reading — `internal/exec/bare.photographAfter` over `Outcome.Artifacts`,
and `revision.measureFinalTree` over `Evidence.Artifacts`, which
`completeAgainstTheWorld` has already settled against the disk. The gate's own
fresh reading reaches the same place by the same route: `gateFocus` already
carries the artifacts, and a focus path that is ITSELF a check file is not
*adjacent* to the change — it is the change, so it leads the selection ahead of
both adjacency ranks and no cut can take it.

Widening the after reading is the one difference between the two halves this
mechanism allows, and two invariants pay for it:

- **The comparison covers rather than matches.** `Strategy.covers` reads a
  superset of the before selection, over the same base command in the same
  workdir, as comparable. Subtracting a wider roster from a narrower one is
  sound in the direction that matters; the reverse is not, and is refused.
- **A check the run wrote cannot be a check the run broke.** A red new test is a
  leaf that has not finished — which is what the acceptance finding is for — and
  not a regression. `Strategy.Widened` keeps the paths the widening joined on,
  and `Reading.Regressed` drops a new failure that either names one of them
  (pytest's `path::test`, go test's package path) or that a before roster kept
  and never mentioned. A reading that was not widened subtracts exactly as it
  always did: a runner that prints its failures and nothing else says nothing
  about its passes, and reading that silence as "no such check" would excuse
  every regression in every such project.

The cost is nothing: the same command against a longer file list, at the same
rung, on the same budget. See PERF.md, "The verification photograph's budget".

## What igel s9 proved: the reader that is silent and the reader that read nothing

*Added 2026-08-29 against the s9 stores under `bench/deepswe/results/*`.*

igel s9 and ink s9 hold **zero** `verification` events. Not a reading, not a
`read:false` refusal, nothing — across four nodes and eight exhausted leaves in
igel's case. ofetch s9, built from the same binary, holds **four**.

The difference is not the projects and not the scope mechanism. It is which
worker the ruler picked: `nodes.subharness` is empty for every node of igel s9
and ink s9, and `bare` for one node of ofetch s9. **Only `bare` journaled the
photograph.** § 2 above already says the reading belongs to the JOB and that the
gate takes one when no worker did — and it does; what it never did was write the
row. So a run whose leaves all went to the generalist could not be told apart,
from outside, from a run whose reader was broken.

> **THE GATE JOURNALS ITS READING OF THE TREE IT IS JUDGING, AND JOURNALS EVERY
> REFUSAL TO TAKE ONE.**

`revision.journalGateReading` writes the event `internal/exec/bare` writes, with
`when: on the tree the gate is judging` and `inherited` set when the row is the
job's baseline carried forward. Four paths reach it:

- the reading the gate took, or retook after a cut;
- the job's baseline, inherited rather than re-run;
- **no workspace** — the gate holds nothing to read;
- **no deadline** — a budget is a share of a wall and there was no wall.

It is a measurement and never a gate: a nil store, an unknown node, or a store
that refuses the row all leave the verdict exactly as it was.

Two things follow that are worth stating separately. `CheckEvidence` was split
into an exported form that takes its own reading and an internal one that is
handed the settlement's — the reader is memoised against the job, so asking twice
cost nothing in wall time and wrote a second row saying the same thing. And **an
empty strategy ladder is impossible**: `verify.placeStrategies` always ends a
place's rungs with the whole suite and the scoped rung is only ever prepended, so
`ReadingStrategies` returns nothing only for a project that declares no way of
checking itself — which `Photograph` answers with a sentence, never a silence.

### And the checklist has no say in whether the world is read

The reading sat BELOW the checklist, and `settleAcceptance` returned early when a
job stated none — so on that path nothing asked whether the project could be read,
`Judgment.Unreadable` was unreachable, and `store.DeliveryGate.Whole()` came back
true over a verification nobody had looked at. Exit 0, with the one question that
could have said otherwise never put.

> **THE READING, AND THE UNREADABLE VERDICT IT SETTLES, ARE TAKEN ON EVERY GATE
> REGARDLESS OF WHETHER A CHECKLIST EXISTS. The checklist governs coverage
> findings only — never whether the world is read.**

`revision.settleUnmeasured` is that half of the settlement, split out of the
coverage branch and asked of every verdict before the checklist is looked for. It
sets `Unmeasured` on a delivery nothing measured and `Unreadable` on the narrower
case that matters at the door: a project that DECLARES a way of checking itself
which this run could not read. See FAILSAFE's seventh failure, clause 5.

## What s10 proved: four ways a settlement was decided by something other than the world

*Added 2026-08-29 against the s10 stores under `bench/deepswe/results/*`.*

### 1. An acquittal is of one finding

textual s10 has ONE gate. It carries `unexercised` naming two groups of stated
behaviours, a verdict `refused` as *what it asked for is already on disk under
the name the request used*, and `overturned: true`. It settled **whole**, exit 0,
at 5 of 20 hidden checks — over its own measurement of thirteen behaviours it had
just found exercised by nothing.

Overturning says the review was wrong about ONE thing: the file it called missing
is on disk. It says nothing about a set measured by a different mechanism, on
different evidence, that closes only when a check exists.

> **`DeliveryGate.Whole()` is false while the coverage set is non-empty, whatever
> a later verdict overturned.** The last line names the count:
> `partial — 2 behaviours the request states have no check`.

`cmd/aforge/do.go`'s `gateStanding` leads with that count rather than with the
gate's prose wherever the verdict itself settled — otherwise the line would name
the finding that LOST as the reason the run is short.

### 2. The checklist is remembered where it is read, not where it is settled

ofetch s10's gate event holds `pass: true` and nothing else: no mapping, no
finding, no `unmeasured`. The planner read **47** points onto `task-2`'s spec and
journaled them; `task-2` was handed over without reaching a delivery gate; and
`task-2-x1`, planned afresh and carrying no `Accept`, reached the run's only gate
with no checklist. The coverage question was not answered wrongly — it was never
asked, and the run left at 42 of 47.

The job-level memory of § "What s6 and s7 proved" was right and had one writer:
the gate. `revision.RememberChecklistForRequest` is called from
`journalAcceptance`, where the checklist is read, so every round of the job
inherits it whether or not the first node was ever judged.

### 3. A judge's say-so is vocabulary; the mapping goes through the citation door

The mapping is one model call, and until now the only thing weighed about its
answer was that the check NAME EXISTED in the roster. ofetch keeps naming the
same family — `When circuitBreaker: true, defaults are halfOpenMaxRequests = 1`
and its siblings — as exercised by nothing while the suite grows checks about the
breaker in general, and a pairing weighed on vocabulary will eventually put those
two together.

> **A check may satisfy a point only where every name the point spells
> DISTINCTIVELY is a name the check spells too** — in its own identity, or in the
> body of the file that identity comes out of.

`revision.GroundMapping` uses grounding.go's own `symbolsIn`/`symbolShaped`, the
same door an admitted citation goes through, with whole-name matching so `Log`
inside `Logger` is not a mention of Log. It only ever REMOVES a pairing. A point
that spells no name is not judged there: there is no structural question to ask
of "Normal scrolling must still update the visible viewport", and a door that
answered *unexercised* to every such behaviour would fail every prose request
this program is given.

### 4. The second reading is aimed at the change, not only at the request

A scope has to be a reading of the REQUEST — at the moment the first reading is
taken there is no diff. A request is not a diff. textual s10 asked for *Log and
RichLog*: `RichLog` resolves to `_rich_log.py`, `Log` is a single word that
resolves to nothing, and both readings ran
`pytest tests/test_concurrency.py tests/test_textlog.py` (3 checks, the two files
that import RichLog through the package front door). The change touched `_log.py`
and `_rich_log.py`, and `tests/test_log.py` — which the repository already had,
named after the changed file by pytest's own convention — was read on neither
side.

> **The second reading's selection = the first's ∪ the structural adjacency of
> the DIFF ∪ the run's own checks.**

`verify.ChangedSources` is the other half of the record `OwnChecks` reads;
`Strategy.WithChangedWork` puts those source files back through `Adjacent`, so
what joins is the checks named after them and the checks whose imports resolve to
them, never a name that merely looks alike. `Strategy.covers` already admits the
superset.

And where the request resolved to nothing at all — ofetch s10 took six readings
and every one was `whole` — a whole rung killed at its ceiling has proved this
project's suite is bigger than the wall. `verify.ChangedWorkStrategy` aims the
second reading at the diff instead. That pair is deliberately not comparable
(a subset, which `covers` refuses and `Regressed` answers nothing to); what it
buys is the ROSTER, which is the half the coverage settlement spends and the half
a cut whole reading has none of.

## What igel s11 proved: a suite cannot see what nobody wrote a check for

*Added 2026-08-29 against
`bench/deepswe/results/igel-persist-feature-schema-deepseek-deepseek-v4-flash-s11/`
and the 37eabd38 autopsy.*

That run deleted `Igel.results_path` and seven sibling public class attributes,
moving them onto instances set in `__init__`. Its own check-level photograph read
the finished tree as an **improvement**: named 2 → 14, red 2 → 0. All **24**
hidden tests failed at setup on `Igel.results_path`, and the store held not one
word about it.

The reading was not wrong. The question it answers is a different one.

> **A CHECK IS EVIDENCE THAT SOMETHING IS EXERCISED. IT IS NOT EVIDENCE THAT
> NOTHING ELSE EXISTS.** The public surface is the half a suite structurally
> cannot see, and a name that was there before the work and is gone after it is a
> fact about the world, measured twice, with no model in the loop.

`verify.Surface` is that half. `PublicSurface` reads the tree's public names with
the baseline reading; `SurfaceOf` re-reads the files the run's own record says it
changed; `Surface.Removed` is the difference, scoped to that record — a name that
vanished from a file nobody touched vanished some other way, and reporting it
would hand a leaf a finding about work it never did. A whole module the record
names and the tree no longer holds contributes every public name it had
(`MissingFrom`).

`revision.RemovedPublicNames` raises it, **Sourced** and admitted with no
citation weighed, in the same place and for the same reason as a check
regression: nobody has to ask for the public names their repository already had.
It buys the repair round every Sourced finding buys. It names the removal and not
a remedy — putting the name back and keeping the new arrangement are both
answers, and which is right is the round's business.

**Read by language shape, never by a list of names.** Go through the standard
library's parser; Python, TS/JS and Rust through conservative line-and-indent
readers. The two ways a python class carries a name are two names —
`Igel.results_path` is reached on the class, `Igel().results_path` on an
instance — and igel s11 is exactly why: a reader matching the bare word would
have called that change no change at all. A leading underscore, an unexported Go
identifier, `private`/`protected`/`#field`, a non-`pub` Rust item: the author
said it is theirs and this takes them at their word. A rename reads as a REMOVAL
of the old name — which is what every caller of it sees — with the new name
visible in the same two readings for whoever wants it.

**The limits are the point.** Only declarations at the start of a line are read.
A name assigned inside a conditional, a class built by a decorator, an export
re-exported through a barrel, a symbol behind a macro: not read, deliberately. A
Go file that does not parse contributes nothing rather than half a package. The
cost of a name invented here is a false blocker on real work; the cost of a name
missed is the state this was written in. Bounds are in PERF.md, "What the
symbol-level photograph costs".

**Journaled as its own kind.** `store.EventSurface` sits beside the verification
event rather than inside it: the two answer different questions from different
evidence, and a run can have either without the other. Every comparison is
written, including one that found nothing — *sixteen files compared, no public
name lost* and *nobody compared anything* are two facts, and the absence of the
row was the only spelling either of them had.

## What textual s13 proved: a check that names the behaviour and asserts nothing about it

*Added 2026-08-29 against
`bench/deepswe/results/textual-richlog-follow-state-deepseek-deepseek-v4-flash-s13/`.*

The run ended at exit 0, gate `task-2-x1` `pass: true` over `tree (4 files)`, with
19 of 20 hidden f2p checks green and 4 of 6 p2p. Gate 1, one round earlier, had
named the behaviour that was later missed:

```
no check exercises: … RichLog.write(expand=True) no longer preserves full-width
justified rendering with current Rich …
```

Round two wrote `tests/test_log.py::test_rich_log_write_expand_preserves_full_width_justified`.
The mapping paired it with that behaviour, `GroundMapping` admitted the pairing —
every name the behaviour spells is in the file — and the finding closed. Here is
the whole of that check's evidence:

```python
rich_log.write("short", expand=True)
await pilot.pause()
assert len(rich_log.lines) > 0
for strip in rich_log.lines:
    assert strip.cell_length is not None
    assert strip.cell_length >= 5
```

`expand` appears in the CALL. It appears in no assertion, in that check or in
either of its two siblings, and `min_width` appears nowhere in the run's 44KB
diff at all. The hidden check for the behaviour —
`test_rich_log_expand_entries_reflow_after_min_width_change` — was red.

> **A BEHAVIOUR THE REQUEST STATES IS EXERCISED BY A CHECK ONLY WHERE THE CHECK'S
> OWN ASSERTIONS NAME ONE OF THE BEHAVIOUR'S OBSERVABLES.** Everything outside an
> assertion is setup, and setup is what a check MENTIONS rather than what it
> CHECKS. A behaviour a check names and no assertion weighs is *weakly
> exercised*: it stays open, it buys the repair round an unexercised behaviour
> buys, and the finding names the identifier to go and assert on.

Both halves are read from the world and no model has a say in either.

**The observables are read off the REQUEST**, by shape, the way `symbolsIn`
already reads symbol entailment out of a citation: a name spelled distinctively
(`is_following_end`, `RichLog.write`, `max_scroll_y`, `#follow-log`) or a name
BOUND to something (`expand=True`, `follow_end(animate: bool = False)`, `y=0`).
`expand` is an English word; `expand=True` is an argument, and the difference is
not in the letters but in what the person wrote beside it. See
`revision.Observables`.

**The assertions are read off the FILE**, by language shape, in the register
`verify.PublicSurface` reads in — `verify.AssertionsIn`. Python's `assert`
statement, `pytest.raises`, `self.assert*`; JavaScript's `expect(…)`, `assert.*`,
ava's `t.is`/`t.deepEqual`; Go's assertion libraries and the `if` condition
beside the `t.Fatalf` it guards, because the comparison is the half that names
anything; Rust's `assert!` family. **The owner of an assertion is the OUTERMOST
definition it sits in** — a textual test declares an `App` subclass with its own
`compose` method inside the test body, and a reader taking the nearest enclosing
`def` finds the test itself asserting nothing at all.

**Every silence favours the check, and that is the whole safety argument.** A
behaviour that names no observable is not judged here — "Normal scrolling must
still update the visible viewport and vertical scrollbar position" is a true
sentence with no identifier in it, and a door that answered "unexercised" to
every such behaviour would fail every prose request this program is given. Only
the checks the run itself wrote are read (`verify.OwnChecks`); a project's
existing suite was written before the request and is not this run's account of
its own work. A file in a language with no reader, a check whose declaration the
reader cannot find, a body the budget could not reach: all left exactly as the
mapping answered them. And ONE observable named in ONE assertion clears the point
outright — which means being generous about what counts as an observable makes
this door quieter, never louder.

**It is a finding of its own, not prose inside another one.**
`store.DeliveryGate.Unasserted` is the list, one entry per line of the request,
each carrying the observables nothing asserted;
`store.ExercisedPoint.Unasserted` is the evidence, on the mapping row that
produced it, so an autopsy can ask WHICH observable was skipped. It leaves the
delivery short in `DeliveryGate.Whole()` for the same reason `Unexercised` does —
an acquittal is of one finding, and a measurement of the repository is not that
finding — and the resident's brief prints it in its own section, asking for an
assertion rather than for another check.

**Also true of this run, and already right:** `plan.Acceptance()` did yield the
PRESERVED behaviours as points of their own — *RichLog still snaps back to the
newest entry after users scroll up, unlike Log*, *Normal scrolling must still
update the visible viewport…*, *it must post only when the boolean actually
changes*. All three were on the checklist and all three were mapped. Two hidden
p2p checks about scrollbar position still failed, and the second of those three
points names no observable at all, so this door is silent on it by design.

### Addendum, textual s16: one observable is not all of them, and the tree is a vocabulary

*Added 2026-08-29 against
`bench/deepswe/results/textual-richlog-follow-state-deepseek-deepseek-v4-flash-s16/`
(18/20 f2p, 4/6 p2p) and `…/ofetch-per-origin-circuit-breaker-…-s16/`.*

The assertion door above shipped and fired — three of s16's four gates carry
`unasserted` rows. It still let the run's largest gap through, twice over.

**Point [2], *Normal scrolling must still update the visible viewport and
vertical scrollbar position for both widgets*, named no observable at all**, so
the door was silent on it and the mapping's word stood. The run asserted
`scroll_y` and `max_scroll_y` **106** times — its content is taller than the
viewport, so the scrolling is real — and **not one assertion reads the ScrollBar
widget's own `position`**, which is exactly what the two hidden p2p checks that
failed assert (`assert 0 == 4 where 0 = ScrollBar(...position=0).position`).

> **A REQUEST NAMES THE TREE IN THE PERSON'S OWN WORDS.** *vertical scrollbar
> position* is `ScrollBar.position` spelled in English, and a program that reads
> observables only out of code-shaped tokens cannot see it. So the tree's own
> public surface is a VOCABULARY: a name is spoken by a sentence where consecutive
> words of the sentence spell exactly the words that name is built out of.

`verify.SurfaceIndex` is that reading (`IndexSurface`, `Spoken`, `Holds`), over
the surface the job's baseline already holds — no new walk and no new budget.
Two words at least, and the name must be QUALIFIED: one word of a sentence is a
word, and textual declares a `ScrollUp` message that "after users scroll up" has
nothing to do with. Measured on the s16 tree at its base commit, exactly **one**
of the thirteen points gains an observable this way — point [2], to
`ScrollBar.position` — and it reads unasserted. Four others get SMALLER: the
tree confirms `RichLog` is a type, and a type is what a check builds rather than
what it weighs.

**And the second half: EACH observable, not any one of them.** The door passed a
point the moment one of its names appeared in one assertion, which is how a
behaviour naming a viewport *and* a scrollbar position is settled by a hundred
assertions about neither. ofetch s16 has the same shape from the other side —
*tracks circuit state independently per origin*, with `vi.fn()` called once
rather than twice and the second origin never weighed.

> **A POINT IS ASSERTED ONLY WHERE EVERY OBSERVABLE IT NAMES APPEARS IN SOME
> ASSERTION OF THE CHECK MAPPED TO IT**, and the finding lists the ones that do
> not: `observables never asserted: ScrollBar.position`.

A qualified observable is satisfied by its member — the request writes
`ScrollBar.position` and a check writes `bar.position`, because the class is what
made the object.

**Two shapes are thrown away so this is survivable.** A hyphen is not an
identifier character in any language read here, so a token held together only by
hyphens is an English compound — `full-width`, `half-open` — and under
*every-observable* it would be a finding nothing could ever close. It is
re-admitted where the person wrote it as a selector (`#follow-log`) or where the
tree declares it. And a bare type the surface confirms is dropped, as above.

**ofetch's door had never opened at all.** All forty-seven of its mapped
identities are vitest's `describe describe case` joined by SPACES, with nothing
in the string to say where the groups end — so no declaration was ever found and
every pairing was left alone. `verify.Assertions.Named` tries the identity's
trailing words, longest first, and takes the first that is a case the file
declares. One trailing word is never tried: that is a coincidence, not a case.

**Journaled.** `store.ExercisedPoint.Observables` records what each point was
weighed against, beside the `Unasserted` conclusion — the resolution is the half
an autopsy cannot reconstruct, because the tree it was read from has moved on.

---

## What #428 proved: a coverage finding is raised only where a check could exist

Two measured runs, one synthetic errand and one real issue, and the same defect
underneath both: the gate asked which repository test exercises something no test
could ever exercise, and bought the rounds that went looking.

**The errand.** "Run the command '…' in this workspace and report the final line
it prints. Change no files." Its acceptance points came back as two ACTIONS of
the run — *the command is run in this workspace*, *the final line it prints is
reported* — and the finding asked which check exercises them. None can, by
construction, on a run that changes nothing. The run was satisfied by its first
leaf two minutes in and hit its 700-second wall twice out of two, at 4 nodes and
$0.048 the first time and 5 nodes and $0.057 the second; the two
coverage-mapping calls, each carrying ~127k prompt tokens of roster, were $0.022
of it on their own. More than 97% of the money was spent after the answer
existed.

**The issue.** Human-Agent-Society/reef#145, both doors, deepseek-v4-flash. The
headless door's fix was correct at 4m17s — the issue's own fail-to-pass tests 6
of 6 green, nothing regressed — and the gate refused it three times on "6
behaviours the request states have no check that exercises them". The run ended
at 753s, $0.0855 and exit 2 with the closing word `partial`; the chat door passed
the same issue at 483s and $0.0163.

### 1. A point is a behaviour or an action, and only behaviours are mapped

`plan.Point.Kind`, `plan.PointBehaviour`, `plan.PointAction`, read by the same
call that reads the checklist and written down in the same journal row
(`store.AcceptancePoint.Kind`). A behaviour is observably true of the finished
work; an action is something the RUN does. `plan.Behaviours` is the filter, and
`settleAcceptance` maps what it returns and counts it for the score line. Actions
stay on the checklist — they are the person's words — and are never mapped and
never named uncovered. **An unknown or absent kind is a behaviour**: the cost of
reading an action as a behaviour is one false finding, and the cost of reading a
behaviour as an action is a stated requirement nothing ever checks.

### 2. A run that changed no code is asked for no check

`revision.codeChanged`. A patch, or a record path that `verify.ChangedSources` or
`verify.OwnChecks` places inside the workspace. A path outside the workspace is
in neither list, which is what keeps a run's own sidecars from reading as a
change to the project. **It answers yes wherever it cannot tell** — an
unobserved run is mapped exactly as it was — because an empty record is only a
fact where somebody was recording (`Evidence.Observed`).

### 3. An empty or unreadable roster is not evidence that no check exists

This is §2's "a reading that named nothing is still a reading" with the case it
did not separate. `revision.rosterSpeaks` is asked only where the check list came
back EMPTY, and it distinguishes a project with a runner and no tests — a
measurement, and a finding — from a suite that could not collect, was killed at
its ceiling, or printed nothing a reader recognised. The second answers nothing,
and mapping against it declares every stated behaviour unexercised. The verdict
then carries `revision.CoverageUnread` and no finding is raised.

### 4. The work's own checks are read off the tree

The exhibit's mapping call carried 1,598 tokens of roster while a 195-line pytest
file with ten named tests sat in the run's own record. `Evidence.Patch` is filled
from `outcome.Account.Patch`, and **nothing in this tree sets `Outcome.Account`**
— the only writer was deleted with `internal/exec/swe.go` in `05b99537` — so the
diff half of `checkEvidence` has been dead on every belt since. `declaredByTheRun`
is the same source reached the one way that still works: `verify.OwnChecks` over
the record settled against the filesystem, `verify.DeclaredChecks` over each
file, bounded by the mapping's own two budgets. THE WORK'S OWN CHECKS ARE THE ONE
SOURCE THAT DOES NOT NEED THE PROJECT TO COLLECT.

The account plumbing itself is not restored here, and until it is, `Evidence.Patch`,
`patchBlock`, `removedChecks` and `Compose`'s patch remain unreachable.
