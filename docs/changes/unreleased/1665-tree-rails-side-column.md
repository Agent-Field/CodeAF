---
kind: fixed
title: the tasks side column draws a family on its own tree connectors again
pr: 1665
surface: [chat]
invalidates:
  - "The tasks side column drew no rail lines: a run's parts hung bare two spaces a level (`strings.Repeat(\"  \", depth)` in planrail.go), which is what #1494 left behind when it dropped the roster forest. A child now rides `├ ` while a sibling follows it and `└ ` where it closes its parent's family, a deeper row carries the `│ ` trunk past every ancestor that had rows still to come, and the last child's levels leave air — drawn in the lead the caller prepends, two cells a level, on the side column and on a task page's `under it` alike."
  - "Two manual pages spelled the task page's connectors `├─`/`└─` (reading-a-task-page.md, worker-harness.md); they spell the drawn `├ `/`└ ` now. The side column's node rows are still one flat line per task — #1494's roster shape stands; the connectors belong to the families hung under a row."
  - "A family deeper than the column's levels drew every row past the cap at the cap's own depth — a child hung beside its parent — and a lead that left less than the floor dropped the row and its whole subtree without a word. One ellipsis row now hangs at the boundary where the levels run out and is the door onto the branch it names (planrail.go's [app.railBoundaryRow])."
  - "A task page's `under it` measured its level budget on the page's own width while drawing its parts in a column at most [planPageKinWidth] cells wide, hanging families deeper than their titles could afford; the cap is now measured on the column it protects (planroom.go)."
  - "The branch's last commit attempted to open every side-column row with its task's number — a railFigWord call that was never written, so nothing at head built, and a shape that contradicts the column's settled laws: one state glyph leads every row in the same number of cells (railclick_test.go), the roster's rows carry no id (railgroups_test.go), and the branch's own c266 pin held the same. A number that varies in width cannot lead rows that must read downward as one column of states; the idea is unlanded and returns to its author for a surface that can hold it."
---

The column's families were bare indentation since #1494, so a run's shape did not
read: a child's title floated right of its parent's with nothing anchoring it, while
the manual (tasks.md) and the plandb CLI tree kept documenting and drawing
connectors. The rail lives in the lead again — one helper, `railLead`, builds it
for both renderers out of the ancestors' last-child flags, and `railRowPlace` is
the one place a row's place in the tree is decided; the lead stays exactly two
cells a level so every width computation that reads it keeps its shape, and a
dependency still never re-parents a row. A family deeper than the column is
named at the boundary by one ellipsis row that is the door onto it, and the
branch's attempt to open every row with its task's number is unlanded — it
cannot hold the column's one-glyph-same-cells law, and is returned to its
author.