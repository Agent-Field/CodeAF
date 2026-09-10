---
kind: fixed
title: The chat's own model can stop a task, through the door your stop takes
pr: 774
surface: [chat, engine, docs]
invalidates:
  - "The `tasks` tool could read, steer, forward, continue and settle a task and had no way to END one. It has `stop`: with an `id` it cancels that task through `Agent.Cancel`, the same door `/stop`, `x` and `Stop task…` take."
  - "Asked to stop a task, the model's nearest move was `say` — a line into the running task. That was never a stop, and the schema and the prompt now say so; a worker that answers such a line by delivering nothing is still checked and still sent back for a repair round."
  - "`Agent.Cancel(id)` was the only stop door. `Agent.CancelWithReason(id, why)` is beside it; `Cancel` is that with no words. A reason is written onto the node as `stopped: <the words>` and reaches only a task — a run, a sub-harness run and a job have no report to carry one."
  - "A stop aimed at work that had already settled answered `has already finished; there is nothing to stop`. Through the tool it now answers with what that task IS — `done`, `stopped`, `your call` — in the words the person's own screen shows, and never as an error."
---

The person's stop has always worked; what was missing was the model's. Told to stop
task 2, a conversation said "Stopped in favor of task 3 … Do not continue" into the
task, the worker wrote down that it had been told to stop and delivered nothing, and
the check read that as an ordinary unfinished run — `not done`, `closing gaps · round
1 of 1`, and the task went on working and spending. The repair machinery is untouched:
a stopped node never reaches the check, so what made that happen was the missing verb.
