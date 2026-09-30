---
kind: changed
title: ctrl+enter queues a follow-up, and queued messages can be taken back
pr: 1689
surface: [chat, engine]
invalidates:
  - "Queueing a message for after the current turn was `ctrl+q`. It is `ctrl+enter`, on a terminal that can tell that chord from a plain enter, and `ctrl+q` is deliberately unbound."
  - "A queued follow-up had no take-backs. It does now: `↑` or a click on its row takes that one message back out of the session's queue before its turn starts, and its stream ends with no events."
  - "The queued queue drew only a count, `after yield · N`. It draws one row per message under the queued glyph, dim, above the box, with a dim line that says what is waiting and how to take one back."
  - "`ctrl+enter` marked a draft as a standing order. The chord is queueing's now; the explicit marked door is `/standing <words>`, which works on every terminal, and the hint under the box says the command."
  - "The manual said plain terminals could queue with `ctrl+q`. Queueing needs the same terminal support `ctrl+shift+enter` does; on a terminal that cannot send the chords the follow-up queue is not available."
---

`UnqueueFollowUp` is on the engine and crosses the wire ([MethodUnqueueFollowUp]):
the surface takes a message back by the stream it has held since the moment it
queued, the engine answers false when the turn already drained it — in which case
the row stays and the message runs — and the take-back is asked off the update loop
like every other door.