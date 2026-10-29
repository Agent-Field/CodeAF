---
kind: fixed
title: Closing a chat releases its sync hold before another window can open it
pr: 0
surface: [chat, engine]
invalidates:
  - "A closed conversation used to leave its sync drive alive until its process exited or finished saving. Its journal now releases that drive before letting another window open the conversation."
  - "Reopening a closed conversation in the same process used to reuse its sync drive and fence. It now starts a fresh drive and acquires a new fence."
---

Closing a chat, tab or terminal finishes its last save and releases its sync hold
while the journal is still locked. A later window's hold and send receipt survive
the earlier process quitting. A refused opener leaves the current owner's hold
alone, and workers and side conversations cannot release their parent's hold.
