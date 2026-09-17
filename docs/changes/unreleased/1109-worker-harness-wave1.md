---
kind: added
title: the worker harness's first wave lands behind CODEAF_TASK_BELT=bash — the bash belt, the plan store on SQLite, and the shell doors
pr: 1109
surface: [engine, chat, build]
invalidates:
  - "The bash task belt's plandb shim exec'd os.Executable() blind, so an engine embedded in another program (the bench driver) sent every plandb call to that program. It now probes for a binary that answers `plandb status` — CODEAF_PLANDB_BIN, the running binary, a sibling plandb, a codeaf on PATH — and records, rather than swallows, a shim it could not arm; the shim directory rides the worker's own command environment, never the process PATH."
  - "A bash-belt worker's shell edits never reached its branch: landing staged only the write/edit ledger, which a shell never fills. Landing now stages the tree's own git status for a bash-belt node, minus what the harness itself writes, and the left-behind sentence no longer loses the first character of every path."
  - "The plan pulse ran only after a bash call or a landing, so a ready task could wait on another worker's next command and root completion could cancel it first. The pulse also runs when a bash-belt worker ends its turn and when a fan slot is freed, and a pass that handed work out never completes the root."
  - "The plan store was one JSON file with an flock sidecar per run. It is plandb.db, SQLite through the Go driver already in go.mod, one transaction per write, WAL once per file; the lock sidecar and both lock_*.go files are gone."
  - "The bash worker's system prompt was the composed chat page with a swapped section. It now opens on the loop policy (prompts/bashtask.md), then the belt's doctrine, then the kept constraints (prompts/bashrules.md); `ask` is off the bash belt; a plan-born child opens on its own task id with the person's request as background; the per-step frame carries steps left, free slots and child news."
  - "The plan store refused a hard dependency across a containment lineage, which the worker page taught as allowed. It is allowed now (ancestor and descendant edges still refused); `task insert --before` lifts the edge it replaces; critical-path, bottlenecks, overview, pivot and note answers carry the shapes the page promises."
  - "The hands a shell could not reach — the billed document parse, web fetch and search, image generation, an exact-match edit — are `codeaf doc`, `codeaf web fetch|search`, `codeaf image --out` and `codeaf patch`, each on the same function its tool runs."
---

Wave 1 of docs/design/worker-harness/DESIGN.md. Everything here is behind
CODEAF_TASK_BELT=bash except the four shell doors and the plan store package,
which are new surfaces; the shipped belt and the conversation are unchanged.
The comparison against the shipped engine is bench/bashloop on the Spark, and
its rows are recorded on the pull request as they land.
