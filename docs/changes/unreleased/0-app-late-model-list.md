---
kind: fixed
title: Announce late model lists to the desktop app
pr: 0
surface: [engine]
invalidates:
  - "A model catalog or provider list finishing after the app's three-second models.list wait had no completion event, leaving the desktop on its partial answer. The app now queues models.changed with empty data once per completion through one lifecycle-owned service subscription and an initial catalog waiter. A full writer queue does not hold the backend lock. Listing keeps its existing bound, and close removes the subscription and cancels and joins the waiter and wire sender."
---
