---
kind: fixed
title: the tasks side column draws a family on its own tree connectors again
pr: 1665
surface: [chat]
invalidates:
  - "The tasks side column drew no rail lines: a run's parts hung bare two spaces a level (`strings.Repeat(\"  \", depth)` in planrail.go), which is what #1494 left behind when it dropped the roster forest. A child now rides `├ ` while a sibling follows it and `└ ` where it closes its parent's family, a deeper row carries the `│ ` trunk past every ancestor that had rows still to come, and the last child's levels leave air — drawn in the lead the caller prepends, two cells a level, on the side column and on a task page's `under it` alike."
  - "Two manual pages spelled the task page's connectors `├─`/`└─` (reading-a-task-page.md, worker-harness.md); they spell the drawn `├ `/`└ ` now. The side column's node rows are still one flat line per task — #1494's roster shape stands; the connectors belong to the families hung under a row."
  - "A family row opened with its state glyph and nothing else, so a family of cut titles could not be told apart by number. A row now opens with its task's number — `#6`, the handle the store names a stored row by, drawn dim — wherever the column has room for it beside the whole name, and the number yields its cells whole the moment keeping it would cut a name (taskident.go's [app.railFigWord])."
  - "A family deeper than the column's levels drew every row past the cap at the cap's own depth — a child hung beside its parent — and a lead that left less than the floor dropped the row and its whole subtree without a word. One ellipsis row now hangs at the boundary where the levels run out and is the door onto the branch it names (planrail.go's [app.railBoundaryRow])."
  - "A task page's `under it` measured its level budget on the page's own width while drawing its parts in a column at most [planPageKinWidth] cells wide, hanging families deeper than their titles could afford; the cap is now measured on the column it protects (planroom.go)."
---

The column's families were bare indentation since #1494, so a run's shape did not
read: a child's title floated right of its parent's with nothing anchoring it, while
the manual (tasks.md) and the plandb CLI tree kept documenting and drawing
connectors. The rail lives in the lead again — one helper, `railLead`, builds it
for both renderers out of the ancestors' last-child flags, and `railRowPlace` is
the one place a row's place in the tree is decided; the lead stays exactly two
cells a level so every width computation that reads it keeps its shape, and a
dependency still never re-parents a row. A family row opens with its task's
number where the column has room for it beside the whole name, and a family
deeper than the column is named at the boundary by one ellipsis row that is
the door onto it.