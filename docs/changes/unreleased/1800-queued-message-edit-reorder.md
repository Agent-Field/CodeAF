---
kind: added
title: Queued messages can be edited and reordered from the desktop
pr: 1800
surface: [engine, remote, docs]
invalidates:
  - "Queued messages could only be taken back one at a time by stream. The engine now also edits a queued message's words in place and moves it to another place in the queue, both refused with a clear 409 once its turn has started."
  - "The desktop hid a removed queued row and said 'Removed here only', because it had no way to take a queued message back from the engine. Remove is now real, and the queue order comes from the engine's snapshot."
---

A message queued behind a running turn can be edited, dragged to a new place
or moved with Alt+Up and Alt+Down. The edit and the move happen under the same
lock the queue's drain takes, so a message is changed before its turn starts or
refused after it, never half of each.
