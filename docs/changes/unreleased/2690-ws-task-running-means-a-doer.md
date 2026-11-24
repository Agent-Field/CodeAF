---
kind: changed
title: a workstream task runs only while something does it; comments never reopen, Send back does
pr: 2690
surface: [factory, engine, docs]
invalidates:
  - "Was true: ticking a task (or Start all) set every Afi or chat task waiting only on it to `running` on the organization's server, with the thread line \"Started: X is done\", even though nothing ran it. Is true now: the released task stays `up_next`, unchanged, with \"Ready to start: X is done\"; `POST /v1/workstreams/{id}/tasks/start_all` names the ready tasks and writes nothing; whoever wakes the doer sets `running` (the desk's Start on Personal; nothing yet on an organization, where the owner can Mark done or Stop)."
  - "Was true: a comment or a posted answer on a done task reopened it (state `up_next`, line \"Reopened\") and re-blocked what waited on it. Is true now: a comment never changes a task's state. Reopening is Send back: `state: up_next` on a done task by anyone who edits other than its owner, or with a `state_reason`, which the thread and `state_reason` record as \"Sent back by <name>: <why>\"."
---
