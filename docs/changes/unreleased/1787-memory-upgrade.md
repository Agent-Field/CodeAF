---
kind: fixed
title: a store written before memory owners existed opens again on the next launch
pr: 1787
surface: [chat, engine]
invalidates:
  - "The memories schema ordered the owner index before the owner column existed, so opening any database written by an earlier release died with `initialize memories schema: no such column: owner` and the launch carried on with no brain. The index is now created beside the column by the owner migration, on a legacy store and a brand new one alike."
  - "The owner migration returned early the moment the column was present, so an upgrade interrupted between the column and its index would have been left without the index forever. It now ensures the index on every open, whichever shape the store is in."
  - "A memory written by an older build that is still running against an upgraded store landed with an empty owner, so no owner-filtered read — the router's shortlist, search, a forget match — could ever reach it. Every open now owns empty rows from their scope again (or quarantines them), and a row that already has an owner is never moved."
---

A database that failed to open under the broken build keeps its notes: the
failure happens before any memory row is deleted or rewritten, so the lines, their
provenance, status and scope are all still in the file. There is no repair command
and no backup to restore — the next open of this build adds the missing column,
owns every row the way its old scope proves, creates the owner index, and reads
the same lines back.

Recovery is a restart on the fixed build, and for a conversation held by a hosted
engine that means stopping the engine rather than just reopening a window: update
codeaf on the machine holding the workspace, and when its work is safe run
`codeaf engine --stop --workspace <folder>` there (or `codeaf engine --stop-all`
if the folder is unknown), then reconnect, which builds a fresh conversation on
this build and repairs the file as it opens. A plain launch with no engine
holding it repairs the file at its next open. Nothing restarts a running engine
on its own, and an engine of the same build is never displaced by another: the
stop is the step that always works, and it is the only one this note promises.
