---
kind: added
title: Starting or creating a factory job can pick which of the repository's pipelines it runs
pr: 2127
surface: [engine]
invalidates:
  - "Was true: a job started from Backlog always ran the issue pipeline (or steps changed for that job), and the app could not name another pipeline when creating one. Is true now: factory.job_start and factory.job_create take pipeline, a kind the repository's file lists, behind the feature factory.start_pipeline; the factory's start_now takes it for a job that never ran and is not queued, and refuses pr, ci and a kind the file does not list before writing anything."
---
