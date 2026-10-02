---
kind: fixed
title: the tasks side column draws a family on its own tree connectors again
pr: 1665
surface: [chat]
invalidates:
  - "The tasks side column drew no rail lines: a run's parts hung bare two spaces a level (`strings.Repeat(\"  \", depth)` in planrail.go), which is what #1494 left behind when it dropped the roster forest. A child now rides `├ ` while a sibling follows it and `└ ` where it closes its parent's family, a deeper row carries the `│ ` trunk past every ancestor that had rows still to come, and the last child's levels leave air — drawn in the lead the caller prepends, two cells a level, on the side column and on a task page's `under it` alike."
  - "Two manual pages spelled the task page's connectors `├─`/`└─` (reading-a-task-page.md, worker-harness.md); they spell the drawn `├ `/`└ ` now. The side column's node rows are still one flat line per task — #1494's roster shape stands; the connectors belong to the families hung under a row."
---

The column's families were bare indentation since #1494, so a run's shape did not
read: a child's title floated right of its parent's with nothing anchoring it, while
the manual (tasks.md) and the plandb CLI tree kept documenting and drawing
connectors. The rail lives in the lead again — one helper, `railLead`, builds it
for both renderers out of the ancestors' last-child flags; `railEntryRow` is
untouched, the lead stays exactly two cells a level so every width computation that
reads it keeps its shape, and a dependency still never re-parents a row.