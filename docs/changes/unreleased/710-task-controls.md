---
kind: changed
title: Task pages group scoped model and Stop controls below the task tree
pr: 710
surface: [chat, docs]
invalidates:
  - "Inside a task, /model previously retained conversation scope or sent an argument to the worker. It now opens or changes the open ordinary task's model; unsupported task types refuse without changing the conversation."
  - "Stopping work was exposed through the task header and an empty-input x shortcut. Expanded task pages now keep a Stop action below the tree, and /stop opens the same scoped confirmation."
---

The expanded task page separates ancestor navigation, the task title and live
facts above the divider. The existing task tree remains first in the right column;
optional conversation context scrolls in its own bounded window, while model,
supported thinking controls and Stop reserve their rows below it. The input names
its recipient and the footer labels conversation totals. Compact frames retain
the existing rail and header controls. Model changes apply on the next task turn.
