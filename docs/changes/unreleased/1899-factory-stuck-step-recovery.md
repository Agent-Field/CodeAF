---
kind: added
title: A stuck step is recovered by running a step again, with words
pr: 1899
surface: [engine, docs]
invalidates:
  - "Was true: the app could not recover a job whose step had stopped short; the wire carried no answer to that question, so only Stop and Drop were offered. Is true now: `factory.job_step_rerun` (`POST /v1/jobs/{id}/steps/{step}/rerun`) takes optional `words`, kept on the job's notes, which every step's brief carries; on the stuck step it tries again, and on `needs_you.back_to`, which an `out_of_rounds` wait now names (the finished step before the stuck one, none for the first step), it sends the work back. A step stop refuses words."
---

Trying again and sending back are the same act, running a step again, so
they are one route with one guard rather than a second answer road beside the
rerun the app already carried.
