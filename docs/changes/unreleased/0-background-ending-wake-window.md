---
kind: fixed
title: nearby background process endings share one idle wake
pr: 0
surface: [engine, chat]
invalidates:
  - "Was true: every failed background process or explicitly requested process notification could start its own idle turn. Is true now: endings nobody is parked waiting on gather for two seconds from the first ending and start one turn with all notes in arrival order. An owed ending releases immediately and its turn absorbs the gathering notes. Endings during a turn keep the existing step-boundary behavior; clean unrequested exits remain quiet and requested deaths remain silent."
---

The notification queue owns the wake window, not the process callers. Each
ending keeps its own note and desktop event; the wire is unchanged. Starting
an ordinary turn or recording the notes cancels the timer, and a callback
already dispatched checks its window identity before acting. Pending notes
survive a declined timer: a completed vision fallback schedules their ordinary
answer, and closing after stopping work still saves their endings to the journal.
