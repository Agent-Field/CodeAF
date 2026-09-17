# Why the bash belt lost — INVESTIGATION

The first grid said the bash belt was worse than the pi belt on every row: more
steps, more money, and a landing that ended `failed` while its tree was green.
The python planner this belt is a port of beats mini-swe everywhere, so the
shape of the loop was never the reason. Two faults in the port were.

Both are fixed on `spark/bash-task-loop`; this file is the diagnosis they came
from, with the evidence in the logs the grid wrote.

## Cause A — the belt told the model two opposite things

`prompts/system.md` carries the batching law, in caps on line 74:

> ASK FOR EVERYTHING YOU NEED IN ONE BREATH. Reads, searches and checks that do
> not depend on each other go out as ONE batch of calls, never one per turn.

The bash belt's envelope runs **exactly one** bash call per response and refuses
anything else. The page swap replaced the `## Specialized Tools` section, and
that sentence sits in `## General` — outside the swapped section — so every bash
worker was sent both laws at once, and obeyed the louder one.

Evidence, from the grid's journals: B's worker and its repair worker each sent
multi-call batches, refused `[not run]`, **eight rejections across the family**.
Four refusals in a row end a run, and every B row ended there.

**Fix.** `prompt.go`'s `bashPageSubstitutions` now rewrites that sentence on the
bash page: "ONE ACTION PER RESPONSE", the one law the envelope enforces.
`TestBashBelt...WorkerPrompt` asserts the page never carries "ONE batch of
calls".

## Cause B — the auditor judged a restore, not the work

CodeAF's audit is a second node that verifies a **clean restore** of the node's
tree. That restore overlays only the pi-tool write ledger — the record `write`
and `edit` keep. A bash worker's writes go through the shell and populate
nothing, so the auditor read a pristine tree no matter what the worker did.

Evidence, the two audits of the same cell:

- pi belt: **VERIFIED — read the staged diff showing `return lo` → `return hi`
  … test file is unmodified.**
- bash belt: **REFUTED — no diff, no staged change, no untracked file; the
  working tree is identical to the starting commit.**

And the tree itself: `trees/1/clamp.go` line 14 read `return hi` — the fix was
there, and the bench's grader (reading the tree) scored the row **pass**. The
auditor never saw it.

**Fix.** The bash belt keeps no auditor. Its task is the planner's loop and
nothing else: one worker, one bash, one landing. The switch is read once
(`bashBeltAsked`) and consulted at `workTaskNode`'s gate, which lands the
node's own account marked unaudited.

One answer the audit was also giving had to be kept: telling a run that
*finished* from one that *gave up*. Dropping the audit naively let a node that
never ran a valid action land `done`. So the runner now keeps that answer
itself — if the run recorded an ending, it lands through the stopped road
(failed, branch kept), exactly as a refused audit left it.

## Still open, smaller

- A repair round can re-run without converging; the design's one-repair cap
  bounds it, but the grid shows it costing steps.
- One `sed -i` idiom flag on B (self-corrected) — the doctrine page teaches
  `grep -c` before `sed`; the model skipped the count once.
- `changed=34` on a three-file fixture: the changed-files count over-counts. A
  bench diagnostic only; it does not touch pass, cost, steps or wall.
