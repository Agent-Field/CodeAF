---
kind: fixed
title: codeaf app says what an ask holds up from the task graph, never from a model's words
pr: 0
surface: [engine]
invalidates:
  - "Was true: an ask's `holding_up` was the asker's own list of blocked work kept only where an entry happened to be a number, so a model's task names were dropped, a stray number became a task id, and an ask from a task did not count the tasks waiting on it. Is true now: `holding_up` is set only on an ask whose source is a task, and is that task plus every open task whose `waits_on` reaches it (nearest first, each once, at most 256), read from the same dependency graph as `Task.holding_up`; an ask from no task carries none."
---
