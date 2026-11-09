---
kind: removed
title: The factory has no open programmatic doors; custom triggers come through AgentField
pr: 2489
surface: [factory, app, docs]
invalidates:
  - "Was true (#2477): POST /api/intake on the factory's address, with the access token or the operator's session, filed outside work as a job. Is true: the route is gone. Built-in connectors (GitHub, Jira) are set up in the app; the desk files with factory.intake_add; anything programmatic goes through AgentField (an AgentField trigger aimed at <node>.intake, or a direct execute of codeaf-factory.intake)."
  - "Was true (#2485): GET /api/intake/{source}/{id} read a filed job's status and POST /api/intake/{source}/{id}/messages added a comment to it. Is true: both routes are gone. AgentField's status and message reasoners do the same through trigger.Status, trigger.StatusOfJob and trigger.Message, which remain as Go functions."
  - "Was true (#2485): `codeaf factory webhook add|list|remove` and /v1/webhooks set a signed webhook per source, and the factory posted job.filed … job.message to it from a durable outbox (trigger-outbox/, secrets in trigger-webhooks.json). Is true: the command, the route, the webhook sink and the outbox are gone, and nothing creates those files (old ones on disk are left alone). A tool hears back through its AgentField execution; Jira hears back as comments."
  - "Was true (#2477): `codeaf factory add --source … --id … --title …` posted work to a running factory's /api/intake. Is true: the command is removed and answers \"codeaf factory add and codeaf factory webhook were removed: file work in the desktop app, connect GitHub or Jira there, or send custom triggers through AgentField\"."
  - "Was true: the desk's factory.intake_add carried the entry to the named factory's POST /api/intake over HTTP, so it worked against any factory the desk was signed in to. Is true: it calls inbox.Add in the app's own process on the factory this machine hosts (with that host's recipe and the repo-off check, `conflict` when the repository is off); any other factory is `not_found` with \"work is filed on the machine that runs the factory; on another machine, connect GitHub or Jira there or send it through AgentField\"."
  - "Was true: the manual's page \"The factory's Trigger API\" (factory-trigger-api) told a person to call HTTP, set webhooks or use Zapier to send work. Is true: that page is deleted; \"AgentField and the factory\" (factory-agentfield) answers those questions, and factory.md says built-in connectors are set up in the app and custom triggers come through AgentField."
---

The owner's line: a WorkAF user never thinks about code, so the factory has no
HTTP intake, no per-source webhooks and no SDK. The AgentField node is the only
programmatic door, and the internal event bus is how a job's moments reach it.
