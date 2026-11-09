---
kind: fixed
title: Session test agents flush the usage ledger before their temporary homes are removed
pr: 2555
surface: [build]
invalidates:
  - "newTestAgent's cleanup closed the agent and settled its writes, which looked like everything a test's home could still receive; the usage ledger's writer belongs to the process and was not covered, so a paid turn's late append could put v3/ back under TempDir's RemoveAll. The cleanup now also calls FlushUsage, as the program's exit does."
---
TestBudgetScopeAndLiveConversationRecovery failed this way in the full check on
2026-11-09. Beside a synced disk writer it failed 2 of 2,000 runs, and 0 with the flush.
