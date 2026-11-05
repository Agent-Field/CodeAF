---
kind: fixed
title: Explicit engine stop prevents deferred group commits from restarting its daemon
pr: 0
surface: [engine, build]
invalidates:
  - "Previously, a deferred group commit could restart a daemon after StopSpawned returned. Now, explicit stop fences durability retries until a new foreground request deliberately reuses the engine."
---
StopSpawned and Daemon.Stop join daemon starts before taking their owned children and rejects late
sync requests without falling back to another writer. Failed flushes retain
pending local records and completed WAL calls for retry; a fresh capture can
restart the engine and finish saving them.

This fixes explicit owner/test teardown. Product quit does not call StopSpawned:
desktop helpers still expire after one idle minute, and no shutdown request is
sent merely because the app quits. Normal chat close keeps its bounded recorder
drain and reports an unfinished save.
