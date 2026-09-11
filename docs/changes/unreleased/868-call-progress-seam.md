---
kind: added
title: Every call can be watched — one seam for a call's whole life, fed from the readings it already has
pr: 868
surface: [engine]
invalidates:
  - "A call that nobody is streaming is NOT invisible any more. `provider.WithCallProgress(ctx, func(provider.CallProgress))` attaches one callback to a context, and every STREAMED call under a waiting controller reports going out, parking on a provider's pacing, thinking, writing and how it ended — so a division, a sizing pass, a mark read or any other caller of internal/session's callRole can be drawn while it runs. Until this, `WithStreamObserver` was the only per-call seam and it carries the ANSWER's text, which is exactly what an errand must not type into a room."
  - "The seam is per QUESTION and not per request, which is the difference a reader has to know. One question is many requests — a hedge puts two on the wire at once, a refusal starts a rescue arm, and a retry, a repaired 400 and a rung of the endpoint ladder each close the model-call log's row and open another. `provider.CallProgress.Attempt` says which request a report is about (0 for the caller's, 1 and up for a rescue) and `provider.CallEnded` is said EXACTLY ONCE, by the door the question returns through. A reader may settle its row on that one ending; it may not settle on an attempt."
  - "A losing arm of a race never becomes the question's ending. Its row says `context canceled` — exhaust, not failure — so `provider.CallEndCancelled` is latched only when nothing else has landed, and any real ending after it takes its place. A surface that read a lost arm as the call being abandoned would be drawing this build's own hedging policy as provider weather."
  - "`provider.WithPacingNotice` is no longer its own account of anything. The pacing park is a PHASE of the same state machine (`provider.CallPaced`), and internal/provider/dispatch.go flips the older bool and the phase from one `park(bool)` closure, so the two cannot disagree. The bool door survives only because its one caller is internal/session/agent.go; retiring it is issue #877, which is the condition on leaving two doors for one wave."
  - "The token counts, the first token and the ending on that seam are NOT counted a second time. They are forwarded from internal/provider/armwatch.go's `streamWatch.note` — the one place every streamed reading is already folded for the hazard controller — and from internal/provider/calllog.go's `Client.record`, the model-call log's only writer, whose law is that every attempt writes a start row and exactly one row that ends it. A `go/ast` law (`TestTheCallProgressSeamHasOneFeeder`) fails the build on a second feeder, and it walks the SELECTOR CHAIN rather than the method names: `….progress.anything` outside armwatch.go is the failure, because `opened` and `paced` are both spelled on other types here and a law that fires on a name is a law somebody deletes the next time it is wrong."
  - "`callProgressBeat` is 33ms and it is `internal/tui3`'s `frameInterval`, not a number this package chose: two reports inside one painted frame differ only in which is thrown away. It is deliberately NOT the link's `remoteFrameInterval` (3× the frame) — a local surface held to the link's stride would tick a token count on one frame in three. internal/provider cannot import internal/tui3, so PERF.md carries the pair and a change to `frameInterval` is a change to this."
  - "`lanestub.Profile.TearAfter` stages the connection going away underneath an answer — so many visible deltas, then no finish frame, no usage and no sentinel. It counts deltas across BOTH shapes of answer, the staged `Answer` and the generated tokens, so the knob cannot silently do nothing. It is the ending the other knobs could not stage: a refusal is a status, a stall is silence, a cancel is this process's own decision, and a reply that was arriving and then was not is none of those. It was 1,884 of 1,887 in-stream failures in the 2026-09-10 census."
---

A task room can draw a worker's call — the model thinking, the tokens climbing,
the seconds since it went out — because a worker's turn streams through the
session's own observer. Nothing else could. A division, a sizing pass and a mark
being read are each one call, and each drew a blank line for between 12 and 219
measured seconds, which is indistinguishable from a process that has stopped.

The events were never missing. Every streamed reading in this process is already
folded into the hazard controller's watch, and every attempt's two ends are
already written down by the one door the model-call log has. This is the seam
that forwards them, and it adds no decoder, no counter and no second opinion
about when a call began.
