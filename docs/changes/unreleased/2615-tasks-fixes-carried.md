---
kind: internal
title: Carry the four task-tree fixes that only reached the workstreams branch onto dev
pr: 2615
surface: [engine, chat, docs]
invalidates:
  - "Was true: the chat manual described the per-task money limit inside the section about the composer's spend cap. Is true now: the tasks page has its own section, The per-task limit covers the whole task tree, and the composer section points to it."
---

Task-tree bounds, worker task scope, a ceiling for each check and the older task
road's bounds (2446, 2452, 2461 and 2464) had merged only into `workstreams/main`,
whose integration pull request was closed unmerged. They are carried here as
separate commits and keep their own change entries.

The one conflict was in the run's notice: dev's `noticeResultWords` is kept and the
per-task limit words are added to its report.
