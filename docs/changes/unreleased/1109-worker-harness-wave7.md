---
kind: changed
title: the worker harness wave seven — the work seat, the belt's pages, parent wakes, the run's store
pr: 1109
surface: [engine, chat, remote]
invalidates:
  - "A run worker sent no reasoning field, so the depth a person configured was paid again on every round of a one-action loop. It answers at the work seat now — low reasoning, and a rung set on the task, the conversation or the turn is still above it."
  - "A bash-belt worker opened on the chat's whole system page: a pane, slash commands and ten tools its belt does not carry. It reads the belt's own two pages alone — the loop policy and the bash doctrine — and not one byte of the chat page or the task worker's page."
  - "A parent was not run again when its children landed: the store auto-completed the composite. It is woken with each child's title, status and result, up to four times, and the report it gives then is its own result — for the root, the run's result."
  - "The run's `plandb` was resolved on the host PATH, so a worker found a stranger's `plandb` and wrote 19 of 85 tests into a store the run never reads. Every command the belt runs is bound now: an export puts the run's shim directory first on PATH and pins `PLANDB_DB` to the run's own store, for the one shell process the call runs and nowhere in the shared environment."
  - "The task rail listed a run's plan flat: every task at column zero, and the only relation a person could read was the word `waits:` on a held row. The rail draws the plan as a tree now — a child indented under the task that requested it, a task held behind work that is not its parent drawn under what it waits on, still wearing `queued · waits: <that task>` — and a task's page shows its children under its steps, each with its live step."
  - "A run whose root did the whole job alone and ended it with `plandb done root` was never reviewed: the store refused a check under a terminal root and the run completed unchecked. A root is not done until its check has landed, whoever wrote its ending: the store reopens it for that one check, a `does not hold` finding opens a `fix:` task the root waits on, and a check's landing wakes nobody."
  - "A task in a belt run could not say which commands prove it, so the review check had nothing it was allowed to run. `plandb add --check '<command>'` declares a task's checks on the node; the check task carries them and runs them first, and a task that declares none is checked by reading against its acceptance alone."
  - "The belt refused `git clone` in a workspace that is not a repository. Where the folder is not inside a git work tree every git verb — clone, checkout, fetch, pull — passes, and inside a repository every refusal stands."
  - "A fast run could answer before its last worker ended, and that worker's spend row or completion met the store the run had just closed — a nil handle and a panic in the middle of a report. Every road out of the run drains its workers first, and a closed store answers `plandb.ErrClosed` to each write and each read the database holds rather than this handle's memory."
  - "The spend page's tasks-by-seat block read the engine directly, so over `--host` it drew nothing and was silently absent. The seat rollup crosses the wire now, and an engine older than the new door answers `no such method`, which the page reads as nothing drawn."
---

Wave 7 of docs/design/worker-harness/DESIGN.md: the fixes the corpus reruns
and the wire audit turned up on the run engine. The work seat stops a run
worker paying the person's depth on every round; a belt worker reads the
belt's own pages; a parent is woken with its children's landings and reports
them as its result; the run's `plandb` is bound to the run's own store on
every command; git is free in a workspace that is not a repository; a run
drains its workers before it answers so a closed store refuses instead of
panicking; and the spend page's seat block crosses `--host`. The measured
reruns are in docs/design/worker-harness/BENCHMARKS.md, and the pull-request
body is docs/design/worker-harness/PR-BODY.md.
