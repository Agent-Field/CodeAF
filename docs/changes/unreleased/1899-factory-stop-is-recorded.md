---
kind: fixed
title: A stopped factory job says it was stopped
pr: 1899
surface: [engine, docs]
invalidates:
  - "Was true: a person's Stop of a whole job set it back to new and only added the log line `stopped · branch kept`, so it read exactly like a fresh backlog job (`state` `backlog`, `can.run`, no `failure`) while the step it cut could still read `running`: the stop of a job no runner here held never touched its steps, and the Stop door then cleared any failure. Is true now: every stop writes one fact, and a stop that cut a step is marked the way a step's own Stop marks it. When a step was running or waiting, the job carries `failure` `{code: stopped, detail: \"Stopped by you\", next: stopped, step: <the step it cut>}` and reads `state` `needs_you` (`needs_you.kind` `failed`, `reason` `failed`), `state_presentation` `{kind: failed, title: \"Step stopped\"}`; the cut step reads `failed` with the same failure, so no step reads running, and `controls.rerun_step` carries on from it. A stop that cut no step (a queued job whose steps had not begun, or a landed one) leaves the job in `backlog` with no `failure`, ready to start, as before. A no to a step out of rounds, to the budget, or to an approve step with nothing before it is the same stop; a step that errored keeps its own failure."
  - "Was true: a failed or stopped job offered Stop (`can.stop`), and the lease's stop turned a failed job back into backlog. Is true now: a job whose run already ended on a failure or a stop has `can.stop` false and its Stop refuses `#12 is not running`, as the runner's own Stop always did; the reset a reassignment or a drop makes first leaves it as it is."
---

A stop is a person's decision about the job, so where it cut a step it is a
fact on the job rather than a log line beside it, the same fact a step's own
Stop writes. A stop that cut nothing has nothing to recover and only takes the
job out of the queue. Undo still puts the run back as it stood in both cases,
and a start still begins a new run at the first step; the manual's factory
page now says what a stopped job keeps and what starting it again does.
