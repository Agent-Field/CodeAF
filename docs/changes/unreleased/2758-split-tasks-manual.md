---
kind: changed
title: The manual explains the split Tasks list and selecting several tasks
pr: 2758
surface: [app, docs]
invalidates:
  - Tasks used to replace the list with a task page at every width; a wide content area now keeps the list beside the task.
  - Command-click in the Tasks list used to open a task tab; it now toggles selection for bulk Done by.
---

The Tasks page documents the 500-pixel list, arrow navigation, the 1100-pixel content-width and open-Afi fallback, and selecting several tasks. Bulk Done by uses the same allowed doers as one task, keeps ownership and results, and refreshes the list after a failed write. Heading probes keep the new questions reachable.
