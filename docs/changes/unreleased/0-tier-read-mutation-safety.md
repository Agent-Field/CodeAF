---
kind: fixed
title: Keep author access lookup failures out of job mutations
pr: 0
surface: [factory, docs]
invalidates:
  - "Transient author access failures could affect issue reconciliation and nested reads could deadlock a job update."
---
Author access failures now appear only on returned read views, never inside
job mutation callbacks. Reopened issues can return to Backlog without a
manufactured failure changing reconciliation decisions.

Consent reads during a locked update use the saved snapshot without waiting
for the same item lock or starting author lookup. Final-review retry can finish.
Output retention runs once per read, preserving the idle-pull read bound.
Jobs without an author or repository never start lookup or show its failure.
