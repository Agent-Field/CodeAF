---
kind: added
title: App clients can make a Factory job with an issue, labels, a budget, context and a retry key
pr: 2415
surface: [engine, factory]
invalidates:
  - "Was true: factory.job_create always made a job without a GitHub issue, took no budget, labels or context, and a retried create could make a second job. Is true now: it takes optional open_issue, group, milestone, budget, context and key, each announced in factory.capabilities."
---

`factory.job_create` takes optional `open_issue` (posts to `POST /v1/issues`
and opens one GitHub issue with the title and text), `group` and `milestone`
(opaque labels stored on the job and returned on its reads), `budget` (one of
the standard steps, the job's budget from creation), `context` (up to 20
`{title, markdown, source?}` attachments, at most 100,000 characters in all,
kept outside the 20,000-character description and read by the plan step) and
`key` (sent as `Idempotency-Key`, so a retried create returns the same job).
`factory.capabilities` announces `create_issue`, `create_group`,
`create_budget`, `create_context` and `create_key`. A Factory older than a
field answers `conflict` with `detail.reason: "factory_too_old"` and nothing
is made. Job reads list attachments by title and source only, and context
never goes to GitHub.
