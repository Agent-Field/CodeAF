---
kind: added
title: A dropped factory job can be brought back
pr: 0
surface: [engine, chat, docs]
invalidates:
  - "Was true: Drop was final. Nothing undid it, `launchable` refused the job as `dropped for good`, and its branch was deleted seven days later. Is true now: `POST /v1/jobs/{id}/restore` (Bring back) returns a dropped job to Backlog: state new, `done_how` and `done_at` cleared, the branch deletion called off (and a branch already deleted is forgotten, so the next run makes a fresh one). Nothing starts. Only the job's person may, and only on a job whose `done_how` is `dropped`; anything else is `409 only a dropped job can be brought back`."
  - "Was true: a job's `can` had no way back from a drop. Is true now: `can.restore` is true for the person who may act on a dropped job."
  - "Was true: the app's job writes had no undo for a drop. Is true now: `factory.job_restore {factory, job, expect}` sends Bring back, answered `FactoryJobActed` with action `restore`, under `factory.job_actions`."
  - "Was true: once the record held a branch delete for a job, the factory never sent another for it. Is true now: only a delete after the job's last drop counts, so a job brought back and dropped again has its new branch deleted too."
  - "Was true: the factory record had no event for undoing a drop. Is true now: kind `restored`, Activity verb `brought back`."
---

Drop stays the way a job ends for good on its own; Bring back is the one door
out of it, and it is a person's. After it the poll treats the job like any
other item.
