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

---

# The s5 sweep: one record, one verdict, and what may overturn a finding

*Appended 2026-08-29 against `bench/deepswe/results/*-s5/` — the first sweep run
on a binary carrying §1–§4 above. Five tasks, one seed. igel ended exit 2 with
its finding named, which is what §2 was built to do. The other four did not, and
the three defects below are why.*

## 5. The record the gate reads is the job's, not the node's

`cmd/aforge/chat.go`, `gateEvidence`. The gate's `Evidence.Artifacts` was the
artifacts of THE ONE LEAF being judged — `outcome.Artifacts` joined onto the job
directory. The settlement narrates something else: `cmd/aforge/do.go`,
`w.produced.list()`, the errand's own registry, which every leaf in the job feeds
as it lands and which is filtered against the disk when it is read.

Two records, and a repair round is exactly where they diverge. A repair is a new
node; it writes nothing new, because the work already landed under its parent.
So the gate that judges it is handed an EMPTY record and told the run was
observed from beginning to end, and `Evidence.namedBlock` prints, of a file that
is on disk:

> `examples/rich_log_follow_state.py — nothing of that name is among what was left behind`

textual s5, `task-2-x3`, is that sentence coming back out of the judge as the
finding it refused the delivery on — while the run's own `--json` artifact list,
printed forty lines later out of the OTHER record, names
`/app/examples/rich_log_follow_state.py`. igel s5 `task-2-x1` is the same shape
("the record shows that the named files were not produced"). A gate whose record
is narrower than the run's is FAILSAFE clause 2 again, one seam along from where
that clause was first written: the evidence was narrower than the world.

> **WHAT THE RUN LEFT BEHIND IS ONE RECORD, HELD BY THE JOB, AND THE GATE READS
> THE SAME ONE THE SETTLEMENT NARRATES.**

`brainOptions.produced` was a write-only sink — the wiring could tell the
registry about a file and could never ask it anything — which is the whole reason
two records existed. It becomes a record with both halves, `add` and `list`, and
`gateEvidence` is handed the job's list unioned with the leaf's own. Path
identity is `namedAs`, which already answers a relative spelling against an
absolute path by suffix, so a deliverable that writes `examples/rich_log_follow_state.py`
and a record that holds `/app/examples/rich_log_follow_state.py` are one file and
never a contradiction. Nothing here is compared by string equality on whatever
spelling the deliverable happened to use.

## 6. Prose in the deliverable is not the world

`internal/revision/judge.go`, `AdmitGapPresent`. It reads an ENUMERATION out of
the citation — three or more items, by shape — and acquits the delivery when
every one of those items appears somewhere in the delivered text, case-folded.
`cmd/aforge/chat.go` then sets `store.DeliveryGate.Overturned`, whose own comment
says the field means the refusal was CHECKED AGAINST THE WORLD.

It was not. The citation is a span of the REQUEST; the text it is checked against
is the deliverable's own account of itself. A worker that restates the request
back in the request's words satisfies it by writing prose, which is what textual
s5 did:

> `gate: refused — The deliverable reports that examples/rich_log_follow_state.py
> exists and is committed, but the run record shows nothing of that name was
> produced … — everything it names is already in the delivered text, in the words
> the request used`

The finding was true, the acquittal was decided by the deliverable, and the run
left with exit 0 at 1 of 20 hidden checks. This is FAILSAFE clause 2 broken in
the strict sense the clause states it: the evidence was sourced FROM THE
COMPONENT BEING CHECKED.

> **A FINDING IS OVERTURNED ONLY BY THE WORLD — a file on disk, a check green in
> a reading, an artifact in the record. The delivered text can overturn a finding
> only when the delivered text IS the whole of what the run left behind.**

That last clause is not a softening; it is the same rule. A question answered in
prose produces nothing but the message, so the message is the artifact and a
citation settled against it is settled against everything the run made. That is
the measured case this door was built for — twelve profiles asked for by name,
twelve profiles in the message, and a gate reporting the twelve were absent — and
it is preserved exactly. The moment the run left a file behind, the record is the
world and the text is a claim about it, so the text settles nothing:
`AdmitGapPresent` is asked for the record and returns no acquittal when the
record holds anything.

The anti-runaway half is untouched. A finding must still be grounded in the
request, the working method or the plan's own promises (`Grounds`,
`citationGrounded`), and a provenance refusal still leaves the finding STANDING
and the run partial, exactly as §2 built it. Refusing to buy a round and
acquitting the work were never the same act; §2 separated them and this keeps
them separate at the other door.

## 7. One reading of "whole", and the person reads it

`cmd/aforge/do.go`. `deliveredWhole` computed the settled verdict as
`Pass || PolishClosed || Overturned`; `gateWords`, forty lines away, computed the
line the person watching reads from `Pass` and `Refused` alone. Two readers, two
contracts, and on three of five s5 runs they disagreed out loud: ink and ofetch
printed

```
gate: fail — The deliverable is a listing of files, not the answer itself …
```

as the last thing the person saw, and left with exit 0. The event carries both a
first verdict and a repair's, and `gateWords` was only ever shown the first.

> **THE SETTLED VERDICT IS ONE FIELD-READING, AND EVERY READER USES IT.**

`store.DeliveryGate.Whole` is that reading, on the event, beside the fields it
combines. `deliveredWhole` is `!gate.Whole()` and `gateWords` says `pass` for a
gate a repair closed. What `PolishClosed` means is unchanged and is worth stating
plainly, because it is the one branch here that is not a defect: it is set only
from a SECOND full `JudgeDeliverable` that answered pass, and that judgement
re-runs the whole world half first — regressions, vanished checks, promised files
against the disk, and its own reading of the final tree. A repair that closed the
gap is a world-checked pass and settles whole. What was wrong was the sentence
the person read on the way out, not the code.

And the run says why it was short, once, last, in the register the stream already
uses:

```
partial — gate: <the finding> (not repaired: <the reason nothing more ran>)
```

FAILSAFE clause 3: a fail-safe that does not reach the person watching is
decoration, and an exit code nobody sees is the quietest decoration there is.

## 8. A repair that moved nothing may not close a finding about the world

§7 left `PolishClosed` alone on the grounds that it is set only from a second
full `JudgeDeliverable` that answered pass, and that judgement re-runs the whole
world half first. Both halves of that are true and the conclusion was still
wrong, because of what the repair in front of it had been.

A failed gate buys one repair and there are two kinds. One re-runs the worker: it
can edit files, run checks, and change what the world says. The other —
`revision.Compose`, taken whenever `Composable` holds, which is every change
worker whose change already landed — rewrites the ACCOUNT of the work and runs
nothing at all. Its own comment says so: *"the outcome is kept, not replaced …
the composition did none of it; only the words are new."*

So the second gate reads a better-written summary of the identical world the
first gate failed. ink s5 and ofetch s5 are both that:

| run | what the first reading said was missing | what the repair did | settled |
| --- | --- | --- | --- |
| ink s5 `task-2` | "the substance of the work: the implemented code, the branch created, and the confirmation that it compiles" | composed a summary | whole, 7 of 25 |
| ofetch s5 `task-2-x1` | "the test results and the confirmation that `pnpm test` exits 0" | composed a summary | whole, 44 of 47 |

ofetch's own FIRST gate had already journaled what the world said —
"the project's own verification (`pnpm test`) exited 1 and named 0 checks" — and
a sentence cannot make a suite exit 0.

> **A REPAIR THAT PROVABLY DID NOT MOVE THE TREE MAY NOT CLOSE A FINDING WHOSE
> GROUND IS THE TREE OR A READING. It may still close a finding whose only ground
> is the delivered text, because writing is what closes that one.**

Both halves are measured, not asserted.

**Did the tree move?** `revision.TreeStamp` over the job's artifact record —
path, size, modification time, folded to a digest — taken as the finding is
raised and again at the re-judgement. Two equal stamps are a round that changed
nothing, which a composition is by construction and an engine re-run that edited
no file is in fact. It is size-and-mtime rather than content because that is the
question every build system in existence answers this way, and content-hashing a
repository's build output would buy the same answer for megabytes. The fact is
journaled as `store.DeliveryGate.Unmoved`, for the reason `Unclosed` and
`Overturned` are journaled: a settlement that turns on it cannot read it out of a
sentence.

**Is the finding's ground the world?** `revision.GroundedInTheWorld`, four doors,
every one of them reading the record rather than a list of words:

1. The gate already settled it against the world — `Mechanical` (the disk) or
   `Sourced` (a measurement). Neither has an opinion in it.
2. It names a file, by `NamedFiles`' own shape rule.
3. It names a command the RECORD says the run ran, or the verification
   entrypoint. Matched as whole strings, not as symbols, and deliberately: a
   command is usually plain words with a space in it — `pnpm test`, `make check`,
   `go test ./...` — which `symbolShaped` rightly refuses to call a
   distinctively-spelled name. Nothing is inferred from prose, because the string
   being matched is one the record supplied. This is the door ofetch trips.
4. **The run left a tree behind and nothing says the tree is good.** This is the
   door that carries the weight and it is written on the fail-safe side. A run
   that produced files was doing work, not writing an answer; a finding against
   it is a finding about that work unless the world says otherwise, and the only
   thing that can say otherwise is the project's own checks, run on the tree
   being handed over, coming back green. A red reading is the world convicting;
   an ABSENT reading is nobody having looked, and NOBODY LOOKED IS NOT NOTHING
   WRONG — the same rule `DeliveryGate.Unmeasured` already writes down one seam
   along. This is the door ink trips.

What stays false — a finding a rewritten account may honestly close — is
therefore the two cases where writing IS the work. A run that left nothing but
its message: the message is the artifact, which is §6's line reused rather than a
second rule, and the twelve-profiles case goes on closing exactly as it did. And
a run whose change the project's own checks PASS, where what is wrong is the
account of it: happy-dom s4's "the deliverable is a list of file paths, not the
implementation itself", over a green suite, is precisely what `Compose` was built
for and it still closes.

The rule is one function, `revision.RepairClosed`, because it had been an
expression at a wiring seam where it was not visible enough to be got wrong.
A repair whose pass does not count falls through to `ExtendForGap` exactly as a
failed one does — so the run tries to BUY the work that closes the finding rather
than settling — and if nothing can be bought it ends partial, with the reason
named on the last line: `(not repaired: the repair rewrote the account and
changed nothing on disk)`.

## What is deliberately not here

No new judge call, no new clock, no new model round: the stamp is a `stat` per
recorded path and the classification is string shape over strings the record
already holds. No knowledge anywhere of what a test framework is called. And no
rule that a composed repair is worthless — it improves the account that ships in
every case, and it still closes the gate in the two where the account was the
only thing wrong.

## Addendum, 2026-08-29: the photograph had no memory

§4 above built a photograph with a before half and an after half, and the bare
worker took both. What it could not do is see across a repair round.

Every round is a new leaf holding a new workspace object, so every round
photographed the tree IT found — which, from the second round on, is a tree the
job has already changed. A check the first round turned red is red in the second
round's baseline, subtracts to nothing, and is never a finding again. textual s5
walked twenty of the repository's own checks down to one across four rounds and
raised no regression at any of them.

> **THE BASELINE IS THE TREE BEFORE THE JOB'S FIRST CHANGE, TAKEN ONCE AND
> INHERITED BY EVERY CONTINUATION.** Every round's after reading is subtracted
> from that one.

`verify.BaselineFor` / `verify.RememberBaseline`. It is remembered against the
tree's own path, because that is what the baseline is a reading of, and against
the job — a digest of the person's own request, the one thing every leaf of a job
holds identically and no two jobs share — because a second job in the same
directory is measuring different work, and the first job's changes are its
world. A job arriving at a root somebody else's job baselined re-baselines it.

Two consequences beyond the finding. The second reading is now owed whenever the
JOB has moved the tree, not only when this leaf did — a continuation that only
rewrote its account still hands over a tree an earlier round may have broken. And
a repair round runs the suite once rather than twice, because the baseline it
would have spent an eighth of its wall on is one it already has.

And the readings are journaled (`store.EventVerification`). Not one of the five
s5 stores holds a row saying a reading had happened, so a project that declares
no verification and a reading that ran and named nothing were the same silence in
the journal — the two opposite diagnoses. FAILSAFE clause 4, which this document
quoted and did not obey.

## Addendum, 2026-08-29: §6 asked one step earlier

§6 settled that the delivered text may overturn a finding ONLY where the text is
the whole of what the run left behind. It left the same question unasked one seam
earlier — what the judge is HANDED as the deliverable in the first place — and
the answer there was still "the worker's message, always".

textual n1 on nemotron is what that cost: three gates in a row refused a job for
what the message was (`the fenced text … contains only {"contract": "..."}`)
while 42KB of changed Python sat in the worktree and the artifact record named
every file of it. The judge convicted a sentence and never saw the tree.

> **THE DELIVERABLE OF A REQUEST THAT CHANGED THE TREE IS THE TREE. The worker's
> final message is its CLAIM about that change, and a claim is read beside the
> thing it is about, never in place of it.**

`revision.Evidence.Subject` decides it from the artifact record settled against
the disk — the same record `completeAgainstTheWorld` already produces — so the
rule is §6's rule with §6's evidence, and a run that left nothing behind is
judged on its message exactly as it always was. The fence then carries the
changed sources and checks with bounded excerpts, the message sits below it under
a heading that names it a claim, and a refusal must name one file of the record
and quote the behaviour it fails or it is not a verdict this gate can read.
FAILSAFE.md's seventeenth chapter has the whole of it, including why the second
half is a schema and not a sentence.

## Addendum, 2026-08-29: the third reading of the same tree

§4 settled that a regression is a blocker and that its evidence comes from the
world. The addendum above it added the half a suite cannot make — a public name
that was there before the work and is not there now. Both compare a repository
to itself and both are answered by a difference: a check that flipped, a name
that vanished.

igel s12 is the case neither of them can reach. The run rebound `configs` from a
dict to an instance of a class it wrote; the suite stayed green, the names
compared eight and lost none, and the twenty-four hidden tests failed on
`'Configs' object does not support item assignment`. **A retained name whose
definition was rewritten is not a difference either reading can see.**

> **A NAME IS NOT A CONTRACT. A definition whose declaration this run's own diff
> rewrote, and the places the rest of the project still uses that name, are one
> fact and the judge is handed both.**

`verify.ChangedDefinitions` and `verify.Consumers`, read from the diff and the
disk with no model in the loop, sampled into the gate's prompt on a share of its
own so a consumer list can never be the reason a changed file went unprinted.
The verdict schema admits a refusal grounded on a consumer's own file and line
beside one grounded on a behaviour of the request — §6's rule is unchanged for
behaviours, and this is a second ground rather than a loosening of that one,
because no behaviour of any request will ever say "and it must still support
item assignment". Such a refusal is Sourced, buys the repair round every measured
finding buys, and is journaled as a finding of its own. FAILSAFE.md's twentieth
chapter has the whole of it, including what it does not promise.
