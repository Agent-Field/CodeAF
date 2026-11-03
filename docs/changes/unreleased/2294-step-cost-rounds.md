---
kind: fixed
title: codeaf step costs include every round once
pr: 2294
surface: [engine]
invalidates:
  - "A worker's latest round total cannot represent a step's earlier rounds. The factory no longer reconstructs round baselines or sends cost-only pipeline snapshots."
---

Workers retain a table of cumulative spending by logical round and send the whole
table in each changed step report. The factory replaces its copy and sums it for
step.cost. Reports that omit or reduce saved rows are stale and cannot replace it.
Resuming a round updates its row; retrying adds a round; a fresh run starts empty.
Coalesced reports retain every round without restoring pipeline edits a person removed.

Job spending includes accepted table growth plus the worker's spending outside
steps, including manager calls. Settlement receipts record that full increase,
so recovering a worker cannot charge acknowledged step spending again.

The app still receives one step cost. Worker reports and worker admission carry
the optional round_costs table. Old saved jobs and old workers without a table
keep the last Spent fallback; there is no migration or reconstruction of missing
historical rounds. Spending includes resumed work and descendants.
