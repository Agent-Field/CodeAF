---
kind: added
title: Pause and resume a running factory job
pr: 0
surface: [engine, chat]
invalidates:
  - "Was true: the app offered Hold and Resume only for queued jobs. Is true: a job's person can also pause a running step and resume it with its work, conversation and spending kept."
  - "Was true: running-job pause bypassed the owner check and action receipts. Is true: Hold and Resume check the job's person and record who acted."
---

Pause cuts the current model round and shows Paused without asking a question.
Resume continues the same step, reusing its conversation when available. Jobs
without a live worker wait in the queue after Resume. A paused running job keeps
its capacity slot, as before. The app wire advertises factory.job_pause so a
desktop can offer Pause beside Stop.
