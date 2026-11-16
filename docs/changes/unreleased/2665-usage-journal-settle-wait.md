---
kind: fixed
title: Closing an agent waits for its spending journal to reach the disk
pr: 2665
surface: [engine]
invalidates:
  - "Was true: drainUsage (Agent.Close, SettleWrites) and flushUsage answered once rows reached the ledger, while the journal sync for those rows could still be waiting out its 100ms interval. That sync locks the ledger, and locking creates the ledger's folder and file, so it could recreate a home its owner had just removed (TempDir cleanup: directory not empty). Is true now: both also wait, under the same usageFlushLimit ceiling, until the journal has synced every row it held when they began (usagePending.appendedSoFar and syncedThrough, awaitUsageJournals)."
  - "Was true: TestPendingPreviousDayDoesNotBlockNewDay set time.Local to the host offset plus 24 hours, which on a UTC machine is +24:00 and which time.Time.MarshalJSON refuses, so its usage row was never written and the next test's flush timed out. Is true now: the call is made at UTC-12 and the new day is read at UTC+12."
  - "Was true: #2648 carried these two fixes on app/v3 only. Is true now: they are on dev, ported onto dev's writer-owned journal loop (journalStop/journalDone from #2597); #2648's own startSync rework was not taken because dev already solved that part."
---
