---
kind: fixed
title: a run's row on the tasks place is the store's task, wears its state and opens its page
pr: 1359
surface: [chat, engine]
invalidates:
  - "A `/task` on the run engine drew a row that wore `working` and whose `enter` opened an empty room. The row was the one the run's door publishes for work the graph holds no node for, and the tasks place had dropped the store's own row for it because the two share a title. The store's row is drawn now: it wears the word its store status maps to (`running`, `queued`, `done`, `incomplete`, `stopped`, `your call`) and `enter` opens the task's page — the work order, the notes, and the trajectory of every command its worker ran."
  - "internal/session's TaskNotice carried no way to tell a row the store answers for from a node of the session's own tree, and internal/tui3's planRowShown said so in its own comment: THIS SURFACE CANNOT SEE THE STORE ID. TaskNotice.PlanTask now names the store task a run's row is, in planStoreID's spelling — the same one PlanTaskRow.ID carries and PlanTaskPage is asked for — set by the door that mints both halves, carried forward by publishRunRow, and kept in the checkpoint. The title match is left to the node road, where a plan-born node's store ids are not the row's number and the node is the half with a room behind it."
---

Found by a real-model tmux drive, not by a unit test: this shipped with
`internal/tui3` and `internal/session` green, because every piece worked and the
place drew the wrong one of two rows. The defect was already reachable under
`CODEAF_TASK_BELT=bash`; making the harness the default belt only promotes it
from opt-in to default, which is why it blocks that change.
