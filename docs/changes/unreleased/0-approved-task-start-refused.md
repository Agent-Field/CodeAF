---
kind: fixed
title: an approved task whose run did not start settles failed instead of staying queued
pr: 0
surface: [chat, engine, docs]
invalidates:
  - "An approved proposal whose run the run road refused (the store's lineage rule, a copy that would not cut, a program that could not start, another folder's run underway) told only the model `task N did not start`. The card's last word was the approval, which every surface reads as queued work, so `codeaf app`'s live task list and `tasks.list` kept a queued task the graph never had. The engine now publishes that task's row as `failed` with the `did not start` sentence as its report, live to every watcher, on the checkpoint and the task index, so a reopened conversation lists it failed too."
  - "`codeaf app` projected a failed task's `result` from the notice's result only, so a row that carried only a report (an unstarted hand-off, a held run whose preparation failed) read failed with no reason. A failed task with no result now carries its report, as the engine's own `tasks.get` read already did."
---

Was true: the clock expiring on a proposal for task 2 that depended on a run
already underway as task 1 restated the card approved, the join made task 2 a
child of task 1, the plan store refused the hard dependency across its own
containment lineage, and nothing after that named task 2. The app heard queued
and kept it forever.

Is true now: the same refusal settles task 2 `failed` with `task 2 did not
start: task "2" has hard dependency "1" across its containment lineage. Nothing
is running for it and nothing was started in its place.` Whether a dependency on
the run's own root should wait for that run instead of joining it is a separate
question this change does not answer.
