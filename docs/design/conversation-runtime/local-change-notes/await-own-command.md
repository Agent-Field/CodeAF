---
kind: fixed
title: a reply whose only remainder is a command it started for you no longer becomes a task
surface: [chat, engine, docs]
invalidates:
  - "A turn that had finished every artifact you asked for and was left only waiting on a background command it started could still be converted into a task whose whole brief was \"wait for the command, then verify the files\". It is not converted any more: the handoff ask now offers the conversation's own live background commands and fork hands by number, the model may answer that the remainder is only those endings, and the runtime verifies the numbers were offered, are still running, and that the request has not moved. A running job on its own still suppresses nothing — a remainder with real work in it hands over exactly as before."
---

Local lane note; root folds this behaviour into its own 653 entry. No pull
request has been opened for it.

calibration-02 cell 018-revision-midwork-aforge: the write seam fired after
report.csv was written and report.md removed, the mark reader drew `(waiting)`,
and task 2 was admitted to wait for `./slow-build.sh` — which woke the
conversation with the right answer 42 seconds later anyway. The task spawned a
repair child and an audit child and the cell hit its 180-second cap.
