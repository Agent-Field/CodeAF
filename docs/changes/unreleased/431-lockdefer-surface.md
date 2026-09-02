---
kind: internal
title: The surface, the executor workspace and the resident hold their mutexes from a defer
pr: 431
surface: [chat, engine, resident]
invalidates:
  - "Snapshot-then-release critical sections in cmd/aforge, the executor workspace and the resident held their mutex without a defer, so a panic absorbed by guard.Go could wedge them forever. Each is a small method holding the lock from a defer now; the provider half follows in part 2, and until it lands `TestEveryLockInTheGuardedTreeUnlocksFromADefer` stays on the known-red ledger."
---

Nineteen sites, every one the snapshot-then-release or test-and-set-then-release
idiom, and in several a bare defer would have deadlocked because the tail re-enters
the same mutex — the host caches unlock and then start a fetch that locks again. So
each became a method that holds the lock for its whole body and answers what the
caller needs, with the tail call left outside. Nothing else in those files moved.
