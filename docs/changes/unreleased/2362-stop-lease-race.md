---
kind: fixed
title: A Stop during lease reservation can no longer restart a job
pr: 2362
surface: [engine]
invalidates:
  - "A job with a pending reply, stopped while a worker was being granted its lease, could be set running again. The grant now decides from the locked job, so the reply gets a thread-only lease and the job stays stopped."
---

The stopped job's pending reply still gets its thread-only lease.
