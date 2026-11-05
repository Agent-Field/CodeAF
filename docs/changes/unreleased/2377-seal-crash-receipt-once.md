---
kind: fixed
title: A crash between saving a turn and clearing the call log books each tool call once
pr: 2377
surface: [engine, chat]
invalidates:
  - "A process that died after a turn was saved but before its finished calls left the local call log replayed them on the next start, so one tool completion could appear in two receipts. Recovery now checks saved receipts first."
---

If codeaf stops right after saving a turn, the next start recognises the calls that turn already saved and does not save them again.

New calls carry an optional unique ID from intent through completion to receipt, so identical parallel calls started in the same millisecond stay separate during replay and WAL cleanup. Older records without IDs retain their hash and start-time identity; existing collisions cannot be disambiguated retroactively.

If the saved turn log or a receipt cannot be read, recovery logs one warning and keeps the original WAL replay unchanged. This preserves startup but cannot deduplicate against unreadable receipts.

Recovery still reads all committed receipts. Call start and seal times use independent wall clocks, so a seal can have a timestamp before its call started. Commit order alone cannot prove that older receipts exclude pending calls; a safe bound needs a durable call-to-turn index.
