---
kind: changed
title: A task can run as one worker over a plan store, beside the node engine
pr: 1109
surface: [engine, chat, build]
invalidates:
  - "A task always ran on the node engine: a `TaskNode` of the session's own tree, in a worktree of its own, judged by a second model. A task can also run on the run engine — `internal/run`'s supervisor over one `internal/plandb` store, one bash-belt worker per task in the run's one working copy — and the node engine is still in the binary beside it."
  - "`CODEAF_TASK_BELT` is the switch between the two, not a word that stopped existing. Unset, it is the run engine; `node`, `legacy` and `off` reach the node engine (#1355 has the current reading)."
  - "`propose_task`, `quick_task`, `divide_work` and `revise_assignment` are still the node engine's doors, and still on its belt. A worker on the run engine splits and steers its work through the plan store instead, by running `plandb` through bash."
  - "The belt refused `git clone` in a workspace that was not a repository. The task's git guard reads the workspace root now: where the folder is not inside a git work tree every git verb — clone, checkout, fetch, pull — passes, and inside a repository the refusals stand."
  - "A parent was not woken when its children landed: the plan pulse ran only after a bash call or a landing. It also fires at a bash-belt worker's turn end and when a fan slot goes back, so a task that became ready is dispatched instead of waiting on some worker's next bash call."
  - "The run's `plandb` was not on the worker's PATH. Every belt command is now prefixed with the run's armed shim directory, so the worker reaches the run's own plan CLI and the process environment is never touched."
---

This entry once said the node engine was deleted and `CODEAF_TASK_BELT` no
longer existed. Neither happened: the node engine — `internal/session`'s
`TaskGraph`, its frontier of `TaskNode`s, a child agent per node in a
`git worktree` of its own, and the verbs that reach it — is in the binary, and
`CODEAF_TASK_BELT` chooses between it and the run engine. What this wave added
is the run engine beside it; #1340 and #1355 are where the default moved, and
`docs/design/worker-harness/LEGACY-INVENTORY.md` maps the two.
