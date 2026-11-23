---
kind: added
title: Pause and resume a running factory job
pr: 2217
surface: [engine, chat]
invalidates:
  - "Was true: the app offered Hold and Resume only for queued jobs. Is true: a job's person can also pause a running step and resume it with its work, conversation and spending kept."
  - "Was true: running-job pause bypassed the owner check and action receipts. Is true: Hold and Resume check the job's person and record who acted."
  - "Was true: a paused running job kept its Jobs at once slot. Is true: Pause releases that slot after saving the interrupted round, and Resume takes a free slot or waits in the ordinary queue."
---

Pause cuts the current model round and shows Paused without asking a question.
Resume continues the same step, reusing its conversation when available. Jobs
release their capacity slot after the interrupted round is saved, letting other
jobs run. Resume takes a free slot or waits in the ordinary queue without model
calls. The paused job still reports state running and Paused; after Resume it
reports queued until a worker takes it. Completed steps and spending stay.
The app wire advertises factory.job_pause so a desktop can offer Pause beside Stop.
