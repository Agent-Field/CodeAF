---
kind: fixed
title: a task room draws its own machine and rate, because news now says which work it is about
pr: 806
surface: [chat, engine, remote]
invalidates:
  - "The two news desks in internal/tui3 were keyed by MODEL. They are keyed by SUBJECT now — phase.go's `newsDeskKey`, which falls back to the model when the news names no subject — so two tasks on one model no longer overwrite each other's clock. The lane RINGS are still keyed by model+lane: those measure a machine, and a subject in that key would split one machine's history."
  - "`provider.PhaseNews` and `session.LaneNews` carry a `Subject` field. Empty means the conversation, which is what every producer that predates the field sends and what an older wire peer sends, so absence behaves exactly as it always did. A node's is `session.NewsSubject(conversation, id)`, spelled `<conversation>#<node>` and minted in one exported place because both sides of the seam have to agree about it."
  - "`provider.WithNode` / `provider.NodeFrom` exist beside `WithSession` / `SessionFrom`. The session stamp says WHOSE errand a call is; the node stamp says WHAT IT IS ABOUT, and the two are different questions — every node of a family shares the root conversation's `newsKey()`."
  - "`tui3`'s `livePhase()` was the only reading of the phase desk and dropped every role but `lane.RoleTalk`, so a node's phase could not be drawn anywhere. There are three readings now: `livePhase()` unchanged for the conversation, `roomPhase()` for the open room's node (any visible role, because the subject already proves whose news it is), and `windowPhase()` choosing between them. It is not a guess about rooms being open — that would put two tasks on one model back on each other's row."
  - "Every rate on the status row was gated on `a.state != stateWorking` and `awaitingReply()`, the CONVERSATION's liveness, so a room showed no throughput for the whole of a task's run. The gate is `windowWorking()` now: the window's own work. A room's liveness is its subject's news being inside `phaseWindow`, which the held and streaming beats make a real answer."
  - "A task room's status line showed the node's model and nothing else about it — `roomModelWord`'s own doc said it wore no served rider. It wears the NODE's now (`app.roomLaneRider`), so the row reads `⠋ Ship the parser fix · task glm-5.2 · via friendli`, and the right edge is the node's rate or the node's phase words. It still wears no reasoning suffix: that dial is the conversation's."
  - "`remote`'s `PhaseWire` and `LaneWire` carry `subject`, and all four converters copy it. A peer that sends none means the conversation, so a surface talking to an older build behaves as it always did."
---

A news item now belongs to a SUBJECT, and a window draws its own subject's news.

The desks were keyed by an address rather than an identity, and the surface paid
for it three ways at once: two tasks on one model overwrote each other, a node's
phase could reach no row at all, and the rate was gated on the liveness of a
conversation that is idle for the whole of a task's run. Inside a task room, with
the node writing, a person read no provider, no tok/s, and a stale `running ask ·
4m 55s` left over from the conversation.
