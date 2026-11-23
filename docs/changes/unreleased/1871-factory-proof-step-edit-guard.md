---
kind: fixed
title: Step edits cannot leave new work beyond proof or on a landed job
pr: 1871
surface: [engine]
invalidates:
  - "Was true: a step edit could append pending work to a landed job, including one without a proof step, and that work could be approved without running. Is true now: every nonempty step edit on a landed job is refused without changing it."
  - "Was true: adding or enabling work after proof, or editing after proof started, could reuse a sheet that never saw that work. Is true now: shared step edits keep work before the earliest proof conversation and freeze after proof starts, including renamed until-proven and prove stages."
  - "Proof conversations cannot be skipped through step edits. A landed job no longer offers edit_steps. Empty edits remain no-ops; a nonempty request that names an already-off step is still refused after landing or proof starts."
---

A human approval gate may follow proof when added before proof starts. Refusals
keep the job, version, proof and lease unchanged. These bounds cover the shared
step-edit door; other historical stage mutation paths remain separate work.
