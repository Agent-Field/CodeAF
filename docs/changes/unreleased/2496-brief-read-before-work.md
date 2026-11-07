---
kind: added
title: A brief written before a job starts is given to every step, and quoted before it is written
pr: 2496
surface: [engine]
invalidates:
  - "A job's read of its issue before work was for the person only: no step was ever given a brief version, and a runner's pulled job carried none. Now a version kept before any step reported is marked before_work, every step's brief carries the newest such read beside the issue (the issue and the person's notes win where they disagree), and the worker's pull carries it as first_read."
  - "The brief read had no price before a write. factory.job_brief and GET /v1/jobs/{id}/brief now carry read_price, the median cost of the reads this factory kept, while the next write would be a paid read before work and once one has run."
---

A person can press Write the brief on a job that has not started, see what a
read typically costs here, and have the job's steps start from that read.
