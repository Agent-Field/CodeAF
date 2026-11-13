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

The one conflict was in the run's notice: dev's `noticeResultWords` is kept. The
pre-merge review and a hand test found defects the carries brought along, fixed here and
described in the carried entries: a per-task raise did not reach the tree ceiling, the
node road counted a folded part's bill again in its parent, the per-task limit sentence
was appended (twice when the guard refused the root, and off the row when the engine's
ceiling ended the run), a pivot counted the tasks it replaced, and the tasks page said a
proposed run reads the person's message. A run the engine ended done whose final receipt
crossed the ceiling is not a limit ending: it keeps its completion and no limit stop.
