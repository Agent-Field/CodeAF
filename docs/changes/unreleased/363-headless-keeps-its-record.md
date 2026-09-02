---
kind: fixed
title: a failed headless run keeps its record, and every headless call names its tag and node
pr: 363
surface: [engine]
invalidates:
  - "`aforge do`'s private store was deleted on the way out whether the run worked or not, and `--keep` was the only way to keep it — so it had to be asked for before anybody knew there would be anything to look at. It is now deleted only on a clean exit. Any other exit — a failure, a refusal, the partial exit 2 — keeps it and prints `record kept at <path>` on the error stream. `--keep` still keeps it always, and so does `AFORGE_DEBUG` set to anything but `0`, `false` or `off`."
  - "The model-call log's node column was empty for whole classes of headless call. It is not any more: the briefs and contracts of a plan name the node they are about, an expansion names the node it is splitting, the delivery gate, the remainder check, the acceptance map and the reshape name theirs, and `aforge exec` names its single leaf `task-1`. A headless plan leaf's rows were anonymous because the executor read `Task.NodeKey`, which that path never sets; it reads `leafKey()` now, which is the number in `NodeID`."
  - "The errands a job makes on its own — distil, narrate, title, consolidate, reflect, sentinel, quorum, craft-repair, craft-params, morning-brief — wrote rows with no tag at all, because they open no routing slot for one to be derived from. `errandContext` now tags each of them with the errand's own name. `plan.Satisfied` used to read as `audit` in the log, which is the routing class it shares rather than the question it asks; it says `satisfied`."
---

A person discovers they wanted a headless run's record after the run went wrong, which under
the old rule was always after it was gone. The keep decision is now one pure function every
headless exit passes through — `keepPrivateStore` in `cmd/aforge/do.go` — and it deletes only
what a clean run made. `AFORGE_DEBUG` is registered as operator plumbing and read directly
here; the package that will own the switch is landing in parallel.

The attribution half is set where the fact is known rather than patched per row: the node
where the pass is about one node, the tag where the errand already names itself.
