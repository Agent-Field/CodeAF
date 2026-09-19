---
kind: fixed
title: every run a conversation has made stays readable
pr: 1234
surface: [chat, engine]
invalidates:
  - "A second hand-off in one conversation renamed the first run's store to `plandb.db.1` and every read opened only the live store, so the first run's row stayed on the rail while its page, its parts and its notes could no longer be opened. Every read now takes the ended stores oldest first and then the live one, and a task of any run opens by the id the rail shows."
  - "An ended run is read, never steered. A note, pause, resume, cancel, amend or priority on one of its tasks answers `that task's run has ended` and changes nothing on disk. Only the run underway takes those."
---

Seen on the real binary on 2026-09-19: after a second hand-off, the first
run's row opened nothing. Driven hosted after this change, with two runs one
after the other: a click on the first run's row opened its page with its
brief, its notes and its steps, and a note typed there was answered `that
task's run has ended` with the words left in the box.

An ended store never changes, so it is opened once and kept for the life of
the conversation, and given back when the conversation closes. The live store
is still opened for each read, so another process's writes are seen.
