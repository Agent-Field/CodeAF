---
kind: fixed
title: Removed computers keep quit progress quiet
pr: 2448
surface: [chat]
invalidates:
  - "A removed computer could claim it was saving to other devices while a hosted close reply was delayed. Quit progress now checks the current pairing's confirmed removal before each sentence."
---

A removed computer quits quietly; it no longer says it is saving to your other devices.
