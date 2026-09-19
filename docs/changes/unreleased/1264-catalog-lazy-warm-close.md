---
kind: fixed
title: closing a process cancels and joins lazy catalog warming before a late cache write
pr: 1264
surface: [engine]
invalidates:
  - "Lazy catalog warming could outlive process close and write its cache afterward, including beneath a home selected by a later launch. Close now cancels and joins the warm, and a fetch that succeeds after cancellation does not write the cache."
  - "A later process launch still warms its catalog normally."
---

# Lazy catalog warming stops cleanly at close

A process close now cancels and joins its in-flight lazy catalog warm. If a fetch succeeds after cancellation, catalog loading checks the canceled context before writing the cache, so no cache write can occur after close returns. A later process launch can still warm its catalog normally.
