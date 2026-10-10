---
kind: added
title: Desktop queued-message client can request immediate delivery
surface: [desktop, chat]
invalidates:
  - "The desktop queue client could only edit, reorder or remove queued messages. It now exposes sendQueuedNow and the conversation hook exposes sendQueueNow."
---

Immediate delivery uses the engine's queue-send action and reads the resulting
snapshot. A delivery race (409) removes the stale queued row and says exactly
"that message has already been sent", including when the refresh fails. The queue
menu remains a separate feature.
