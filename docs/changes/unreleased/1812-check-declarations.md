---
kind: fixed
title: task check declarations are refused before work when they cannot be rerun
pr: 1812
surface: [engine, docs]
invalidates:
  - "Task creation and check replacement accepted compound commands or variable-dependent declarations that the completion gate could never approve. Admission now asks that gate's existing predicate before changing the plan, and explains how to declare separate commands from the task-copy root."
---
