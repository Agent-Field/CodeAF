---
kind: changed
title: a workstream task starts by itself when what it waits on is done, unless it says it waits for you
surface: [factory, engine, docs]
invalidates:
  - "Was true: when a tick or a run's done finished the last thing an Afi task waited on, the task stayed up_next with \"Ready to start: X is done\" and waited for a person's Start, even inside a plan someone had already started (E2 #7). Is true now: on an organization's server, where the hello says org_task_starts, the server starts that Afi task in the same write, with the rights of whoever finished the work, and its thread says \"Started: X is done\". It stays Ready only when the task says starts=by_hand, it belongs to a plan (a batch of five or more) nobody has started, its owner turned \"Start tasks when what they wait on is done\" off (GET/PUT /v1/me/work, factory.org_work and factory.org_work_set), it is a chat's task, or Afi cannot take it now."
  - "Was true: a task had no say in whether it starts by itself. Is true now: `starts` (`when_ready`, `by_hand`, or absent for the default) is a task field its owner or whoever looks after the workstream sets with the ordinary task change (403 for anyone else); every Afi and chat task reads back `starts_when_ready`, filled when read."
  - "Was true: Afi's ws_task_create tool said nothing in a batch starts until a person presses Start, and stopped there, so Afi told people a started plan still waited on their Start. Is true now: it says a started plan's tasks start by themselves as their waits clear, and ws_tasks on an organization says which tasks start by themselves."
---
