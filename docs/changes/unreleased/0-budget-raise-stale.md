---
kind: fixed
title: Raising a busy job's budget isn't refused because its spend moved
pr: 0
surface: [engine, factory]
invalidates:
  - "Was true: factory.job_set_budget refused with stale when any report moved the job's version between the read and the raise. Now only a competing budget edit conflicts."
---

A person raising the budget of a running job could be refused with "stale: job
moved on while you looked" because a worker reported spend in between. The
desktop's one retry could lose the same race. Progress no longer blocks a budget
edit. Someone else's budget edit since the version read returns a conflict that
isn't retried, and a finished or dropped job still refuses.
