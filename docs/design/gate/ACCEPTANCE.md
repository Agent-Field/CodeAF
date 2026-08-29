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
