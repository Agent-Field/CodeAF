---
kind: fixed
title: a remainder is the original assignment plus a finding, and it divides on its size
pr: 544
surface: [engine]
invalidates:
  - "a remainder's goal is composed from the brief of the node it is continuing — no longer true, and it was how a remainder of a remainder wrapped the previous round's whole text. `internal/resident/overrun.go` and its cooperative twin write `originalAssignment(node.Brief)`, which peels the exact preambles (`OverrunPreamble`, `CooperativePreamble`) and keeps the innermost original assignment. The finding for the current round is added once, as it always was."
  - "a remainder the spine answers with several stages is planned in full — no longer true, and the sentence at the foot of this entry's first version said exactly that. For an `Undivided` build the stages are folded into the one node (`plan.foldedStage`) and the ruler's reach decides; only past reach does the pipeline run. `Options.Undivided` no longer requires `len(choice.Stages) == 1`."
  - "a remainder whose words list its pieces is divided into them — no longer true. A remainder divides only when the ruler puts it past one worker; `internal/plan/enumerated.go`'s `admitsEnumeratedPieces(node, options)` returns false for `Undivided`, and every seam that could divide on a node's words asks through it."
  - "making remainder `MaxDepth` 1 meant both size and enumerated wording could send a remainder through the full division pipeline — no longer true. `MaxDepth` stays 1 so an oversized remainder can divide into parts or stages, but the `Undivided` shortcut, `JudgeSplit`, and `dividesInTime` cannot divide one merely because its text names pieces."
---

**A REMAINDER IS THE SAME PIECE WITH A FINDING, AND ITS SIZE IS THE SIZE OF THE
FINDING RATHER THAN OF THE GOAL.** Three things divided one anyway, and two
canary cells measured them.

**The wrapper nested.** A remainder's goal is a preamble, then `The original
assignment:`, then the node's brief, then this round's criterion, partial, state
and finding. The node a remainder plans is handed that whole composed text as its
brief — `plan.Build`'s undivided shortcut writes `Brief: goal` — so the *second*
remainder wrapped the *first* remainder's text. On the acceptance cell for this
PR (tox issue #4031, on 88933fd23) round two's goal read `Finish work… The
original assignment: Finish work… The original assignment: Implement issue #4031
…`: **28,468 characters, two wrappers, the issue text twice over and the current
finding buried at the end**. The spine planned the issue again rather than the
finding, and every prompt of that round grew with the goal — **the round-two gate
call cost 116,674 prompt tokens.** Now `originalAssignment` peels the exact
preambles, one round per turn, and cuts each at the first block that ends an
assignment; `remainderSections` is the one place that says where an assignment
stops, and a block added to either wrapper without an entry there fails a test.
A brief that never went through either wrapper comes back unchanged, which is
every fresh job.

**The spine's stage count divided it.** The undivided shortcut required exactly
one stage; anything else went past it into the full pipeline with the ruler never
asked. On that same cell the second remainder — carrying `2 unexercised
behaviours` — came back from the spine as three stages, "Check unit tests",
"Check integration test", "Verify no traceback". Three steps one worker would
take. The shortcut was skipped and the round fanned out, bound, sized, audited,
briefed and wrote **seven contracts**; the cell's spend doubled. The spine's
shape is advice for a fresh plan and a **list** for a remainder, so the stages are
now folded into the one node and the ruler is asked its one reach question about
the whole. Past reach, the pipeline runs as before and the stages inform the
fan-out untouched.

**Its own words divided it.** After #424 raised remainder `MaxDepth` from 0 to 1,
a remainder read its own words first: one that listed eight failing tests "named
several pieces", fell through to the full pipeline, was drawn as six and then
eight leaves, and the canary's `tox-dev-tox-4031-do` cell on dev 79515001 ran
fourteen parallel repair leaves with revise ×14 to the wall. Across nine cells
the do-door spend doubled from $0.46 to $0.95 while quality stayed flat.
`admitsEnumeratedPieces(node, options)` is the one place that decides which builds
may divide from the pieces written on a node. It refuses `Undivided`, and the
three seams — the `Undivided` shortcut in `plan.go`, `JudgeSplit` in `expand.go`,
and `dividesInTime` in `sequence.go` — ask through it.

**What did not change.** A fresh plan that names three lanes still divides, and a
fresh build with three spine stages still goes through fan-out, bind and sizing.
A remainder measured past one worker still divides into parts or stages.
`MaxDepth` remains 1, the split gate remains off everywhere, and the criterion,
partial, state, files, record and transcript still travel on every round.
