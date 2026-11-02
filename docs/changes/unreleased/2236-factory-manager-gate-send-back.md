---
kind: fixed
title: A job manager can send a waiting plan back with your changes
pr: 2236
surface: [engine]
invalidates:
  - "The manager's saved status exposed a question identity only for step questions, leaving approval gates without one."
  - "The manager could describe a plan revision without an accepted send-back action."
---

Tell the job manager what to change at a plan approval gate. It can now send
that plan back with your words, so the previous step runs again in a new round
and asks for approval afterward. Saved status identifies every kind of waiting
gate. A stale decision is refused without changing the job.

The manager checks the action result before reporting a change. Refused or
unconfirmed actions are explained plainly; acceptance does not claim that a
worker has already started.
