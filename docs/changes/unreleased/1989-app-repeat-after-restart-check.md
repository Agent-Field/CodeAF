---
kind: fixed
title: Restart replay checks distinguish a read-only open from a first turn
pr: 1989
surface: [engine]
invalidates:
  - "The restart replay test treated any unfinished conversation open as a first turn; it now waits for a read-only open to settle and checks first-turn work separately."
---

Repeating a crew creation request after a restart still creates no agent and
sends no first message. Its real-journal regression test retains the boot check,
checks for started, running or queued turns, and verifies that the scripted model
received no new request after the restart. The wait for a read-only crew open is
bounded to five seconds.
