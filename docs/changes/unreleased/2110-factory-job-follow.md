---
kind: added
title: App clients can follow a factory job and read whether they follow it
pr: 2110
surface: [engine]
invalidates:
  - "Was true: a job read carried nothing about who follows it. Is true now: it carries following, the caller's own switch, absent when off; nobody else's is ever answered."
  - "Was true: every write to a factory job's document bumped its revision, the version expect checks. Is true now: store.UpdatePersonal writes one person's own fact (following, the Brief revision seen) and keeps the revision and Changed, and a change that changes nothing writes nothing."
---

`factory.job_follow {factory, job, on}` (hello feature `factory.job_follow`)
posts to the factory's new `POST /v1/jobs/{id}/follow`. It is not `follow_pr`,
the factory's own step that maintains a published pull request. The switch is
stored and answered only. No notification is sent for a followed job yet,
because no factory job event reaches the app over the wire.
