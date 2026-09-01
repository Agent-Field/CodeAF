---
kind: fixed
title: a park is numbered, so a park-shaped test waits past the park it already saw
pr: 202
surface: [engine]
invalidates:
  - "`waitParked` in `internal/session/task_park_test.go` took `(t, node)` and waited for `node.parked` alone. It takes `(t, node, past uint64)`, waits for a park LATER than `past`, and answers that park's generation for the next wait to name; every caller in the file passes one."
  - "`TaskNode.parked` was the whole of what a waiter could know about a park. There is now `TaskNode.parkGen` beside it — bumped inside the same hold of the graph's lock that sets the flag — and `TaskNode.parkStanding()`, which answers both in one reading; `waitingOnItsPieces` is written through it."
  - "#200 recorded `TestAParentIsAskedNothingUntilEveryPartHasReported` as flaking 2 runs in 40 under `-race` with no cure. It is fixed: 4 failures in 40 measured on `dev`, none in 40 here, and none in a `-race -count=20` pass over all eleven tests in the file."
---

A parent woken by one part's report unparks, reads it, finds another part still
outstanding and parks again, and `parked` is true on both sides of that window —
so a waiter that asked only "is it parked" could return on the park the parent
had NOT yet woken from, and the assertion one line later landed in the unpark →
re-park gap and read the wrong park. The cure is the generation and not a longer
poll: a tolerant wait would pass on a quiet machine while hiding a parent that
had genuinely stopped re-parking, which is the regression these tests exist to
catch.
