---
kind: changed
title: A factory job's crew closes when the job ends, and crews read only what they need
pr: 0
surface: [engine, chat, docs]
invalidates:
  - "Was true: a factory job's room stayed open after the job ended, so every factory job added an open crew for good. Is true now: when a write takes a job to its end (factory.JobState done) its room closes quietly, with no report and no traffic line; only a person bringing the job back (a door that clears DoneHow) reopens it, and a one-time sweep closes the rooms of jobs that had already ended."
  - "Was true: the app's crews watch, its 2 s poll and most crew lookups read every crew, open or closed, so they slowed as crews piled up (a tick took 33 s at 10,000 crews). Is true now: the watch follows the open crews, and lookups read by id, lineage, subtree or a conversation's own crews through new store indexes; at 10,000 crews a tick, the poll, an agent's seat, the overview's first page and a one-crew write cost what they cost at 15."
  - "Was true: crews.list with closed set, and crews.archive, listed every closed crew. Is true now: they list the person's closed crews; a finished factory job's room is read by id where its job or a tab names it (crews.get)."
---

New store reads: `LoadOpen`, `LoadTeams`, `LoadLineage`, `LoadSubtree`,
`LoadRoot`, `LoadAround`, `LoadTeamsOf`, `LoadFoundedBy`,
`LoadConversationView`, `LoadClosedCrews`, `ConversationReferenced`,
`InspectConversation` and `AnyManager`. `UpdateOpen` writes over the open
crews only; reopening, archiving, deleting and the terminal's whole-list edits
keep `Update`. The session package names member conversations for the index
(`teams.SetConversationName`). `factorySweepEndedRooms` is marked for removal.
