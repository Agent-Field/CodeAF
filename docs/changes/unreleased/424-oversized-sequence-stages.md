---
kind: fixed
title: an oversized node that cannot run in parallel is divided into ordered stages
pr: 424
surface: [engine]
invalidates:
  - "The planner's expansion could only ask one question. `expandScoped` was flat by design — one fan-out, no spine — so a node whose inside is a sequence was asked the simultaneity question twice, answered it truthfully with one part twice, and was journaled `the division gave back one piece, which is the node again`. It now has a second move (`internal/plan/sequence.go`): when the fan-out returns one part for a node the ruler put past one worker's reach, the stage question is asked of that node alone and the ordered stages are spliced as a chain."
  - "The undivided shortcut in `plan.Build` used to be taken on the spine's answer alone — one stage meant one worker with the goal as its brief, with no fan-out and no sizing. It now sizes that one node first (one call) and takes the shortcut only when the node is atomic; anything larger falls through to the whole pipeline."
  - "An oversized node whose sizing named fewer than two pieces was refused at `plan.JudgeSplit` as `no two pieces could be named for it`, before any expansion ran. It now goes straight to the stage question — no fan-out, one call — because the sizing prompt's own instruction made an empty list evidence of a sequence. Atomic and borderline nodes keep that refusal, and a node within one worker's reach is still never staged."
  - "The sizing prompt used to tell the model `If the inside of the node is a sequence, or if you would only be restating it in smaller words, there are no pieces.` It now asks an oversized node whose inside is a sequence to name the ordered stages it passes through instead; only a restatement returns an empty list. `internal/plan/testdata/size_prompt_baseline.golden` moves with it."
  - "`selectForExpansion` switched on `node.Size` with cases for oversized and borderline only, so a node `JudgeSplit` admitted on measured capacity — which is always sized atomic — fell through and was neither divided nor refused. Every admission now lands in a list."
  - "The remainder planner ran at `MaxDepth: 0`, which refused every node at `JudgeSplit` before it was weighed and ran the expansion loop zero times. A remainder is now allowed one level of division — `MaxDepth: 1` — and still no tree."
---

The size prompt has always named two things that discharge the burden of dividing:
pieces that genuinely run at the same time, or a node that cannot be brought to an
end inside what one worker can hold. Only the first had machinery behind it, so an
oversized node whose inside was a sequence had no shape to become and ran whole
against the very budget it had already been measured past.
