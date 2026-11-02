---
kind: internal
title: Reuse unchanged engine plan and task-index reads and expire idle journal records
pr: 1936
surface: [engine, docs]
invalidates:
  - Unchanged plan reads previously reloaded the whole plan; matching database stamps now reuse it while committed writes still refresh the next read.
  - Journal caches previously kept up to 32 records without idle expiry; they now keep at most eight and expire unleased records after 30 seconds without a read.
---

Conversation task listings share plan handles across run and plan projections,
read each trajectory file once per listing, and avoid copying the run history
per plan task. Project task indexes reuse decoded rows for up to
four files of at most 4 MiB each, with replacement-sensitive stamps and a shared
mtime/ctime settling guard. Concurrent misses decode independently; per-path
singleflight was removed to avoid stranded waiters after decoder panics and
stale results shared across unsettled same-stamp rewrites. Returned rows own
their nested files, crew and verdict evidence, and their slice capacity equals
their length so hidden rows cannot expose cached fields after filtering or limiting.
Eviction clears retained rows; journal expiry targets
the earliest unleased deadline. See `docs/perf/engine-memory-safe.md` for gates.

Only the low-risk engine subset of the original memory work ships here. Idle
member release and the app task-list disk cache are follow-up work. Member
ownership, reader refcounts, persistence writes and GC policy retain their
base behavior.
