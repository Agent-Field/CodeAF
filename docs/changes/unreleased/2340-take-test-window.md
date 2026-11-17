---
kind: internal
title: Take tests judge rows only after the take starts and wait for the backend to close
pr: 2340
surface: [app]
invalidates:
  - "appNoEndedTake judged every row after the request was sent, including the chat feed's correct row from before the take began."
  - "A test backend's cleanup Close meant the backend had stopped; closing the wire may already have started Close, and a second Close returns at once."
---

Test-only. Product behaviour is unchanged.
