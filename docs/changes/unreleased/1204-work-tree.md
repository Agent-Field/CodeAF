---
kind: changed
title: the task rail and the task page draw a belt run's plan as a tree, and the work tab is designed
pr: 1204
surface: [chat]
invalidates:
  - "A held plan row was drawn under the task it waits on, out of its own family. A row sits under the task that requested it, always; a dependency shows as `queued · waits: <task>` on the row and never moves it."
  - "The rail listed every plan task at every state. A family whose every task is done or failed folds to one line with its count; a family with running or queued work stays open, and `enter` on the folded line opens its page."
  - "A task's page showed one level of children. It shows the whole subtree under its steps, each row with its live `$ <command>` line, `· N queued behind it` on a row other open tasks wait on, and a `waits` section in both directions; `enter` on a subtree row opens that task's page and `esc` returns."
  - "A plan row on the chat's rail took two to four lines: the tasks page's stats line wrapped under the title, and the steps and money were drawn a second time under a running row's live command. A plan row on the rail is one line — the connector, the state mark, the fitted title — with `waits: <task>` at the end of a held row's line (never cut short of the task's name) and the one `$ <command>` line under a row with a step in flight; the run's own row ends in the dot row at the rail's width tier, and a run of one task shows no dots."
  - "The chat's rail drew one flat row per `/task` and nothing of the tasks a worker added to the plan; the run's tree was only on the tasks page. The rail draws the run's tree out of the tasks place's own reading, in a conversation that never opened that page, and the run's row carries its progress on a run rooted at a task's number, where it used to be counted only for a root named `root`."
---

The owner ran a `/task` under the belt and saw a flat list of tasks with no
relation between them. The store is a graph and the workers write it; the
surface now draws it. `docs/design/worker-harness/TREE.md` and `WORK-TAB.md`
are the rulings; the dot row, the rail's activity order and the home
dashboard's `work` tab land in this pull request as their own cells.

A plan row on the rail spent up to four of the column's lines, because the
rail drew the tasks page's own two-line card and its under-block. The rail is
read beside a conversation somebody is typing into, so its projection of the
run is one line per task with the state's tail at the end of the line, and the
steps and money stay on the page's rows, which have the width for them.
