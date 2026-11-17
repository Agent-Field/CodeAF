---
kind: fixed
title: Memory off no longer spends CPU marking job events, and Memory on does it by index
pr: 2333
surface: [engine]
invalidates:
  - "Every two-second knowledge pass ran a full scan of the events per job session, even with Memory off. It now runs only with Memory on, and seeks through the partial index profile_session_lookup, built once on the first pass with Memory on."
  - "A knowledge database write waited at most 1 s for another writer before it failed. It now waits up to 15 s, so a write during the index build succeeds."
---
