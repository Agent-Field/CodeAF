---
kind: added
title: Invite someone by email with a team and workstreams; their tasks wait until they join
surface: [engine, app, chat]
invalidates:
  - "Was true: an organization admitted people only through anonymous access links, and nobody could be given work before they signed in. Is true now: POST /v1/invites (appwire factory.org_invite_create, hello word org_invites) invites one person by email or name, with a team and workstreams, for 7 days; before joining they are invite:<id> on those workstreams and can own or do tasks, and a task they do waits until they join. Joining puts them in the team and workstreams and hands them the tasks."
  - "Was true: removing a person from the organization left their tasks with them, and they stayed on /v1/people. Is true now: the tasks they owned go to the admin who removed them, under Handed to you, and the person leaves /v1/people and the invites list."
  - "Was true: GET /v1/access/links listed every link. Is true now: an invite made through /v1/invites is listed only there, never again among the access links."
  - "Was true: an organization's server never sent mail. Is true now: with CODEAF_SMTP_URL and CODEAF_SMTP_FROM set, codeaf factory serve emails invites; without them an invite is a link to copy."
---
