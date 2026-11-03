---
kind: fixed
title: codeaf jobs keep spending across restarts
pr: 0
surface: [factory, app]
invalidates:
  - "Starting a job again or bringing it back used to erase earlier run spending from job totals. Lifetime spending now keeps those dollars."
---

Fill in the pull request number in the file name and frontmatter before opening
this change for review.

Jobs save earlier spending as SpentBefore. Start and Bring back clear work while
keeping money; Run as new starts a separate job at zero. Late acknowledged
receipts add only their growth to the run that spent it, even after restart.
Undo Stop keeps money reported since the stop.

The job and queue wire add optional lifetime_cost beside the unchanged current
run cost. Step costs, per-run budgets and the daily spending rail stay unchanged.
Spending by repository and model uses the known lifetime total, placed on the
latest run's start day, or creation day without a run. A range containing only
a second run includes earlier runs too; these are placed job totals, not exact
per-call range totals. Earlier money without a model split stays unknown.

App job and queue reads for this computer's factory use the larger of the saved
lifetime and its tagged ledger total only when the factory confirms one job
with that name across the whole readable floor, before queue filters. Copies
with FromJob and jobs whose source issue shares the name keep only saved money;
older factories without this proof also keep saved money. Run as new normally
clears the issue number, so its new floor ID gives it a distinct ledger name.
Supported forget paths preserve the ID high-water mark. The ledger is read again only when its
path, size or modification time changes. Missing or smaller totals do not lower
known spending. Other machines show saved totals. Untagged lost spending cannot
be guessed. This read does not rewrite the factory's saved Spending totals.
