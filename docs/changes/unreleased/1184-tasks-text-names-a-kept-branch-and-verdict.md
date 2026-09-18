---
kind: added
title: The `tasks` text and the record line name a failed node's kept branch and verdict
pr: 1184
surface: [engine]
invalidates:
  - "The `tasks` text and a task's record line named no branch and no verdict for a node that ended failed or unverified. Both now name the kept `task/<slug>` branch and the record's own verdict word (`failed`/`unverified`), the same two words #1182 put on the headless envelope."
  - "`TaskIndexEntry` carried a node's kept branch only inside its `artifactUri` (`git:<branch>`). It now carries the branch verbatim in a `branch` field, and a `tasks` row drops the duplicate `artifact` spelling when it only repeats it."
---

A `/task` node that ends failed or unverified keeps its deliverable on its own
`task/<slug>` branch and does **not** merge it home ([`keptWork`], #1178). The
headless `do` envelope already names that branch and its verdict (#1182:
`kept_branch`, `verdict`). This is the chat-side counterpart: the same two facts
now reach a person and a model through the `tasks` tool's plain text and through
the record line a resumed session opens with — in the record's own words, so a
reader joining the two surfaces reads one vocabulary and not two.

**The record line.** A resumed graph's summary ([`taskRecovery.note`],
`internal/session/task_store.go`) counted failed and unverified nodes
([`taskRecovery.countSettled`]) but dropped their branch, so a person told
`1 incomplete` had nowhere to go and look; only `interrupted` nodes named a kept
branch ([`taskRecovery.branches`]). The two settled states now collect their kept
branches beside it and wear the same `(branch <b> kept)` clause, reusing
[`keptBranches`]. A node that kept no branch — one that never reached a
repository — gains no clause.

**The `tasks` text.** The row a `tasks` answer is built from
([`TaskIndexEntry`], `internal/session/task_index.go`) carried the branch only
inside `artifactUri`. It now carries it verbatim in a new `branch` field, set
from the node's own branch and merge word ([`keptBranchOf`]). The row text
([`taskWhereClauses`], `internal/session/tools_tasks.go`) names `kept branch <b>`
— and drops the bare `artifact git:<b>` when it only repeats that — and names the
record's verdict word, `failed` or `unverified`, read from the row's own `Status`
([`TaskFailed`]/[`TaskUnverified`]) exactly as #1182's envelope carries `verdict`.

Only the record and the plain text moved: the surface renderer is untouched and
reads the branch and verdict off the record the way it always reads a row.

[`keptWork`]: ../../internal/session/task_run.go
[`taskRecovery.note`]: ../../internal/session/task_store.go
[`taskRecovery.countSettled`]: ../../internal/session/task_store.go
[`taskRecovery.branches`]: ../../internal/session/task_store.go
[`keptBranches`]: ../../internal/session/task_store.go
[`keptBranchOf`]: ../../internal/session/task_store.go
[`TaskIndexEntry`]: ../../internal/session/task_index.go
[`taskWhereClauses`]: ../../internal/session/tools_tasks.go
[`TaskFailed`]: ../../internal/session/task_contract.go
[`TaskUnverified`]: ../../internal/session/task_contract.go
