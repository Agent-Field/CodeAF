---
kind: fixed
title: a job has one manager, and all its spend counts against its budget
pr: 2283
surface: [engine]
invalidates:
  - "Asks that raced when a job started could each make a manager, and each joined the job's team. Now one is made and every ask gets it."
  - "Only the manager currently linked from a job counted toward its cost and cap. Every durably owned job conversation now counts, including competing manager opens."
  - "Reopening a seeded conversation could repeat saved call receipts under its folder identity. Rebuild now recognizes its journal identity as well."
---

Starting a job and opening its chat at the same moment now make one manager.
Extra managers left by earlier builds charge the job and stop at its budget,
even before a repository checkout exists. Reopening saved conversations keeps
calls already recorded from being charged twice. Existing duplicate ledger rows
are left unchanged.
