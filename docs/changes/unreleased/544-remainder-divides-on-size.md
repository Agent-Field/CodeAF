---
kind: fixed
title: a remainder divides on its size and never on its words
pr: 544
surface: [engine]
invalidates:
  - "a remainder whose words list its pieces is divided into them — no longer true. A remainder divides only when the ruler puts it past one worker; `internal/plan/enumerated.go`'s `admitsEnumeratedPieces(node, options)` returns false for `Undivided`, and every seam that could divide on a node's words asks through it."
  - "making remainder `MaxDepth` 1 meant both size and enumerated wording could send a remainder through the full division pipeline — no longer true. `MaxDepth` stays 1 so an oversized remainder can divide into parts or stages, but the `Undivided` shortcut, `JudgeSplit`, and `dividesInTime` cannot divide one merely because its text names pieces."
---

After #424 raised remainder `MaxDepth` from 0 to 1, a remainder could divide when
it was too large for one worker. But a remainder also read its own words first:
one that listed eight failing tests "named several pieces", fell through to the
full pipeline, was drawn as six and then eight leaves, and the canary's
`tox-dev-tox-4031-do` cell on dev 79515001 ran fourteen parallel repair leaves
with revise x14 to the wall. Across nine cells the do-door spend doubled from
$0.46 to $0.95 while quality stayed flat.

**THE LAW: a remainder divides on its size and never on its words.**
`admitsEnumeratedPieces(node, options)` is the one place that decides which
builds may divide from the pieces written on a node. It refuses `Undivided`, and
the three seams — the `Undivided` shortcut in `plan.go`, `JudgeSplit` in
`expand.go`, and `dividesInTime` in `sequence.go` — ask through it. A fresh plan
that names three lanes still divides. A remainder measured past one worker still
divides into parts or stages. `MaxDepth` remains 1, and the split gate remains
off everywhere.

What did not change: when the spine itself answers a remainder with several
gated stages, that remainder is still planned in full.
