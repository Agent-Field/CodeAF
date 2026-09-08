---
kind: fixed
title: a leaf is judged on the job's tree, not on what it happened to write itself
pr: 628
surface: [engine, docs]
invalidates:
  - "The wire settlement #464 added — a leaf marked failed by the wire after its work landed is judged on the tree — used to be reachable only by a leaf that had written a file OF ITS OWN. `cmd/aforge/settledOnTheTree` guarded on the leaf's own artifact list while handing `gateEvidence` the job's merged record three lines below it, so the guard and the gate read two different lists. A repair or resumed leaf writes nothing of its own, because the work landed under the attempt before it, and it therefore reached that guard with an empty list BY CONSTRUCTION: one measured run bought a repair round, had the repair worker's first and only call refused by the provider in 33 ms, and reported a failure over a job record that named the fixed file and a tree whose checks were green. The guard, the `Files:` block in the delivery and the gate's own record now read one value, `jobArtifacts(record, artifacts)`, assembled ONCE above the guard — once and not at each seam because `errandRegistry.list` filters against the disk, so two readings are two lists and a settlement that admitted work on one and judged another would be the same defect at a new address."
  - "#464's entry said the settlement widened nothing, and listed \"a leaf that failed on the wire having written no file\" among the endings that fail exactly as they did. That sentence is now wrong by one shape and right about the rest. A leaf refused before its first move is judged on what the JOB has left on the tree, exactly as a delivered one would be. What still fails untouched: a run that left nothing anywhere (which buys no gate call and no request question at all), a leaf whose own work errored, a leaf the clock ended, a gate that could not be reached, a request answered no, and a settlement whose journal row could not be written. `settledOnTheTree` is handed a job record by the headless errand and by no other driver, so every other door behaves as it did."
  - "`internal/manual/chat/adaptive-runs.md` told a person the tree review applies where \"the worker left files behind\", and that a worker cut off having written no file \"still fails as it did\". Both stopped being true. The page now says what is judged is whatever the RUN left on the tree — this worker's files or an earlier attempt's — and names the case a person arrives with: a repair round whose first call was refused dies before it reaches a tool, and that retry is judged on the fix already on disk."
  - "`cmd/aforge/gatesettlement_test.go`'s `TestEveryDeliveryGateIsHeldToTheJobsRecord` demanded that the argument after `gateEvidence(node, task.Spec, outcome,` be a literal `jobArtifacts(` call. It now also accepts the name that merge is bound to, and only where exactly one binding in `chat.go` gets that name from `jobArtifacts` — so the law it states is unchanged and the binding is part of what it watches."
---

A gate is held to the job's record and not the node's; `docs/design/gate/SETTLEMENT.md`
§5 settled that after a repair round was refused over files that were sitting on disk.
The wire settlement added afterwards asked the same question one line earlier and kept
its own narrower answer to it — so the run that most needed the settlement, a repair
leaf whose provider refused it before it could reach a single tool, was the one leaf
shape that could never reach it.
