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
