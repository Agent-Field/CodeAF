---
kind: internal
title: the task-family redesign is written down — what the code does, the refusals, what is open
pr: 245
surface: [docs, engine]
invalidates:
  - "There was no page that said what a task family is, why folder-ground parts silently lost their files, or which decision each lane under #228 settled. `docs/design/task-families/DESIGN.md` is that page, written against `dev` with the whole wave landed — #237, #239, #236, #240, #262 and #270."
---

Tracking root #228. The code is the wave; this is the memory of the decisions, so
a session does not re-argue the silent folder-ground loss, the soft overlap
advice, the dangling `commit-tree` freeze #246 tried, or the mirror baseline as
the landing's own record. It ends with what is still open — #255/#256, the
complexity ratchet #260, the depth ruling #261, and `restoreFromFolder`
re-mirroring at check time.
