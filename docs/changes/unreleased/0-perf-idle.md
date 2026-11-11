---
kind: fixed
title: Idle sessions share their request watch and skip redundant state writes
pr: 0
surface: [engine, app, chat]
invalidates:
  - Presence was rewritten on every state nudge and five-second beat; unchanged state now skips redundant writes between seven-second atomic JSON refreshes.
  - Each held session polled deletion and takeover requests every quarter second; one process-wide directory watcher now delivers requests with a five-second safety scan.
---

Unchanged state notifications no longer replace presence.json. One shared
seven-second clock atomically refreshes the JSON updatedAt stamp even when
nothing changed, so older reader builds keep seeing live claims. Two refresh
intervals fit inside the unchanged fifteen-second expiry, allowing one missed
refresh with one second of slack. Active work still checks its snapshot on the
five-second maintenance beat, and state changes are published immediately.

One shared directory watcher delivers deletion and takeover requests, with
subscribe-before-check and one five-second recovery pass. Requests are published
atomically so notifications cannot expose half-written requests. Agent-close
subscriptions replace the app's per-connection quarter-second timer.

While the app client is connected, crew and automation refreshes compare input
stamps before reading their stores. Crew inputs include live member state,
traffic, decisions, spending, settings and the day; failed holds retain their
retry schedule. Automation inputs include the database and WAL, including writes
through another connection. Identical serialized views emit no change event.
