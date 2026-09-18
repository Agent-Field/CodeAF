---
kind: fixed
title: the auditor's calls say they are the auditor's and which node they check
pr: 1181
surface: [engine]
invalidates:
  - "An auditor's provider calls were indistinguishable from a conversation's own turns in every record: the model-call log tagged both `turn` and named no node, and the usage ledger wrote no role and no task on either. A check and a session turn on the same model were the same row. The auditor's calls now tag `auditor` — its role's own word — and name the id of the node they check; the usage row carries `role: auditor` and that node's `task` on the high seat."
  - "`Config.taskID` is the node an agent IS and stays 0 for the auditor, because the auditor is not the node it reads. The node it CHECKS now travels in the new `Config.checksNode` fact, set by `newAuditAgent` from the node it was handed."
---

The auditor is rebuilt for one node at a time (`newAuditAgent`), has that node in
hand, and recorded nothing of it: `crewRole` was already set so the router priced
its calls as a judge's, but the two reader-facing records never learned either
fact. This is the smallest cut that gets them there.

`loop.go` stamps `callPurpose(a.config.crewRole)` and `WithCallNode(checksNode)`
for an agent that answers for a crew role, before the turn/task branch a worker
takes — the role's word is a role's, not a new constant in `clientdoor.go`'s
block of non-roles, and it resolves through `roles.RoleAuditor` like every other
role-worn purpose. `addUsage` banks the turn with that same role word so
`TagUsage` writes it beside the seat the auditor already billed to, and
`usageNode` files the row under the node the auditor checked rather than the
`taskID` it deliberately leaves empty.

The progress check's auditor (`task_run.go`) shares `newAuditAgent`, so it carries
the same two facts without a second change. The pool's record was read and left
alone, and no file under `internal/pool/record` changed: its `Row` is one row per
judged seat score — metric, seat, model, score, judge, door, size, day — written by
`Recorder.Record` out of judge scores at the pool's own `record` command and never
out of a provider call, and it carries no call count and no cost. An auditor's tag
and the node it checks have no field there to land in.
