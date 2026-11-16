---
kind: fixed
title: A usage test can no longer crash the session test binary, and closing an agent waits for its sync
pr: 2648
surface: [engine]
invalidates:
  - "Was true: the usage journal's background sync goroutine and its channels were started by whichever caller appended a row first (usagePending.append). A caller inside a synctest bubble therefore owned a 100ms fake-clock timer that could outlive the bubble's main goroutine (a synctest deadlock), and the ledger writer, outside the bubble, then woke that bubbled channel in finishSync. That fatal error killed the whole internal/session test binary, so CI listed dozens of unrelated tests as failing. TestClosingAnAgentInsideASyncBubbleLeavesTheWriterAlone hit it in about 9 runs of 10 locally, and cmd/codeaf's TestProcessCloseJoinsTheUsageWriter failed for the same reason. Is true now: only the ledger's writer starts the sync (usagePending.startSync, called from usageWriter.run's write and from finishSync). An appender only writes and counts its row, and only hurries a sync that is already running."
  - "Was true: drainUsage (Agent.Close, SettleWrites) and flushUsage answered once rows reached the ledger, while the journal sync for those rows could still be waiting out its 100ms interval. That sync locks the ledger, and locking creates the ledger's folder and file, so it could recreate a home its owner had just removed (TempDir cleanup: directory not empty). Is true now: both also wait, under the same ceiling, until the journal has synced every row it held when they began (usagePending.appendedSoFar and syncedThrough, awaitUsageJournals)."
---
