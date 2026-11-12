---
kind: added
title: Factory jobs can be made with an issue, labels, a budget, a retry key and a split parent
pr: 2617
surface: [engine, factory, app]
invalidates:
  - "Was true: factory.job_create always made a job without a GitHub issue, took no budget or labels, a retried create could make a second job, and a job's plan read only its description. Is true now: factory.job_create takes optional open_issue, group, milestone, budget and key, and factory.job_context_set attaches context the plan step reads."
  - "Was true: a job created from part of another job carried no split lineage. Is true now: factory.job_create accepts split_from, and the new job's reads retain the parent job number."
---

`factory.job_create` takes optional `open_issue` (posts to `POST /v1/issues`
and opens one GitHub issue with the title and text), `group` and `milestone`
(opaque labels stored on the job and returned on its reads), `budget` (one of
the standard steps, the job's budget from creation), `key` (sent as
`Idempotency-Key`, so a resent create never makes a second job or issue) and `split_from`
(a positive job number held by the same Factory and visible to the caller; the
new job's reads return it, its activity records `split from #N`, and the
parent is unchanged and is checked from its saved copy).

`factory.job_context_set {factory, job, expect, context}` replaces a job's
context attachments while it has no run: up to 20 `{title, markdown, source?}`
items, at most 100,000 characters in all, kept outside the 20,000-character
description. The plan step that runs reads them whole (the first conversation
step that runs when there is no plan step); job reads list them by title and
source only, and they never go to GitHub.

`factory.capabilities` announces `create_issue`, `create_group`,
`create_budget`, `create_key`, `create_split_from` and `job_context`; hello
names `factory.job_context`. Invalid values are `bad_params` before anything is
sent, and a missing or hidden split parent is refused before any job or issue
is made. A Factory older than a field or route answers `conflict` with
`detail.reason: "factory_too_old"` and nothing changes. A job's `group` here is
a label only: go-aheads still name the jobs they cover by number.

This must merge together with #2619. On its own, a Factory replays every keyed
answer for 24 hours, a refusal or 5xx included, so a resend after one cannot
succeed. #2619 lets a create that wrote nothing run again on the same key, and
still replays every answer given after the job or issue was made.

Carried from #2415 and #2429, which merged only into the closed integration
branch of #2306.
