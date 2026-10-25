---
kind: internal
title: Reuse unchanged engine plan and task-index reads and expire idle journal records
pr: 1933
surface: [engine, docs]
invalidates:
  - Unchanged plan reads previously reloaded the whole plan; matching database stamps now reuse it while committed writes still refresh the next read.
  - Journal caches previously kept up to 32 records without idle expiry; they now keep at most eight and expire unleased records after 30 seconds without a read.
---

Conversation task listings share plan handles across run rows and avoid copying
the run history per plan task. Project task indexes reuse decoded rows for up to
four files of at most 4 MiB each, with replacement-sensitive stamps and a shared
mtime/ctime settling guard. Eviction clears retained rows; journal expiry targets
the earliest unleased deadline. See `docs/perf/engine-memory-safe.md` for gates.

Only the low-risk engine subset of the original memory work ships here. Idle
member release and the app task-list disk cache are follow-up work. Member
ownership, reader refcounts, persistence writes and GC policy retain their
base behavior.
