---
kind: added
title: An organization workstream keeps tasks on its server, and every Factory job in it reads as a task
pr: 2672
surface: [factory, app]
invalidates:
  - "A workstream's work was its Factory jobs and nothing else; it now has tasks (one owner each, done by Afi, a chat, the Factory or a person) under /v1/workstreams/{id}/tasks and factory.org_workstream_task*, behind the hello word org_workstream_tasks."
  - "A Factory job in a workstream was only a job; it is now also that workstream's task job:<n>, derived from the job on every read and never copied, so a person can hand it on, date it or make other tasks wait on it."
---

Only the owner answers a task's question (Everyone means the first answer,
recorded; the workstream's fallback after a day), only the owner ticks a person
task and the tick starts what waited on it, a reply on a done task reopens it,
and Afi's offer under a comment is taken with five seconds to undo.
