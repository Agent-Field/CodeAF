---
kind: added
title: Remember in a filed desktop chat saves a place knowledge line with Undo
surface: [desktop, chat, engine]
invalidates:
  - "The remember tool only saved general memory. Filed desktop chats now save a line in their first parent-most direct place by default, with an actionable transcript note."
  - "Place Undo required the process-local receipt stack. Remembered lines now carry a targeted inverse across engine processes that preserves unrelated writes and refuses changed facts."
---

Explicit user, project and env scopes retain general memory. Unplaced chats
retain their existing behavior. Place lines record the source chat and time;
the saved note and its Undo token survive transcript replay.
