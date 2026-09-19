---
kind: fixed
title: a parked task is finished only by a worker woken for it
pr: 1271
surface: [engine]
invalidates:
  - "A worker that parked its task with `plandb wait` could still finish it. A round the worker had already started still ends its tool, and when that tool was the task's own `plandb done` the plan store admitted it, because the claim a park releases was never what the store checked for the run's own task."
  - "On the run's own task that closed a run over work nobody had reviewed: the root parked on its child, the child wrote its own done, the root's late finish landed, and the run answered `done` while the child's worker was still out. Five loaded runs in four hundred did this and no quiet one did. Every one left a task that was done and still flagged waiting."
  - "The store now refuses an ending on a parked task, on all three roads that write one: a worker's done, a worker's fail, and the run's own completion of its root. The late worker hears `task \"<id>\" is waiting and can only be finished after it is woken`. Nothing else changes: the task stays parked, and the run wakes it the ordinary way when its wait is over. Done together with waiting is a pair no road in the store can write."
---

A person sees no new word. The run that used to end early now reviews the child,
wakes its root, and answers with what the woken worker said.
