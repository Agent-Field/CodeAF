---
kind: fixed
title: stop it ends a run, from the card, from the run's own page, and from the chat
pr: 1250
surface: [chat, engine]
invalidates:
  - "A run under the bash belt could not be stopped by anything a person pressed. The stop card's `stop it` answered `there is no task 1 in this session` and the run carried on to its own landing; the run's own page answered `x stop it` and `p pause` with `the harness owns the root task`. Now `stop it` ends the run: every open part is ended, what it was running is cut off, no further model call is made for it, and nothing goes into the folder."
  - "The stop a surface sends for a row (`task:N`) went to the task graph and nowhere else, and a run's rows are not in it. The session now resolves that id to whoever owns the row, the live run first and the graph second, so a window built before this change stops a run through an engine built after it, and the model's own `tasks … stop` ends a run too."
  - "A run was started on a context nothing could cut and kept no cancel. It keeps one now. A stop writes the ending into the run's store first (`plandb.Store.StopRoot`, the runtime's verb for a person's word), then cuts the context, then commits what was made on the run's own branch and gives the copy back."
  - "A stopped run stayed open in its store, so the next `/task` in the same conversation adopted it and picked the stopped work back up. A stopped run is over in the store, and the next `/task` starts a fresh run."
  - "A surface asks for a run's four-line summary again whenever the run's rows move, and a stop moves them, so a stop taken from the run's page was followed about twenty seconds later by one more model call. The engine declines a summary for a run a person stopped; the last reading it had stands."
  - "`p pause` was offered under the run's own task and could only be refused. Nothing holds a whole run: the key is not offered there and is a letter in the note. A hold asked of a run by an older window answers `a run is not held as a whole: hold one of its parts, or stop it`."
  - "`x stop it` on the run's own page sent the store's cancel directly. It now raises the `Stop this task?` card, and the page steps aside for it the way a background job's page does, because the page takes the frame whole and the card is drawn above the message box."
  - "While a task's page was on its way every key was held for it, `ctrl+c` included, and an answer that came back for a tab the person had left kept the hold standing until `esc`. `ctrl+c` is read before the hold and before the page, and every ending of the read ends the hold."
---

Measured on the real binary, hosted, belt on, on 2026-09-19 at trunk 12840eeb5:
the card was raised four times of four and `stop it` stopped nothing four times
of four. `ctrl+c` once, `ctrl+c` twice and `/quit` each close the window and
leave the run going in the engine, three of three each, and its work lands by
itself; THAT IS UNCHANGED HERE and is a separate open question. This change is
about an explicit stop only.

Two laws hold it. In `internal/session`, every function that publishes a row in
a state a stop means something in is listed with the cancel kind that reaches its
owner and the test that proves it by stopping one; a new publisher fails the law
until it has both. In `internal/tui3`, every verb word the foot offers, for every
kind of row a run's store can hold, is pressed against a real store over the real
wire, and none may be refused.
