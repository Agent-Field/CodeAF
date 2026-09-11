---
kind: fixed
title: A conversation that ran a quick task reopens with its task graph
pr: 869
surface: [engine, chat]
invalidates:
  - "A quick task's empty acceptance was harmless. It was not: the checkpoint's validator refused any node with no acceptance, and its refusal is whole-document, so ONE quick task anywhere in a conversation's history threw the WHOLE task graph away on the way back in — every finished row, every piece of running work, the whole family — leaving one line in a log file saying the checkpoint was corrupt. Now whether a node owes an acceptance is a property of its KIND, declared once in `kindsWithoutAcceptance` and read through `acceptanceHolds`; a quick node's blank is the answer, and everything else is refused exactly as strictly as before."
  - "A quick node that was WAITING ITS TURN when the window closed used to be unreachable (the whole file was refused). Now the file loads, and that node SETTLES rather than going back on the frontier: its list is not in the checkpoint, and `runTaskNode` dispatches on that list, so a resumed one would have been handed to an ordinary worker with a copy of the folder, a branch and a check. Its row reads `the quick task never started before aforge closed, and it does not resume — ask for it again`. A quick node that was RUNNING settles as it always did."
  - "The manual said nothing about what a quick task leaves behind a restart. `internal/manual/chat/tasks.md` now has *A quick task after a restart*, per state, and `how-tasks-run.md`'s row 5b names the quick exception beside the design's."
---

Found on a real checkpoint: four `quick` nodes with `"acceptance": ""` beside three
ordinary ones, and a conversation that reopened with nothing in its column. The fix is
not a placeholder "DONE WHEN" for quick nodes — nothing would ever read one — but a
single declaration of which kinds carry an acceptance, read by the builder that leaves
the field empty and by the validator that refuses a record without one, with a law test
over the builders so the two cannot drift apart again.

The record's own shape — carrying a quick node's line, items, ticks and files so a
queued one can resume as a quick node rather than settling — is a separate change.
