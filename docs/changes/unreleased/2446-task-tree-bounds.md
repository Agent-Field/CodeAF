---
kind: fixed
title: task workers keep their own assignment and bounded task trees
pr: 2446
surface: [engine, chat, docs]
invalidates:
  - "Was true: conversation proposals opened their workers on the whole conversation request, including instructions to hand out tasks. Is true now: proposed run workers read their own work first and carry the conversation request under a rule that scopes it to that assignment."
  - "Was true: run-worker task creation had no duplicate-title, depth or work-count gate, and a crewless run could have no dollar ceiling. Is true now: worker growth refuses duplicate titles, more than two descendant levels or more than 20 counted tasks, and ordinary conversation runs share the configured per-task ceiling across their whole tree."
---

Growth refusals are atomic across concurrent workers and leave a refused batch unwritten.
Countdown-approved proposals joining a live run obey the same gate; explicit approval and
typed tasks keep their existing join authority. Task-cap call refusals stop the entire run
as a dollar-limit ending, while a checker's own ceiling remains local to its check. The
report names the configured task limit and `/crew` when that ceiling binds. Delegated
programs retain their own ceilings.

Continuing an interrupted run subtracts its persisted spend from the task allowance
and retains the configured-limit explanation. Only checks seated by the review round
are exempt from the task count; a worker's `--role check` tasks still count. Existing
cost and elapsed limits retain completed-work precedence; only a typed task-wide
spend refusal suppresses completion.
