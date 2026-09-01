---
kind: fixed
title: only work demotes an answer, and a turn's end settles the whole turn
pr: 190
surface: [chat]
invalidates:
  - "Where no workfold was derived for a turn, the answer hierarchy demoted the answer itself as soon as ANYTHING followed it in that turn — the surface's own notes included — and a demoted block is drawn plain, so the reply came back as the markdown characters it was typed as, indented into the work column (#178). A note is now stepped over exactly as a divider is; only work demotes."
  - "The turns this could reach are narrower than the report suggests, and the narrowness is the useful fact: a turn that derives a fold is classified by the fold and was never at risk. It bit where no fold exists — a group blocked from folding (a task, connect or standing card, a `cancel…` note, a seam), a group with no thought and no call before its answer, and any room, which folds nothing at all."
  - "The settle was a property of the block the stream was last writing into (`app.closeLive`) and rested on an unstated invariant — that an assistant block stops being live exactly when something settles it, kept by a dozen scattered call sites. It is now a property of the TURN BOUNDARY: `app.settleTurn` settles every assistant block of the ending turn, idempotently, from both of the turn's endings (`EventTurnDone` and the stream closing behind it). No shape was found where a block actually missed its settle; this states the invariant rather than fixing a reproduced defect."
---

The report read the symptom as a block that never settled. It is not: the block
settles and is then classified as narration, which is drawn with no markdown at
all on purpose — MARKDOWN OWNS WEIGHT, and a `##` inside narration would render
heavier than the answer under it (hierarchy.go). That law is unchanged and
narration is still plain. What changed is who may demote an answer: the model
opening more work under it, never a line this surface wrote about the turn.

Measured against a real model on this branch's parent, the reproduction is a turn
that carries a task proposal — which correctly blocks the fold, since a decision
may not disappear into a chip — then a markdown answer, then the turn's own
`⟲ … cached` line. Turn one renders; that turn comes back raw. On the fixed
binary all three turns of the same conversation render. An ordinary
question-and-answer turn against a thinking model folds, and never showed the
defect at all, which is why it survived this long.
