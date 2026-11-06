---
kind: fixed
title: the older task road refuses duplicate work in a tree and holds the tree to one spend total
pr: 2464
surface: [engine, chat, docs]
invalidates:
  - "Was true: with CODEAF_TASK_BELT=node two tasks in one tree could hand out the same work, and only each node's own bill was limited. Is true now: duplicate titles are refused tree-wide and the whole tree shares one spend total."
---

The duplicate check uses the plan store's title normalization, so both task roads compare
work the same way. The tree total is the lower of the per-task limit (crew.task_cap) and
the conversation's remaining limit; reaching it stops the whole tree and names the limit.
A typed task starts its own tree. The default road is unchanged.
