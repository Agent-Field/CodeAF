---
kind: added
title: Trigger API — messages and status for outside work, one event bus, signed webhooks per source
pr: 2485
surface: [factory, docs]
invalidates:
  - "Was true: a job filed from outside GitHub could not be told more after it was filed. Is true: POST /api/intake/{source}/{id}/messages {author?, text} (and trigger.Message, which AgentField's message reasoner calls) adds the words as a comment on the job, read by the next step's brief and handled once by the manager exactly as a GitHub comment is (a stranger's job reads it as context only), answering {job, accepted, outcome, line}."
  - "Was true: an outside caller could not read how far its job was. Is true: GET /api/intake/{source}/{id} (and trigger.Status / trigger.StatusOfJob) answers {job, state, steps_done, steps_total, route?, pr_url?, question?, updated}, read off the job wire's own facts."
  - "Was true: inbox.Notify told only the job's source's inbox.Replier, of three moments (pr_opened, merged, stuck), and a stop with a question was sent as stuck. Is true: inbox.Notify is one event bus: every moment goes to the source's replier and to every inbox.RegisterSink (the signed webhook is one; AgentField registers another), and EventKind also has filed, started, question, done and message. A stop with a question is `question`; Jira comments it as it commented stuck, and ignores the new kinds."
  - "Was true: the factory sent nothing outside but Jira comments. Is true: `codeaf factory webhook add SOURCE URL` (/v1/webhooks, operator only) prints a signing secret and makes the factory POST job.filed, job.started, job.question, job.pr_opened, job.merged, job.stuck, job.done and job.message for that source's jobs, signed X-Codeaf-Signature: t=…,sha256=HMAC(\"<t>.<body>\"), from a durable outbox retried until a 2xx for three days."
  - "Was true (in the wave 2 design): codeaf would mint a token per source and ship Go and TypeScript SDKs. Is true: neither exists; every Trigger API call takes the factory's operator token, and AgentField's SDKs and DID auth are the way for a caller with its own credential."
---

The message and status logic lives in `internal/factory/trigger`
(`Message`, `Status`, `StatusOfJob`) so the HTTP routes and AgentField's
reasoners share one door. GitHub's poller and its comments are unchanged.
The manual's page is "The factory's Trigger API".
