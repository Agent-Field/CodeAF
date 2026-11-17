---
kind: changed
title: Crews are kept in teams.db, an SQLite store with no size cap, and teams.json is no longer read
pr: 2066
surface: [engine, chat, docs]
invalidates:
  - "Was true: teams lived in <profile>/teams.json, rewritten whole on every change, and the crews overview, union and organize reads refused a file over 2 MB with `the teams file is larger than 2097152 bytes`. Is true now: teams live in <profile>/teams.db (SQLite, WAL), a write changes only the rows that differ, and no read has a size cap."
  - "Was true: a spaces.json or teams.json from an earlier build was read and migrated. Is true now: there is no migration; the new store starts empty, and teams.json and spaces.json are never read, written, renamed or removed."
  - "Was true: a load carried every member's factory brief and brief revisions. Is true now: briefs are rows of their own that a load leaves out; teams.WithBriefs reads one member's when they are shown."
  - "Was true: teams.Stamp and the overview's revision were taken from the teams file's bytes or stat. Is true now: they are the store's id and its write counter, moved by every write that changed a row; the app's crew poll, the work graph's crew read and a session's team watch read it too, where they read teams.db's stat."
  - "Was true: crews.overview read the whole file and refused one that broke the tree's rules. Is true now: it pages one level in SQL by the store's index, and the tree's rules are kept by the store's tidy on every write."
---

`teams.ReadSnapshot` takes no byte limit, `appwire.OverviewMaxBytes`,
`teams.SnapshotMaxBytes`, `teams.ErrSnapshot*` and `teams.LegacyFileName` are
gone, and an unreadable store is `teams.ErrUnreadable`. A store a newer build
wrote is the separate `teams.ErrNewer`, and an I/O failure is neither.
`teams.SetAside` takes the teams lock and moves only a file that is not a
database or is another program's, WAL first; a newer build's store and a
damaged store of this one's own stay where they are. A foreign database is
inspected read-only before WAL is turned on, so it is left byte for byte.
Tests outside the package state a store with `internal/teams/teamstest`.
