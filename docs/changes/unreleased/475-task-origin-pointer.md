---
kind: added
title: a task brief names the journal path and line of the person's original words
pr: 475
surface: [engine, chat]
invalidates:
  - "The brief was the worker's only view of the person's ask, and a long ask was clipped at 6000 bytes with no recourse. The brief now names the session journal's real filesystem path and the line where the person's turn began — `grep or read /path/to/session.jsonl, line 12` — so the worker can read the original words itself. The brief remains the contract; the pointer is an address, not inherited context."
---

A restatement is bounded on purpose. The worker already has `read` and `grep`, so
an address is enough: the conversation journal and the line the turn opened on.
A nested task and every part of a division inherit that same pointer, never their
own journal. A standing firing has a journal and no person turn, so it carries
an empty origin and draws nothing.
