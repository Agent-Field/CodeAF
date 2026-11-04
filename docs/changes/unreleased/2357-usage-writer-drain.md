---
kind: fixed
title: Closing a conversation waits for queued spending records
pr: 2357
surface: [engine, chat]
invalidates:
  - Closing a session previously left queued usage rows behind; it now drains that ledger under a bounded wait.
---

Closing a conversation now waits for its queued spending records, including the
last call, before returning. A stalled ledger can still leave rows pending after
the two-second limit on each close; that leaves one line in the log and counts
those rows as not written down until they are. Other conversations sharing the ledger
can keep recording spending.
