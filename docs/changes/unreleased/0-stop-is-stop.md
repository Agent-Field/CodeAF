---
kind: fixed
title: A person's Stop stays stopped until their next start
pr: 0
surface: [engine, factory]
invalidates:
  - "A queued team question could wake a stopped job's manager and launch, approve or publish the job again."
  - "A Stop before a step began could look like a job that had never started."
---

Persist the person's Stop independently of the engine state and Undo's timer.
Every launch and worker admission refuses that record; automatic recovery,
proof approval, final readiness review and publication also refuse it. Stop
takes down the pending step question and worker signals. Late team traffic is
acknowledged without a model call, including after restart. An in-flight
manager may finish speaking, but its tools cannot restart or publish the job.

Start, Carry on, Run again and Run again from here are explicit person controls
that can grant work again. Undo also restores the prior run during its existing
five-second window. Resume pipeline is also an explicit person grant after
manual control. Run as new starts a separate job and leaves the original
stopped. Person chat remains available without granting execution authority.

Legacy StopUndo and explicit stopped failures are honoured. A legacy job with
no surviving stop evidence cannot be distinguished from a never-started job;
free-form activity or generic stream ending text is not execution authority.
The publication guards and already-sent PR receipt handling from #2299 remain.
