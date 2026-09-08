---
kind: fixed
title: a run that has not landed carries no landing time, and the index answers with the file's last word
pr: 644
surface: [chat, engine]
invalidates:
  - "`newOrchestrateFamily` minted an adaptive run's opening row with `EndedAt: family.started`, so a run that had not ended carried its START instant in the field that means when it ended. That row leaves the field empty now. `family.started` is unchanged and still read for one thing only — the duration the CLOSING row carries — and `recordRoot` and `recordNode` go on stamping real landings."
  - "`ReadTaskIndex` collapsed a node's several rows by sorting on `EndedAt` and keeping the winner, which only ever worked because every row on disk had a clock. It collapses on the FILE's own order now, before the display sort: the index is append-only and written one row per write, so the later line about a node is the later thing that happened to it. `newestPerNode` is `lastPerNode`. Without this an undated live row would out-sort every landing — it files at the moment of the reading — and a finished run would have answered `running` out of the file forever, and been closed a second time as abandoned work on the next launch."
  - "The record card drew `landed <age> ago` over an adaptive run that was still going, home's work band drew an age hard against its right edge for one, the \"since you left\" summary counted its files as change since the person last looked, and `rollUp` moved a project's newest-landing moment to a run's START. All four read the ending field and all four are right the moment it is empty; none of them changed, and every one of them is now pinned by a test."
  - "#608's landing law listed `newOrchestrateFamily` in `liveLandingEndedAtWriters` and called it \"the odd one … out of its scope to move\". The entry is gone, so `TestOnlyALiveLandingStampsARowWithNow` now refuses that stamp rather than excusing it."
  - "`tasks.md` said work \"still claiming to be running has no clock\" on the record card, which was true of a task and false of a run that plans itself. It covers both now, and home's page says the work band's right edge is empty for anything that has not landed."
---

The ordering rules are untouched, and that is the point: `taskIndexAt` and
`tasksEntryAt` still file a live or undated row at the moment of the reading, so
a running run still sorts to the top of the tasks place and still falls inside
today's window. What changed is that the collapse stopped borrowing that
ordering to decide which of a node's rows is TRUE, which is a different question
and the one the file's own order already answered.
