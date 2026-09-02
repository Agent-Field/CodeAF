---
kind: changed
title: a task page reads a correction as a correction — steers are elbows and the engine reads the record
pr: 351
surface: [chat, engine]
invalidates:
  - "A line steered into a task drew as an ordinary new message and opened a turn. It draws as a `└ ` elbow where it was said and opens none, so a task page's turn count is lower than it used to be."
  - "A replayed task page showed yesterday's corrections as fresh questions with a `›`. They come back as elbows. Replayed pages therefore read differently than they did, and that is the fix, not a regression: the old replay was the lie."
  - "The room said nothing when it took a line into a working node, and wrote its own dim `it was waiting on its pieces` row when the node was parked. Every task steer now carries a short fading clause on the elbow itself — `· delivered`, or the waiting sentence — and both are `internal/session`'s words, not the surface's."
  - "`internal/tui3`'s room parsed a node's session file itself. It never parses JSONL again: `session.ReadTranscript` and `session.ReadTranscriptBytes` are the door, and `replayBlocks` is the only entry-shaping path for both the conversation and a task page."
  - "A line delivered into a node was journaled as a plain user message. It now carries a steer mark — the instant it was sent, delivered, and the engine's landing sentence — so a reopened page can tell a correction from a brief."
  - "`readRoomJournal`, `readRoomJournalTail`, `readRoomJournalBytes` and `shapeRoomJournal` were `internal/tui3`'s functions. They are gone, together with the `journalArgsLimit`/`journalOutputLimit` caps hand-copied from `internal/session`."
---

Lane L3 of #252. The task page and the conversation were meant to be one transcript
grammar pointed at two speakers, and the page had drifted into a second reading of the
record with a shorter idea of what a record can say — so the one act a person goes to a
task to perform, correcting work already running, was the act it drew as something else.

Two moves. The reading of a session file belongs to the engine now: one door, no lock, no
repair, answering the display shape a live conversation is already drawn from — so a mark
the engine writes cannot be a mark a surface fails to know about. And a correction typed
into a task is drawn with the mark this surface already draws a correction with, plus the
one thing a crossing to another agent owes the person: a short receipt that the words
arrived, on the same fade the conversation's own landing clause takes.
