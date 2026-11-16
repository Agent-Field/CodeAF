---
kind: changed
title: Start and Start all on an organization's workstream run Afi on the server
pr: 2695
surface: [factory, engine, docs]
invalidates:
  - "Was true: on an organization's workstream nothing could start an Afi task; `start_all` only named the ready tasks and wrote nothing, and the desk offered no Start. Is true now: a person's `state: running` on an Afi task starts Afi in the task's own run on the server, inside the same write, and is 409 with nothing kept when it cannot (no runner, already running, or a chat's task); `start_all` starts the group's ready Afi tasks and answers those now running."
---
