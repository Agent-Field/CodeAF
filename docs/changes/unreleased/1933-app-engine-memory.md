---
kind: changed
title: The desktop engine no longer holds every crew member in memory
pr: 1933
surface: [engine]
invalidates:
  - "A member of an open crew stayed held in memory for as long as its crew was open, and listing tasks, asks or processes opened every member. Now a member with nothing in flight is read from its files, held only for one read to build its task list, and let go after the idle time like any other conversation."
  - "A settled checkpoint used to be replaced on every open, preventing task-list cache admission for conversations with saved tasks. Byte-identical checkpoint writes are now skipped; historical memory measurements did not record cache coverage and need a fresh run."
  - "The journal record cache kept up to 32 records until the app quit. It now keeps at most 8 and drops one that nobody has read for 30 seconds."
---

`tasks.list`, `asks.list` and `processes.list` answer for a crew member at rest
without keeping its conversation. The task list is the same owner-backed list as
before; it is remembered while the member's files are unchanged, in memory and
under `<home>/cache/app-tasks` for the next launch (same build only). Plan reads,
the project task index and the run-row listing no longer repeat their reads for
each row, and the collector percent follows the live heap. No protocol field
changed. See docs/perf/memory-2026-10-24.md.

Idle release now shares task admission's gate and waits for every active reader.
It checks current background work rather than trusting a prior listing. Cache
entries require matching file identities and timestamps before and after their
read; files inside a two-second timestamp tick are not retained. Both cache
paths share read admission, disk decoding is size-bounded, and the 24 MiB memory
account includes retained Go structures and their referenced data. Index eviction
clears discarded references, and record expiration targets the earliest deadline.
The performance note now distinguishes the app's actual GC defaults from the
terminal's settings and identifies the missing historical environment record.

Reader completion is one-shot and deferred, including when a read panics. Owner
read errors propagate instead of certifying a disk-only task list. Opening a
settled task checkpoint preserves its file identity when its encoded bytes are
unchanged, so real conversations with saved tasks can populate both caches.
Record, rest and project-index caches share the same modification/change-time
settling guard. Disk task-list storage is capped at 96 MiB and 256 files, with
the oldest files evicted first. A first-use scan and minute-spaced idle sweeps
prune entries for missing or deleted agents; ordinary writes reuse the inventory.
Deleting an agent in the app also drops its memory and disk task-list entries.
