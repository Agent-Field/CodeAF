---
kind: fixed
title: a step edit no longer drops a running job back to Backlog when its Write is slow to stop
pr: 2490
surface: [factory, engine]
invalidates:
  - "Was true: after a step edit withdrew the worker, the next lease's go-on was dropped when the withdrawn round was still letting go of a model call (the runner read the cancelled, unpaused control as unchanged); the old drive then ended, nothing ran the job, and it fell back to new in Backlog with Write still marked running. Is true now: go-on on a cancelled control whose job the store holds paused waits for the old drive and revives the job at its step."
  - "Was true: TestFactoryManagersE2E said nothing about a job that stalled after proof. Is true now: it logs each change in what the landed job waits on (state, reason, approval, publication, claims not shown, Auto's block reason) and keeps job-<id>.json beside the logs."
---
