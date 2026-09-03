# The seed list for a second wave

Six more rows from the cold-user trial's first item. **They are NOT in
`LEDGER.md` and #518 is not reopened for them** — that ledger is closed at 181
of 181, and a wave whose scope moves while it is being reviewed cannot be
reviewed. This file is the next wave's starting point.

**Every row here is split**, because most of them are two defects wearing one
sentence. The half this surface owns is WHAT THE PRODUCT SAYS AND SHOWS. The
engine half — why the thing happened at all — is being filed separately, and a
lane that takes a row here should take only the half under `shows:`.

Each row records what I checked against `ui/polish-v0` before writing it down,
because three rows in the last wave turned out to name the wrong thing, and each
of those cost a lane a day.

---

**N1 — the gate's refusal opens on a fact it invented, and buries the one thing
a person can act on.**

**VERIFIED, with the frame.** The trial kept it rather than describing it —
`bench/worker-trial/captures/510-run1-stream.log` on branch
`trial/aforge-as-worker` @`25cb52250` (worktree `~/af-trial`), with the two leaf
records beside it and the analysis in that directory's `README.md`. The live
store is `/home/santosh/.aforge/runs/aforge-do-1923110067`. The sentence, as a
person saw it:

```
gate: refused — CLAUDE.md — The only file this run changed is CLAUDE.md; no
leaf-harness change was implemented. There is no per-round counter of identical
timed-out tool calls, no new stop-reason kind distinct from the wall and wire
failure carrying command and count, no exported threshold constant, no
scripted-brain acceptance test, and no docs/changes/unreleased/ entry for PR
510. The worker's own message says it stopped 'before implementing'. The file of
record, CLAUDE.md, performs none of the required behaviour. — no more work could
be started on it  41m17s
```

**The verdict is right and almost every clause is true.** The run delivered
nothing. But it changed NO files — `git status` was empty, and both leaf records
show zero `write` calls, zero `edit` calls and no shell redirect across 156
commands. So the refusal opens on something that did not happen, and closes on a
sentence BUILT from it.

`engine:` why the gate believed a file changed. Not ours, and being filed
separately — **and the wording is wrong independently of it.** Even with the
invented fact removed, the three faults below stand.

`shows:` three, and the fix for all of them is one shape.

1. **It leads with its weakest claim and buries its strongest.** The true,
   enumerated, actionable part — what is MISSING — sits in the middle of ninety
   words. A person skims the first clause, believes a file was touched, and goes
   looking for a diff that does not exist.
2. **`the file of record` is machinery vocabulary**, and it is load-bearing in
   the one sentence a skimmer reads twice. It names nothing a person can open.
3. **`— no more work could be started on it` is the outcome**, and it is the
   least prominent thing in the line, tacked on after an em-dash at the end.

**THE SHAPE TO WRITE IT IN IS ALREADY ON THE NEXT LINE.** Directly underneath,
the same refusal says `no check exercises — <the check> — and 27 more`, and the
trial's own reading is that this was the only part it could act on without going
digging. Fact, then the quoted thing, then the count. So:

- **Lead with the outcome.** `refused` and what that means for the work, first.
- **Then the strongest TRUE fact**, which is what is absent — quote ONE of them
  whole and count the rest, exactly as the line below does. A reader who wants
  the other four goes to the record; a reader who wants to know whether to look
  at all is finished in one line.
- **State the tree as a fact, never as a premise.** `no files changed` is a
  thing to check and report, not a clause to build an argument on top of.
- **No `file of record`.**

Which gives something of this shape, and a lane taking this row should treat it
as the target rather than the wording:

```
gate: refused — nothing the brief asked for is there — no per-round counter of
identical timed-out tool calls — and 4 more · no files changed · 41m17s
```

— sev: high

### Six specimens, and what they settle

The trial went looking for more rather than letting one line stand for the
class. **Provenance is not uniform and the row must not pretend it is.** `C`, `D` and
the twice-refused pair have exact `file:line` and were read in context. `A`, `B`
and `E` are verbatim text recovered by a `grep -rh` across several trees, and
**they have no location and never will**: two later passes over a SUPERSET of
the scope that first matched them found nothing. They were on this disk when
they were read and were gone about thirty minutes later — almost certainly a
transient run store that got reaped underneath them.

So they are **SHAPE ONLY, permanently** — not "until the paths land". The text
is verbatim and stands; nobody can go back and read the stream around it, so it
cannot carry a citation and must not be asked to.

**AND THAT IS THE LESSON, not a footnote to it.** The same thing would have
happened to the `CLAUDE.md` specimen if it had been described rather than copied
out, and the reason the engine halves could be filed at all is that somebody
pinned a store. Evidence that lives only in a run store is evidence with a
half-life. Copy it out and commit it the moment you read it.

**THE BRANCH THE FIRST SPECIMEN GOT WRONG — a file genuinely was written:**

```
A. gate: refused — register.txt — The deliverable changed the tree (register.txt
was written) but the request explicitly said 'Do not write, create or edit any
file'. This is a direct violation of the request's instruction. — no more work
could be started on it  6m49s
```

**This settles rule 3, and it settles it on both sides.** `A` states the tree as
a FACT and builds the violation on the REQUEST. The `CLAUDE.md` specimen states
the tree as a PREMISE and builds the verdict on the file. Same gate, same
grammar, and the difference between a sentence that survives its first clause
being wrong and one that collapses with it. Neither `A` nor `B` says `file of
record`, so that phrase is not the gate's habit — it is one line's.

**THREE SHAPES, and a lane must write one sentence that serves all three:**

- **with a leading file token** (`A`, `B`) — a file really was written, and the
  comparison is against the request.
- **with no file at all** (`C`, `D`) — the deliverable itself is the problem
  (`The deliverable is a plan for what to run next, not the finished work
  itself`). Same grammar minus the leading token, and the sentence carries its
  own subject without trouble.
- **`E` IS NOT A REFUSAL, and I had this wrong in an earlier revision of this
  file.** Its body appears in the trial's own log under a DIFFERENT LABEL, word
  for word (`510-run1-stream.log:73`, committed and pushed, so the body has a
  permanent citation even though the `gate: refused —` rendering of it does
  not):

  ```
  nothing is changing — carrying on has stopped changing anything — twice over
  now, nothing was written or altered — so this is handed over as it stands
  26m1s
  ```

  against the reaped rendering:

  ```
  gate: refused — carrying on has stopped changing anything — twice over now,
  nothing was written or altered — so this is handed over as it stands. Nothing
  further was started.
  ```

  **Same body, two labels, and only one of them leads with the outcome.** So `E`
  does not belong in the list above at all: at the gate it opens `gate: refused`
  like every other specimen. The outcome-first virtue was never the sentence's —
  it belongs to the label `nothing is changing`, which one lane applies and the
  other does not.

  **That is a better row than the one it replaces**, and it changes what a lane
  is being asked to do. `lead with the outcome` is not a rule to invent and
  argue for: THIS PRODUCT ALREADY OWNS AN OUTCOME-FIRST LABEL FOR THIS EXACT
  BODY and does not use it at the gate. The work is to apply consistently
  something that already happens half the time.

**AND A REAL OUTCOME-FIRST LINE, citable, from the same run as the worst
specimen** (`510-run1-stream.log:116`, its closing line):

```
partial — no relevant progress in 2 rounds; last change: nothing this job is
about has changed  45m0s
```

The verdict first, then the reason, then the state of the tree as a plain fact.
**Argue the house rule from this one run and nothing else.** At `41m17s` the
same stream spends ninety words opening on a claim about a file that is not
true; at `45m0s` it spends sixteen and opens on the verdict. Four minutes apart,
one log, and a reader can see the whole argument without being asked to trust
six sources of differing provenance.

**TWO CONSTRAINTS THE SPECIMENS ADD, and both would have been missed:**

1. **A run can be refused MORE THAN ONCE and still exit 0.** Two refusals at
   30m11s and 50m22s on one run, then exit 0
   (`…/scratchpad/s3sec.md:61-62`). So whatever is written has to read sensibly
   THE THIRD TIME A PERSON SEES IT in one stream — which is an argument against
   any long sentence, and for the `fact — quoted thing — count` shape that can
   be skimmed at a glance and told apart from its neighbours.
2. **The header can disagree with its own sentence.** In `B` the leading token
   names one file while the prose names two. That is its own small defect and it
   is filed as `N7`.

**N2 — `do --json` carries no `exit_code`, and the stream never prints one.**
`shows:` verified on this branch — the envelope has `ok`, `stop`, `error` and
fifteen other keys, and no number. `stop` is the better field for a program to
branch on and it is what the manual teaches, but a `--json` object CAPTURED TO A
FILE has lost the process's exit status, and the exit ladder is the thing every
script's caller compares. The mapping is deterministic (`exitFor`), so the number
is free.
`engine:` none. This one is entirely ours.
— sev: med

**N3 — a refused run leaves nothing on the tree despite `-w`, and to see what it
understood you must already know `aforge why` and the store path.**
`shows:` the refusal is a dead end. Something that stops without writing has to
say where its record is and how to read it — the developer trial has already
shown twice that a person will go hunting in the wrong directory rather than
guess. This is the same shape as `--debug` naming a folder nobody could find,
which is closed in #518, and the fix is the same one: name the door in the
sentence.
`engine:` whether a refused run ought to leave something on the tree at all.
— sev: high

**N4 — the leaf contract tells the worker to read a file, and the worker has no
read tool.**

**VERIFIED, verbatim, and the exchange is the whole row.** The contract is
composed at RUNTIME and exists in the tree nowhere — I looked, and there is no
such string in `internal/` or `cmd/` — so this is preserved only because the
trial copied it out: `bench/worker-trial/captures/` on `trial/aforge-as-worker`,
**pushed to origin at `a4500aadf`**, which is why it survives its store when `A`,
`B` and `E` did not.

The brief handed to node `task-2` at turn 0 opens:

```
Read CLAUDE.md first; then locate the leaf harness round loop under internal/
(search for where tool timeout errors are handled and where existing stop
reasons like the wall are declared …
```

and the first thing that worker did with it:

```
turn 1 · read ←  {"path": "CLAUDE.md"}
turn 1 · read →  no tool named "read". Available: sh, job, write, edit, web,
                 recall, capabilities (loads: media, documents)
```

`shows:` **the refusal itself is well built** — it names what is missing and
then lists what there is, which is more than most of this product's refusals
manage, and it is the shape the rest of them should copy. What is wrong is
upstream of it: a brief written from one list and a belt built from another,
when this repository's own law is that a capability which cannot work is ABSENT
rather than broken. The surface half is whether a person watching the stream is
ever shown that a worker was told to do something it had no way of doing — a
person reading this run saw a `✓`, not this.

`engine:` the brief and the belt disagreeing — **filed as #538**, so do not
re-file it.
— sev: med

**N5 — a token-budget overrun of 1.7x goes unnoticed until the settle line.**
`shows:` the budget is a limit somebody SET, so passing it is news, and news
arrives when it happens rather than in the receipt afterwards. The live status
line already carries context and spend and already has the reservation machinery
(#518's `costCell`) to grow a segment without shoving the cluster sideways.
`engine:` why an overrun of that size is possible.
— sev: high

**N6 — the settle line hides that about 12% of spend went on 429 retries and
hedge waste.**
`shows:` one number for "what this cost" and no way to tell work from waste. A
person tuning their spend cannot act on a figure that mixes them, and the
emptiness law is no help here because the waste is not zero — it is real money
with a cause. The question to answer first is a design one and it belongs to
whoever takes the row: is retry-and-hedge waste a SECOND FIGURE beside the
spend, or a clause under it, or a fact the spend page owns rather than the
settle line?
`engine:` the retry and hedge behaviour itself.
— sev: med

**N7 — a refusal's leading token names one file while its own prose names two.**
`shows:` specimen `B` leads with `CONVENTIONS.md` and then says `wrote
CONVENTIONS.md and register.txt`. The token is the part a person reads first and
the part a stream is skimmed by, so a header that disagrees with its sentence
sends somebody to the wrong file. Either the token carries the count the way
every other folded thing on this surface does, or it names the deliverable
rather than one of its files.
`engine:` none.
— sev: med

**N8 — a node reported success having written nothing, and the gate
contradicted it twenty-eight minutes later.**

**ATTRIBUTION VERIFIED rather than assumed** — the trial went back and checked
which node the tick belonged to instead of trusting its own summary:

```
run-510.log:46   ✓ Timeout stop reason          (linear) 13m43s
run-510.log:47   ▶ Implement fix 510            (linear) 13m43s
```

The `✓` is node `task-2`, whose record is nineteen turns, fifty-two `sh` calls,
one failed `read`, **zero `write`, zero `edit`**, and no `cat >`, `tee`, `>>`,
heredoc, `sed -i` or `patch` in any of the fifty-two commands. The gate refused
the same work at 41m17s. Both nodes in that run wrote nothing, so the row would
hold either way — but the figures now attach to the ticked node and not to its
sibling.

**And the `✓` was not the planner believing the work was done.** The sibling
node started at 13m43s — the same instant — with a contract that CORRECTS the
first one, naming the file, the function and the constant the first was left to
find:

```
Start in internal/exec/linear.go: read the runTools/executeTool path and the
StopReason definitions near StopDeadline/StopCancelled, then write the change
there first, before any test …
```

So at the moment the surface drew a `✓`, the layer above it was visibly acting
on the opposite conclusion.

`shows:` a `✓` is the strongest thing this surface can say, and a person who
reads one stops watching. Two marks about one piece of work disagreeing half an
hour apart is worse than either being wrong on its own. The row is what a `✓` is
ALLOWED to mean — whether it may be drawn for a node that produced nothing, and
whether it should survive being contradicted rather than sitting in the
scrollback as the last word a skimmer saw.

`engine:` why the node reported success.
*Frame:* `bench/worker-trial/captures/` on `trial/aforge-as-worker` @`a4500aadf`.
— sev: high

---

## What the last wave would tell this one

- **Reproduce before editing.** Three rows in #518 named the wrong thing — a
  glyph that would only be that letter if the product still had its old name, an
  emoji class that never caused the bend blamed on it, and a mechanism I wrote
  down myself and got wrong. All three were caught by looking at the code or the
  frame instead of the row.
- **Revert every fix and watch its test fail.** Six tests in #518 passed against
  the very defect they named. Nothing else caught any of them.
- **A cold user is worth more than an audit.** Seven findings in one afternoon,
  four of them real, none of which twelve audits had found — because everybody
  else already knew what the program meant to do. Run the trial BEFORE the wave.
- **Copy the evidence out the moment you read it, and commit it.** Three of the
  six refusal specimens in this file have no location and never will: they were
  on this disk when they were read and gone half an hour later, because they
  lived in a run store that got reaped. The ones that survived did so because
  somebody pinned them to a branch and pushed it. A description of a frame is
  not a frame, and a path into a run store is not a citation — it is a citation
  with a half-life.
