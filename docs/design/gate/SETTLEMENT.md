# Settlement: why eight runs declared victory over work they had not done

*Written 2026-08-29 against `bench/deepswe/AUTOPSY.md` and the ten stores under
`bench/deepswe/results/`. Ten headless `aforge do` runs, five DeepSWE tasks, two
seeds, every model knob pinned to one model, each in its own graded container.
Eight graded runs, eight exit 0, eight rewards of 0. The median run stopped at
ten minutes of a ninety-minute wall having spent eight cents.*

This document is the reasoning behind the change. It is written the way
[FAILSAFE.md](../failsafe/FAILSAFE.md) asked for: each defect named at the line
it lives on, each fix stated as a law rather than as a patch, and every clause of
the fail-safe rule the defect broke said out loud.

## The shape of the failure

Seven of the eight graded runs settled AFTER their own review gate had named the
missing work and been refused with one sentence:

> `gate: refused — what the review asked for next is not in the request`

igel s1, node `task-39`, at four minutes and six seconds of ninety:

> The deliverable does not contain the actual code changes that implement the
> feature schema persistence and validation rules.

The review was right. The node then wrote "All 41 tests pass. Here's a summary of
what was done", and the process exited 0.

So the gate saw the gap, said the gap, was overruled by a rule, and the exit code
sided with the rule. Three separate mechanisms had to agree for that to happen,
and all three are wrong in the same direction: **each of them treats the absence
of an argument as evidence of completeness.**

## 1. Grounding: the invariant asked the wrong question

`internal/revision/judge.go:1230`, `citationGrounded`, decides whether a review's
finding may buy work. Its test is one line:

```go
if strings.Contains(ground, key) { return true }
```

— the finding, whitespace-normalised, must be ONE CONTIGUOUS RUN of characters
inside the request. Read the ten stores and that single line accounts for most of
the sweep.

### 1a. A quotation may elide, and the rule did not know it

Seven of the ten refused citations are quotations of the request that skip a
middle. igel s1 `task-39` cited, verbatim except for one elision:

> "After fit, write feature_schema.joblib in the results directory and record
> feature_schema_path, input_features, dropped_features, and
> duplicate_feature_aliases in description.json. dropped_features must be an
> object with excluded, constant, and duplicate lists. dataset.features must
> support include, exclude, drop_constant, and drop_duplicate. **...** evaluate,
> predict, and /predict must load and apply the persisted schema before any model
> call."

Every character on either side of the `...` is the person's own text. The whole
string is a substring of nothing, so the finding was refused as an invention and
the run settled. textual (both seeds), ink s2, happy-dom s3 and igel s2 (both
gates) fail exactly this way; the marker is `...` or a bare `…` depending on what
the model felt like typing.

This is FAILSAFE clause 1 — *detect by structure, not by vocabulary* — worn the
other way round. The structural fact is that **a quotation is a sequence of
spans**, and the rule was reading it as a single token of prose.

### 1b. Two doors, two ground sets

`AdmitGapRevision` (`judge.go:1269`) grounds a finding against the request AND the
working method. `ExtendForGap` (`judge.go:1377`) grounds it against the request
ALONE. So a finding that quotes the method survives the first door and dies at
the second, having already cost a repair round.

ink s1 spent both of its gates on this. Its citation, both times:

> "The whole finished implementation is written out in the worker's own final
> message, not merely saved to a file."

That is the working method, verbatim — a standard fixed before the work started,
that the worker was actually held to. happy-dom s1's second gate is the same
shape. This is the two-components-two-contracts defect FAILSAFE already named
once, in the row about the mechanical gate and the citation invariant.

### 1c. Neither door could see what the plan promised

`MissingProduces` (`internal/revision/produces.go:48`) emits its citations FROM
`plan.Done.Produces`, and `Evidence.Done` already carries that structure to the
judge — but no grounding rule reads it. A finding that names an output the
compiled plan promised, in the plan's own words, is grounded by construction, and
was being refused for not appearing in the request's prose.

### The law

> **A finding is grounded when everything it points at is something this run
> promised before it began working.**

Three things were promised before the work started and cannot have moved in
response to it: the person's request, the working method the leaf was held to,
and the compiled plan's own promises (its aim, its `Produces`, its
`Conditions`). Those three are now one value, `revision.Grounds`, and BOTH doors
weigh a finding against all three. One contract, one reader.

A citation is grounded three ways, each on its own terms:

1. **As a quotation.** It is split at its elisions and every segment must be a
   verbatim span of some ground. An elision is a structural mark — three or more
   dots, or `…` — and not a word, so this stays a structural test.
2. **As a file name.** Unchanged: the same file under either spelling, by
   `namesSameFile`.
3. **As a named thing.** This is the entailment door, and it is what admits
   "does not implement X" where X is what the request asks for. A citation that
   is neither a quotation nor a file name is grounded when it names at least one
   SYMBOL that a ground also names and names no symbol that no ground names.

A symbol is recognised by shape, the way `namedFile` and `enumerationItem`
already are in this package: a token carrying an internal `_`, `.`, `/` or `-`,
an internal capital, or a digit beside letters — `dataset.features`,
`is_following_end`, `IntersectionObserver`, `gridTemplateColumns`, `HTTP`. A
plain English word is deliberately NOT a symbol, and that is the line that keeps
self-authored scope out: the measured failure this whole invariant exists for was
a gate holding a worker to "March refers to any calendar year present in the
data", where "March" is the person's word and "calendar year present in the data"
is the run's own. Under the shape rule that citation names no symbol, so the
entailment door does not open for it and it is refused exactly as before.

**What is still refused.** Prose that quotes nothing, names no file, and names no
symbol any ground names. That is the whole of what self-authored scope looks
like, and it is what the limiter was always for. FAILSAFE clause 5 is explicit
that the limiter's job is to stop rounds that invent scope, not rounds that close
promised scope; before this change it could not tell the two apart, because it
was asking about characters rather than about promises.

## 2. A refused finding was settling the run whole

`cmd/aforge/do.go:1483`, `deliveredWhole`, is where the exit code is decided:

```go
!gate.Pass && !gate.PolishClosed &&
    (gate.Mechanical || gate.Unclosed || strings.TrimSpace(gate.Refused) == "")
```

Read it in the other direction: **any refusal at all, unless it was mechanical or
unclosed, makes the run whole.** `store.DeliveryGate.Refused` is one field
holding refusals of two categorically different kinds, and the exit code cannot
tell them apart:

| refusal | what it checked | what it proves |
| --- | --- | --- |
| `AdmitGapArtifact` — "already on disk under the name the request used" | the filesystem | the finding is WRONG. The thing is there. |
| `AdmitGapPresent` — "already in the delivered text" | the deliverable | the finding is WRONG. The thing is there. |
| `admitGapCitations` — "not in the request" | the finding's provenance | only that no round will be BOUGHT |
| the spent ledger — "the same words were already worked on once" | the ledger | only that no round will be bought |

The first two are FAILSAFE clause 2 in good working order: evidence sourced from
the world, and it overturns the finding. The last two check nothing about the
world at all. Refusing a citation on provenance declines to spend money; it does
not make the missing work appear. The run is still handing over less than it
promised, and it was handing it over as success.

> **A refusal acquits only when it was checked against the world. A finding
> refused on provenance, or left unclosed, still stands, and a run that ends with
> a finding standing is partial.**

`store.DeliveryGate` gains one field, `Overturned`, recorded rather than inferred
for the reason `Unclosed` was: it is the field the exit code turns on, and a
sentence is not a field. `deliveredWhole` now reads: a failing gate delivers
whole only when the polish closed it or the finding was overturned against the
world.

And the stream has to say which finding. `gateWords` (`do.go:1174`) printed the
refusal sentence and nothing else, so a person watching read
"refused — what the review asked for next is not in the request" and never
learned what the review had said was missing. It now names the finding first and
the reason second, because the finding is the news.

## 3. Ten minutes of ninety, by choice

Nothing ran out. The wall is 5400 seconds; the runs stopped at 220, 246, 580,
591, 622, 837, 1528 and 1659 seconds, having spent between five and twenty cents
of an unbounded allowance. **Every one of them chose to stop.** Raising the wall
buys nothing; raising a retry count buys a retry of the same blind decision. That
is the defect stated plainly: *a run with 86 minutes and 99% of its money left,
holding a finding it agrees with, stopped.*

Two things made stopping the default.

The first is grounding, above: an unadmitted finding never reaches
`ReplanOverrunAs`, so the growth governor — the one component in this system that
decides from measured evidence — was never asked.

The second is the spent-citation ledger (`AdmitGapCitation`, `judge.go:1125`),
which is a count of one wearing the clothes of an invariant: words that bought a
round may never buy another, whatever that round did. That is the same species of
mistake as a retry count. The settle lane already built the thing that decides
this properly — `store.JobGrowth` records, per round, how many files the round
actually left behind (`Produced`, `Measured`) and a digest of the remainder it
was aimed at (`Remainder`) — and `growJob` (`internal/resident/grow.go:394`)
already refuses a lineage that has changed nothing twice (standstill) or been
handed the same remainder twice (fixed point).

> **The ledger bounds SCOPE; the growth journal bounds REPETITION.** A citation
> already worked on may buy one further round when the journal shows the round it
> bought actually moved the tree. When the journal shows it moved nothing, the
> veto stands, and when there is no journal to read the veto stands — the
> fail-safe direction for a bound on new work.

Convergence survives, and is now stronger than it was: standstill stops a lineage
after two fruitless rounds, the fixed point stops a gate that names the same gap
twice, `MaxOverrunRounds` remains the backstop it says it is, and the daily rail
still pauses for consent. None of those is a clock and none of them is new here.

One clock does enter, and only as a floor. `ExtendForGap` now asks whether the
run's own deadline still holds room for a leaf to run; if it does not, the gap is
refused as **unclosed** — which under §2 is exit 2, partial. Buying a round that
the wall will kill mid-flight spends money to deliver nothing, and pretending a
run that had no time is a run that was whole is the same lie in a different
field. The floor is derived, not typed: see PERF.md.

## 4. Regression is a blocker, and the evidence must come from the world

igel s2 and both textual runs shipped a patch that deleted an attribute the
repository already had:

```
AttributeError: <class 'igel.igel.Igel'> has no attribute 'results_path'
RichLog object has no attribute '_size_known'
```

Every one of the twenty-four hidden fail-to-pass tests failed on setup. The
leaf's own narrow tests were green — it wrote them, and they do not touch
`results_path` — so from inside the run there was no signal at all. igel s2 ran
the tests thirty-three times, three times as often as s1, and scored 5 → 0. Test
COUNT correlates with nothing. What none of the ten runs did was run the
project's own suite before and after and treat a new failure as a blocker.

The machinery for this exists and is unreachable. `fullverification.Discover`
found the project's own build and test entrypoints from `package.json` scripts,
`Makefile` targets, CI workflow files, `go.mod`, `Cargo.toml`, `pyproject`;
`codeaf`'s baseline photographed them before the work and named the tests that
had gone from green to red. Both lived under `internal/swepro/internal/`, which
by Go's own rule no worker outside `internal/swepro` may import, so the bare and
linear workers — the only workers `aforge do` uses on the plain path — could not
have it. `internal/exec/bare/bare.go:67` never sets `Outcome.Baseline`, and an
empty `Baseline` reads downstream as *no claim*, not as *nobody looked*.

> **A check that passed before the work and fails after it is a finding the gate
> raises itself, and it is admitted without any citation.**

The law moves to `internal/verify`, one package, reachable by everyone: discover
the project's own entrypoints, run one, read the failing test names out of its
output, and name the ones that are new. `internal/swepro/codeaf` keeps its
pipeline and now reads the law from there rather than owning a second copy of it.
The bare worker photographs the project's own verification before its first turn
and again after its last, and carries what it finds on the outcome.

Grounding does not apply to this finding and must not. A regression is not a
reading of the request — it is a measurement of the world, which is FAILSAFE
clause 2 exactly, and there is no citation to weigh because the person never had
to ask for their repository to keep working. `Judgment.Sourced` says so, the
admission rules step aside for it, and `deliveredWhole` reads it the way it reads
a mechanical gap: no refusal of a citation can make a red test green.

**The cost is bounded, and derived.** A photograph is taken only when the project
declares a verification entrypoint at all, only when the tree actually changed
between the two readings, and each run is capped at a share of the leaf's own
wall — a leaf too short to afford the measurement does not take it, rather than
spending its whole life measuring. The share and its arithmetic are in PERF.md,
which is where every cap in this repository is stated once.

## What is deliberately not here

No new retry counts. No new timeouts as levers — the one clock added is a floor
that refuses to buy work the wall cannot hold, and it makes runs longer, not
shorter. No new models and no new calls on the common path: the grounding work is
string shape, the settlement work is a field, and the regression work is the
project's own command, which the run was already running by hand nine times an
hour without ever comparing two readings of it.
