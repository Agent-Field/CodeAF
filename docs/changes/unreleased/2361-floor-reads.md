---
kind: fixed
title: Idle factory polls read only job documents that changed
pr: 2361
surface: [engine]
invalidates:
  - "Idle lease pulls, settlement and runner capacity checks decoded every job document on each look. They now reuse unchanged documents after checking every file's identity, size and modification time."
---

Each List result owns its mutable data. Repository migration and output retention
still run on each look. Writes from another store and replacements with unchanged
size and modification time are visible on the next look.

Lease grants and execution gates still read the job under its lock. A Stop that
lands during selection permits only a thread-only lease for a pending reply,
without restarting job steps.
