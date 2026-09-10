---
kind: fixed
title: The live rate, the via rider and the phase words cross to a conversation on an engine host
pr: 747
surface: [chat]
invalidates:
  - "An engine-hosted conversation drew no live rate at the right edge of the status row, no `via <machine>` rider on the seam, no phase words (`connecting · 1.2s`, `first word …`, `thinking`, `writing`) and no `served` row in `/status`. The pulse fell back to `waiting for <model> · 4s` and stayed there. That was EVERY BARE `aforge` IN A WORKSPACE, not an edge case: a plain launch does not run the engine in the window's own process — it dials the machine's session host (`internal/enginehost`) and the surface talks to a `remote.Agent`. All four figures are measured where the request is made, posted onto `internal/session`'s two process-global readers, and read by `internal/tui3` — a road that ran entirely inside the engine's process and stopped there. The surface's code for all four was right the whole time; there was no road. Two push frame kinds carry them now, `\"phase\" (PhaseWire)` and `\"lane\" (LaneWire)`, and `--host` gets the same thing across a real network."
  - "Nothing on the wire named a conversation, because nothing on the wire needed to. Phase and lane news now carry a `Session` — the ROOT conversation's id, so a task node's readings are filed under the window that asked for the work rather than under the node's own journal — stamped on the turn context beside the role (`provider.WithSession`, `internal/session`'s newskey.go). A session host registers ONE reader for every conversation it runs, so news that could not name its own would be drawn on every window at once."
  - "A stalled PINNED lane on a hosted conversation BORROWED the alternative without asking, because `provider.phaseListening` correctly read that build as having nobody to ask. It asks now — `coreweave is slow · switch to auto? (y)` reaches the window — and `y` has a road home: `MethodAnswerLaneOffer`, which rides the existing wire version because an older engine's `no such method` reads as false, and false already means `there was nothing to answer`."
  - "The wire version was 13 and still is. Neither new kind needs a door: an unknown kind is ignored by any surface (client.go's reader) and an engine that never sends one leaves a surface drawing exactly what it drew before, which is nothing."
  - "Anything that carried a moment across the wire carried a moment. Phase news carries ELAPSED TIMES — how long this phase has lasted, how long is left before something is done about the wait — and the surface rebuilds both against its own clock, because it ages a phase out after fifteen seconds and counts a clock up from when it began, and a wall clock from a machine a few seconds out would either drop every phase on arrival or draw one that started before it did. A lane sighting carries no moment at all and is stamped where it lands."
  - "The offer token rode inside `PhaseNews`. It does not cross: the token is the engine's own bookkeeping, and the answer names the conversation the person is sitting in rather than a token they were handed."
---

The report was that the bottom of the screen had gone quiet: no tokens a second
while an answer wrote itself, no machine named beside the model afterwards, and
the pulse stuck on `waiting for <model> · 4s` — on the owner's own machine,
opening aforge the way it is meant to be opened.

Nothing on the surface was wrong. What was missing was that a plain launch has
been a two-process arrangement since the session host landed, and the three
seams that carry a turn's telemetry — the phase clock, the lane sighting, and
the reader each is pushed onto — were written before that and never crossed.

Two things had to be true to build the road, and they are the whole change. News
had to say WHOSE it is, because one host runs many conversations behind one
reader. And it may never wait: the fan-out runs on a turn's own stream
goroutine, between two deltas, so each window has an outbox with a floor under
it and a window that has stopped reading loses readings rather than holding up
the answer they describe — safe in a way a dropped event never is, because a
phase says itself again every second while it lasts.

One thing followed. A pinned lane that stalls used to borrow quietly on a hosted
conversation, on the honest grounds that there was nobody to ask. There is
somebody now, so it asks — and the `y` that answers got a road back in the same
change, because a question drawn over a key that does nothing is worse than the
silence it replaced.
