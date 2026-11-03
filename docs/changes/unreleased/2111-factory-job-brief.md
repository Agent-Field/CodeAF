---
kind: added
title: A factory job keeps its brief versions, and the app reads the newest with factory.job_brief
pr: 2111
surface: [engine]
invalidates:
  - "Was true: a factory job had no brief; the job page could only show steps and chat. Is true now: a job keeps its last 20 brief versions (verdict, facts, palette blocks, changed keys), GET /v1/jobs/{id}/brief answers the newest one (or a stub made from engine data before there is one) with its status and the keys changed since a given rev, and an engine whose hello says `factory.job_brief` serves it as `factory.job_brief {factory, job, since?}`. The facts row is computed from the steps' structured results, never from a model. The job read carries `brief: {rev, status}` once a brief exists."
---
