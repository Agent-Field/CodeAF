---
kind: fixed
title: Explicit engine stop prevents deferred group commits from restarting its daemon
pr: 2394
surface: [engine, build]
invalidates:
  - "Previously, a deferred group commit could restart a daemon after StopSpawned returned. Now, explicit stop fences durability retries until a new owner deliberately reuses the engine."
---
Both explicit stop APIs fence late sync requests without fallback. StopSpawned
also joins daemon starts before taking its child list. Failed flushes retain
pending local records and completed WAL calls for retry. Recorders created before
stop cannot restart pending captures, even after a new recorder resumes saving.
A newly opened recorder can restart the engine and finish saving them. Requests
waiting for another stop still honor their context deadlines.

This fixes explicit owner/test teardown. Product quit does not call StopSpawned:
desktop helpers still expire after one idle minute, and no shutdown request is
sent merely because the app quits. Normal chat close keeps its bounded recorder
drain and reports an unfinished save.
