---
kind: fixed
title: the money on the status line is the whole tree's, live, not the conversation's half of it
pr: 153
surface: [chat, engine]
invalidates:
  - "The status line's `$` was what THIS CONVERSATION had spent, and a task's money reached it only when the task closed. It is now the whole subtree — the conversation and every task it started, at every depth — added up on the frame clock, so a family working for two hours moves the figure while it works rather than at the end."
  - "`/cost` printed up to five lines. It prints up to seven: `conversation` and `tasks` now sit under `spend` and add up to it. Both are dropped when the work has spent nothing, so a conversation with no tasks reads exactly as it did."
  - "`session.UsageLine` carried three ids saying what money was spent on — a conversation, a node, a standing item. It carries a fourth, `Root`, the conversation a piece of work belongs to; it is written on every agent inside a family including the check and repair rounds, and absent on a conversation's own line and on a standing firing. `session.UsageTree` is the rollup over it."
  - "A task agent fell back to the machine's own ledger when its conversation had been pointed at another file, so a family's rows landed in two places. `Config.usageLedger` travels to a node now."
  - "The money segment's warm ink at four fifths of the conversation's limit was measured on the conversation's own books. It is measured on the figure that is drawn — the whole tree. The engine's refusal is unchanged and still reads the books, which each task's tally lands in as it closes."
  - "aforge had no live answer to `what is this run costing me` short of widening the task column and reading a parent row. It has one, and it is the number already on the screen. Per-task dollar ceilings are still not a thing and were rejected on purpose (#141): the bound stays at the wallet, and what changed is what you can see."
---

Nothing was miscounted underneath. Every model call has always written one line
into the usage ledger where the call was made, and a fold writes none — so the
money was all there, exactly once, all along. What the ledger could not say was
WHOSE: a node's line names the node's own journal, which is a file nobody
outside the family has heard of, so there was no way to add a running tree back
onto the conversation that started it. Measured on the run this came from:
`$2.53` on the row for two hours, against `$51.05` actually being spent.
