---
kind: fixed
title: Factory holds survive lost heartbeat replies and finishing drives
pr: 2330
surface: [factory]
invalidates:
  - A hold used to be forgotten when its heartbeat reply was lost; live heartbeats now repeat the factory's current hold until the worker releases the lease.
  - A cancelled drive could end before its pause log was saved; the pause is now written before the control lets the drive end.
---

Resume supersedes an older uncollected hold and still takes a fresh worker slot
through the queue. The heartbeat uses the existing hold signal so older workers
remain compatible. Holding an already held runner stays idempotent.

Concurrent holds and spending answers complete without a lock inversion. Pause
writes finish before cancellation or admission of another paid round; a failed
write leaves the live control unchanged.
