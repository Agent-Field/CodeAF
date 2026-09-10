---
kind: fixed
title: A question crosses the engine link, so the road an ordinary aforge takes has one
pr: 768
surface: [chat, remote]
invalidates:
  - "Questions did not cross the session-host link, which is the road a plain `aforge` or `aforge chat` in a project takes: internal/remote.Agent had ResolveQuestion and neither WatchQuestions nor OpenQuestions, internal/tui3 asserts the three as ONE seam, so no question was ever drawn there and every `ask` stopped the turn with nothing on any screen. They cross now, on wire version 14's `question` lane, and the answer crosses whole. Anything that says questions need `aforge chat --no-host`, or that answering over `--host` is not built, is stale — internal/manual/chat/questions.md carried both denials and no longer does."
  - "The wire protocol is version 14, not 13. An engine of a different version is refused at the door, as it has always been."
  - "A task page opened onto work in a conversation this window is not in draws that conversation's open question, dim, with no key on it (`? <head> · answered in the window that owns this work`). It used to draw a running clock over work that had stopped on a question an hour earlier, because the owner's task lane cannot say a node is waiting on a person — a node sitting on a question is still `running`."
  - "A session stopped on the model's own `ask` writes `waiting` and the whole question to its presence file, so home and every other window read it. It used to write `working`."
---

The session host landed in #736 and turned an ordinary `aforge` into a SURFACE
talking to this machine's engine over a unix socket. Questions arrived after it,
on a standing subscription of their own that no frame carried — so the half a
person presses worked perfectly (`ResolveQuestion` has carried `session.Answer`
whole since it landed) and the half that puts a question on the screen did not
exist. Measured on the shipped road: three minutes on an `ask`, no block, no
chip, the turn still running.

Version 14 is one intent up (`Question.Watch`) and one fact down (the `question`
frame), on the shape versions 8 and 11 named for the task rail and the harness
lane. Everything still open is replayed the moment a surface attaches, so a
question raised into an empty room is waiting when somebody arrives.
`OpenQuestions` is answered from what that lane has already said rather than from
a round trip, on version 4's rule that facts come down unasked.

The number moves rather than riding version 13 for `Task.Watch`'s reason: an
older engine answers the new subscription with "no such method" and leaves the
lane dark with nothing on the screen saying why. Refused at the door, a person is
told their engine is an older aforge. Never to silence.

And the law that would have caught it is checkable now: `tui3.DrawsQuestions` is
`DrawsTasks`' sibling, asserted in cmd/aforge where the door holds both halves.
