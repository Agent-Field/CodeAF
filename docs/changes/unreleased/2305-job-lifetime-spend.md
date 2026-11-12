---
kind: fixed
title: codeaf jobs keep spending across restarts
pr: 2305
surface: [factory, app]
invalidates:
  - "Starting a job again or bringing it back used to erase earlier run spending from job totals. Lifetime spending now keeps those dollars."
---

Jobs save earlier spending as SpentBefore. Start and Bring back clear work while
keeping money; Run as new starts a separate job at zero. Late acknowledged
receipts add only their growth to the run that spent it, even after Drop and Bring back or a restart on another machine. Archived
receipts require the original lease, machine and person saved in the receipt;
older receipts without those identities cannot accept late usage.
Undo Stop keeps money reported since the stop.

The job and queue wire add optional lifetime_cost beside the unchanged current
run cost. Step costs, per-run budgets and the daily spending rail stay unchanged.
Spending by repository and model uses the known lifetime total, placed on the
latest run's start day, or creation day without a run. A range containing only
a second run includes earlier runs too; these are placed job totals, not exact
per-call range totals. Earlier money without a model split stays unknown.

App job and queue reads use only saved spending. Ledger recovery is disabled:
local and remote factory jobs can share both a ledger tag and this worker's
workspace, so neither identifies which factory spent the money. Older jobs
show only their known saved totals; lost ledger-only spending is not guessed.

An older binary still running can rewrite a saved job and drop SpentBefore and
Archived, like any newly added saved field; this change adds no version fence.
