---
kind: changed
title: the status word says working while a task subtree still turns at rest
pr: 1191
surface: [chat]
invalidates:
  - "The status row's state word read `idle` whenever the conversation's own turn was over, even while a task it had handed out was still turning — so the row said `idle` under a tab strip that already drew the same conversation as `working`. It now reads `working` for a door at rest whose task subtree (or background job) is still running, the tab strip's own word, with no spinner and no clock — those belong to a turn, and the turn is over."
---

The figures on the row — the ledger, the meter, the job and watch counts — are
untouched and were already right. Only the word moved, and it moved at the render
site (`app.stateWord`), reading the surface's frame-safe `app.frontSignal` rather
than writing `app.state`, which stays the behavioural predicate the spinner, the
clock, ticking, barge-in and the background-work question all read.
