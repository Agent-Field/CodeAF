---
kind: fixed
title: the hand-over tests count what the model was told, not how long the queue is
pr: 1024
surface: [engine]
invalidates:
  - "`steeringQueue` was the way a test asked what a session has said to the model. It reads only what is STILL QUEUED, and a note that wakes a turn is drained out from under it on another goroutine — use `timesSaidToModel`, which counts a line across the queue and the transcript together, for anything enqueued with a wake."
---

`TestAnsweringLetAforgeDecideTwiceStandsRatherThanRefusing` reported that a press
enqueued MINUS TWO lines, which is a queue that shrank rather than a press that
sent anything. Handing a decision over is a wake note, so the press starts a turn
of its own; that turn drains the queue into the transcript and its end gives
every undecided node back to the person. Both are the product doing what it
says, and the test was subtracting two numbers a goroutine was moving underneath
it. It now counts how many times the hand-over line has been put in front of the
model — queued or already drained — and holds the woken turn open while the
second press lands, which is the scenario it was always written about.
