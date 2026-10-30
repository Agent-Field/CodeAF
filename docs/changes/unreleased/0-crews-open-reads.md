---
kind: changed
title: A factory job's crew closes when the job ends, and crews read only what they need
pr: 0
surface: [engine, chat, docs]
invalidates:
  - "Was true: a factory job's room stayed open after the job ended, so every factory job added an open crew for good. Is true now: when a job is over (an end recorded on it, a dismissal, or its pull request merged or closed; an approved job still publishing is not) its room closes quietly, with no report, no traffic line and no change to who reports to whom, and is marked job_ended; the job coming back (a person's door or by itself, such as a pull request reopened) reopens a room so marked, never one a person closed or archived; and a sweep that runs once per profile closes the rooms of jobs already over, leaving any a person reopened."
  - "Was true: crews.overview answered a crews store it could not read with the code engine. Is true now: it answers with the new additive code unreadable (a store a newer build wrote, or a file that is not the store), and the app says so without offering a retry."
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
