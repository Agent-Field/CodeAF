---
kind: added
title: App clients can record which Brief revision each person last saw on a factory job
pr: 2108
surface: [engine]
invalidates:
  - "Was true: every write to a factory job's document bumped its revision, the version expect checks. Is true now: store.UpdatePersonal writes one person's own fact (the Brief revision they saw) and keeps the revision and Changed, so another person's open page is not made stale."
  - "Was true: a job read carried no per-person seen state. Is true now: it carries brief_seen, the caller's own last seen Brief revision, absent for none; nobody else's is ever answered."
---

`factory.job_brief_seen {factory, job, rev}` (hello feature
`factory.job_brief_seen`) posts to the factory's new
`POST /v1/jobs/{id}/brief_seen`. The kept revision only moves forward: a lower
one keeps the higher. It takes no `expect`, because a seen mark is not a
decision about the job.
