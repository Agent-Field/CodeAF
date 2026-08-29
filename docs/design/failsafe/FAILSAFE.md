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

## A seventh failure, 2026-08-29: the lease that expired on a clock

*Added against `bench/deepswe/results/ink-grid-box-layout-…-s6/` and
`happy-dom-…-s6/`, the two runs that spent their whole ninety-minute wall.*

ink s6 spent **$0.850 and 38.76M prompt tokens** and scored 22 of 25. Its node
`task-2` started five times. The releases are a metronome:

```
07:08:03 node_started  task-2 token=1
07:30:13 node_released task-2 token=1 → re-claimed token=3, same second
07:52:23 node_released task-2 token=3 → re-claimed token=5, same second
08:14:34 node_released task-2 token=5 → re-claimed token=7, same second
08:36:40 node_released task-2 token=7 → re-claimed token=9, same second
```

**22m10s, 22m10s, 22m11s, 22m06s.** No `✗` line, no fault, no hung call: the
leaf had flushed a batch of its own recorded turns thirty seconds before each
one. It is not a turn or token ceiling either — attempt 1 reached turn 72 and
attempt 2 reached turn 138 inside the same 22m10s, with `leaf_mode
{"turns":200,"tokens":150000}` re-emitted unchanged. happy-dom s6 restarted on
the same cadence (22m11s, 21m59s).

### The arithmetic

Three numbers, none of them wrong on its own:

| | |
| --- | --- |
| `exec.SubharnessInfo.Deadline(150_000)` → the linear floor | **15m** |
| `cmd/aforge/chat.go`'s `watchdog := deadline + 2*time.Minute` | **17m** |
| `resident.claimReaperPad`, added by `Runner.RaiseStaleAge(watchdog)` | **+5m** |
| `store.ReleaseSilent`'s window, swept every 500ms | **= 22m** |

Plus up to `runnerQuietCeiling` of dispatch-loop latency: **22m10s**.

### Two defects, one clause each

**Clause 2, source evidence from the world.** The window was measured from
`nodes.started_at`, which is stamped once, when the claim is granted. So the
question the reaper actually asked was "how long has this worker been ALIVE" —
and one claim legitimately carries the executor's own deadline *and* the retry a
spent deadline earns, which is twice this window. The CAS inside `Release` was
believed to protect a live worker ("the token has moved and the release fails");
it does not, because a leaf does not touch its own token between turns. So the
node was re-claimed inside the same second while its first worker went on
writing to the same workspace.

> **A CLAIM IS HELD BY EVIDENCE OF LIFE, NOT BY A CLOCK.** A worker leaves
> durable marks as it works — a billed model call, a recorded turn — and the
> newest of those, floored at the claim's own start, is when the node was last
> known to be worked. The window bounds SILENCE. A leaf that keeps calling keeps
> its claim for as long as it keeps calling; a claim silent through the window is
> held by nobody, and the release says so in the journal.

And a second clause under the same heading, because the first one alone would
still have doubled the node. A release is a change to a row; the goroutine that
held the claim is not party to the transaction and does not notice. The ink
store shows the cost directly — the release at 07:30:13.856, the re-claim at
07:30:13.873, and thereafter two transcript streams under one node id, turns
1–25 of the new attempt flushed in between turns 45 and 67 of the old one. Two
workers, one checkout, each undoing the other's edits, both billed.

And a third clause, because the first one named only the marks the journal
holds. Every durable sign of life is written when something FINISHES — a usage
row when a call is billed, a transcript flush when sixty-four entries fill, and
that batching is deliberate: one write per tool result would put several hundred
rows under a node and turn the database into a log file. So a leaf spending
twenty minutes inside three long shell commands writes nothing at all, and a
sweep of the store cannot tell it from a corpse. The answer is not a flush timer
— a second clock answering a question the first clock is already wrong about,
paid for with a durable write on every leaf in the system to rescue the rare
quiet one.

> **A CALL IN FLIGHT IS A SIGN OF LIFE, AND IT IS A SPAN AND NOT A PING.** The
> worker asked the model, or started a command, and has not been answered yet;
> that fact is known in this process, for free, by the code that is waiting. It
> is reported through the context exactly as the transcript sink already is
> (`exec.Working`), and the listener is the scheduler, because the reaper is in
> the same process as the worker it would reap. "A tool call was issued" would
> keep a claim alive for one instant and go quiet again for the seven minutes the
> command actually runs, which is the case this exists for — so what is reported
> is the beginning and the end, and everything between them is a worker
> demonstrably waiting on something. A worker that has never marked anything is
> not alive by default: the journal is then the only account of it, which is the
> account the sweep already read.

> **A CLAIM IS NOT TAKEN FROM A WORKER, THE WORKER IS STOPPED.** Every leaf runs
> on a context this process can end. The reaper cancels; the node stays Running
> and unclaimable; the worker's OWN landing releases it, so the release happens
> strictly after the goroutine returned. `leaf_stopped` carries the token and is
> journaled immediately before that release, so any store can be checked for the
> ordering: where a `node_released` for a token is not preceded by a
> `leaf_stopped` for it, a worker was overtaken. The backstop under the backstop
> is `claimReaperPad` — a worker that ignores the cancellation for as long as a
> landing leaf is given is gone, and the claim is taken without it, journaled as
> exactly that.

**Clause 4, leave a record.** Four different endings arrived as the same silence:
a worker that hung, a worker whose deadline legitimately expired, a claim the
reaper took back, and a leaf handed its predecessor's work that started over
anyway. Exhaustion is not a restart — it is the growth governor's own input —
and it is now journaled with what ran out and how far it got
(`store.EventLeafExhausted`), the reaper's release carries its reason, and a
claim that picked up recorded work says how much (`store.EventLeafResumed`). All
three reach the headless stream.

### And the restart did not resume

`resident.BankedTranscript` had shipped the day before and the seed still went
out cold. Two reasons, both structural:

- **The seed was read across every attempt in the record at once.** A node's
  transcript is every attempt ever made under it, appended, and each attempt
  numbers its turns from one. "Every entry whose turn is above the last turn
  minus twelve" is therefore a window on nothing: on the third claim it composed
  turns 79–90 of one attempt interleaved with turns 34–45 of another. A run is
  now found by structure — the turn counter only rises inside one attempt, so
  where it goes backwards a new attempt began.
- **Twelve turns of 138 is a file dump, not a memory.** The attempt created
  `src/grid-layout.ts` on turn 25 and was interrupted on turn 45; the twelve-turn
  window could not see it. The seed now carries an **outline of every turn of the
  run** — what it said and what it ran, one line each — ahead of the verbatim
  tail, and the file list is the workspace's own before-and-after reading rather
  than the directory listing, which on a shared workspace is somebody else's
  repository.

### What the decomposed leaves cost

Recorded, not acted on. happy-dom s6's `task-2` decomposed into eight leaves
after its own restarts. **None of the eight ever restarted, and together they
cost $0.108** — an eighth of what one attempt at the monolithic leaf cost
($0.224 for 10.66M prompt tokens). Every one of them fit inside a single
deadline, so none of them met the reaper at all. Whether that is decomposition
paying for itself or simply small leaves being small is not settled here; it is
written down because the two runs that hit the wall are the two that never
decomposed early.
## An eighth failure, 2026-08-29: the reading of the wrong thing, and the finding nobody heard

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

---

## A ninth failure, 2026-08-29: adjacency by substring, and a focus that was empty

*Added against `bench/deepswe/results/textual-…-s8` and `…happy-dom-…-s8`, taken
on the wave that made readings scoped. Both are clause 1 — detect by STRUCTURE —
broken in the new mechanism itself.*

**textual s8: a substring is not a relationship.** The job touched `_log.py`,
`_rich_log.py`, `widget.py` and `messages.py`. The reader flattened every name to
its letters and asked whether a test file's TEXT contained one, so the stem `log`
matched `dialog`, `catalog`, `logic` and `logging` wherever they appeared. The
selection was **40 of 251 test files** — a third of the suite, spanning
tests/animations, command_palette, css, directory_tree, document, footer and
input — and the reading was killed at its ceiling of 1m53s naming nothing.

Adjacency is now two structural relationships, ranked:

1. the test file NAMED AFTER the touched file by the runner's own convention —
   `test_<stem>.py`, `<stem>.test.ts`, `<stem>_test.go` — the stem compared
   whole; plus the test files beside it;
2. the test files whose IMPORT STATEMENTS resolve to the touched module. For
   Python that includes the package entry point's own re-exports, because
   `from textual.widgets import RichLog` is an import of `_rich_log.py` and the
   `__init__.py` is the only thing that says so. For JavaScript a relative
   specifier is resolved against the importing file's own directory.

Only import lines are read; only whole identifiers match, so `Log` is not
`Logger` and `log` is not `dialog`. The structural answer for that same job is
three files. And **a selection larger than an eighth of the suite is not a
scope**: past that it is a sample of the same order as the whole thing, and it is
cut back to its rank-1 core.

**happy-dom s8: the focus was empty.** Both readings were taken at the repository
ROOT with `scope: whole` and both were killed at 1m53s. Nothing was wrong with
the workspace declaration (`workspaces: ["packages/*", …]` beside a turbo.json)
or with the nearest-manifest walk. The focus was built only from paths the
request SPELLS OUT, and that request spells none — it says "Implement
`observe()`, `unobserve()`, `disconnect()` and `takeRecords()`" and names
`IntersectionObserver`, four times, and no path at all. So no package was ever
touched as far as the reader knew and the ladder had only root rungs.

> **A REQUEST THAT NAMES A THING THIS REPOSITORY HAS A FILE FOR IS A REQUEST
> ABOUT THAT FILE.** `verify.NamedSubjects` reads the identifiers a request uses
> in the repository's own spelling, and `verify.Locate` resolves each of them,
> whole, against a file the workspace holds. A name that matches nothing costs
> nothing.

Measured in that task image on 2026-08-29: the root reading is killed at its
ceiling naming nothing; the reading this repair takes — vitest inside
`packages/happy-dom` over `test/intersection-observer/IntersectionObserver.test.ts`
— exits 0 and names 4 checks. The `package` the reading was taken in is on the
`verification` event whenever a member was read.

**And clause 4 again: a cut is not a settled refusal.** A scoped reading killed
at its ceiling is a fact about a size this program chose, not about the tree, so
it is the one remembered answer a later round does not inherit
(`verify.Reading.Retakeable`). What the cut measured is a CEILING on the per-file
cost and never a target — a reading killed over forty files "affords"
thirty-six by that arithmetic, which is the same reading again — so the retake is
the smaller of that ceiling and a halving, floored at the rank-1 core.
## A tenth failure, 2026-08-29: the room the leaf never had

*Added against `bench/deepswe/results/ink-grid-box-layout-…-s8` and
`textual-richlog-follow-state-…-s8`. Three defects, and every one of them is the
same shape: a bound that belonged to the whole was applied to a part, or a
record that belonged to the part was only ever written by the whole.*

ink s8 is one node, one attempt, seventeen minutes, and exit 1. Its stream reads:

```
16m44s still waiting: 0 tasks pending, 1 running · last call 16m44s ago
17m17s ⏳ the worker did not come back within 17m0s and was given up on
       — its work is recorded and the node goes back on the queue
17m17s ✗ CSS Grid layout support
```

then `leaf_exhausted`, then `node_failed` in the same second, then a deliverable
of `executor did not return within 17m0s; abandoned` — over a 26,248-byte patch,
109 recorded turns, 9 test invocations, and 73 minutes of unspent wall. Its
`cost.json` reads **$0.000228, one usage row, 4,563 prompt tokens** — the
planner's single call, and nothing else.

### 1. A tool outlived the leaf's room and took the leaf with it

The pi-ported belt's `bash` takes an optional timeout from the model and its
schema says "no default timeout", so a command the model did not think to bound
inherited the leaf's whole fifteen-minute envelope. When the envelope expired,
three things happened at once and all three were wrong: the command was killed
and reported a clean success (the cut was tested for `context.Canceled`, and a
deadline is `DeadlineExceeded`), the loop's next turn-boundary check found a dead
context and stopped — so the output never reached the model that asked for it —
and the watchdog two minutes above was already counting.

> **A TOOL CALL RUNS INSIDE THE ROOM THE LEAF HAS LEFT, LESS WHAT IT TAKES THAT
> LEAF TO LAND.** Both halves are read, not chosen. The room is the context's own
> deadline. The landing cost is the slowest model call this leaf has actually
> made plus the slowest transcript flush it has actually taken, because landing
> is exactly those two things happening once more — so a leaf on a slow machine
> measures a slow machine. A cut command returns `cut after 9m12s; output so
> far: …` with everything the accumulator held, and the leaf reads it, decides,
> and lands with words of its own.

A per-command timeout constant would have been the wrong repair twice over: it is
wrong on every machine it was not picked on (these readings are taken in amd64
containers under qemu), and "does this command fit a number" is not the question.
`internal/exec/bare/room.go`; PERF.md, *A tool call's room*.

The watchdog above it learned the same lesson the reaper learned in the seventh
failure. It fired on a flat timer and, when it fired, returned **without
cancelling the leaf** — so the abandoned goroutine went on spending and kept its
children. It now reads the same `exec.Working` spans the reaper reads
(`exec.AlsoWithLiveness` composes rather than displaces), re-arms over silence
that is actually silence, and when it does give up it STOPS the worker and takes
what the worker then lands.

### 2. An abandoned node went back on the queue and nobody claimed it

The sentence in the journal was a lie, and the code one function away was the
proof: `resident.Runner.runOne` answered every non-context error with
`store.Fail`, and `store.Ready` offers pending rows only. So the node was
terminal in the same second it was said to be requeued, the settlement watch saw
one terminal node, and the run left with exit 1 and no gate verdict at all.

> **AN ENDING THAT IS EXHAUSTION IS NOT A VERDICT ON THE WORK.** The claim goes
> back with its reason in the journal, the node is offered again, and the next
> claim RESUMES from the record the last one left. The exit belongs to the
> delivery gate; no abandoned node decides it.

It is gated on there being something to resume from, and that is what keeps it
from being an unbounded retry: a re-claim that reads an empty record is the same
cold start again, and an attempt that recorded not one turn before the clock
stopped it has told us the only thing it is going to.

The rule is one function, `exec.Requeued`, and BOTH schedulers ask it. The
resident's was fixed first and the one-shot `aforge run` scheduler went on
failing an abandoned node outright for a day afterwards — the same defect, one
package along, in code nobody had looked at because the sentence it printed was
the same. What is local to each caller is only what the RECORD is: the resident
reads the transcript bank, and the one-shot scheduler, which has none, reads the
node's own row and is bounded to a single requeue because a record that cannot
grow would send the node round forever.

And the requeue is said in the register it belongs to. The stream marked every
release that carried a reason with `✗`, so the ordinary end of a leaf that ran
out of its room was announced as a fault one line under the `⏳` that had just
said, correctly, that nothing had failed. The release now carries the count of
recorded turns it hands on — the same fact the next claim's resume seed is built
from — and a release that hands work on reads `↻ … picked up again from 45
recorded turns`. `✗` is kept for the release it was added for: a claim taken back
over a worker that never answered, which hands on nothing.

And the deadline now reaches the growth governor. `Outcome.Overran()` excludes
the clock on purpose — it answers "was this leaf too big for its TOKEN
envelope" — so reading it at the continuation site meant the one ending that most
needs more room got none. `leafRanOutOfRoom` is the predicate the record already
used for exactly this question, and it is now the one the replan reads.

### 3. Every continuation started cold

textual s8 journaled **six** exhaustions — "still working when it ran out of its
tokens — 25 turns in", at 5m28s, 6m34s, 11m4s, 18m8s — and not one `↻ … resumed`
line. The re-drive happened; the resume did not. `BankedRun` is read at claim
time from `node.Attempt > 0`, and a continuation is a DIFFERENT NODE ID
(`task-2` → `task-2-x1`), so that read could never fire for it. What the
continuation did get was `Growth.State` — the leaf's own summary of itself — and
an attempt stopped mid-turn has summarised almost nothing, because summarising is
what a leaf does when it is finishing.

> **WHATEVER CONTINUES THE WORK IS SEEDED FROM THE PREDECESSOR'S RECORD, AND
> SAYS SO.** The in-place retry, the requeue and the continuation now read one
> bank and render it under one set of headers, and the resumption is journaled
> against the node that is actually resuming (`store.EventLeafResumed` on the
> continuation's sink), so the stream says how much was picked up.

### 4. Usage was banked per landing, so an interrupted leaf's spend vanished

A leaf's spend reached the journal exactly once, out of `exec.Outcome.Usage`, on
the way out of the run. Every ending that returns no outcome therefore returned
no money: 109 billed calls, one usage row, $0.000228. The asymmetry is the tell —
the transcript had been hardened against precisely these endings a wave earlier,
with a flush on each side of the abandonment, and the money had no equivalent.

> **A BILLED RESPONSE WRITES ITS ROW WHEN IT ARRIVES.** The provider already
> knows what a call cost at the moment it decodes the answer, and its adapter is
> the one door every outbound call in the process passes through — it already
> writes a per-call row there for the call log. A turn roll-up stays, as a
> different KIND of record (`usage_turns`), and is no longer the only one.

The fix is not another flush on another ending; there is always one more ending.
`provider.WithBilling` and `cmd/aforge`'s `leafBanker`, with
`resident.ExecResult.SpendBanked` so the landing does not write the same money
twice. `cost.json` and the settlement's money line read the `usage` table, so
both now see an interrupted leaf.

---

## An eleventh failure, 2026-08-29: the belt that had none of it

*Added against `bench/deepswe/results/ink-grid-box-layout-…-s9`, the first run on
the wave that fixed the eighth. The banking worked — 57 usage rows, $0.0456,
every call durable. Everything else that wave built was inert, and for one
reason: it had been built on the wrong belt.*

ink s9 ran three leaves, exhausted all three, created its continuation, and
settled partial at 1 of 25 in 478 seconds. Its store holds **zero transcript
rows and zero verification events**, and its exhaustion lines name a bound that
never fired.

### The one root cause

`internal/exec/bare` is a *lighter* worker — the cheapest whole-taker for small
work. `internal/exec/linear` is the GENERALIST: what a node gets when nothing
routed it, which on an unrouted job is every node. Three separate lanes had put
their mechanism in `bare` and stopped:

| mechanism | writers in the whole tree, before this |
| --- | --- |
| `exec.TranscriptFrom` — a leaf's turns, under its node | one: `bare/loop.go:238` |
| `store.RecordVerification` — the project's own checks, read | one: `bare/verification.go:221` |

So the default worker recorded nothing and read nothing, and every mechanism
built on those two records was dead on the path that actually runs.

> **A MECHANISM THAT ONLY THE OPTIONAL WORKER HAS IS A MECHANISM THE RUN DOES
> NOT HAVE.** The question to ask of any fail-safe is not "is it wired" but
> "is it wired on the belt a node gets when nobody chose one".

The blast radius, all three of s9's symptoms from that one cause:

- **No resume line.** The continuation path was correct — `leafRanOutOfRoom`
  covers a token exhaustion, `ReplanOverrunAs` fired, `job_growth reason=overrun`
  is in the store. `resident.BankedRun` then read a transcript table with nothing
  in it, so `Growth.Resumed` was zero and no `leaf_resumed` row was written. The
  seed the eighth failure built was reaching for a record that the generalist had
  never written.
- **No verification.** Not "the exhausted landing skipped the photograph" — the
  generalist has never taken one, on any path. (Its *world* photograph is fine:
  `WatchTree`/`RecordChanges` are symmetric across all three belts and survive an
  exhausted landing. Only the check-reading was missing.)
- **The gate had nothing to weigh** and refused on the deliverable's own prose.

The repair is one implementation reachable by every belt: the transcript is
wired at the **flight recorder**, which every turn and every harness note of the
generalist already passes through, and the photograph is lifted out of `bare`
into `exec` so both belts call the same functions.

### And the bound that fired had no name

All three leaves journaled `leaf_exhausted bound=budget` — *"it was still working
when it ran out of its tokens — 17 turns in"* — against a `leaf_mode` advertising
`tokens: 150000`. No attempt reached 150,000. What landed every one of them was
`reuseCeiling`, at 240,000 prompt tokens sent, exactly four landing turns before
the number the record printed:

```
task-2 attempt 1   crossed 240,000 sent at turn 13, landed at 17
task-2 attempt 2   crossed 240,000 sent at turn 12, landed at 16
task-2-x1          crossed 240,000 sent at turn  9, landed at 13
```

Attempt one had spent 104,064 of its 150,000-token grant and 372,941 of its
450,000 raw ceiling. It was cut at turn 13 of a 200-turn grant with 31% of its
money unspent, and the record said it had run out of tokens.

> **A LEAF HAS FIVE CEILINGS AND THREE OF THEM SPOKE WITH ONE VOICE.** A bound
> that fires names itself, with its own two numbers and their unit, or the record
> is an absence that means five things at once — clause 4, again, and the first
> three readings of this store each blamed a different meter.

And the meter that fired should not have. Σ over turns of the prompt is
`turns × mean-context` wearing a token name: any transcript that only grows
re-sends its prefix every turn, so the sum climbs identically for a leaf doing
hard work and one circling. ink s9 stopped at a duplication factor of 8.9×; the
audited runaway the bound was written for ran 11.2×. **No detector lives in a gap
of 1.26×** — and the ceiling is stated against the *window* while it is consumed
against the *transcript*, so it punishes the leaf that carries less.

> **WHAT LANDS A LEAF IS WHAT ITS WORK COSTS.** The runaway both Σ-bounds were
> written for is the no-progress guard's case, and `noprogress.go` opens by
> saying that a magnitude bound cannot separate "many turns because the work is
> hard" from "many turns because it is stuck". Both numbers survive as pressure
> on the wrap-up warning, where firing early costs a sentence instead of a run.

This is the second time the same lesson has been learned here: `maxTurnBackstop`
was raised from 40 to 400 because forty "also stopped honest complex work". The
reuse ceiling was a turn bound in disguise, sitting three times tighter than the
forty that had already been rejected.

---

## A twelfth failure, 2026-08-29: what the job knew and never said

*Added against `bench/deepswe/results/textual-richlog-follow-state-…-s9`. Both
defects are the same shape as the ninth and neither is a belt: a fact the job had
measured, structured, and journaled, which then reached the worker through
somebody's prose — or not at all.*

### 1. A job's memory is a property of its lineage, not of a node id

`task-2` ran 350 recorded rows over 80 turns and exhausted on its budget. Thirteen
minutes later a growth round on `reason: gap` spliced five FRESH IDS under it, and
`task-2-x1-n2`'s first recorded row is turn 1, *"Let me start by examining the
existing codebase"*, followed by `find /app`. `task-2-x1-n1` opens the same way.
No resume event, on any of the fourteen nodes three rounds spliced.

The seed built for the eighth failure worked, on the two paths where the id stays
the same — the in-place retry and the requeue after a claim comes back — and on
the one growth reason it had been wired for. **A growing job does not keep its
id.** It splices new ones beside the work, under the root, so the result is
announced like any other deliverable; so every path that grows a job is a path
where a node-shaped seed finds an empty record and starts cold beside a workspace
full of its predecessors' work.

> **THE RECORDED RUNS ARE A LINEAGE PROPERTY.** Every node spliced under a
> lineage — by an overrun, a gap round, a cooperative split, a deferred
> resumption — is seeded from the recorded runs of that lineage, and the
> resumption is journaled against the node that is actually resuming.

It is read at the splice and not passed in by the caller. `replanOverrun` is the
one seam every growing job passes through, and a property of the work that
arrives as a parameter is a property of whichever caller somebody remembered:
that is exactly how this survived the eighth failure's repair, which wired the
overrun call site and left the gap round — the one this run actually took —
reaching the graph through the same function with an empty field.

### 2. A measurement that reaches the worker as prose has not reached the worker

The coverage gate measured the same two unexercised behaviours on all three
rounds — **2, then 2, then 2**. Those rounds briefed fourteen nodes. **Thirteen
of the fourteen name neither behaviour**; one names one, by luck of the planner's
wording. The job measured its shortfall three times, spent fourteen leaves, and
never told a worker what it was.

`store.DeliveryGate.Unexercised` is a list, and it was made one deliberately —
the seventh failure's own repair, so that a finding could not be lost inside a
judge's paragraph. It was then flattened back into a paragraph on the only path
that mattered: written into a goal, handed to a planner, and restated in whatever
words that planner chose. A model summarising a page of instructions drops a
two-item list most times it is asked.

> **WHAT A WORKER IS TOLD IT IS SHORT OF IS NOT A THING ANOTHER MODEL GETS TO
> PARAPHRASE.** The open findings are read from the record — the gate's
> unexercised behaviours, the last reading's failing checks, the standing gap —
> and written into a fixed section at the top of the brief by the composer, at
> the last moment before a worker reads anything, which is the only point
> downstream of every planner.

Both repairs share one reader, `resident.ReadOpenFindings`, because the planner
sizing a remainder and the worker doing it are short of the same things.

### And a note on how the first of these was found twice

`LineageBank` returned "no record" for a lineage that had 80 turns, because the
query it was built on appended one `ORDER BY` to another and did not parse. The
error was swallowed into the same empty return an honest absence uses — the
sixth failure's rule, broken inside the fix for the ninth. It logs now. **An
unreadable record and an empty one must never be the same value**, and the place
that lesson keeps having to be learned is the error path of whatever was just
built.
## A thirteenth failure, 2026-08-29: the table of blanks

*Added against the s9 sweep's stores — `bench/deepswe/results/{ink,igel}-…-s9/`
— where every node's `subharness` column was empty.*

The question an autopsy asks first is who did the work. The store could not
answer it. `nodes.subharness` was blank on every node of both runs, and the
diagnosis that followed cost a day.

The column was never lying; it was answering a different question. It holds the
worker a node was **assigned** — the compiler's routing judgement, or the
escalation that replaced it — and the compiler routes almost nothing, so its
honest answer for ordinary work is nothing at all. What actually happens is that
the dispatch path resolves that empty assignment against the registry, gets the
generalist, and runs it. `linear` is never a registered subharness, nothing
degrades, nothing is wrong, and **nothing anywhere writes down that a worker
took the node.**

So one blank meant four things: nobody routed it, nobody claimed it, nobody ran
it, or a worker ran it and nobody said which. It also could not tell a node that
got the specialist it was promised from one whose build had no such specialist
and quietly ran the generalist wearing its name — which is a benchmark cell
silently measuring the wrong program. And the rig's own "VOID if a `swe` node
ran" guard read this column, so the guard was asking the ask.

> **THE WORKER THAT RAN A NODE IS A FACT ABOUT THE RUN, AND IT IS WRITTEN DOWN
> WHERE IT HAPPENS.** Not derived afterwards from an assignment that is empty by
> design, and not left to a reader to infer from an absence. The generalist says
> `linear` out loud, for the same reason it has a name at all: an unnamed worker
> and a worker nobody recorded look identical, and the difference is the whole
> autopsy.

`nodes.ran` carries it, `store.EventNodeRan` journals it with the worker it
replaced and why, and both are written at ONE seam — `runningWorker` in
`cmd/aforge/subharness.go`, the single place the surface builds a leaf's
executor, with a source test that fails the build on a second one. The
assignment column keeps its own meaning untouched: one column, one question,
and the two are allowed to disagree, because the runs worth reading are exactly
the ones where they do.

The headless stream says it too — every `▶` and every `✓` now carries its worker
in parentheses, the generalist included — so the reading that cost a day is a
line a person watching already has. Clause 3 and clause 4 are one fix here: a
record nobody can read and a stream that does not say it are the same silence.

## A fourteenth failure, 2026-08-29: the reading that read something and reported nothing

*ink s10 and s11, `bench/deepswe/results/ink-grid-box-layout-*`, 2026-08-29.*

Every leaf of both sweeps journals the same pair. The baseline: `npx ava --tap`,
`read: true`, **44** checks named (156 on one node), `exit: -1` — killed at its
ceiling with a roster already in hand. The finished tree: the same command, the
same workdir, `read: false`, `named: 0`, `exit: 0`, and the sentence *the
finished tree was not read: `npx ava --tap` was killed at its ceiling without
finishing*.

Three things that sentence is not. It is not the reader: the same reader named 44
checks off the same command minutes earlier. It is not the scope: both rungs are
`whole`, and nothing had narrowed anything. It is not the room: the runner ran
and streamed.

**The after path discarded a roster it had already read.** `verify.RunReading`
returns the names it parsed whether or not the ceiling fired — that is the ink s7
repair — and `photograph()` keeps them on the before side. `PhotographAfter` had
its own `case after.TimedOut` that set `Unread` and threw the result away.

Three clauses this breaks. **Clause 4**, a record that can be autopsied: two
sweeps of stores say *nobody could read this tree* about a tree that was read.
**Clause 3**, reaching the person: the gate is handed *was not read* and cannot
tell it from a project that declares no verification. And **clause 5**'s floor,
because the coverage settlement spends the after roster and there was none.

What it states:

> **A READING THAT PRODUCED OUTPUT PRODUCED A READING.** A cut roster is kept and
> marked partial — it answers *does a check for this exist* and never *did this
> work break something*. A command that RAN and named nothing says that, in those
> words, rather than borrowing the sentence for a tree nobody could read. And a
> rung this program DERIVED that names nothing is retaken on the rung the baseline
> proved, once, before anything is reported unreadable.

---

## A fifteenth failure, 2026-08-29: the rounds that were free, and the wall nobody could see

*ink s10 and s11, happy-dom s10, `bench/deepswe/results/`, 2026-08-29.*

Three runs, three walls. Each was given 5400 seconds and spent 5401 of them.
Each ended `settled: false`. Between ink s10 and happy-dom s10 there is **not one
gate event** — a gate is cut at settlement, and a run the clock kills mid-round
never settles, so nothing was ever judged. ink s10 cost $0.538 and delivered a
verdict on nothing at all.

Every rule that should have stopped this already existed. Four things were wrong
with them, and they are four readings of one mistake: **the governor was
measuring the wrong quantity, over the wrong unit, at the wrong scope, against a
clock it could not see.**

### 1. Progress was measured over any file, so a stuck model bought rounds for free

ink s10's five `job_growth` rows are all `reason: overrun`, on one lineage —
`task-2 → x1 → x2 → x3`. The remainder digest changed on every single round
(`9c6637ca → d0ef521a → f804b0fb → 18a30ad8 → 64ae4b6d`), so the fixed-point rule
saw motion. The produced count was 15, 5, 9, 4, 4, so the standstill rule saw
motion too. What the run had actually written, recoverable only from resume
payloads in the transcript, was `debug-grid.ts`, `debug-grid2.ts`,
`debug-grid3.tsx`, `debug-grid10.ts`, `debug-grid11.ts`, `debug-yoga.ts`,
`debug-yoga2.ts`, `debug-test2.tsx`, `debug-test3.tsx`, `debug-grid-pos.tsx`.

A model that is stuck writes scratch files, and a scratch file is a file. **Clause
2 was satisfied and clause 1 was not**: the evidence came from the world — the
workspace's own before-and-after diff, exactly as it should — and the DETECTOR
read it as a count of paths, which is a vocabulary the work being checked
controls. What finally stopped ink s10 was arithmetic, `cause: rounds`, four
rounds and ninety minutes later.

> **A ROUND MOVED SOMETHING ONLY IF IT MOVED WHAT THE JOB IS ABOUT.** Check files
> always. Source files inside the focus the request names, and beside it — the
> job's own focus, resolved by `verify.Locate` and made adjacent by
> `verify.Adjacent`'s first rank, which is the identical reading the verification
> photograph takes to decide how much of a project to read. And the job's own
> shortfall getting smaller: a failing check that passes, a stated behaviour
> brought under a check, a deleted public name put back
> (`store.SurfaceReading`), a review finding answered. Everything else the round
> wrote is journaled BY NAME as scratch, because "this round wrote fourteen files
> and moved none of them" is a recognisable failure and the names are what make
> it recognisable.

No pattern for `debug-` appears anywhere in the mechanism, and none may. That is
one model's spelling and a rule written against it would be one spelling behind
forever. What is structural is the relationship between what a round wrote and
what the request is about — and a job that names nothing this workspace holds
narrows by nothing, which is verify's own reading of an empty focus and the
fail-safe direction here.

### 2. An overrun round was not a round

ink s10 exhausted eight times and journaled five growth decisions. The gap is the
leaf that ran out of room and was claimed again IN PLACE — same node id, attempt
raised by one, its own banked transcript handed back to it. Nothing is added, so
nothing passes the growth gate, so the ledger never sees it and no rule that
counts rounds out of that ledger can weigh it.

> **A BODY OF WORK IS A ROUND WHATEVER MADE IT POSSIBLE.** A resumption is
> journaled as an admitted round with the same evidence every other round
> carries. It does not spend the round CAP — the cap bounds how many times a job
> may be made bigger, and this makes it no bigger — and it is weighed by the
> evidence rules, which is exactly what it was escaping.

### 3. A standstill was a fact about a lineage, and the wall is the job's

happy-dom s10 is the demonstration, and it is exact. `task-2-x1` asked for a
third overrun round and was **refused, `cause: standstill`**, in the right words,
at the right time. Then `task-2-x2` — a sibling lineage — bought a revision round
and ran the remaining forty minutes into the wall. One lineage being declared a
fixed point said nothing whatever to the other.

> **THE JOB HAS ONE WALL, SO A STANDSTILL IS WEIGHED AT THE JOB.** Two consecutive
> measured rounds anywhere in the job that moved nothing relevant, and no lineage
> of it may buy another. The floor holds unchanged: two, never one, because a
> leaf can run out before it writes its first file and that is precisely the
> round a repair exists for (clause 5).

The fixed point stays per-lineage, because it is a claim about one text: two
lineages are two remainders and their being different says nothing.

### 4. The wall existed in one process and not in the machinery

`revision.outOfWall` has read `ctx.Deadline()` since the SETTLEMENT §3 wave, and
in a headless run it has never once fired. `aforge do` builds its timeout context
and hands it to the **settlement watcher**; the brain that plans, claims,
executes and grows runs on `context.Background()`. So every rule in the program
that asks "is there time left to finish another round of work" was asking a
context with no deadline and being told there is no limit.

> **THE WALL THE WATCHER IS WATCHING IS THE WALL THE WORK RUNS UNDER.** The
> errand's timeout is the run context's deadline. And a job that cannot hold
> another round of work stops growing, so what is running lands and the job
> settles and a gate verdict exists — the bound derived from the job's OWN
> measured rounds, never a typed number (PERF.md, "What a round of a job costs").

### And the person is told

All four of these were already in the journal, on a node inside a job nobody
opens, while the last line of the run said `partial` and named the clock. A run
that reached its wall having already been refused for a standstill must not
report the clock as the reason; the clock is what it ran into afterwards.

```
partial — no relevant progress in 3 rounds; last change: src/grid.ts
partial — no time left for another round of work
```

Clause 3, again, and it is the third chapter in this document to end on it.


## What is deliberately not here

No new round cap, no new timeout, no new constant of any kind as the lever. The
round cap was reached in ink s10 and it was too late by ninety minutes; a
smaller one would have been the wrong number for every other run. What was
missing is a detector that could tell fourteen debug files from a fix, a ledger
that counted the rounds that actually happened, a scope that matched the thing
being bounded, and a clock the machinery could read.

## A sixteenth failure, 2026-08-29: a name read out of a sentence

*ofetch nemotron n1,
`bench/deepswe/results/ofetch-per-origin-circuit-breaker-nvidia-nemotron-3.5-lightning-n1`,
2026-08-29.*

The `task-2` gate reads: *This work broke checks that were passing before it:
**to**. They were measured twice with the project's own command, before the work
and after it.* The `task-2-x3` gate on the same run names its regressions
correctly — `ofetch calls hooks, ofetch hook errors` — so the mechanism works;
this one reading did not.

The baseline is `whole, named 28, red 0`, read through vitest's own JSON. Four
after-readings come back `named 1, red 1`, every one of them flagged
`read_as_plain`. The suite had failed to COLLECT — an import that would not
resolve — so `vitest run --reporter=json` printed no JSON, the reader for that
format found no record, and `RunReading` fell back to the shared PASS/FAIL
vocabulary over an error dump. There the dotnet/xunit line
`^\s*(?:Failed|X)\s+(\S+)\s` met the English sentence *Failed to load url
./circuit-breaker* and captured `to`.

Two clauses. **Clause 1**, detect by structure: `\S+` after an English word
matches an English word, and a check name has to come from the runner's own
test-record grammar. **Clause 2**, source the evidence from the world: a suite
that never ran a check has no roster, and what a fallback scrapes out of its
error text is a guess about a suite that did not run — subtracted against a
baseline of 28 it convicted the work of breaking a check that does not exist.

What it states:

> **A NAME COMES FROM THE RUNNER'S OWN TEST-RECORD GRAMMAR, NEVER FROM A
> SENTENCE.** The dotnet/xunit patterns require the fully-qualified name that
> runner actually prints. And **a runner told to print a machine-readable report
> prints one whenever it ran its tests at all**, so its absence is a fact:
> `Result.Uncollected`, carrying the runner's own words as `Error`, never
> subtracted against a reading that did collect, and reported as *its suite
> failed to collect* rather than as a red check.

## A seventeenth failure, 2026-08-29: the judge that read a sentence about the tree

*Added against `bench/deepswe/results/textual-richlog-follow-state-nvidia-nemotron-3.5-lightning-n1/`.*

Three delivery gates refused the same job in a row, and all three refusals were
about a sentence:

> `gate: refused — The deliverable is a single JSON contract string, not the
> required Python source files. The fenced text between BEGIN DELIVERABLE and
> END DELIVERABLE contains only {"contract": "..."} — no _log.py, _rich_log.py,
> or examples/rich_log_follow_state.py.`

> `gate: fail — The deliverable contains only text describing what was
> supposedly done, not the actual code … The actual code files need to be
> examined against the requirements.`

Every clause of both is true of the fenced text and false of the run. The
worktree held forty-two kilobytes of changed Python; the artifact record named
six files; the run's own footer named them back to the person — *"Whatever the
account above says, 6 files reached disk and can be opened"*. The judge never
saw any of it, because the gate handed it the worker's final message as THE
deliverable and put the tree underneath as a record the message could be checked
against. `$0.46`, twenty-eight minutes, `partial`.

The proximate cause is a model quirk and worth naming only to dismiss it: this
worker answered the delivery fence with `{"contract": "…"}`. Fine. **A gate must
be immune to what the worker SAYS**, and this one was built the other way round.

### Clause 2, in its strictest form

> **A fail-safe sources its evidence from the WORLD, not from the component it
> is checking.**

The deliverable of a request that changed a repository is the change. The
worker's message is a CLAIM about that change, and a claim is read beside the
thing it is about, never in place of it —
`docs/design/gate/SETTLEMENT.md` §6 had already settled exactly this for what may
OVERTURN a finding, and the same sentence was still false one step earlier, at
what the judge is handed in the first place.

So the fence's contents are decided by the record and not by the prompt's
layout (`internal/revision/subject.go`):

| the record | what the fence holds | what sits below it |
| --- | --- | --- |
| holds files | the changed sources and checks, with bounded excerpts of what is in them | the worker's message, headed *"THIS IS A CLAIM ABOUT THE DELIVERABLE AND IS NOT THE DELIVERABLE"* |
| holds nothing | the worker's message — it IS the artifact | the run records, unchanged |

The split is `verify.ChangedSources` and `verify.OwnChecks`, which is the same
reading the gate's own verification is taken through; the excerpt budget is one
share of the judge's room, documented in PERF.md; and what the record NAMES and
the tree does not hold — a path that is not on disk, a directory wearing a
file's name — survives in the record block either way, because those two lines
are the ones that convict.

### And a finding about the fence is unsayable, not discouraged

The prompt already said, in its own words, *"Read the fenced text itself before
you say anything about it"*. Three verdicts described a JSON object anyway. A
sentence is not a mechanism.

The shape of a refusal now follows the subject. Over a changed tree a fail
carries `file` — one entry of the record, with the record itself as the field's
enum, so a routed endpoint refuses anything else on the wire — plus the span of
the request that file fails. A verdict that names neither is not a verdict this
gate can read, and it travels the path every unreadable answer travels:
`internal/shaped` asks once more with the schema and the model's own words
quoted back, and when that fails too the gate FAULTS. Which is the only honest
ending. A judge that cannot say what is wrong with a file the run changed has
not judged the run, and the floor says a check that did not happen may not be
the reason a run reports itself whole.

The validation lives in the verdict's own `UnmarshalJSON` on purpose: a verdict
that is unreadable BY CONTRACT and one that is unreadable by syntax then travel
one path, get one re-ask, and end in one typed fault. A second repair ladder
would have been a second contract.

### The fence's shape is a repairable shape

`no structured_repair event fired either` — and that is the third defect. Every
ask in this system that wants a JSON object and gets prose is repaired by one
seam. The delivery is the one ask pointing the other way, and it had no repair
at all, so a model quirk became three silent re-drives.

`internal/shaped.Prose` is `Answer`'s mirror: one re-ask, the model's own object
quoted back, journaled as `structured_repair` with `lane: delivery`. An answer
that cannot be reshaped comes back as the original, because a delivery whose
shape could not be fixed is still the delivery.

### Clause 4, and the line the person reads

`store.DeliveryGate.Subject` carries `tree (6 files)` or `claim`. Without it
those three refusals are, afterwards, indistinguishable from three refusals over
a real reading of the world — the only way to tell was to reconstruct the prompt
from the transcript. And the refusal a person watches now opens with a path in
the repository rather than with a description of a sentence:

```
gate: fail — src/textual/widgets/_rich_log.py — follow_end is declared and never posts FollowChanged
```

## An eighteenth failure, 2026-08-29: the checks the run wrote, called checks it broke

*happy-dom nemotron n1,
`bench/deepswe/results/happy-dom-deterministic-intersectionobserver-nvidia-nemotron-3.5-lightning-n1`.*

The gate reads *This work broke checks that were passing before it:
IntersectionObserver initial observation queuing Queues an entry for each newly
observed target, …*. The grader's p2p on that same tree is **9/9**.

Every reading of the job ran the identical command —
`npx vitest run --reporter=json test/intersection-observer/IntersectionObserver.test.ts`
— scoped to one file, in one package. The baseline is `named 4, red 0` on all
nine of its readings. The after-readings go 27/25, then 0, then 27/1 three times,
then 4/0, then 33/18, then 5, then 4. **The roster grew from 4 to 33 because the
run WROTE those checks**, into the very file the reading was scoped to. The
"broken" ones did not exist when the baseline was taken.

No rule fired and nothing looked wrong. The comparison was subtracting the two
FAILING lists, which is exact only while both readings run the same set of
checks — and a run writes checks. Every earlier guard was a way of noticing that
the set had changed (a widened selection, a file added to the command); here the
command never changed at all.

Clause 2, evidence from the world: the baseline's own ROSTER is the record of
what existed, and it was in hand the whole time. Clause 3, reaching the person:
a leaf told it broke the repository and a leaf told its own new tests do not pass
will do two different things about it.

What it states:

> **A REGRESSION IS A CHECK THAT WAS NAMED GREEN AT THE JOB BASELINE AND IS RED
> NOW — nothing else.** A red check the baseline's roster never named is the
> run's OWN, and it is a different finding in its own words: `the checks this
> work wrote fail: …`. Sourced like the regression, buying the same round, with
> its own gate line and its own journal field. Where the baseline named no GREEN
> check the question cannot be asked — a runner that prints only its failures has
> a reported list that IS its failure list — and the old subtraction stands.

### Addendum, the same day: what the first sweep carrying it found

Three faults, one sweep, and all three are the same mistake in different clothes
— a fact about the READING reported as a fact about the work.

**The excerpt was judged as the deliverable.** ofetch v4-flash s12 (4/47, against
42/47 the seed before) refused on *"The deliverable does not include the actual
content of the circuit breaker module … the fenced text shows only a truncated
excerpt ending mid-sentence"*. textual v4-flash s12 (15/20 against 17/20) refused
twice more, on *"The file is truncated — it cuts off before the implementation of
`write(expand=True)`"* and *"the fenced material is a description of what the
files contain, not the files themselves"*. Every clause of all three is true of
the block and false of the run.

So no file is ever printed in part. Contents appear entire or the file appears by
name and size only, and the page says which. Saying *"this is an excerpt"* above
it was the first fix and it is not one: textual's store holds two
`structured_repair` rows on lane `gate`, so the verdict shape DID refuse the
complaint and the re-ask returned it wearing a file and a quote. **A percept is
not argued with; it is removed.**

**And a gap is a behaviour, never a file's completeness.** The record already
answers whether a file exists, and no behaviour a request states is about a file
being on disk — so where the request states behaviours (the acceptance checklist,
read before any work existed), those spans are the schema's enum for `quote` and
the only ones a refusal over a changed tree may be built on. Containment runs one
way only: a quote that is PART of a listed behaviour is that behaviour quoted
shorter, and a quote that CONTAINS one is a longer sentence with a behaviour
inside it — which is exactly what ofetch's refusal was. A request that states no
behaviours turns the requirement off, because a contract nobody was shown is a
contract nobody can satisfy.

**Two readings of one job may not refuse each other in silence.** Both runs then
did the same thing: the repair round the finding bought was refused
`goal-already-covered`, and the run reported `partial — no more work could be
started on it`. The gate says the delivery is short; the coverage question, asked
of the whole job's criterion against everything landed, says there is nothing to
add. That sentence is neither answer.

> **THE FIRST ROUND AFTER A FINDING THAT NAMES A FILE OF THE RECORD IS NOT THE
> COVERAGE QUESTION'S TO REFUSE** — the finding is itself a reading of the world,
> and a narrower one. **And where coverage refuses a later round, the finding is
> what was wrong**: it is journaled as overturned and the verdict is taken again
> from the settled fields, never handed over as a shortfall nobody can act on.

`GrowRequest.Grounded` carries the first half; `Extension.Overturned`, read off
the growth journal rather than threaded back through four signatures, carries the
second. Every cap the governor holds is untouched by both.

**And clause 4 again: the subject did not reach the journal.** `subject` was
empty on every gate of both runs, so the one question an autopsy of this
mechanism asks — did the review read the world or a sentence — had no answer on
the runs that needed it. It was being written onto each judgement at the place
that built one, and a judgement is built at thirteen places. It is now stamped
once, at the single exit of `JudgeDeliverable`, over whatever comes back —
including a fault. **A field set by every constructor is a field the next
constructor forgets.**

**And clause 4 once more, one seed later.** textual v4-flash s13 journaled both
gates correctly — `subject: tree (3 files)`, `tree (4 files)`, and a refusal
quoting a verbatim behaviour — and an autopsy still could not tell whether the
quote had PASSED the checklist enum or there had been no checklist to pass. Two
`reasked` rows on lane `gate` had the same problem in the other direction: "the
answer was not readable" is a model reasoning out loud and a caller's own
contract refusing a well-formed answer, and one word was journaled for both.
Settling it meant reading completion-token counts out of the usage table three
events either side (294 and 258 rejected, 12 accepted — prose, both times, so
the re-ask was doing real work).

> **A FAIL-SAFE THAT FIRED WITHOUT SAYING WHICH DOOR IT WAS IS HALF A RECORD.**

`store.DeliveryGate.HeldPoint` carries the behaviour a verdict was held to — the
matched span, `checklist: N behaviours` on a pass, or `checklist: empty` where
the request states none and the requirement was off — derived at the same single
exit `Subject` is. `store.StructuredRepair.Note` carries the decode error
verbatim, which is where the two rejection doors already differ: `response
contains no JSON object` against `parse response: a fail must quote one of the
behaviours this request states …`. Nothing classifies anything; the readers that
already disagree are simply quoted.

## A nineteenth failure, 2026-08-29: the check that mentioned it and weighed nothing

*textual v4-flash s13,
`bench/deepswe/results/textual-richlog-follow-state-deepseek-deepseek-v4-flash-s13`.*

Exit 0, `pass: true`, subject `tree (4 files)`, 19 of 20 hidden f2p and 4 of 6
p2p. The gate's first round had named the behaviour that was later missed —
`RichLog.write(expand=True) no longer preserves full-width justified rendering
with current Rich` — as exercised by nothing. Its second round mapped that
behaviour to a check the run had just written, and the finding closed.

The check calls `rich_log.write("short", expand=True)` and then asserts
`len(rich_log.lines) > 0` and `strip.cell_length >= 5`. `expand` is in the call.
It is in no assertion of that check or of either of its two siblings, and
`min_width` is nowhere in the run's diff. The hidden check for the behaviour,
`test_rich_log_expand_entries_reflow_after_min_width_change`, was red.

Every existing door held and every one of them was answering a different
question. The mapping asked a model whether the check exercises the behaviour and
got yes. `GroundMapping` asked whether the check's file SPELLS the names the
behaviour spells and got yes — truthfully, because the call spells them. Clause
2, evidence from the world: the file was on disk the whole time, and the fact
that decides this is a fact about which lines of it are assertions.

What it states:

> **A BEHAVIOUR IS EXERCISED BY A CHECK ONLY WHERE THE CHECK'S OWN ASSERTIONS
> NAME ONE OF THE BEHAVIOUR'S OBSERVABLES** — the identifiers the request itself
> spelled, read by shape from the person's words, matched against the text of the
> check's assertion statements, read by shape from the file. Setup is not an
> assertion. A behaviour a check NAMES and no assertion WEIGHS is *weakly
> exercised*: its own finding, its own gate line (`asserted by no check: … —
> observables never asserted: …`), its own journal field, buying the round an
> unexercised behaviour buys.

And the fail-safe direction is stated once and holds everywhere: **a behaviour
that names no observable, a check in an unparsed language, a check whose
declaration cannot be found, and a check the run did not write are all left
exactly as the mapping answered them**, and one observable in one assertion
clears the point. A floor that refuses everything is not a floor — which is why
`Normal scrolling must still update the visible viewport and vertical scrollbar
position`, a true sentence of the same request with no identifier in it, is
asked nothing by this door even though two hidden checks about scrollbar
position failed.

## A twentieth failure, 2026-08-29: the name that stayed and the shape that moved

*igel v4-flash s12,
`bench/deepswe/results/igel-persist-feature-schema-deepseek-deepseek-v4-flash-s12`.*

The presence photograph worked, and worked twice. Round one:

```
surface {"compared": 8, "lost": 8,
         "names": ["Igel.results_path", "Igel.default_model_path", …]}
gate    This work removed a public name that existed before it: Igel.results_path, …
```

That refusal bought a repair round; the round put the eight class attributes
back; the next reading came back `lost: 0`. Then all **24** hidden tests failed:

```
E       TypeError: 'Configs' object does not support item assignment
```

The run had rewritten `igel/configs.py`. The module used to bind `configs` to a
dict; it now binds it to an instance of a small class the run wrote, with `get`,
`__getitem__` and `__contains__` — and no `__setitem__`. The name `configs` was
there before and there after. Every door in this harness was looking somewhere
else:

| door | what it asked | what it said |
| --- | --- | --- |
| check-level reading | what does this project's suite say | green |
| presence photograph | is the name still there | `lost: 0` — and true |
| assertion door | do the run's checks weigh the behaviours the REQUEST states | those behaviours, unaffected |

Nobody writes down "and the config object must still support item assignment",
so no behaviour of any request could ever have carried this finding, and the
citation invariant would refuse it if one tried.

> **A NAME IS NOT A CONTRACT.** A definition whose DECLARATION this run's own
> diff rewrote, beside the places the rest of the project still uses that name
> and the syntactic SHAPE of each use, is a fact about the world measured
> without a model — and it is the only fact that catches a name kept and a shape
> moved.

`verify.ChangedDefinitions` intersects the hunks of the run's own diff with the
declaration spans the surface reader already locates, scoped to the run's own
record; `verify.Consumers` walks the project for whole-identifier usage sites
outside those hunks, one walk for all the names, and reads each site's shape
from the token beside it — a closed set (`call(N args)`, `subscript`,
`subscript-assign`, `attribute .x`, `iterate`, `instantiate`, and `use` for
everything it cannot name). Nothing here resolves a type or follows an import.
The judge is shown the definition, its span on either side of the work, and its
consumers grouped by shape with a sampled site, its line number and the line's
own text; whether a class with `__getitem__` and no `__setitem__` survives
twenty callers that subscript-assign it is left to the reader that is paid to
make judgements. Clause 1 for the reading, clause 2 for where it comes from.

### The one reader that could not see it

`configs` is a module-level binding, and the python surface reader read class
bodies and module `def`/`class` and **not** module-level assignments — alone
among the four. Go reports an exported `var`, TypeScript an exported `const`,
Rust a `pub static`; python's singleton, table or path handed out by a package
was read by nothing, which is why the name that moved in s12 was never in the
photograph at all. It is read now, by the same underscore rule everything else
in that file uses.

### And the refusal has a ground of its own

Over a changed tree a fail must name a file and quote a span the judge was
shown. The record is what the run WROTE; a consumer is somewhere else in the
same repository the run broke without touching. So the `file` enum gains the
consumer files and the `quote` enum gains the consumer lines — the sampled ones,
never one held back — and only where there was already an enum to join, because
a request that states no behaviour leaves the quote free and narrowing it here
would be the opposite of what this door is for. The HeldPoints rule is
unmoved: a refusal quoting neither a stated behaviour nor a line it was shown is
still not a verdict this gate can read, and still faults.

A consumer-grounded refusal is **Sourced** — there is no span of the request to
cite, for the same reason a regression has none — and it is its own line, its
own journal field (`store.DeliveryGate.Consumers`) and its own event
(`store.EventConsumers`, journaled whether or not anything was found):

```
gate: fail — tests/test_igel.py — configs changed and its 2 consumers still use it as subscript-assign: tests/test_igel.py:5, tests/test_igel.py:6. Configs implements __getitem__ and no __setitem__, so this line raises TypeError.
```

### What this would and would not have caught

Said plainly, because a fail-safe that is oversold is a fail-safe nobody
re-measures. **In igel's own repository at that commit there are zero
subscript-assign sites for `configs`.** Run against the s12 tree and its own
patch, this reads the changed declaration as `configs` (`igel/configs.py`: the
diff replaced lines 3–44 of the file as it was; the declaration now stands at
line 73) and finds **23** usage sites elsewhere — 20 of them `attribute .get`,
3 of them `use` — and the assignment that actually broke the run is
`monkeypatch.setitem(configs, …)` inside a HIDDEN test this harness never sees.
So what this would have put in front of the s12 judge is that a definition it
had just rewritten is used in twenty-three places it did not touch, and left the
judgement there. It closes the class of failure; it does not promise this
instance.

### What is deliberately not here

No type checker, no import resolution, no semantic model of what a shape means.
Every one of those would need a toolchain per language in a container this
harness does not control, and would fail closed the moment it could not run —
which is the shape of a fail-safe that becomes a false blocker on real work.

## A twenty-first failure, 2026-08-29: the stub that was replaced, called a check that was deleted

*happy-dom v4-flash s13,
`bench/deepswe/results/happy-dom-deterministic-intersectionobserver-deepseek-deepseek-v4-flash-s13`.*

The request asks for a real `IntersectionObserver`. The repository's own checks
for it are stubs — `IntersectionObserver observe() Does nothing`, and three
siblings of the same shape. The run rewrote them into real checks under the
identical describe path. The gate answered:

```
gate: fail — This work removed checks that existed before it: IntersectionObserver disconnect() Does nothing, IntersectionObserver observe() Does nothing, IntersectionObserver takeRecords() Returns empty array, IntersectionObserver unobserve() Does nothing.
```

Four times, on four rounds, in the same words. Every repair round it bought was
aimed at putting back checks that had not gone anywhere, so nothing it did could
close it, and the run finished at 9 of 14 where the previous seed reached 12.
The grader's p2p on that tree is **9/9**: nothing it looked for was missing.

The subtraction was exact and the conclusion was not. `Reading.Vanished` asked
which NAMES the after roster stopped reporting, and a name is not a subject. A
run's job is very often to rewrite checks, and a rewritten check changes its
title while staying exactly where it was.

> **A CHECK THAT EXISTED BEFORE AND IS ABSENT AFTER IS REMOVED ONLY IF NOTHING
> AFTER IT COVERS ITS SUBJECT.** The subject is read out of the runner's own
> grammar and never out of a word list: the name's parent path — the describe
> chain, the pytest node id's file and class, the Go test a subtest hangs off —
> plus the leading CODE-SHAPED tokens of its leaf title, with the clause after
> them dropped by structure. A code-shaped token is one carrying punctuation or
> an inner capital that a sentence does not carry: `observe()`,
> `IntersectionObserver`, `RichLog.write`. `Does` is prose.

`verify.CheckSubject` reads it, as a prefix of the name itself so two names
carrying one subject carry it byte for byte. `verify.SplitReplaced` sorts the
names a roster stopped reporting into the ones nothing covers and the ones a
later check took over; `Reading.Vanished` reports only the first, and
`Reading.Replaced` carries the second as a record. Whether the checks that
replaced a stub PASS is a different question, and the finding for it already
exists — `the checks this work wrote fail: …` (the eighteenth failure above).

**Two fail-safes, both in the direction that costs nothing.** A runner whose
names carry no path — TAP prints a plain sentence — has nothing here to read,
and its rosters subtract exactly as they always did. And where the path is
spelled with whitespace rather than a separator, a single leading code name is
the top-level describe every check in the file shares: `IntersectionObserver` is
a file and `IntersectionObserver observe()` is a subject, so one token alone is
not read as one. An explicit separator is the runner SAYING the name is a path,
and one level of that — a pytest file, a Go test function — is enough.

**And the record says the mechanism ran.** A mechanism that silently declines to
convict cannot be told apart from one nothing reached, which is clause 4. The
verification journal row gains `replaced: N`, beside `named` and `red`. The gate
line for a genuine removal is unchanged.

### And a finding's identity was its sentence

The same four rounds raised the same finding and no reader could say so. A
finding's only structure was its prose, with a bounded list of names glued into
the middle of it, so one finding raised over two different tails read as two
different findings and two identical raisings read as one repetition nobody
counted. `Judgment.Finding` and `store.DeliveryGate.Finding` name which
measurement raised the gap — `regression`, `own-checks-failing`,
`removed-public-name`, `removed-checks` — beside the `quotes` that were already
there. The pair is the comparable thing, and it is set on all four of the gate's
sourced findings rather than on the one that needed it.

### What is deliberately not here

No reading of the test SOURCE to recover a describe chain. The check names a
diff yields (`verify.PatchChecks`) are bare leaf titles with no path in them, so
they carry no subject and are compared by name as before — the diff half of the
removal finding is unchanged by all of this. And no attempt to decide whether the
replacement is as strong as what it replaced: that is a judgement, the gate has a
judge for judgements, and a mechanism that tried it by counting assertions would
be back to reading vocabulary.


## A twenty-second failure, 2026-08-29: four rounds for one finding, and the gate that never happened

*happy-dom `deterministic-intersectionobserver` and ofetch `per-origin-circuit-breaker`,
v4-flash s13, `bench/deepswe/results/`, 2026-08-29.*

Two runs, one wall each, two different ways of spending it on nothing.

happy-dom s13 raised **one finding four times**. Four consecutive delivery
judgements carried `This work removed checks that existed before it:
IntersectionObserver disconnect() Does nothing, IntersectionObserver observe()
Does nothing, …` — the same four names, in the same order, word for word — and
each one bought a repair round. The run ended `settled: true` at 2663 seconds and
$0.135 with 9 of 14 hidden checks; the same task on the previous seed, working
the same problem, reached 12.

ofetch s13 spent 5407 seconds, 422 model calls and eight growth rounds and
journaled **not one delivery judgement**. Its own governor had already said the
wall was too near — twice, `cause: out-of-wall`, inside the last ninety seconds —
and both times the run kept going, because a refusal declines to ADD work and
says nothing whatever about the work already queued. Thirteen pending leaves and
a pending job root at the wall, `settled: false`, and nothing judged.

### 1. The journal lost its memory at the one moment it exists for

A repair of a top-level job is spliced BESIDE the job and not beneath it — a
delivery law, so the finished remainder is announced like any other deliverable —
which makes its sink a top-level node and therefore, to `jobRootID`, its own job
root. The growth journal was keyed by whichever node asked. So happy-dom's four
rounds wrote four journals of one row each, under `task-2`, `task-2-x1`,
`task-2-x2`, `task-2-x3`, and every rule that weighs a round against the one
before it read back an empty journal.

The evidence was there and unreadable. All four rows carry the IDENTICAL
remainder digest `dc919e4e34171c4a` — which is the fixed-point rule's entire
subject, invisible to it because no two of those rows were ever read together.
The round counter survived only by accident: the splice derives it from id
arithmetic rather than from the journal, which is why `cause: rounds` eventually
fired and nothing else ever did. `revision.SpentCitations` and
`coverageOverturned` had meanwhile always read this journal under the LINEAGE
root, so the writer and two of its readers disagreed about the name of the thing.

> **A JOB'S GROWTH JOURNAL IS THE JOB'S.** It is keyed by the lineage root — the
> "-x" law asked of `OverrunLineage` rather than re-spelled — so a repair that
> continues as a top-level job is the same job it continues.

### 2. A round is bought FOR something, and that is what has to move

Every rule the governor had asks a question about the round: did the tree change
(standstill), did the reviewer type the same sentence (fixed point), how many
have there been (the cap). None of them asks about the thing the round was BOUGHT
for. happy-dom's rounds each changed a file the job was about, so the standstill
rule correctly saw motion on every one — while the finding they were bought for
did not move at all.

> **A FINDING HAS ITS OWN FIXED POINT.** The finding a round is bought to close
> travels with the round, is journaled beside it (`store.JobGrowth.Finding`), and
> is compared against the next round's — the KIND the measurement gives itself
> (`store.DeliveryGate.Finding`) and a digest of the names it cites, never the
> paragraph. Two rounds bought for one finding that both ended with it standing
> are a standstill of that finding, and no third is bought for it: it is handed
> over named instead. The floor is clause 5's and is unchanged — the first round
> a finding buys is never refused — and a finding of another kind is another
> finding, which buys its own.

The prose digest stands aside where a structured finding exists. The two disagree
in both directions: a finding reworded escapes the digest entirely, and two
genuinely different findings a reviewer happened to phrase alike are refused by
it. Where the record can say which finding a round was bought for, the record
decides.

### 3. A gate always precedes the wall

`CauseOutOfWall` did exactly what it was written to do in ofetch s13 and it
bought nothing, because refusing to grow a job is not stopping one. The scheduler
kept claiming the leaves that were already queued, the clock killed the last of
them mid-flight, the job root never became ready, and no gate was ever cut.

> **A REFUSAL TAKEN WITH THE WALL NEARER THAN ONE MEASURED ROUND STOPS THE JOB'S
> QUEUED WORK.** Every part that has not started is retired, every part this
> process is behind is asked to stop and cancelled, and the job root then has
> nothing left to wait for — so it runs, is judged, and the verdict exists.
> `resident.Runner.CloseOut`, reached through the seam every governor decision
> already passes (`SetJobCloser`), so it is a property of the refusal and not of
> whichever caller was wired for it.

And the guarantee does not rest on anybody asking. A job only asks to grow when
something in it ENDS, and a job whose queued leaves keep starting never asks —
which is precisely the job that reaches the wall unjudged. So the settlement
watch holds the same grip and uses it on the clock alone: at the job's own pace
from the wall, a job with no verdict anywhere in its lineage is driven to one. A
job that already has a verdict keeps its last minutes; the guarantee is that a
verdict EXISTS, not that a second is bought at the price of the work that would
have earned it.

### And the estimate is no longer load-bearing

`jobPace` was the LONGEST interval between two admitted rounds. Rounds are
long-tailed — one round of ofetch s13 took twenty minutes while the median of its
five was under four — so a single slow round taught the governor to refuse
everything for the rest of the run. It is the median now, plus the reading pace,
which is the one thing a run ever measures about the machine it is on
(`store.VerificationReading.Elapsed`: forty-five seconds for a suite that takes
eight natively, in an amd64 container under qemu). What makes it safe to relax a
bound is that the bound is no longer the only thing holding: the settlement watch
forces a verdict at the same distance from the wall whether or not any rule here
noticed. PERF.md carries the derivation.

```
partial — IntersectionObserver disconnect() Does nothing and 3 more stood through 2 rounds of repair
```

### What is deliberately not here

No new cap, no new timeout, no new constant of any kind. The round cap stopped
happy-dom s13 and it was two rounds and eight minutes too late; a smaller one
would be the wrong number for every other run. What was missing is a journal that
remembered across the boundary it exists to weigh, an identity for the thing a
round was bought for, and a refusal that stops a job rather than only declining
to enlarge it.

And no forcing of a job that has already been judged, and none on a job whose
pace nothing has measured. With nothing measured there is no honest moment to
choose, and stopping work early on a guess is the failure the whole mechanism
exists to avoid.


## A twenty-third failure, 2026-08-29: a claim about the plan, overruling a measurement of the world

*ink `grid-box-layout` and igel `persist-feature-schema`, v4-flash s14 and s13,
`bench/deepswe/results/`, 2026-08-29.*

ink s14's growth journal reads: `overrun 1 allowed`, then **`revision 1 refused
goal-already-covered`**, **`overrun 2 refused goal-already-covered`**, **`gap 2
refused goal-already-covered`**. Meanwhile its own readings were running 172
checks with 18 red, and the one gate it ever cut held six behaviours of the
request that nothing exercised — "the display style property accepts `grid`",
"gridTemplateColumns accepts a space-separated string of track sizes…". 11 of 25
hidden checks. igel s13 refused two revision rounds the same way while the
surface photograph reported eight deleted public names, on five consecutive
readings.

**Coverage is a claim about the PLAN. A finding is evidence from the WORLD.**
The satisfaction question asks a model whether the acceptance points of a plan
are mapped onto work that has landed or is running. That can be perfectly true
of a job whose checks are red, whose stated behaviours nothing exercises, whose
public names the change deleted, and whose callers a reshaped definition left
behind — because not one of those is a point on the plan. So the two never
disagreed about anything; they were answering different questions, and the wrong
one was binding.

The exemption that existed reached for exactly this and was three dimensions too
narrow: it covered the FIRST GAP round bought by a FILE-CITED finding. An overrun
round, a revision round, and every round after the first were still refused.

> **A COVERAGE REFUSAL IS IMPOSSIBLE WHILE ANY OPEN FINDING STANDS ON THE JOB**,
> whatever the round and whatever asked for it: red checks in the latest reading,
> behaviours nothing exercises or nothing asserts, public names the surface
> reading says are gone, callers a changed definition left behind, or an
> unclosed mechanical gap. Where nothing stands, coverage may refuse — that is
> the question it was built for. The caps are unchanged and remain the only
> other limiters: rounds, standstill, the finding's own fixed point, the wall.

And it is read AT THE JOB. igel s13's refusal was weighed under `task-2-x1`,
whose id namespace does not contain `task-2-x1-n1` — which is where the eight
lost names had been journaled ninety seconds earlier. A repair round is a
different lineage from the work it repairs; the job has one world.

The question is still PUT, and its answer is still journaled, as
`store.JobGrowth.CoveredDespite`: a model saying "nothing is left" over a tree
with eighteen red checks in it is the measurement the row exists to record, and
a run that skipped the question would have nothing to show for it but a round
that quietly happened.

### What is deliberately not here

No weakening of coverage where it is right. A job with a clean latest reading, no
standing gate finding and no lost names is exactly the job this question was
built to stop, and it still stops it. And no new evidence: every kind read here
is a structure some other mechanism already journals — `DeliveryGate`,
`VerificationReading`, `SurfaceReading` — assembled through the account of the
record every brief is already composed from.
