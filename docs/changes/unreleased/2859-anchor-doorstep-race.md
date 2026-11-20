---
kind: fixed
title: Anchoring a conversation to a project no longer races the answer doorstep
pr: 2859
surface: [engine, chat]
invalidates:
  - "AnchorWorkspace assigned the whole config.Place, rewriting Place.Dir while the presence goroutine read it without the lock. It now writes only Place.Workspace and Place.Owned; Place.Dir is fixed after construction and safe to read without a.mu."
  - "TestUsageMillionRowLedgerFlush failed under -race on its 2 s flush deadline. The flush takes about 0.8 s plain and about 5 s under the race detector; the test now judges the deadline only on non-race builds, and PERF.md says so."
---
