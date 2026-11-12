---
kind: fixed
title: branches.list pages every recorded task branch within a bounded request
pr: 0
surface: [engine]
invalidates:
  - "branches.list read only the first 200 task branches a conversation's records named, silently, and asked git four uncancellable questions of each. It now walks every recorded name by cursor ({agent, cursor, limit}; 50 by default, 100 at most) within a 3 s page, answering head, recorded, next and incomplete."
  - "A branches.list whose git timed out answered repo:false, and unreadable task records answered no branches. A timeout is now busy, unreadable records engine, oversized ones too_large; repo:false is only a known absence of a HEAD commit (no folder, git's exact not-a-repository diagnostic under LC_ALL=C, no work tree, or an unborn branch); any other git exit, safe.directory refusal, unreadable folder or HEAD naming nothing is engine."
  - "Any number of branches.list calls could run at once. At most two run in the engine process; a third is busy immediately, with nothing queued."
  - "A branches.list cursor did not exist. Next is tied to the recorded list and the HEAD commit it was issued for, and is refused bad_params once either changes, never re-aimed."
---

Rows are only names the conversation's own task records carry, with
tombstoned tasks and their descendants left out (found in one linear pass, not
the store's fixed-point loop); a `task/*` ref somebody else made is never
listed. Counts come from a streamed `git diff --numstat -z`: absent when git
could not give them (never zero), lower bounds when `counts_capped`. Long
titles are cut at 512 bytes with `title_truncated`. Decoding the task
checkpoint is bounded by size (32 MiB), not interrupted by the clock; the HEAD
check, reading the records and every git call are.
