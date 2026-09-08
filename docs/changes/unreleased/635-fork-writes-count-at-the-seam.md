---
kind: fixed
title: a fork's edits spend the same write allowance as the same edits made inline
pr: 635
surface: [chat, engine, docs]
invalidates:
  - "`fork` was the way around the write allowance. A reply that handed its edits to hands could change any number of files in the person's own checkout and was never moved to a watched task: the seam counts by walking the REPLY'S OWN batch, a hand's `edit` is in the hand's batch, and a `fork` call itself changes nothing — so `writeAllowanceFiles` and `writeAllowanceCalls` were both still zero when the turn ended, with three header files changed in a live tree and nothing to open (measured 2026-09-02 on deepseek/deepseek-v4-flash, 3 of 6 attempts). NOW: each of a hand's finished calls is read by the seam's own `workspaceWrites` — the same predicate that answers what an inline call changes under the workspace — and counted on the caller, one group per call, so a hand's `edit` and a hand's `sed -i` each spend the allowance exactly as they would inline, and a generated picture counts as nothing in both places. The hand REPORT'S file list is deliberately not what feeds it: that list is built from a wider table and would have counted the picture and missed the shell command."
  - "The account of a hand's writes did not have to outlive the turn. It does: a reply with a hand still out ENDS rather than carrying on (#95), the write meter is minted per turn and dropped with it, and a hand's report then wakes the session — so a count written into the running meter at the moment a hand came home would land in a dead turn's meter on the commonest path. NOW: what a hand lands waits on the session, and the seam drains it into whichever turn next reaches a step boundary — the one already running, or the one the hand's own report wakes — taken and emptied under one lock so every landed call is counted once and none falls between two turns."
  - "The manual described what counts as a write with no mention of hands, so the honest reading of it was that forking put an edit outside the count. `internal/manual/chat/tasks.md` now carries *Do hands get around the file limit*, which says a hand's changes spend the same allowance, that the count arrives when the hand reports back and may therefore be read by a later reply, and that a refused write and anything outside the folder count nothing."
---

The allowance was written from a measured run of forty-eight `sed -i` calls in
somebody's live checkout, and its whole claim is that the ground a write lands in
is what makes it the person's business. A hand writes in the caller's own
directory by construction — same folder, no worktree, no branch — so which
agent's ledger happens to hold the call is bookkeeping, and the writes were
falling between two of them and counted by neither.
