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
