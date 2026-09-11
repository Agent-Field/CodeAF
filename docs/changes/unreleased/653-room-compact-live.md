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

The live-work disclosure is separate from settled-phase keys, so opening it does
not unfold old phases. Caption/tool expansion tracks call identity through
journal refresh and coalescence. The page times its own live batch and never
borrows the parent conversation's response-wait or connection-loss state.
