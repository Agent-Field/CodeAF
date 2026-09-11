---
kind: fixed
title: a question you talk past comes down, and your own words go out at once
pr: 870
surface: [chat, engine, docs]
invalidates:
  - "An interrupted turn could sit idle for a minute before resuming. What was actually happening was worse and had no clock in it at all: a sentence typed while an `ask` question stood open queued behind that question, and the question was waiting for the same sentence, so NO REQUEST WAS EVER MADE. Measured 2026-09-10 in two conversations — seven minutes and fifty-two seconds in one, until the person pressed Escape, which then dropped their words with the stopped turn. A steer now ends every `ask` this session is parked on, and the next request is on the wire inside `lane.SpokenWithin`."
  - "A parked `ask` had two endings — answered, or its own lane let go — held in a bare `Agent.askWaits` map that four files reached into with their own delete-then-touch. It has THREE, and they are named methods on one type with the rule in its doc comment (`internal/session/askwait.go`): `answerLocked`, `talkedPastLocked`, `letGoLocked`. The map is gone; nothing outside that file may touch the channels."
  - "`internal/manual/chat/questions.md` said that with nothing pressed, what you type is a message to the conversation and THE QUESTION WAITS. It does not wait: sending a message takes the question back, the row reads `no longer needed · you said something else instead`, and your own line is marked `took this instead of the question`."
  - "A question released this way is not an answer and is recorded as none: the `ask` tool hands the model a sentence saying nobody answered and that the person's words are the next thing it will read, and no `DecisionRecord` is written. A model that assumed a released question had been answered with a default is wrong."
  - "`hedgeRace.abandon` — reached exactly when a person's steer cancelled the race — drained its arms for up to `abandonGrace` BEFORE answering its caller, so a cancelled call could spend the whole of `lane.SpokenWithin` in front of somebody who had just typed a correction. The drain, the offer's withdrawal and the two budget notes are `hedgeRace.accountForTheAbandoned` now, on a guarded goroutine nobody waits on; the caller gets `ctx.Err()` immediately. `spokenWaitExceptions`'s reason for `(*hedgeRace).drainArms` changed with it — it is not in the request path any more, rather than being in it with nobody listening."
---

There is no resume branch and no new timer: the turn goes back round
`Agent.runTurn`'s own loop, through the same drain and the same send every other
step uses, carrying the question's result and then the person's words. What was
missing was an ending for the wait, not a road back from it.
