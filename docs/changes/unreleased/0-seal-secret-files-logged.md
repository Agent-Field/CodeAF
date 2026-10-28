---
kind: changed
title: A file kept out of the saved history for a secret is logged, not said in the chat
pr: 0
surface: [engine, chat]
invalidates:
  - "Was true: every file the seal guard newly withheld for a secret was one notice in the chat, so a fresh checkout of a repository whose tests carry fake keys printed a dozen lines. Is true now: those lines go to the engine's log (cellstore.Guard.Kept, nil logs), and the cell's .furrowpolicy still lists every withheld path."
---

Withholding is unchanged: the file stays out of the saved history and travels
through the key vault only to paired computers. A file this machine cannot read,
and a vault that cannot be written, are still said in the chat, because the
person can fix them.
