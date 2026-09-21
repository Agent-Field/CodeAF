---
kind: fixed
title: no run answers done while a finished piece of work has had no review
pr: 1275
surface: [engine, chat]
invalidates:
  - "A run could answer `done` with a finished task never checked, and only under load. A worker writes its own done to the plan store and comes home a moment later; its return is what adds its check. The run read the store row as the landing, so when a root finished, or was woken, in that moment, the check was added too late or refused, and the refusal was dropped without a word. #1233 closed the first of these orders for a root that did the work alone; the others stayed open."
  - "A landing is now a return the run has absorbed, not a store row. The root's completion, a parent's wake and the run's own answer all wait for the worker of a done task to come home. The wait is bounded by the worker: it reads its own row when the command it is in ends, at most 600 seconds (the belt's ceiling on one command)."
  - "A check can now be seated beneath a task that already reads done: `Store.AddReviewCheck` (it was `AddRootCheck`, for a childless done root only) adds the check and moves every done task above it back to waiting on it, each keeping the result it earned. Nothing but a check comes in that way. A root reopened like this is owed the wake any parent is owed for a landing it was never given."
  - "A review the store will not seat ends the run as `incomplete`. It used to be dropped, and the run answered done with the round silently absent."
  - "A return already waiting in the run's channel is absorbed before a pass looks at the root's ending, and the root's row is read after that, never before."
---

A person sees no new word. The manual's section on who checks a task's work says
what the run waits for and for how long.
