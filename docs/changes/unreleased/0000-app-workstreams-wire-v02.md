---
kind: added
title: Workstream drafts and pages carry sources and kept proposals grant go-aheads
pr: 0
surface: [app]
invalidates:
  - The agent's go-ahead proposals used to be decision records with a text prefix and are now goahead records.
  - record.list without a kind used to return every record and now leaves goahead records out unless asked.
  - collections.db files from earlier workstreams builds are now refused because schema 3 changed in place.
---

The wire contract adds draft size, budget and source, source attribution on page
creation and saves, and agent and origin conversation IDs on workstream rows.
Draft fields round-trip through plans; job items refuse them. Page history keeps
the source of each version, and a save without one clears the current source.

Go-ahead records carry structured proposals. Keeping one grants exactly once in
the same transaction after checking the current target, end and workstream state.
Dropping it revokes its active grant; keeping it again grants a new one. Go-ahead
records cannot be superseded. Marking done ends grants without changing records.

The agent uses these structured proposals, with end dates at most 89 UTC days
ahead, and stamps added draft jobs with a validated source. Agent reads include
go-ahead records; ordinary record lists include them only when requested.
