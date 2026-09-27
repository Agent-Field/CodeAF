---
kind: fixed
title: Opening a task room gives its note box the keyboard
pr: 1556
surface: [chat, docs]
invalidates:
  - "Opening a task with alt+t then Enter used to leave the roster holding the keyboard. A note appeared in the task's box but Enter reopened the row instead of sending it. Opening the room now releases roster focus, including the narrow overlay, so Enter sends the note to that task. Alt+t still returns to roster navigation."
---
