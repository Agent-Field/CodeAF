---
kind: added
title: codeaf app can rewind, delete, record and ask the manual
pr: 0
surface: [engine, chat]
invalidates:
  - "The slash catalogue had no typed conversation verbs for rewind, delete, debug or manual. agents.rewind.points, agents.rewind, agents.rewind.undo, agents.delete, agents.debug and agents.manual now call shared terminal behavior through the conversation's owning engine."
  - "A rewind could only cut history. One kept rewind can now be undone before newer work depends on the cut; the old append-only record remains available."
---

Conversation methods advertise their own hello features and forward to the owning
engine over a host connection. Delete retains the terminal's manager protection;
manual is a normal model turn with the same instruction and visible slash line.
Debug keeps the record for the conversation's subsequent turns and names its folder.
