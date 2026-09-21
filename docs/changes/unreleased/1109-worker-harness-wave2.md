---
kind: added
title: the worker harness wave two — pause, person notes, finish gate, archive, spend, crew arms
pr: 1109
surface: [engine, build]
invalidates:
  - "A task in the plan store could only be cancelled or left running. `plandb task pause` now holds a task and its subtree out of the ready frontier and `task resume` returns it; a paused task is never handed to a worker."
  - "A note on a task carried no author, so a worker could not tell a person's steering from a sibling's record. Notes carry who wrote them, and a store read of what changed since a moment (`Changed`) wakes a waiter instead of leaving it on its clock."
  - "`plandb done` finished a task whose children or hard dependencies were still open. `CanFinish` gates it now and answers with the sentence naming what is still open."
  - "Finished subtrees stayed in the live plan forever, so `overview` and `status` grew with every run. `Archive` moves finished subtrees older than a window out of the live plan; `Archived` reads them back."
  - "The plan store kept no spend. `AddSpend` records one row per model call — task, model, seat, dollars, tokens — so a run's cost is readable from the store and the supervisor's limits have something to read."
  - "`bench/bashloop` ran both belts on one model only. `-seats one|crew` and `-arms A-one,B-one,A-crew,B-crew` run either belt on the profile's own tier rows, and the summary and CSV name each arm's seats and models."
---

Wave 2 of docs/design/worker-harness/DESIGN.md: the plan store gains the
verbs the chat surface and the supervisor need (D4, D5, D8, D11), and the
bench can compare the belts on the crew as well as on one model. Everything
is behind CODEAF_TASK_BELT=bash except the store package and the bench;
the shipped belt is unchanged.
