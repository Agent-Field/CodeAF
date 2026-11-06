---
kind: fixed
title: proposed run workers receive only their assignment and add only their own children
pr: 2452
surface: [engine, chat, app, docs]
invalidates:
  - "Was true: proposed runs carried the conversation request, admission evidence and original-message pointer under a scope rule. Is true now: their documents contain only the proposed title, work, deliverable and acceptance."
  - "Was true: a run worker could add siblings or root children and evade depth limits by renaming tasks. Is true now: each worker adds only beneath its own task and has five counted additions, alongside the existing tree bounds."
  - "Was true: app reads dropped nonnumeric plan parents and a stop could leave open children below an ended root. Is true now: listed parents match listed task ids and stopping cancels lingering work even when the root already ended."
---

Worker additions retain their adding task in the live store and archive. Engine-created
handoffs, reviews and fixes do not consume the worker allowance; cancelled additions do
not count. Run-bound callers without a task binding retain their previous bounds and
default parent. Person-typed briefs, delegated program words and the older session-tree
road retain their existing documents.

App lists inherit recorded run groups and reflect stopped store states while worker
returns settle. A stop preserves earlier completed results and clears open holds.
Joined assignments and their descendants inherit their own ask. Woken workers distinguish
their own dispatched children from other work they must not redo. Setting a store aside
ends lingering open descendants even below a completed root; stopping an older retained
run can repair its children without touching the newer run. Released ready work reads
queued once its supervisor is gone, while a live run keeps its existing display.
