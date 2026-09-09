---
kind: fixed
title: a turn the conversation starts on its own reaches the screen on the ordinary door
pr: 699
surface: [chat]
invalidates:
  - "A turn the conversation started on its own — a background job exiting, a task landing, a watch firing — reached the screen only over `--no-host`. On the ordinary door it ran, spent and journalled invisibly: the window sat at `idle` and the answer waited in the transcript for whoever opened the conversation next. It is drawn now, on both doors, within a second or two of reaching the journal. `internal/remote/wakelane.go` carried the turn correctly the whole time; the surface discarded it."
  - "The two lanes a connection carries — the turns another window started (`app.watchFollowing`) and who holds the keyboard (`app.watchDriving`) — used the TURN generation `a.gen`, and every comment about them said so. They carry `app.linkGen`, which moves only when a conversation leaves the front. `a.gen` moves once per turn, so the standing waits went stale on the person's first sentence, were discarded, and were never armed again."
  - "A window that had typed once stopped hearing that the keyboard had moved on another machine, so it kept drawing a composer it could no longer send with. The hand-over reaches it however many turns have gone by."
---

The surface end of the wake lane is what was broken, which is why every
`internal/remote` test and every follow-lane test here passed while the real door
did not: in a test nothing starts a turn before the arrival, and starting a turn
is the whole of the step.

`clearConversation` bumps `linkGen` beside every other lane's generation, so an
arrival from the conversation this window walked away from is still discarded and
still arms nothing — including when the conversation replacing it is local and
arms no connection lanes at all.

`internal/manual/chat/how-tasks-run.md` said this reply crosses the wire like any
other and now says it on the ordinary way of starting aforge too, naming a job
exiting and a watch firing beside a task landing.
