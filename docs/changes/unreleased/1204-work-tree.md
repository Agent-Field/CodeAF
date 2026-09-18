---
kind: changed
title: the task rail and the task page draw a belt run's plan as a tree, and the work tab is designed
pr: 1204
surface: [chat]
invalidates:
  - "A held plan row was drawn under the task it waits on, out of its own family. A row sits under the task that requested it, always; a dependency shows as `queued · waits: <task>` on the row and never moves it."
  - "The rail listed every plan task at every state. A family whose every task is done or failed folds to one line with its count; a family with running or queued work stays open, and `enter` on the folded line opens its page."
  - "A task's page showed one level of children. It shows the whole subtree under its steps, each row with its live `$ <command>` line, `· N queued behind it` on a row other open tasks wait on, and a `waits` section in both directions; `enter` on a subtree row opens that task's page and `esc` returns."
---

The owner ran a `/task` under the belt and saw a flat list of tasks with no
relation between them. The store is a graph and the workers write it; the
surface now draws it. `docs/design/worker-harness/TREE.md` and `WORK-TAB.md`
are the rulings; the dot row, the rail's activity order and the home
dashboard's `work` tab land in this pull request as their own cells.
