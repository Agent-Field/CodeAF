---
kind: fixed
title: A crash between saving a turn and clearing the call log books each tool call once
pr: 2377
surface: [engine, chat]
invalidates:
  - "A process that died after a turn was saved but before its finished calls left the local call log replayed them on the next start, so one tool completion could appear in two receipts. Recovery now checks saved receipts first."
---

If codeaf stops right after saving a turn, the next start recognises the calls that turn already saved and does not save them again.
