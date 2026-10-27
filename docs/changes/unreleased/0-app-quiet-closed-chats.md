---
kind: changed
title: Quiet desktop chats use a bounded one-minute recovery sweep
pr: 0
surface: [engine, docs]
invalidates:
  - "The desktop backend checked every closed chat's work files every two seconds. It now checks hot chats every two seconds and sweeps other closed chats in bounded batches within a minute, without adding per-chat file watches."
---

Startup recovery still checks every eligible candidate. Live factory stages,
kept or starting services, existing crew holds, due retries and chats seen live
within the last 10 minutes keep prompt checks. Crew traffic and team events,
including watcher errors, retain their existing immediate reconciliation.

Other closed chats admit changed work on their next sweep visit, within a
minute (plus one tick at the boundary), including after missed notifications or
missing/re-created directories. Profile and crew traffic stamps are shared
within each admission pass. The watched directories remain the shared profile,
teams and per-team traffic directories, so native watch resources grow with
teams rather than saved chat history.
