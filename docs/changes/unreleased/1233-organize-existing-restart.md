---
kind: fixed
title: Organize existing chats resumes the durable job after cancel or restart
pr: 1233
surface: [chat]
invalidates:
  - "EnqueueJob minted a new pending organize_existing row beside a cancelled, deferred, or failed twin, so quit/reopen left cancelled plus a second pending survey. The same coalesce key now resumes that durable row as pending."
---

Repeated clicks still coalesce while pending or leased. Completed work may start a
new row. No second scheduler.
