---
kind: changed
title: Every task opens the one task room, on either engine
pr: 1485
surface: [chat, docs]
invalidates:
  - A run's task opened a separate store-backed page (taskSheetPlan, taskPlanFrame); it now opens the task room, read from the store.
  - The run's page had no transcript or work tab; every task room now has both, switched with tab over an empty box.
  - The run's page took p to hold a part and cancelled a part directly; in the room p is a letter and x raises the stop card.
  - Tokens and the model were said not to be in the plan store; the room's head reads both from the spend ledger.
  - The tasks place's enter and the run's tab opened the plan page; both open the task room now.
---

A task on the run engine now opens the same task room as any other task. Its transcript is
the worker's steps drawn as the room's own calls, its work tab is the run's working copy
difference, its box leaves a note on the task, and `x` stops it. The head shows the model
and tokens when the store's ledger records them. Every task room gains `transcript` and
`work` tabs.
