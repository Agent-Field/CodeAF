---
kind: fixed
title: Acting on a waiting job no longer fails with 'job moved on' when nothing changed
pr: 2463
surface: [engine, factory]
invalidates:
  - "Was true: brief-writing bookkeeping or an unchanged worker report could make an action on a waiting job stale. Now those writes alone do not invalidate job control decisions; other doors retain strict version checks."
---

Give back, try again, send back and stop controls no longer refuse with
"stale: job moved on while you looked" when only brief-writing status or the
Changed timestamp moved. Approval, publication and plan revision remain strict.
Changes to state, assignment, budget, steps, questions or results still require
a fresh read, including a newly kept brief or a late manager reply. Budget edits retain their existing
conflict rules. Document revisions and wire versions still advance as before.

The saved document adds action_revision beside budget_revision. A missing field,
including after an older engine rewrites the document, falls back to the full
revision and strict version checks.
