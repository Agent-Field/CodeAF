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
failure happens before any memory row is deleted or rewritten, so a line's words,
its provenance and whether it was still active are all still in the file. There
is no repair command and no backup to restore — the next open of this build adds
the missing column, gives every row the owner its old scope proves, creates the
owner index, and reads the same lines back. A row the old store kept as a project
cannot prove which project it was, so it also gains the `legacy-project`
quarantine marker and needs a proven owner before a conversation sees it again;
nothing else about it is rewritten.

Recovery is a launch of the fixed build, and installing it does not by itself
change an engine already running. On the machine holding the workspace, updating
codeaf and reconnecting or opening a conversation there is the whole repair: a
newer build replaces an older engine as the window connects, busy or not (the
turn it catches stops where it is and keeps its partial reply), and the new
engine repairs the file as it opens. A window held by an engine on another
machine is not reached by that reconnect — update codeaf there, and when its work
is safe run `codeaf engine --stop --workspace <folder>` there first
(`codeaf engine --status` names the engine if the folder is unclear). A plain
launch with no engine holding it repairs the file at its next open. An idle
engine of the same build may restart to pick up a changed terminal environment; a
busy one keeps running and is only replaced when a newer build connects.
