---
kind: added
title: Trigger API — messages and status for outside work, one event bus
pr: 2485
surface: [factory, docs]
invalidates:
  - "Was true: a job filed from outside GitHub could not be told more after it was filed. Is true: trigger.Message, which AgentField's message reasoner calls, adds the words as a comment on the job, read by the next step's brief and handled once by the manager exactly as a GitHub comment is (a stranger's job reads it as context only), answering {job, accepted, outcome, line}. (The HTTP door this PR also opened, POST /api/intake/{source}/{id}/messages, was removed again by factory/gi-close-doors)."
  - "Was true: an outside caller could not read how far its job was. Is true: trigger.Status / trigger.StatusOfJob, which AgentField's status reasoner calls, answers {job, state, steps_done, steps_total, route?, pr_url?, question?, updated}, read off the job wire's own facts. (GET /api/intake/{source}/{id} was removed again by factory/gi-close-doors)."
  - "Was true: inbox.Notify told only the job's source's inbox.Replier, of three moments (pr_opened, merged, stuck), and a stop with a question was sent as stuck. Is true: inbox.Notify is one event bus: every moment goes to the source's replier and to every inbox.RegisterSink (AgentField registers one), and EventKind also has filed, started, question, done and message. A stop with a question is `question`; Jira comments it as it commented stuck, and ignores the new kinds."
  - "Was true (#2487): the AgentField node read every open execution's job on a 20-second tick, and its message reasoner handed the text to the job's manager conversation (factory.job_send) answering {job, delivered}. Is true: the node is a sink on the event bus (inbox.RegisterSink \"agentfield\"), reporting each moment as it happens and retelling an execution quiet for three minutes with its lease; its message and status reasoners call trigger.Message and trigger.Status, so a message is a comment on the job answering {job, accepted, outcome, line} (text 1 to 8000 characters), and status answers the Trigger API's {job, state, steps_done, steps_total, route?, pr_url?, question?, updated}."
  - "Was true (in the wave 2 design): codeaf would mint a token per source and ship Go and TypeScript SDKs. Is true: neither exists; AgentField's SDKs and its triggers are the only programmatic way in."
---

The message and status logic lives in `internal/factory/trigger`
(`Message`, `Status`, `StatusOfJob`), called by AgentField's reasoners.
GitHub's poller and its comments are unchanged. The signed webhooks, their
outbox and the HTTP routes this PR added were removed by
factory/gi-close-doors; the manual's page is "AgentField and the factory".
