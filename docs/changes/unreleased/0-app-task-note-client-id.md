---
kind: added
title: codeaf app keeps a task note's client id on the page and the event
pr: 0
surface: [engine]
invalidates:
  - "Was true: `tasks.note` took a `client_id` that only the returned entry echoed; `tasks.get` notes had no `client_id` and were named `<task>.n<position>`, and `note.changed` named the process-local `tnN`, so a surface could pair its sent note with the page only by words. Is true now: for a graph task the engine stores the opaque, bounded (`MaxClientIDBytes`, 256 bytes) `client_id` with the note in its persisted record, so it survives a restart, and `tasks.get` notes and `note.changed` carry it. Absent still means unknown: plan-store notes, the task's own lines, handed-over program notes and records from before this change carry none, and nothing is matched by text or stamped by position. `tnN` ids remain process-local."
  - "Was true: a graph task note's `client_id` was written to the app's own sidecar in a second write after the note was delivered, so a delivered note could come back as an error, and a running quick task's note went through an unkeyed fallback. Is true now: the key is written onto the task's direction in the same checkpoint write that admits the words, before any worker is handed them; a disk that refuses that write refuses the note with nothing delivered. A running quick task takes a keyed `note` through `AppTaskAction` too, and a settled one is still refused. Two notes under one key are two notes: the key is never a retry identity. A `brief` whose page brief cannot be kept after delivery answers `ErrBriefNotKept`, which is not a refusal. Sidecar keys written by earlier builds still read back where the direction has none."
---
