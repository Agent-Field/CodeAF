---
kind: fixed
title: standing order project scoping normalization, live rail allowance words, and task activity labels
pr: 1555
surface: [chat, engine]
invalidates:
  - "Standing orders with project altitude previously failed workspace equality checks when comparing cleaned and uncleaned paths. Path cleaning is now normalized for project-scoped orders."
  - "Standing proposal cards previously quoted stale default budget allowances. They now resolve against the live daily spend rail."
  - "Standing card history rows previously displayed 'said:' for task action firings. They now display 'task:' for task executions."
---
