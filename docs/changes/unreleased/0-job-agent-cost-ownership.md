---
kind: fixed
title: codeaf counts every job-owned manager against its budget
pr: 0
surface: [engine]
invalidates:
  - "Only the manager currently linked from a job counted toward its cost and cap. Every durably owned job conversation now counts, including competing manager opens."
  - "Reopening a seeded conversation could repeat saved call receipts under its folder identity. Rebuild now recognizes its journal identity as well."
---

A job's extra manager conversations now charge the job and stop at its budget,
even before a repository checkout exists. Reopening saved conversations keeps
calls already recorded from being charged twice. Existing duplicate ledger rows
are left unchanged.
