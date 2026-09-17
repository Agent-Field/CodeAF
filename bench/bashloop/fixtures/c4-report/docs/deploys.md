# Deploys

The service shipped 47 deploys last quarter, up from 31 the quarter before.
Three of them were rolled back within an hour (6.4% of deploys), all three
because a migration ran before its backfill. Median time from merge to
production is 22 minutes; the slowest step is the manual smoke checklist,
which adds 8 minutes on every deploy.
