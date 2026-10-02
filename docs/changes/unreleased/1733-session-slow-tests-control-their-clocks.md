---
kind: internal
title: twelve slow session tests wait on acknowledgements instead of wall clocks
pr: 1733
surface: [engine]
invalidates:
  - "The slowest internal/session tests (the watch tickers, the team loop breaker, the slow phase listener, the young-bash steer grace, the stalled checker, the task-baseline attribution trio) took about 123 seconds between them, waiting on real time. They now drive the same real commands, queues and landings through acknowledgements and explicit clock advances, about 4 seconds in all. The two plandb CLI tests that run a real loop through bash (about 35 seconds each) are unchanged and remain the slowest in the package."
  - "jobRegistry.watchTickWait, Agent.steerAfter, Config.teamWatchManual and Config.auditTimeout are new private test seams. Each is nil or false in production and falls through to the call it replaced, so no window, default or limit moved."
---
