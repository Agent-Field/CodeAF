---
kind: changed
title: the split gate is off by default, and the count and the plan's sizing stay as pins
pr: 435
surface: [engine]
invalidates:
  - "the split gate is armed unless somebody wrote `AFORGE_SPLITGATE=0`, and a division has to name six separate items to stand — no longer true: the gate is OFF unless somebody pins it ON. With nothing set, every division the planner drew and every division a worker asks for is kept. `AFORGE_SPLITGATE=1` puts the six-item count back, `judgment` asks the plan's own sizing and falls back to that count, `0` is off spelled out, and anything unrecognised is off — the reverse of the old rule, because off is now the default and a typo must not put a floor back under somebody's divisions."
  - "`AFORGE_SPLITGATE=lanes` counts the shapes a division is written down in and takes a written-out division at its word — gone. It was one arm of the experiment, it helped one planner and hurt another, and internal/splitgate/lanes.go, `Lanes`, `ExplicitDivision` and `Count` were deleted with it. The word now reads as off like any other unrecognised value."
  - "the six-item floor is what refuses a narrow division in the v3 engine — only where a run pinned the gate on. `divide_work`'s own description no longer promises the floor on an unpinned binary, because a model reasons from that sentence and would talk itself out of a division nobody was going to refuse. The floor still arms the verb: work whose text names fewer than six items is not handed `divide_work` at all, which did not change."
  - "internal/splitgate.Count is the current mode's counting — gone with the mode that needed it. There is one counting again, `Items`, and callers read it directly; what a pin changes is who decides, not what is counted."
---

The gate shipped armed, with a six-item floor under every division, and #418
said the floor folds real divisions: a brief that names three lanes over one
file counts zero items and is refused. Two repairs were proposed and neither was
argued to a conclusion, so PR #435 made every reading reachable from one binary
and the choice was handed to a designed experiment — four planner arms against
four readings of this gate, 273 judged draws on the plan door.

**The experiment picked the gate off.** The front it drew is the stages planner
with the gate having no say: the same quality as the best armed cell, a third of
its unrecoverable draws, and the lowest cost of the three cells that tie at the
top. Every armed reading paid for the narrow divisions it refused by folding
wide ones. `docs/design/plan-gate-doe/REPORT.md` is the report and the decision.

So the default moved, and it moved on measurement rather than on argument. The
count is not wrong everywhere and is kept as `AFORGE_SPLITGATE=1`; the plan's
own sizing is kept as `judgment`, which can only add keeps to that count. The
counting arm the experiment ran against them lost and its code is deleted rather
than left as a fourth thing a person could type.

What did not change: the counting itself, the sentence a folded node carries
verbatim (#418 quotes it), the free-lane test, the mastermind's reading of every
division, and which work is offered the split at all — a worker is still only
handed `divide_work` where something read the work as wide, and the brief's own
count is still one of the three readings that does that.
