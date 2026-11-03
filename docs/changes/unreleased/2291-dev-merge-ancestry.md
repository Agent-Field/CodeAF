---
kind: internal
title: Record dev ancestry in app/v3 with a true merge
pr: 2291
surface: [build]
invalidates: []
---

PR #2099 already integrated these dev features through a single-parent commit.
This true merge records the 49 dev commits in app/v3 ancestry while retaining
the published #2099 choices and later app/v3 work. Land this change as a merge
commit so future dev merges use the recorded ancestry.

A follow-up real merge brings five newer dev commits, including safer team
sharing, the pinned embedded-engine update and relay configuration fixes.
Team sharing retains app/v3's explicit opt-in default while adding pairing
gates, complete first-sharing backups, history visibility safeguards and
replacement recovery receipts adapted to SQLite.

The imported local team refresh test now seeds unrepaired SQLite rows and
verifies persisted repairs with a snapshot that performs no repair. Settings
save and turn completion tests explicitly enable sharing before checking
off-loop pairing adoption; missing or empty `teams.share` remains off.
