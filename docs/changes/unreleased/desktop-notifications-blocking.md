---
kind: changed
title: Desktop notification clicks start Next up
surface: [desktop, chat]
invalidates:
  - "Notification delivery used a separate world schema and could lose the engine's blocking metadata. It now reads the conversation world store and posts only blocking questions and failures, once per item and grouped by place."
  - "Clicking a question notification only focused its tray. It now starts Next up at that item and advances as questions leave the feed. Answered notifications open only their conversation."
  - "The dock badge could miss questions because its native reader expected numeric counts. It now counts all waiting questions, including the current conversation, with a presence fallback for disk-only work."
---

Notification activation clears stale item identities as well as stale question targets.
