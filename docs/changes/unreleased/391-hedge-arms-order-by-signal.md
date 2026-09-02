---
kind: internal
title: a scripted lane stalls until a signal, so the hedge tests stop asserting luck
pr: 391
surface: [engine]
invalidates:
  - "The winner-B hedge tests failing under starvation read as a load flake in the hedge. It was the fixture: it ordered its two arms by elapsed time, and `hedge.go` was right in both outcomes."
  - "`lanestub.Profile` staged a mid-answer stall with `StallFor` alone. It now also takes `StallUntil <-chan struct{}`, which holds the arm until the channel closes or the request is cancelled and overrides `StallFor` when set."
  - "Four lane tests bounded an outcome with a wall clock — `took < 300ms`, `took < 500ms` twice, and a two-second ceiling on a hundredfold-scaled first-token reading. None of them are there now; each says the ordering or the relationship it was really about, which holds at any speed."
  - "`TestARefusedRescueWalksToTheNextLaneRatherThanRelaxingTheRequest` timed its stalled first rung with a 900 ms timer. It is held on a signal now, so the walk reaching C is the rung order and not the arithmetic."
---

`TestALaneThatStallsMidAnswerIsHedgedAndTheAnswerArrivesWhole` and
`TestOneCallMakesOneChoiceAndBothHalvesUseIt` gave the stalling arm a
200 ms timer and then asserted that the rescue took the answer. Forty
milliseconds of slack decided it, and a starved run ate the slack — but a
primary that comes back and finishes first KEEPING the answer is the law,
not the bug, which is what `TestAnAlmostFinishedAnswerIsNeverAbandoned`
demands. Both now hold the arm on a channel closed only at teardown.
`hedge.go` is untouched.

The same reading swept the siblings, and every one of them had a claim
that did not need a clock to make. The late-first-token rescue proves
itself by naming nobody as the loser — a lane names itself on every chunk
it writes, so an A that had got a word out would be sitting there. The
cold-store ceiling proves itself with the reason word and B's answer. The
keepalive test's beats come in two hundred milliseconds apart under a
three hundred millisecond ceiling, so a build that reset on a keepalive
would never hedge here at all, and that it hedged is the claim. And
`internal/lane`'s instrument test read a 7.68 ms sleep multiplied by a
hundred against a fixed two-second ceiling, where a fifteen-millisecond
late wake was a failure; it keeps its floors and asserts the relationship
it is for — a quick first token timed apart from a slow one.

Found on the way and NOT fixed here, because it is a different fault:
`TestARefusedRescueWalksToTheNextLaneRatherThanRelaxingTheRequest` only
passes as the first run of its model in a process. `internal/provider`'s
package-level `sharedVelocity` carries what it learned about the refusing
lane across runs, so at `-count=2` the walk skips B and goes straight to
C. It is repeat-unsafe at `dev` too, and it wants its own issue.
