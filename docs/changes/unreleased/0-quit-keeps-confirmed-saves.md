---
kind: fixed
title: Quit preserves confirmed saves after their engine owner stops
pr: 0
surface: [chat, engine]
invalidates:
  - "Process exit previously retried even already durable engine routes; it now retries only outstanding engine debt."
---

Quitting a hosted chat no longer reports a failed local save because an earlier,
already saved chat's engine owner stopped. The final process flush checks only
outstanding engine debt and still flushes pending local records. Each chat's
close, upload and ownership boundaries continue to force their own sync.

Explicit stops still refuse stale captures and queued syncs without fallback.
Unconfirmed records and completed calls remain pending for WAL replay by a fresh
recorder; no saving deadline or budget changes.
