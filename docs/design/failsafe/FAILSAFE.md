# Fail-safes that close the loop

*Written 2026-08-28 after one day of headless runs surfaced five failures. Every
one of them hit a fail-safe that already existed. This document is about why the
fail-safes did not save the run, and the one rule that would have.*

## What happened, and what was supposed to catch it

| failure | the fail-safe that existed | why it did not save the run |
| --- | --- | --- |
| GLM's thinking pass ate the whole `max_tokens` ceiling; the planner got an empty answer | the reflex tier had a 10× ceiling | the fix lived in one caller, not at the seam every request passes (fixed: `2a998408`, headroom in `encodeRequest`) |
| `max_price` admitted only the first-party endpoint, which the account's privacy setting excludes; three identical 404s, dead node | the relaxation ladder, whose FIRST rung drops `max_price` | the ladder's detector is a phrase allowlist, and the router used a sentence not on it (fixed: `3abf6dbb`, phrases + per-model memo — but see rule 1 below for the real fix) |
| the compaction rebuild read index 0 of a slice it had just emptied | `guard` caught the panic; the scheduler escalated the leaf from `bare` to `swe` two seconds later | the escalation, the fault, and the SWE baseline's seven-minute `go test` were all invisible in the headless stream, which said only `still waiting: 1 running`. The operator read it as a hang and killed it (fixed: `c32dcfe8` for the crash; visibility is rule 3) |
| the delivery gate failed a leaf that had written nothing; the repair round was refused; the node was delivered as done, exit 0 | the gate, the repair round, and the citation invariant that stops runaway self-authored rounds | the mechanical gate emits a comma list of PLAN-resolved paths and the invariant demands ONE verbatim span of the USER's text: two components, two contracts. And the anti-runaway limiter has no floor — it can refuse the one round a run with zero artifacts obviously needs (lane `ui/lane-gate`) |
| the audit said "nothing named out.txt was left behind" while the file sat on disk | the gate reads "what the run left behind" | that record is TOOL-sourced (`workspace.artifacts`, filled by the write tool) and the model wrote with a shell command. The evidence was narrower than the world |

## The rule

> **A fail-safe is closed-loop or it is decoration.** It detects by STRUCTURE,
> not by vocabulary. It sources its evidence from the WORLD, not from the
> component it is checking. It PROPAGATES to the verdict the person reads.
> It leaves a RECORD that can be autopsied. And it has a FLOOR that cannot
> deliver nothing as done.

Each of the five failures broke exactly one clause.

### 1. Detect by structure, not vocabulary

`endpointRefusalPhrases` is a list of sentences the router has been seen to say.
It will always be one sentence behind. The structural fact is available without
reading a word: **a 404 or 400 from the router's own JSON error envelope, on a
model the catalog knows, is a routing refusal** — a wrong base URL does not answer
in the router's envelope, and a model the catalog does not know is a different
error the caller must see. The phrase list survives only as a hint for the
message a person reads. *(lane `ui/lane-refusal`)*

### 2. Source evidence from the world

"What the run left behind" is answered by the filesystem, not by which tool was
used to write. The workspace snapshots the tree before a leaf runs and diffs it
after: every file created, changed or deleted is an artifact, whatever wrote it.
The tool-sourced list is kept as the *deliverable* flag (the worker's own claim
of what matters) layered over the diff, never instead of it. *(lane `ui/lane-evidence`)*

### 3. Propagate to the verdict

A caught fault, a worker escalation, a subharness phase change, a refused gap —
each is a fact about the run that changes what the person should expect. In the
headless stream every one of them is a line, in the same register as `▶` and
`✓`: `✗ bare: runtime error … → escalated to swe`, `swe: baseline (go test, may
take minutes)`, `gate: refused — not in the request`. `still waiting` is what is
printed when nothing is known, and after this it is rarely true that nothing is
known. *(lane `ui/lane-evidence`)*

### 4. Leave a record

A leaf's transcript — every assistant turn, tool call and tool result, bounded —
is persisted under its node in the store, and flushed before the node settles and
on fault. A $0.50 run that cannot be autopsied is a run nobody can learn from.
*(lane `ui/lane-leaflog`)*

### 5. A floor under the limiter

The citation invariant exists to stop self-authored rounds from running forever.
It must not also stop the one round a run with a **mechanical** gap — files the
plan promised and the disk does not hold — plainly needs. A refused mechanical gap
is not "the gate being wrong"; it is a fact, and it is delivered as *partial*
(exit 2), never whole. And a gap is a list of citations, each grounded on its own
terms — a verbatim span, or a file the person named by any spelling — so the
mechanical gate and the invariant finally speak one contract. *(lane `ui/lane-gate`)*

## What is deliberately not here

No new retry counts, no new timeouts, no new models. Every failure above already
had its retry; what it lacked was a detector that could see the failure, evidence
that matched the world, or a line that told the person. Adding a sixth retry to a
blind fail-safe buys a sixth blind retry.

---

## A sixth failure, 2026-08-29: the reading that was never there

*Added against `bench/deepswe/results/textual-richlog-follow-state-…-s6/`, the
first graded run on the wave that made the verification photograph work.*

The run scored 18 of 20 hidden fail-to-pass tests and its store holds **no
verification event at all**. No roster, no before, no after, no regression
finding. From outside it is indistinguishable from a project that declares no
way of checking itself.

What actually happened is in the usage timestamps. `bare` took the node over at
07:13:47 and made its first model call at 07:19:14 — **five minutes and
twenty-seven seconds** with no call, which is the reading running and being
killed at its ceiling. The command it ran was `python3 -m pytest -rA` over
textual's whole repository: 3,422 collected tests, measured at 793s in that same
image — well over twice the budget it was given.

Two defects, one clause each.

**Clause 1, detect by structure.** textual's Makefile says `run := poetry run`
and then `$(run) pytest tests/ -n 16 --dist=loadgroup $(ARGS)`. The reader took
`$(run)` for the command's name, found no runner in the recipe, and fell through
to a whole-repository invocation. A make variable and an environment launcher are
both STRUCTURE — one is the file's own assignment table, the other is a program
whose entire job is to run another program — and a reader that cannot see past
either of them cannot see any recipe a real project writes.

**Clause 4, leave a record.** Four different things return no reading: a project
that declares no verification, a wall too short to afford one, a shell the
preamble cannot be trusted in, and a command killed at its ceiling. They cost a
run nothing, nothing, nothing and an eighth of its wall. All four returned the
same zero value, silently, so the only thing an autopsy could read was an absence
that meant four things at once.

> **A MEASUREMENT THAT WAS NOT TAKEN IS A FACT ABOUT THE RUN, AND IT IS WRITTEN
> DOWN WITH ITS REASON AND ITS PRICE.** An absence in the record is never a
> diagnosis; it is the four diagnoses nobody can now tell apart.

`verify.Reading.Unread` carries the sentence, `store.EventVerification` carries
the row with `read: false`, and the reason is remembered against the job so the
next round does not spend the wall discovering it again.

---

## A seventh failure, 2026-08-29: the reading of the wrong thing, and the finding nobody heard

*Added against the s6 and s7 stores under `bench/deepswe/results/`. Four defects,
four clauses, and every one of them a mechanism that existed and did not fire.*

**Clause 1, detect by structure — the runner lives in a package.** happy-dom's
root `npm test` is `turbo run test`, a fan-out whose whole job is to run each
package's own command. Measured in its task image at its base commit: it exits 1
in 5.3 seconds with 0 of 4 tasks successful and names no check of any package;
`npx vitest run --reporter=json` at the root is killed at a 180-second ceiling
naming nothing; and the same runner inside `packages/happy-dom`, handed the test
file next to the change, exits 0 and names 173 checks. A WORKSPACE'S PACKAGES ARE
DECLARED — `workspaces`, `pnpm-workspace.yaml`, `lerna.json`, `[workspace]
members`, `go.work`, or a manifest per package under a fan-out tool — and a path
belongs to the nearest manifest above it. `verify.Members`, `verify.MemberFor`.

**A budget cannot rescue a measurement of the wrong size.** textual's
whole-repository pytest is 3,422 tests and 793 seconds against a 5m30s budget, so
the only rung the ladder had was one that could never finish. The repair is not a
bigger ceiling: A READING IS SCOPED BEFORE IT IS BOUNDED. The checks adjacent to
the change come first — the test files the work is in, beside, or named by — and
the whole suite is what is below them. `verify.Adjacent`, and the scope rides on
the strategy so two readings of different scopes are never subtracted from each
other.

**Clause 3, propagate to the verdict — the finding was a paragraph.** igel s6's
gate event held `exercises: 17 rows, 3 unmapped`, and its coverage gap survived
only as text glued into the middle of the judge's own `gap` string. Prose glued
onto a gap is invisible three ways: nothing journals it as a finding, the stream
prints `firstLine(gap)` and never reaches it, and the round it rides on is bought
on the judge's citation — so a refusal of THAT citation takes the measurement
down with it. It is now a list (`Judgment.Unexercised`, `store.DeliveryGate.
Unexercised`), one narrated line, and a finding that buys its own round when the
judge's words are refused (`Judgment.measuredHalf`).

**Clause 2, source evidence from the world — "not produced" of a file on disk.**
igel s6's gate said `feature_schema.joblib` "was not produced" while
`model_results/feature_schema.joblib` sat on disk and in the graded patch. The
record it read is the artifact REGISTRY, which is a report of what leaves
claimed; what a run left behind is answered by the filesystem. Every name the
request or the plan asks about is now settled against one bounded walk of the
workspace before anything reads the record, and a file matching the named
basename-and-suffix anywhere under it is produced, quoted at its fuller path.
Binary files are deliverables like any other. `Evidence.completeAgainstTheWorld`.

**Clause 5, a floor that cannot deliver nothing as done.** ink s7 journaled its
cut `npx ava --tap` correctly — killed at its ceiling of 1m53s — and then passed
the round-two gate over a tree with no roster at all and left with exit 0 at 13
of 25 hidden checks. A PASS OVER A SUITE NOBODY COULD READ IS NOT A PASS OVER A
CHECKED DELIVERY: it settles partial, exit 2, with the reason on the last line.
A project that declares no verification at all is not charged for it — that
question is unanswerable rather than unanswered — and which of the two it was is
journaled (`store.DeliveryGate.Unreadable`).

**And clause 4 once more, from the other side.** A cut reading used to return
nothing whatever. What a runner named before its ceiling fired is a real roster
of everything it reached; it answers "does a check for this exist" and it may
never answer "did this work break something". It is kept as
`verify.Reading.Partial`, with `CutAfter` beside it — the only thing a run ever
learns about the pace of the machine it is on, which matters because these
readings are taken in amd64 containers under qemu where everything is five to ten
times slower than the wall-derived arithmetic assumes.
