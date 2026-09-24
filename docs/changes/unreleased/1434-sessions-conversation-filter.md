---
kind: changed
title: Sessions filters whole conversations by name, project or nested task
pr: 1434
surface: [chat, docs]
invalidates:
  - "Filtering Sessions by a task name kept only matching tasks and their ancestors. It now keeps the entire owning conversation, including unmatched siblings and descendants."
  - "Project paths and task display labels could fail to find their conversations. Sessions now matches those alongside conversation names, project names and full task titles, including conversations without tasks."
---
