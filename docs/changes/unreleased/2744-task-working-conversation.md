---
kind: added
title: A task's Working conversation is readable by its workstream, and its owner or starter steers it
pr: 2744
surface: [engine, factory, app]
invalidates:
  - "Was true: a task Afi ran on an organization's server published no agent event; nobody could read what it said or did, only the task's state, results and thread. Is true now: every line of the run is kept and published as task.work to everyone who can see the workstream, read with GET /v1/workstreams/{id}/tasks/{tid}/work and relayed to the desk as factory.org_workstream_task_work (hello org_task_work)."
  - "Was true: anything a person typed to an agent in an organization was private. Is true now: Afi's pane and a person's own chats stay private, but a steer typed on a task (POST .../work/messages, owner or starter only, while the run works) is a line everyone in the workstream reads."
  - "Was true: the task-run prompt told Afi nobody reads what it says during a task. Is true now: it is told everyone reads it and that the owner or starter may steer it."
---
