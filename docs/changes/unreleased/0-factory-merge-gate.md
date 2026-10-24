---
kind: added
title: Factory jobs merge their own pull request through one gate in code
pr: 0
surface: [engine, chat, docs]
invalidates:
  - "Was true: the factory never merged anything; a job followed its pull request until someone merged it on the forge, and nothing in a pipeline file said anything about merging. Is true now: the job's assignee presses Merge (`POST /v1/jobs/{id}/merge`, `factory.job_merge`), which waits five seconds with Undo and is carried out by the runner holding the assignee's forge account; `.codeaf/factory.yaml` `merge: {auto: on|off, method}` lets the factory merge on its own. Auto-merge is off unless the file turns it on, and a personal file may only turn it off."
  - "Was true: a job's wire had no merge state, and a pull request's head commit and mergeability were not on it. Is true now: `job.merge {auto, method, run, blocked}` and `can.merge`; `job.pr` carries `head_sha`, `mergeable`, `checks_read` and `reviews_read`; reasons gain `merging` (running) and `merge_failed` (needs you); the record gains `merge_asked`, `merge_queued` and `merge_refused`."
---

Every merge goes through `factory.MergeGate`, a pure function of the job that
fails closed: it merges only a pull request the factory opened, only the
exact commit whose proof was approved, and never a draft, a conflict, a
failing or running check, or a branch still waiting on required reviews; a
fact nobody read refuses. It runs on the factory when the merge is asked for
and again on the runner on a fresh read, and the forge is handed the exact
commit, so a head that moves after both is refused there too. A merge is sent
once; a lost answer is followed by a read, never a second merge. The pipeline's
merge settings are fixed on a job when its proof is approved; an automatic
merge also needs every claim proven and a passing check, waits quietly until
then, and a failed merge is never retried on its own. No agent has a merge
tool.
