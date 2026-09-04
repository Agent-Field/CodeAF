---
kind: fixed
title: a run that was handed a list answers the list, item by item, at every ending
pr: 640
surface: [engine, chat, docs]
invalidates:
  - "`store.AcceptanceFor` was written to read a run's own checklist back out of the journal and had NO CALLER anywhere in the tree outside its own test. The checklist was journaled before the work began and read by nobody until the delivery gate, so a run that stopped before reaching a gate settled none of it and said none of it. Every ending reads it back now, and the closing words account for each point as `answered`, `not answered` or `not reached`."
  - "A stopped node recorded its cause, the whole transport error and the files it left behind, and nothing about what was asked for. A run handed four numbered faults answered three, ran out of clock, and handed back `context deadline exceeded` and one path — so the next hand-off had to read the tree to work out which of the four was still open. The account now rides that node's own recorded error, below the file list, under the heading `What was asked for, and what happened to each:`."
  - "`aforge do --json` had no field for the request's own checklist, so a rig comparing runs could not tell a list that was answered from one that was dropped without reading prose. It has `checklist` — one `{behaviour, state, why}` row per point, whole and never clipped, present on exactly the runs whose journal carried a checklist. The person's block is bounded and counts the lines it cannot show; the machine's list is not bounded at all."
  - "`answered` may be said by ONE reader and it is the delivery gate's own mapping of points onto checks. Nothing else in a run's record is entitled to the word: a file the run changed is not evidence the behaviour holds, and a point naming a file the run never wrote reads `not answered: nothing this run wrote is prose.go` — a statement about the run's files, which is a fact the ending holds, and not about the behaviour, which is not."
  - "The chat corpus explained what an acceptance checklist is and stopped there, so the block a person now meets at the end of a stopped run had no page behind it. `adaptive-runs.md` carries the section, and it states the one thing the words alone do not: `not reached` means nothing in the run's record settles the point, never that the work was skipped."
---

Nothing here spends anything, and that is what makes it affordable on every
ending rather than the tidy ones. The points are journaled, the gate's mapping is
journaled, the run's own file record is already in hand — no model call, no
provider call, no second reading of the tree. A run that stops early is entitled
to stop early; it was never entitled to hand back a file list and let the reader
work out which of four things it had done.

One shape is deliberately not covered and is written down where the reading
happens: a job the planner breaks into several leaves journals a checklist
against each leaf and none against the root that settles them, so `--json`'s
`checklist` is empty there. The account still reaches the person by the other
road, on the failing leaf's own error.
