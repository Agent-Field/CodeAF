---
kind: changed
title: A task is one worker and a plan store; the older node engine is deleted
pr: 1109
surface: [engine, chat, build]
invalidates:
  - "`propose_task`, `quick_task`, `divide_work` and `revise_assignment` were a task's and a worker's doors to the node engine. They are gone; a task splits and is steered through the plan store its worker reaches by running `plandb` through bash."
  - "The `tasks` window read this session's own task graph. Its engine half is gone; a run is read from the plan store, and the rows a pane draws come from the store and the project index."
  - "`CODEAF_TASK_BELT` chose between the shipped node belt and the bash belt, and `CODEAF_TASK_ENGINE=legacy` kept the node engine reachable. Neither exists; the run engine is the only task engine and it wears one belt."
  - "`codeaf do` dispatched a task through the node engine's own door. It is a run with no chat attached over the same plan store, and its exit ladder, JSON envelope and usage ledger are unchanged."
  - "A task node was judged by a second model that read its worktree and answered VERIFIED or REFUTED. There is no auditor call; a task cannot finish while a child or a dependency is open, and the action that asks to finish must exit 0."
  - "A worker handed its parts to `divide_work` and each ran in a worktree of its own. Parts are `plandb split` tasks in the run's one working copy, and the run's landing is a single commit on its root."
---

The node engine was `internal/session`'s `TaskGraph`: a frontier that admitted
`TaskNode`s, a child agent per node in a `git worktree` of its own, an auditor,
and the verbs that reached it. Its replacement is the run — `internal/run`'s
supervisor over one `internal/plandb` store, one bash-belt worker per task, all
in the run's one working copy, coordinated through the store. This entry lands
with the deletion; `docs/design/worker-harness/LEGACY-INVENTORY.md` is the map
of what went and what stayed, and the manual pages and prompts that named the
old tools change in the same commit.
