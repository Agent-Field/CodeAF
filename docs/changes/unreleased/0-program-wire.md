---
kind: added
title: a specialist's task page carries its stages, actions and ending on the app wire
pr: 0
surface: [engine]
invalidates:
  - "Was true: `appwire.TaskPage` was `{task, result, files, log, notes, brief}` and a specialist's stages, shown actions, ceiling, models, listening state and ending facts reached only the terminal (`session.PlanProgram`); `session.AppTask` read the page and threw the program away. Is true now: `TaskPage.program` (`appwire.TaskProgram`) is present for a task a specialist was handed, built from that same page read (no second read, no new host method; `session.AppTaskView.Program` carries it over `--host`). Stages are the hello's own list with the person's word for each (a wordless stage keeps the word before it); senior-dev's 15 stages are 7 consecutive words, not 6, so the app groups consecutive equal words and nothing is forced to six. `last_stage` is the newest stage record's name. Actions are the newest 60 with `earlier_actions` counting the rest; `ending` is present only while the log's last line is the ending. NOT carried: attempt number, `auto` or earlier runs (the record is the latest process, not a history), a stage index or percent, a step's `detail`, and any file handle for `reports` (display names only). Absent `program` is also what an engine that predates the field sends."
---

Baseline note: this is PR 0, a preserved local milestone, not ready for a public PR.
