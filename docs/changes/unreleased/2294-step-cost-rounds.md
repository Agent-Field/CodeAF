---
kind: fixed
title: codeaf step costs include every round once
pr: 2294
surface: [engine]
invalidates:
  - "A worker's latest round total cannot represent a step's earlier rounds. The factory no longer reconstructs round baselines or sends cost-only pipeline snapshots."
---

Workers retain a table of cumulative spending by logical round and send the whole
table in each changed step report. The factory keeps the largest value for each round and sums those values for
step.cost. Partial, repeated, contradictory and out-of-order tables cannot erase
rows or lower spending; any increased row still adds its new spending.
Resuming a round updates its row; retrying adds a round; a fresh run starts empty.
Coalesced reports retain every round without restoring pipeline edits a person removed.

Job spending includes accepted table growth plus the worker's spending outside
steps, including manager calls. Settlement receipts record that full increase,
so recovering a worker cannot charge acknowledged step spending again.

The app still receives one step cost. Worker admission advertises round_cost_tables. Only admitted workers send
round_costs and exclude table growth from Cost. Without that marker, workers
send the established body with all new spending in Cost. Old saved jobs and old workers without a table
use the last Spent fallback, including after a table-capable worker; there is no migration or reconstruction of missing
historical rounds. Spending includes resumed work and descendants.
