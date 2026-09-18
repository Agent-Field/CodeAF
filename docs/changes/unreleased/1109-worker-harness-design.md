---
kind: changed
title: the worker harness design replaces the task engine under /task and do, in waves on one branch
pr: 1109
surface: [docs, engine, chat]
invalidates:
  - "The bash-only task worker and the plan store were an experiment behind CODEAF_TASK_BELT=bash on the branch spark/bash-task-loop, judged not to replace the shipped belt. They are now the design for the task engine itself (docs/design/worker-harness/DESIGN.md): one worker, one bash tool, the plan in PlanDB, a runtime that owns the lifecycle, under both /task and codeaf do; the old engine stays reachable as CODEAF_TASK_ENGINE=legacy for one comparison and is then deleted."
  - "The experiment's report scored the bash-only worker 0 of 3 on the fan-out cell and read it as the worker not asking to split. The zero was a harness fault: the plandb shim ran the bench driver instead of the CLI, and the worker had tried to split. The measured cheaper-and-faster result stands; the capability loss does not."
  - "The plan store was one JSON file per run beside the session, archived when the run ended. The design makes it one store per home, in SQLite through the Go driver already in go.mod, with every row tagged by project and chat; scope is a filter on views, never a file boundary."
---

This entry records the design only; each wave that lands on the branch carries
its own entry naming what moved.
