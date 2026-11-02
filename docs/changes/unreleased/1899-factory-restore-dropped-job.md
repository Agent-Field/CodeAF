---
kind: added
title: A dropped factory job can be brought back, as a fresh job
pr: 1899
surface: [engine, chat, docs]
invalidates:
  - "Was true: Drop was final. Nothing undid it, and `launchable` refused the job as `dropped for good`. Is true now: `POST /v1/jobs/{id}/restore` (Bring back) returns a dropped job to Backlog as if it had never run: state new, `done_how` and `done_at` cleared, and every trace of its run taken off (steps and spend, failure, approval, stop, question, proof, change, head commit, the machine it ran on, publishing, pull request, merge). Its notes and step edits are taken off, and its conversations went with the drop (0-factory-drop-deletes-conversations.md); its issue, assignee, priority, budget, model and activity stay. Nothing starts. Only the job's person may, and only on a job whose `done_how` is `dropped`; anything else is `409 only a dropped job can be brought back`."
  - "Was true: a job's `can` had no way back from a drop. Is true now: `can.restore` is true for the person who may act on a dropped job."
  - "Was true: the app's job writes had no undo for a drop. Is true now: `factory.job_restore {factory, job, expect}` sends Bring back, answered `FactoryJobActed` with action `restore`, under `factory.job_actions`."
  - "Was true: the factory record had no event for undoing a drop. Is true now: kind `restored`, Activity verb `brought back`; when the job had opened a pull request, the receipt's detail is `earlier pr · <url>`."
---

Drop stays the way a job ends on its own; Bring back is the one door out of
it, and it is a person's. The drop deleted the job's local branch and folder
and its conversations, so the job comes back with nothing of its old run: a start begins at the first
step, on a new branch, from $0. A pull request the old run opened is
forgotten rather than kept, because the poll would otherwise go on writing
that pull request's state onto a Backlog job its fresh run never made; it is
never closed, and the receipt names it. A fresh run cannot open a second pull
request beside it by accident: its push to the same branch name is refused
while the old branch is still on GitHub, because a push never forces.
