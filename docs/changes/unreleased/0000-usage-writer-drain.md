---
kind: fixed
title: Closing a conversation waits for queued spending records
pr: 0
surface: [engine, chat]
invalidates:
  - Session write settlement previously left queued usage rows behind; it now drains that ledger under a bounded wait.
---

Closing a conversation now waits for its queued spending records, including the
last call, before returning. A stalled ledger can still leave rows pending after
the two-second limit on each settlement. Other conversations sharing the ledger
can keep recording spending.
