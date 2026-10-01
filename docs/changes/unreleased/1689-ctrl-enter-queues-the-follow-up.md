---
kind: changed
title: ctrl+enter queues a follow-up, and queued messages can be taken back
pr: 1689
surface: [chat, engine]
invalidates:
  - "Queueing a message for after the current turn was `ctrl+q`. It is `ctrl+enter`, on a terminal that can tell that chord from a plain enter, and `ctrl+q` is deliberately unbound."
  - "A queued follow-up had no take-backs. It does now: a click on its row takes that one message back out of the session's queue before its turn starts — pasted documents and all — and its stream ends with no events. `↑` does not reach the queue; it stays the parked block's and history's key."
  - "The keys row under the box named `ctrl+shift+enter stops and sends` while a turn ran. That slot is the queue key's now — `enter steers it in · ctrl+enter queue · esc interrupt`, with words in the box on a terminal that can send the chord. `ctrl+shift+enter` still stops and sends and the key sheet lists it; the foot no longer names it."
  - "The queued queue drew only a count, `after yield · N`. It draws one row per message under the queued glyph, dim, above the box, with a dim line that says what is waiting and how to take one back."
  - "`ctrl+enter` marked a draft as a standing order. The chord is queueing's now; the explicit marked door is `/standing <words>`, which works on every terminal, and the hint under the box says the command."
  - "The manual said plain terminals could queue with `ctrl+q`. Queueing needs the same terminal support `ctrl+shift+enter` does; on a terminal that cannot send the chords the follow-up queue is not available."
---

`UnqueueFollowUp` is on the engine and crosses the wire ([MethodUnqueueFollowUp]):
the surface takes a message back by the stream it has held since the moment it
queued, the engine answers false when the turn already drained it — in which case
the row stays and the message runs — and the take-back is asked off the update loop
like every other door. A hosted chat's agent is the telemetry tee
(`cmd/codeaf`'s countingAgent), which hands the surface a copy of each
follow-up's stream; the tee maps the copy back to the stream the remote agent
minted, or the take-back names a stream the far end never saw and answers false.