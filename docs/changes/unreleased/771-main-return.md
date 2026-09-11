---
kind: added
title: A pinned Back to main row returns directly from nested tasks
pr: 771
surface: [chat]
invalidates:
  - "Returning from a task relied on the header or breadcrumbs. The task column now pins a clickable Back to main row above its list at every depth."
---

The row stays visible while the task list scrolls, highlights under the pointer,
and returns directly to the conversation without stopping work. It is absent
when the main conversation is already open.
