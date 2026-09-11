---
kind: fixed
title: a countdown on a question is let go when the conversation closes, like everything else it armed
pr: 963
surface: [engine]
invalidates:
  - "`Agent.Close` let go of a steer's armed grace (`stopSteerGraceLocked`) but not of a clock armed on a question, so a `recommend` rule with an hour on it left a live `time.Timer` and its goroutine behind a closed conversation for that hour. It calls `Agent.stopAskClocksLocked` beside it now. Nothing a person could see changes: `askClockRanOut` already refused to decide anything on a closed session, which is the guard, and this is the other half — the clock does not fire at all. A settled question kept so its answer could be changed is dropped by the same call, which is where the book's own close was already doing the work."
---

The line was named in #954's own report as owed rather than written, because
`internal/session/agent.go` was another lane's file in that wave and a one-line
reach into it would have been a merge conflict for no gain. It is written here on
its own, with the test that could only pin half of it before — the book clearing
what it kept — now pinning the whole of it through `Close`.
