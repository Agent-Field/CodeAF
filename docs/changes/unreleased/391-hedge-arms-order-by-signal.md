---
kind: internal
title: a scripted lane stalls until a signal, so the hedge tests stop asserting luck
pr: 391
surface: [engine]
invalidates:
  - "The winner-B hedge tests failing under starvation read as a load flake in the hedge. It was the fixture: it ordered its two arms by elapsed time, and `hedge.go` was right in both outcomes."
  - "`lanestub.Profile` staged a mid-answer stall with `StallFor` alone. It now also takes `StallUntil <-chan struct{}`, which holds the arm until the channel closes or the request is cancelled and overrides `StallFor` when set."
---

`TestALaneThatStallsMidAnswerIsHedgedAndTheAnswerArrivesWhole` and
`TestOneCallMakesOneChoiceAndBothHalvesUseIt` gave the stalling arm a
200 ms timer and then asserted that the rescue took the answer. Forty
milliseconds of slack decided it, and a starved run ate the slack — but a
primary that comes back and finishes first KEEPING the answer is the law,
not the bug, which is what `TestAnAlmostFinishedAnswerIsNeverAbandoned`
demands. Both now hold the arm on a channel closed only at teardown.
`hedge.go` is untouched.
