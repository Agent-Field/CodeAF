---
kind: fixed
title: Factory verification waits for owned checks and preserves usable results
pr: 1899
surface: [engine, chat]
invalidates:
  - "An idle model turn could complete a factory step on a progress report and terminate a healthy background suite. Checkpoint finality now reaches the runner; stage-owned checks and their completion delivery keep the round live without provider polling."
  - "Every new Test conversation forgot earlier measured passes. Finished rounds now contribute independently qualifying current-checkout evidence with original provenance, while unresolved requirements remain visible."
  - "Nested negative-test fixtures could become permanent job failures. New observer records carry an explicit owning worktree boundary and recognized parent checks isolate their descendant observers."
  - "A restart could revoke a user-granted round extension or replace earlier round history. Effective limits persist, histories merge by conversation, and same-job replacement waits for executor cleanup."
  - "Worker report refusals could discard results by advancing their baseline. Reports now freeze their request and replay key, retain unconfirmed results, and stop explicitly on bounded or deterministic delivery failure."
---

Running-check purpose and start time cross the additive worker header and job
read model, so the desktop can show a real check wait inside its existing shared
Activity. Production app sleep/wake now reports runner presence. Manual keeps
its existing approval meaning; a check wait does not become a failed assertion.

If the initial delivery artifact cannot be saved, cleanup retains the original
mirror so unacknowledged results and spending are not erased. Completion refusals
also preserve the recovery owner's newer stopped assessment instead of withdrawing
it with the model's stale claim.

This is incident containment. It does not add an explicit coverage manifest,
transactional cross-crash accounting, automatic old-lease replay, process
reconciliation after arbitrary crashes, or bounded asleep-lease retention.
