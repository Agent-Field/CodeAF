---
kind: fixed
title: a finished leaf's ending is journaled, so a restart restores it as done
pr: 606
surface: [engine]
invalidates:
  - "`store.PlanGraph` held the plan as it was planned, because `plans.journal` fired only at plan registration and on a revision that edited something. A node finishing journals too now, so a rehydrated document carries each landed leaf's `checked`, `result`, `cost`, `turns`, `tokens`, `stop`, `verdict`, `artifacts` and `calibration`."
  - "`cmd/aforge/chat.go`'s `recordOutcome` and `internal/exec/schedule.go`'s `apply` each wrote a leaf's ending onto its plan node in their own copy of the same assignments. `exec.Settle` is the one place that writes one, and `internal/exec/settle_test.go` refuses any other plain assignment to a plan node's `Checked` anywhere in the checkout."
  - "The headless scheduler wrote eight of the ending's fields and never `Calibration`, so the `profile.Record` `cmd/aforge/run.go` builds carried no account of the worker's own fit. It carries one now, on every landed node of `aforge plan run`."
  - "`head.renderPlan`'s comment said the journal is written when a plan is made and when it is revised, never once per landed leaf, so a document read on its own would report a finished job as entirely pending. That is no longer why states come off the durable rows: the document now carries a landed leaf's measurements, and the rows are read because they are what a node another process has since claimed is known by."
  - "A step's turns in the `plan` tool's rendering came off the journaled document and were therefore always absent. They are written down now, so the column says something."
---

The revision pass that decides a job's remainder reads a node's `Checked` — the
clause saying this step changed files and its own suite came back green — and
after a restart it never had one, so it went on proposing children to re-run
what had already been proved. The value existed in memory and never reached
disk. Both doors now settle a leaf through one seam, and the seam journals; the
store still owns what happened, so a journal saying done cannot overrule a row
another process has claimed. The write is best-effort in the sense the
surrounding code already means, and no bound was introduced — journalling a
sixty-node plan once per landed leaf is under three seconds of sqlite across a
whole run — so `PERF.md` is unchanged.
