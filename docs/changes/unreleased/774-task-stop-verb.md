---
kind: fixed
title: The model's task doors now reach the same work yours do — stop, settle, and a bare id
pr: 774
surface: [chat, engine, docs]
invalidates:
  - "The `tasks` tool could read, steer, forward, continue and settle a task and had no way to END one. It has `stop`: with an `id` it cancels that task through `Agent.Cancel`, the same door `/stop`, `x` and `Stop task…` take."
  - "Asked to stop a task, the model's nearest move was `say` — a line into the running task. That was never a stop, and the schema and the prompt now say so; a worker that answers such a line by delivering nothing is still checked and still sent back for a repair round."
  - "`Agent.Cancel(id)` was the only stop door. `Agent.CancelWithReason(id, why)` is beside it; `Cancel` is that with no words. A reason is written onto the node as `stopped: <the words>` and reaches only a task — a run, a sub-harness run and a job have no report to carry one."
  - "A stop aimed at work that had already settled answered `has already finished; there is nothing to stop`. Through the tool it now answers with what that task IS — `done`, `stopped`, `your call` — in the words the person's own screen shows, and never as an error."
  - "The `tasks` tool resolved a bare id with `LookupTask` against the whole project index, so \"task 2\" meant the NEWEST task 2 any conversation in the project had ever run. A number now means this conversation's own task first (`Agent.taskByToken`); a slug still means the newest row that wears it."
  - "`let aforge decide` could hand a decision the model's own `tasks … resolve` was then refused for — \"there is no graph left to settle it in\", naming another conversation's worktree — on every recovered task. The person's door and the model's reach the same node now."
  - "Pressing `let aforge decide` twice handed the model the same decision twice, in two identical lines. The second press answers `already handed to aforge` (`session.ErrTaskHandedOver`) and enqueues nothing; the card keeps reading `handed to aforge for this one` rather than `already answered`."
  - "A merge round in flight was in-memory only, so a session killed under one came back saying nothing about it. `taskRecord.Resolving` records the claim, the resume says `1 your call (its merge round was cut)`, the claim is dropped so the next press is taken — and nothing restarts the round."
---

Four defects with one shape: the model's doors onto a task were narrower than the
person's, so a promise the surface made could not be kept.

The chat had no stop verb, so asked to stop task 2 it said "stop, do not continue"
INTO the task — which the worker complied with by delivering nothing, and the check
read as an ordinary unfinished run: `closing gaps · round 1 of 1`, still spending.
`let aforge decide` handed the model a decision its own settle door then refused,
because a bare id was looked up across every conversation in the project and matched
a stranger's newer task 2. A second press on that card sent a second identical
instruction. And a merge round that died with its process left a card that read as
though the person had never pressed anything.
