---
kind: added
title: app Inbox reads resolved receipts and agents reorder queued messages
pr: 1800
surface: [engine, remote, docs]
invalidates:
  - "The app could only list waiting Inbox items, so Done today lost their detail after resolution. GET /app/v1/inbox/{id} now retains the item with its answer and receipt for 24 hours in the current binary launch."
  - "The binary could edit or remove one pending agent message but could not reorder the queue. PUT /app/v1/agents/{id}/queue now atomically saves an exact permutation of the pending ids and publishes queue.changed."
---

Resolved snapshots do not decide source waiting state or admit another answer.
Undo cancels the pending decision; malformed and stale queue orders leave the
saved messages intact. The published wire contracts include both new routes.
