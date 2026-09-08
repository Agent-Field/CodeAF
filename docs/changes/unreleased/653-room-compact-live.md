---
kind: changed
title: Keep running task work compact and expandable
pr: 653
surface: [chat]
invalidates:
  - "A running task page always exposed its current tool and reasoning stream. It now uses the conversation's compact live step display, with the details available by click or ctrl+e."
---

Settled task phases retain their separate disclosures. Questions, failures, user
corrections, and answers remain visible. Returning to a task restores expanded live
work only while the same work is still current; it never opens an unrelated phase
or the finished task's whole-work chip.
