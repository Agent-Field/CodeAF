---
kind: internal
title: A conversation's spend total is saved and announced once, however its stamps were scheduled
pr: 2858
surface: [engine]
invalidates:
  - "TestConcurrentTurnAndLateSpendStampsKeepTheLatestMeter failed about two runs in three under -race (and a few in thirty without it) on canvas/connected and its base. The test owed a second stamp beside the one a late bill's bank already owes, and when the deferred writer did not coalesce them the same .375 was written and announced twice. It is green in 600 of 600 runs under -race at -cpu=1,2,8."
  - "Two owed spend stamps that captured the same meter both rewrote meta.json and both published SpendSaved. The second now writes and says nothing; a stamp at a turn's seal that also records the reply time is unchanged."
---

The deferred writer coalesces two owes only when the second arrives before it
starts, so whether a figure was said once or twice was left to the scheduler.
The agent now keeps the total it last saved under `spendStampMu`, and
`TestAnUnchangedSpendIsNotSavedOrSaidTwice` pins that without any goroutine.
