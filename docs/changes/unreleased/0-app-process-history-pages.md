---
kind: added
title: codeaf app pages a conversation's whole process history and reads output by byte offset
pr: 0
surface: [engine]
invalidates:
  - "Was true: a conversation's processes were one JSON array, processes.json, rewritten whole on every job update and read whole by every reader; processes.list had no bound. Is true now: each job is one small record under the conversation's processes/ folder, written alone, and processes.page reads at most 100 rows, 4 MiB and 2 s per call with a cursor. processes.json is still read and folded in by the engine that holds the conversation, and is no longer written."
  - "Was true: processes a task worker started were kept in a tasks/processes.json that nothing read, so a conversation's process list never showed them. Is true now: they are in the conversation's history, with started_by {kind: task, task} taken from the worker's own construction."
  - "Was true: a saved running process with no live job was listed as stopped, with an ending computed from its last elapsed snapshot. Is true now: processes.page says unknown (not_held or owner_lost) with no exit code and no ending; processes.list keeps stopped but no longer invents ended_at."
  - "Was true: processes.list answered every process however many there were. Is true now: a history over 1000 records, holding unreadable records or not read whole within 2 s answers too_large; processes.page reads the rest."
  - "Was true: the only output read was processes.output by line number, which reads from the start of the file to reach a line and follows links. Is true now: processes.log reads by byte offset within 512 KiB from the path the owner's record names, without following links."
---
