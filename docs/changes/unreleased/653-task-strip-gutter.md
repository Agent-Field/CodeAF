---
kind: fixed
title: Align compact task navigation with the reading gutter
pr: 653
surface: [chat]
invalidates:
  - "Compact task chips started at the terminal edge while the task page and composer were inset. The strip now shares their reading gutter, with task, overflow and running-work pointer targets shifted together."
---

The strip is laid out inside the available width before the gutter is added.
Its chip padding remains inside that shared margin. Phone-sized windows keep
their existing whole-row task door without an added gutter.
