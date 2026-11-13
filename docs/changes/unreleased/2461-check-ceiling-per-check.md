---
kind: fixed
title: each check in a task run gets its own checker spend ceiling
pr: 2461
surface: [engine, chat, docs]
invalidates:
  - "Was true: a run's checker ceiling was tallied across every check in the run, so once one check spent it, every later check was refused on its first call. Is true now: each check task has its own checker ceiling."
---

Every check's spend still counts toward the run's per-task limit and the day's limits.
A crew that serves one task keeps its per-seat ceiling, and saved crew records keep their
per-seat totals.
