---
kind: fixed
title: Factory signals survive lost heartbeat replies
pr: 0
surface: [factory]
invalidates:
  - Building a heartbeat reply used to consume interactive signals before its worker received them; a current worker now acknowledges delivery on its next heartbeat.
  - A repeated heartbeat reply could replay a person's choice; the worker now remembers delivery IDs only while the factory can redeliver them and applies each once.
---

The lease desk retains signals in order until their delivery IDs are acknowledged.
Message and question IDs retain their original meanings. Recheck, publish and
merge stay queued for the pull that carries them out, never a live heartbeat. Stop and lease
release clear interactive signals, so they cannot leak into a later run.

Workers opt in through a heartbeat header, following the existing worker-header
compatibility pattern. An older worker still collects once without acknowledging;
an older factory ignores the new header and receives the same empty JSON body.
The older-worker hold fallback from #2330 remains, with its tests. A recheck
queued while the previous worker is finishing now survives that lease release
and starts once through the next pull; it never sets the publishing flag.

Failed effects remain pending and retry on the next heartbeat without releasing
the lease. Only saved job state can make an unsuccessful run-specific signal
moot; a missing mirror item does not. Replaced questions are also moot.
Acknowledgements are batched and retired after a successful heartbeat omits their
IDs, so a long run cannot accumulate oversized heartbeat headers or dedup history.
