---
kind: added
title: App clients can make a Factory job with an issue, labels, a budget and a retry key, and attach context
pr: 2415
surface: [engine, factory]
invalidates:
  - "Was true: factory.job_create always made a job without a GitHub issue, took no budget or labels, a retried create could make a second job, and a job's plan read only its description. Is true now: factory.job_create takes optional open_issue, group, milestone, budget and key, and factory.job_context_set attaches context the plan step reads."
---

`factory.job_create` takes optional `open_issue` (posts to `POST /v1/issues`
and opens one GitHub issue with the title and text), `group` and `milestone`
(opaque labels stored on the job and returned on its reads), `budget` (one of
the standard steps, the job's budget from creation) and `key` (sent as
`Idempotency-Key`, so a retried create returns the same job).
`factory.job_context_set {factory, job, expect, context}` replaces a job's
context attachments while it has no run: up to 20 `{title, markdown, source?}`
items, at most 100,000 characters in all, kept outside the 20,000-character
description. The plan step that runs reads them whole (the first conversation
step that runs when there is no plan step); job reads list them by title and
source only, and they never go to GitHub. `factory.capabilities` announces
`create_issue`, `create_group`, `create_budget`, `create_key` and
`job_context`; hello names `factory.job_context`. A Factory older than a
field or route answers `conflict` with `detail.reason: "factory_too_old"` and
nothing changes.
