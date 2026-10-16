---
kind: added
title: codeaf app tasks carry the message that started them and can be dismissed and restored
pr: 0
surface: [engine]
invalidates:
  - "Was true: the engine recorded no block for a task — which message or `/task` started it — so a surface could only guess a grouping from titles or the order tasks were made in, and a failed task could not be hidden without deleting or declining it. Is true now: every task admitted from a typed message (`propose_task`, a node's sub-task, a division's part, a run row, an adaptive run and its workers) or a typed `/task` carries `source` {id, kind `message`|`directive`, text (first 4096 bytes), at}, captured at admission, inherited by children from their parent and never from the conversation's newest message, persisted in tasks.json and read back on `tasks.list`, `task.changed` and `work.graph`. Absent means unknown: records from before this, and turns nobody typed into, have none and nothing is reconstructed."
  - "Was true: there was no way to hide a settled failure. Is true now: `tasks.dismiss` / `tasks.restore` set a reversible `dismissed` flag on a task, a run row or a plan row — persisted with the checkpoint (a failed write rolls back and errors), said as `task.changed` and in `work.graph` (`nodes[].dismissed`, `totals.dismissed`) — and change nothing else: no retry, stop, decline, delete or dispatch. A task that is not a settled failure, or that something live waits on or hangs under, is refused."
---
